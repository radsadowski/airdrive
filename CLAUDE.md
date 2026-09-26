# AirDrive — Coding Rules

These rules are enforced on every PR. They encode decisions made during
code review so the same mistakes don't recur.

---

## Project structure & how things work

### Big picture

AirDrive is a macOS desktop app built with **Wails v3** (Go backend + React/TS
webview). The backend never serves HTML — it serves a prebuilt Vite bundle
(`frontend/dist/`) embedded via `go:embed`. In dev mode (`wails3 dev`) the
webview proxies to the Vite dev server on `127.0.0.1:9245` instead.

```
wails3 dev -config ./build/config.yml   # dev (hot reload on both sides)
wails3 build                            # production binary → bin/airdrive
```

### Go layout

```
main.go                         entry point: open DB, create watcher,
                                register services, open window, app.Run()

internal/
  events/events.go              all Wails event name constants (rule 4)

  entities/
    entities.go                 cross-layer types: File, Page, Location,
                                ListDirOpts — exposed to the Wails bridge
    converters.go               ToFile(), ToPage() — domain model → entity

  bridge/                       THE ONLY PACKAGE that imports Wails.
    appservice.go               AppService: Version(), Platform(), PickFolder()
    location_service.go         LocationService: list/add/remove locations + crawl
    file_service.go             FileService: listDir/stat/mkdir/rename/move/delete/
                                download/search

  provider/
    provider.go                 StorageProvider interface + shared types
                                (FileInfo, SortSpec, ListPage, ChangeEvent)
    registry.go                 Registry: locationID → StorageProvider map
    localfs/localfs.go          local filesystem provider (Phase 2)
    gdrive/                     Google Drive provider: files.go CRUD,
                                sync.go changes feed + subtree walk,
                                browse.go pre-location folder picker,
                                thumbnail.go, transport.go retry
    s3compat/                   S3-compatible provider (minio-go): files.go
                                CRUD + trash prefix, sync.go full walk

  index/
    db.go                       SQLite via modernc.org/sqlite: open, migrate,
                                UpsertFile, ListDir, Search (FTS5), DeleteFile
    migrations/0001_initial.sql goose schema: locations, files, files_fts,
                                transfers, sync_pairs/state (v2-ready)

  watcher/
    watcher.go                  fsnotify wrapper: watches all subdirs of each
                                localfs location, debounces events (300 ms),
                                upserts/deletes from index, calls OnChange
```

### Request flow: browsing a folder

```
React FileBrowser
  → FileSvc.ListDir (Wails binding)
    → FileService.ListDir (Go)
      → index.DB.ListDir (SQLite query, ~10 ms, no network)
        → returns entities.Page to bridge
          → serialised to TS Page by generated bindings
            → toPage() in WailsClient adapts to contract FileInfo[]
              → React re-renders
```

The UI **never reads from the provider directly** while browsing — it always
goes through SQLite. The provider is only called for mutations (mkdir, rename,
delete, download, upload) and during the initial crawl.

### Request flow: filesystem change (watcher → UI)

```
macOS kernel (FSEvents)
  → fsnotify.Watcher (Go channel)
    → watcher.loop() goroutine
      → schedule(path): debounce timer reset / set (300 ms)
        → handle(path, op) after silence:
            Create  → provider.StatPath + index.UpsertFile + watcher.Add (new dirs)
            Remove  → index.DeleteFile + index.DeleteChildrenOf
            Write   → provider.StatPath + index.UpsertFile
          → OnChange(locationID, dirID)
            → app.Event.Emit(ev.FsChanged, ...)
              → Events.On('fs:changed') in events.ts
                → applyFsChange() in app store
                  → refresh() → ListDir from SQLite → React re-renders (~326 ms total)
```

### Frontend layout

