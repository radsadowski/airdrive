package bridge

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/radosny/airdrive/internal/index"
	"github.com/radosny/airdrive/internal/provider"
	mockprovider "github.com/radosny/airdrive/internal/provider/mock"
)

// thumbnailingProvider satisfies both capabilities the service type-asserts.
type thumbnailingProvider struct {
	*mockprovider.MockStorageProvider
	*mockprovider.MockThumbnailer
}

func openThumbService(t *testing.T) (*ThumbnailService, *index.DB, string) {
	t.Helper()
	db, err := index.Open(filepath.Join(t.TempDir(), "test.db"), slog.New(slog.NewTextHandler(io.Discard, nil)))
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })
	cacheDir := t.TempDir()
	svc, err := NewThumbnailService(db, provider.NewRegistry(), slog.New(slog.NewTextHandler(io.Discard, nil)), cacheDir)
	require.NoError(t, err)
	return svc, db, cacheDir
}

func seedImage(t *testing.T, db *index.DB, id, etag, preview string) {
	t.Helper()
	err := db.UpsertLocation(context.Background(), index.Location{
		ID:   "L1",
		Kind: "gdrive",
		Name: "Drive",
		Root: "My Drive",
	})
	require.NoError(t, err)
	err = db.UpsertFile(context.Background(), provider.FileInfo{
		ID:          id,
		LocationID:  "L1",
		Name:        filepath.Base(id),
		Kind:        provider.FileKindFile,
		MIME:        "image/jpeg",
		Etag:        etag,
		PreviewKind: preview,
		ModifiedAt:  time.Now(),
	})
	require.NoError(t, err)
}

func serveThumb(t *testing.T, svc *ThumbnailService, locationID, fileID string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/thumb/"+locationID+"/"+url.PathEscape(fileID), nil)
	rec := httptest.NewRecorder()
	svc.ServeHTTP(rec, req)
	return rec
}

// wireThumb registers a mock provider for L1 and returns its thumbnail side.
func wireThumb(t *testing.T, svc *ThumbnailService, ctrl *gomock.Controller) *thumbnailingProvider {
	t.Helper()
	p := &thumbnailingProvider{
		mockprovider.NewMockStorageProvider(ctrl),
		mockprovider.NewMockThumbnailer(ctrl),
	}
	svc.registry.Register("L1", p)
	return p
}

func TestWhenAThumbnailIsServedTwiceThenTheProviderIsCalledOnce(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	svc, db, _ := openThumbService(t)
	ctrl := gomock.NewController(t)

	seedImage(t, db, "F1", "E1", "image")
	p := wireThumb(t, svc, ctrl)
	p.MockThumbnailer.EXPECT().Thumbnail(gomock.Any(), "L1", "F1").Return([]byte("THUMBDATA"), nil)

	r.Equal(http.StatusOK, serveThumb(t, svc, "L1", "F1").Code)
	r.Equal(http.StatusOK, serveThumb(t, svc, "L1", "F1").Code)
}

func TestWhenTheEtagChangesThenTheThumbnailIsRefetched(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	svc, db, _ := openThumbService(t)
	ctrl := gomock.NewController(t)

	seedImage(t, db, "F1", "E1", "image")
	p := wireThumb(t, svc, ctrl)
	p.MockThumbnailer.EXPECT().Thumbnail(gomock.Any(), "L1", "F1").Return([]byte("V1"), nil)

	rec := serveThumb(t, svc, "L1", "F1")
	r.Equal(http.StatusOK, rec.Code)

	seedImage(t, db, "F1", "E2", "image")
	p.MockThumbnailer.EXPECT().Thumbnail(gomock.Any(), "L1", "F1").Return([]byte("V2"), nil)
	rec = serveThumb(t, svc, "L1", "F1")
	r.Equal(http.StatusOK, rec.Code)
	r.Equal("V2", rec.Body.String(), "etag change must invalidate the cache")
}

