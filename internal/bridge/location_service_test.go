package bridge

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/minio/minio-go/v7"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	"golang.org/x/oauth2"

	"github.com/radosny/airdrive/internal/auth"
	"github.com/radosny/airdrive/internal/entities"
	"github.com/radosny/airdrive/internal/events"
	"github.com/radosny/airdrive/internal/index"
	"github.com/radosny/airdrive/internal/provider"
	"github.com/radosny/airdrive/internal/provider/gdrive"
	mockgdrive "github.com/radosny/airdrive/internal/provider/gdrive/mock"
	mockprovider "github.com/radosny/airdrive/internal/provider/mock"
	"github.com/radosny/airdrive/internal/provider/s3compat"
	mocks3compat "github.com/radosny/airdrive/internal/provider/s3compat/mock"
	"github.com/radosny/airdrive/internal/watcher"
)

// emitRecorder captures everything sent through the LocationService emit seam.
type emitRecorder struct {
	mu       sync.Mutex
	names    []string
	payloads []map[string]any
}

func (e *emitRecorder) record(name string, data ...any) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.names = append(e.names, name)
	payload, _ := data[0].(map[string]any)
	e.payloads = append(e.payloads, payload)
}

func (e *emitRecorder) states(evName string) []any {
	e.mu.Lock()
	defer e.mu.Unlock()
	var out []any
	for i, n := range e.names {
		if n == evName {
			if p := e.payloads[i]; p != nil {
				out = append(out, p["state"])
			} else {
				out = append(out, nil)
			}
		}
	}
	return out
}

func (e *emitRecorder) count(evName string) int {
	e.mu.Lock()
	defer e.mu.Unlock()
	n := 0
	for _, name := range e.names {
		if name == evName {
			n++
		}
	}
	return n
}

func openService(t *testing.T) (*LocationService, *index.DB, *emitRecorder) {
	t.Helper()
	db, err := index.Open(filepath.Join(t.TempDir(), "test.db"), slog.New(slog.NewTextHandler(io.Discard, nil)))
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })
	rec := &emitRecorder{}
	svc := NewLocationService(nil, db, provider.NewRegistry(), nil, slog.New(slog.NewTextHandler(io.Discard, nil)))
	svc.emit = rec.record
	return svc, db, rec
}

func seedFile(t *testing.T, db *index.DB, id, parent string) {
	t.Helper()
	err := db.UpsertFile(context.Background(), provider.FileInfo{
		ID:         id,
		LocationID: "L1",
		ParentID:   parent,
		Name:       id + ".txt",
		Kind:       provider.FileKindFile,
		MIME:       "text/plain",
		ModifiedAt: time.Now(),
	})
	require.NoError(t, err)
}

func seedLocation(t *testing.T, db *index.DB, id index.Location) {
	t.Helper()
	require.NoError(t, db.UpsertLocation(context.Background(), id))
}

// syncScript programs a MockStorageProvider's SyncChanges: it emits events and
// returns (cursor, err), capturing the cursor it was called with.
func syncScript(p *mockprovider.MockStorageProvider, cursor string, events []provider.ChangeEvent, err error) *string {
	var got string
	p.EXPECT().SyncChanges(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ context.Context, _, c string, emit func([]provider.ChangeEvent)) (string, error) {
			got = c
			if len(events) > 0 {
				emit(events)
			}
			return cursor, err
		}).AnyTimes()
	return &got
}

// reconcilingProvider marks a scripted mock as index-diffing.
type reconcilingProvider struct {
	*mockprovider.MockStorageProvider
}

func (reconcilingProvider) Reconciles() bool { return true }

// waitIdle blocks until the crawl goroutine finishes (LocationStatus "idle"
// is emitted after the cursor is persisted).
func waitIdle(t *testing.T, rec *emitRecorder) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		for _, st := range rec.states(events.LocationStatus) {
			if st == "idle" {
				return
			}
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("timed out waiting for crawl to reach idle")
}

