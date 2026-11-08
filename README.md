# 🗂️ AirDrive

A macOS desktop app that unifies **local folders**, **Google Drive**, and
**S3-compatible storage** (R2, MinIO, B2, AWS) into one file-manager UI:
browse, search, organize, and transfer files across all three from a single
native app.

Built with Wails v3 (Go backend + React/TypeScript webview), with a local
SQLite index as the read path and migration-grade transfers that move files
between providers with MD5 verification.

[![CI](https://github.com/radsadowski/airdrive/actions/workflows/ci.yml/badge.svg)](https://github.com/radsadowski/airdrive/actions/workflows/ci.yml)
[![Release](https://github.com/radsadowski/airdrive/actions/workflows/release.yml/badge.svg)](https://github.com/radsadowski/airdrive/actions/workflows/release.yml)

## 📥 Installation

**Direct download** — grab the latest `.dmg` from the [Releases](https://github.com/radsadowski/airdrive/releases) page.

**Homebrew** — add the tap, then install the cask:

```sh
brew tap radsadowski/airdrive
brew install --cask airdrive
```

On first launch macOS may warn that the app is from an unidentified developer.
This is expected (the app is ad-hoc signed, not notarized). To proceed:

1. Drag `AirDrive.app` to `/Applications`.
2. Right-click the app in Finder and choose **Open**.
3. Click **Open** again in the Gatekeeper dialog.

Or, from the terminal:

```sh
xattr -d com.apple.quarantine /Applications/AirDrive.app
```

## 📑 Table of Contents

- [Features](#features)
  - [📁 File Management](#-file-management)
  - [☁️ Storage Providers](#️-storage-providers)
  - [🔄 Transfers](#-transfers)
  - [🔒 Security](#-security)
- [📋 Requirements](#-requirements)
- [🗺️ Roadmap](#️-roadmap)
- [🛠️ Development](#️-development)
  - [🏗️ Architecture](#️-architecture)
  - [🧰 Tech Stack](#-tech-stack)
  - [🚀 Running in Dev Mode](#-running-in-dev-mode)
  - [⌨️ Useful Commands](#️-useful-commands)
  - [🧪 Testing](#-testing)
  - [📦 Building](#-building)

## ✨ Features

### 📁 File Management

- **Browse & navigate**: folder trees across all connected locations,
  instant listing from the local SQLite index (no network on click)
- **🔍 Full-text search**: FTS5-powered search across every indexed file,
  filterable by size, date, and kind
- **✏️ File operations**: create, rename, move, and delete (to provider
  trash, never permanent)
- **🖼️ Image thumbnails**: in the browser grid for local folders (via
  macOS `sips`) and Google Drive (server-side thumbnails), served from
  an etag-keyed cache

### ☁️ Storage Providers

- **💾 Local folders**: indexed with fsnotify live watching
- **📧 Google Drive**: OAuth sign-in with a folder picker that scopes
  the location to one Drive folder or all of My Drive; starred folders
  (including Drive-for-desktop "Computers" backups) are pickable
- **🪣 S3-compatible buckets**: R2, MinIO, B2, or AWS via connection
  testing with bucket listing; deletions go to an in-bucket
  `.airdrive-trash/` prefix

### 🔄 Transfers

- **📋 Copy & move** between any two locations (e.g. Drive → R2, local
  → S3, S3 → local)
- **⚖️ Conflict policies**: smart (skip identical, keep both on
  difference), overwrite, or skip existing
- **✅ MD5 verification**: content hash verified end-to-end during
  transfer when the source exposes a checksum
- **📊 Progress & controls**: per-file byte progress with speed in
  the transfers panel; pause, resume, cancel, and retry-failed
- **🗑️ Move semantics**: source items are trashed only after all
  their files transfer successfully; a failed job keeps the source
  intact
- **⬆️ Uploads**: drag-and-drop files from Finder onto any location,
  or use the Upload button with the native file picker

### 🔒 Security

- **🔐 OAuth 2.0 with PKCE** for Google Drive: tokens live in the macOS
  Keychain, never in SQLite
- **🔑 S3 credentials**: access keys stored in the Keychain, never on
  disk in plain text
- **🛡️ Never-permanent deletes**: local folders go to `~/.Trash`,
  Drive to Drive's trash, S3 to the in-bucket trash prefix

## 📋 Requirements

- 🍎 macOS (no Windows/Linux builds)
- [Go](https://go.dev/) 1.26+
- [Node.js](https://nodejs.org/) 20+
- [Wails v3 CLI](https://v3alpha.wails.io/) `v3.0.0-beta.26`:
  ```sh
  go install github.com/wailsapp/wails/v3/cmd/wails3@v3.0.0-beta.26
  ```

## 🗺️ Roadmap

- 🔄 **One-way folder sync**: sync a Drive folder into an S3 bucket;
  Drive changes propagate, S3-side edits stay
- 🔁 **Two-way folder-pair sync**: R2 as a sync hub across machines
- 📦 **Dropbox & OneDrive**: additional storage providers
- 🤖 **MCP server**: expose file search and management to AI agents
  via the Model Context Protocol
- 💬 **AI chat**: natural-language file search, summarization, and
  analysis across all connected storage
- 🏷️ **Smart organization**: AI-powered auto-tagging, duplicate
  detection, and content-based file suggestions
- 📸 **Instagram backup**: archive your posts, reels, stories and media
  into any connected location. Professional (Creator/Business) accounts
  via the Instagram Graph API `GET /me/media`; personal accounts by
  ingesting Meta's data-export archives (no official personal-account API
  since Meta retired Basic Display in December 2024)
- 👥 **Facebook backup**: archive Facebook posts and media the same way,
  scoped to what Meta's API permissions allow, plus data-export ingestion
  as the universal fallback

## 🛠️ Development

### 🏗️ Architecture

A **Wails v3** app: a Go backend paired with a React/TypeScript
webview frontend, packaged as a single native macOS binary.

- **🔧 Backend (Go)**: a local SQLite index (FTS5 search) is the single
  source of truth the UI reads from; a `StorageProvider` abstraction
  lets local folders, Google Drive, and S3 buckets share one interface.
  Only `internal/bridge` imports Wails: everything else is plain,
  testable Go.
- **🎨 Frontend (React + TS + Vite)**: calls a typed `ApiClient` contract
  with two implementations: a `WailsClient` (real bindings) and a
  `MockClient` (fully stateful in-memory app, so the entire UI runs
  in a plain browser with `vite dev`, no Go required).
- **💾 Storage**: SQLite via `modernc.org/sqlite` (pure Go, no CGo);
  the index is disposable (rebuildable by re-crawling) while transfer
  state is durable.

### 🧰 Tech Stack

| Concern | Choice |
|---|---|
| App shell | Wails v3 (Go) |
| Frontend | React + TypeScript + Vite |
| UI styling | Tailwind CSS v4 |
| UI state | Zustand |
| Virtualized lists | TanStack Virtual |
| Index & search | SQLite + FTS5 |
| Migrations | `pressly/goose` |
| Filesystem watching | `fsnotify` (debounced) |
| Google Drive | `golang.org/x/oauth2` + `google.golang.org/api/drive/v3` |
| S3-compatible storage | `github.com/minio/minio-go/v7` |
| Credentials | macOS Keychain (`zalando/go-keyring`) |
| Testing | `stretchr/testify` + `go.uber.org/mock` |

### 🚀 Running in Dev Mode

```sh
make dev        # or: wails3 dev -config ./build/config.yml
```

1. Installs frontend dependencies (`npm --prefix frontend install`, once)
2. Starts the Vite dev server on `127.0.0.1:9245`
3. Builds and launches the Go binary, which loads the webview pointed
   at the dev server: hot reload on both sides

Frontend alone in a browser (no Go, mock backend):

```sh
cd frontend && npm install && npm run dev
```

### ⌨️ Useful Commands

```sh
make help     # list everything
make dev      # wails3 dev (hot reload)
make lint     # golangci-lint run ./...
make lint-fix # auto-fix what the linters can
make fmt      # apply the golangci-lint formatters
make mocks    # regenerate gomock mocks after interface changes
make test     # go test -race ./...
```

### 🧪 Testing

Table-driven, parallel, and asserted via `require` + gomock generated
mocks (see CLAUDE.md rule 7). The gdrive provider runs against a fake
Drive API over `httptest`; the S3 provider against a fake S3 XML
server: no network or real credentials needed.

```sh
go test ./...                 # whole suite
go test -race ./internal/... # with the race detector
```

### 📦 Building

```sh
wails3 package
```

Builds the frontend, compiles the production binary, and packages an
ad-hoc signed app bundle at `bin/AirDrive.app`. The release workflow wraps
that in a `.dmg` (`create-dmg`) and publishes it to GitHub Releases: every
push to `master` refreshes the `nightly` pre-release, and every `v*.*.*`
tag creates a stable release and updates the Homebrew tap.
