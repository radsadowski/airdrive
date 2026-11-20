package bridge

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/wailsapp/wails/v3/pkg/application"
	"golang.org/x/oauth2"

	"github.com/radosny/airdrive/internal/auth"
	"github.com/radosny/airdrive/internal/entities"
	ev "github.com/radosny/airdrive/internal/events"
	"github.com/radosny/airdrive/internal/index"
	"github.com/radosny/airdrive/internal/provider"
	"github.com/radosny/airdrive/internal/provider/gdrive"
	"github.com/radosny/airdrive/internal/provider/localfs"
	"github.com/radosny/airdrive/internal/provider/s3compat"
	"github.com/radosny/airdrive/internal/watcher"
)

// LocationService exposes location management to the frontend.
type LocationService struct {
	app      *application.App
	db       *index.DB
	registry *provider.Registry
	watcher  *watcher.Manager
	logger   *slog.Logger

	// emit is the event seam, wired by SetApp; the no-op default lets tests
	// inject a recorder without a Wails app.
	emit func(name string, data ...any)

	// DI seams with production defaults, overridden in tests.
	openBrowser           func(url string) error
	doSignIn              func(ctx context.Context, openBrowser func(string) error) (auth.SignInResult, error)
	fetchAccount          func(ctx context.Context, ts oauth2.TokenSource) (auth.AccountInfo, error)
	saveToken             func(locationID string, tok auth.GdriveToken) error
	loadToken             func(locationID string) (auth.GdriveToken, error)
	deleteToken           func(locationID string) error
	newGdriveProvider     func(ctx context.Context, locationID string, logger *slog.Logger) (provider.StorageProvider, error)
	newGdriveProviderWith func(ctx context.Context, locationID string, tok auth.GdriveToken, logger *slog.Logger) (provider.StorageProvider, error)
	newGdriveBrowser      func(ctx context.Context, ts oauth2.TokenSource) (gdrive.BrowseClient, error)
	newS3Client           func(cfg entities.S3Config) (s3compat.BucketLister, error)
	newS3Provider         func(ctx context.Context, locationID string, logger *slog.Logger) (provider.StorageProvider, error)
	saveCreds             func(locationID string, data []byte) error
	loadCreds             func(locationID string) ([]byte, error)
	deleteCreds           func(locationID string) error

	mu             sync.Mutex
	gdriveSessions map[string]*gdriveSession
	now            func() time.Time
	// statuses remembers the last state pushed per location, so a UI that
	// loads after a failed restore still sees the error instead of "idle".
	statuses map[string]locationStatus
}

// locationStatus is the last state reported for a location.
type locationStatus struct {
	state  string
	errMsg string
}

// gdriveSessionTTL bounds how long a sign-in can browse before confirming.
const gdriveSessionTTL = 10 * time.Minute

// Location states reported to the UI.
const (
	locationStateIdle     = "idle"
	locationStateIndexing = "indexing"
	locationStateError    = "error"
)

// gdriveSession is the in-memory sign-in state between connect and confirm.
type gdriveSession struct {
	token   auth.GdriveToken
	account auth.AccountInfo
	expires time.Time
}

func NewLocationService(app *application.App, db *index.DB, registry *provider.Registry, w *watcher.Manager, logger *slog.Logger) *LocationService {
	s := &LocationService{
		app:      app,
		db:       db,
		registry: registry,
		watcher:  w,
		logger:   logger,
		emit:     func(string, ...any) {},

		gdriveSessions: map[string]*gdriveSession{},
		statuses:       map[string]locationStatus{},
		now:            time.Now,
	}
	s.doSignIn = func(ctx context.Context, openBrowser func(string) error) (auth.SignInResult, error) {
		return auth.SignIn(ctx, openBrowser)
	}
	s.fetchAccount = auth.FetchAccountInfo
	s.saveToken = auth.SaveToken
	s.loadToken = auth.LoadToken
	s.deleteToken = auth.DeleteToken
	s.newGdriveProvider = s.realNewGdriveProvider
	s.newGdriveProviderWith = s.buildGdriveWith
	s.newGdriveBrowser = func(ctx context.Context, ts oauth2.TokenSource) (gdrive.BrowseClient, error) {
		return gdrive.NewBrowseClient(ctx, ts)
	}
	s.newS3Client = func(cfg entities.S3Config) (s3compat.BucketLister, error) {
		return s3compat.NewBucketLister(cfg.Endpoint, cfg.Region, cfg.AccessKeyID, cfg.SecretAccessKey, cfg.PathStyle)
	}
	s.newS3Provider = s.realNewS3Provider
	s.saveCreds = auth.SaveCredentials
	s.loadCreds = auth.LoadCredentials
	s.deleteCreds = auth.DeleteCredentials
	registry.SetLoader(s.providerFor)
	return s
}

