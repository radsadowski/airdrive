package bridge

import (
	"context"
	"io"
	"log/slog"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/radosny/airdrive/internal/index"
	"github.com/radosny/airdrive/internal/provider"
	mockprovider "github.com/radosny/airdrive/internal/provider/mock"
)

func TestWhenADirectoryIsDeletedThenTheIndexDropsItsDescendants(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	ctrl := gomock.NewController(t)
	db, err := index.Open(filepath.Join(t.TempDir(), "test.db"), slog.New(slog.NewTextHandler(io.Discard, nil)))
	r.NoError(err)
	t.Cleanup(func() { db.Close() })

	registry := provider.NewRegistry()
	p := mockprovider.NewMockStorageProvider(ctrl)
	registry.Register("L1", p)

	ctx := context.Background()
	r.NoError(db.UpsertLocation(ctx, index.Location{ID: "L1", Kind: provider.KindLocalFS, Name: "L1", Root: "/tmp"}))
	seedTreeFile := func(id, parent string, kind provider.FileKind) {
		r.NoError(db.UpsertFile(ctx, provider.FileInfo{
			ID:         id,
			LocationID: "L1",
			ParentID:   parent,
			Name:       id,
			Kind:       kind,
			MIME:       "text/plain",
		}))
	}
	seedTreeFile("d1", "", provider.FileKindDir)
	seedTreeFile("d1/a", "d1", provider.FileKindFile)
	seedTreeFile("keep", "", provider.FileKindFile)

	p.EXPECT().Delete(gomock.Any(), "L1", []string{"d1"}).Return(nil)

	svc := NewFileService(nil, db, registry, slog.New(slog.NewTextHandler(io.Discard, nil)))
	r.NoError(svc.DeleteFiles("L1", []string{"d1"}))

	for _, gone := range []string{"d1", "d1/a"} {
		_, byIDErr := db.FileByID(ctx, "L1", gone)
		r.Error(byIDErr, "%s must be gone from the index", gone)
	}
	_, err = db.FileByID(ctx, "L1", "keep")
	r.NoError(err)
}
