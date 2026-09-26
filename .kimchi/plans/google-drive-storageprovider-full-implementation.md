<!-- kimchi-work-id: a364c1b3-ab89-4a82-9a71-6820420970c6 -->
# Google Drive StorageProvider — full implementation + end-to-end wiring

## Goal

Implement all 11 `StorageProvider` methods for Google Drive in `internal/provider/gdrive`, and wire it end-to-end so a connected Drive location auto-indexes on connect, restores on app restart, syncs incrementally via Drive's changes feed, and supports full CRUD from the UI — all reads served from SQLite.

## Constraints

- **No new dependencies** — `google.golang.org/api v0.299.0` (drive/v3) and `golang.org/x/oauth2 v0.37.0` already in go.mod.
- **My Drive only** (project v1 scope, per `internal/auth/oauth.go` comment); full `drive` scope already granted.
- Code rules (CLAUDE.md): only `bridge/` imports Wails; entity types in `entities/`; converters in `converters.go`; **nil on error**; events via `internal/events` constants; **no silent error discards** (log Warn/Debug); README updated in same change.
- **Scope decisions from interview**: no batch-upsert scale work (per-file upserts stay); **no automated tests** (manual verification); Google-native files (Docs/Sheets/Slides) are listed/browsable but `Download` returns a clear error in v1.
- `wails3` CLI pinned to `v3.0.0-beta.26`.

## Chunks

### Chunk 1 — Extract shared MIME/preview helpers

- **Scope**: Move `mimeFor`/`previewKind` from localfs to the provider package so gdrive reuses them.
- **Files Changed**:
  - `internal/provider/mime.go` (new — exported `MimeFor(name string, isDir bool) string`, `PreviewKind(mime string) string`, verbatim moves)
  - `internal/provider/localfs/mime.go` (deleted)
  - `internal/provider/localfs/localfs.go` (call sites → `provider.MimeFor` / `provider.PreviewKind`)
- **Depends On**: none.
- **Accept When**: `go build ./...` and `go vet ./...` pass; localfs behavior unchanged (pure-function move); `wails3 dev` still browses a local folder with correct icons/preview kinds.
- **Test Coverage**: none (user decision) — manual browse check in dev mode.

### Chunk 2 — gdrive provider package

- **Scope**: All 11 `StorageProvider` methods, Drive API mapping, ID semantics, retry transport, token persistence.
- **Files Changed** (all new):
  - `internal/provider/gdrive/gdrive.go` — `Provider` struct (`locationID`, `*drive.Service`, `rootID`, logger), `New(ctx, locationID, oauth2.TokenSource, logger)`; `New` builds the service via `option.WithHTTPClient` (retry transport under `oauth2.Transport`) and resolves the real root folder ID once via `Files.Get("root").Fields("id").Do()`; `toFileInfo(*drive.File) provider.FileInfo` mapping: `ID=f.Id`, `ParentID = f.Parents[0]` normalized to `""` when it equals `rootID` (so `db.ListDir(dirID="")` matches root children), `Kind` from folder mime, `Size=f.Size` (0 for Google-native), `MIME=f.MimeType`, `Etag=strconv.FormatInt(f.Version, 10)` (per-file revision counter — bumps on any change; better sync signal than content-only headRevisionId), `ModifiedAt` from `f.ModifiedTime`, `PreviewKind` via `provider.PreviewKind` with `application/vnd.google-apps.*` (non-folder) mapped to `"binary"`.
  - `internal/provider/gdrive/files.go` — CRUD: `List` (`files.list`, `q: '<dirID|root>' in parents and trashed = false`, `pageSize` ≤1000, **cursor = Drive pageToken verbatim**, orderBy mapping name→`folder,name` / modifiedAt→`folder,modifiedTime` / size→`quotaBytesUsed`, masked `Fields`); `Stat` (`files.get`); `Mkdir` (`files.create`, folder mime, parent or `"root"`); `Rename` (`files.update` name); `MoveWithin` (per file: `files.get` parents → `files.update` with `addParents`/`removeParents`); `Delete` (`files.update` `trashed=true`); `Download` — if `application/vnd.google-apps.*` (non-folder) or shortcut → `fmt.Errorf("gdrive: %q is a Google-native/shortcut file — export-download is not supported in v1", name)`, else `Files.Get(id).Download(ctx)` stream; `Upload` (`files.create` multipart, mime via `provider.MimeFor`).
  - `internal/provider/gdrive/sync.go` — `SyncChanges`: cursor `""` → `changes.getStartPageToken` (token captured before walk), full walk via `files.list` (`q: trashed = false`, pageSize 1000, emit each page as `ChangeCreated` batch), return the start token; cursor ≠ `""` → `changes.list` (`pageToken`, `includeRemoved`, `spaces=drive`): `removed` or `file.trashed` → `ChangeDeleted`, else `ChangeCreated`/`ChangeModified`; return `newStartPageToken` after the last page.
  - `internal/provider/gdrive/transport.go` — (a) `retryTransport`: `http.RoundTripper` retrying **GETs on 429/502/503/504** and **all verbs on 429** (quota refusal = definitely not executed; never retries POST/PATCH on 5xx to avoid double-mkdir), honoring `Retry-After`, exponential backoff with jitter (base 250ms, cap 30s), ~5 attempts; (b) `persistingTokenSource`: wraps `oauth2.ReuseTokenSource`, re-saves via `auth.SaveToken(locationID, …)` when the refresh token rotates — required by `internal/auth/loopback.go`'s documented contract.
