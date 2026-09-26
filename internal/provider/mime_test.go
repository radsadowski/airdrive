package provider

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestWhenTheNameIsMappedThenTheMimeIsResolved(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		file  string
		isDir bool
		want  string
	}{
		{"when the file is a directory then the mime is inode/directory", "anything", true, "inode/directory"},
		{"when the extension is lowercase then the table matches", "photo.png", false, "image/png"},
		{"when the extension is uppercase then it still matches", "PHOTO.JPG", false, "image/jpeg"},
		{"when the name has a compound extension then only the last one counts", "archive.tar.gz", false, "application/gzip"},
		{"when the table misses the extension then the stdlib registry answers", "file.svgz", false, "image/svg+xml"},
		{"when the extension is unknown then it falls back to octet-stream", "blob.datx", false, "application/octet-stream"},
		{"when the name has no extension then it falls back to octet-stream", "Makefile", false, "application/octet-stream"},
		{"when the name is markdown then it maps to text/markdown", "notes.md", false, "text/markdown"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			r := require.New(t)
			r.Equal(tt.want, MimeFor(tt.file, tt.isDir), "MimeFor(%q, %v)", tt.file, tt.isDir)
		})
	}
}

func TestWhenTheMimeIsClassifiedThenThePreviewKindIsResolved(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		mime string
		want string
	}{
		{"when the mime is image/png then the preview is image", "image/png", "image"},
		{"when the mime is image/heic then the preview is image", "image/heic", "image"},
		{"when the mime is video/mp4 then the preview is video", "video/mp4", "video"},
		{"when the mime is audio/mpeg then the preview is audio", "audio/mpeg", "audio"},
		{"when the mime is application/pdf then the preview is pdf", "application/pdf", "pdf"},
		{"when the mime is text/plain then the preview is text", "text/plain", "text"},
		{"when the mime is text/x-go then the preview is text", "text/x-go", "text"},
		{"when the mime is application/json then the preview is text", "application/json", "text"},
		{"when the mime is application/zip then the preview is binary", "application/zip", "binary"},
		{"when the mime is inode/directory then the preview is binary", "inode/directory", "binary"},
		{"when the mime is empty then the preview is binary", "", "binary"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			r := require.New(t)
			r.Equal(tt.want, PreviewKind(tt.mime), "PreviewKind(%q)", tt.mime)
		})
	}
}
