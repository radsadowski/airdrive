package bridge

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"path/filepath"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	ev "github.com/radosny/airdrive/internal/events"
	"github.com/radosny/airdrive/internal/index"
	"github.com/radosny/airdrive/internal/provider"
	mockprovider "github.com/radosny/airdrive/internal/provider/mock"
)

type opEventRecorder struct {
	mu       sync.Mutex
	payloads []map[string]any
}

func (r *opEventRecorder) record(name string, data ...any) {
	if name != ev.FileOpProgress {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	payload, _ := data[0].(map[string]any)
	r.payloads = append(r.payloads, payload)
}

func (r *opEventRecorder) snapshot() []map[string]any {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]map[string]any(nil), r.payloads...)
}

func newOpTestService(t *testing.T, ctrl *gomock.Controller) (*FileService, *mockprovider.MockStorageProvider, *opEventRecorder) {
	t.Helper()
	db, err := index.Open(filepath.Join(t.TempDir(), "test.db"), slog.New(slog.NewTextHandler(io.Discard, nil)))
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })
	require.NoError(t, db.UpsertLocation(context.Background(), index.Location{
		ID: "L1", Kind: provider.KindLocalFS, Name: "L1", Root: t.TempDir(),
	}))

	registry := provider.NewRegistry()
	p := mockprovider.NewMockStorageProvider(ctrl)
	registry.Register("L1", p)

	rec := &opEventRecorder{}
	svc := NewFileService(nil, db, registry, slog.New(slog.NewTextHandler(io.Discard, nil)))
	svc.emit = rec.record
	return svc, p, rec
}

func seedOpFile(t *testing.T, svc *FileService, id string) {
	t.Helper()
	require.NoError(t, svc.db.UpsertFile(context.Background(), provider.FileInfo{
		ID:         id,
		LocationID: "L1",
		Name:       id,
		Kind:       provider.FileKindFile,
		MIME:       "text/plain",
	}))
}

func TestWhenFilesAreDeletedThenProgressTicksForEveryItem(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	ctrl := gomock.NewController(t)
	svc, p, rec := newOpTestService(t, ctrl)

	for _, id := range []string{"a", "b"} {
		seedOpFile(t, svc, id)
		p.EXPECT().Delete(gomock.Any(), "L1", []string{id}).Return(nil)
	}

	r.NoError(svc.DeleteFiles("L1", []string{"a", "b"}))

	events := rec.snapshot()
	r.Len(events, 4, "start, one tick per item, done")
	r.Equal(opDelete, events[0]["op"])
	r.Equal(0, events[0]["done"])
	r.Equal(2, events[3]["total"])
	r.Equal(2, events[3]["done"])
	r.Equal(opDone, events[3]["state"])
}

func TestWhenAFilesWithinAMoveFailsThenProgressEndsInError(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	ctrl := gomock.NewController(t)
	svc, p, rec := newOpTestService(t, ctrl)

	seedOpFile(t, svc, "a")
	p.EXPECT().MoveWithin(gomock.Any(), "L1", []string{"a"}, "").Return(errors.New("disk on fire"))

	err := svc.MoveWithin("L1", []string{"a"}, "")
	r.ErrorContains(err, "disk on fire")

	events := rec.snapshot()
	r.Len(events, 2, "start plus the failure")
	r.Equal(opMove, events[1]["op"])
	r.Equal(opError, events[1]["state"])
	r.Equal("disk on fire", events[1]["error"])
}

func TestWhenFilesMoveWithinALocationThenProgressTicksForEveryItem(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	ctrl := gomock.NewController(t)
	svc, p, rec := newOpTestService(t, ctrl)

	for _, id := range []string{"a", "b"} {
		seedOpFile(t, svc, id)
		p.EXPECT().MoveWithin(gomock.Any(), "L1", []string{id}, "").Return(nil)
		p.EXPECT().Stat(gomock.Any(), "L1", id).Return(provider.FileInfo{}, errors.New("not found"))
	}

	r.NoError(svc.MoveWithin("L1", []string{"a", "b"}, ""))

	events := rec.snapshot()
	r.Len(events, 4, "start, one tick per item, done")
	r.Equal(opMove, events[0]["op"])
	r.Equal(2, events[3]["done"])
	r.Equal(opDone, events[3]["state"])
}
