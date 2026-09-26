package index

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/radosny/airdrive/internal/provider"
)

func openTestDB(t *testing.T) *DB {
	t.Helper()
	db, err := Open(filepath.Join(t.TempDir(), "test.db"), slog.New(slog.NewTextHandler(io.Discard, nil)))
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })
	return db
}

func seedLocation(t *testing.T, db *DB, id, config string) {
	t.Helper()
	err := db.UpsertLocation(context.Background(), Location{
		ID:     id,
		Kind:   provider.KindGDrive,
		Name:   "test",
		Root:   "My Drive",
		Config: config,
	})
	require.NoError(t, err)
}

func seedFile(t *testing.T, db *DB, id, locationID string) {
	t.Helper()
	err := db.UpsertFile(context.Background(), provider.FileInfo{
		ID:         id,
		LocationID: locationID,
		Name:       id + ".txt",
		Kind:       provider.FileKindFile,
		MIME:       "text/plain",
	})
	require.NoError(t, err)
}

func TestWhenACursorIsStoredThenItRoundTrips(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	db := openTestDB(t)
	ctx := context.Background()
	seedLocation(t, db, "L1", "{}")

	err := db.SetSyncCursor(ctx, "L1", "TOKEN123")
	r.NoError(err)

	got, err := db.GetSyncCursor(ctx, "L1")
	r.NoError(err)
	r.Equal("TOKEN123", got)

	r.NoError(db.SetSyncCursor(ctx, "L1", "TOKEN456"))
	got, err = db.GetSyncCursor(ctx, "L1")
	r.NoError(err)
	r.Equal("TOKEN456", got)
}

func TestWhenNoCursorWasEverStoredThenItIsEmpty(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	db := openTestDB(t)
	ctx := context.Background()
	seedLocation(t, db, "L1", "{}")

	got, err := db.GetSyncCursor(ctx, "L1")
	r.NoError(err)
	r.Empty(got)
}

func TestWhenTheCursorIsSetThenSiblingConfigKeysSurvive(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	db := openTestDB(t)
	ctx := context.Background()
	seedLocation(t, db, "L1", `{"otherKey":"keep-me"}`)

	r.NoError(db.SetSyncCursor(ctx, "L1", "TOK"))

	loc, err := db.LocationByID(ctx, "L1")
	r.NoError(err)

	var cfg map[string]any
	r.NoError(json.Unmarshal([]byte(loc.Config), &cfg))
	r.Equal("TOK", cfg["syncCursor"])
	r.Equal("keep-me", cfg["otherKey"])
}

func TestWhenTheLocationDoesNotExistThenCursorAccessErrors(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	db := openTestDB(t)
	ctx := context.Background()

	_, err := db.GetSyncCursor(ctx, "NOPE")
	r.Error(err)

	r.Error(db.SetSyncCursor(ctx, "NOPE", "TOK"))
}

func TestWhenAGdriveRootIDIsStoredThenItRoundTrips(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	db := openTestDB(t)
	ctx := context.Background()
	seedLocation(t, db, "L1", "{}")

	err := db.SetGdriveRootID(ctx, "L1", "PICKEDFOLDER")
	r.NoError(err)

	got, err := db.GetGdriveRootID(ctx, "L1")
	r.NoError(err)
	r.Equal("PICKEDFOLDER", got)
}

func TestWhenNoGdriveRootIDWasStoredThenItIsEmpty(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	db := openTestDB(t)
	ctx := context.Background()
	seedLocation(t, db, "L1", `{"syncCursor":"KEEP"}`)

	got, err := db.GetGdriveRootID(ctx, "L1")
	r.NoError(err)
	r.Empty(got)

	r.NoError(db.SetGdriveRootID(ctx, "L1", "R1"))
	cursor, err := db.GetSyncCursor(ctx, "L1")
	r.NoError(err)
	r.Equal("KEEP", cursor, "setting the root ID must preserve sibling config keys")
}

func TestWhenReconcileRunsThenOnlyUnseenFilesOfThatLocationAreRemoved(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	db := openTestDB(t)
	ctx := context.Background()
	seedLocation(t, db, "L1", "{}")
	seedLocation(t, db, "L2", "{}")
	seedFile(t, db, "A", "L1")
	seedFile(t, db, "B", "L1")
	seedFile(t, db, "C", "L1")
	seedFile(t, db, "OTHER", "L2")

	removed, err := db.DeleteFilesNotIn(ctx, "L1", []string{"A"})
	r.NoError(err)
	r.Equal(int64(2), removed)

	_, err = db.FileByID(ctx, "L1", "A")
	r.NoError(err, "kept file survives")
	_, err = db.FileByID(ctx, "L1", "B")
	r.Error(err, "unseen file removed")
	_, err = db.FileByID(ctx, "L1", "C")
	r.Error(err, "unseen file removed")

	other, err := db.FileByID(ctx, "L2", "OTHER")
	r.NoError(err)
	r.Equal("L2", other.LocationID, "other locations are untouched")
}

func TestWhenReconcileKeepsEverythingThenNothingIsRemoved(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	db := openTestDB(t)
	ctx := context.Background()
	seedLocation(t, db, "L1", "{}")
	seedFile(t, db, "A", "L1")
	seedFile(t, db, "B", "L1")

	removed, err := db.DeleteFilesNotIn(ctx, "L1", []string{"A", "B"})
	r.NoError(err)
	r.Equal(int64(0), removed)
}

func TestWhenTwoLocationsShareAFileIDThenBothRowsSurvive(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	db := openTestDB(t)
	ctx := context.Background()
	seedLocation(t, db, "L1", "{}")
	seedLocation(t, db, "L2", "{}")

	r.NoError(db.UpsertFile(ctx, provider.FileInfo{ID: "backup/photo.jpg", LocationID: "L1", ParentID: "backup/", Name: "photo.jpg", Kind: provider.FileKindFile}))
	r.NoError(db.UpsertFile(ctx, provider.FileInfo{ID: "backup/photo.jpg", LocationID: "L2", ParentID: "", Name: "photo.jpg", Kind: provider.FileKindFile}))

	a, err := db.FileByID(ctx, "L1", "backup/photo.jpg")
	r.NoError(err)
	r.Equal("L1", a.LocationID)
	r.Equal("backup/", a.ParentID, "L1's row must keep its own parent")

	b, err := db.FileByID(ctx, "L2", "backup/photo.jpg")
	r.NoError(err)
	r.Equal("L2", b.LocationID)
	r.Equal("", b.ParentID)

	r.NoError(db.DeleteFile(ctx, "L1", "backup/photo.jpg"))
	_, err = db.FileByID(ctx, "L1", "backup/photo.jpg")
	r.Error(err, "delete is scoped to the location")
	_, err = db.FileByID(ctx, "L2", "backup/photo.jpg")
	r.NoError(err, "the other location's row survives")
}