```
frontend/src/
  App.tsx                       mounts AppShell, wires events, applies theme
  app.css                       Tailwind v4 @theme tokens (dark + light overrides)

  views/AppShell.tsx            root layout: TitleBar | Sidebar | main | overlays
  components/                   UI: sidebar, browser, transfers, search, preview,
                                wizards, devpanel, toasts, …
  stores/
    app.ts                      locations, browse state, selection, breadcrumbs
    transfers.ts                batch/job live state (event-driven)
    ui.ts                       toasts, overlays, settings, wizard state

  lib/
    api/
      contract.ts               THE spec: ApiClient + EventBus interfaces +
                                all shared TS types (FileInfo, Location, …)
      index.ts                  client factory: detects window.wails → WailsClient,
                                else → MockClient
      mock/                     stateful in-memory app (dev/browser mode)
        client.ts               MockClient + dev controls (failNext, offline, …)
        state.ts                in-memory file tree + locations
        runtime.ts              transfer engine + crawl simulation
        data.ts                 seeded fake-data generators
      wails/client.ts           WailsClient: wraps generated bindings,
                                adapts entities.* → contract types
    events.ts                   wires app.Event → store actions
    actions.ts                  file op helpers (delete, rename, paste, …)
    backend.ts                  useBackendVersion() — Go→UI loop proof

  bindings/                     AUTO-GENERATED by `wails3 generate bindings -ts`
                                do not edit manually
```

### Key invariants

| Invariant | Where enforced |
|---|---|
| Only `bridge/` imports Wails | import lint / code review |
| Entity types live in `entities/` | rule 1 + CLAUDE.md |
| Converters live in `converters.go` | rule 2 + CLAUDE.md |
| Nil on error, never empty struct | rule 3 + CLAUDE.md |
| Event names are constants | rule 4 + CLAUDE.md |
| UI reads files from SQLite only | architecture convention |
| Frontend auto-detects `window.wails` | `api/index.ts:clientMode()` |
| Mock is the default in a plain browser | `api/index.ts:clientMode()` |

---

## 1. Cross-layer types → `internal/entities`

Every struct that is:
- exposed to the Wails bridge (mirrors a TypeScript type), **or**
- shared between two or more internal packages

**must live in `internal/entities/`**.

Bridge service files (`internal/bridge/*.go`) must **not** define their own
struct types — import from `internal/entities` instead. Type names carry no
`DTO` suffix; the package name already provides that context.

```go
// WRONG — type defined in the service file
type File struct { ... }

// CORRECT — imported from entities
import "github.com/radosny/airdrive/internal/entities"
func (s *FileService) Stat(...) (*entities.File, error) { ... }
```

## 2. Converters → `internal/entities/converters.go`

All functions that convert between a Go domain model (e.g. `provider.FileInfo`)
and an entity type (e.g. `entities.File`) live in `internal/entities/converters.go`.

No converter logic may live inside a bridge service method.

```go
// WRONG — conversion inline in the service
return &entities.File{ID: f.ID, Name: f.Name, ...}

// CORRECT — call the converter
result := entities.ToFile(f)
return &result, nil
```

## 3. Return `nil` on error, never empty structs

Functions returning a struct + error must return a **pointer** so callers
get `nil` (not a zero-value struct) on the error path.

```go
// WRONG — zero-value struct on error is misleading
func (s *LocationService) AddLocalLocation(...) (entities.Location, error) {
    return entities.Location{}, err
}

// CORRECT — nil pointer is unambiguous on error
func (s *LocationService) AddLocalLocation(...) (*entities.Location, error) {
    return nil, err
}
```

## 5. Never discard errors silently

If a function returns an error it **must** be handled — either returned to the
caller or logged. The `_ =` idiom on an error return is forbidden outside tests.

- **State-changing failures** (index writes, watcher ops) → `logger.Warn`
- **Expected races** (stat after debounce, Remove on already-gone path) → `logger.Debug`

```go
// WRONG — silent index drift, impossible to diagnose
_ = m.db.DeleteFile(ctx, path)

// CORRECT — log at appropriate level
if err := m.db.DeleteFile(ctx, path); err != nil {
    m.logger.Warn("watcher: delete file from index", "path", path, "err", err)
}
```