func TestWhenTheFeedDeletesAFileThenTheIndexDropsItAndTheNewCursorIsPersisted(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	svc, db, rec := openService(t)
	ctx := context.Background()
	ctrl := gomock.NewController(t)

	seedLocation(t, db, index.Location{ID: "L1", Kind: provider.KindGDrive, Name: "Drive", Root: "My Drive"})
	seedFile(t, db, "F1", "")
	seedFile(t, db, "F9", "")

	f2 := provider.FileInfo{ID: "F2", LocationID: "L1", Name: "new.txt", Kind: provider.FileKindFile, MIME: "text/plain"}
	p := mockprovider.NewMockStorageProvider(ctrl)
	syncScript(p, "NEWTOKEN", []provider.ChangeEvent{
		{FileID: "F2", Kind: provider.ChangeCreated, File: &f2},
		{FileID: "F1", Kind: provider.ChangeDeleted},
	}, nil)

	svc.crawl("L1", p)

	_, err := db.FileByID(ctx, "L1", "F1")
	r.Error(err, "deleted file must be removed from the index")
	_, err = db.FileByID(ctx, "L1", "F9")
	r.NoError(err, "files outside the feed must be untouched")
	got, err := db.FileByID(ctx, "L1", "F2")
	r.NoError(err)
	r.Equal("new.txt", got.Name)

	cursor, err := db.GetSyncCursor(ctx, "L1")
	r.NoError(err)
	r.Equal("NEWTOKEN", cursor)

	r.Equal([]any{"indexing", "idle"}, rec.states(events.LocationStatus))
	r.GreaterOrEqual(rec.count(events.IndexProgress), 1)
}

func TestWhenACursorIsStoredThenCrawlResumesFromIt(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	svc, db, _ := openService(t)
	ctx := context.Background()
	ctrl := gomock.NewController(t)

	seedLocation(t, db, index.Location{ID: "L1", Kind: provider.KindGDrive, Name: "Drive", Root: "My Drive"})
	r.NoError(db.SetSyncCursor(ctx, "L1", "CUR123"))

	p := mockprovider.NewMockStorageProvider(ctrl)
	got := syncScript(p, "CUR456", nil, nil)

	svc.crawl("L1", p)
	r.Equal("CUR123", *got, "crawl must resume from the stored cursor")

	cursor, err := db.GetSyncCursor(ctx, "L1")
	r.NoError(err)
	r.Equal("CUR456", cursor)
}

func TestWhenCrawlFailsThenErrorStatusIsEmittedAndTheCursorIsKept(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	svc, db, rec := openService(t)
	ctx := context.Background()
	ctrl := gomock.NewController(t)

	seedLocation(t, db, index.Location{ID: "L1", Kind: provider.KindGDrive, Name: "Drive", Root: "My Drive"})
	r.NoError(db.SetSyncCursor(ctx, "L1", "OLDTOKEN"))

	p := mockprovider.NewMockStorageProvider(ctrl)
	syncScript(p, "", nil, errors.New("boom"))

	svc.crawl("L1", p)

	r.Equal([]any{"indexing", "error"}, rec.states(events.LocationStatus))
	cursor, err := db.GetSyncCursor(ctx, "L1")
	r.NoError(err)
	r.Equal("OLDTOKEN", cursor, "failed crawl must not advance the cursor")
}

// ---- Google Drive connect / restore --------------------------------------- //

// gdriveDeps wires the gdrive DI seams; the mock provider and browser carry
// each test's EXPECTs. Zero SyncChanges expectations means "any crawl fails".
type gdriveDeps struct {
	signInToken  auth.GdriveToken
	signInErr    error
	account      auth.AccountInfo
	accountErr   error
	loadTokenErr error
	providerErr  error
	provider     *mockprovider.MockStorageProvider
	browser      *mockgdrive.MockBrowseClient
	saved        []string
	deleted      []string
}

