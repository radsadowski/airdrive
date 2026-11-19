package index

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/radosny/airdrive/internal/provider"
)

func TestWhenADirectoryIsDeletedThenItsWholeSubtreeIsPurged(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	db := openTestDB(t)
	ctx := context.Background()

	for _, id := range []string{"L1", "L2"} {
		r.NoError(db.UpsertLocation(ctx, Location{ID: id, Kind: provider.KindLocalFS, Name: id, Root: "/tmp"}))
	}

	seedTree := func(id, parent, locationID string, kind provider.FileKind) {
		r.NoError(db.UpsertFile(ctx, provider.FileInfo{
			ID:         id,
			LocationID: locationID,
			ParentID:   parent,
			Name:       id,
			Kind:       kind,
			MIME:       "text/plain",
		}))
	}

	seedTree("d1", "", "L1", provider.FileKindDir)
	seedTree("d1/a", "d1", "L1", provider.FileKindFile)
	seedTree("d1/sub", "d1", "L1", provider.FileKindDir)
	seedTree("d1/sub/b", "d1/sub", "L1", provider.FileKindFile)
	seedTree("keep", "", "L1", provider.FileKindFile)
	seedTree("d1", "", "L2", provider.FileKindDir)

	r.NoError(db.DeleteTree(ctx, "L1", "d1"))

	for _, gone := range []string{"d1", "d1/a", "d1/sub", "d1/sub/b"} {
		_, err := db.FileByID(ctx, "L1", gone)
		r.Error(err, "%s must be gone", gone)
	}
	_, err := db.FileByID(ctx, "L1", "keep")
	r.NoError(err, "siblings of the deleted directory stay")
	_, err = db.FileByID(ctx, "L2", "d1")
	r.NoError(err, "the same id in another location stays")
}

func TestWhenAFileIsDeletedThenOnlyThatRowIsRemoved(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	db := openTestDB(t)
	ctx := context.Background()
	r.NoError(db.UpsertLocation(ctx, Location{ID: "L1", Kind: provider.KindLocalFS, Name: "L1", Root: "/tmp"}))

	seedTree := func(id, parent string) {
		r.NoError(db.UpsertFile(ctx, provider.FileInfo{
			ID:         id,
			LocationID: "L1",
			ParentID:   parent,
			Name:       id,
			Kind:       provider.FileKindFile,
			MIME:       "text/plain",
		}))
	}
	seedTree("a", "")
	seedTree("b", "")

	r.NoError(db.DeleteTree(ctx, "L1", "a"))

	_, err := db.FileByID(ctx, "L1", "a")
	r.Error(err)
	_, err = db.FileByID(ctx, "L1", "b")
	r.NoError(err)
}
