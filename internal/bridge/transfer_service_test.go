package bridge

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/radosny/airdrive/internal/entities"
	"github.com/radosny/airdrive/internal/events"
	"github.com/radosny/airdrive/internal/index"
	"github.com/radosny/airdrive/internal/provider"
	mockprovider "github.com/radosny/airdrive/internal/provider/mock"
)

func openTransferService(t *testing.T) (*TransferService, *index.DB, *emitRecorder) {
	t.Helper()
	db, err := index.Open(filepath.Join(t.TempDir(), "test.db"), slog.New(slog.NewTextHandler(io.Discard, nil)))
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })
	rec := &emitRecorder{}
	svc := NewTransferService(db, provider.NewRegistry(), slog.New(slog.NewTextHandler(io.Discard, nil)), nil)
	svc.emit = rec.record
	return svc, db, rec
}

func seedTransferTree(t *testing.T, db *index.DB) {
	t.Helper()
	ctx := context.Background()
	require.NoError(t, db.UpsertLocation(ctx, index.Location{ID: "SRC", Kind: provider.KindGDrive, Name: "Drive", Root: "My Drive"}))
	require.NoError(t, db.UpsertLocation(ctx, index.Location{ID: "DST", Kind: provider.KindS3, Name: "R2", Root: "content"}))
	for _, f := range []provider.FileInfo{
		{ID: "root.txt", LocationID: "SRC", ParentID: "", Name: "root.txt", Kind: provider.FileKindFile, Size: 10},
		{ID: "sub", LocationID: "SRC", ParentID: "", Name: "sub", Kind: provider.FileKindDir},
		{ID: "sub/a.png", LocationID: "SRC", ParentID: "sub", Name: "a.png", Kind: provider.FileKindFile, Size: 20, MD5: "d41d8cd98f00b204e9800998ecf8427e"},
	} {
		require.NoError(t, db.UpsertFile(ctx, f))
	}
}

func md5Of(b []byte) string {
	sum := md5.Sum(b)
	return hex.EncodeToString(sum[:])
}

func waitBatchDone(t *testing.T, rec *emitRecorder) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if rec.count(events.TransferBatchDone) > 0 {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("timed out waiting for batch completion")
}

func TestWhenASingleFileIsCopiedThenItStreamsToTheDestination(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	svc, db, rec := openTransferService(t)
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

	data := []byte("0123456789")
	src.EXPECT().Download(gomock.Any(), "SRC", "root.txt").
		Return(io.NopCloser(io.MultiReader(io.LimitReader(newRepeatReader(data), 10))), provider.FileInfo{
			ID: "root.txt", LocationID: "SRC", Name: "root.txt", Size: 10, MD5: md5Of(data),
		}, nil)
	dst.EXPECT().Stat(gomock.Any(), "DST", "root.txt").
		Return(provider.FileInfo{}, fmt.Errorf("not found")).
		AnyTimes()
	dst.EXPECT().Upload(gomock.Any(), "DST", "", "root.txt", gomock.Any(), int64(10)).
		DoAndReturn(func(_ context.Context, _, _, _ string, rd io.Reader, _ int64) (provider.FileInfo, error) {
			b, _ := io.ReadAll(rd)
			if md5Of(b) != md5Of(data) {
				return provider.FileInfo{}, fmt.Errorf("content mismatch")
			}
			return provider.FileInfo{ID: "root.txt", LocationID: "DST", Name: "root.txt", Size: int64(len(b))}, nil
		})

	batchID, err := svc.Enqueue(entities.EnqueueRequest{
		Action: "copy", FileIDs: []string{"root.txt"},
		DestLocationID: "DST", ConflictPolicy: "smart",
	})
	r.NoError(err)
	r.NotEmpty(batchID)

	waitBatchDone(t, rec)

	jobs, err := db.JobsForBatches(context.Background(), []string{batchID})
	r.NoError(err)
	r.Len(jobs, 1)
	r.Equal("done", jobs[0].State)
	r.True(jobs[0].MD5Verified)

	dest, err := db.FileByID(context.Background(), "DST", "root.txt")
	r.NoError(err, "dest index row upserted")
	r.Equal("root.txt", dest.Name)
}

func newRepeatReader(b []byte) io.Reader {
	return &repeatReader{b: b}
}

type repeatReader struct {
	b      []byte
	offset int
}

