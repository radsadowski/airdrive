package bridge

import (
	"io"
	"log/slog"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/radosny/airdrive/internal/entities"
)

func TestWhenNoDownloadFolderIsStoredThenTheHomeDownloadsFolderIsUsed(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	svc, err := NewSettingsService(t.TempDir(), slog.New(slog.NewTextHandler(io.Discard, nil)))
	r.NoError(err)

	cfg, err := svc.Get()
	r.NoError(err)
	r.True(strings.HasSuffix(cfg.DownloadDir, string(filepath.Separator)+"Downloads"), cfg.DownloadDir)
}

func TestWhenTheDownloadFolderIsChangedThenItPersists(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	dataDir := t.TempDir()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	svc, err := NewSettingsService(dataDir, logger)
	r.NoError(err)

	dir := filepath.Join(dataDir, "incoming")
	_, err = svc.Update(map[string]any{"downloadDir": dir})
	r.NoError(err)

	reopened, err := NewSettingsService(dataDir, logger)
	r.NoError(err)
	cfg, err := reopened.Get()
	r.NoError(err)
	r.Equal(dir, cfg.DownloadDir)
}

func TestWhenDebugLoggingIsEnabledThenItPersists(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	dataDir := t.TempDir()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	svc, err := NewSettingsService(dataDir, logger)
	r.NoError(err)

	cfg, err := svc.Get()
	r.NoError(err)
	r.False(cfg.DebugLog, "the stream is off by default")

	_, err = svc.Update(map[string]any{"debugLog": true})
	r.NoError(err)

	reopened, err := NewSettingsService(dataDir, logger)
	r.NoError(err)
	cfg, err = reopened.Get()
	r.NoError(err)
	r.True(cfg.DebugLog)
}

func TestWhenTheDebugSettingChangesThenTheHookSeesIt(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	svc, err := NewSettingsService(t.TempDir(), slog.New(slog.NewTextHandler(io.Discard, nil)))
	r.NoError(err)

	var seen []bool
	svc.SetOnChange(func(cfg entities.Settings) { seen = append(seen, cfg.DebugLog) })

	_, err = svc.Update(map[string]any{"debugLog": true})
	r.NoError(err)
	_, err = svc.Update(map[string]any{"debugLog": false})
	r.NoError(err)

	r.Equal([]bool{true, false}, seen)
}