func wireGdrive(t *testing.T, svc *LocationService, d *gdriveDeps) *gdriveDeps {
	t.Helper()
	ctrl := gomock.NewController(t)
	if d.provider == nil {
		d.provider = mockprovider.NewMockStorageProvider(ctrl)
	}
	if d.browser == nil {
		d.browser = mockgdrive.NewMockBrowseClient(ctrl)
	}
	svc.openBrowser = func(string) error { return nil }
	svc.doSignIn = func(context.Context, func(string) error) (auth.SignInResult, error) {
		return auth.SignInResult{Token: d.signInToken}, d.signInErr
	}
	svc.fetchAccount = func(context.Context, oauth2.TokenSource) (auth.AccountInfo, error) {
		return d.account, d.accountErr
	}
	svc.saveToken = func(locationID string, _ auth.GdriveToken) error {
		d.saved = append(d.saved, locationID)
		return nil
	}
	svc.deleteToken = func(locationID string) error {
		d.deleted = append(d.deleted, locationID)
		return nil
	}
	svc.loadToken = func(string) (auth.GdriveToken, error) {
		return auth.GdriveToken{}, d.loadTokenErr
	}
	svc.newGdriveProvider = func(context.Context, string, *slog.Logger) (provider.StorageProvider, error) {
		if d.providerErr != nil {
			return nil, d.providerErr
		}
		return d.provider, nil
	}
	svc.newGdriveBrowser = func(context.Context, oauth2.TokenSource) (gdrive.BrowseClient, error) {
		return d.browser, nil
	}
	return d
}

func TestWhenGdriveIsConnectedThenASessionIsReturnedWithoutALocation(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	svc, db, _ := openService(t)

	deps := wireGdrive(t, svc, &gdriveDeps{
		signInToken: auth.GdriveToken{AccessToken: "A", RefreshToken: "RT"},
		account:     auth.AccountInfo{Name: "Jane Doe", Email: "jane@example.com"},
	})

	sess, err := svc.ConnectGdrive()
	r.NoError(err)
	r.NotEmpty(sess.SessionID)
	r.Equal("jane@example.com", sess.Email)
	r.Equal("Jane Doe", sess.Name)

	locs, err := db.AllLocations(context.Background())
	r.NoError(err)
	r.Empty(locs, "no location may exist before a folder is confirmed")
	r.Empty(deps.saved, "the token stays in memory until confirm")
}

func TestWhenFoldersAreBrowsedThenTheyComeFromTheBrowserSeam(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	svc, _, _ := openService(t)

	deps := wireGdrive(t, svc, &gdriveDeps{
		signInToken: auth.GdriveToken{AccessToken: "A", RefreshToken: "RT"},
		account:     auth.AccountInfo{Email: "jane@example.com"},
	})
	deps.browser.EXPECT().Folders(gomock.Any(), "").Return([]provider.FileInfo{
		{ID: "D1", Name: "Photos", Kind: provider.FileKindDir, LocationID: "L1"},
		{ID: "D2", Name: "Docs", Kind: provider.FileKindDir, LocationID: "L1"},
	}, nil)

	sess, err := svc.ConnectGdrive()
	r.NoError(err)

	folders, err := svc.BrowseGdriveFolders(sess.SessionID, "")
	r.NoError(err)
	r.Len(folders, 2)
	r.Equal("D1", folders[0].ID)
	r.Equal("dir", folders[0].Kind)
	r.Equal("Photos", folders[0].Name)
}

func TestWhenTheSessionIsUnknownThenBrowsingErrors(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	svc, _, _ := openService(t)

	_, err := svc.BrowseGdriveFolders("NOPE", "")
	r.Error(err)
}

func TestWhenTheSessionExpiresThenBrowsingAndConfirmingError(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	svc, _, _ := openService(t)
	deps := wireGdrive(t, svc, &gdriveDeps{
		signInToken: auth.GdriveToken{AccessToken: "A", RefreshToken: "RT"},
		account:     auth.AccountInfo{Email: "jane@example.com"},
	})
	sess, err := svc.ConnectGdrive()
	r.NoError(err)

	svc.now = func() time.Time { return time.Now().Add(11 * time.Minute) }

	_, err = svc.BrowseGdriveFolders(sess.SessionID, "")
	r.Error(err)
	_, err = svc.ConfirmGdriveLocation(sess.SessionID, "PICKED")
	r.Error(err)
	r.Empty(deps.saved, "an expired session must not persist anything")
}

