package gdrive

import (
	"context"
	"fmt"

	"google.golang.org/api/drive/v3"

	"github.com/radosny/airdrive/internal/provider"
)

// Interface conformance.
var _ provider.StorageProvider = (*Provider)(nil)

// SyncChanges implements incremental sync: cursor "" = full walk (the start
// token is captured before enumerating, so changes made during the walk are
// caught next time), any other cursor = changes.list pagination. Live
// changes are emitted as ChangeCreated (upserts — created vs modified is
// indistinguishable from a single change entry). Roots outside the My Drive
// corpus (Computers section) never appear in the changes feed, so they
// always re-list the subtree instead.
func (p *Provider) SyncChanges(ctx context.Context, locationID, cursor string, emit func([]provider.ChangeEvent)) (string, error) {
	if cursor == "" || !p.syncRootInMyDrive {
		return p.fullWalk(ctx, emit)
	}
	return p.changesSince(ctx, cursor, emit)
}

func (p *Provider) fullWalk(ctx context.Context, emit func([]provider.ChangeEvent)) (string, error) {
	start, err := p.svc.Changes.GetStartPageToken().Context(ctx).Do()
	if err != nil {
		return "", fmt.Errorf("gdrive: get start page token: %w", err)
	}
	if p.syncRoot != "" {
		return p.walkSubtree(ctx, start.StartPageToken, emit)
	}
	return p.walkAll(ctx, start.StartPageToken, emit)
}

// walkSubtree enumerates the picked folder's subtree breadth-first: one
// files.list per directory, discovered folders appended to the worklist.
func (p *Provider) walkSubtree(ctx context.Context, startToken string, emit func([]provider.ChangeEvent)) (string, error) {
	dirs := []string{p.syncRoot}
	for len(dirs) > 0 {
		dir := dirs[0]
		dirs = dirs[1:]
		pageToken := ""
		for {
			call := p.svc.Files.List().Context(ctx).
				Q(fmt.Sprintf("'%s' in parents and trashed = false", dir)).
				Fields("files(" + fileFields + "),nextPageToken").
				PageSize(1000)
			if pageToken != "" {
				call = call.PageToken(pageToken)
			}
			res, err := call.Do()
			if err != nil {
				return "", fmt.Errorf("gdrive: walk %q: %w", dir, err)
			}
			if len(res.Files) > 0 {
				batch := make([]provider.ChangeEvent, 0, len(res.Files))
				for _, f := range res.Files {
					fi := p.toFileInfo(f)
					batch = append(batch, provider.ChangeEvent{FileID: f.Id, Kind: provider.ChangeCreated, File: &fi})
					if f.MimeType == folderMIME {
						dirs = append(dirs, f.Id)
					}
				}
				emit(batch)
			}
			if res.NextPageToken == "" {
				break
			}
			pageToken = res.NextPageToken
		}
	}
	return startToken, nil
}

func (p *Provider) walkAll(ctx context.Context, startToken string, emit func([]provider.ChangeEvent)) (string, error) {
	pageToken := ""
	for {
		call := p.svc.Files.List().Context(ctx).
			Q("trashed = false").
			Fields("files(" + fileFields + "),nextPageToken").
			PageSize(1000)
		if pageToken != "" {
			call = call.PageToken(pageToken)
		}
		res, err := call.Do()
		if err != nil {
			return "", fmt.Errorf("gdrive: full walk: %w", err)
		}
		if len(res.Files) > 0 {
			batch := make([]provider.ChangeEvent, 0, len(res.Files))
			for _, f := range res.Files {
				fi := p.toFileInfo(f)
				batch = append(batch, provider.ChangeEvent{FileID: f.Id, Kind: provider.ChangeCreated, File: &fi})
			}
			emit(batch)
		}
		if res.NextPageToken == "" {
			return startToken, nil
		}
		pageToken = res.NextPageToken
	}
}

func (p *Provider) changesSince(ctx context.Context, cursor string, emit func([]provider.ChangeEvent)) (string, error) {
	var filter *subtreeFilter
	if p.syncRoot != "" {
		filter = &subtreeFilter{p: p, seen: map[string]bool{}}
	}
	pageToken := cursor
	for {
		res, err := p.svc.Changes.List(pageToken).Context(ctx).
			IncludeRemoved(true).
			RestrictToMyDrive(true).
			Fields("changes(fileId,removed,file(" + fileFields + ",trashed)),newStartPageToken,nextPageToken").
			Do()
		if err != nil {
			return "", fmt.Errorf("gdrive: changes.list: %w", err)
		}
		if len(res.Changes) > 0 {
			batch := make([]provider.ChangeEvent, 0, len(res.Changes))
			for _, ch := range res.Changes {
				switch {
				case ch.Removed:
					batch = append(batch, provider.ChangeEvent{FileID: ch.FileId, Kind: provider.ChangeDeleted})
				case ch.File == nil:
					// nothing to act on
				case ch.File.Trashed:
					batch = append(batch, provider.ChangeEvent{FileID: ch.FileId, Kind: provider.ChangeDeleted})
				case filter != nil && !filter.contains(ctx, ch.File):
					batch = append(batch, provider.ChangeEvent{FileID: ch.FileId, Kind: provider.ChangeDeleted})
				default:
					fi := p.toFileInfo(ch.File)
					batch = append(batch, provider.ChangeEvent{FileID: ch.FileId, Kind: provider.ChangeCreated, File: &fi})
				}
			}
			if len(batch) > 0 {
				emit(batch)
			}
		}
		if res.NextPageToken == "" {
			return res.NewStartPageToken, nil
		}
		pageToken = res.NextPageToken
	}
}

// subtreeFilter decides whether files belong to the picked sync root's
// subtree, memoising parent-chain lookups for the whole sync run.
type subtreeFilter struct {
	p    *Provider
	seen map[string]bool
}

func (f *subtreeFilter) contains(ctx context.Context, file *drive.File) bool {
	if len(file.Parents) == 0 {
		return false
	}
	return f.dirInSubtree(ctx, file.Parents[0])
}

// dirInSubtree walks the folder's parent chain until the sync root (in) or
// a parentless folder (out), memoising every hop.
func (f *subtreeFilter) dirInSubtree(ctx context.Context, dirID string) bool {
	if dirID == f.p.syncRoot {
		return true
	}
	if in, ok := f.seen[dirID]; ok {
		return in
	}
	in := false
	if got, err := f.p.svc.Files.Get(dirID).Context(ctx).Fields("parents").Do(); err == nil && len(got.Parents) > 0 {
		in = f.dirInSubtree(ctx, got.Parents[0])
	}
	f.seen[dirID] = in
	return in
}
