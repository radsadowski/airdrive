package localfs

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/radosny/airdrive/internal/provider"
)

// Interface conformance for the optional capability.
var _ provider.Thumbnailer = (*Provider)(nil)

// Thumbnail renders a 256px JPEG via macOS-native sips; the source path is
// abs()-sandboxed under the location root. Formats sips cannot read fail
// with an error, which callers turn into a file-icon fallback.
func (p *Provider) Thumbnail(ctx context.Context, locationID, fileID string) ([]byte, error) {
	path, err := p.abs(fileID)
	if err != nil {
		return nil, err
	}
	if provider.PreviewKind(provider.MimeFor(filepath.Base(path), false)) != provider.PreviewImage {
		return nil, fmt.Errorf("localfs: %q is not an image", filepath.Base(path))
	}

	tmp, err := os.CreateTemp("", "airdrive-thumb-*.jpg")
	if err != nil {
		return nil, fmt.Errorf("localfs: thumb temp: %w", err)
	}
	name := tmp.Name()
	if err := tmp.Close(); err != nil {
		return nil, fmt.Errorf("localfs: thumb temp close: %w", err)
	}
	defer os.Remove(name)

	cmd := exec.CommandContext(ctx, "sips", "-Z", "256", "-s", "format", "jpeg", path, "--out", name) //nolint:gosec // path is abs()-sandboxed under the location root
	if out, err := cmd.CombinedOutput(); err != nil {
		return nil, fmt.Errorf("localfs: sips: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return os.ReadFile(name)
}