## 4. Event names → `internal/events` constants

Every Wails event name **must** be a named constant in `internal/events/events.go`.
String literals like `"fs:changed"` are forbidden in `.Emit()` and `.RegisterEvent()` calls.

When adding a new event:
1. Add the constant to `internal/events/events.go`.
2. Add `application.RegisterEvent` in `main.go` `init()`.
3. Add the matching key to `AppEventMap` in `frontend/src/lib/api/contract.ts`.

```go
// WRONG
app.Event.Emit("fs:changed", payload)

// CORRECT
import ev "github.com/radosny/airdrive/internal/events"
app.Event.Emit(ev.FsChanged, payload)
```

## 6. Keep `README.md` in sync

Whenever a change affects what's in the README — new/changed requirements,
dev/build commands, tech stack, or overall project description/status —
**update `README.md` in the same change**. Don't let it drift out of date.

## 7. Tests: parallel by default, assert with testify `require`

Every Go test — including each table-driven subtest — must call `t.Parallel()`
so the suite runs concurrently, and must assert via `r := require.New(t)`
from `github.com/stretchr/testify` (fail-fast, no manual `if got != want`
boilerplate). `stretchr/testify` and `go.uber.org/mock` (gomock, v0.6.0) are
the two sanctioned test-only dependencies (user decisions, 2026-10-10 and
2026-10-25). Interface fakes are forbidden where a generated mock exists:
run `make mocks` after changing `internal/provider/provider.go`, `gdrive/browse.go`
or `s3compat/s3compat.go` (each carries a `//go:generate` directive); script
mocks with `EXPECT(...)` instead of hand-written fake types. HTTP-level fakes
(fake Drive / fake S3 servers over httptest) stay hand-written — they exercise
real clients, not interfaces.

```go
// WRONG — serial test, manual assertion boilerplate
func TestMimeFor(t *testing.T) {
	if got := MimeFor("a.png", false); got != "image/png" {
		t.Errorf("got %q", got)
	}
}

// CORRECT — parallel + require
func TestWhenTheExtensionIsLowercaseThenTheMimeIsResolved(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	r.Equal("image/png", MimeFor("a.png", false))
}
```

Additionally, every test name must read as a **when X then Y** sentence: the
`name` field of each table-driven case, and the function name (CamelCase) for
non-table tests. A `--- FAIL:` line should explain the expected behaviour
without opening the test body.

```go
// WRONG — labels, not behaviours
{"uppercase ext", "PHOTO.JPG", false, "image/jpeg"}
func TestMkdir(t *testing.T) { ... }

// CORRECT — when X then Y
{"when the extension is uppercase then it still matches", "PHOTO.JPG", false, "image/jpeg"}
func TestWhenMkdirTargetsRootThenTheResolvedRootIDIsUsed(t *testing.T) { ... }
```

## 8. Comments: concise declaration docs only

Declaration comments (package, type, function) are the only comments allowed
and must be as concise as possible — one line where one line fits. Comments
inside function bodies are forbidden: if code needs a mid-function
explanation, extract a helper with a doc comment or rename until the code
reads plainly. The "why" belongs in the doc comment (or a `require` failure
message in tests), never in the body.

```go
// WRONG — in-body narration
func crawl(id string) {
	// Resume from the stored cursor, falling back to a full walk.
	cursor, err := db.GetSyncCursor(ctx, id)
	// Persist only on success so interrupted walks re-walk.
	db.SetSyncCursor(ctx, id, newCursor)
}

// CORRECT — the why lives in the doc comment
// crawl syncs a provider into the index. The cursor persists only on
// success, so an interrupted full walk re-walks from scratch.
func crawl(id string) {
	cursor, err := db.GetSyncCursor(ctx, id)
	db.SetSyncCursor(ctx, id, newCursor)
}
```