- **Depends On**: Chunk 1.
- **Accept When**: `go build ./...` + `go vet ./...` pass; the package compiles standalone with no Wails imports; `var _ provider.StorageProvider = (*gdrive.Provider)(nil)` assertion present.
- **Test Coverage**: none (user decision).

### Chunk 3 — Index sync-cursor persistence

- **Scope**: Persist the changes-feed page token in the existing `locations.config` JSON column — no migration.
- **Files Changed**:
  - `internal/index/db.go` — add `GetSyncCursor(ctx, locationID) (string, error)` and `SetSyncCursor(ctx, locationID, cursor string) error` (read-modify-write the `config` JSON's `"syncCursor"` key; treat unset config as empty cursor).
- **Depends On**: none (parallelizable with Chunks 1–2).
- **Accept When**: build passes; localfs path unaffected (localfs ignores the cursor on input and its returned cursor is simply stored and unused next time — backward compatible).
- **Test Coverage**: none (user decision).

### Chunk 4 — Incremental crawl with deletion handling

- **Scope**: Make the generic crawl Drive-capable.
- **Files Changed**:
  - `internal/bridge/location_service.go` — `crawl()`: read the location's stored cursor (`db.LocationByID` → `GetSyncCursor`) and pass it to `SyncChanges`; in the emit loop handle `ChangeDeleted` → `db.DeleteFile` (log `Warn` on error, per rule 5) alongside the existing upsert; after a successful sync, persist the returned cursor via `SetSyncCursor`.
- **Depends On**: Chunk 3 (compiles independently; end-to-end behavior needs Chunk 2).
- **Accept When**: build passes; localfs full re-walk still works identically (localfs ignores cursors); for gdrive, a file deleted in Drive's web UI disappears from AirDrive's index after `refreshLocation`, and a second refresh is visibly fast (changes feed, not re-walk).
- **Test Coverage**: none (user decision).

### Chunk 5 — Connect + Restore wiring

- **Scope**: Registration on connect and startup; auto-index; AuthService removal.
- **Files Changed**:
  - `internal/bridge/location_service.go` — add `ConnectGdrive() (*entities.Location, error)` (moved from AuthService; keeps same method name/signature): `auth.SignIn` → `auth.FetchAccountInfo` → `auth.SaveToken(id, token)` → `UpsertLocation` → construct `gdrive` provider (persisting token source) → `registry.Register` → `go s.crawl(id, p)` (full walk, cursor ""). Add `Restore() error` (moves `main.go restoreLocations` here): localfs branch unchanged (register + watch); gdrive branch — `auth.LoadToken` (missing/invalid → emit `ev.LocationStatus` state `"error"` with a clear "credentials missing — remove and re-connect the location" message, per rule 5 logging), `auth.TokenSource` → persisting wrapper → `gdrive.New` → `registry.Register` → `go s.crawl(id, p)` (incremental via stored cursor).
  - `internal/bridge/auth_service.go` — **deleted** (its only method moved).
  - `main.go` — drop AuthService construction/registration/service registration; replace `go restoreLocations(...)` with `go locSvc.Restore()`; delete the now-unused `restoreLocations` func (and its imports).
  - `frontend/src/bindings/...` — regenerated via `wails3 generate bindings -ts` (authservice module disappears, locationservice gains `ConnectGdrive`).
  - `frontend/src/lib/api/wails/client.ts` — `connectGdrive()` body: `AuthSvc.ConnectGdrive()` → `LocSvc.ConnectGdrive()`; drop the `AuthSvc` import.
- **Depends On**: Chunks 2, 3, 4.
- **Accept When**: connecting Drive in the app immediately starts indexing (sidebar "indexing" → file count via `IndexProgress`, same UX as adding a local folder); quitting and relaunching restores the location with no re-connect and runs the cheap incremental sync; `npm run build` (frontend) passes with no type errors.
- **Test Coverage**: none (user decision).

### Chunk 6 — README + final verification pass

- **Scope**: Docs sync + full manual verification of the acceptance criteria.
- **Files Changed**:
  - `README.md` — status section: Google Drive now supports sign-in **plus browsing/CRUD/search via the index** (S3 + transfers still pending).
- **Depends On**: Chunk 5.
- **Accept When**: all 9 confirmed completion criteria verified manually (see Verification Strategy).
- **Test Coverage**: none (user decision).

## Verification Strategy

After every chunk: `go build ./...` && `go vet ./...`. Frontend chunks additionally: `npm --prefix frontend run build`.

Final manual pass in `wails3 dev -config ./build/config.yml` with a real Google account (no automated tests per user decision):

1. Connect Drive → auto-index starts, file count grows, status ends "idle" with correct count/size.
2. Browse folders, expand, paginate — all from SQLite (instant, no per-click network).
3. Search overlay finds Drive files.
4. `mkdir`/`rename`/`move`/`delete` from AirDrive are visible in Drive's web UI; delete lands in Drive trash; download of a binary file lands in `~/Downloads`.
5. Rename/delete a file in Drive's web UI → refresh in AirDrive → index updates (deletions honored); second refresh is near-instant (changes feed).
6. Download a Google Doc/shortcut → clear v1 error, nothing written.
7. Quit + relaunch → location restored, browsing works, incremental sync runs.
8. Shortcuts appear in listings as files.

## Decision Log

| # | Decision | Rationale / Rejected alternative |
|---|---|---|
| 1 | `ConnectGdrive` moves to `LocationService`; `AuthService` deleted | Symmetry with `AddLocalLocation` (one service owns create/register/crawl). Rejected: injecting registry into AuthService duplicates location-creation logic. User-confirmed. |
| 2 | `LocationService.Restore()` + auto incremental sync on startup | Changes feed makes startup sync 1–3 API calls regardless of downtime; ghosts otherwise persist until manual refresh. User-confirmed. |
| 3 | Cursor saved only on crawl completion; interrupted crawl re-walks | Cursor doubles as "snapshot complete" flag — mid-walk resume would silently miss files added in already-passed pages (changes feed won't backfill unchanged files). User-confirmed. |
| 4 | MIME/preview helpers extracted to `internal/provider/mime.go` | Project rule (>2 uses → extract); rejected duplication (drift). User-confirmed. |
| 5 | Rotated refresh tokens re-saved to Keychain | `loopback.go` documents this contract; without it a rotation kills the location on next launch. User-confirmed. |
| 6 | Shortcuts listed as files, Download errors | Consistent with Google-native handling; hiding them makes counts disagree with Drive's UI. User-confirmed. |
| 7 | Google-native Download = clear error in v1 | User-confirmed; export-format decision deferred. |
| 8 | No automated tests; no batch-upsert scale work | User-confirmed scope. |
| 9 | Retry transport added | **Verified in module cache**: `api@v0.299.0/drive/v3/drive-gen.go` `FilesListCall.doRequest` → plain `gensupport.SendRequest`, no default retry — a single 429 would kill a 1M-file crawl. Retry policy: GETs on 429/5xx, all verbs on 429 only (never double-execute mutations). |
| 10 | Etag = Drive `version` (int) | Bumps on metadata *and* content changes — the right change-detection anchor; `headRevisionId` misses metadata-only changes. |
| 11 | Root ID fetched eagerly in `New()` via `Files.Get("root")` | Needed to normalize `parentID` (root ID → `""`) so SQLite `ListDir(dirID="")` matches root children; queries use the `'root'` alias. |
| 12 | Sync cursor in `locations.config` JSON | Column already exists, documented for "non-secret settings" — no migration. |
| 13 | `parentID` = first of `f.Parents` | Single-parent is the Drive model now; multi-parent legacy items appear once under their first parent. |

## Risks

- **Rate limits on huge Drives** — mitigated by the retry transport (Chunk 2) and 1000/page enumeration; the walk is metadata-only. Residual: a very slow first crawl (accepted with scope decision 8).
- **Changes-feed edge cases** (untrash, parent moves mid-sync) — mitigations: `removed || trashed` → delete; `ChangeCreated` on any live change re-upserts with fresh parents; full re-walk remains the fallback via `refreshLocation` after removing the stored cursor.
- **Token revoked mid-session** — provider calls surface errors → `LocationStatus` "error" with a re-connect message; no data loss (index is disposable).
- **No automated tests** — the localfs mime extraction is a verbatim pure-function move (low risk); provider behavior is verified only via the manual checklist, so regressions would surface as UI-visible breakage rather than CI failures. Accepted by user.
- **Bindings regeneration churn** — `wails3 generate bindings -ts` may emit unrelated diffs; CLI is pinned to `v3.0.0-beta.26` to match the repo.

## Open Questions

None — all resolved through the interview and grill session (decisions 1–8 user-confirmed; 9–13 grounded in code/module-cache verification).