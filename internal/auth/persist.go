// Token rotation persistence — loopback.go's documented contract.
package auth

import (
	"context"
	"log/slog"
	"sync"

	"golang.org/x/oauth2"
)

// NewPersistingTokenSource returns a TokenSource that refreshes access
// tokens and re-saves the credential whenever Google rotates the refresh
// token — otherwise a rotation dies with the app process and the location
// must be re-connected.
func NewPersistingTokenSource(ctx context.Context, logger *slog.Logger, locationID string, tok GdriveToken) oauth2.TokenSource {
	if logger == nil {
		logger = slog.Default()
	}
	inner := TokenSource(ctx, tok)
	save := func(t GdriveToken) error { return SaveToken(locationID, t) }
	return newPersistingTokenSource(tok.toOAuthToken(), inner, save, logger)
}

func newPersistingTokenSource(initial *oauth2.Token, inner oauth2.TokenSource, save func(GdriveToken) error, logger *slog.Logger) oauth2.TokenSource {
	return &persistingTokenSource{
		last:   initial,
		inner:  inner,
		save:   save,
		logger: logger,
	}
}

// persistingTokenSource persists genuine rotations only: a new non-empty
// refresh token differing from the last seen. An empty refresh token never
// overwrites the stored credential; a failed save never breaks delivery.
type persistingTokenSource struct {
	mu     sync.Mutex
	last   *oauth2.Token
	inner  oauth2.TokenSource
	save   func(GdriveToken) error
	logger *slog.Logger
}

func (s *persistingTokenSource) Token() (*oauth2.Token, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	tok, err := s.inner.Token()
	if err != nil {
		return nil, err
	}
	if tok != nil && tok.RefreshToken != "" && (s.last == nil || tok.RefreshToken != s.last.RefreshToken) {
		if serr := s.save(fromOAuthToken(tok)); serr != nil {
			s.logger.Warn("google token: persist rotated token", "err", serr)
		}
	}
	s.last = tok
	return tok, nil
}
