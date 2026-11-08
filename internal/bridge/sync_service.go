// SyncService is the bridge surface for sync policies: pair CRUD, pause,
// manual SyncNow, and (wired by the engine) periodic reconcile orchestration.
package bridge

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/google/uuid"

	"github.com/radosny/airdrive/internal/entities"
	"github.com/radosny/airdrive/internal/index"
	"github.com/radosny/airdrive/internal/provider"
)

// Sync direction values stored in sync_pairs.direction.
const (
	directionAToB = "a_to_b"
	directionBToA = "b_to_a"
	directionBoth = "both"
)

// SyncService manages sync pairs.
type SyncService struct {
	db        *index.DB
	registry  *provider.Registry
	logger    *slog.Logger
	reconcile func(ctx context.Context, pairID string, opts ReconcileOpts) error
}

// NewSyncService creates the service; the engine wires reconcile separately.
func NewSyncService(db *index.DB, registry *provider.Registry, logger *slog.Logger) *SyncService {
	return &SyncService{
		db:       db,
		registry: registry,
		logger:   logger,
		reconcile: func(context.Context, string, ReconcileOpts) error {
			return fmt.Errorf("sync engine not started")
		},
	}
}

// CreateSyncPair validates and persists a pair; empty direction/actions get
// the defaults (a_to_b, additions+updates).
func (s *SyncService) CreateSyncPair(ctx context.Context, in entities.SyncPair) (*entities.SyncPair, error) {
	if in.LocationA == "" || in.LocationB == "" {
		return nil, fmt.Errorf("pick two storages to sync")
	}
	if in.LocationA == in.LocationB {
		return nil, fmt.Errorf("pick two different storages: a storage cannot sync with itself")
	}
	nameA, nameB, err := s.locationNames(ctx, in.LocationA, in.LocationB)
	if err != nil {
		return nil, err
	}

	row := entities.FromEntitySyncPair(in)
	row.ID = "pair_" + uuid.NewString()
	if row.Direction == "" {
		row.Direction = directionAToB
	}
	switch row.Direction {
	case directionAToB, directionBToA, directionBoth:
	default:
		return nil, fmt.Errorf("unknown sync direction %q", row.Direction)
	}
	row.Actions &= 0b111
	if row.Actions == 0 {
		row.Actions = 0b11 // additions + updates; deletions stay opt-in
	}
	if row.Name == "" {
		arrow := "→"
		switch row.Direction {
		case directionBoth:
			arrow = "⇄"
		case directionBToA:
			arrow = "←"
		}
		row.Name = nameA + " " + arrow + " " + nameB
	}

	if err := s.db.CreateSyncPair(ctx, row); err != nil {
		return nil, fmt.Errorf("create sync pair: %w", err)
	}
	if in.Backfill {
		id := row.ID
		go func() { //nolint:gosec // the initial backfill outlives this Wails request context on purpose
			if err := s.reconcile(context.Background(), id, ReconcileOpts{Backfill: true, RefreshFirst: true}); err != nil {
				s.logger.Warn("sync pair: initial backfill", "id", id, "err", err)
			}
		}()
	}
	created, err := s.db.GetSyncPair(ctx, row.ID)
	if err != nil {
		return nil, fmt.Errorf("read created sync pair: %w", err)
	}
	out := entities.ToEntitySyncPair(created)
	return &out, nil
}

// ListSyncPairs returns every pair as entities.
func (s *SyncService) ListSyncPairs(ctx context.Context) ([]entities.SyncPair, error) {
	rows, err := s.db.ListSyncPairs(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]entities.SyncPair, 0, len(rows))
	for _, row := range rows {
		out = append(out, entities.ToEntitySyncPair(row))
	}
	return out, nil
}

// PauseSync flips the paused flag; paused pairs are skipped by scheduling.
func (s *SyncService) PauseSync(ctx context.Context, id string, paused bool) error {
	pair, err := s.db.GetSyncPair(ctx, id)
	if err != nil {
		return fmt.Errorf("pause sync: %w", err)
	}
	pair.Paused = paused
	return s.db.UpdateSyncPair(ctx, pair)
}

// DeleteSyncPair removes a pair and its recorded state.
func (s *SyncService) DeleteSyncPair(ctx context.Context, id string) error {
	return s.db.DeleteSyncPair(ctx, id)
}

// SyncNow runs one immediate reconcile for the pair, scheduling aside.
func (s *SyncService) SyncNow(ctx context.Context, id string) error {
	return s.reconcile(ctx, id, ReconcileOpts{RefreshFirst: true})
}

// SetReconcile installs the engine's hook once the app is running.
func (s *SyncService) SetReconcile(fn func(ctx context.Context, pairID string, opts ReconcileOpts) error) {
	s.reconcile = fn
}

// locationNames resolves both ids and errors when either storage is unknown.
func (s *SyncService) locationNames(ctx context.Context, a, b string) (string, string, error) {
	locs, err := s.db.AllLocations(ctx)
	if err != nil {
		return "", "", fmt.Errorf("list locations: %w", err)
	}
	names := map[string]string{}
	for _, l := range locs {
		names[l.ID] = l.Name
	}
	nameA, okA := names[a]
	nameB, okB := names[b]
	if !okA || !okB {
		return "", "", fmt.Errorf("pick two existing storages")
	}
	return nameA, nameB, nil
}
