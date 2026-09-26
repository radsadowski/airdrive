package localfs

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/jpeg"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
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

	b, err := p.Thumbnail(context.Background(), "L1", img)
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

	_, err := p.Thumbnail(context.Background(), "L1", txt)
	r.Error(err)
}

func TestWhenTheLocalPathEscapesRootThenThumbnailErrors(t *testing.T) {
	t.Parallel()
	r := require.New(t)

	outside := t.TempDir()
	img := writeTestJPEG(t, outside, "secret.jpg", 64, 64)
	p := New("L1", t.TempDir())

	_, err := p.Thumbnail(context.Background(), "L1", img)
	r.Error(err)
}
