// Sync engine: turns a sync pair's delta into real copies and deletes
// through the existing transfer and file services, then records what it
// did so the next pass is a no-op.
package bridge

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/radosny/airdrive/internal/entities"
	ev "github.com/radosny/airdrive/internal/events"
	"github.com/radosny/airdrive/internal/index"
	"github.com/radosny/airdrive/internal/provider"
	"github.com/radosny/airdrive/internal/syncdelta"
)

// SyncEngine applies sync pair plans through the app's real file-moving
// services. One instance is shared by the bridge SyncService (SyncNow,
// create-with-backfill) and the scheduler.
type SyncEngine struct {
	db        *index.DB
	registry  *provider.Registry
	transfers *TransferService
	files     *FileService
	logger    *slog.Logger
	emit      func(name string, payload map[string]any)

	mu       sync.Mutex
	inflight map[string]bool
}

// NewSyncEngine wires every collaborator the engine needs.
func NewSyncEngine(db *index.DB, registry *provider.Registry, transfers *TransferService, files *FileService, logger *slog.Logger, emit func(name string, payload map[string]any)) *SyncEngine {
	return &SyncEngine{
		db:        db,
		registry:  registry,
		transfers: transfers,
		files:     files,
		logger:    logger,
		emit:      emit,
		inflight:  map[string]bool{},
	}
}

// ReconcileOpts tunes one reconcile pass.
type ReconcileOpts struct {
	// Backfill makes the pass copy what destinations lack, never delete.
	Backfill bool
	// RefreshFirst re-reads both providers into the index before planning.
	RefreshFirst bool
}

// ReconcileHook returns the SyncService-compatible hook for periodic,
// SyncNow, and create-with-backfill callers.
func (e *SyncEngine) ReconcileHook() func(ctx context.Context, pairID string, opts ReconcileOpts) error {
	return e.Reconcile
}

// Reconcile runs one pass for a pair. Reentrancy is blocked per pair.
func (e *SyncEngine) Reconcile(ctx context.Context, pairID string, opts ReconcileOpts) error {
	pair, err := e.db.GetSyncPair(ctx, pairID)
	if err != nil {
		return fmt.Errorf("sync reconcile: %w", err)
	}
	if pair.Paused {
		return nil
	}

	e.mu.Lock()
	if e.inflight[pairID] {
		e.mu.Unlock()
		return nil
	}
	e.inflight[pairID] = true
	e.mu.Unlock()
	defer e.release(pairID)

	return e.reconcile(ctx, pair, opts)
}

// release drops the inflight guard.
func (e *SyncEngine) release(pairID string) {
	e.mu.Lock()
	delete(e.inflight, pairID)
	e.mu.Unlock()
}

// reconcile is the unguarded core of Reconcile.
func (e *SyncEngine) reconcile(ctx context.Context, pair index.SyncPair, opts ReconcileOpts) error {
	if opts.RefreshFirst {
		for _, locID := range []string{pair.LocationA, pair.LocationB} {
			if err := e.refreshLocation(ctx, locID); err != nil {
				e.recordStatus(ctx, pair.ID, outcome{lastErr: "refresh " + locID + ": " + err.Error()})
				return fmt.Errorf("sync %s: refresh %s: %w", pair.ID, locID, err)
			}
		}
	}

	sideA, err := e.buildSide(ctx, pair.LocationA, pair.PathA)
	if err != nil {
		return fmt.Errorf("sync %s: map A: %w", pair.ID, err)
	}
	sideB, err := e.buildSide(ctx, pair.LocationB, pair.PathB)
	if err != nil {
		return fmt.Errorf("sync %s: map B: %w", pair.ID, err)
	}
	state, err := e.db.SyncStateRows(ctx, pair.ID)
	if err != nil {
		return fmt.Errorf("sync %s: state: %w", pair.ID, err)
	}

	steps := syncdelta.Plan(
		syncdelta.Direction(pair.Direction), pair.Actions,
		sideA, sideB, stateRowsToEntries(state), opts.Backfill,
	)
	if len(steps) == 0 {
		e.recordStatus(ctx, pair.ID, outcome{})
		return nil
	}

	totalCopies := 0
	for _, s := range steps {
		if s.Kind == syncdelta.CopyAToB || s.Kind == syncdelta.CopyBToA {
			if s.Src.Kind != string(provider.FileKindDir) {
				totalCopies++
			}
		}
	}
	e.emitProgress(pair.ID, pair.Name, "syncing", outcome{}, 0, totalCopies, 0, 0)

	out := e.apply(ctx, pair, steps)
	if err := e.refreshAfterWrites(ctx, pair, steps); err != nil {
		e.logger.Warn("sync: post-apply refresh failed, state may lag one pass", "pair", pair.ID, "err", err)
	}
	if err := e.recordState(ctx, pair, steps); err != nil {
		e.logger.Warn("sync: record state", "pair", pair.ID, "err", err)
	}
	e.recordStatus(ctx, pair.ID, out)
	e.emitProgress(pair.ID, pair.Name, "idle", out, 0, 0, 0, 0)
	e.emitPair(pair.ID)
	return nil
}

