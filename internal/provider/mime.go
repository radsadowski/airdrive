// MIME type and preview-kind classification shared by all providers.
package provider

import (
	"mime"
	"path/filepath"
	"strings"
)

var mimeByExt = map[string]string{
	".jpg": "image/jpeg", ".jpeg": "image/jpeg", ".png": "image/png",
	".gif": "image/gif", ".webp": "image/webp", ".heic": "image/heic",
	".svg": "image/svg+xml",
	".mp4": "video/mp4", ".mov": "video/quicktime", ".mkv": "video/x-matroska",
	".mp3": "audio/mpeg", ".wav": "audio/wav", ".flac": "audio/flac",
	".pdf": "application/pdf",
	".txt": "text/plain", ".md": "text/markdown", ".csv": "text/csv",
	".go": "text/x-go", ".ts": "text/typescript", ".tsx": "text/typescript",
	".js": "text/javascript", ".json": "application/json",
	".py": "text/x-python", ".rs": "text/rust", ".sh": "text/x-sh",
	".yml": "text/yaml", ".yaml": "text/yaml",
	".zip": "application/zip", ".tar": "application/x-tar",
	".gz": "application/gzip", ".dmg": "application/x-apple-diskimage",
}

// MimeFor maps a file name (and dir-ness) to a MIME type, falling back to
// the stdlib registry and finally application/octet-stream.
func MimeFor(name string, isDir bool) string {
	if isDir {
		return "inode/directory"
	}
	ext := strings.ToLower(filepath.Ext(name))
	if m, ok := mimeByExt[ext]; ok {
		return m
	}
	if m := mime.TypeByExtension(ext); m != "" {
		return m
	}
	return "application/octet-stream"
}

// PreviewKind classifies a MIME type into the UI's preview categories:
// "image" | "text" | "pdf" | "video" | "audio" | "binary".
func PreviewKind(m string) string {
	switch {
	case strings.HasPrefix(m, "image/"):
		return "image"
	case strings.HasPrefix(m, "video/"):
		return "video"
	case strings.HasPrefix(m, "audio/"):
		return "audio"
	case m == "application/pdf":
		return "pdf"
	case strings.HasPrefix(m, "text/"), m == "application/json":
		return "text"
	default:
		return "binary"
	}
}