func (r *repeatReader) Read(p []byte) (int, error) {
	if r.offset >= len(r.b) {
		return 0, io.EOF
	}
	n := copy(p, r.b[r.offset:])
	r.offset += n
	return n, nil
}

func TestWhenAFolderIsCopiedThenDestDirsAreCreatedAndFilesLand(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	svc, db, rec := openTransferService(t)
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

	subDir := provider.FileInfo{ID: "sub/", LocationID: "DST", Name: "sub", Kind: provider.FileKindDir}
	dst.EXPECT().Mkdir(gomock.Any(), "DST", "", "sub").Return(subDir, nil)

	png := []byte("fake image data....")
	src.EXPECT().Download(gomock.Any(), "SRC", "sub/a.png").
		Return(io.NopCloser(newRepeatReader(png)), provider.FileInfo{
			ID: "sub/a.png", LocationID: "SRC", Name: "a.png", Size: 20, MD5: md5Of(png),
		}, nil)
	dst.EXPECT().Stat(gomock.Any(), "DST", "sub/a.png").Return(provider.FileInfo{}, fmt.Errorf("nf")).AnyTimes()
	dst.EXPECT().Upload(gomock.Any(), "DST", "sub/", "a.png", gomock.Any(), int64(20)).
		DoAndReturn(func(_ context.Context, _, _, _ string, rd io.Reader, _ int64) (provider.FileInfo, error) {
			b, _ := io.ReadAll(rd)
			if md5Of(b) != md5Of(png) {
				return provider.FileInfo{}, fmt.Errorf("content mismatch")
			}
			return provider.FileInfo{ID: "sub/a.png", LocationID: "DST", ParentID: "sub/", Name: "a.png", Size: int64(len(b))}, nil
		})

	batchID, err := svc.Enqueue(entities.EnqueueRequest{
		Action: "copy", FileIDs: []string{"sub"},
		DestLocationID: "DST", ConflictPolicy: "smart",
	})
	r.NoError(err)

	waitBatchDone(t, rec)

	jobs, err := db.JobsForBatches(context.Background(), []string{batchID})
	r.NoError(err)
	r.Len(jobs, 1, "only files become jobs; dirs expand")
	r.Equal("done", jobs[0].State)

	dirRow, err := db.FileByID(context.Background(), "DST", "sub/")
	r.NoError(err, "dest dir row upserted from Mkdir")
	r.Equal(provider.FileKindDir, dirRow.Kind)
}

func TestWhenSmartConflictsFindAnIdenticalFileThenItIsSkipped(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	svc, db, rec := openTransferService(t)
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

	dst.EXPECT().Stat(gomock.Any(), "DST", "root.txt").
		Return(provider.FileInfo{ID: "root.txt", Name: "root.txt", Size: 10, Kind: provider.FileKindFile}, nil)

	batchID, err := svc.Enqueue(entities.EnqueueRequest{
		Action: "copy", FileIDs: []string{"root.txt"},
		DestLocationID: "DST", ConflictPolicy: "smart",
	})
	r.NoError(err)

	waitBatchDone(t, rec)

	jobs, err := db.JobsForBatches(context.Background(), []string{batchID})
	r.NoError(err)
	r.Len(jobs, 1)
	r.Equal("skipped", jobs[0].State, "same name + size → identical → skip")
}

func TestWhenAMoveCompletesThenTheSourceItemIsDeleted(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	svc, db, rec := openTransferService(t)
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

	data := []byte("0123456789")
	src.EXPECT().Download(gomock.Any(), "SRC", "root.txt").
		Return(io.NopCloser(newRepeatReader(data)), provider.FileInfo{
			ID: "root.txt", LocationID: "SRC", Name: "root.txt", Size: 10, MD5: md5Of(data),
		}, nil)
	dst.EXPECT().Stat(gomock.Any(), "DST", "root.txt").Return(provider.FileInfo{}, fmt.Errorf("nf")).AnyTimes()
	dst.EXPECT().Upload(gomock.Any(), "DST", "", "root.txt", gomock.Any(), int64(10)).
		DoAndReturn(func(_ context.Context, _, _, _ string, rd io.Reader, _ int64) (provider.FileInfo, error) {
			b, _ := io.ReadAll(rd)
			return provider.FileInfo{ID: "root.txt", LocationID: "DST", Name: "root.txt", Size: int64(len(b))}, nil
		})
	src.EXPECT().Delete(gomock.Any(), "SRC", []string{"root.txt"}).Return(nil)

	batchID, err := svc.Enqueue(entities.EnqueueRequest{
		Action: "move", FileIDs: []string{"root.txt"},
		DestLocationID: "DST", ConflictPolicy: "smart",
	})
	r.NoError(err)

	waitBatchDone(t, rec)

	jobs, err := db.JobsForBatches(context.Background(), []string{batchID})
	r.NoError(err)
	r.Len(jobs, 1)
	r.Equal("done", jobs[0].State)

	_, err = db.FileByID(context.Background(), "SRC", "root.txt")
	r.Error(err, "source index row removed after move")
}

