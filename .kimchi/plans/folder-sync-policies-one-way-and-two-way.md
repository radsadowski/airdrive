<!-- kimchi-work-id: 774cb79b-df5a-4dec-8c1d-bb6da1b2215a -->
# Folder sync policies (one-way and two-way)

## Goal
Users define reusable sync policies between any two locations (local folder, Drive folder, S3 bucket): **one-way** (X→Y mirror of chosen actions) or **two-way** (both sides converge), with per-policy action selection (additions / updates / deletions), plain-language explanations, continuous + hourly + manual triggering, and transfer-engine visibility for every moved byte.

## Constraints
- [decision] Conflict rule for two-way: **newest-modified wins**; delete beats modify on collision.
- [decision] Scheduling: continuous (watcher-driven, 60 s debounce) + hourly sweep + manual "Sync now".
- [decision] New-policy defaults: additions + updates ON, deletions OFF (warning when enabling).
- [decision] Moves run through the existing transfer engine (progress, pause/cancel/retry, MD5 in the Transfers panel).
- [decision] Wizard asks "sync existing files now?" default yes (backfill).
- [safety c4] A policy only deletes files **previously recorded in its sync_state**; never unknown destination files; the backfill pass never deletes.
- Index DB is disposable: edit `0001_initial.sql` in place; the probe rebuild self-heals old DBs.
- TDD red → green per behavior; rule-7 test style; rule-9 gate before push; README user-friendly + roadmap updated (rule 6).

## Chunks

### 1. Schema + pair CRUD + bridge SyncService skeleton
- **Scope**: `0001_initial.sql`: `sync_pairs` gains columns `direction TEXT ('a_to_b'|'b_to_a'|'both')`, `actions INTEGER` (bitmask 1=created,2=modified,4=deleted), `paused INTEGER`, `created_at TEXT`; keep `sync_state` as-is (`pair_id, file_id, last_etag_a, last_etag_b`, last_mtimes). DB methods: `CreateSyncPair`, `ListSyncPairs`, `UpdateSyncPair` (pause, actions), `DeleteSyncPair` (+cascade state), `GetSyncState`/ `UpsertSyncState`. `entities.SyncPair`. Bridge `SyncService`: `CreateSyncPair` (+validation), `ListSyncPairs`, `PauseSync`, `DeleteSyncPair`, `SyncNow`. Regenerate bindings.
- **Files Changed**: `internal/index/migrations/0001_initial.sql`, `internal/index/db.go`, `internal/index/db_test.go`, `internal/entities/entities.go`, `internal/bridge/sync_service.go`, `internal/bridge/sync_service_test.go`, `main.go`, regenerated `frontend/src/bindings/`
- **Depends On**: none
- **Accept When**: CRUD roundtrips via table tests; validation rejects self-pairs and nested same-location paths (both directions) with clear errors; bindings regenerate clean.
- **Test Coverage**: `db_test.go` pair CRUD; `sync_service_test.go` validation + CRUD via mocked registry/db
- **Open Questions**: none

### 2. Pure delta engine (`internal/syncdelta`)
- **Scope**: new package, no I/O: input = the two subtree file maps (from the index, keyed by path: etag, mtime, size, kind) + prior sync_state rows + pair (direction, actions). Output = ordered plan: `[]PlanStep {CABA: CopyAToB, CopyBToA, DeleteOnA, DeleteOnB, Stet}` plus the sync_state mutations to record after each successful apply. Rules: one-way a_to_b: never emits CopyBToA/DeleteOnA; updates compare etag (fall back to mtime+size when an etag side is empty); two-way unchanged-since-etag sides fast-path Stet; both-changed → record `Conflict` resolution via newer mtime; delete-vs-modify → delete emitted (when deletes enabled) and the modify side's state cleared; deletes only for files present in prior sync_state; backfill mode: emit only copies for files missing on the destination, never deletes.
- **Files Changed**: `internal/syncdelta/syncdelta.go`, `internal/syncdelta/syncdelta_test.go`
- **Depends On**: none
- **Accept When**: every rule in the spec is exercised by a when-X-then-Y table case; loop-protection invariant tested: applying a plan then re-running delta with recorded state yields an empty plan.
- **Test Coverage**: exhaustive table tests incl. masks, conflicts, unseen-file safety
- **Open Questions**: none

