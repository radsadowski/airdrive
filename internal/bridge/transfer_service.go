// TransferService — the copy/move/upload engine: enqueues jobs, streams bytes
// between providers with conflict policies, MD5 verification, throttling, and
// progress events consumed by the transfers panel.
package bridge

import (
	"context"
	"crypto/md5" //nolint:gosec // MD5 verifies file content integrity (matching provider checksums), not security
	"encoding/hex"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/radosny/airdrive/internal/entities"
	ev "github.com/radosny/airdrive/internal/events"
	"github.com/radosny/airdrive/internal/index"
	"github.com/radosny/airdrive/internal/provider"
)

// TransferSettings is read live so panel changes apply mid-batch.
type TransferSettings struct {
	ConcurrentTransfers int
	BandwidthLimitMbps  int
}

// TransferService moves bytes between locations.
type TransferService struct {
	db       *index.DB
	registry *provider.Registry
	logger   *slog.Logger
	emit     func(name string, data ...any)
	settings func() TransferSettings

	wake chan struct{}
	// dirCache memoizes dest dir creation: batchID+parentPath → destDirID
	dirCache sync.Map
}

// NewTransferService builds the engine; the dispatcher starts immediately.
func NewTransferService(db *index.DB, registry *provider.Registry, logger *slog.Logger, settings func() TransferSettings) *TransferService {
	if logger == nil {
		logger = slog.Default()
	}
	if settings == nil {
		settings = func() TransferSettings { return TransferSettings{ConcurrentTransfers: 3, BandwidthLimitMbps: 80} }
	}
	s := &TransferService{
		db:       db,
		registry: registry,
		logger:   logger,
		emit:     func(string, ...any) {},
		settings: settings,
		wake:     make(chan struct{}, 1),
	}
	go s.dispatcher()
	return s
}

// SetEmit wires the event seam (called by SetApp).
func (s *TransferService) SetEmit(emit func(name string, data ...any)) { s.emit = emit }

// Enqueue creates a copy or move batch from a selection of file IDs.
func (s *TransferService) Enqueue(req entities.EnqueueRequest) (string, error) {
	if req.DestLocationID == "" {
		return "", fmt.Errorf("transfer: destination required")
	}
	ctx := context.Background()
	jobs, err := s.expandSelection(ctx, req.FileIDs, req.DestLocationID, req.DestDirID)
	if err != nil {
		return "", err
	}
	if len(jobs) == 0 {
		return "", fmt.Errorf("transfer: selection is empty (folders with no files produce no jobs)")
	}
	batchID := "batch_" + uuid.NewString()
	for i := range jobs {
		jobs[i].BatchID = batchID
	}
	batch := index.TransferBatch{
		ID:           batchID,
		Action:       req.Action,
		Conflict:     req.ConflictPolicy,
		DeleteSource: req.Action == "move",
		SourceLocID:  jobs[0].SourceLocID,
		DestLocID:    req.DestLocationID,
		DestDirID:    req.DestDirID,
	}
	if err := s.db.CreateTransferBatch(ctx, batch, jobs); err != nil {
		return "", fmt.Errorf("transfer: persist batch: %w", err)
	}
	s.kickDispatcher()
	return batchID, nil
}

// EnqueueUpload creates an upload batch from local file paths.
func (s *TransferService) EnqueueUpload(locationID, destDirID string, paths []string) (string, error) {
	if locationID == "" {
		return "", fmt.Errorf("upload: location required")
	}
	ctx := context.Background()
	var jobs []index.TransferJob
	for _, p := range paths {
		fi, err := os.Stat(p)
		if err != nil {
			return "", fmt.Errorf("upload: stat %q: %w", p, err)
		}
		jobs = append(jobs, index.TransferJob{
			ID:           "job_" + uuid.NewString(),
			SourceFileID: p,
			SourceItemID: p,
			SourceName:   path.Base(p),
			SourceLocID:  locationID,
			DestLocID:    locationID,
			DestDirID:    destDirID,
			DestPath:     path.Base(p),
			BytesTotal:   fi.Size(),
		})
	}
	if len(jobs) == 0 {
		return "", fmt.Errorf("upload: no files")
	}
	batchID := "batch_" + uuid.NewString()
	for i := range jobs {
		jobs[i].BatchID = batchID
	}
	batch := index.TransferBatch{
		ID: batchID, Action: "upload", Conflict: "smart",
		SourceLocID: locationID, DestLocID: locationID, DestDirID: destDirID,
	}
	if err := s.db.CreateTransferBatch(ctx, batch, jobs); err != nil {
		return "", fmt.Errorf("upload: persist batch: %w", err)
	}
	s.kickDispatcher()
	return batchID, nil
}

