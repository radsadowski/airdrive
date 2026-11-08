package localfs

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/jpeg"
	_ "image/png"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/radosny/airdrive/internal/provider"
)

// writeTestJPEG creates a real w×h JPEG so sips has genuine image input.
func writeTestJPEG(t *testing.T, dir, name string, w, h int) string {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, color.RGBA{R: uint8(x % 256), G: uint8(y % 256), B: 128, A: 255})
		}
	}
	path := filepath.Join(dir, name)
	f, err := os.Create(path)
	require.NoError(t, err)
	defer f.Close()
	require.NoError(t, jpeg.Encode(f, img, nil))
	return path
}

func TestWhenALocalImageThumbnailedThenSipsCapsItAt256px(t *testing.T) {
	t.Parallel()
	r := require.New(t)

	root := t.TempDir()
	img := writeTestJPEG(t, root, "big.jpg", 1024, 512)
	p := New("L1", root)

	b, err := p.Thumbnail(context.Background(), "L1", img, provider.ThumbSmall)
	r.NoError(err)

	cfg, format, err := image.DecodeConfig(bytes.NewReader(b))
	r.NoError(err)
	r.Equal("jpeg", format)
	r.LessOrEqual(cfg.Width, 256, "width must be capped at 256")
	r.LessOrEqual(cfg.Height, 256, "height must be capped at 256")
	r.Greater(cfg.Width, 0)
}

func TestWhenTheLocalFileIsNotAnImageThenThumbnailErrors(t *testing.T) {
	t.Parallel()
	r := require.New(t)

	root := t.TempDir()
	txt := filepath.Join(root, "notes.txt")
	require.NoError(t, os.WriteFile(txt, []byte("hi"), 0o644))
	p := New("L1", root)

	_, err := p.Thumbnail(context.Background(), "L1", txt, provider.ThumbSmall)
	r.Error(err)
}

func TestWhenTheLocalPathEscapesRootThenThumbnailErrors(t *testing.T) {
	t.Parallel()
	r := require.New(t)

	outside := t.TempDir()
	img := writeTestJPEG(t, outside, "secret.jpg", 64, 64)
	p := New("L1", t.TempDir())

	_, err := p.Thumbnail(context.Background(), "L1", img, provider.ThumbSmall)
	r.Error(err)
}

func TestWhenTheFileIsAVideoThenQuickLookRendersIt(t *testing.T) {
	t.Parallel()
	r := require.New(t)

	root := t.TempDir()
	src, err := os.ReadFile("testdata/fixture.mov") // committed tiny AVFoundation-generated clip
	r.NoError(err)
	clip := filepath.Join(root, "clip.mov")
	r.NoError(os.WriteFile(clip, src, 0o644))
	p := New("L1", root)

	b, err := p.Thumbnail(context.Background(), "L1", clip, provider.ThumbSmall)
	r.NoError(err)

	cfg, format, err := image.DecodeConfig(bytes.NewReader(b))
	r.NoError(err, "the QuickLook output must decode as an image")
	r.Contains([]string{"jpeg", "png", "gif"}, format)
	r.LessOrEqual(cfg.Width, provider.ThumbSmall, "width must be capped at the requested size")
	r.LessOrEqual(cfg.Height, provider.ThumbSmall, "height must be capped at the requested size")
	r.Greater(cfg.Width, 0)
}

func TestWhenAVideoFailsToRenderThenTheErrorPropagates(t *testing.T) {
	t.Parallel()
	r := require.New(t)

	root := t.TempDir()
	clip := filepath.Join(root, "bad.mp4")
	r.NoError(os.WriteFile(clip, []byte("this is not a video container at all"), 0o644))
	p := New("L1", root)

	_, err := p.Thumbnail(context.Background(), "L1", clip, provider.ThumbSmall)
	r.Error(err)
}
