# AirDrive

A macOS desktop app that unifies **local folders**, **Google Drive**, and any
**S3-compatible storage** (primary target: **Cloudflare R2**) into one
file-manager UI — a Spacedrive-style manager for browsing, organizing, and
eventually migrating files across all three.

The initial goal (v1) is a fast, unified file manager with migration-grade
transfers — built to move ~1M files / TBs off Google Drive onto R2 with
resumable, verified, idempotent transfers. v2 turns R2 into a sync hub with
two-way folder-pair sync across machines.

> Status: early development. Local folders are fully wired end-to-end
> (browse/index/watch); Google Drive is wired end-to-end too — sign-in with
> a folder picker that scopes the location to one Drive folder (or all of
> My Drive; starred folders — including Drive-for-desktop "Computers"
> backups — are pickable and sync by subtree re-listing), browse/index/search
> via SQLite, full CRUD, and incremental sync through Drive's changes feed.
> S3-compatible buckets (R2/MinIO/B2/AWS via minio-go) are wired end-to-end:
> test-connection + bucket locations with browse/search/CRUD, deletions to an
> in-bucket `.airdrive-trash/` prefix, and reconcile refreshes that detect
> remote deletions. Image thumbnails render in the browser for local and
> Drive locations (Drive: server-side thumbnailLink; local: macOS `sips`),
> served from an etag-keyed cache by the Go asset middleware. The transfer
> engine is not implemented yet. See `.planning/` / the project plan for
> phase-by-phase status.

## How it works

AirDrive is a **Wails v3** app: a Go backend paired with a React/TypeScript
webview frontend, packaged as a single native macOS binary.

- **Backend (Go)** — owns all real work: a local SQLite index (with FTS5
  full-text search) is the single source of truth the UI reads from; a
  filesystem watcher keeps it live; a `StorageProvider` abstraction lets
  local folders, Google Drive, and S3-compatible buckets share one interface
  for browsing/CRUD/transfers. Only one package (`internal/bridge`) is
  allowed to import Wails — everything else is plain, testable Go.
- **Frontend (React + TypeScript + Vite)** — never talks to Wails directly.
  It calls a typed `ApiClient` contract that has two implementations: a
  `WailsClient` (talks to the real Go backend via generated bindings) and a
  `MockClient` (a fully stateful in-memory fake, so the whole UI can be
  built and iterated on in a browser with `vite dev`, no Go required).
- **Storage** — SQLite via `modernc.org/sqlite` (pure Go, no CGo) with
  `pressly/goose` migrations; the index is disposable (rebuildable via
  re-crawl) while transfer/journal state is precious and durable.
- **UI** — Tailwind CSS (v4) + Zustand for state + TanStack Virtual for
  large virtualized file lists.

### Tech stack

| Concern | Choice |
|---|---|
| App shell | Wails v3 (Go) |
| Frontend | React + TypeScript + Vite |
| UI styling | Tailwind CSS v4 |
| UI state | Zustand |
| Virtualized lists | TanStack Virtual |
| Local index / search | SQLite (`modernc.org/sqlite`) + FTS5 |
| Migrations | `pressly/goose` |
| Local filesystem watching | `fsnotify` (debounced) |
| Google Drive auth | `golang.org/x/oauth2` (PKCE) + `google.golang.org/api/drive/v3` |
| S3-compatible storage | `github.com/minio/minio-go/v7` (user-sanctioned exception to stdlib-only) |
| Credentials storage | macOS Keychain (`zalando/go-keyring`) |
| Go tests | stdlib `testing` (parallel, table-driven) + `stretchr/testify` |

## Requirements

- macOS (this app targets macOS only; no Windows/Linux builds)
- [Go](https://go.dev/) 1.26+
- [Node.js](https://nodejs.org/) 20+ (Vite/React frontend tooling)
- [Wails v3 CLI](https://v3alpha.wails.io/), pinned to `v3.0.0-beta.26` to
  match this repo:
  ```sh
  go install github.com/wailsapp/wails/v3/cmd/wails3@v3.0.0-beta.26
  ```

## Running in dev mode

```sh
wails3 dev -config ./build/config.yml
```

This will:
1. Install frontend dependencies (`npm --prefix frontend install`, once).
2. Start the Vite dev server in the background on `127.0.0.1:9245`.
3. Build and launch the Go binary, which loads the webview pointed at the
   Vite dev server for hot reload on both the Go and React sides.

Editing Go files triggers a rebuild + relaunch; editing frontend files
hot-reloads via Vite.

You can also run the frontend alone in a regular browser (no Go, no Wails)
against the mock backend:

```sh
cd frontend
npm install
npm run dev
```

## Make targets

```sh
make help     # list everything
make dev      # wails3 dev -config ./build/config.yml (hot reload, both sides)
make lint     # golangci-lint run ./... (config: .golangci.yml)
make lint-fix # auto-fix what the linters can
make fmt      # apply the golangci-lint formatters
make mocks    # regenerate gomock mocks after interface changes
make test     # go test -race ./...
```

## Building

```sh
wails3 build
```

Produces a native macOS binary in `bin/`.

## Testing

The Go test suite is table-driven, runs in parallel, and asserts via
`stretchr/testify` `require` plus `go.uber.org/mock` generated mocks (see
CLAUDE.md rule 7; regenerate after interface changes with `make mocks`). The
gdrive provider is tested against a fake Drive API served by `httptest`, so
no network or real credentials are needed:

```sh
go test ./...                # whole suite
go test -race ./internal/... # with the race detector
```

Live behaviour (OAuth, real Drive traffic) is verified manually with
`wails3 dev` and a real Google account.