func TestWhenTheFileIsNotAnImageThenServeReturns404(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	svc, db, _ := openThumbService(t)
	ctrl := gomock.NewController(t)

	seedImage(t, db, "F1", "E1", "binary")
	p := wireThumb(t, svc, ctrl)
	p.MockThumbnailer.EXPECT().Thumbnail(gomock.Any(), gomock.Any(), gomock.Any()).Times(0)

	r.Equal(http.StatusNotFound, serveThumb(t, svc, "L1", "F1").Code)
}

func TestWhenTheFileIsUnknownThenServeReturns404(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	svc, _, _ := openThumbService(t)
	ctrl := gomock.NewController(t)
	wireThumb(t, svc, ctrl)

	r.Equal(http.StatusNotFound, serveThumb(t, svc, "L1", "NOPE").Code)
}

func TestWhenTheProviderHasNoThumbnailerThenServeReturns404(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	svc, db, _ := openThumbService(t)
	ctrl := gomock.NewController(t)

	seedImage(t, db, "F1", "E1", "image")
	svc.registry.Register("L1", mockprovider.NewMockStorageProvider(ctrl))

	r.Equal(http.StatusNotFound, serveThumb(t, svc, "L1", "F1").Code)
}

func TestWhenTheProviderFailsThenServeReturns404(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	svc, db, _ := openThumbService(t)
	ctrl := gomock.NewController(t)

	seedImage(t, db, "F1", "E1", "image")
	p := wireThumb(t, svc, ctrl)
	p.MockThumbnailer.EXPECT().Thumbnail(gomock.Any(), "L1", "F1").Return(nil, errors.New("sips exploded"))

	r.Equal(http.StatusNotFound, serveThumb(t, svc, "L1", "F1").Code)
}

func TestWhenTheFileIDContainsSlashesThenItRoundTrips(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	svc, db, _ := openThumbService(t)
	ctrl := gomock.NewController(t)

	fileID := "/tmp/airdrive root/sub dir/photo 1.jpg"
	seedImage(t, db, fileID, "E1", "image")
	p := wireThumb(t, svc, ctrl)
	p.MockThumbnailer.EXPECT().Thumbnail(gomock.Any(), "L1", fileID).Return([]byte("LOCALTHUMB"), nil)

	rec := serveThumb(t, svc, "L1", fileID)
	r.Equal(http.StatusOK, rec.Code)
	r.Equal("LOCALTHUMB", rec.Body.String())
}

func TestWhenTheLocationDoesNotOwnTheFileThenServeReturns404(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	svc, db, _ := openThumbService(t)
	ctrl := gomock.NewController(t)

	seedImage(t, db, "F1", "E1", "image")
	wireThumb(t, svc, ctrl)

	r.Equal(http.StatusNotFound, serveThumb(t, svc, "L2", "F1").Code)
}

func TestWhenThePathIsNotAThumbRouteThenMiddlewarePassesThrough(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	svc, _, _ := openThumbService(t)
	ctrl := gomock.NewController(t)
	wireThumb(t, svc, ctrl)

	var passedThrough bool
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		passedThrough = true
		w.WriteHeader(http.StatusOK)
	})
	handler := svc.Middleware(next)

	req := httptest.NewRequest(http.MethodGet, "/index.html", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	r.True(passedThrough)

	passedThrough = false
	req = httptest.NewRequest(http.MethodGet, "/thumb/L1/F1", nil)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	r.False(passedThrough, "thumb requests must not reach the asset server")
	r.Equal(http.StatusNotFound, rec.Code, "unregistered file degrades to 404")
}

func TestWhenSweepRunsThenStaleCacheEntriesAreRemoved(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	svc, _, cacheDir := openThumbService(t)

	fresh := filepath.Join(cacheDir, "fresh.thumb")
	stale := filepath.Join(cacheDir, "stale.thumb")
	r.NoError(os.WriteFile(fresh, []byte("x"), 0o600))
	r.NoError(os.WriteFile(stale, []byte("x"), 0o600))
	old := time.Now().Add(-40 * 24 * time.Hour)
	r.NoError(os.Chtimes(stale, old, old))

	svc.Sweep()

	_, err := os.Stat(fresh)
	r.NoError(err)
	_, err = os.Stat(stale)
	r.True(os.IsNotExist(err))
}
