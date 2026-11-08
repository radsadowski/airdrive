package bridge

import (
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/radosny/airdrive/internal/index"
	"github.com/radosny/airdrive/internal/provider"
	"github.com/radosny/airdrive/internal/provider/localfs"
)

// syncE2E builds a real two-localfs playground: two tmp roots, real
// providers, a real transfer engine, a real file service.
type syncE2E struct {
	t       *testing.T
	r       *require.Assertions
	engine  *SyncEngine
	db      *index.DB
	rootA   string
	rootB   string
	svc     *SyncService
	created map[string]bool
}

func newSyncE2E(t *testing.T) *syncE2E {
	t.Helper()
	db, err := index.Open(filepath.Join(t.TempDir(), "test.db"), slog.New(slog.NewTextHandler(io.Discard, nil)))
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })

	registry := provider.NewRegistry()
	rootA := t.TempDir()
	rootB := t.TempDir()
	logOut := io.Discard
	if os.Getenv("SYNC_E2E_VERBOSE") == "1" {
		logOut = os.Stdout
	}
	logger := slog.New(slog.NewTextHandler(logOut, &slog.HandlerOptions{Level: slog.LevelDebug}))
	registry.Register("LA", localfs.New("LA", rootA))
	registry.Register("LB", localfs.New("LB", rootB))
	require.NoError(t, db.UpsertLocation(context.Background(), index.Location{ID: "LA", Kind: provider.KindLocalFS, Name: "A", Root: rootA}))
	require.NoError(t, db.UpsertLocation(context.Background(), index.Location{ID: "LB", Kind: provider.KindLocalFS, Name: "B", Root: rootB}))

	transfers := NewTransferService(db, registry, logger, func() TransferSettings {
		return TransferSettings{ConcurrentTransfers: 2, BandwidthLimitMbps: 0}
	})
	files := NewFileService(nil, db, registry, logger)
	engine := NewSyncEngine(db, registry, transfers, files, logger, nil)
	svc := NewSyncService(db, registry, logger)
	svc.SetReconcile(engine.ReconcileHook())
	return &syncE2E{t: t, r: require.New(t), engine: engine, db: db, rootA: rootA, rootB: rootB, svc: svc, created: map[string]bool{}}
}

// mustPair creates a pair row between the two roots.
func (e *syncE2E) mustPair(direction string, actions int) string {
	e.t.Helper()
	ctx := context.Background()
	id := "pair_" + direction
	err := e.db.CreateSyncPair(ctx, index.SyncPair{
		ID: id, Name: "test", LocationA: "LA", PathA: e.rootA,
		LocationB: "LB", PathB: e.rootB, Direction: direction, Actions: actions,
	})
	require.NoError(e.t, err)
	return id
}

// mustWrite plants a file under a root.
func (e *syncE2E) mustWrite(root, rel, content string) {
	e.t.Helper()
	abs := filepath.Join(root, rel)
	require.NoError(e.t, os.MkdirAll(filepath.Dir(abs), 0o755))
	require.NoError(e.t, os.WriteFile(abs, []byte(content), 0o644))
}

// mustRead returns the file content under a root.
func (e *syncE2E) mustRead(root, rel string) (string, bool) {
	b, err := os.ReadFile(filepath.Join(root, rel))
	if err != nil {
		return "", false
	}
	return string(b), true
}

// run executes one reconcile pass through the engine directly (the service's
// hook is async for SyncNow).
func (e *syncE2E) run(pairID string, opts ReconcileOpts) {
	e.t.Helper()
	opts.RefreshFirst = true
	require.NoError(e.t, e.engine.Reconcile(context.Background(), pairID, opts))
}

// status fetches the pair row.
func (e *syncE2E) status(pairID string) index.SyncPair {
	e.t.Helper()
	p, err := e.db.GetSyncPair(context.Background(), pairID)
	require.NoError(e.t, err)
	return p
}