func TestWhenAFolderIsConfirmedThenALocationRootedThereIsCreated(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	svc, db, rec := openService(t)
	ctx := context.Background()

	deps := wireGdrive(t, svc, &gdriveDeps{
		signInToken: auth.GdriveToken{AccessToken: "A", RefreshToken: "RT"},
		account:     auth.AccountInfo{Name: "Jane Doe", Email: "jane@example.com"},
	})
	deps.browser.EXPECT().Folder(gomock.Any(), "PICKED").Return(
		provider.FileInfo{ID: "PICKED", Name: "Vacation", Kind: provider.FileKindDir}, nil)
	syncScript(deps.provider, "FIRSTTOKEN", nil, nil)

	sess, err := svc.ConnectGdrive()
	r.NoError(err)

	loc, err := svc.ConfirmGdriveLocation(sess.SessionID, "PICKED")
	r.NoError(err)
	r.Equal("gdrive", loc.Kind)
	r.Equal("Vacation", loc.Root)
	r.Equal("Jane Doe · Vacation", loc.Name)
	r.Equal("indexing", loc.Status)
	r.NotEmpty(loc.ID)

	r.Equal([]string{loc.ID}, deps.saved)

	stored, err := db.LocationByID(ctx, loc.ID)
	r.NoError(err)
	r.Equal("gdrive", string(stored.Kind))
	rootID, err := db.GetGdriveRootID(ctx, loc.ID)
	r.NoError(err)
	r.Equal("PICKED", rootID)

	p, err := svc.registry.Get(loc.ID)
	r.NoError(err)
	r.Same(deps.provider, p)

	waitIdle(t, rec)
	cursor, err := db.GetSyncCursor(ctx, loc.ID)
	r.NoError(err)
	r.Equal("FIRSTTOKEN", cursor)

	_, err = svc.BrowseGdriveFolders(sess.SessionID, "")
	r.Error(err, "confirming consumes the session")
}

func TestWhenStarredFoldersAreRequestedThenTheyComeFromTheBrowserSeam(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	svc, _, _ := openService(t)

	deps := wireGdrive(t, svc, &gdriveDeps{
		signInToken: auth.GdriveToken{AccessToken: "A", RefreshToken: "RT"},
		account:     auth.AccountInfo{Email: "jane@example.com"},
	})
	deps.browser.EXPECT().StarredFolders(gomock.Any()).Return([]provider.FileInfo{
		{ID: "S1", Name: "MacBook Backups", Kind: provider.FileKindDir},
	}, nil)

	sess, err := svc.ConnectGdrive()
	r.NoError(err)

	starred, err := svc.StarredGdriveFolders(sess.SessionID)
	r.NoError(err)
	r.Len(starred, 1)
	r.Equal("S1", starred[0].ID)
	r.Equal("dir", starred[0].Kind)

	_, err = svc.StarredGdriveFolders("NOPE")
	r.Error(err)
}

func TestWhenWholeDriveIsConfirmedThenTheLocationCoversEverything(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	svc, db, _ := openService(t)
	ctx := context.Background()
	deps := wireGdrive(t, svc, &gdriveDeps{
		signInToken: auth.GdriveToken{AccessToken: "A", RefreshToken: "RT"},
		account:     auth.AccountInfo{Name: "Jane Doe", Email: "jane@example.com"},
	})
	syncScript(deps.provider, "TOK", nil, nil)

	sess, err := svc.ConnectGdrive()
	r.NoError(err)

	loc, err := svc.ConfirmGdriveLocation(sess.SessionID, "")
	r.NoError(err)
	r.Equal("My Drive", loc.Root)
	r.Equal("Jane Doe", loc.Name)

	rootID, err := db.GetGdriveRootID(ctx, loc.ID)
	r.NoError(err)
	r.Empty(rootID, "whole Drive keeps an empty sync root")
}

func TestWhenTheLocationSaveFailsThenTheSavedTokenIsRolledBack(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	svc, db, _ := openService(t)

	deps := wireGdrive(t, svc, &gdriveDeps{
		signInToken: auth.GdriveToken{AccessToken: "A", RefreshToken: "RT"},
		account:     auth.AccountInfo{Email: "jane@example.com"},
	})
	deps.browser.EXPECT().Folder(gomock.Any(), "PICKED").Return(
		provider.FileInfo{ID: "PICKED", Name: "Vacation", Kind: provider.FileKindDir}, nil).AnyTimes()

	sess, err := svc.ConnectGdrive()
	r.NoError(err)

	db.Close()

	_, err = svc.ConfirmGdriveLocation(sess.SessionID, "PICKED")
	r.Error(err)
	r.Len(deps.saved, 1, "token was saved before the location write")
	r.Len(deps.deleted, 1, "token must be rolled back when the location is never created")
	r.Equal(deps.saved[0], deps.deleted[0])
}

