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

**Direct download**: grab the latest `.dmg` from the [Releases](https://github.com/radsadowski/airdrive/releases) page.

**Homebrew**: add the tap, then install the cask:

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
  - [☁️ Where Your Files Can Live](#️-where-your-files-can-live)
  - [🔄 Moving Files Between Storages](#-moving-files-between-storages)
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

- **All your files, one window**: browse every connected location side
  by side: folders on this Mac, Google Drive, S3 buckets
- **Instant browsing**: folders open right away, even in the cloud;
  nothing waits on the network
- **🔍 Search everything**: find any file by name across all your
  storage at once; narrow it down by size, date, or type
- **✏️ Everyday file actions**: create folders, rename, move, and
  delete. Deleting always goes to a trash, never permanently
- **🖼️ Photos and videos at a glance**: real image thumbnails and video
  stills in the grid, plus a large preview when you open a file

### ☁️ Where Your Files Can Live

- **💾 Local folders**: any folder on this Mac; new and changed files
  show up on their own
- **📧 Google Drive**: sign in with your Google account, then connect
  all of My Drive or just one folder. Starred folders and
  Drive-for-desktop computer backups work too
- **🪣 S3-compatible storage**: Cloudflare R2, MinIO, Backblaze B2, or
  AWS. Deleting moves files to a trash folder inside the bucket, so an
  accidental click never destroys anything

### 🔄 Moving Files Between Storages

- **📋 Copy or move**: drive to bucket, Mac to cloud, cloud to Mac;
  anything to anything
- **⚖️ You decide what happens to duplicates**: keep both, replace, or
  skip. By default identical files are skipped and different ones are
  kept side by side
- **✅ Arrives intact**: every copy is double-checked on arrival, so a
  broken transfer never silently replaces a good file
- **📊 Live progress**: watch per-file speed, pause and resume big
  copies, cancel, or retry just the files that failed
- **⬆️ Uploads**: drop files from Finder onto any cloud location, or
  use the Upload button

### 🔒 Security

- **🔐 Sign in the standard way**: Google sign-in happens in your
  browser; the sign-in token stays in the macOS Keychain, never in the
  app's own database
- **🔑 Cloud passwords protected**: S3 access keys also live in the
  Keychain, never on disk in plain text
- **🛡️ Nothing is erased outright**: local files go to the Mac trash,
  Drive files to Drive trash, bucket files to a trash folder you can
  empty yourself

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
- 🗺️ **Photo map**: plot photos on an interactive world map by their GPS
  coordinates, clustered by place, with jump from a pin to the file's
  folder. Coordinates come from EXIF metadata (local files, and cloud
  files read during indexing); Instagram strips EXIF, so Instagram posts
  use the separate location tag in export metadata instead

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