// SetApp injects the Wails app reference after application.New returns.
func (s *LocationService) SetApp(app *application.App) {
	s.app = app
	s.emit = func(name string, data ...any) { app.Event.Emit(name, data...) }
	s.openBrowser = app.Browser.OpenURL
}

// ListLocations returns all connected locations with live file counts from SQLite.
func (s *LocationService) ListLocations() ([]entities.Location, error) {
	ctx := context.Background()
	locs, err := s.db.AllLocations(ctx)
	if err != nil {
		return nil, fmt.Errorf("list locations: %w", err)
	}
	out := make([]entities.Location, 0, len(locs))
	for _, l := range locs {
		count, size, _ := s.db.CountFiles(ctx, l.ID)
		state, errMsg := s.status(l.ID)
		out = append(out, entities.Location{
			ID:        l.ID,
			Kind:      string(l.Kind),
			Name:      l.Name,
			Root:      l.Root,
			Status:    state,
			FileCount: count,
			TotalSize: size,
			Error:     errMsg,
		})
	}
	return out, nil
}

// AddLocalLocation adds a local folder as a new location and starts a crawl.
func (s *LocationService) AddLocalLocation(root, name string) (*entities.Location, error) {
	if name == "" {
		name = "Local: " + root
	}
	id := uuid.NewString()
	loc := index.Location{
		ID:   id,
		Kind: provider.KindLocalFS,
		Name: name,
		Root: root,
	}
	if err := s.db.UpsertLocation(context.Background(), loc); err != nil {
		return nil, fmt.Errorf("add local location: %w", err)
	}
	p := localfs.New(id, root)
	s.registry.Register(id, p)

	if err := s.watcher.Watch(id, root); err != nil {
		s.logger.Warn("watcher start", "err", err)
	}

	go s.crawl(id, p)

	return &entities.Location{
		ID:     id,
		Kind:   string(provider.KindLocalFS),
		Name:   name,
		Root:   root,
		Status: "indexing",
	}, nil
}

// RefreshLocation triggers a background re-crawl of a location, re-indexing
// any files that have changed since the last crawl.
func (s *LocationService) RefreshLocation(locationID string) error {
	loc, err := s.db.LocationByID(context.Background(), locationID)
	if err != nil {
		return fmt.Errorf("refresh location: %w", err)
	}
	p, err := s.registry.Get(locationID)
	if err != nil {
		return fmt.Errorf("refresh location: no provider for %q: %w", locationID, err)
	}
	go s.crawl(loc.ID, p)
	return nil
}

// RemoveLocation deletes a location and its files from the index.
func (s *LocationService) RemoveLocation(locationID string) error {
	s.registry.Unregister(locationID)
	s.watcher.Unwatch(locationID)
	s.mu.Lock()
	delete(s.statuses, locationID)
	s.mu.Unlock()
	if err := auth.DeleteToken(locationID); err != nil {
		// Not fatal: the location row is the source of truth; a stray
		// Keychain item is harmless clutter, not a correctness bug.
		s.logger.Warn("remove location: keychain cleanup", "id", locationID, "err", err)
	}
	return s.db.DeleteLocation(context.Background(), locationID)
}

