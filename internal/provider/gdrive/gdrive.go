// Package gdrive implements StorageProvider for one Google Drive (My Drive)
// location. It never imports internal/auth — callers hand it an oauth2.TokenSource.
package gdrive

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"golang.org/x/oauth2"
	"google.golang.org/api/drive/v3"
	"google.golang.org/api/option"

	"github.com/radosny/airdrive/internal/provider"
)

const (
	folderMIME         = "application/vnd.google-apps.folder"
	googleNativePrefix = "application/vnd.google-apps."
)

// Provider implements provider.StorageProvider for one Google Drive location.
type Provider struct {
	locationID string
	svc        *drive.Service
	client     *http.Client
	rootID     string // effective root: the picked folder, or the resolved Drive root
	syncRoot   string // picked folder ID; "" = whole Drive
	// syncRootInMyDrive is false when the picked root sits outside the My Drive
	// corpus (Computers section) — those files never appear in the changes
	// feed, so SyncChanges always re-lists instead.
	syncRootInMyDrive bool
	logger            *slog.Logger
}

// New builds a Provider scoped to rootID — the Drive folder to sync ("" =
// whole Drive, resolved to the real root ID). The effective root normalises
// toFileInfo's parentID ("" under the sync root), and a retrying transport
// layers under OAuth. Extra ClientOptions are appended for tests.
func New(ctx context.Context, locationID string, ts oauth2.TokenSource, logger *slog.Logger, rootID string, opts ...option.ClientOption) (*Provider, error) {
	if logger == nil {
		logger = slog.Default()
	}
	client := &http.Client{
		Transport: &oauth2.Transport{
			Source: ts,
			Base:   newRetryTransport(),
		},
	}
	svc, err := drive.NewService(ctx, append([]option.ClientOption{option.WithHTTPClient(client)}, opts...)...)
	if err != nil {
		return nil, fmt.Errorf("gdrive: drive service: %w", err)
	}
	driveRoot, err := svc.Files.Get("root").Fields("id").Context(ctx).Do()
	if err != nil {
		return nil, fmt.Errorf("gdrive: resolve root: %w", err)
	}
	effective := rootID
	inMyDrive := true
	if effective == "" {
		effective = driveRoot.Id
	} else {
		root, rErr := svc.Files.Get(rootID).Fields("id,parents").Context(ctx).Do()
		if rErr != nil {
			return nil, fmt.Errorf("gdrive: resolve sync root: %w", rErr)
		}
		inMyDrive = rootWithinMyDrive(ctx, svc, root, driveRoot.Id)
	}
	return &Provider{
		locationID:        locationID,
		svc:               svc,
		client:            client,
		rootID:            effective,
		syncRoot:          rootID,
		syncRootInMyDrive: inMyDrive,
		logger:            logger,
	}, nil
}

// rootWithinMyDrive walks the folder's parent chain looking for the My Drive
// root; Computers-section folders terminate without reaching it.
func rootWithinMyDrive(ctx context.Context, svc *drive.Service, root *drive.File, driveRootID string) bool {
	cur := root
	for hops := 0; hops < 20 && len(cur.Parents) > 0; hops++ {
		if cur.Parents[0] == driveRootID || cur.Parents[0] == driveRootAlias {
			return true
		}
		next, err := svc.Files.Get(cur.Parents[0]).Fields("id,parents").Context(ctx).Do()
		if err != nil {
			return false
		}
		cur = next
	}
	return false
}

func (p *Provider) ID() string          { return p.locationID }
func (p *Provider) Kind() provider.Kind { return provider.KindGDrive }

// Reconciles reports whether this root's deletions need the crawl's
// index-diff: Computers-section roots never appear in the changes feed.
func (p *Provider) Reconciles() bool { return !p.syncRootInMyDrive }

// toFileInfo maps a Drive file resource onto the FileInfo contract,
// normalising the root's real ID to parentID="" and denying previews to
// Google-native files.
func (p *Provider) toFileInfo(f *drive.File) provider.FileInfo {
	parentID := ""
	if len(f.Parents) > 0 && f.Parents[0] != p.rootID {
		parentID = f.Parents[0]
	}
	kind := provider.FileKindFile
	if f.MimeType == folderMIME {
		kind = provider.FileKindDir
	}
	preview := provider.PreviewKind(f.MimeType)
	if isGoogleNative(f.MimeType) {
		preview = "binary"
	}
	var modified time.Time
	if f.ModifiedTime != "" {
		modified, _ = time.Parse(time.RFC3339, f.ModifiedTime)
	}
	return provider.FileInfo{
		ID:          f.Id,
		LocationID:  p.locationID,
		ParentID:    parentID,
		Name:        f.Name,
		Kind:        kind,
		Size:        f.Size,
		MIME:        f.MimeType,
		Etag:        strconv.FormatInt(f.Version, 10),
		PreviewKind: preview,
		ModifiedAt:  modified,
	}
}

// parseModified parses a Drive RFC3339 timestamp, zero on failure.
func parseModified(s string) time.Time {
	if s == "" {
		return time.Time{}
	}
	t, _ := time.Parse(time.RFC3339, s)
	return t
}