### 3a. Reconcile engine: apply + backfill + SyncNow
- **Scope**: `SyncService.reconcile(pairID)`: load pair (perform action if not paused), pull subtree file maps for path_a/path_b from the **index** (prefix walk, no live provider scans), run syncdelta, apply each plan step through `TransferService` enqueue (copy steps, conflict=overwrite, verify MD5) and `FileService.Delete` (delete steps); register batch completion (poll `ListTransferBatches` progress callback goroutine) → record sync_state mutations from delta; write `pair.LatestStatus` to the DB row (last_run_at, counts, last_error). `SyncNow` → reconcile immediately. Backfill: create-with-backfill → one initial reconcile in backfill mode. Loop guard: `reconcileInFlight[pairID]` set: change-events for a pair's locations are ignored while its own write pass runs; also refresh index for dest provider after apply (dest provider's own crawl/watcher eventually updates; gdrive changes feed covers it) — assert convergence: after events settle, next reconcile is empty (chunk-2 property test makes this real).
- **Files Changed**: `internal/bridge/sync_engine.go`, `internal/bridge/sync_engine_test.go`, `internal/bridge/sync_service.go`
- **Depends On**: 1, 2
- **Accept When**: integration test: fake gdrive (changes) ↔ fake s3 (initial walk): create policy a_to_b with backfill → pre-seeded files copied over, sync_state recorded, second reconcile no-op; edit source → propagates; edit dest → NOT copied back; enable deletions → removed unseen earlier and now → dest file deleted, after having been seen.
- **Test Coverage**: `sync_engine_test.go` via fake HTTP servers + real transfer engine wiring; scheduled-once verify via SyncNow
- **Open Questions**: none

### 3b. Scheduling: continuous + hourly + manual
- **Scope**: Fan-in of index-changed signals (watcher OnChange for localfs; the crawl/changes feed completion already emits `location:status` idle; hook app-wide signal where practical — simplest deterministic trigger: reuse the same path the location-status sync emits on, plus an in-process debouncer keyed by location: 60 s quiet → reconcile all non-paused pairs touching that location) + hourly `time.Ticker` sweep over all pairs + the chunk-3a `SyncNow`. Debouncer is injectable (test with a fake clock via an injected `func(time.Duration) <-chan time.Time`).
- **Files Changed**: `internal/bridge/sync_scheduler.go`, `internal/bridge/sync_scheduler_test.go`
- **Depends On**: 3a
- **Accept When**: debounce coalesces a burst into one reconcile per location; hourly ticker reconciles paused=0 pairs only; in-flight guard from 3a prevents reentrancy (test); no goroutine leak (WaitGroup counted).
- **Test Coverage**: `sync_scheduler_test.go` with injected clock + fake reconcile counters
- **Open Questions**: none