// crawl syncs a provider into the index, emitting progress events. The
// cursor persists only on success, so an interrupted full walk re-walks
// from scratch: never a partial resume.
func (s *LocationService) crawl(locationID string, p provider.StorageProvider) {
	s.emitLocationStatus(locationID, locationStateIndexing, "")
	var done int64
	reconciler, _ := p.(provider.Reconciler)
	reconcile := reconciler != nil && reconciler.Reconciles()
	var seen []string

	cursor, err := s.db.GetSyncCursor(context.Background(), locationID)
	if err != nil {
		s.logger.Warn("crawl: read sync cursor", "location", locationID, "err", err)
		cursor = ""
	}

	s.logger.Debug("crawl start", "location", locationID, "cursor", cursor)
	newCursor, err := p.SyncChanges(context.Background(), locationID, cursor, func(batch []provider.ChangeEvent) {
		for _, change := range batch {
			switch change.Kind {
			case provider.ChangeDeleted:
				if dErr := s.db.DeleteFile(context.Background(), locationID, change.FileID); dErr != nil {
					s.logger.Warn("crawl delete", "id", change.FileID, "err", dErr)
				}
			default:
				if change.File != nil {
					if uErr := s.db.UpsertFile(context.Background(), *change.File); uErr != nil {
						s.logger.Warn("crawl upsert", "err", uErr)
					}
				}
			}
			if reconcile && change.File != nil {
				seen = append(seen, change.FileID)
			}
			done++
		}
		s.logger.Debug("crawl batch", "location", locationID, "changes", len(batch), "done", done)
		s.emit(ev.IndexProgress, map[string]any{
			"locationId": locationID,
			"filesDone":  done,
			"filesTotal": 0, // unknown during a crawl; 0 renders indeterminate
		})
		s.emit(ev.FsChanged, map[string]any{
			"locationId": locationID,
			"dirId":      nil,
		})
	})
	if err != nil {
		s.logger.Error("crawl failed", "location", locationID, "err", err)
		s.emitLocationStatus(locationID, locationStateError, err.Error())
		return
	}

	if uErr := s.db.SetSyncCursor(context.Background(), locationID, newCursor); uErr != nil {
		s.logger.Warn("crawl: persist sync cursor", "location", locationID, "err", uErr)
	}

	if reconcile {
		if n, dErr := s.db.DeleteFilesNotIn(context.Background(), locationID, seen); dErr != nil {
			s.logger.Warn("crawl: reconcile", "location", locationID, "err", dErr)
		} else if n > 0 {
			s.logger.Info("crawl: reconciled deletions", "location", locationID, "removed", n)
			s.emit(ev.FsChanged, map[string]any{
				"locationId": locationID,
				"dirId":      nil,
			})
		}
	}

	count, size, _ := s.db.CountFiles(context.Background(), locationID)
	s.recordLocationStatus(locationID, locationStateIdle, "")
	s.emit(ev.LocationStatus, map[string]any{
		"locationId":   locationID,
		"state":        locationStateIdle,
		"fileCount":    count,
		"totalSize":    size,
		"lastSyncedAt": time.Now().UTC().Format(time.RFC3339),
	})
	s.emit(ev.FsChanged, map[string]any{
		"locationId": locationID,
		"dirId":      nil,
	})
}

func (s *LocationService) emitLocationStatus(locationID, state, errMsg string) {
	s.recordLocationStatus(locationID, state, errMsg)
	s.emit(ev.LocationStatus, map[string]any{
		"locationId": locationID,
		"state":      state,
		"error":      errMsg,
	})
}

// recordLocationStatus remembers the last state so ListLocations can report it
// after a UI reload, not only while the event is on the wire.
func (s *LocationService) recordLocationStatus(locationID, state, errMsg string) {
	s.mu.Lock()
	s.statuses[locationID] = locationStatus{state: state, errMsg: errMsg}
	s.mu.Unlock()
}

