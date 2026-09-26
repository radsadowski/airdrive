// ThumbnailService serves provider thumbnails to the webview.
package bridge

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/radosny/airdrive/internal/index"
	"github.com/radosny/airdrive/internal/provider"
)

const (
	thumbRoute  = "/thumb/"
	thumbMaxAge = 30 * 24 * time.Hour
)

// ThumbnailService serves GET /thumb/{locationID}/{escaped-fileID} from an
// etag-keyed disk cache, falling back to the provider's Thumbnailer. Every
// failure path is a 404 so the UI degrades to the file icon.
type ThumbnailService struct {
	db       *index.DB
	registry *provider.Registry
	logger   *slog.Logger
	cacheDir string
}

// NewThumbnailService creates the service and its cache directory.
func NewThumbnailService(db *index.DB, registry *provider.Registry, logger *slog.Logger, cacheDir string) (*ThumbnailService, error) {
	if err := os.MkdirAll(cacheDir, 0o700); err != nil {
		return nil, fmt.Errorf("thumbnail cache dir: %w", err)
	}
	return &ThumbnailService{db: db, registry: registry, logger: logger, cacheDir: cacheDir}, nil
}

func (s *ThumbnailService) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet || !strings.HasPrefix(r.URL.EscapedPath(), thumbRoute) {
		http.NotFound(w, r)
		return
	}
	rest := strings.TrimPrefix(r.URL.EscapedPath(), thumbRoute)
	parts := strings.Split(rest, "/")
	if len(parts) != 2 {
		http.NotFound(w, r)
		return
	}
	locationID, err := url.PathUnescape(parts[0])
	if err != nil {
		http.NotFound(w, r)
		return
	}
	fileID, err := url.PathUnescape(parts[1])
	if err != nil {
		http.NotFound(w, r)
		return
	}

	ctx := r.Context()
	fi, err := s.db.FileByID(ctx, locationID, fileID)
	if err != nil || fi.LocationID != locationID || fi.PreviewKind != provider.PreviewImage {
		http.NotFound(w, r)
		return
	}

	cachePath := filepath.Join(s.cacheDir, cacheKey(locationID, fileID, fi.Etag))
	if data, rErr := os.ReadFile(cachePath); rErr == nil { //nolint:gosec // cachePath is a sha256-hex file name inside cacheDir, never user input
		s.writeThumb(w, data)
		if tErr := os.Chtimes(cachePath, time.Now(), time.Now()); tErr != nil {
			s.logger.Debug("thumb cache touch", "err", tErr)
		}
		return
	}

	p, err := s.registry.Get(locationID)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	th, ok := p.(provider.Thumbnailer)
	if !ok {
		http.NotFound(w, r)
		return
	}
	data, err := th.Thumbnail(ctx, locationID, fileID)
	if err != nil {
		s.logger.Warn("thumbnail render", "location", locationID, "file", fileID, "err", err)
		http.NotFound(w, r)
		return
	}
	if len(data) == 0 {
		http.NotFound(w, r)
		return
	}
	s.writeCache(cachePath, data)
	s.writeThumb(w, data)
}

// Middleware routes /thumb requests to the service and everything else to
// the next handler — handed to wails AssetOptions.Middleware.
func (s *ThumbnailService) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.EscapedPath(), thumbRoute) {
			s.ServeHTTP(w, r)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// Sweep drops cache entries whose mtime is older than a month.
func (s *ThumbnailService) Sweep() {
	entries, err := os.ReadDir(s.cacheDir)
	if err != nil {
		s.logger.Warn("thumb sweep", "err", err)
		return
	}
	for _, e := range entries {
		info, iErr := e.Info()
		if iErr != nil {
			continue
		}
		if time.Since(info.ModTime()) > thumbMaxAge {
			if rmErr := os.Remove(filepath.Join(s.cacheDir, e.Name())); rmErr != nil {
				s.logger.Warn("thumb sweep remove", "file", e.Name(), "err", rmErr)
			}
		}
	}
}

func (s *ThumbnailService) writeThumb(w http.ResponseWriter, data []byte) {
	w.Header().Set("Content-Type", http.DetectContentType(data))
	w.Header().Set("Cache-Control", "private, max-age=86400")
	_, _ = w.Write(data) //nolint:gosec // content type is sniffed via DetectContentType, so bytes can never render as HTML
}

// writeCache persists atomically (temp + rename) so a concurrent reader
// never observes a partial file.
func (s *ThumbnailService) writeCache(path string, data []byte) {
	tmp, err := os.CreateTemp(s.cacheDir, "*.tmp")
	if err != nil {
		s.logger.Warn("thumb cache write", "err", err)
		return
	}
	name := tmp.Name()
	if _, wErr := tmp.Write(data); wErr != nil {
		s.logger.Warn("thumb cache write", "err", wErr)
	}
	if cErr := tmp.Close(); cErr != nil {
		s.logger.Warn("thumb cache write", "err", cErr)
	}
	if rmErr := os.Rename(name, path); rmErr != nil { //nolint:gosec // both paths are inside cacheDir; the target is a sha256-hex name
		os.Remove(name)
		s.logger.Warn("thumb cache rename", "err", rmErr)
	}
}

// cacheKey hashes the identity triple so any etag change invalidates.
func cacheKey(locationID, fileID, etag string) string {
	sum := sha256.Sum256([]byte(locationID + "\x00" + fileID + "\x00" + etag))
	return hex.EncodeToString(sum[:]) + ".thumb"
}
