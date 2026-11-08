package bridge

import (
	"context"
	"io"
	"log/slog"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/radosny/airdrive/internal/index"
	"github.com/radosny/airdrive/internal/provider"
)

// fakeClock drives scheduler timers deterministically.
type fakeClock struct {
	mu    sync.Mutex
	chans []chan time.Time
}

func (c *fakeClock) after(d time.Duration) <-chan time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	ch := make(chan time.Time, 1)
	c.chans = append(c.chans, ch)
	return ch
}

// fire releases the n-th timer armed so far.
func (c *fakeClock) fire(n int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if n < len(c.chans) {
		c.chans[n] <- time.Now()
	}
}

func (c *fakeClock) armed() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.chans)
}

// schedulerFixture isolates a scheduler around a recording hook.
type schedulerFixture struct {
	sched *SyncScheduler
	clock *fakeClock
	db    *index.DB
	mu    sync.Mutex
	calls []string
}

func newSchedulerFixture(t *testing.T) *schedulerFixture {
	t.Helper()
	db, err := index.Open(filepath.Join(t.TempDir(), "test.db"), slog.New(slog.NewTextHandler(io.Discard, nil)))
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })
	f := &schedulerFixture{clock: &fakeClock{}, db: db}
	hook := func(ctx context.Context, pairID string, opts ReconcileOpts) error {
		f.mu.Lock()
		f.calls = append(f.calls, pairID)
		f.mu.Unlock()
		return nil
	}
	f.sched = NewSyncScheduler(db, hook, slog.New(slog.NewTextHandler(io.Discard, nil)))
	f.sched.after = f.clock.after
	t.Cleanup(f.sched.Stop)
	return f
}

// seedPair plants a pair row matching LA + LB.
func (f *schedulerFixture) seedPair(t *testing.T, id, lastRunAt string, paused bool) {
	t.Helper()
	rootA := t.TempDir()
	require.NoError(t, f.db.UpsertLocation(context.Background(), index.Location{ID: "LA", Kind: provider.KindLocalFS, Name: "A", Root: rootA}))
	require.NoError(t, f.db.UpsertLocation(context.Background(), index.Location{ID: "LB", Kind: provider.KindLocalFS, Name: "B", Root: rootBFrom(t)}))
	require.NoError(t, f.db.CreateSyncPair(context.Background(), index.SyncPair{
		ID: id, Name: id, LocationA: "LA", PathA: rootA,
		LocationB: "LB", PathB: rootBFrom(t), Direction: directionAToB, Actions: 0b11,
		Paused: paused, LastRunAt: lastRunAt,
	}))
}

// rootBFrom hands a stable tmp root for the fixture.
func rootBFrom(t *testing.T) string {
	t.Helper()
	return t.TempDir()
}

func (f *schedulerFixture) recordedCalls() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string{}, f.calls...)
}

// waitForCalls polls until n calls land.
func (f *schedulerFixture) waitForCalls(t *testing.T, n int) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if len(f.recordedCalls()) >= n {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("expected %d reconcile calls, got %v", n, f.recordedCalls())
}

func TestWhenAFileEventLandsThenTheTouchingPairReconcilesAfterTheDelay(t *testing.T) {
	t.Parallel()
	f := newSchedulerFixture(t)
	f.seedPair(t, "pair_X", "", false)

	f.sched.NotifyFileEvent("LA")
	require.Equal(t, 0, len(f.recordedCalls()), "no immediate reconcile")
	f.clock.fire(0)
	f.waitForCalls(t, 1)
	require.Equal(t, []string{"pair_X"}, f.recordedCalls())
}

func TestWhenABurstOfEditsLandsThenTheyCoalesceIntoOnePass(t *testing.T) {
	t.Parallel()
	f := newSchedulerFixture(t)
	f.seedPair(t, "pair_burst", "", false)

	f.sched.NotifyFileEvent("LA")
	f.sched.NotifyFileEvent("LA")
	f.sched.NotifyFileEvent("LA")
	require.Equal(t, 3, f.clock.armed(), "each event arms a timer")
	f.clock.fire(0)
	f.clock.fire(1)
	f.clock.fire(2) // only the last generation wins
	time.Sleep(150 * time.Millisecond)
	require.Equal(t, 1, len(f.recordedCalls()), "stale timers self-cancel: calls=%v", f.recordedCalls())
}

func TestWhenTwoLocationsChangeThenEachSchedulesIndependently(t *testing.T) {
	t.Parallel()
	f := newSchedulerFixture(t)
	f.seedPair(t, "pair_two", "", false)

	f.sched.NotifyFileEvent("LA")
	f.sched.NotifyFileEvent("LB")
	f.clock.fire(0)
	f.clock.fire(1)
	f.waitForCalls(t, 2)
}

func TestWhenTheEventHitsAnUnpairedLocationThenNothingRuns(t *testing.T) {
	t.Parallel()
	f := newSchedulerFixture(t)
	f.seedPair(t, "pair_quiet", "", false)

	f.sched.NotifyFileEvent("NOPE")
	require.Equal(t, 0, f.clock.armed())
	time.Sleep(60 * time.Millisecond)
	require.Empty(t, f.recordedCalls())
}

func TestWhenTheSweepMeetsAStalePairThenItReconcilesButSkipsFreshAndPaused(t *testing.T) {
	t.Parallel()
	f := newSchedulerFixture(t)
	f.seedPair(t, "pair_stale", "", false)
	stale := "pair_fresh"
	f.seedPair(t, stale, "", true) // paused
	require.NoError(t, f.db.CreateSyncPair(context.Background(), index.SyncPair{
		ID: "pair_fresh_run", Name: "fresh", LocationA: "LA", PathA: "/x",
		LocationB: "LB", PathB: "/y", Direction: directionAToB, Actions: 0b11,
	}))
	require.NoError(t, f.db.UpdateSyncPairStatus(context.Background(), "pair_fresh_run", time.Now().UTC().Format(time.RFC3339), `{"copied":0}`, ""))

	f.sched.sweep()
	calls := f.recordedCalls()
	require.Contains(t, calls, "pair_stale")
	require.NotContains(t, calls, stale)
	require.NotContains(t, calls, "pair_fresh_run")
}

func TestWhenTheSchedulerStopsThenTheSweepLoopExitsPromptly(t *testing.T) {
	t.Parallel()
	f := newSchedulerFixture(t)
	f.sched.tick = func(d time.Duration) *time.Ticker { return time.NewTicker(5 * time.Millisecond) }
	f.sched.Start()
	var stopped atomic.Bool
	go func() {
		f.sched.Stop()
		stopped.Store(true)
	}()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if stopped.Load() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("Stop hung: sweep loop leaked")
}

func TestWhenCancelledBeforeTheTimerFiresThenTheHookNeverRuns(t *testing.T) {
	t.Parallel()
	f := newSchedulerFixture(t)
	f.seedPair(t, "pair_cancel", "", false)
	f.sched.NotifyFileEvent("LA")
	f.sched.cancel()
	f.clock.fire(0)
	time.Sleep(80 * time.Millisecond)
	require.Empty(t, f.recordedCalls())
}