func TestWhenARestoredGdriveLocationHasACursorThenCrawlResumesFromIt(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	svc, db, rec := openService(t)
	ctx := context.Background()

	seedLocation(t, db, index.Location{ID: "L1", Kind: provider.KindGDrive, Name: "Drive", Root: "My Drive"})
	r.NoError(db.SetSyncCursor(ctx, "L1", "STOREDCUR"))

	deps := wireGdrive(t, svc, &gdriveDeps{})
	got := syncScript(deps.provider, "NEXTCUR", nil, nil)

	r.NoError(svc.Restore())

	p, err := svc.registry.Get("L1")
	r.NoError(err)
	r.Same(deps.provider, p)
	waitIdle(t, rec)
	r.Equal("STOREDCUR", *got, "restore must resume from the stored cursor")
}

func TestWhenCredentialsAreMissingThenRestoreEmitsErrorAndDoesNotCrawl(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	svc, db, rec := openService(t)

	seedLocation(t, db, index.Location{ID: "L1", Kind: provider.KindGDrive, Name: "Drive", Root: "My Drive"})

	wireGdrive(t, svc, &gdriveDeps{loadTokenErr: errors.New("keychain: not found")})

	r.NoError(svc.Restore())

	r.Equal([]any{"error"}, rec.states(events.LocationStatus))
	_, err := svc.registry.Get("L1")
	r.Error(err, "no provider must be registered without credentials")
}

func TestWhenALocalfsLocationIsRestoredThenItRegistersWithoutCrawling(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	svc, db, rec := openService(t)

	root := t.TempDir()
	seedLocation(t, db, index.Location{ID: "LOCAL1", Kind: provider.KindLocalFS, Name: "Local", Root: root})

	w, err := watcher.New(db, svc.logger, func(string, string) {})
	r.NoError(err)
	t.Cleanup(func() { w.Close() })
	svc.watcher = w

	r.NoError(svc.Restore())

	p, err := svc.registry.Get("LOCAL1")
	r.NoError(err)
	r.Equal(provider.KindLocalFS, p.Kind())
	r.Empty(rec.states(events.LocationStatus), "localfs freshness comes from the watcher, not a crawl")
}

func TestWhenAReconcileCrawlRunsThenRemoteDeletionsVanish(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	svc, db, rec := openService(t)
	ctx := context.Background()
	ctrl := gomock.NewController(t)

	seedLocation(t, db, index.Location{ID: "L1", Kind: provider.KindS3, Name: "R2", Root: "content"})
	seedFile(t, db, "A", "")
	seedFile(t, db, "GHOST", "")
	seedFile(t, db, "KEPT", "")

	p := &reconcilingProvider{mockprovider.NewMockStorageProvider(ctrl)}
	syncScript(p.MockStorageProvider, "", []provider.ChangeEvent{
		{FileID: "A", Kind: provider.ChangeCreated, File: &provider.FileInfo{ID: "A", LocationID: "L1", Name: "a.txt"}},
		{FileID: "KEPT", Kind: provider.ChangeCreated, File: &provider.FileInfo{ID: "KEPT", LocationID: "L1", Name: "kept.txt"}},
	}, nil)

	svc.crawl("L1", p)

	_, err := db.FileByID(ctx, "L1", "GHOST")
	r.Error(err, "the remote deletion must vanish from the index")
	_, err = db.FileByID(ctx, "L1", "A")
	r.NoError(err)
	_, err = db.FileByID(ctx, "L1", "KEPT")
	r.NoError(err)
	waitIdle(t, rec)
}

