package gdrive

import (
	"context"
	"fmt"
	"io"
	"net/http"

	"github.com/radosny/airdrive/internal/provider"
)

// Interface conformance for the optional capability.
var _ provider.Thumbnailer = (*Provider)(nil)

// Thumbnail fetches the file's server-side thumbnail. Drive's thumbnailLink
// is short-lived and only resolves with the authenticated OAuth client.
func (p *Provider) Thumbnail(ctx context.Context, locationID, fileID string) ([]byte, error) {
	f, err := p.svc.Files.Get(fileID).Context(ctx).Fields("thumbnailLink").Do()
	if err != nil {
		return nil, fmt.Errorf("gdrive: thumbnail metadata for %q: %w", fileID, err)
	}
	if f.ThumbnailLink == "" {
		return nil, fmt.Errorf("gdrive: no thumbnail for %q", fileID)
	}
	resp, err := p.client.Get(f.ThumbnailLink)
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
