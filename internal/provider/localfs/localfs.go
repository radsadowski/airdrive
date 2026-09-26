// Package localfs implements StorageProvider for macOS local folders.
package localfs

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/radosny/airdrive/internal/provider"
)

// Provider implements provider.StorageProvider for a single root directory.
type Provider struct {
	locationID string
	root       string // absolute path
}

// New creates a localfs provider for root. root must be absolute.
func New(locationID, root string) *Provider {
	return &Provider{locationID: locationID, root: filepath.Clean(root)}
}

func (p *Provider) ID() string          { return p.locationID }
func (p *Provider) Kind() provider.Kind { return provider.KindLocalFS }

// ---- helpers ------------------------------------------------------------- //

// abs resolves a fileID (which IS the absolute path for localfs) and checks
// it is still sandboxed under root.
func (p *Provider) abs(fileID string) (string, error) {
	if fileID == "" {
		return p.root, nil
	}
	// fileID for localfs = absolute path
	clean := filepath.Clean(fileID)
	if !strings.HasPrefix(clean, p.root) {
		return "", fmt.Errorf("localfs: path %q escapes root", fileID)
	}
	return clean, nil
}

// StatPath is the exported version of infoFromStat, used by the watcher.
func (p *Provider) StatPath(path string, fi os.FileInfo) provider.FileInfo {
	return p.infoFromStat(path, fi)
}

func (p *Provider) infoFromStat(path string, fi os.FileInfo) provider.FileInfo {
	// parentID is the absolute path of the parent dir, EXCEPT for direct
	// children of root whose parentID must be "" so ListDir(dirID="") finds them.
	parentID := ""
	if path != p.root {
		parent := filepath.Dir(path)
		if parent != p.root {
			parentID = parent
		}
	}
	kind := provider.FileKindFile
	if fi.IsDir() {
		kind = provider.FileKindDir
	}
	etag := fmt.Sprintf("%d:%d", fi.ModTime().Unix(), fi.Size())
	m := provider.MimeFor(fi.Name(), fi.IsDir())
	return provider.FileInfo{
		ID:          path,
		LocationID:  p.locationID,
		ParentID:    parentID,
		Name:        fi.Name(),
		Kind:        kind,
		Size:        fi.Size(),
		MIME:        m,
		Etag:        etag,
		PreviewKind: provider.PreviewKind(m),
		ModifiedAt:  fi.ModTime().UTC(),
	}
}

// ---- StorageProvider ----------------------------------------------------- //

func (p *Provider) List(ctx context.Context, locationID, dirID string, sort provider.SortSpec, cursor string, limit int) (provider.ListPage, error) {
	dir, err := p.abs(dirID)
	if err != nil {
		return provider.ListPage{}, err
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return provider.ListPage{}, fmt.Errorf("localfs: readdir %q: %w", dir, err)
	}
	if limit <= 0 || limit > 2000 {
		limit = 400
	}
	var offset int
	if _, err := fmt.Sscanf(cursor, "%d", &offset); err != nil {
		offset = 0
	}

	var items []provider.FileInfo
	for i, e := range entries {
		if i < offset {
			continue
		}
		if len(items) >= limit+1 {
			break
		}
		fi, err := e.Info()
		if err != nil {
			continue
		}
		items = append(items, p.infoFromStat(filepath.Join(dir, e.Name()), fi))
	}
	var nextCursor string
	if len(items) > limit {
		items = items[:limit]
		nextCursor = fmt.Sprintf("%d", offset+limit)
	}
	return provider.ListPage{Items: items, Cursor: nextCursor}, nil
}

func (p *Provider) Stat(ctx context.Context, locationID, fileID string) (provider.FileInfo, error) {
	path, err := p.abs(fileID)
	if err != nil {
		return provider.FileInfo{}, err
	}
	fi, err := os.Stat(path)
	if err != nil {
		return provider.FileInfo{}, fmt.Errorf("localfs: stat: %w", err)
	}
	return p.infoFromStat(path, fi), nil
}

func (p *Provider) Mkdir(ctx context.Context, locationID, parentID, name string) (provider.FileInfo, error) {
	parent, err := p.abs(parentID)
	if err != nil {
		return provider.FileInfo{}, err
	}
	newPath := filepath.Join(parent, name)
	if err := os.Mkdir(newPath, 0o755); err != nil {
		return provider.FileInfo{}, fmt.Errorf("localfs: mkdir: %w", err)
	}
	fi, _ := os.Stat(newPath)
	return p.infoFromStat(newPath, fi), nil
}