// outcome counts step results for the status row.
type outcome struct {
	copied  int
	deleted int
	skipped int
	failed  int
	lastErr string
}

// apply executes plan steps: dir steps as direct mkdirs, file steps grouped
// per destination directory into transfer batches, delete steps through the
// file service. Batches are awaited so state recording reflects provider
// truth; a job-level failure lands in outcome.failed and replays next pass.
func (e *SyncEngine) apply(ctx context.Context, pair index.SyncPair, steps []syncdelta.Step) outcome {
	out := outcome{}
	dstDirs := map[string]string{}
	type copyGroup struct {
		dest   destSide
		parent string
	}
	groups := map[copyGroup][]string{}
	groupOrder := []copyGroup{}

	for _, step := range steps {
		switch step.Kind {
		case syncdelta.CopyAToB, syncdelta.CopyBToA:
			dest := destOf(pair, step)
			if step.Src.Kind == string(provider.FileKindDir) {
				if _, err := e.materializeDestDir(ctx, dest, step.Path, dstDirs); err != nil {
					e.logger.Warn("sync: mkdir dir", "path", step.Path, "err", err)
					out.failed++
				} else {
					out.copied++
				}
				continue
			}
			parent, err := e.materializeDestDir(ctx, dest, parentRel(step.Path), dstDirs)
			if err != nil {
				e.logger.Warn("sync: dest dir", "path", step.Path, "err", err)
				out.failed++
				continue
			}
			g := copyGroup{dest: dest, parent: parent}
			if _, seen := groups[g]; !seen {
				groupOrder = append(groupOrder, g)
			}
			groups[g] = append(groups[g], step.Src.ID)
			out.copied++

		case syncdelta.DeleteOnA, syncdelta.DeleteOnB:
			loc := pair.LocationA
			if step.Kind == syncdelta.DeleteOnB {
				loc = pair.LocationB
			}
			if err := e.files.DeleteFiles(loc, []string{step.Dst.ID}); err != nil {
				e.logger.Warn("sync: delete", "path", step.Path, "err", err)
				out.failed++
				continue
			}
			out.deleted++
		}
	}

	var batches []string
	for _, g := range groupOrder {
		batchID, err := e.transfers.Enqueue(entities.EnqueueRequest{
			Action:         "copy",
			FileIDs:        groups[g],
			DestLocationID: g.dest.locationID,
			DestDirID:      g.parent,
			ConflictPolicy: "overwrite",
		})
		if err != nil {
			e.logger.Warn("sync: enqueue group", "dest", g.parent, "files", len(groups[g]), "err", err)
			out.failed += len(groups[g])
			out.copied -= len(groups[g])
			continue
		}
		batches = append(batches, batchID)
	}

	if jobs, err := e.waitForBatches(ctx, batches, pair); err != nil || jobs > 0 {
		out.failed += jobs
		out.copied -= jobs
	}
	return out
}