// status returns the last state reported for a location, defaulting to idle.
func (s *LocationService) status(locationID string) (string, string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	st, ok := s.statuses[locationID]
	if !ok {
		return locationStateIdle, ""
	}
	return st.state, st.errMsg
}

// ---- Google Drive connect / restore -------------------------------------- //

// realNewGdriveProvider builds the real provider from the Keychain token
// and the location's picked sync root.
func (s *LocationService) realNewGdriveProvider(ctx context.Context, locationID string, logger *slog.Logger) (provider.StorageProvider, error) {
	tok, err := s.loadToken(locationID)
	if err != nil {
		return nil, fmt.Errorf("gdrive provider: load token: %w", err)
	}
	return s.newGdriveProviderWith(ctx, locationID, tok, logger)
}

// buildGdriveWith constructs a Drive provider from an explicit token, so a
// reconnect can validate the new sign-in before it replaces the stored token.
func (s *LocationService) buildGdriveWith(ctx context.Context, locationID string, tok auth.GdriveToken, logger *slog.Logger) (provider.StorageProvider, error) {
	rootID, err := s.db.GetGdriveRootID(ctx, locationID)
	if err != nil {
		s.logger.Warn("gdrive provider: read sync root", "id", locationID, "err", err)
		rootID = ""
	}
	ts := auth.NewPersistingTokenSource(ctx, logger, locationID, tok)
	return gdrive.New(ctx, locationID, ts, logger, rootID)
}

// ConnectGdrive is phase one of the connect flow: one full interactive
// Google sign-in (PKCE, loopback redirect), returning a short-lived session
// the wizard uses to browse folders and confirm the sync root. It blocks
// until the user finishes in the browser (~3 min timeout).
func (s *LocationService) ConnectGdrive() (*entities.GdriveSession, error) {
	ctx := context.Background()

	if s.openBrowser == nil {
		return nil, errors.New("gdrive connect: browser not wired (app not set)")
	}

	result, err := s.doSignIn(ctx, s.openBrowser)
	if err != nil {
		return nil, fmt.Errorf("google sign-in: %w", err)
	}
	ts := auth.TokenSource(ctx, result.Token)
	info, err := s.fetchAccount(ctx, ts)
	if err != nil {
		return nil, fmt.Errorf("google sign-in: fetch account info: %w", err)
	}

	id := uuid.NewString()
	s.mu.Lock()
	s.pruneSessionsLocked()
	s.gdriveSessions[id] = &gdriveSession{
		token:   result.Token,
		account: info,
		expires: s.now().Add(gdriveSessionTTL),
	}
	s.mu.Unlock()

	s.logger.Info("gdrive account signed in", "email", info.Email)
	return &entities.GdriveSession{SessionID: id, Email: info.Email, Name: info.Name}, nil
}

// BrowseGdriveFolders lists the subfolders of dirID ("" = My Drive root) for
// the wizard's folder picker.
func (s *LocationService) BrowseGdriveFolders(sessionID, dirID string) ([]entities.File, error) {
	sess, err := s.session(sessionID)
	if err != nil {
		return nil, err
	}
	ctx := context.Background()
	browser, err := s.newGdriveBrowser(ctx, auth.TokenSource(ctx, sess.token))
	if err != nil {
		return nil, fmt.Errorf("gdrive browse: %w", err)
	}
	folders, err := browser.Folders(ctx, dirID)
	if err != nil {
		return nil, fmt.Errorf("gdrive browse: %w", err)
	}
	out := make([]entities.File, 0, len(folders))
	for _, f := range folders {
		out = append(out, entities.ToFile(f))
	}
	return out, nil
}