func TestWhenAMoveJobFailsThenTheSourceItemSurvives(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	svc, db, rec := openTransferService(t)
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

	src.EXPECT().Download(gomock.Any(), "SRC", "root.txt").
		Return(nil, provider.FileInfo{}, fmt.Errorf("drive 500"))
	dst.EXPECT().Stat(gomock.Any(), "DST", gomock.Any()).Return(provider.FileInfo{}, fmt.Errorf("nf")).AnyTimes()

	_, err := svc.Enqueue(entities.EnqueueRequest{
		Action: "move", FileIDs: []string{"root.txt"},
		DestLocationID: "DST", ConflictPolicy: "smart",
	})
	r.NoError(err)

	waitBatchDone(t, rec)

	_, err = db.FileByID(context.Background(), "SRC", "root.txt")
	r.NoError(err, "a failed job must keep the source intact")
}

func TestWhenAFileIsUploadedThenTheLocalPathStreamsToTheDest(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	svc, db, rec := openTransferService(t)
	ctrl := gomock.NewController(t)
	ctx := context.Background()

	require.NoError(t, db.UpsertLocation(ctx, index.Location{ID: "DST", Kind: provider.KindS3, Name: "R2", Root: "content"}))
	dst := mockprovider.NewMockStorageProvider(ctrl)
	svc.registry.Register("DST", dst)
	dst.EXPECT().ID().Return("DST").AnyTimes()
	dst.EXPECT().Kind().Return(provider.KindS3).AnyTimes()

	tmp := filepath.Join(t.TempDir(), "upload.txt")
	data := []byte("upload me")
	require.NoError(t, os.WriteFile(tmp, data, 0o644))

	dst.EXPECT().Stat(gomock.Any(), "DST", "upload.txt").Return(provider.FileInfo{}, fmt.Errorf("nf")).AnyTimes()
	dst.EXPECT().Upload(gomock.Any(), "DST", "", "upload.txt", gomock.Any(), int64(len(data))).
		DoAndReturn(func(_ context.Context, _, _, _ string, rd io.Reader, _ int64) (provider.FileInfo, error) {
			b, _ := io.ReadAll(rd)
			if string(b) != string(data) {
				return provider.FileInfo{}, fmt.Errorf("content mismatch")
			}
			return provider.FileInfo{ID: "upload.txt", LocationID: "DST", Name: "upload.txt", Size: int64(len(b))}, nil
		})

	batchID, err := svc.EnqueueUpload("DST", "", []string{tmp})
	r.NoError(err)

	waitBatchDone(t, rec)

	jobs, err := db.JobsForBatches(ctx, []string{batchID})
	r.NoError(err)
	r.Len(jobs, 1)
	r.Equal("done", jobs[0].State)
}

func TestWhenAMDBatchIsCanceledThenPendingJobsAreCanceled(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	svc, db, _ := openTransferService(t)
	ctx := context.Background()
	seedTransferTree(t, db)

	batchID := "batch_test"
	r.NoError(db.CreateTransferBatch(ctx, index.TransferBatch{ID: batchID, Action: "copy", Conflict: "smart"}, []index.TransferJob{
		{ID: "J1", SourceName: "a", SourceLocID: "SRC", DestLocID: "DST", BytesTotal: 10},
	}))

	r.NoError(svc.PauseBatch(batchID))
	jobs, err := db.JobsForBatches(ctx, []string{batchID})
	r.NoError(err)
	r.Len(jobs, 1)
	r.Equal("paused", jobs[0].State, "pause stops dispatch by marking jobs paused")

	r.NoError(svc.CancelBatch(batchID))
	jobs, err = db.JobsForBatches(ctx, []string{batchID})
	r.NoError(err)
	r.Equal("canceled", jobs[0].State)
}
