package gdrive

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/radosny/airdrive/internal/provider"
)

// Interface conformance for the optional capability.
var _ provider.Thumbnailer = (*Provider)(nil)

// Thumbnail fetches the file's server-side thumbnail. Drive's thumbnailLink
// is short-lived and only resolves with the authenticated OAuth client.
// The link carries a size token (=s220) that is rewritten to the requested
// size so previews are sharp while tiles stay cheap.
func (p *Provider) Thumbnail(ctx context.Context, locationID, fileID string, size int) ([]byte, error) {
	f, err := p.svc.Files.Get(fileID).Context(ctx).Fields("thumbnailLink").Do()
	if err != nil {
		return nil, fmt.Errorf("gdrive: thumbnail metadata for %q: %w", fileID, err)
	}
	if f.ThumbnailLink == "" {
		return nil, fmt.Errorf("gdrive: no thumbnail for %q", fileID)
	}
	resp, err := p.client.Get(sizedThumbLink(f.ThumbnailLink, size))
	if err != nil {
		return nil, fmt.Errorf("gdrive: fetch thumbnail for %q: %w", fileID, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("gdrive: fetch thumbnail for %q: status %d", fileID, resp.StatusCode)
	}
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("gdrive: read thumbnail for %q: %w", fileID, err)
	}
	return b, nil
}

// sizedThumbLink rewrites a Drive thumbnail's trailing =s<px> size token to
// size. Links without the token are returned unchanged: appending one is not
// sanctioned by the API.
func sizedThumbLink(link string, size int) string {
	const marker = "=s"
	i := strings.LastIndex(link, marker)
	if i < 0 {
		return link
	}
	for _, ch := range link[i+len(marker):] {
		if ch < '0' || ch > '9' {
			return link
		}
	}
	return link[:i] + marker + strconv.Itoa(size)
}