// StarredGdriveFolders lists starred folders account-wide: the only
// API-visible path to Computers-section (Drive for desktop) folders.
func (s *LocationService) StarredGdriveFolders(sessionID string) ([]entities.File, error) {
	sess, err := s.session(sessionID)
	if err != nil {
		return nil, err
	}
	ctx := context.Background()
	browser, err := s.newGdriveBrowser(ctx, auth.TokenSource(ctx, sess.token))
	if err != nil {
		return nil, fmt.Errorf("gdrive starred: %w", err)
	}
	folders, err := browser.StarredFolders(ctx)
	if err != nil {
		return nil, fmt.Errorf("gdrive starred: %w", err)
	}
	out := make([]entities.File, 0, len(folders))
	for _, f := range folders {
		out = append(out, entities.ToFile(f))
	}
	return out, nil
}

// ConfirmGdriveLocation is phase two: it creates the location rooted at
// folderID ("" = whole Drive), persists the token to the Keychain, registers
// the provider, and starts the initial crawl. Consumes the session.
func (s *LocationService) ConfirmGdriveLocation(sessionID, folderID string) (*entities.Location, error) {
	sess, err := s.session(sessionID)
	if err != nil {
		return nil, err
	}
	ctx := context.Background()

	rootDisplay := "My Drive"
	name := sess.account.Name
	if name == "" {
		name = sess.account.Email
	}
	if folderID != "" {
		browser, bErr := s.newGdriveBrowser(ctx, auth.TokenSource(ctx, sess.token))
		if bErr != nil {
			return nil, fmt.Errorf("gdrive confirm: %w", bErr)
		}
		fi, fErr := browser.Folder(ctx, folderID)
		if fErr != nil {
			return nil, fmt.Errorf("gdrive confirm: resolve folder: %w", fErr)
		}
		rootDisplay = fi.Name
		name = fmt.Sprintf("%s · %s", name, fi.Name)
	}

	id := uuid.NewString()
	if err := s.saveToken(id, sess.token); err != nil {
		return nil, fmt.Errorf("gdrive confirm: save token: %w", err)
	}

	loc := index.Location{
		ID:   id,
		Kind: provider.KindGDrive,
		Name: name,
		Root: rootDisplay,
	}
	if err := s.db.UpsertLocation(ctx, loc); err != nil {
		if dErr := s.deleteToken(id); dErr != nil {
			s.logger.Warn("gdrive confirm: rollback token", "err", dErr)
		}
		return nil, fmt.Errorf("gdrive confirm: save location: %w", err)
	}
	if folderID != "" {
		if rErr := s.db.SetGdriveRootID(ctx, id, folderID); rErr != nil {
			s.logger.Warn("gdrive confirm: persist sync root", "id", id, "err", rErr)
		}
	}

	p, err := s.newGdriveProvider(ctx, id, s.logger)
	if err != nil {
		if dErr := s.deleteToken(id); dErr != nil {
			s.logger.Warn("gdrive confirm: rollback token", "err", dErr)
		}
		return nil, fmt.Errorf("gdrive confirm: provider: %w", err)
	}

	s.registry.Register(id, p)
	go s.crawl(id, p)

	s.mu.Lock()
	delete(s.gdriveSessions, sessionID)
	s.mu.Unlock()

	s.logger.Info("gdrive location connected", "location", id, "root", rootDisplay, "email", sess.account.Email)

	return &entities.Location{
		ID:     id,
		Kind:   string(provider.KindGDrive),
		Name:   name,
		Root:   rootDisplay,
		Status: locationStateIndexing,
	}, nil
}