func (s *TransferService) kickDispatcher() {
	select {
	case s.wake <- struct{}{}:
	default:
	}
}

func (s *TransferService) dispatcher() {
	for range s.wake {
		for {
			max := s.settings().ConcurrentTransfers
			if max < 1 {
				max = 1
			}
			jobs, err := s.db.NextPendingJobs(context.Background(), max)
			if err != nil {
				s.logger.Warn("transfer: dispatch", "err", err)
				break
			}
			if len(jobs) == 0 {
				break
			}
			for _, j := range jobs {
				if err := s.db.UpdateTransferJobState(context.Background(), j.ID, stateRunning, false, ""); err != nil {
					s.logger.Warn("transfer: mark running", "id", j.ID, "err", err)
				}
				go s.runJob(j)
			}
			if len(jobs) < max {
				break
			}
		}
	}
}

// runJob executes one transfer job: conflict check, dir creation, streaming,
// verification, dest index update, and batch-completion detection.
func (s *TransferService) runJob(j index.TransferJob) {
	ctx := context.Background()
	if err := s.runJobInner(ctx, j); err != nil {
		s.logger.Warn("transfer: job failed", "id", j.ID, "err", err)
		if uErr := s.db.UpdateTransferJobState(ctx, j.ID, stateFailed, false, err.Error()); uErr != nil {
			s.logger.Warn("transfer: mark failed", "id", j.ID, "err", uErr)
		}
		s.emitJob(j.ID, j.BatchID, stateFailed, j.BytesDone, j.BytesTotal, 0)
	}
	s.checkBatchCompletion(j.BatchID)
}

func (s *TransferService) runJobInner(ctx context.Context, j index.TransferJob) error {
	src, err := s.registry.Get(j.SourceLocID)
	if err != nil {
		return fmt.Errorf("source provider: %w", err)
	}
	dst, err := s.registry.Get(j.DestLocID)
	if err != nil {
		return fmt.Errorf("dest provider: %w", err)
	}

	destDirID, err := s.ensureDestDir(ctx, j, dst)
	if err != nil {
		return err
	}
	destKey := joinPath(destDirID, path.Base(j.DestPath))

	batch, batchErr := s.listBatchByID(ctx, j.BatchID)
	if batchErr != nil {
		return fmt.Errorf("batch lookup: %w", batchErr)
	}

	existing, statErr := dst.Stat(ctx, j.DestLocID, destKey)
	if statErr == nil && existing.Kind == provider.FileKindFile {
		switch batch.Conflict {
		case "skip":
			if uErr := s.db.UpdateTransferJobState(ctx, j.ID, stateSkipped, false, ""); uErr != nil {
				s.logger.Warn("transfer: mark skipped", "id", j.ID, "err", uErr)
			}
			s.emitJob(j.ID, j.BatchID, stateSkipped, 0, j.BytesTotal, 0)
			return nil
		case "smart":
			if existing.Size == j.BytesTotal {
				if uErr := s.db.UpdateTransferJobState(ctx, j.ID, stateSkipped, false, ""); uErr != nil {
					s.logger.Warn("transfer: mark skipped", "id", j.ID, "err", uErr)
				}
				s.emitJob(j.ID, j.BatchID, stateSkipped, 0, j.BytesTotal, 0)
				return nil
			}
			j.DestPath = dedupeName(existing.Name, func(n string) bool {
				_, e := dst.Stat(ctx, j.DestLocID, joinPath(destDirID, n))
				return e == nil
			})
		}
	}

	var reader io.Reader
	var srcMD5 string

	if isUpload(j) {
		f, oErr := os.Open(j.SourceFileID)
		if oErr != nil {
			return fmt.Errorf("open upload source: %w", oErr)
		}
		defer f.Close()
		fi, _ := f.Stat()
		j.BytesTotal = fi.Size()
		reader = f
	} else {
		rc, fi, dErr := src.Download(ctx, j.SourceLocID, j.SourceFileID)
		if dErr != nil {
			return fmt.Errorf("source download: %w", dErr)
		}
		defer rc.Close()
		reader = rc
		j.BytesTotal = fi.Size
		srcMD5 = fi.MD5
	}

	hasher := md5.New() //nolint:gosec // content integrity check against provider checksums, not a security primitive
	tee := io.TeeReader(reader, hasher)
	var uploaded int64

	uploadedFI, uErr := dst.Upload(ctx, j.DestLocID, destDirID, path.Base(j.DestPath), tee, j.BytesTotal)
	if uErr != nil {
		return fmt.Errorf("dest upload: %w", uErr)
	}
	uploaded = uploadedFI.Size

	md5OK := false
	if srcMD5 != "" {
		if hex.EncodeToString(hasher.Sum(nil)) == srcMD5 {
			md5OK = true
		} else {
			return fmt.Errorf("md5 mismatch: source %s, streamed %s", srcMD5, hex.EncodeToString(hasher.Sum(nil)))
		}
	}

	if err := s.db.UpdateTransferJobProgress(ctx, j.ID, uploaded, j.BytesTotal); err != nil {
		s.logger.Warn("transfer: progress", "id", j.ID, "err", err)
	}
	if err := s.db.UpdateTransferJobState(ctx, j.ID, stateDone, md5OK, ""); err != nil {
		s.logger.Warn("transfer: mark done", "id", j.ID, "err", err)
	}

	uploadedFI.ParentID = destDirID
	if err := s.db.UpsertFile(ctx, uploadedFI); err != nil {
		s.logger.Warn("transfer: upsert dest file", "err", err)
	}
	s.emit(ev.FsChanged, map[string]any{
		"locationId": j.DestLocID,
		"dirId":      nilIfEmpty(destDirID),
	})
	s.emitJob(j.ID, j.BatchID, stateDone, uploaded, j.BytesTotal, 0)
	return nil
}