func TestWhenANonReconcileCrawlRunsThenAbsentFilesAreKept(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	svc, db, _ := openService(t)
	ctx := context.Background()
	ctrl := gomock.NewController(t)

	seedLocation(t, db, index.Location{ID: "L1", Kind: provider.KindGDrive, Name: "Drive", Root: "My Drive"})
	seedFile(t, db, "GHOST", "")

	p := mockprovider.NewMockStorageProvider(ctrl)
	syncScript(p, "TOK", []provider.ChangeEvent{
		{FileID: "A", Kind: provider.ChangeCreated, File: &provider.FileInfo{ID: "A", LocationID: "L1", Name: "a.txt"}},
	}, nil)

	svc.crawl("L1", p)

	_, err := db.FileByID(ctx, "L1", "GHOST")
	r.NoError(err, "changes-feed providers manage deletions themselves — no reconcile")
}

// ---- S3 connect / restore -------------------------------------------------- //

func TestWhenTheS3ConnectionIsTestedThenBucketsComeBack(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	svc, _, _ := openService(t)
	ctrl := gomock.NewController(t)

	s3c := mocks3compat.NewMockBucketLister(ctrl)
	s3c.EXPECT().ListBuckets(gomock.Any()).Return([]string{"content", "media"}, nil)
	svc.newS3Client = func(entities.S3Config) (s3compat.BucketLister, error) {
		return s3c, nil
	}

	res, err := svc.TestS3Connection(entities.S3Config{Endpoint: "https://x.r2.cloudflarestorage.com", Bucket: "content"})
	r.NoError(err)
	r.True(res.OK)
	r.Equal([]string{"content", "media"}, res.Buckets)
}

func TestWhenTheS3ConnectionTestFailsThenTheErrorSurfaces(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	svc, _, _ := openService(t)
	ctrl := gomock.NewController(t)

	s3c := mocks3compat.NewMockBucketLister(ctrl)
	s3c.EXPECT().ListBuckets(gomock.Any()).Return(nil, errors.New("403 InvalidAccessKeyId"))
	svc.newS3Client = func(entities.S3Config) (s3compat.BucketLister, error) {
		return s3c, nil
	}

	res, err := svc.TestS3Connection(entities.S3Config{})
	r.NoError(err, "a failed connection is a result, not an error")
	r.False(res.OK)
	r.Contains(res.Error, "InvalidAccessKeyId")
}

func TestWhenR2DeniesBucketListingThenTheStatusAndRequestIDSurface(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	svc, _, _ := openService(t)
	ctrl := gomock.NewController(t)

	s3c := mocks3compat.NewMockBucketLister(ctrl)
	s3c.EXPECT().ListBuckets(gomock.Any()).Return(nil, minio.ErrorResponse{
		Code: "AccessDenied", Message: "Access Denied.",
		StatusCode: http.StatusForbidden, RequestID: "tx-abc-123",
	})
	s3c.EXPECT().BucketExists(gomock.Any(), "content").Return(false, minio.ErrorResponse{
		Code: "AccessDenied", StatusCode: http.StatusForbidden, RequestID: "tx-bucket-456",
	})
	svc.newS3Client = func(entities.S3Config) (s3compat.BucketLister, error) {
		return s3c, nil
	}

	res, err := svc.TestS3Connection(entities.S3Config{Bucket: "content"})
	r.NoError(err)
	r.False(res.OK)
	r.Contains(res.Error, "AccessDenied")
	r.Contains(res.Error, "HTTP 403")
	r.Contains(res.Error, "tx-bucket-456", "the bucket probe error is the actionable one")
}

func TestWhenBucketListingIsDeniedButTheBucketIsReadableThenTheTestPasses(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	svc, _, _ := openService(t)
	ctrl := gomock.NewController(t)

	s3c := mocks3compat.NewMockBucketLister(ctrl)
	s3c.EXPECT().ListBuckets(gomock.Any()).Return(nil, minio.ErrorResponse{
		Code: "AccessDenied", Message: "Access Denied.", StatusCode: http.StatusForbidden, RequestID: "tx-list-1",
	})
	s3c.EXPECT().BucketExists(gomock.Any(), "content").Return(true, nil)
	svc.newS3Client = func(entities.S3Config) (s3compat.BucketLister, error) {
		return s3c, nil
	}

	res, err := svc.TestS3Connection(entities.S3Config{
		Preset: "r2", Endpoint: "https://acct.r2.cloudflarestorage.com", Bucket: "content",
	})
	r.NoError(err)
	r.True(res.OK, "bucket-scoped R2 tokens are denied ListBuckets but can connect to their bucket")
	r.Equal([]string{"content"}, res.Buckets)
}