// stateCount returns how many sync_state rows exist for the pair.
func (e *syncE2E) stateCount(pairID string) int {
	e.t.Helper()
	rows, err := e.db.SyncStateRows(context.Background(), pairID)
	require.NoError(e.t, err)
	return len(rows)
}

func TestWhenAOneWayPairBackfillsThenTheDestinationMirrorsTheSource(t *testing.T) {
	t.Parallel()
	e := newSyncE2E(t)
	e.mustWrite(e.rootA, "README.txt", "top-level")
	e.mustWrite(e.rootA, "pics/holiday.jpg", "jpeg-bytes")
	e.mustWrite(e.rootA, "deep/nested/xy.txt", "deep")

	pairID := e.mustPair(directionAToB, 0b11)
	e.run(pairID, ReconcileOpts{Backfill: true})

	got, ok := e.mustRead(e.rootB, "README.txt")
	e.r.True(ok)
	e.r.Equal("top-level", got)
	got, ok = e.mustRead(e.rootB, "pics/holiday.jpg")
	e.r.True(ok)
	e.r.Equal("jpeg-bytes", got)
	got, ok = e.mustRead(e.rootB, "deep/nested/xy.txt")
	e.r.True(ok)
	e.r.Equal("deep", got)

	e.r.GreaterOrEqual(e.stateCount(pairID), 3)
	st := e.status(pairID)
	e.r.NotEmpty(st.LastRunAt)
	e.r.Empty(st.LastError)
	e.r.Contains(st.LastCounts, "\"copied\"")
}

func TestWhenNothingChangedThenASecondPassIsANoOp(t *testing.T) {
	t.Parallel()
	e := newSyncE2E(t)
	e.mustWrite(e.rootA, "a.txt", "one")
	pairID := e.mustPair(directionAToB, 0b11)
	e.run(pairID, ReconcileOpts{Backfill: false})
	before := e.stateCount(pairID)
	e.run(pairID, ReconcileOpts{Backfill: false})
	e.r.Equal(before, e.stateCount(pairID))
	st := e.status(pairID)
	e.r.Contains(st.LastCounts, "\"copied\":0")
	e.r.Contains(st.LastCounts, "\"deleted\":0")
}

func TestWhenTheSourceFileChangesThenTheUpdatePropagates(t *testing.T) {
	t.Parallel()
	e := newSyncE2E(t)
	e.mustWrite(e.rootA, "a.txt", "v1")
	pairID := e.mustPair(directionAToB, 0b11)
	e.run(pairID, ReconcileOpts{})

	e.mustWrite(e.rootA, "a.txt", "v2-longer-body")
	e.run(pairID, ReconcileOpts{})
	got, ok := e.mustRead(e.rootB, "a.txt")
	e.r.True(ok)
	e.r.Equal("v2-longer-body", got)
}

func TestWhenDeletionsOptedInThenRemovingFromARemovesFromB(t *testing.T) {
	t.Parallel()
	e := newSyncE2E(t)
	e.mustWrite(e.rootA, "bye.txt", "temp")
	pairID := e.mustPair(directionAToB, 0b111) // includes deletes
	e.run(pairID, ReconcileOpts{})
	_, ok := e.mustRead(e.rootB, "bye.txt")
	e.r.True(ok)

	require.NoError(t, os.Remove(filepath.Join(e.rootA, "bye.txt")))
	e.run(pairID, ReconcileOpts{})
	_, ok = e.mustRead(e.rootB, "bye.txt")
	e.r.False(ok)
	e.r.Equal(0, e.stateCount(pairID), "state row for the deleted path drops too")
}