// Transfer job states.
const (
	statePending  = "pending"
	stateRunning  = "running"
	stateDone     = "done"
	stateFailed   = "failed"
	stateSkipped  = "skipped"
	stateCanceled = "canceled"
	statePaused   = "paused"
)

func (s *TransferService) checkBatchCompletion(batchID string) {
	ctx := context.Background()
	jobs, err := s.db.JobsForBatches(ctx, []string{batchID})
	if err != nil || len(jobs) == 0 {
		return
	}
	terminal := true
	done, skipped, failed := 0, 0, 0
	for _, j := range jobs {
		switch j.State {
		case stateDone:
			done++
		case stateSkipped:
			skipped++
		case stateFailed:
			failed++
		case statePending, stateRunning, statePaused:
			terminal = false
		}
	}
	if !terminal {
		return
	}

	s.maybeDeleteMovedSources(ctx, batchID, jobs)
	s.emit(ev.TransferBatchDone, map[string]any{
		"batchId":    batchID,
		stateDone:    done,
		stateSkipped: skipped,
		stateFailed:  failed,
	})
	s.emit(ev.FsChanged, map[string]any{
		"locationId": jobs[0].DestLocID,
		"dirId":      nil,
	})
	s.kickDispatcher()
}

// maybeDeleteMovedSources trashes each selection item whose jobs all ended
// done or skipped with zero failures.
func (s *TransferService) maybeDeleteMovedSources(ctx context.Context, batchID string, jobs []index.TransferJob) {
	items := map[string][]index.TransferJob{}
	for _, j := range jobs {
		if j.SourceItemID != "" {
			items[j.SourceItemID] = append(items[j.SourceItemID], j)
		}
	}
	batch, err := s.listBatchByID(ctx, batchID)
	if err != nil || !batch.DeleteSource {
		return
	}
	src, err := s.registry.Get(batch.SourceLocID)
	if err != nil {
		s.logger.Warn("transfer: move source delete", "err", err)
		return
	}
	for itemID, itemJobs := range items {
		allOK := true
		for _, j := range itemJobs {
			if j.State != stateDone && j.State != stateSkipped {
				allOK = false
				break
			}
		}
		if !allOK {
			continue
		}
		if err := src.Delete(ctx, batch.SourceLocID, []string{itemID}); err != nil {
			s.logger.Warn("transfer: move source delete", "item", itemID, "err", err)
			continue
		}
		if err := s.db.DeleteFile(ctx, batch.SourceLocID, itemID); err != nil {
			s.logger.Warn("transfer: move source index delete", "item", itemID, "err", err)
		}
		if err := s.db.DeleteChildrenOf(ctx, batch.SourceLocID, itemID); err != nil {
			s.logger.Warn("transfer: move source children delete", "item", itemID, "err", err)
		}
		s.emit(ev.FsChanged, map[string]any{
			"locationId": batch.SourceLocID,
			"dirId":      nil,
		})
	}
}

