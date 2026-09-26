package bridge

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/wailsapp/wails/v3/pkg/application"

	"github.com/radosny/airdrive/internal/entities"
	ev "github.com/radosny/airdrive/internal/events"
	"github.com/radosny/airdrive/internal/index"
	"github.com/radosny/airdrive/internal/provider"
)

// Sort field name constants — must match the TypeScript SortField union
// ('name' | 'size' | 'modifiedAt' | 'kind') in frontend/src/lib/api/contract.ts.
const (
	sortFieldSize       = "size"
	sortFieldModifiedAt = "modifiedAt"
	sortFieldKind       = "kind"
)

// FileService exposes file operations to the frontend.
type FileService struct {
	app      *application.App
	db       *index.DB
	registry *provider.Registry
	logger   *slog.Logger
}

func NewFileService(app *application.App, db *index.DB, registry *provider.Registry, logger *slog.Logger) *FileService {
	return &FileService{app: app, db: db, registry: registry, logger: logger}
}

// SetApp injects the Wails app reference after application.New returns.
func (s *FileService) SetApp(app *application.App) { s.app = app }

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

// MoveWithin moves files to another directory within the same location.
func (s *FileService) MoveWithin(locationID string, fileIDs []string, destDirID string) error {
	p, err := s.provider(locationID)
	if err != nil {
		return err
	}
	oldParents := map[string]string{}
	for _, id := range fileIDs {
		if f, err := s.db.FileByID(context.Background(), locationID, id); err == nil {
			oldParents[id] = f.ParentID
		}
	}
	if err := p.MoveWithin(context.Background(), locationID, fileIDs, destDirID); err != nil {
		return fmt.Errorf("move: %w", err)
	}
	for _, id := range fileIDs {
		newPath := filepath.Join(destDirID, filepath.Base(id))
		_ = s.db.DeleteFile(context.Background(), locationID, id)
		if f, err := p.Stat(context.Background(), locationID, newPath); err == nil {
			_ = s.db.UpsertFile(context.Background(), f)
		}
		if old, ok := oldParents[id]; ok {
			s.emitFsChanged(locationID, old)
		}
	}
	s.emitFsChanged(locationID, destDirID)
	return nil
}

// DeleteFiles moves files to trash and removes them from the index.
func (s *FileService) DeleteFiles(locationID string, fileIDs []string) error {
	p, err := s.provider(locationID)
	if err != nil {
		return err
	}
	parents := map[string]bool{}
	for _, id := range fileIDs {
		if f, err := s.db.FileByID(context.Background(), locationID, id); err == nil {
			parents[f.ParentID] = true
		}
	}
	if err := p.Delete(context.Background(), locationID, fileIDs); err != nil {
		return fmt.Errorf("delete: %w", err)
	}
	for _, id := range fileIDs {
		_ = s.db.DeleteFile(context.Background(), locationID, id)
	}
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
	if s.app == nil {
		return
	}
	s.app.Event.Emit(ev.FsChanged, map[string]any{
		"locationId": locationID,
		"dirId":      nilIfEmpty(dirID),
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