func TestWhenDeletionsStayOffThenTheDestinationKeepsStragglers(t *testing.T) {
	t.Parallel()
	e := newSyncE2E(t)
	e.mustWrite(e.rootA, "keep.txt", "safe")
	pairID := e.mustPair(directionAToB, 0b11) // default: no deletes
	e.run(pairID, ReconcileOpts{})

	require.NoError(t, os.Remove(filepath.Join(e.rootA, "keep.txt")))
	e.run(pairID, ReconcileOpts{})
	got, ok := e.mustRead(e.rootB, "keep.txt")
	e.r.True(ok, "destinations never lose files when deletions are off")
	e.r.Equal("safe", got)
}

func TestWhenSyncingBothWaysThenEachSideSeesNewFilesFromTheOther(t *testing.T) {
	t.Parallel()
	e := newSyncE2E(t)
	e.mustWrite(e.rootA, "from-a.txt", "A")
	e.mustWrite(e.rootB, "from-b.txt", "B")
	pairID := e.mustPair(directionBoth, 0b11)
	e.run(pairID, ReconcileOpts{})

	got, ok := e.mustRead(e.rootB, "from-a.txt")
	e.r.True(ok)
	e.r.Equal("A", got)
	got, ok = e.mustRead(e.rootA, "from-b.txt")
	e.r.True(ok)
	e.r.Equal("B", got)
	e.r.GreaterOrEqual(e.stateCount(pairID), 2)
}

func TestWhenAPairIsPausedThenReconcileIsQuiet(t *testing.T) {
	t.Parallel()
	e := newSyncE2E(t)
	e.mustWrite(e.rootA, "stay.txt", "paused")
	pairID := e.mustPair(directionAToB, 0b11)
	pair, err := e.db.GetSyncPair(context.Background(), pairID)
	require.NoError(t, err)
	pair.Paused = true
	require.NoError(t, e.db.UpdateSyncPair(context.Background(), pair))

	require.NoError(t, e.engine.Reconcile(context.Background(), pairID, ReconcileOpts{RefreshFirst: true}))
	_, ok := e.mustRead(e.rootB, "stay.txt")
	e.r.False(ok)
}

func TestWhenSyncNowRunsThroughTheService(t *testing.T) {
	t.Parallel()
	e := newSyncE2E(t)
	e.mustWrite(e.rootA, "via-ui.txt", "hello")
	pairID := e.mustPair(directionAToB, 0b11)

	require.NoError(t, e.svc.SyncNow(context.Background(), pairID))
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if _, ok := e.mustRead(e.rootB, "via-ui.txt"); ok {
			return
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatal("sync now never produced the file at B")
}

// evRec captures emitted engine events in order.
type evRec struct {
	events []map[string]any
	names  []string
}

func (r *evRec) record(name string, payload map[string]any) {
	r.names = append(r.names, name)
	r.events = append(r.events, payload)
}

func TestWhenAPassRunsThenProgressEventsBracketItWithLiveCounts(t *testing.T) {
	t.Parallel()
	e := newSyncE2E(t)
	rec := &evRec{}
	e.engine.emit = rec.record
	e.mustWrite(e.rootA, "a.txt", "one")
	e.mustWrite(e.rootA, "b.txt", "two")
	pairID := e.mustPair(directionAToB, 0b11)

	e.run(pairID, ReconcileOpts{Backfill: true})

	var progress []map[string]any
	for i, name := range rec.names {
		if name == "sync:progress" {
			progress = append(progress, rec.events[i])
		}
	}
	e.r.GreaterOrEqual(len(progress), 2, "at least a start and a finish event")
	first := progress[0]
	e.r.Equal("syncing", first["state"])
	e.r.Equal(2, first["filesTotal"])
	last := progress[len(progress)-1]
	e.r.Equal("idle", last["state"])
	e.r.GreaterOrEqual(last["copied"], 2)
	e.r.Equal(pairID, last["pairId"])
	e.r.NotEmpty(last["name"])

	for _, p := range progress[:len(progress)-1] {
		e.r.Equal("syncing", p["state"])
		e.r.LessOrEqual(p["filesDone"].(int), p["filesTotal"].(int))
	}
}
