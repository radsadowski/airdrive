package bridge

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"

	"github.com/google/uuid"
	"github.com/wailsapp/wails/v3/pkg/application"

	"github.com/radosny/airdrive/internal/entities"
	ev "github.com/radosny/airdrive/internal/events"
	"github.com/radosny/airdrive/internal/index"
	"github.com/radosny/airdrive/internal/provider"
)

// Sort field name constants: must match the TypeScript SortField union
// ('name' | 'size' | 'modifiedAt' | 'kind') in frontend/src/lib/api/contract.ts.
const (
	sortFieldSize       = "size"
	sortFieldModifiedAt = "modifiedAt"
	sortFieldKind       = "kind"
)

// File operation kinds and states reported through fileop:progress.
const (
	opDelete  = "delete"
	opMove    = "move"
	opRunning = "running"
	opDone    = "done"
	opError   = "error"
)

// fileOpWorkers bounds how many selected items a delete or in-place move
// handles at once: enough to keep a local disk or an API busy, few enough to
// stay polite with Drive quotas.
const fileOpWorkers = 3

// FileService exposes file operations to the frontend.
type FileService struct {
	app      *application.App
	db       *index.DB
	registry *provider.Registry
	logger   *slog.Logger
	// emit is the event seam; the no-op default lets tests record progress
	// without a Wails app.
	emit func(name string, data ...any)
}

func NewFileService(app *application.App, db *index.DB, registry *provider.Registry, logger *slog.Logger) *FileService {
	return &FileService{app: app, db: db, registry: registry, logger: logger, emit: func(string, ...any) {}}
}

// SetApp injects the Wails app reference after application.New returns.
func (s *FileService) SetApp(app *application.App) {
	s.app = app
	s.emit = func(name string, data ...any) { app.Event.Emit(name, data...) }
}

func (s *FileService) provider(locationID string) (provider.StorageProvider, error) {
	return s.registry.Get(locationID)
}

// ListDir returns a paginated, sorted directory listing from SQLite.
func (s *FileService) ListDir(locationID, dirID string, opts entities.ListDirOpts) (*entities.Page, error) {
	sort := provider.SortSpec{Field: provider.SortByName, Asc: true}
	switch opts.SortField {
	case sortFieldSize:
		sort.Field = provider.SortBySize
	case sortFieldModifiedAt:
		sort.Field = provider.SortByModifiedAt
	case sortFieldKind:
		sort.Field = provider.SortByKind
	}
	sort.Asc = opts.SortAsc || opts.SortField == ""

	page, err := s.db.ListDir(context.Background(), locationID, dirID, sort, opts.Cursor, opts.Limit, opts.IncludeHidden)
	if err != nil {
		return nil, fmt.Errorf("listdir: %w", err)
	}
	result := entities.ToPage(page)
	return &result, nil
}

// Stat returns metadata for a single file.
func (s *FileService) Stat(locationID, fileID string) (*entities.File, error) {
	f, err := s.db.FileByID(context.Background(), locationID, fileID)
	if err != nil {
		p, pErr := s.provider(locationID)
		if pErr != nil {
			return nil, fmt.Errorf("stat: %w", err)
		}
		f, err = p.Stat(context.Background(), locationID, fileID)
		if err != nil {
			return nil, fmt.Errorf("stat: %w", err)
		}
	}
	result := entities.ToFile(f)
	return &result, nil
}

// Mkdir creates a new directory and adds it to the index.
func (s *FileService) Mkdir(locationID, parentID, name string) (*entities.File, error) {
	p, err := s.provider(locationID)
	if err != nil {
		return nil, err
	}
	f, err := p.Mkdir(context.Background(), locationID, parentID, name)
	if err != nil {
		return nil, fmt.Errorf("mkdir: %w", err)
	}
	_ = s.db.UpsertFile(context.Background(), f)
	s.emitFsChanged(locationID, parentID)
	result := entities.ToFile(f)
	return &result, nil
}

// Rename renames a file in both the filesystem and the index.
func (s *FileService) Rename(locationID, fileID, newName string) (*entities.File, error) {
	p, err := s.provider(locationID)
	if err != nil {
		return nil, err
	}
	f, err := p.Rename(context.Background(), locationID, fileID, newName)
	if err != nil {
		return nil, fmt.Errorf("rename: %w", err)
	}
	_ = s.db.DeleteFile(context.Background(), locationID, fileID)
	_ = s.db.UpsertFile(context.Background(), f)
	s.emitFsChanged(locationID, f.ParentID)
	result := entities.ToFile(f)
	return &result, nil
}

