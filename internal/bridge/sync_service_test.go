package bridge

import (
	"context"
	"io"
	"log/slog"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/radosny/airdrive/internal/entities"
	"github.com/radosny/airdrive/internal/index"
	"github.com/radosny/airdrive/internal/provider"
)

func openSyncService(t *testing.T) *SyncService {
	t.Helper()
	db, err := index.Open(filepath.Join(t.TempDir(), "test.db"), slog.New(slog.NewTextHandler(io.Discard, nil)))
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })
	svc := NewSyncService(db, provider.NewRegistry(), slog.New(slog.NewTextHandler(io.Discard, nil)))
	seedLoc := func(id, name string) {
		require.NoError(t, db.UpsertLocation(context.Background(), index.Location{ID: id, Kind: provider.KindLocalFS, Name: name, Root: "/x"}))
	}
	seedLoc("L1", "Local: pics")
	seedLoc("L2", "R2: content")
	return svc
}

func TestWhenThePairEndsAreTheSameStorageThenCreateFails(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	svc := openSyncService(t)

	_, err := svc.CreateSyncPair(context.Background(), entities.SyncPair{LocationA: "L1", LocationB: "L1"})
	r.Error(err)
	r.Contains(err.Error(), "different storages")
}

func TestWhenAnEndLocationIsUnknownThenCreateFails(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	svc := openSyncService(t)

	_, err := svc.CreateSyncPair(context.Background(), entities.SyncPair{LocationA: "L1", LocationB: "NOPE"})
	r.Error(err)
	r.Contains(err.Error(), "existing storages")
}

func TestWhenDirectionAndActionsAreEmptyThenDefaultsApply(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	svc := openSyncService(t)

	pair, err := svc.CreateSyncPair(context.Background(), entities.SyncPair{LocationA: "L1", LocationB: "L2", PathA: "/pics", PathB: "backups/"})
	r.NoError(err)
	r.Equal("a_to_b", pair.Direction)
	r.Equal(0b11, pair.Actions, "additions+updates on, deletions explicitly off")
	r.Equal("Local: pics → R2: content", pair.Name)
	r.NotEmpty(pair.ID)
	r.NotEmpty(pair.CreatedAt)
}

func TestWhenBothWaysThenTheNameUsesTheMirrorGlyph(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	svc := openSyncService(t)

	pair, err := svc.CreateSyncPair(context.Background(), entities.SyncPair{LocationA: "L1", LocationB: "L2", Direction: "both", Actions: 7})
	r.NoError(err)
	r.Equal("Local: pics ⇄ R2: content", pair.Name)
}

func TestWhenAPairIsPausedThenItRoundTrips(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	svc := openSyncService(t)

	pair, err := svc.CreateSyncPair(context.Background(), entities.SyncPair{LocationA: "L1", LocationB: "L2"})
	r.NoError(err)
	r.NoError(svc.PauseSync(context.Background(), pair.ID, true))

	pairs, err := svc.ListSyncPairs(context.Background())
	r.NoError(err)
	r.Len(pairs, 1)
	r.True(pairs[0].Paused)
}

func TestWhenAPairIsDeletedThenListingIsEmpty(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	svc := openSyncService(t)

	pair, err := svc.CreateSyncPair(context.Background(), entities.SyncPair{LocationA: "L1", LocationB: "L2"})
	r.NoError(err)
	r.NoError(svc.DeleteSyncPair(context.Background(), pair.ID))

	pairs, err := svc.ListSyncPairs(context.Background())
	r.NoError(err)
	r.Empty(pairs)
}

func TestWhenUpdatingAPairThenItsPolicyFieldsApply(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	svc := openSyncService(t)
	ctx := context.Background()
	created, err := svc.CreateSyncPair(ctx, entities.SyncPair{LocationA: "L1", LocationB: "L2"})
	r.NoError(err)

	updated, err := svc.UpdateSyncPair(ctx, entities.SyncPair{
		ID: created.ID, Name: "nightly backup", LocationA: "L1", LocationB: "L2",
		Direction: "both", Actions: 0b111,
	})
	r.NoError(err)
	r.Equal("nightly backup", updated.Name)
	r.Equal("both", updated.Direction)
	r.Equal(0b111, updated.Actions)

	gone, err := svc.db.GetSyncPair(ctx, created.ID)
	r.NoError(err)
	r.Equal("both", gone.Direction)
	r.Equal("nightly backup", gone.Name)
}

func TestWhenUpdatingWithAnUnknownDirectionThenTheServiceRejects(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	svc := openSyncService(t)
	ctx := context.Background()
	created, err := svc.CreateSyncPair(ctx, entities.SyncPair{LocationA: "L1", LocationB: "L2"})
	r.NoError(err)

	_, err = svc.UpdateSyncPair(ctx, entities.SyncPair{
		ID: created.ID, LocationA: "L1", LocationB: "L2", Direction: "sideways",
	})
	r.Error(err)
}

func TestWhenUpdatingAMissingPairThenTheServiceErrors(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	svc := openSyncService(t)
	_, err := svc.UpdateSyncPair(context.Background(), entities.SyncPair{
		ID: "pair_ghost", LocationA: "L1", LocationB: "L2", Direction: "a_to_b",
	})
	r.Error(err)
}