// ReconnectGdrive replaces the stored sign-in of an existing Drive location, so
// an expired or revoked refresh token can be renewed without losing the
// location id, its index, its sync cursor, or the sync pairs that reference it.
func (s *LocationService) ReconnectGdrive(locationID string) (*entities.Location, error) {
	ctx := context.Background()
	loc, err := s.db.LocationByID(ctx, locationID)
	if err != nil {
		return nil, fmt.Errorf("reconnect gdrive: %w", err)
	}
	if loc.Kind != provider.KindGDrive {
		return nil, fmt.Errorf("reconnect gdrive: location %q is %s, not gdrive", locationID, loc.Kind)
	}
	if s.openBrowser == nil {
		return nil, errors.New("gdrive reconnect: browser not wired (app not set)")
	}
	result, err := s.doSignIn(ctx, s.openBrowser)
	if err != nil {
		return nil, fmt.Errorf("google sign-in: %w", err)
	}
	p, err := s.newGdriveProviderWith(ctx, loc.ID, result.Token, s.logger)
	if err != nil {
		return nil, fmt.Errorf("reconnect gdrive: %w", err)
	}
	if err := s.saveToken(loc.ID, result.Token); err != nil {
		return nil, fmt.Errorf("reconnect gdrive: save token: %w", err)
	}
	s.registry.Register(loc.ID, p)
	s.emitLocationStatus(loc.ID, locationStateIndexing, "")
	go s.crawl(loc.ID, p) //nolint:gosec // the crawl outlives the sign-in request

	s.logger.Info("gdrive location reconnected", "location", loc.ID, "name", loc.Name)
	return &entities.Location{
		ID:     loc.ID,
		Kind:   string(loc.Kind),
		Name:   loc.Name,
		Root:   loc.Root,
		Status: locationStateIndexing,
	}, nil
}

// LocationHealth reports whether a location's provider is usable right now and,
// when it is not, whether the user has to sign in again. Navigating to a dead
// location calls it, so the reconnect prompt needs no manual check.
func (s *LocationService) LocationHealth(locationID string) (*entities.LocationHealth, error) {
	ctx := context.Background()
	loc, err := s.db.LocationByID(ctx, locationID)
	if err != nil {
		return nil, fmt.Errorf("location health: %w", err)
	}
	if _, err := s.registry.Get(locationID); err != nil {
		return &entities.LocationHealth{
			Connected: false,
			NeedsAuth: loc.Kind == provider.KindGDrive && strings.Contains(err.Error(), "invalid_grant"),
			Error:     connectHint(loc.Kind, err),
		}, nil
	}
	return &entities.LocationHealth{Connected: true}, nil
}

// session returns a live sign-in session or an expiry error.
func (s *LocationService) session(sessionID string) (*gdriveSession, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	sess, ok := s.gdriveSessions[sessionID]
	if !ok || s.now().After(sess.expires) {
		if ok {
			delete(s.gdriveSessions, sessionID)
		}
		return nil, errors.New("google sign-in session expired: start again")
	}
	return sess, nil
}

// pruneSessionsLocked drops expired sessions; caller holds mu.
func (s *LocationService) pruneSessionsLocked() {
	for id, sess := range s.gdriveSessions {
		if s.now().After(sess.expires) {
			delete(s.gdriveSessions, id)
		}
	}
}

// TestS3Connection validates credentials by listing buckets; a failed
// connection is a result, not an error. Failures log server-side with the
// connection parameters (never the secret) plus the request id Cloudflare
// can correlate in the R2 dashboard.
func (s *LocationService) TestS3Connection(cfg entities.S3Config) (*entities.S3TestResult, error) {
	client, err := s.newS3Client(cfg)
	if err != nil {
		s.logger.Warn("s3 connection test failed", "endpoint", cfg.Endpoint, "err", err)
		return &entities.S3TestResult{OK: false, Error: err.Error()}, nil
	}
	buckets, err := client.ListBuckets(context.Background())
	if err != nil {
		if cfg.Bucket != "" {
			if exists, bErr := client.BucketExists(context.Background(), cfg.Bucket); bErr == nil && exists {
				s.logger.Warn("s3 connection test: bucket listing denied but bucket readable: bucket-scoped token?",
					"endpoint", cfg.Endpoint, "bucket", cfg.Bucket, "listErr", s3compat.DescribeError(err))
				return &entities.S3TestResult{OK: true, Buckets: []string{cfg.Bucket}}, nil
			} else if bErr != nil {
				desc := s3compat.DescribeError(bErr)
				s.logger.Warn("s3 connection test failed",
					"endpoint", cfg.Endpoint, "region", cfg.Region, "bucket", cfg.Bucket, "err", desc)
				return &entities.S3TestResult{OK: false, Error: desc}, nil
			}
		}
		desc := s3compat.DescribeError(err)
		s.logger.Warn("s3 connection test failed",
			"endpoint", cfg.Endpoint, "region", cfg.Region,
			"pathStyle", cfg.PathStyle, "bucket", cfg.Bucket, "err", desc)
		return &entities.S3TestResult{OK: false, Error: desc}, nil
	}
	s.logger.Info("s3 connection test ok", "endpoint", cfg.Endpoint, "buckets", len(buckets))
	return &entities.S3TestResult{OK: true, Buckets: buckets}, nil
}

