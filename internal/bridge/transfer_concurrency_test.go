package bridge

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/radosny/airdrive/internal/entities"
	"github.com/radosny/airdrive/internal/provider"
	mockprovider "github.com/radosny/airdrive/internal/provider/mock"
)

func TestWhenTheQueueIsLongerThanTheWorkerBudgetThenOnlyTheBudgetRunsAtOnce(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	svc, db, rec := openTransferService(t)
	const budget = 2
	svc.settings = func() TransferSettings { return TransferSettings{ConcurrentTransfers: budget} }
	ctrl := gomock.NewController(t)
	seedTransferTree(t, db)

	src := mockprovider.NewMockStorageProvider(ctrl)
	dst := mockprovider.NewMockStorageProvider(ctrl)
	svc.registry.Register("SRC", src)
	svc.registry.Register("DST", dst)
	src.EXPECT().ID().Return("SRC").AnyTimes()
	dst.EXPECT().ID().Return("DST").AnyTimes()
	src.EXPECT().Kind().Return(provider.KindGDrive).AnyTimes()
	dst.EXPECT().Kind().Return(provider.KindS3).AnyTimes()

	ids := make([]string, 0, 6)
	for i := 0; i < 6; i++ {
		id := fmt.Sprintf("bulk_%d.txt", i)
		ids = append(ids, id)
		r.NoError(db.UpsertFile(context.Background(), provider.FileInfo{
			ID: id, LocationID: "SRC", Name: id, Kind: provider.FileKindFile, Size: 1,
		}))
	}

	release := make(chan struct{})
	started := make(chan struct{}, len(ids))
	var mu sync.Mutex
	inFlight, peak := 0, 0
	src.EXPECT().Download(gomock.Any(), "SRC", gomock.Any()).Times(len(ids)).DoAndReturn(
		func(context.Context, string, string) (io.ReadCloser, provider.FileInfo, error) {
			mu.Lock()
			inFlight++
			if inFlight > peak {
				peak = inFlight
			}
			mu.Unlock()
			started <- struct{}{}
			<-release
			mu.Lock()
			inFlight--
			mu.Unlock()
			return io.NopCloser(bytes.NewReader([]byte("x"))), provider.FileInfo{Name: "x", Size: 1}, nil
		})
	dst.EXPECT().Stat(gomock.Any(), "DST", gomock.Any()).
		Return(provider.FileInfo{}, errors.New("not found")).AnyTimes()
	dst.EXPECT().Upload(gomock.Any(), "DST", gomock.Any(), gomock.Any(), gomock.Any(), int64(1)).
		DoAndReturn(func(_ context.Context, _, _, name string, rd io.Reader, _ int64) (provider.FileInfo, error) {
			_, _ = io.ReadAll(rd)
			return provider.FileInfo{ID: name, LocationID: "DST", Name: name, Size: 1}, nil
		}).AnyTimes()

	_, err := svc.Enqueue(entities.EnqueueRequest{
		Action: "copy", FileIDs: ids, DestLocationID: "DST", ConflictPolicy: "smart",
	})
	r.NoError(err)

	for i := 0; i < budget; i++ {
		select {
		case <-started:
		case <-time.After(3 * time.Second):
			r.FailNow("workers never started")
		}
	}
	select {
	case <-started:
		r.FailNow("a third job started while the budget was two")
	case <-time.After(250 * time.Millisecond):
	}

	close(release)
	waitBatchDone(t, rec)

	mu.Lock()
	defer mu.Unlock()
	r.LessOrEqual(peak, budget)
	r.Equal(budget, peak)
}
