// Package watcher wraps fsnotify with debouncing and index sync for localfs locations.
package watcher

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"

	"github.com/radosny/airdrive/internal/index"
	"github.com/radosny/airdrive/internal/provider/localfs"
)

const debounce = 300 * time.Millisecond

// OnChange is called after the index is updated with the affected locationID
// and dirID (empty string = root), so the bridge can emit fs:changed.
type OnChange func(locationID, dirID string)

// Manager watches one or more local directories and keeps the SQLite index
// in sync with the filesystem.
type Manager struct {
	watcher  *fsnotify.Watcher
	db       *index.DB
	logger   *slog.Logger
	onChange OnChange

	mu        sync.Mutex
	locations map[string]locEntry // locationID → entry
	// pending debounce timers: abs path → timer
	pending map[string]*time.Timer
}

type locEntry struct {
	locationID string
	root       string
	provider   *localfs.Provider
}

func New(db *index.DB, logger *slog.Logger, onChange OnChange) (*Manager, error) {
	w, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}
	m := &Manager{
		watcher:   w,
		db:        db,
		logger:    logger,
		onChange:  onChange,
		locations: make(map[string]locEntry),
		pending:   make(map[string]*time.Timer),
	}
	go m.loop()
	return m, nil
}

// Watch adds recursive watching for a localfs location.
func (m *Manager) Watch(locationID, root string) error {
	p := localfs.New(locationID, root)
	m.mu.Lock()
	m.locations[locationID] = locEntry{locationID: locationID, root: root, provider: p}
	m.mu.Unlock()
	return m.addDirRecursive(root)
}

// Unwatch stops watching a location.
func (m *Manager) Unwatch(locationID string) {
	m.mu.Lock()
	entry, ok := m.locations[locationID]
	delete(m.locations, locationID)
	m.mu.Unlock()
	if ok {
		// Remove all watches under this root.
		for _, name := range m.watcher.WatchList() {
			if strings.HasPrefix(name, entry.root) {
				_ = m.watcher.Remove(name)
			}
		}
	}
}

// Close stops all watchers.
func (m *Manager) Close() error { return m.watcher.Close() }

func (m *Manager) addDirRecursive(root string) error {
	return filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil || !d.IsDir() {
			return nil
		}
		if err := m.watcher.Add(path); err != nil {
			m.logger.Warn("watcher add", "path", path, "err", err)
		}
		return nil
	})
}

func (m *Manager) loop() {
	for {
		select {
		case ev, ok := <-m.watcher.Events:
			if !ok {
				return
			}
			m.schedule(ev)
		case err, ok := <-m.watcher.Errors:
			if !ok {
				return
			}
			m.logger.Warn("watcher error", "err", err)
		}
	}
}

func (m *Manager) schedule(ev fsnotify.Event) {
	m.mu.Lock()
	defer m.mu.Unlock()
	path := ev.Name
	if t, ok := m.pending[path]; ok {
		t.Reset(debounce)
		return
	}
	m.pending[path] = time.AfterFunc(debounce, func() {
		m.mu.Lock()
		delete(m.pending, path)
		m.mu.Unlock()
		m.handle(path, ev.Op)
	})
}

func (m *Manager) handle(path string, op fsnotify.Op) {
	m.mu.Lock()
	var entry locEntry
	for _, e := range m.locations {
		if strings.HasPrefix(path, e.root) {
			entry = e
			break
		}
	}
	m.mu.Unlock()
	if entry.locationID == "" {
		return
	}

	ctx := context.Background()

	switch {
	case op.Has(fsnotify.Remove) || op.Has(fsnotify.Rename):
		if err := m.db.DeleteFile(ctx, entry.locationID, path); err != nil {
			m.logger.Warn("watcher: delete file from index", "path", path, "err", err)
		}
		if err := m.db.DeleteChildrenOf(ctx, entry.locationID, path); err != nil {
			m.logger.Warn("watcher: delete children from index", "path", path, "err", err)
		}
		// Remove watch — may fail if the path was already removed; expected race.
		if err := m.watcher.Remove(path); err != nil {
			m.logger.Debug("watcher: remove watch (expected if path already gone)", "path", path, "err", err)
		}

	case op.Has(fsnotify.Create):
		fi, err := os.Stat(path)
		if err != nil {
			// File removed between event and debounce firing — expected race.
			m.logger.Debug("watcher: stat on create (file may have been removed)", "path", path, "err", err)
			return
		}
		info := entry.provider.StatPath(path, fi)
		if err := m.db.UpsertFile(ctx, info); err != nil {
			m.logger.Warn("watcher: upsert file into index", "path", path, "err", err)
		}
		if fi.IsDir() {
			if err := m.addDirRecursive(path); err != nil {
				m.logger.Warn("watcher: watch new dir", "path", path, "err", err)
			}
		}

	case op.Has(fsnotify.Write):
		fi, err := os.Stat(path)
		if err != nil {
			m.logger.Debug("watcher: stat on write (file may have been removed)", "path", path, "err", err)
			return
		}
		if !fi.IsDir() {
			info := entry.provider.StatPath(path, fi)
			if err := m.db.UpsertFile(ctx, info); err != nil {
				m.logger.Warn("watcher: upsert file into index", "path", path, "err", err)
			}
		}
	}

	dirID := filepath.Dir(path)
	if dirID == entry.root {
		dirID = ""
	}
	m.onChange(entry.locationID, dirID)
}
