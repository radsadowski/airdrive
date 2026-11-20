package index

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/radosny/airdrive/internal/provider"
)

func TestWhenAPathIsRequestedThenItIsBuiltFromIndexedNames(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	db := openTestDB(t)
	ctx := context.Background()
	r.NoError(db.UpsertLocation(ctx, Location{ID: "L1", Kind: provider.KindLocalFS, Name: "L1", Root: "/tmp"}))

	for _, f := range []provider.FileInfo{
		{ID: "a", LocationID: "L1", Name: "a", Kind: provider.FileKindDir},
		{ID: "a/b", LocationID: "L1", ParentID: "a", Name: "b", Kind: provider.FileKindDir},
		{ID: "a/b/c.txt", LocationID: "L1", ParentID: "a/b", Name: "c.txt", Kind: provider.FileKindFile},
	} {
		r.NoError(db.UpsertFile(ctx, f))
	}

	path, err := db.DisplayPath(ctx, "L1", "a/b/c.txt")
	r.NoError(err)
	r.Equal("a/b/c.txt", path)
}

func TestWhenAFileSitsAtTheRootThenItsPathIsItsName(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	db := openTestDB(t)
	ctx := context.Background()
	r.NoError(db.UpsertLocation(ctx, Location{ID: "L1", Kind: provider.KindS3, Name: "L1", Root: "bucket"}))
	r.NoError(db.UpsertFile(ctx, provider.FileInfo{ID: "top.txt", LocationID: "L1", Name: "top.txt", Kind: provider.FileKindFile}))

	path, err := db.DisplayPath(ctx, "L1", "top.txt")
	r.NoError(err)
	r.Equal("top.txt", path)
}