### 4. Frontend: sync wizard + policies section
- **Scope**: contract `SyncPair`, `SyncAction`, service methods + events (`sync:updated {pairId, status, lastRunAt, counts...}`); mock client implements a tiny simulated pair engine (status transitions only); new "Sync" section: wizard dialog (steps: pick storages → pick folders → direction radio with two plain-language cards "One-way: keeps Y up to date with X. Changes in Y stay on Y only." / "Two-way: both sides always look the same. Edits propagate both directions." → action checkboxes with a deletion warning + backfill checkbox default-on → review) and a policies list with per-policy row: direction glyph (→ / ⇄), action badges, status line (last run, counts, next sweep ETA), buttons: Sync now / pause-resume / edit actions+paths / delete; transfers panel shows sync batches labeled `sync: <policy name>`.
- **Files Changed**: `frontend/src/lib/api/contract.ts`, `frontend/src/lib/api/wails/client.ts`, `frontend/src/lib/api/mock/{client,state,runtime}.ts`, `frontend/src/lib/events.ts`, `frontend/src/stores/app.ts` (+`stores/sync.ts` if cleaner), `frontend/src/components/sync.tsx` (new), `frontend/src/views/AppShell.tsx`, `frontend/src/components/sidebar.tsx`, `frontend/src/components/wizards.tsx`, `frontend/src/app.css` (only if a token is needed)
- **Depends On**: 3b (needs `SyncNow` + statuses); mock independent (can build in parallel against the contract)
- **Accept When**: `npm run build` + `npx oxlint src/` clean; mock-mode demo: create a two-way policy, add a file in mock storage A, see it appear in B via the mock runtime; wizard copy readable by a non-technical user (requirement #3); deletion checkbox shows the warning text.
- **Test Coverage**: build + manual; mock runtime covered by oxlint clean compile
- **Open Questions**: none

### 5. README, roadmap, final gate, manual E2E
- **Scope**: user-friendly feature bullets for both sync modes; roadmap: check off one-way and two-way items (this feature ships both, merging roadmap rows); full rule-9 gate; manual E2E matrix: local↔Drive two-way, Drive→bucket one-way with deletions enabled, verify convergence, pause, delete-policy cleanup.
- **Files Changed**: `README.md`
- **Depends On**: 1–4
- **Accept When**: gate all-green; CI green on push; nightly refreshed.
- **Test Coverage**: n/a
- **Open Questions**: none

## Verification Strategy
1. Unit: exhaustive syncdelta tables (each spec rule = one named case); CRUD + validation; scheduler with injected clock; loop-protection property.
2. Integration: chunk-3a E2E using the repo's existing fake Drive / fake S3 HTTP servers and the real transfer engine and real DB.
3. Rule-9 gate on every push (build, vet, golangci-lint pinned, full `go test -count=1`, frontend build + oxlint).
4. CI green, then manual user E2E matrix (chunk 5).

## Decision Log
| Decision | Rationale | Rejected |
|---|---|---|
| Engine reads file maps from the index, not live provider walks | index already mirrors both storages with etags/mtimes; delta is instant and scan-free | re-walking providers per pass (network-heavy, slow) |
| One pair row + `direction` enum | two-way = one entity with symmetric rules; actions mask applies both directions | two directional rows per two-way pair (double bookkeeping) |
| Newest-wins conflict (+ delete over modify) | deterministic, zero interruption; matches user pick | keep-both rename (noise), ask-user (large UI scope now) |
| Deletes via provider `Delete` (auto-trash semantics per provider) | mirror intent + provider trash = safe by construction | hard-unlink deletes (irreversible) |
| Only delete previously-synced files | anti-catastrophe for point-at-wrong-folder, first-run sweeps, dest-only files | free-form mirror deletions (data-loss risk) |
| sync_state mutations recorded ONLY after job success | crash mid-pass = next pass replays the unfinished delta | optimistic state (silent divergence) |
| Hourly timer + per-location 60 s debounce + SyncNow | cheap, deterministic, covers missed events | cron expressions per policy (overkill) |

## Risks
- **Looping A↔B re-sync**: watcher fires for files the sync itself wrote → guard = in-flight flag + sync-state short-circuit makes replays empty. Tested as property in chunk 2 + reentrancy test in 3b.
- **Big first backfill floods the transfers queue** (10k files): acceptable for v1 (progress visible, pausable); a future "background quiet batching" option can cap concurrency per policy further without touching this design.
- **Watcher gaps on cold dests**: e.g., location added *before* this feature: its indexed etags may be stale → hourly sweep covers convergence eventually; documented behavior.
- **Modify-only-because-mtime-differ (etag empty both sides)**: false-positive copies on providers without etags; etag+mtime+size fallback ordering narrows it; MD5 verify still passes (same bytes) so nothing destructive.
- **Deleting a location with active pairs** → pairs reference dangling ids: chunk 1 cascade test; UI hides them silently (decision: not a blocker for v1).

## Implementation notes (as-built deltas)

- chunk 3 E2E uses two real localfs locations rather than fake Drive ↔
  fake S3: the fakes live in their own packages' test files and cannot
  cross-import; localfs exercises the same transfer/index/state engine
  end to end on real file systems
- providers now declare Reconciles() where their sync feed is a full
  inventory walk (localfs, s3compat): both the UI crawler and the engine
  refresh use it to diff deletions out of the index; gdrive feeds signal
  deletions themselves
- copy batches carry conflict=overwrite always: sync semantics says the
  delta pass owns the decision, so per-job collision asks must not
  rewrite names (keep-both would break convergence)
- file copies group per destination parent directory: one batch per
  parent, not one per file (keeps the Transfers drawer readable during
  backfills)
- chunk 4 wizard: compared to the ASCII sketch, both endpoint folder
  pickers render side-by-side above the direction cards, and backfill
  read "copy existing files right away" (default on); edit policies do
  not offer backfill (their fill already ran)
- transfers are not renamed to "sync: <name>": batch progress already
  lands in the Transfers drawer with per-job MD5 status; the pair card
  reports per-run counts instead