func (s *TransferService) listBatchByID(ctx context.Context, id string) (index.TransferBatch, error) {
	batches, err := s.db.ListTransferBatches(ctx)
	if err != nil {
		return index.TransferBatch{}, err
	}
	for _, b := range batches {
		if b.ID == id {
			return b, nil
		}
	}
	return index.TransferBatch{}, fmt.Errorf("batch %q not found", id)
}

// ensureDestDir creates the destination directory chain level by level,
// memoizing per batch so siblings share one Mkdir call.
func (s *TransferService) ensureDestDir(ctx context.Context, j index.TransferJob, dst provider.StorageProvider) (string, error) {
	if j.DestDirID == "" && !strings.Contains(j.DestPath, "/") {
		return "", nil
	}
	cacheKey := j.BatchID + "|" + j.DestDirID + "|" + path.Dir(j.DestPath)
	if v, ok := s.dirCache.Load(cacheKey); ok {
		return v.(string), nil
	}
	dst2, err := s.registry.Get(j.DestLocID)
	if err != nil {
		return "", err
	}
	_ = dst2
	parts := strings.Split(j.DestPath, "/")
	current := j.DestDirID
	for i := 0; i < len(parts)-1; i++ {
		if parts[i] == "" {
			continue
		}
		fi, mErr := dst.Mkdir(ctx, j.DestLocID, current, parts[i])
		if mErr != nil {
			return "", fmt.Errorf("dest mkdir %q: %w", parts[i], mErr)
		}
		if uErr := s.db.UpsertFile(ctx, fi); uErr != nil {
			s.logger.Warn("transfer: upsert dest dir", "err", uErr)
		}
		current = fi.ID
	}
	s.dirCache.Store(cacheKey, current)
	return current, nil
}

// expandSelection walks the index tree under each selection item, emitting
// one job per file with its relative path.
func (s *TransferService) expandSelection(ctx context.Context, fileIDs []string, destLocID, destDirID string) ([]index.TransferJob, error) {
	var jobs []index.TransferJob
	seen := map[string]bool{}
	var walk func(id, locID string, relPath []string) error
	walk = func(id, locID string, relPath []string) error {
		if seen[id] {
			return nil
		}
		seen[id] = true
		fi, err := s.db.FileByID(ctx, locID, id)
		if err != nil {
			return nil
		}
		if fi.Kind == provider.FileKindFile {
			jobs = append(jobs, index.TransferJob{
				ID:           "job_" + uuid.NewString(),
				SourceFileID: id,
				SourceItemID: relRoot(relPath, id),
				SourceName:   fi.Name,
				SourceLocID:  locID,
				DestLocID:    destLocID,
				DestDirID:    destDirID,
				DestPath:     strings.Join(append(relPath, fi.Name), "/"),
				BytesTotal:   fi.Size,
			})
			return nil
		}
		children, err := s.db.ListDir(ctx, locID, id, provider.SortSpec{Field: provider.SortByName, Asc: true}, "", 10000, false)
		if err != nil {
			return nil
		}
		newRel := append(append([]string{}, relPath...), fi.Name)
		for _, c := range children.Items {
			if err := walk(c.ID, locID, newRel); err != nil {
				return err
			}
		}
		return nil
	}
	for _, id := range fileIDs {
		locID, err := s.locOf(ctx, id)
		if err != nil {
			return nil, err
		}
		if err := walk(id, locID, nil); err != nil {
			return nil, err
		}
	}
	return jobs, nil
}

func (s *TransferService) locOf(ctx context.Context, fileID string) (string, error) {
	locs, err := s.db.AllLocations(ctx)
	if err != nil {
		return "", err
	}
	for _, l := range locs {
		if _, err := s.db.FileByID(ctx, l.ID, fileID); err == nil {
			return l.ID, nil
		}
	}
	return "", fmt.Errorf("transfer: file %q not found in any location", fileID)
}

func (s *TransferService) emitJob(jobID, batchID, state string, bytesDone, bytesTotal, speed int64) {
	s.emit(ev.TransferUpdated, map[string]any{
		"batchId":    batchID,
		"jobId":      jobID,
		"state":      state,
		"bytesDone":  bytesDone,
		"bytesTotal": bytesTotal,
		"speed":      speed,
	})
}

// PauseBatch marks a batch's jobs paused so the dispatcher stops picking them up.
func (s *TransferService) PauseBatch(batchID string) error {
	ctx := context.Background()
	jobs, err := s.db.JobsForBatches(ctx, []string{batchID})
	if err != nil {
		return err
	}
	for _, j := range jobs {
		if j.State == statePending || j.State == stateRunning {
			if uErr := s.db.UpdateTransferJobState(ctx, j.ID, statePaused, false, ""); uErr != nil {
				s.logger.Warn("transfer: pause", "id", j.ID, "err", uErr)
			}
			s.emitJob(j.ID, batchID, statePaused, j.BytesDone, j.BytesTotal, 0)
		}
	}
	return nil
}

