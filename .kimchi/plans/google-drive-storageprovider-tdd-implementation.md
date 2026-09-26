<!-- kimchi-work-id: a364c1b3-ab89-4a82-9a71-6820420970c6 -->
# Google Drive StorageProvider — TDD implementation + end-to-end wiring

## Goal

Implement all 11 `StorageProvider` methods for Google Drive in `internal/provider/gdrive` test-first (red → green → refactor per behavior), and wire it end-to-end so a connected Drive location auto-indexes on connect, restores on app restart, syncs incrementally via Drive's changes feed, and supports full CRUD from the UI — all reads served from SQLite.

## Constraints

- **TDD Iron Law**: no production code without a failing test first, per behavior unit. Watch RED, minimal GREEN, refactor with tests staying green. The cycle repeats per method/behavior — not one giant red phase.
  - Exceptions (per TDD skill, with user's standing approval via this plan): `main.go` service-registration lines, regenerated Wails bindings, README text, and the one-line `client.ts` binding swap — configuration/generated code, verified by build + typecheck.
  - Frontend is **Go-only TDD**: no test framework exists in `frontend/`; adding vitest would be a new dev dependency, conflicting with "no new dependencies". Frontend changes verified via `tsc -b && vite build`.
- **No new Go dependencies** — tests use stdlib `testing`/`httptest` only. `google.golang.org/api v0.299.0` (drive/v3) and `golang.org/x/oauth2 v0.37.0` already in go.mod.
- **My Drive only** (project v1 scope); full `drive` scope already granted.
- Code rules (CLAUDE.md): only `bridge/` imports Wails; entities in `entities/`; converters in `converters.go`; nil on error; events via `internal/events` constants; no silent error discards (Warn/Debug logs); README updated in same change.
- **Scope decisions from interview**: no batch-upsert scale work; Google-native files (Docs/Sheets/Slides) are listed/browsable but `Download` errors in v1. **Superseded decision**: "no automated tests" is reversed — TDD adopted per user request.
- `wails3` CLI pinned to `v3.0.0-beta.26`.

## Chunks

### Chunk 1 — Shared MIME/preview helpers (tests first)

- **Scope**: Move `mimeFor`/`previewKind` from localfs to the provider package, test-first at the target API.
- **Files Changed**:
  - `internal/provider/mime_test.go` (new — table-driven tests for `MimeFor` (case-insensitive ext match, dir → `inode/directory`, unknown ext → `application/octet-stream`) and `PreviewKind` (image/video/audio/pdf/text/json → binary classification))
  - `internal/provider/mime.go` (new — exported `MimeFor(name string, isDir bool) string`, `PreviewKind(mime string) string`, verbatim moves from localfs)
  - `internal/provider/localfs/mime.go` (deleted)
  - `internal/provider/localfs/localfs.go` (call sites → `provider.MimeFor` / `provider.PreviewKind`)
- **Depends On**: none.
- **Accept When**: RED verified — `go test ./internal/provider/` fails to compile (functions undefined) — then GREEN after the move; full `go build ./...`, `go vet ./...`, `go test ./...` pass; localfs behavior unchanged.
- **Test Coverage**: `internal/provider/mime_test.go` (created above).

### Chunk 2 — gdrive provider package (TDD per method)

- **Scope**: All 11 `StorageProvider` methods + retry transport, against a fake Drive API served by `httptest`.
- **Files Changed** (all new):
  - `internal/provider/gdrive/fakedrive_test.go` — shared test helper: `newFakeDrive(t)` returning an `httptest.Server` + request recorder. Routes: `GET /drive/v3/files` (list, honors `q`/`pageToken`/`pageSize`), `GET /drive/v3/files/root` (→ `{"id":"ROOTID"}`), `GET|PATCH /drive/v3/files/{id}`, `POST /drive/v3/files` + `/upload/drive/v3/files` (create/multipart), `GET /drive/v3/files/{id}?alt=media`, `GET /drive/v3/changes/startPageToken`, `GET /drive/v3/changes`. JSON fixtures: minimal `FileList`, `File`, `ChangeList`. Provider built in tests via `gdrive.New(ctx, "loc1", staticTS{}, logger, option.WithEndpoint(srv.URL))`.
  - `internal/provider/gdrive/gdrive_test.go` — table-driven `toFileInfo` tests: folder mime → `dir`; `Parents[0] == rootID` → `ParentID ""`; version → `Etag`; Google-native non-folder mime → `PreviewKind "binary"`; `ModifiedTime` parsing.
  - `internal/provider/gdrive/gdrive.go` — `Provider` struct (`locationID`, `*drive.Service`, `rootID`, logger); `New(ctx, locationID string, ts oauth2.TokenSource, logger *slog.Logger, opts ...option.ClientOption)` — builds the service with `option.WithHTTPClient(&http.Client{Transport: &oauth2.Transport{Source: ts, Base: newRetryTransport()}})` plus caller opts (tests pass `option.WithEndpoint`); resolves root ID once via `Files.Get("root").Fields("id")`; `toFileInfo` per the table tests. **gdrive does NOT import `internal/auth`** — it takes a bare `oauth2.TokenSource`.
  - `internal/provider/gdrive/files_test.go` — per-method tests asserting request shape + response mapping: `List` (query contains `'root' in parents` / `'<id>' in parents` and `trashed = false`; pageToken cursor round-trips; orderBy mapping); `Stat`; `Mkdir` (folder mime + parent body); `Rename`; `MoveWithin` (get parents → update `addParents`/`removeParents`); `Delete` (`trashed:true` body); `Download` (binary → stream + FileInfo; Google-native/shortcut → clear v1 error, nothing fetched); `Upload` (multipart create; mime via `provider.MimeFor`).
  - `internal/provider/gdrive/files.go` — implementations above.
  - `internal/provider/gdrive/sync_test.go` — `SyncChanges`: cursor `""` → full walk over two pages → all `ChangeCreated`, returned token = `startPageToken`; cursor ≠ `""` → `removed` → `ChangeDeleted`, `file.trashed` → `ChangeDeleted`, live change → `ChangeCreated`/`ChangeModified`; multi-page changes; returns `newStartPageToken`.
  - `internal/provider/gdrive/sync.go` — implementations above.
  - `internal/provider/gdrive/transport_test.go` — `retryTransport`: GET on 429→429→200 succeeds with 3 attempts; GET retried on 503; POST **not** retried on 503; `Retry-After` honored; injectable `sleep func(time.Duration)` (no-op in tests) keeps them fast.
  - `internal/provider/gdrive/transport.go` — `retryTransport` (field-injectable sleep, ~5 attempts, exp backoff + jitter, cap 30s; retry GETs on 429/502/503/504, all verbs on 429 only).
- **Depends On**: Chunk 1.
- **Accept When**: every method's test watched RED before its implementation; `go test ./internal/provider/gdrive/` green; `go build ./...` + `go vet ./...` pass; `var _ provider.StorageProvider = (*Provider)(nil)` present.
- **Test Coverage**: the four `*_test.go` files above (fake server is shared test infrastructure, real HTTP round-trips — no mocked interfaces).

### Chunk 3 — Token-rotation persistence (TDD)

- **Scope**: Persist rotated refresh tokens back to the Keychain, per `loopback.go`'s documented contract.
- **Files Changed**:
  - `internal/auth/persist_test.go` (new — fake inner `oauth2.TokenSource` + recorder `save func(GdriveToken) error`: rotation triggers save; unchanged token → no save; save error logged, doesn't break `Token()`)
  - `internal/auth/persist.go` (new — `NewPersistingTokenSource(ctx, logger, tok GdriveToken) oauth2.TokenSource` wrapping `oauth2.ReuseTokenSource`; internal constructor takes the injected save func; save failures → `logger.Warn`, never fatal)
- **Depends On**: none (parallelizable with Chunks 1–2).
- **Accept When**: RED watched (constructor/behavior undefined) → GREEN; tests use only the injected save — **no real Keychain writes in tests**.
- **Test Coverage**: `internal/auth/persist_test.go`.

### Chunk 4 — Index sync-cursor persistence (TDD)

- **Scope**: Persist the changes-feed page token in the existing `locations.config` JSON column — no migration.
- **Files Changed**:
  - `internal/index/db_test.go` (new — `index.Open(filepath.Join(t.TempDir(), "test.db"))` real migrations; round-trip `SetSyncCursor` → `GetSyncCursor`; empty config → `""`; pre-existing sibling JSON keys survive the write)
  - `internal/index/db.go` — add `GetSyncCursor(ctx, locationID) (string, error)` / `SetSyncCursor(ctx, locationID, cursor string) error` (read-modify-write `config`'s `"syncCursor"` key)
- **Depends On**: none (parallelizable).
- **Accept When**: RED watched → GREEN; localfs unaffected (its returned cursor is stored but never read).
- **Test Coverage**: `internal/index/db_test.go`.

### Chunk 5 — Incremental crawl + testability seam (TDD)

- **Scope**: Make the generic crawl Drive-capable; introduce the event-emitter seam that makes `LocationService` unit-testable without Wails.
- **Files Changed**:
  - `internal/bridge/location_service_test.go` (new — fake `provider.StorageProvider` with scripted `SyncChanges` (emits creates + a deletion, asserts it was called with the cursor read from the DB, signals a channel for the async goroutine); real `index.DB` on a temp file; recorder `emit`; assert: deleted file gone from index, new cursor persisted, `LocationStatus` indexing→idle + `IndexProgress` events fired, localfs-style full walk (cursor `""`) still works)
  - `internal/bridge/location_service.go` — add `emit func(name string, data ...any)` field (no-op default in constructor; wired to `s.app.Event.Emit` in `SetApp`); `crawl()`: read stored cursor → pass to `SyncChanges`; handle `ChangeDeleted` → `db.DeleteFile` (`Warn` on error, rule 5); persist returned cursor via `SetSyncCursor`; all `Emit` call sites go through `s.emit`
- **Depends On**: Chunks 3 + 4 (compiles after 3; behavior needs a provider — fake in tests).
- **Accept When**: RED watched (deletion/cursor behavior absent) → GREEN; `go test ./...` green.
- **Test Coverage**: `internal/bridge/location_service_test.go`.

### Chunk 6 — Connect + Restore wiring (TDD for orchestration)

- **Scope**: Registration on connect and startup; AuthService removal; DI fields for testable orchestration.
- **Files Changed**:
  - `internal/bridge/location_service_test.go` (extended — `ConnectGdrive` orchestration via injected `doSignIn`/`fetchAccountInfo`/`saveToken` + `newGdriveProvider` returning the fake provider: asserts location row, registry registration, crawl started (channel signal), returned entity; `Restore`: gdrive location + valid fake token → provider registered + crawl called **with the stored cursor**; missing token → `LocationStatus` "error" event with re-connect message and no crawl; localfs location → provider registered, no crawl)
  - `internal/bridge/location_service.go` — DI fields with production defaults set in the constructor: `openBrowser` (← `app.Browser.OpenURL` in `SetApp`), `doSignIn` (← `auth.SignIn`), `fetchAccountInfo` (← `auth.FetchAccountInfo`), `saveToken`/`loadToken` (← `auth.SaveToken`/`auth.LoadToken`), `newGdriveProvider` (← builds `auth.TokenSource` → `auth.NewPersistingTokenSource` → `gdrive.New`). Add `ConnectGdrive()` (moved from AuthService, same name/signature): sign-in → account info → save token → `UpsertLocation` → register → `go s.crawl(id, p)` (full walk). Add `Restore() error` (moves `main.go restoreLocations`): localfs → register + watch; gdrive → `loadToken` (missing → `emitLocationStatus` error, `Warn` log) → `newGdriveProvider` → register → `go s.crawl(id, p)` (incremental via stored cursor).
  - `internal/bridge/auth_service.go` — **deleted**.
  - `main.go` — drop AuthService wiring; `go locSvc.Restore()` replaces `restoreLocations` (deleted). Build-verified (config-code exception).
  - `frontend/src/bindings/...` — regenerated via `wails3 generate bindings -ts` (generated-code exception).
  - `frontend/src/lib/api/wails/client.ts` — `AuthSvc.ConnectGdrive()` → `LocSvc.ConnectGdrive()`; drop `AuthSvc` import. Typecheck-verified (exception above).
- **Depends On**: Chunks 2, 3, 4, 5.
- **Accept When**: orchestration tests RED first → GREEN; `go test ./...` green; `npm --prefix frontend run build` passes; async crawls synchronized in tests via channel signals (no sleeps/races; run `go test -race ./internal/bridge/`).
- **Test Coverage**: `internal/bridge/location_service_test.go` (extended).

### Chunk 7 — README + final verification pass

- **Scope**: Docs sync + full verification of all acceptance criteria.
- **Files Changed**:
  - `README.md` — status: Google Drive supports sign-in **plus browsing/CRUD/search via the index**; test commands documented (`go test ./...`).
- **Depends On**: Chunk 6.
- **Accept When**: all criteria below verified (automated + manual).
- **Test Coverage**: n/a (docs).

## Verification Strategy

After every chunk: `go build ./...` && `go vet ./...` && `go test ./...` (plus `go test -race ./internal/bridge/ ./internal/provider/gdrive/` for the concurrent paths). TDD discipline: each test watched failing for the expected reason before its implementation; failures fixed in code, never in tests.

Manual end-to-end pass in `wails3 dev -config ./build/config.yml` with a real Google account (fixtures can't prove live-API truth):

1. Connect Drive → auto-index starts, file count grows, status ends "idle" with correct count/size.
2. Browse folders, expand, paginate — all from SQLite (instant, no per-click network).
3. Search overlay finds Drive files.
4. `mkdir`/`rename`/`move`/`delete` from AirDrive visible in Drive's web UI; delete lands in Drive trash; download of a binary file lands in `~/Downloads`.
5. Rename/delete a file in Drive's web UI → refresh in AirDrive → index updates; second refresh near-instant (changes feed).
6. Download a Google Doc/shortcut → clear v1 error, nothing written.
7. Quit + relaunch → location restored, browsing works, incremental sync runs.
8. Shortcuts appear in listings as files.

## Decision Log

| # | Decision | Rationale / Rejected alternative |
|---|---|---|
| 1 | `ConnectGdrive` moves to `LocationService`; `AuthService` deleted | Symmetry with `AddLocalLocation`. Rejected: injecting registry into AuthService duplicates location-creation logic. User-confirmed. |
| 2 | `LocationService.Restore()` + auto incremental sync on startup | Changes feed = 1–3 API calls regardless of downtime; ghosts otherwise persist until manual refresh. User-confirmed. |
| 3 | Cursor saved only on crawl completion; interrupted crawl re-walks | Cursor doubles as "snapshot complete" flag — mid-walk resume silently misses files added in already-passed pages. User-confirmed. |
| 4 | MIME/preview helpers extracted to `internal/provider/mime.go` | Project rule (>2 uses → extract); rejected duplication (drift). User-confirmed. |
| 5 | Rotated refresh tokens re-saved to Keychain | `loopback.go` documents this contract. User-confirmed. |
| 6 | Shortcuts listed as files, Download errors | Consistent with Google-native handling; hiding them makes counts disagree with Drive's UI. User-confirmed. |
| 7 | Google-native Download = clear error in v1 | User-confirmed; export-format decision deferred. |
| 8 | **TDD adopted — supersedes the earlier "no tests" interview decision** | User request ("enrich the plan with TDD"). Red→green→refactor per behavior; generated/config code exempt per skill. |
| 9 | Fake Drive API = shared `httptest` server (real HTTP round-trips), not mocked interfaces | TDD skill: real code, mocks only when unavoidable; the drive client is exercised through its real transport stack incl. the retry layer. |
| 10 | `gdrive.New` takes `opts ...option.ClientOption` and does **not** import `internal/auth` | Testability (endpoint injection) + layering (token persistence belongs to `auth` as `NewPersistingTokenSource`). |
| 11 | `LocationService` gets DI fields (`emit`, `openBrowser`, `doSignIn`, `fetchAccountInfo`, `saveToken`/`loadToken`, `newGdriveProvider`) with production defaults | Only seam that makes bridge logic testable without constructing a Wails `application.App`; defaults keep production wiring single-sourced in `SetApp`/constructor. |
| 12 | Retry transport added | **Verified in module cache**: `api@v0.299.0/drive/v3/drive-gen.go` `FilesListCall.doRequest` → plain `gensupport.SendRequest`, no default retry. GETs retried on 429/5xx, all verbs on 429 only (never double-execute mutations). Injectable sleep for fast tests. |
| 13 | Etag = Drive `version`; root ID resolved eagerly in `New()` | `version` bumps on metadata+content (right sync anchor); root ID needed to normalize `parentID` (root → `""`) so `ListDir(dirID="")` matches root children. |
| 14 | Sync cursor in `locations.config` JSON; tests use `t.TempDir()` DB files | No migration; explicit temp files avoid `:memory:` DSN assumptions with goose. |
| 15 | Frontend: Go-only TDD | No test framework in `frontend/` (verified `package.json`); adding vitest = new dev dependency, conflicts with constraint. One-line binding swap + regenerated bindings are config/generated-code exceptions, verified by `tsc -b`. |

## Risks

- **Fixture drift** — the fake Drive server's JSON shapes may diverge from the real API. Mitigated by the manual E2E checklist (above) against a real account, which remains the final gate.
- **Async crawl in tests** — goroutine-based; tests must synchronize via channels with timeouts (no sleeps), plus `-race`. Specified in Chunk 6.
- **Rate limits on huge Drives** — mitigated by the retry transport and 1000/page enumeration; walk is metadata-only. Slow first crawl accepted (scope decision).
- **Changes-feed edge cases** (untrash, parent moves mid-sync) — `removed || trashed` → delete; live change → re-upsert with fresh parents; full re-walk remains the fallback.
- **Token revoked mid-session** — provider calls surface errors → `LocationStatus` "error" + re-connect message; index is disposable.
- **DI fields increase LocationService surface** — defaults live in one place (constructor + `SetApp`); risk of drift is contained by the orchestration tests that exercise the same defaults.

## Open Questions

None — all resolved (decisions 1–7 user-confirmed in the grill session; 8–15 grounded in code/module-cache verification and this plan's TDD enrichment).