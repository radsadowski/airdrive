// SettingsService — persisted settings for the transfer engine and the
// settings panel. Atomic JSON writes to dataDir/settings.json.
package bridge

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"

	"github.com/radosny/airdrive/internal/entities"
)

// SettingsService reads and writes app settings.
type SettingsService struct {
	mu     sync.Mutex
	path   string
	logger *slog.Logger
}

// NewSettingsService creates the service; the settings file is created with
// defaults on first use.
func NewSettingsService(dataDir string, logger *slog.Logger) (*SettingsService, error) {
	if logger == nil {
		logger = slog.Default()
	}
	s := &SettingsService{
		path:   filepath.Join(dataDir, "settings.json"),
		logger: logger,
	}
	if err := os.MkdirAll(dataDir, 0o700); err != nil {
		return nil, fmt.Errorf("settings: mkdir: %w", err)
	}
	if _, err := os.Stat(s.path); os.IsNotExist(err) {
		if wErr := s.write(defaultSettings()); wErr != nil {
			return nil, fmt.Errorf("settings: defaults: %w", wErr)
		}
	}
	return s, nil
}

// Get returns the current settings.
func (s *SettingsService) Get() (entities.Settings, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.read()
}

// Update applies a patch and persists atomically.
func (s *SettingsService) Update(patch map[string]any) (entities.Settings, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	cur, err := s.read()
	if err != nil {
		return cur, err
	}
	if v, ok := patch["theme"].(string); ok {
		cur.Theme = v
	}
	if v, ok := patch["defaultView"].(string); ok {
		cur.DefaultView = v
	}
	if v, ok := patch["concurrentTransfers"].(float64); ok {
		cur.ConcurrentTransfers = int(v)
	}
	if v, ok := patch["bandwidthLimitMbps"].(float64); ok {
		cur.BandwidthLimitMbps = int(v)
	}
	if v, ok := patch["showHiddenFiles"].(bool); ok {
		cur.ShowHiddenFiles = v
	}
	if v, ok := patch["downloadDir"].(string); ok {
		cur.DownloadDir = v
	}
	if err := s.write(cur); err != nil {
		return cur, err
	}
	return cur, nil
}

func defaultSettings() entities.Settings {
	return entities.Settings{
		Theme:               "dark",
		DefaultView:         "list",
		ConcurrentTransfers: 3,
		BandwidthLimitMbps:  80,
		ShowHiddenFiles:     false,
	}
}

func (s *SettingsService) read() (entities.Settings, error) {
	b, err := os.ReadFile(s.path)
	if err != nil {
		return defaultSettings(), fmt.Errorf("settings: read: %w", err)
	}
	var out entities.Settings
	if err := json.Unmarshal(b, &out); err != nil {
		return defaultSettings(), fmt.Errorf("settings: parse: %w", err)
	}
	return out, nil
}

func (s *SettingsService) write(cfg entities.Settings) error {
	b, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("settings: marshal: %w", err)
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return fmt.Errorf("settings: write: %w", err)
	}
	if err := os.Rename(tmp, s.path); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("settings: rename: %w", err)
	}
	return nil
}
