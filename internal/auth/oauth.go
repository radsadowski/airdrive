// Package auth implements the Google OAuth2 (PKCE) sign-in flow and secure
// token storage. It is used by internal/bridge to connect Google Drive
// locations; nothing outside this package talks to Google's OAuth endpoints
// or the OS keychain directly.
package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
)

// TODO(gdrive-setup): replace with your real Google Cloud OAuth Client ID
// (type: Desktop app). Desktop-app client "secrets" are not confidential per
// Google's own OAuth guidance (https://developers.google.com/identity/protocols/oauth2/native-app)
// — they identify the app, not authenticate it — so compiling them into the
// binary is the documented approach, not a security shortcut.
const (
	gdriveClientID     = "128822057748-0ifmurij7mor5r5c04rgh3k39pr3mijp.apps.googleusercontent.com" //nolint:gosec // desktop-app client ID — not confidential per Google's OAuth native-app guidance
	gdriveClientSecret = "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"                                      //nolint:gosec // compiled in per Google's documented approach for installed apps
)

// DriveScopes requests full CRUD on My Drive (matches the plan's v1 scope:
// "My Drive only, full CRUD"). drive.file (per-file grants) would be more
// conservative but breaks browsing pre-existing files, which we need.
var driveScopes = []string{"https://www.googleapis.com/auth/drive"}

// oauthConfig returns the OAuth2 config for a given loopback redirect URL.
// redirectURL must be http://127.0.0.1:<port>/callback — the port is chosen
// fresh per sign-in attempt by the loopback server.
func oauthConfig(redirectURL string) *oauth2.Config {
	return &oauth2.Config{
		ClientID:     gdriveClientID,
		ClientSecret: gdriveClientSecret,
		Endpoint:     google.Endpoint,
		RedirectURL:  redirectURL,
		Scopes:       driveScopes,
	}
}

// pkcePair is a PKCE verifier/challenge pair (RFC 7636, S256 method).
type pkcePair struct {
	Verifier  string
	Challenge string
}

// newPKCEPair generates a fresh random verifier and its S256 challenge.
func newPKCEPair() (pkcePair, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return pkcePair{}, err
	}
	verifier := base64.RawURLEncoding.EncodeToString(raw)
	sum := sha256.Sum256([]byte(verifier))
	challenge := base64.RawURLEncoding.EncodeToString(sum[:])
	return pkcePair{Verifier: verifier, Challenge: challenge}, nil
}

// newState generates a random anti-CSRF state token for the auth request.
func newState() (string, error) {
	raw := make([]byte, 16)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}