// MoveWithin moves files to another directory within the same location,
// reporting the indexed scope of each selected item. Items move by
// fileOpWorkers at a time; a folder is renamed as one provider primitive, so
// its step lands when the whole subtree moved.
func (s *FileService) MoveWithin(locationID string, fileIDs []string, destDirID string) error {
	p, err := s.provider(locationID)
	if err != nil {
		return err
	}
	ctx := context.Background()
	targets := s.opTargets(ctx, locationID, fileIDs)
	if len(targets) == 0 {
		return nil
	}
	total := 0
	for _, t := range targets {
		total += t.count
	}
	oldParents := map[string]string{}
	for _, t := range targets {
		if f, err := s.db.FileByID(ctx, locationID, t.id); err == nil {
			oldParents[t.id] = f.ParentID
		}
	}

	opID := uuid.NewString()
	var done atomic.Int64
	var failed atomic.Bool
	var mu sync.Mutex
	var firstErr error
	s.emitOp(opID, opMove, locationID, 0, total, opRunning, targets[0].name, "")

	var wg sync.WaitGroup
	sem := make(chan struct{}, fileOpWorkers)
	for _, t := range targets {
		if failed.Load() {
			break
		}
		sem <- struct{}{}
		wg.Add(1)
		go func(t opTarget) {
			defer wg.Done()
			defer func() { <-sem }()
			if err := p.MoveWithin(ctx, locationID, []string{t.id}, destDirID); err != nil {
				mu.Lock()
				if firstErr == nil {
					firstErr = err
					failed.Store(true)
					s.emitOp(opID, opMove, locationID, int(done.Load()), total, opError, t.name, err.Error())
				}
				mu.Unlock()
				return
			}
			newPath := filepath.Join(destDirID, filepath.Base(t.id))
			_ = s.db.DeleteFile(ctx, locationID, t.id)
			if f, err := p.Stat(ctx, locationID, newPath); err == nil {
				_ = s.db.UpsertFile(ctx, f)
			}
			if old, ok := oldParents[t.id]; ok {
				s.emitFsChanged(locationID, old)
			}
			n := int(done.Add(int64(t.count)))
			if !failed.Load() {
				s.emitOp(opID, opMove, locationID, n, total, opRunning, t.name, "")
			}
		}(t)
	}
	wg.Wait()

	mu.Lock()
	err = firstErr
	mu.Unlock()
	if err != nil {
		return fmt.Errorf("move: %w", err)
	}
	s.emitOp(opID, opMove, locationID, total, total, opDone, "", "")
	s.emitFsChanged(locationID, destDirID)
	return nil
}

// opTarget is one selected item with the number of index rows it covers.
type opTarget struct {
	id    string
	name  string
	count int
}

// opTargets resolves selections to names and indexed subtree sizes, so a
// folder operation reports the real scope instead of one opaque step.
func (s *FileService) opTargets(ctx context.Context, locationID string, fileIDs []string) []opTarget {
	targets := make([]opTarget, 0, len(fileIDs))
	for _, id := range fileIDs {
		t := opTarget{id: id, count: 1}
		if f, err := s.db.FileByID(ctx, locationID, id); err == nil {
			t.name = f.Name
		}
		if n, err := s.db.CountSubtree(ctx, locationID, id); err == nil && n > 0 {
			t.count = n
		}
		targets = append(targets, t)
	}
	return targets
}

