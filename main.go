package main

import (
	"embed"
	"log"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/wailsapp/wails/v3/pkg/application"

	"github.com/radosny/airdrive/internal/bridge"
	ev "github.com/radosny/airdrive/internal/events"
	"github.com/radosny/airdrive/internal/index"
	"github.com/radosny/airdrive/internal/provider"
	"github.com/radosny/airdrive/internal/watcher"
)

//go:embed all:frontend/dist
var assets embed.FS

func init() {
	// Register all event types so wails3 generate bindings produces typed TS APIs.
	application.RegisterEvent[map[string]any](ev.AppReady)
	application.RegisterEvent[map[string]any](ev.IndexProgress)
	application.RegisterEvent[map[string]any](ev.FsChanged)
	application.RegisterEvent[map[string]any](ev.LocationStatus)
	application.RegisterEvent[map[string]any](ev.TransferUpdated)
	application.RegisterEvent[map[string]any](ev.TransferBatchDone)
	application.RegisterEvent[map[string]any](ev.ToastShow)
}

func main() {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))

	// Open the SQLite index (or create it on first run).
	dataDir, err := dataDirectory()
	if err != nil {
		log.Fatal("data directory:", err)
	}
	db, err := index.Open(filepath.Join(dataDir, "index.db"), logger)
	if err != nil {
		log.Fatal("index open:", err)
	}
	defer db.Close()

	registry := provider.NewRegistry()

	thumbs, err := bridge.NewThumbnailService(db, registry, logger, filepath.Join(dataDir, "thumbs"))
	if err != nil {
		log.Fatal("thumbnails:", err)
	}
	go thumbs.Sweep()

	// Watcher is created before the app so it can be passed into services.
	// The onChange callback emits fs:changed via the app — set after New.
	var appRef *application.App
	w, err := watcher.New(db, logger, func(locationID, dirID string) {
		if appRef != nil {
			var dirVal any
			if dirID != "" {
				dirVal = dirID
			}
			appRef.Event.Emit(ev.FsChanged, map[string]any{
				"locationId": locationID,
				"dirId":      dirVal,
			})
		}
	})
	if err != nil {
		log.Fatal("watcher:", err)
	}
	defer w.Close()

	// Services hold a pointer that we fill in after application.New.
	appSvc := bridge.NewAppService(nil)
	locSvc := bridge.NewLocationService(nil, db, registry, w, logger)
	fileSvc := bridge.NewFileService(nil, db, registry, logger)

	app := application.New(application.Options{
		Name:        "AirDrive",
		Description: "One cockpit for every storage you own",
		Services: []application.Service{
			application.NewService(appSvc),
			application.NewService(locSvc),
			application.NewService(fileSvc),
		},
		Assets: application.AssetOptions{
			Handler:    application.AssetFileServerFS(assets),
			Middleware: thumbs.Middleware,
		},
		Mac: application.MacOptions{
			ApplicationShouldTerminateAfterLastWindowClosed: true,
		},
	})

	// Inject the real app reference now that it exists.
	appRef = app
	appSvc.SetApp(app)
	locSvc.SetApp(app)
	fileSvc.SetApp(app)

	app.Window.NewWithOptions(application.WebviewWindowOptions{
		Title:  "AirDrive",
		Width:  1360,
		Height: 860,
		Mac: application.MacWindow{
			InvisibleTitleBarHeight: 38,
			Backdrop:                application.MacBackdropTranslucent,
			TitleBar:                application.MacTitleBarHiddenInset,
		},
		BackgroundColour: application.NewRGB(11, 12, 15),
		URL:              "/",
	})

	// Restore locations from the previous session (async so app starts fast).
	go func() {
		if err := locSvc.Restore(); err != nil {
			logger.Error("restore locations", "err", err)
		}
	}()

	app.Event.Emit(ev.AppReady, map[string]any{
		"version":  "0.1.0",
		"mode":     "wails",
		"platform": "darwin",
	})

	if err := app.Run(); err != nil {
		log.Fatal(err)
	}
}

// dataDirectory returns ~/Library/Application Support/AirDrive, creating it.
func dataDirectory() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(home, "Library", "Application Support", "AirDrive")
	return dir, os.MkdirAll(dir, 0o700)
}