// destSide names where a copy step writes.
type destSide struct {
	locationID string
	rootID     string
}

// destOf picks the destination edge for a copy step.
func destOf(pair index.SyncPair, step syncdelta.Step) destSide {
	if step.Kind == syncdelta.CopyBToA {
		return destSide{locationID: pair.LocationA, rootID: pair.PathA}
	}
	return destSide{locationID: pair.LocationB, rootID: pair.PathB}
}

// materializeDestDir resolves relDir under destRoot on the destination,
// creating missing directory segments along the way.
func (e *SyncEngine) materializeDestDir(ctx context.Context, dest destSide, relDir string, cache map[string]string) (string, error) {
	key := dest.locationID + "|" + relDir
	if id, ok := cache[key]; ok {
		return id, nil
	}
	if relDir == "" {
		cache[key] = dest.rootID
		return dest.rootID, nil
	}
	current := dest.rootID
	rel := ""
	for _, seg := range splitRel(relDir) {
		rel = rel + "/" + seg

		if id, ok := cache[dest.locationID+"|"+rel]; ok {
			current = id
			continue
		}
		id, err := e.findChildDir(ctx, dest.locationID, current, seg)
		if err != nil {
			return "", err
		}
		if id == "" {
			created, err := e.files.Mkdir(dest.locationID, current, seg)
			if err != nil {
				return "", fmt.Errorf("mkdir %q: %w", rel, err)
			}
			id = created.ID
		}
		cache[dest.locationID+"|"+rel] = id
		current = id
	}
	return current, nil
}

// findChildDir resolves a named directory under a parent id from the index.
func (e *SyncEngine) findChildDir(ctx context.Context, locationID, parentID, name string) (string, error) {
	page, err := e.db.ListDir(ctx, locationID, parentID, provider.SortSpec{}, "", 2000, true)
	if err != nil {
		return "", fmt.Errorf("list %q: %w", parentID, err)
	}
	for _, item := range page.Items {
		if item.Kind == provider.FileKindDir && item.Name == name {
			return item.ID, nil
		}
	}
	return "", nil
}

// waitForBatches polls until every enqueued batch completes and returns the
// number of failed jobs. Each poll cycle forwards a live aggregate to the
// UI as sync progress, computed across ALL batches at once so counters
// never jump backwards between batches.
func (e *SyncEngine) waitForBatches(ctx context.Context, batchIDs []string, pair index.SyncPair) (int, error) {
	if len(batchIDs) == 0 {
		return 0, nil
	}
	failed := 0
	for {
		done, total := 0, 0
		var bytesDone, bytesTotal int64
		jobs, err := e.db.JobsForBatches(ctx, batchIDs)
		if err != nil {
			return failed, err
		}
		for _, j := range jobs {
			total++
			bytesDone += j.BytesDone
			bytesTotal += j.BytesTotal
			switch j.State {
			case stateDone, stateFailed, stateSkipped, stateCanceled:
				done++
				if j.State == stateFailed {
					failed++
				}
			}
		}
		e.emitProgress(pair.ID, pair.Name, "syncing", outcome{}, done, total, bytesDone, bytesTotal)
		if total == 0 || done == total {
			break
		}
		select {
		case <-ctx.Done():
			return failed, ctx.Err()
		case <-time.After(200 * time.Millisecond):
		}
	}
	return failed, nil
}

// refreshAfterWrites re-syncs the sides the pass wrote to, so recorded
// state matches what the providers now report.
func (e *SyncEngine) refreshAfterWrites(ctx context.Context, pair index.SyncPair, steps []syncdelta.Step) error {
	written := map[string]bool{}
	for _, s := range steps {
		switch s.Kind {
		case syncdelta.CopyAToB, syncdelta.DeleteOnB:
			written[pair.LocationB] = true
		case syncdelta.CopyBToA, syncdelta.DeleteOnA:
			written[pair.LocationA] = true
		}
	}
	for locID := range written {
		if err := e.refreshLocation(ctx, locID); err != nil {
			return err
		}
	}
	return nil
}

