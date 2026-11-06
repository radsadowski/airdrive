package index

import (
	"context"
	"database/sql"
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

func TestWhenAFileWithAnMD5IsStoredThenItRoundTrips(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	db := openTestDB(t)
	ctx := context.Background()
	seedLocation(t, db, "L1", "{}")

	err := db.UpsertFile(ctx, provider.FileInfo{
		ID: "F1", LocationID: "L1", Name: "a.png",
		Kind: provider.FileKindFile, Etag: "v7", MD5: "d41d8cd98f00b204e9800998ecf8427e",
	})
	r.NoError(err)

	got, err := db.FileByID(ctx, "L1", "F1")
	r.NoError(err)
	r.Equal("d41d8cd98f00b204e9800998ecf8427e", got.MD5)
}

func TestWhenTheDatabasePredatesTheMD5ColumnThenOpenRebuildsIt(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	path := filepath.Join(t.TempDir(), "test.db")

	legacy, err := sql.Open("sqlite", path)
	r.NoError(err)
	_, err = legacy.Exec(`
		CREATE TABLE goose_db_version (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			version_id INTEGER NOT NULL,
			is_applied INTEGER NOT NULL,
			tstamp TIMESTAMP DEFAULT CURRENT_TIMESTAMP
		);
		INSERT INTO goose_db_version (version_id, is_applied) VALUES (1, 1);
		CREATE TABLE locations (
			id TEXT PRIMARY KEY, kind TEXT NOT NULL, name TEXT NOT NULL,
			root TEXT NOT NULL, config TEXT NOT NULL DEFAULT '{}',
			created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ','now'))
		);
		CREATE TABLE files (
			id TEXT NOT NULL, location_id TEXT NOT NULL, parent_id TEXT NOT NULL DEFAULT '',
			name TEXT NOT NULL, kind TEXT NOT NULL, size INTEGER NOT NULL DEFAULT 0,
			mime TEXT NOT NULL DEFAULT '', etag TEXT NOT NULL DEFAULT '',
			preview_kind TEXT NOT NULL DEFAULT '', modified_at TEXT NOT NULL,
			indexed_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ','now')),
			PRIMARY KEY (location_id, id)
		);
	`)
	r.NoError(err)
	r.NoError(legacy.Close())

	db, err := Open(path, nil)
	r.NoError(err)
	t.Cleanup(func() { db.Close() })

	seedLocation(t, db, "L1", "{}")
	err = db.UpsertFile(context.Background(), provider.FileInfo{
		ID: "F1", LocationID: "L1", Name: "a.png",
		Kind: provider.FileKindFile, MD5: "d41d8cd98f00b204e9800998ecf8427e",
	})
	r.NoError(err, "files.md5 must exist after Open rebuilds a legacy database")
}

func TestWhenATransferBatchIsCreatedThenJobsPersistWithIt(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	db := openTestDB(t)
	ctx := context.Background()

	batch := TransferBatch{
		ID: "B1", Action: "copy", Conflict: "smart", DeleteSource: false,
		SourceLocID: "SRC", DestLocID: "DST", DestDirID: "",
	}
	jobs := []TransferJob{
		{ID: "J1", BatchID: "B1", SourceFileID: "F1", SourceName: "a.png", SourceLocID: "SRC", DestLocID: "DST", DestPath: "a.png", BytesTotal: 100},
		{ID: "J2", BatchID: "B1", SourceFileID: "F2", SourceName: "b/c.txt", SourceLocID: "SRC", DestLocID: "DST", DestPath: "b/c.txt", BytesTotal: 200},
	}
	r.NoError(db.CreateTransferBatch(ctx, batch, jobs))

	got, err := db.JobsForBatches(ctx, []string{"B1"})
	r.NoError(err)
	r.Len(got, 2)
	r.Equal("pending", got[0].State)
	r.Equal(int64(100), got[0].BytesTotal)

	pending, err := db.NextPendingJobs(ctx, 10)
	r.NoError(err)
	r.Len(pending, 2)

	r.NoError(db.UpdateTransferJobState(ctx, "J1", "done", true, ""))
	r.NoError(db.UpdateTransferJobProgress(ctx, "J2", 150, 200))

	pending, err = db.NextPendingJobs(ctx, 10)
	r.NoError(err)
	r.Len(pending, 1, "done jobs are not pending")
	r.Equal("J2", pending[0].ID)

	got, err = db.JobsForBatches(ctx, []string{"B1"})
	r.NoError(err)
	byID := map[string]TransferJob{}
	for _, j := range got {
		byID[j.ID] = j
	}
	r.True(byID["J1"].MD5Verified)
	r.Equal(int64(150), byID["J2"].BytesDone)
}

func TestWhenBatchesAreListedThenCountsComeComputed(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	db := openTestDB(t)
	ctx := context.Background()

	r.NoError(db.CreateTransferBatch(ctx, TransferBatch{ID: "B1", Action: "copy", Conflict: "smart"}, []TransferJob{
		{ID: "J1", BatchID: "B1", SourceName: "a", SourceLocID: "S", DestLocID: "D"},
		{ID: "J2", BatchID: "B1", SourceName: "b", SourceLocID: "S", DestLocID: "D"},
		{ID: "J3", BatchID: "B1", SourceName: "c", SourceLocID: "S", DestLocID: "D"},
	}))
	r.NoError(db.UpdateTransferJobState(ctx, "J1", "done", true, ""))
	r.NoError(db.UpdateTransferJobState(ctx, "J2", "failed", false, "boom"))
	r.NoError(db.UpdateTransferJobState(ctx, "J3", "skipped", false, ""))

	batches, err := db.ListTransferBatches(ctx)
	r.NoError(err)
	r.Len(batches, 1)
	r.Equal(1, batches[0].Done)
	r.Equal(1, batches[0].Failed)
	r.Equal(1, batches[0].Skipped)
	r.Equal(3, batches[0].Total)
}

func TestWhenInterruptedTransfersAreFailedThenRunningAndPausedFlip(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	db := openTestDB(t)
	ctx := context.Background()

	r.NoError(db.CreateTransferBatch(ctx, TransferBatch{ID: "B1", Action: "copy"}, []TransferJob{
		{ID: "J1", BatchID: "B1", SourceName: "a", SourceLocID: "S", DestLocID: "D"},
		{ID: "J2", BatchID: "B1", SourceName: "b", SourceLocID: "S", DestLocID: "D"},
		{ID: "J3", BatchID: "B1", SourceName: "c", SourceLocID: "S", DestLocID: "D"},
	}))
	r.NoError(db.UpdateTransferJobState(ctx, "J1", "running", false, ""))
	r.NoError(db.UpdateTransferJobState(ctx, "J2", "paused", false, ""))
	r.NoError(db.UpdateTransferJobState(ctx, "J3", "done", true, ""))

	n, err := db.FailInterruptedTransfers(ctx, "interrupted by app restart")
	r.NoError(err)
	r.Equal(int64(2), n)

	got, err := db.JobsForBatches(ctx, []string{"B1"})
	r.NoError(err)
	for _, j := range got {
		if j.ID == "J3" {
			r.Equal("done", j.State)
		} else {
			r.Equal("failed", j.State)
			r.Contains(j.Error, "interrupted")
		}
	}
}
