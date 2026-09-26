package gdrive

import (
	"context"
	"fmt"
	"io"
	"strings"

	"google.golang.org/api/drive/v3"
	"google.golang.org/api/googleapi"

	"github.com/radosny/airdrive/internal/provider"
)

// fileFields is the field mask every metadata request asks for.
const fileFields = "id,name,mimeType,parents,size,modifiedTime,version"

// parentID maps "" to the resolved root ID for parents fields (the "root"
// alias is only reliable in query filters).
func (p *Provider) parentID(id string) string {
	if id == "" {
		return p.rootID
	}
	return id
}

// driveRootAlias is Drive's query alias for the My Drive root.
const driveRootAlias = "root"

// queryParent maps "" to Drive's "root" alias for q filters.
func queryParent(id string) string {
	if id == "" {
		return driveRootAlias
	}
	return id
}

// isGoogleNative reports whether a Drive MIME type has no binary content
// (Docs, Sheets, Shortcuts, …) — listable but not downloadable in v1.
func isGoogleNative(mime string) bool {
	return mime != folderMIME && strings.HasPrefix(mime, googleNativePrefix)
}

func (p *Provider) List(ctx context.Context, locationID, dirID string, sort provider.SortSpec, cursor string, limit int) (provider.ListPage, error) {
	pageSize := int64(1000)
	if limit > 0 && int64(limit) < pageSize {
		pageSize = int64(limit)
	}
	call := p.svc.Files.List().Context(ctx).
		Q(fmt.Sprintf("'%s' in parents and trashed = false", queryParent(dirID))).
		Fields("files(" + fileFields + "),nextPageToken").
		PageSize(pageSize).
		OrderBy(orderByFor(sort))
	if cursor != "" {
		call = call.PageToken(cursor)
	}
	res, err := call.Do()
	if err != nil {
		return provider.ListPage{}, fmt.Errorf("gdrive: list %q: %w", dirID, err)
	}
	items := make([]provider.FileInfo, 0, len(res.Files))
	for _, f := range res.Files {
		items = append(items, p.toFileInfo(f))
	}
	return provider.ListPage{Items: items, Cursor: res.NextPageToken}, nil
}

// orderByFor maps contract sort fields onto Drive's orderBy keys; "kind" has
// no Drive equivalent and maps to folders-first + name, like the default.
func orderByFor(sort provider.SortSpec) string {
	if sort.Field == "" {
		sort.Field = provider.SortByName
		sort.Asc = true
	}
	dir := " asc"
	if !sort.Asc {
		dir = " desc"
	}
	switch sort.Field {
	case provider.SortBySize:
		return "quotaBytesUsed" + dir
	case provider.SortByModifiedAt:
		return "folder,modifiedTime" + dir
	default:
		return "folder,name" + dir
	}
}

func (p *Provider) Stat(ctx context.Context, locationID, fileID string) (provider.FileInfo, error) {
	f, err := p.svc.Files.Get(fileID).Context(ctx).Fields(fileFields).Do()
	if err != nil {
		return provider.FileInfo{}, fmt.Errorf("gdrive: get %q: %w", fileID, err)
	}
	return p.toFileInfo(f), nil
}

func (p *Provider) Mkdir(ctx context.Context, locationID, parentID, name string) (provider.FileInfo, error) {
	f, err := p.svc.Files.Create(&drive.File{
		Name:     name,
		MimeType: folderMIME,
		Parents:  []string{p.parentID(parentID)},
	}).Context(ctx).Fields(fileFields).Do()
	if err != nil {
		return provider.FileInfo{}, fmt.Errorf("gdrive: mkdir %q: %w", name, err)
	}
	return p.toFileInfo(f), nil
}

func (p *Provider) Rename(ctx context.Context, locationID, fileID, newName string) (provider.FileInfo, error) {
	f, err := p.svc.Files.Update(fileID, &drive.File{Name: newName}).
		Context(ctx).Fields(fileFields).Do()
	if err != nil {
		return provider.FileInfo{}, fmt.Errorf("gdrive: rename %q: %w", fileID, err)
	}
	return p.toFileInfo(f), nil
}

// MoveWithin costs one metadata GET (current parents) plus one PATCH per
// file, since Drive models a move as add/remove-parents.
func (p *Provider) MoveWithin(ctx context.Context, locationID string, fileIDs []string, destDirID string) error {
	dest := p.parentID(destDirID)
	for _, id := range fileIDs {
		cur, err := p.svc.Files.Get(id).Context(ctx).Fields("parents").Do()
		if err != nil {
			return fmt.Errorf("gdrive: get parents of %q: %w", id, err)
		}
		call := p.svc.Files.Update(id, &drive.File{}).
			Context(ctx).AddParents(dest)
		if len(cur.Parents) > 0 {
			call = call.RemoveParents(strings.Join(cur.Parents, ","))
		}
		if _, err := call.Do(); err != nil {
			return fmt.Errorf("gdrive: move %q: %w", id, err)
		}
	}
	return nil
}

// Delete moves files to Drive's own trash — never permanent.
func (p *Provider) Delete(ctx context.Context, locationID string, fileIDs []string) error {
	for _, id := range fileIDs {
		if _, err := p.svc.Files.Update(id, &drive.File{Trashed: true}).Context(ctx).Do(); err != nil {
			return fmt.Errorf("gdrive: trash %q: %w", id, err)
		}
	}
	return nil
}

func (p *Provider) Download(ctx context.Context, locationID, fileID string) (io.ReadCloser, provider.FileInfo, error) {
	f, err := p.svc.Files.Get(fileID).Context(ctx).Fields(fileFields).Do()
	if err != nil {
		return nil, provider.FileInfo{}, fmt.Errorf("gdrive: get %q: %w", fileID, err)
	}
	if f.MimeType == folderMIME {
		return nil, provider.FileInfo{}, fmt.Errorf("gdrive: %q is a directory — not downloadable", f.Name)
	}
	if isGoogleNative(f.MimeType) {
		return nil, provider.FileInfo{}, fmt.Errorf("gdrive: %q is a Google-native file (%s) — export-download is not supported in v1", f.Name, f.MimeType)
	}
	res, err := p.svc.Files.Get(fileID).Context(ctx).Download()
	if err != nil {
		return nil, provider.FileInfo{}, fmt.Errorf("gdrive: download %q: %w", fileID, err)
	}
	return res.Body, p.toFileInfo(f), nil
}

func (p *Provider) Upload(ctx context.Context, locationID, parentID, name string, r io.Reader, size int64) (provider.FileInfo, error) {
	mime := provider.MimeFor(name, false)
	f, err := p.svc.Files.Create(&drive.File{
		Name:     name,
		Parents:  []string{p.parentID(parentID)},
		MimeType: mime,
	}).
		Context(ctx).
		Media(r, googleapi.ContentType(mime)).
		Fields(fileFields).
		Do()
	if err != nil {
		return provider.FileInfo{}, fmt.Errorf("gdrive: upload %q: %w", name, err)
	}
	return p.toFileInfo(f), nil
}