// AddS3Location creates a bucket-backed location from the wizard config:
// credentials to the Keychain, connection details to the config JSON, then
// register the provider and start the initial crawl.
func (s *LocationService) AddS3Location(cfg entities.S3Config) (*entities.Location, error) {
	ctx := context.Background()
	id := uuid.NewString()
	s.logger.Info("s3 location connecting", "preset", cfg.Preset, "endpoint", cfg.Endpoint, "bucket", cfg.Bucket)

	creds, err := json.Marshal(s3compat.Credentials{AccessKeyID: cfg.AccessKeyID, SecretAccessKey: cfg.SecretAccessKey})
	if err != nil {
		return nil, fmt.Errorf("s3 add: marshal credentials: %w", err)
	}
	if err := s.saveCreds(id, creds); err != nil {
		return nil, fmt.Errorf("s3 add: save credentials: %w", err)
	}

	name := cfg.Bucket
	if cfg.Preset != "" {
		name = strings.ToUpper(cfg.Preset) + ": " + cfg.Bucket
	}
	loc := index.Location{ID: id, Kind: provider.KindS3, Name: name, Root: cfg.Bucket}
	if err := s.db.UpsertLocation(ctx, loc); err != nil {
		if dErr := s.deleteCreds(id); dErr != nil {
			s.logger.Warn("s3 add: rollback credentials", "err", dErr)
		}
		return nil, fmt.Errorf("s3 add: save location: %w", err)
	}
	if err := s.db.SetConfigValues(ctx, id, map[string]string{
		"s3Endpoint":  cfg.Endpoint,
		"s3Region":    cfg.Region,
		"s3Bucket":    cfg.Bucket,
		"s3PathStyle": strconv.FormatBool(cfg.PathStyle),
	}); err != nil {
		s.logger.Warn("s3 add: persist config", "id", id, "err", err)
	}

	p, err := s.newS3Provider(ctx, id, s.logger)
	if err != nil {
		if dErr := s.deleteCreds(id); dErr != nil {
			s.logger.Warn("s3 add: rollback credentials", "err", dErr)
		}
		return nil, fmt.Errorf("s3 add: provider: %w", err)
	}

	s.registry.Register(id, p)
	go s.crawl(id, p)

	s.logger.Info("s3 location connected", "location", id, "bucket", cfg.Bucket)
	return &entities.Location{
		ID:     id,
		Kind:   string(provider.KindS3),
		Name:   name,
		Root:   cfg.Bucket,
		Status: "indexing",
	}, nil
}

// realNewS3Provider rebuilds the bucket provider from stored config + creds.
func (s *LocationService) realNewS3Provider(ctx context.Context, locationID string, logger *slog.Logger) (provider.StorageProvider, error) {
	credsJSON, err := s.loadCreds(locationID)
	if err != nil {
		s.logger.Warn("s3 provider: load credentials", "id", locationID, "err", err)
		return nil, fmt.Errorf("s3 provider: load credentials: %w", err)
	}
	var creds s3compat.Credentials
	if err := json.Unmarshal(credsJSON, &creds); err != nil {
		s.logger.Warn("s3 provider: parse credentials", "id", locationID, "err", err)
		return nil, fmt.Errorf("s3 provider: parse credentials: %w", err)
	}
	vals, err := s.db.GetConfigValues(ctx, locationID, "s3Endpoint", "s3Region", "s3Bucket", "s3PathStyle")
	if err != nil {
		s.logger.Warn("s3 provider: read config", "id", locationID, "err", err)
		return nil, fmt.Errorf("s3 provider: read config: %w", err)
	}
	return s3compat.New(locationID, s3compat.Config{
		Endpoint:  vals["s3Endpoint"],
		Region:    vals["s3Region"],
		Bucket:    vals["s3Bucket"],
		PathStyle: vals["s3PathStyle"] == "true",
	}, creds, logger)
}