// refreshLocation pumps one provider's changes feed into the index, then
// reconciles deletions for full-inventory walkers (localfs, s3compat):
// rows the walk never touched are gone. Feed-based providers (gdrive)
// signal deletions themselves.
func (e *SyncEngine) refreshLocation(ctx context.Context, locationID string) error {
	p, err := e.registry.Get(locationID)
	if err != nil {
		return err
	}
	cursor, err := e.db.GetSyncCursor(ctx, locationID)
	if err != nil {
		e.logger.Debug("sync: read cursor", "location", locationID, "err", err)
		cursor = ""
	}
	var seen []string
	newCursor, err := p.SyncChanges(ctx, locationID, cursor, func(batch []provider.ChangeEvent) {
		for _, change := range batch {
			switch change.Kind {
			case provider.ChangeDeleted:
				if dErr := e.db.DeleteFile(ctx, locationID, change.FileID); dErr != nil {
					e.logger.Warn("sync: refresh delete", "id", change.FileID, "err", dErr)
				}
			default:
				if change.File != nil {
					if uErr := e.db.UpsertFile(ctx, *change.File); uErr != nil {
						e.logger.Warn("sync: refresh upsert", "err", uErr)
					}
				}
			}
			if change.File != nil {
				seen = append(seen, change.FileID)
			}
		}
	})
	if err != nil {
		return err
	}
	reconciler, _ := p.(provider.Reconciler)
	if reconciler != nil && reconciler.Reconciles() {
		if n, dErr := e.db.DeleteFilesNotIn(ctx, locationID, seen); dErr != nil {
			e.logger.Warn("sync: refresh reconcile", "location", locationID, "err", dErr)
		} else if n > 0 {
			e.logger.Debug("sync: refresh reconciled deletions", "location", locationID, "removed", n)
		}
	}
	return e.db.SetSyncCursor(ctx, locationID, newCursor)
}

// buildSide turns a location's index rows into a subtree-relative map.
func (e *SyncEngine) buildSide(ctx context.Context, locationID, rootID string) (syncdelta.Side, error) {
	rows, err := e.db.AllFiles(ctx, locationID)
	if err != nil {
		return nil, err
	}
	loc, err := e.db.LocationByID(ctx, locationID)
	if err != nil {
		return nil, err
	}
	byID := map[string]provider.FileInfo{}
	for _, f := range rows {
		byID[f.ID] = f
	}
	out := syncdelta.Side{}
	for _, f := range rows {
		rel := relUnderRoot(byID, f, rootID, loc.Root)
		if rel == "" {
			continue
		}
		out[rel] = syncdelta.File{
			Path:  rel,
			ID:    f.ID,
			Kind:  string(f.Kind),
			Size:  f.Size,
			Etag:  f.Etag,
			Mtime: f.ModifiedAt,
		}
	}
	return out, nil
}

// relUnderRoot walks f's parent chain and returns the path relative to
// rootID, or "" when f lies outside it. Providers disagree about what the
// location root looks like in the index: gdrive children point at the root
// folder's row id, while localfs marks the location top with an empty
// parent; locRoot covers the second case.
func relUnderRoot(byID map[string]provider.FileInfo, f provider.FileInfo, rootID, locRoot string) string {
	rel := ""
	cur := f
	for {
		if rel == "" {
			rel = cur.Name
		} else {
			rel = cur.Name + "/" + rel
		}
		if rootID != "" && cur.ParentID == rootID {
			return rel
		}
		if cur.ParentID == "" {
			if rootID == "" || rootID == locRoot {
				return rel
			}
			return ""
		}
		parent, ok := byID[cur.ParentID]
		if !ok {
			return ""
		}
		cur = parent
	}
}

