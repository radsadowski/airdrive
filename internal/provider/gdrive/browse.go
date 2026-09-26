// Pre-location folder browsing for the Drive connect flow.
package gdrive

//go:generate go run go.uber.org/mock/mockgen -source=browse.go -destination=mock/mock.go -package=mock

import (
	"context"
	"fmt"
	"net/http"

	"golang.org/x/oauth2"
	"google.golang.org/api/drive/v3"
	"google.golang.org/api/option"

	"github.com/radosny/airdrive/internal/provider"
)

// BrowseClient lists folders on an account before any location exists —
// used by the connect wizard's folder picker.
type BrowseClient interface {
	// Folders returns the subfolders of dirID ("" = My Drive root).
	Folders(ctx context.Context, dirID string) ([]provider.FileInfo, error)
	// Folder returns one folder's metadata by ID.
	Folder(ctx context.Context, fileID string) (provider.FileInfo, error)
	// StarredFolders returns starred folders anywhere on the account — the
	// only API-visible path to Computers-section (Drive for desktop) folders.
	StarredFolders(ctx context.Context) ([]provider.FileInfo, error)
}

// NewBrowseClient builds a BrowseClient on the same authed, retrying
// transport as the provider.
func NewBrowseClient(ctx context.Context, ts oauth2.TokenSource, opts ...option.ClientOption) (BrowseClient, error) {
	client := &http.Client{
		Transport: &oauth2.Transport{
			Source: ts,
			Base:   newRetryTransport(),
		},
	}
	svc, err := drive.NewService(ctx, append([]option.ClientOption{option.WithHTTPClient(client)}, opts...)...)
	if err != nil {
		return nil, fmt.Errorf("gdrive: browse service: %w", err)
	}
	return &browseClient{svc: svc, rootID: ""}, nil
}

type browseClient struct {
	svc    *drive.Service
	rootID string
}

// foldersQ is the folder-only children query; the browse flow only ever
// wants directories to navigate.
const foldersQ = " in parents and trashed = false and mimeType = 'application/vnd.google-apps.folder'"

func (b *browseClient) Folders(ctx context.Context, dirID string) ([]provider.FileInfo, error) {
	parent := queryParent(dirID)
	res, err := b.svc.Files.List().Context(ctx).
		Q("'" + parent + "'" + foldersQ).
		Fields("files(" + fileFields + "),nextPageToken").
		PageSize(1000).
		Do()
	if err != nil {
		return nil, fmt.Errorf("gdrive: browse folders of %q: %w", parent, err)
	}
	out := make([]provider.FileInfo, 0, len(res.Files))
	for _, f := range res.Files {
		out = append(out, browseToFileInfo(f))
	}
	return out, nil
}

func (b *browseClient) Folder(ctx context.Context, fileID string) (provider.FileInfo, error) {
	f, err := b.svc.Files.Get(fileID).Context(ctx).Fields(fileFields).Do()
	if err != nil {
		return provider.FileInfo{}, fmt.Errorf("gdrive: browse folder %q: %w", fileID, err)
	}
	return browseToFileInfo(f), nil
}

func (b *browseClient) StarredFolders(ctx context.Context) ([]provider.FileInfo, error) {
	res, err := b.svc.Files.List().Context(ctx).
		Q("starred = true and trashed = false and mimeType = 'application/vnd.google-apps.folder'").
		Fields("files(" + fileFields + "),nextPageToken").
		PageSize(1000).
		Do()
	if err != nil {
		return nil, fmt.Errorf("gdrive: starred folders: %w", err)
	}
	out := make([]provider.FileInfo, 0, len(res.Files))
	for _, f := range res.Files {
		out = append(out, browseToFileInfo(f))
	}
	return out, nil
}

// browseToFileInfo maps a Drive folder for the picker: ParentID stays
// verbatim (the picker navigates by ID and does not need root normalisation).
func browseToFileInfo(f *drive.File) provider.FileInfo {
	parentID := ""
	if len(f.Parents) > 0 {
		parentID = f.Parents[0]
	}
	return provider.FileInfo{
		ID:         f.Id,
		ParentID:   parentID,
		Name:       f.Name,
		Kind:       provider.FileKindDir,
		MIME:       f.MimeType,
		Etag:       fmt.Sprintf("%d", f.Version),
		ModifiedAt: parseModified(f.ModifiedTime),
	}
}