func (p *Provider) Rename(ctx context.Context, locationID, fileID, newName string) (provider.FileInfo, error) {
	path, err := p.abs(fileID)
	if err != nil {
		return provider.FileInfo{}, err
	}
	newPath := filepath.Join(filepath.Dir(path), newName)
	if err := os.Rename(path, newPath); err != nil {
		return provider.FileInfo{}, fmt.Errorf("localfs: rename: %w", err)
	}
	fi, _ := os.Stat(newPath)
	return p.infoFromStat(newPath, fi), nil
}

func (p *Provider) MoveWithin(ctx context.Context, locationID string, fileIDs []string, destDirID string) error {
	dest, err := p.abs(destDirID)
	if err != nil {
		return err
	}
	for _, id := range fileIDs {
		src, err := p.abs(id)
		if err != nil {
			return err
		}
		if err := os.Rename(src, filepath.Join(dest, filepath.Base(src))); err != nil {
			return fmt.Errorf("localfs: move %q: %w", src, err)
		}
	}
	return nil
}

// Delete moves files to ~/.Trash using macOS-compatible naming.
func (p *Provider) Delete(ctx context.Context, locationID string, fileIDs []string) error {
	trashDir := filepath.Join(os.Getenv("HOME"), ".Trash")
	if err := os.MkdirAll(trashDir, 0o700); err != nil { //nolint:gosec // trashDir is the fixed $HOME/.Trash, not user input
		return err
	}
	for _, id := range fileIDs {
		src, err := p.abs(id)
		if err != nil {
			return err
		}
		base := filepath.Base(src)
		dest := filepath.Join(trashDir, base)
		// Deduplicate: append UUID suffix if a name collision exists in Trash.
		if _, err := os.Stat(dest); err == nil { //nolint:gosec // dest is ~/.Trash plus the base name of an abs()-sandboxed path
			ext := filepath.Ext(base)
			stem := strings.TrimSuffix(base, ext)
			dest = filepath.Join(trashDir, stem+" "+uuid.NewString()[:8]+ext)
		}
		if err := os.Rename(src, dest); err != nil { //nolint:gosec // src is abs()-sandboxed; dest is ~/.Trash plus its base name
			return fmt.Errorf("localfs: trash %q: %w", src, err)
		}
	}
	return nil
}

func (p *Provider) Download(ctx context.Context, locationID, fileID string) (io.ReadCloser, provider.FileInfo, error) {
	path, err := p.abs(fileID)
	if err != nil {
		return nil, provider.FileInfo{}, err
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, provider.FileInfo{}, fmt.Errorf("localfs: open: %w", err)
	}
	fi, _ := os.Stat(path)
	return f, p.infoFromStat(path, fi), nil
}

func (p *Provider) Upload(ctx context.Context, locationID, parentID, name string, r io.Reader, size int64) (provider.FileInfo, error) {
	parent, err := p.abs(parentID)
	if err != nil {
		return provider.FileInfo{}, err
	}
	dest := filepath.Join(parent, name)
	f, err := os.Create(dest)
	if err != nil {
		return provider.FileInfo{}, fmt.Errorf("localfs: create: %w", err)
	}
	defer f.Close()
	if _, err := io.Copy(f, r); err != nil {
		os.Remove(dest)
		return provider.FileInfo{}, fmt.Errorf("localfs: write: %w", err)
	}
	fi, _ := os.Stat(dest)
	return p.infoFromStat(dest, fi), nil
}

// SyncChanges for localfs is handled by the fsnotify watcher; this method
// does a full walk (cursor="" always) and emits all current files.
func (p *Provider) SyncChanges(ctx context.Context, locationID, cursor string, emit func([]provider.ChangeEvent)) (string, error) {
	var batch []provider.ChangeEvent
	err := filepath.WalkDir(p.root, func(path string, d os.DirEntry, err error) error {
		if err != nil || path == p.root {
			return err
		}
		fi, err := d.Info()
		if err != nil {
			return nil
		}
		info := p.infoFromStat(path, fi)
		batch = append(batch, provider.ChangeEvent{
			FileID: path,
			Kind:   provider.ChangeCreated,
			File:   &info,
		})
		if len(batch) >= 500 {
			emit(batch)
			batch = batch[:0]
		}
		return nil
	})
	if len(batch) > 0 {
		emit(batch)
	}
	return time.Now().Format(time.RFC3339), err
}