func TestWhenAnS3LocationIsAddedThenItIsPersistedAndCrawled(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	svc, db, rec := openService(t)
	ctx := context.Background()
	ctrl := gomock.NewController(t)

	p := mockprovider.NewMockStorageProvider(ctrl)
	syncScript(p, "S3TOK", nil, nil)
	svc.newS3Provider = func(context.Context, string, *slog.Logger) (provider.StorageProvider, error) {
		return p, nil
	}
	var savedCreds []string
	svc.saveCreds = func(locationID string, _ []byte) error {
		savedCreds = append(savedCreds, locationID)
		return nil
	}

	loc, err := svc.AddS3Location(entities.S3Config{
		Preset: "r2", Endpoint: "https://acct.r2.cloudflarestorage.com",
		Region: "auto", Bucket: "content", PathStyle: true,
		AccessKeyID: "AK", SecretAccessKey: "SK",
	})
	r.NoError(err)
	r.Equal("s3", loc.Kind)
	r.Equal("content", loc.Root)
	r.Equal("R2 — content", loc.Name)
	r.Equal("indexing", loc.Status)

	stored, err := db.LocationByID(ctx, loc.ID)
	r.NoError(err)
	r.Equal("s3", string(stored.Kind))
	r.Equal("content", stored.Root)

	cfgVal, err := db.GetConfigValue(ctx, loc.ID, "s3Endpoint")
	r.NoError(err)
	r.Equal("https://acct.r2.cloudflarestorage.com", cfgVal)
	cfgVal, err = db.GetConfigValue(ctx, loc.ID, "s3Bucket")
	r.NoError(err)
	r.Equal("content", cfgVal)

	r.Equal([]string{loc.ID}, savedCreds, "credentials go to the Keychain once")

	got, err := svc.registry.Get(loc.ID)
	r.NoError(err)
	r.Same(p, got)
	waitIdle(t, rec)
	cursor, err := db.GetSyncCursor(ctx, loc.ID)
	r.NoError(err)
	r.Equal("S3TOK", cursor)
}

func TestWhenTheS3LocationSaveFailsThenCredentialsAreRolledBack(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	svc, db, _ := openService(t)

	var saved, deleted []string
	svc.saveCreds = func(locationID string, _ []byte) error {
		saved = append(saved, locationID)
		return nil
	}
	svc.deleteCreds = func(locationID string) error {
		deleted = append(deleted, locationID)
		return nil
	}
	db.Close()

	_, err := svc.AddS3Location(entities.S3Config{Preset: "r2", Bucket: "b"})
	r.Error(err)
	r.Len(saved, 1)
	r.Equal(saved, deleted, "credentials roll back when the location is never created")
}

func TestWhenARestoredS3LocationExistsThenItRegistersAndReconciles(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	svc, db, rec := openService(t)
	ctx := context.Background()
	ctrl := gomock.NewController(t)

	seedLocation(t, db, index.Location{ID: "S3L", Kind: provider.KindS3, Name: "R2 — content", Root: "content"})
	r.NoError(db.SetConfigValues(ctx, "S3L", map[string]string{
		"s3Endpoint":  "https://acct.r2.cloudflarestorage.com",
		"s3Region":    "auto",
		"s3Bucket":    "content",
		"s3PathStyle": "true",
	}))

	p := &reconcilingProvider{mockprovider.NewMockStorageProvider(ctrl)}
	syncScript(p.MockStorageProvider, "S3TOK", nil, nil)
	svc.newS3Provider = func(context.Context, string, *slog.Logger) (provider.StorageProvider, error) {
		return p, nil
	}
	svc.loadCreds = func(string) ([]byte, error) {
		return []byte(`{"accessKeyId":"AK","secretAccessKey":"SK"}`), nil
	}

	r.NoError(svc.Restore())

	got, err := svc.registry.Get("S3L")
	r.NoError(err)
	r.Same(p, got)
	waitIdle(t, rec)
}
