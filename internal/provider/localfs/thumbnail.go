package localfs

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/radosny/airdrive/internal/provider"
)

// Interface conformance for the optional capability.
var _ provider.Thumbnailer = (*Provider)(nil)

// Thumbnail renders a size-pixel image via macOS-native tools: sips for
// still images, QuickLook for videos. The source path is abs()-sandboxed
// under the location root; unreadable formats fail with an error, which
// callers turn into a file-icon fallback.
func (p *Provider) Thumbnail(ctx context.Context, locationID, fileID string, size int) ([]byte, error) {
	path, err := p.abs(fileID)
	if err != nil {
		return nil, err
	}
	switch kind := provider.PreviewKind(provider.MimeFor(filepath.Base(path), false)); kind {
	case provider.PreviewImage:
		return thumbImage(ctx, path, size)
	case "video":
		return thumbVideo(ctx, path, size)
	default:
		return nil, fmt.Errorf("localfs: %q cannot be thumbnailed (kind %s)", filepath.Base(path), kind)
	}
}

// thumbImage downscales a still image to a size-pixel JPEG via sips.
func thumbImage(ctx context.Context, path string, size int) ([]byte, error) {
	tmp, err := os.CreateTemp("", "airdrive-thumb-*.jpg")
	if err != nil {
		return nil, fmt.Errorf("localfs: thumb temp: %w", err)
	}
	name := tmp.Name()
	if err := tmp.Close(); err != nil {
		return nil, fmt.Errorf("localfs: thumb temp close: %w", err)
	}
	defer os.Remove(name)

	cmd := exec.CommandContext( //nolint:gosec // path is abs()-sandboxed under the location root; size comes from the bridge's allowlist
		ctx,
		"sips",
		"-Z",
		strconv.Itoa(size),
		"-s",
		"format",
		"jpeg",
		path,
		"--out",
		name,
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		return nil, fmt.Errorf("localfs: sips: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return os.ReadFile(name)
}

// thumbVideo renders a size-pixel frame of a video via QuickLook; qlmanage
// writes <basename>.png into the out dir, so the file is found by glob
// rather than a derived name. Its output goes to a log file rather than a
// pipe: QuickLookSatellite outlives qlmanage while keeping inherited pipe
// fds open, which would otherwise hang cmd.Run in Wait. QuickLook can
// wedge for minutes on malformed containers, so the whole render is
// capped by a timeout, and the output file is polled briefly because
// qlmanage sometimes returns before the satellite finishes writing.
func thumbVideo(ctx context.Context, path string, size int) ([]byte, error) {
	dir, err := os.MkdirTemp("", "airdrive-ql-*")
	if err != nil {
		return nil, fmt.Errorf("localfs: ql temp dir: %w", err)
	}
	defer os.RemoveAll(dir)

	logPath := filepath.Join(dir, "qlmanage.log")
	logFile, err := os.Create(logPath) //nolint:gosec // logPath is a fixed name inside the temp dir created above
	if err != nil {
		return nil, fmt.Errorf("localfs: ql log: %w", err)
	}
	defer logFile.Close()

	renderCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext( //nolint:gosec // path is abs()-sandboxed under the location root; size comes from the bridge's allowlist
		renderCtx,
		"qlmanage",
		"-t",
		"-s",
		strconv.Itoa(size),
		path,
		"-o",
		dir,
	)
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	if err := cmd.Run(); err != nil {
		logTail, _ := os.ReadFile(logPath) //nolint:gosec // logPath is a fixed name inside the temp dir created above
		return nil, fmt.Errorf("localfs: qlmanage: %w: %s", err, strings.TrimSpace(string(logTail)))
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		matches, _ := filepath.Glob(filepath.Join(dir, "*.png"))
		if len(matches) == 1 {
			if data, rErr := os.ReadFile(matches[0]); rErr == nil && len(data) > 0 { //nolint:gosec // matches[0] is a globbed name inside the temp dir created above
				return data, nil
			}
		}
		if time.Now().After(deadline) {
			return nil, fmt.Errorf("localfs: qlmanage produced no thumbnail for %q", filepath.Base(path))
		}
		time.Sleep(100 * time.Millisecond)
	}
}
