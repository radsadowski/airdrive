package bridge

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/radosny/airdrive/internal/index"
)

// SyncDebounceDelay is how long after the last local change a pair sync
// fires. Bursts of edits collapse into one pass.
const SyncDebounceDelay = 60 * time.Second

// SyncSweepInterval is the safety-net cadence for pairs that events missed.
const SyncSweepInterval = time.Hour

// SyncScheduler turns "something changed" signals and the clock into
// reconcile calls on the engine. Debounce keys per location: every new
// event on that location resets its timer.
type SyncScheduler struct {
	db        *index.DB
	reconHook func(ctx context.Context, pairID string, opts ReconcileOpts) error
	logger    *slog.Logger

	after func(time.Duration) <-chan time.Time
	tick  func(time.Duration) *time.Ticker

	ctx    context.Context //nolint:containedctx // the scheduler's lifetime is the app's; cancel+ctx travel together on purpose
	cancel context.CancelFunc

	mu      sync.Mutex
	pending map[string]map[string]bool // locationID -> pairIDs
	gen     map[string]int             // locationID -> monotonically increasing event count
	started bool
	done    chan struct{}
}

// NewSyncScheduler wires the scheduler; after/tick injectable for tests.
func NewSyncScheduler(db *index.DB, hook func(ctx context.Context, pairID string, opts ReconcileOpts) error, logger *slog.Logger) *SyncScheduler {
	ctx, cancel := context.WithCancel(context.Background())
	return &SyncScheduler{
		db:        db,
		reconHook: hook,
		logger:    logger,
		after:     time.After,
		tick:      time.NewTicker,
		ctx:       ctx,
		cancel:    cancel,
		pending:   map[string]map[string]bool{},
		gen:       map[string]int{},
		done:      make(chan struct{}),
	}
}

// Start launches the hourly sweep loop.
func (s *SyncScheduler) Start() {
	s.mu.Lock()
	s.started = true
	s.mu.Unlock()
	go s.sweepLoop()
}

// Stop cancels background work and waits for the sweep loop to exit.
func (s *SyncScheduler) Stop() {
	s.cancel()
	s.mu.Lock()
	started := s.started
	s.started = false
	s.mu.Unlock()
	if started {
		<-s.done
	}
}

// NotifyFileEvent marks every pair touching a location dirty and resets
// that location's debounce timer.
func (s *SyncScheduler) NotifyFileEvent(locationID string) {
	pairs, err := s.pairsFor(locationID)
	if err != nil || len(pairs) == 0 {
		return
	}
	s.mu.Lock()
	if s.pending[locationID] == nil {
		s.pending[locationID] = map[string]bool{}
	}
	for _, id := range pairs {
		s.pending[locationID][id] = true
	}
	s.gen[locationID]++
	myGen := s.gen[locationID]
	s.mu.Unlock()

	timer := s.after(SyncDebounceDelay)
	go func() {
		select {
		case <-s.ctx.Done():
		case <-timer:
			s.flushIfFresh(locationID, myGen)
		}
	}()
}

// flushIfFresh reconciles the location's pending pairs only when no
// newer event arrived since the timer was armed and the scheduler still runs.
func (s *SyncScheduler) flushIfFresh(locationID string, myGen int) {
	if s.ctx.Err() != nil {
		return
	}
	s.mu.Lock()
	if s.gen[locationID] != myGen {
		s.mu.Unlock()
		return
	}
	ids := s.pending[locationID]
	delete(s.pending, locationID)
	s.mu.Unlock()
	for id := range ids {
		if err := s.reconHook(s.ctx, id, ReconcileOpts{}); err != nil {
			s.logger.Warn("sync scheduler: reconcile", "pair", id, "err", err)
		}
	}
}

// sweepLoop ticks hourly and reconciles every pair not synced within the
// interval.
func (s *SyncScheduler) sweepLoop() {
	t := s.tick(SyncSweepInterval)
	defer t.Stop()
	for {
		select {
		case <-s.ctx.Done():
			close(s.done)
			return
		case <-t.C:
			s.sweep()
		}
	}
}

// sweep reconciles pairs whose last run sits older than the interval.
func (s *SyncScheduler) sweep() {
	pairs, err := s.db.ListSyncPairs(context.Background())
	if err != nil {
		s.logger.Warn("sync scheduler: list pairs", "err", err)
		return
	}
	for _, p := range pairs {
		if p.Paused || fresh(p.LastRunAt, SyncSweepInterval) {
			continue
		}
		if err := s.reconHook(s.ctx, p.ID, ReconcileOpts{RefreshFirst: true}); err != nil {
			s.logger.Warn("sync scheduler: sweep", "pair", p.ID, "err", err)
		}
	}
}

// fresh reports whether an RFC3339 stamp is younger than maxAge.
func fresh(stamp string, maxAge time.Duration) bool {
	if stamp == "" {
		return false
	}
	t, err := time.Parse(time.RFC3339, stamp)
	if err != nil {
		return false
	}
	return time.Since(t) < maxAge
}

// pairsFor lists pair ids that include a location.
func (s *SyncScheduler) pairsFor(locationID string) ([]string, error) {
	pairs, err := s.db.ListSyncPairs(context.Background())
	if err != nil {
		return nil, err
	}
	var out []string
	for _, p := range pairs {
		if p.LocationA == locationID || p.LocationB == locationID {
			out = append(out, p.ID)
		}
	}
	return out, nil
}