// Restore registers a provider for every persisted location and starts an
// incremental crawl for remote ones; localfs freshness comes from the watcher.
// A location whose provider cannot be built is reported as an error and is
// repaired lazily by providerFor on first use.
func (s *LocationService) Restore() error {
	ctx := context.Background()
	locs, err := s.db.AllLocations(ctx)
	if err != nil {
		return fmt.Errorf("restore locations: %w", err)
	}
	for _, l := range locs {
		p, pErr := s.buildProvider(ctx, l)
		if pErr != nil {
			s.logger.Warn("restore location", "id", l.ID, "kind", l.Kind, "err", pErr)
			s.emitLocationStatus(l.ID, locationStateError, connectHint(l.Kind, pErr))
			continue
		}
		s.registry.Register(l.ID, p)
		if l.Kind != provider.KindLocalFS {
			go s.crawl(l.ID, p) // incremental via the stored cursor
		}
		s.logger.Info("restored location", "id", l.ID, "kind", l.Kind, "root", l.Root)
	}
	return nil
}

// buildProvider constructs the provider for a persisted location. Restore and
// the registry's lazy fallback both go through it, so they cannot drift apart.
func (s *LocationService) buildProvider(ctx context.Context, loc index.Location) (provider.StorageProvider, error) {
	switch loc.Kind {
	case provider.KindLocalFS:
		if s.watcher != nil {
			if err := s.watcher.Watch(loc.ID, loc.Root); err != nil {
				s.logger.Warn("watcher start", "id", loc.ID, "err", err)
			}
		}
		return localfs.New(loc.ID, loc.Root), nil
	case provider.KindGDrive:
		return s.newGdriveProvider(ctx, loc.ID, s.logger)
	case provider.KindS3:
		return s.newS3Provider(ctx, loc.ID, s.logger)
	default:
		return nil, fmt.Errorf("unknown location kind %q", loc.Kind)
	}
}

// providerFor returns the provider for a persisted location, building and
// registering it when an earlier restore attempt failed. The registry calls it
// lazily, so one failed startup no longer disables the location until relaunch.
func (s *LocationService) providerFor(ctx context.Context, locationID string) (provider.StorageProvider, error) {
	loc, err := s.db.LocationByID(ctx, locationID)
	if err != nil {
		return nil, fmt.Errorf("location %q: %w", locationID, err)
	}
	p, err := s.buildProvider(ctx, loc)
	if err != nil {
		s.emitLocationStatus(loc.ID, locationStateError, connectHint(loc.Kind, err))
		return nil, fmt.Errorf("%s is not connected: %w", loc.Name, err)
	}
	s.registry.Register(loc.ID, p)
	if loc.Kind != provider.KindLocalFS {
		go s.crawl(loc.ID, p) //nolint:gosec // the crawl outlives the RPC that triggered the rebuild
	}
	return p, nil
}

// connectHint turns a provider construction failure into the message the
// sidebar shows, so a disconnected location reads as something to act on.
func connectHint(kind provider.Kind, err error) string {
	switch kind {
	case provider.KindGDrive:
		if strings.Contains(err.Error(), "invalid_grant") {
			return "Google Drive sign-in has expired. Reconnect the location to sign in again."
		}
		return "Google Drive is not connected: " + err.Error() + ". Reconnect the location if this keeps happening."
	case provider.KindS3:
		return "The bucket is not reachable: " + err.Error() + ". Reconnect the location if this keeps happening."
	default:
		return err.Error()
	}
}