// DeleteFiles moves files to trash and removes them from the index, reporting
// the indexed scope of each selected item so a folder delete shows how many
// entries it covers and which one is in flight. Selected items are removed by
// fileOpWorkers at a time; a folder is still one provider primitive.
func (s *FileService) DeleteFiles(locationID string, fileIDs []string) error {
	p, err := s.provider(locationID)
	if err != nil {
		return err
	}
	ctx := context.Background()
	targets := s.opTargets(ctx, locationID, fileIDs)
	if len(targets) == 0 {
		return nil
	}
	total := 0
	for _, t := range targets {
		total += t.count
	}
	parents := map[string]bool{}
	for _, t := range targets {
		if f, err := s.db.FileByID(ctx, locationID, t.id); err == nil {
			parents[f.ParentID] = true
		}
	}

	opID := uuid.NewString()
	var done atomic.Int64
	var failed atomic.Bool
	var mu sync.Mutex
	var firstErr error
	s.emitOp(opID, opDelete, locationID, 0, total, opRunning, targets[0].name, "")

	var wg sync.WaitGroup
	sem := make(chan struct{}, fileOpWorkers)
	for _, t := range targets {
		if failed.Load() {
			break
		}
		sem <- struct{}{}
		wg.Add(1)
		go func(t opTarget) {
			defer wg.Done()
			defer func() { <-sem }()
			if err := p.Delete(ctx, locationID, []string{t.id}); err != nil {
				mu.Lock()
				if firstErr == nil {
					firstErr = err
					failed.Store(true)
					s.emitOp(opID, opDelete, locationID, int(done.Load()), total, opError, t.name, err.Error())
				}
				mu.Unlock()
				return
			}
			if dErr := s.db.DeleteTree(ctx, locationID, t.id); dErr != nil {
				s.logger.Warn("delete file from index", "location", locationID, "id", t.id, "err", dErr)
			}
			n := int(done.Add(int64(t.count)))
			if !failed.Load() {
				s.emitOp(opID, opDelete, locationID, n, total, opRunning, t.name, "")
			}
		}(t)
	}
	wg.Wait()

	mu.Lock()
	err = firstErr
	mu.Unlock()
	if err != nil {
		return fmt.Errorf("delete: %w", err)
	}
	s.emitOp(opID, opDelete, locationID, total, total, opDone, "", "")
	for parent := range parents {
		s.emitFsChanged(locationID, parent)
	}
	return nil
}

// Download copies files to ~/Downloads and returns the destination paths.
func (s *FileService) Download(locationID string, fileIDs []string) ([]string, error) {
	p, err := s.provider(locationID)
	if err != nil {
		return nil, err
	}
	downloadsDir := filepath.Join(os.Getenv("HOME"), "Downloads")
	var targets []string
	for _, id := range fileIDs {
		rc, fi, err := p.Download(context.Background(), locationID, id)
		if err != nil {
			return nil, fmt.Errorf("download %q: %w", id, err)
		}
		safe := sanitizeName(fi.Name)
		if safe == "" {
			return nil, fmt.Errorf("download %q: unsafe file name", fi.Name)
		}
		destPath := filepath.Join(downloadsDir, safe)
		if err := writeFile(destPath, rc); err != nil {
			rc.Close()
			return nil, fmt.Errorf("write download %q: %w", fi.Name, err)
		}
		rc.Close()
		targets = append(targets, destPath)
	}
	return targets, nil
}

// Search runs an FTS5 query across all indexed files.
func (s *FileService) Search(text string, locationIDs []string, sizeMin, sizeMax int64, modifiedFrom, modifiedTo, kind string, limit int, cursor string) (*entities.Page, error) {
	q := index.SearchQuery{
		Text:         text,
		LocationIDs:  locationIDs,
		SizeMin:      sizeMin,
		SizeMax:      sizeMax,
		ModifiedFrom: modifiedFrom,
		ModifiedTo:   modifiedTo,
		Kind:         kind,
		Limit:        limit,
		Cursor:       cursor,
	}
	page, err := s.db.Search(context.Background(), q)
	if err != nil {
		s.logger.Error("search failed", "text", text, "err", err)
		return nil, fmt.Errorf("search: %w", err)
	}
	s.logger.Info("search", "text", text, "results", len(page.Items))
	result := entities.ToPage(page)
	return &result, nil
}

// ---- helpers ------------------------------------------------------------- //

func (s *FileService) emitFsChanged(locationID, dirID string) {
	s.emit(ev.FsChanged, map[string]any{
		"locationId": locationID,
		"dirId":      nilIfEmpty(dirID),
	})
}

// emitOp reports progress for a file operation shown in the activity bar.
func (s *FileService) emitOp(opID, op, locationID string, done, total int, state, current, errMsg string) {
	s.emit(ev.FileOpProgress, map[string]any{
		"opId":       opID,
		"op":         op,
		"locationId": locationID,
		"done":       done,
		"total":      total,
		"state":      state,
		"current":    current,
		"error":      errMsg,
	})
}

func nilIfEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// sanitizeName reduces a provider-supplied file name to a bare base name,
// neutralizing path traversal from remote metadata.
func sanitizeName(name string) string {
	base := filepath.Base(name)
	if base == "." || base == ".." || base == string(filepath.Separator) {
		return ""
	}
	return base
}

func writeFile(dest string, r io.Reader) error {
	f, err := os.Create(dest) //nolint:gosec // dest is sanitizeName (filepath.Base) joined under the fixed Downloads dir
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = io.Copy(f, r)
	return err
}