// recordState writes post-apply state for steps whose effects are visible
// in the refreshed index; anything invisible replays next pass.
func (e *SyncEngine) recordState(ctx context.Context, pair index.SyncPair, steps []syncdelta.Step) error {
	sideA, err := e.buildSide(ctx, pair.LocationA, pair.PathA)
	if err != nil {
		return err
	}
	sideB, err := e.buildSide(ctx, pair.LocationB, pair.PathB)
	if err != nil {
		return err
	}
	now := time.Now().UTC().Format(time.RFC3339)
	for _, step := range steps {
		switch step.Kind {
		case syncdelta.CopyAToB, syncdelta.CopyBToA:
			fa, onA := sideA[step.Path]
			fb, onB := sideB[step.Path]
			if !onA || !onB {
				continue
			}
			if err := e.db.UpsertSyncState(ctx, index.SyncState{
				PairID:       pair.ID,
				Path:         step.Path,
				LastEtagA:    fa.Etag,
				LastEtagB:    fb.Etag,
				LastMtimeA:   fa.Mtime.UTC().Format(time.RFC3339),
				LastMtimeB:   fb.Mtime.UTC().Format(time.RFC3339),
				LastSyncedAt: now,
			}); err != nil {
				return err
			}
		case syncdelta.DeleteOnA, syncdelta.DeleteOnB:
			if err := e.db.DeleteSyncState(ctx, pair.ID, step.Path); err != nil {
				return err
			}
		}
	}
	return nil
}

// recordStatus stamps the pair with the outcome of this pass.
func (e *SyncEngine) recordStatus(ctx context.Context, pairID string, out outcome) {
	counts := map[string]int{
		"copied":  out.copied,
		"deleted": out.deleted,
		"skipped": out.skipped,
		"failed":  out.failed,
	}
	blob, _ := json.Marshal(counts)
	if err := e.db.UpdateSyncPairStatus(ctx, pairID, time.Now().UTC().Format(time.RFC3339), string(blob), out.lastErr); err != nil {
		e.logger.Warn("sync: write status", "pair", pairID, "err", err)
	}
}

// emitProgress notifies the UI about one running pass with live aggregates.
func (e *SyncEngine) emitProgress(pairID, name, state string, out outcome, filesDone, filesTotal int, bytesDone, bytesTotal int64) {
	if e.emit == nil {
		return
	}
	e.emit(ev.SyncProgress, map[string]any{
		"pairId":     pairID,
		"name":       name,
		"state":      state,
		"filesDone":  filesDone,
		"filesTotal": filesTotal,
		"bytesDone":  bytesDone,
		"bytesTotal": bytesTotal,
		"copied":     out.copied,
		"deleted":    out.deleted,
		"failed":     out.failed,
	})
}

// emitPair notifies the UI about a completed pass when an emitter is wired.
func (e *SyncEngine) emitPair(pairID string) {
	if e.emit != nil {
		e.emit(ev.SyncUpdated, map[string]any{"pairId": pairID, "state": "idle"})
	}
}

// stateRowsToEntries projects index rows into engine entries.
func stateRowsToEntries(rows []index.SyncState) map[string]syncdelta.StateEntry {
	out := map[string]syncdelta.StateEntry{}
	for _, r := range rows {
		out[r.Path] = syncdelta.StateEntry{
			Path:   r.Path,
			IDA:    r.IDA,
			IDB:    r.IDB,
			EtagA:  r.LastEtagA,
			EtagB:  r.LastEtagB,
			MtimeA: parseTime(r.LastMtimeA),
			MtimeB: parseTime(r.LastMtimeB),
		}
	}
	return out
}

// parseTime tolerantly reads RFC3339, returning zero on failure.
func parseTime(s string) time.Time {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return time.Time{}
	}
	return t
}

// parentRel gives the relative parent of a path ("" at the root level).
func parentRel(p string) string {
	for i := len(p) - 1; i >= 0; i-- {
		if p[i] == '/' {
			return p[:i]
		}
	}
	return ""
}

// splitRel splits a relative dir path into segments.
func splitRel(rel string) []string {
	var out []string
	cur := ""
	for _, ch := range rel {
		if ch == '/' {
			out = append(out, cur)
			cur = ""
			continue
		}
		cur += string(ch)
	}
	if cur != "" {
		out = append(out, cur)
	}
	return out
}