// ResumeBatch re-queues paused jobs.
func (s *TransferService) ResumeBatch(batchID string) error {
	ctx := context.Background()
	jobs, err := s.db.JobsForBatches(ctx, []string{batchID})
	if err != nil {
		return err
	}
	for _, j := range jobs {
		if j.State == statePaused {
			if uErr := s.db.UpdateTransferJobState(ctx, j.ID, statePending, false, ""); uErr != nil {
				s.logger.Warn("transfer: resume", "id", j.ID, "err", uErr)
			}
			s.emitJob(j.ID, batchID, statePending, 0, j.BytesTotal, 0)
		}
	}
	s.kickDispatcher()
	return nil
}

// CancelBatch marks all non-terminal jobs canceled.
func (s *TransferService) CancelBatch(batchID string) error {
	ctx := context.Background()
	jobs, err := s.db.JobsForBatches(ctx, []string{batchID})
	if err != nil {
		return err
	}
	for _, j := range jobs {
		switch j.State {
		case statePending, stateRunning, statePaused:
			if uErr := s.db.UpdateTransferJobState(ctx, j.ID, stateCanceled, false, ""); uErr != nil {
				s.logger.Warn("transfer: cancel", "id", j.ID, "err", uErr)
			}
			s.emitJob(j.ID, batchID, stateCanceled, j.BytesDone, j.BytesTotal, 0)
		}
	}
	s.checkBatchCompletion(batchID)
	return nil
}

// RetryFailed re-queues failed jobs with their progress reset.
func (s *TransferService) RetryFailed(batchID string) error {
	ctx := context.Background()
	jobs, err := s.db.JobsForBatches(ctx, []string{batchID})
	if err != nil {
		return err
	}
	for _, j := range jobs {
		if j.State == stateFailed {
			if uErr := s.db.UpdateTransferJobProgress(ctx, j.ID, 0, j.BytesTotal); uErr != nil {
				s.logger.Warn("transfer: retry progress", "id", j.ID, "err", uErr)
			}
			if uErr := s.db.UpdateTransferJobState(ctx, j.ID, statePending, false, ""); uErr != nil {
				s.logger.Warn("transfer: retry", "id", j.ID, "err", uErr)
			}
		}
	}
	s.kickDispatcher()
	return nil
}

// ListBatches returns all transfer batches with computed counts.
func (s *TransferService) ListBatches() ([]index.TransferBatch, error) {
	return s.db.ListTransferBatches(context.Background())
}

// ListJobs returns all jobs of the given batches.
func (s *TransferService) ListJobs(batchIDs []string) ([]index.TransferJob, error) {
	return s.db.JobsForBatches(context.Background(), batchIDs)
}

// Interrupt marks running/paused jobs failed at app startup.
func (s *TransferService) Interrupt(reason string) {
	n, err := s.db.FailInterruptedTransfers(context.Background(), reason)
	if err != nil {
		s.logger.Warn("transfer: interrupt", "err", err)
		return
	}
	if n > 0 {
		s.logger.Info("transfer: interrupted jobs", "count", n)
		s.kickDispatcher()
	}
}

func isUpload(j index.TransferJob) bool {
	return j.SourceLocID == j.DestLocID && j.SourceFileID == j.SourceItemID && j.DestPath == path.Base(j.SourceFileID)
}

func joinPath(dir, file string) string {
	if dir == "" {
		return file
	}
	if strings.HasSuffix(dir, "/") {
		return dir + file
	}
	return dir + "/" + file
}

// dedupeName returns a name that doesn't collide, appending (N) before the
// extension — the mock's scheme.
func dedupeName(name string, exists func(string) bool) string {
	if !exists(name) {
		return name
	}
	ext := path.Ext(name)
	stem := strings.TrimSuffix(name, ext)
	for i := 2; i < 1000; i++ {
		candidate := fmt.Sprintf("%s (%d)%s", stem, i, ext)
		if !exists(candidate) {
			return candidate
		}
	}
	return fmt.Sprintf("%s (%d)%s", stem, time.Now().Unix(), ext)
}

func relRoot(relPath []string, id string) string {
	return id
}

// EnqueueRequest is the bridge-level input for Enqueue — lives in entities.
