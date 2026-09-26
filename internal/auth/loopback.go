package auth

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"time"

	"golang.org/x/oauth2"
)

const loopbackTimeout = 3 * time.Minute

// GdriveToken is what we persist per Google Drive location.
type GdriveToken struct {
	AccessToken  string    `json:"accessToken"`
	RefreshToken string    `json:"refreshToken"`
	Expiry       time.Time `json:"expiry"`
	TokenType    string    `json:"tokenType"`
}

func fromOAuthToken(t *oauth2.Token) GdriveToken {
	return GdriveToken{
		AccessToken:  t.AccessToken,
		RefreshToken: t.RefreshToken,
		Expiry:       t.Expiry,
		TokenType:    t.TokenType,
	}
}

func (t GdriveToken) toOAuthToken() *oauth2.Token {
	return &oauth2.Token{
		AccessToken:  t.AccessToken,
		RefreshToken: t.RefreshToken,
		Expiry:       t.Expiry,
		TokenType:    t.TokenType,
	}
}

// SignInResult is returned after a full interactive sign-in completes.
type SignInResult struct {
	Token GdriveToken
}

// openBrowser abstracts app.Browser.OpenURL so this package stays
// Wails-agnostic (only internal/bridge imports Wails).
type openBrowserFunc func(url string) error

// SignIn runs one full interactive OAuth flow:
//  1. start a loopback HTTP server on a random 127.0.0.1 port
//  2. build the Google authorize URL (PKCE + state) with that port's redirect
//  3. call openBrowser to show it to the user
//  4. wait for Google to redirect back with ?code=...&state=...
//  5. exchange the code for tokens
//
// It blocks until the flow completes, fails, or ctx is canceled/times out.
func SignIn(ctx context.Context, openBrowser openBrowserFunc) (SignInResult, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return SignInResult{}, fmt.Errorf("loopback listen: %w", err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	redirectURL := fmt.Sprintf("http://127.0.0.1:%d/callback", port)

	pkce, err := newPKCEPair()
	if err != nil {
		return SignInResult{}, fmt.Errorf("pkce: %w", err)
	}
	state, err := newState()
	if err != nil {
		return SignInResult{}, fmt.Errorf("state: %w", err)
	}

	cfg := oauthConfig(redirectURL)
	authURL := cfg.AuthCodeURL(state,
		oauth2.AccessTypeOffline, // request a refresh token
		oauth2.SetAuthURLParam("code_challenge", pkce.Challenge),
		oauth2.SetAuthURLParam("code_challenge_method", "S256"),
		oauth2.SetAuthURLParam("prompt", "consent"), // ensures refresh_token on re-auth
	)

	type result struct {
		code string
		err  error
	}
	resultCh := make(chan result, 1)

	mux := http.NewServeMux()
	mux.HandleFunc("/callback", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if errMsg := q.Get("error"); errMsg != "" {
			writeCallbackPage(w, false)
			resultCh <- result{err: fmt.Errorf("google returned error: %s", errMsg)}
			return
		}
		if q.Get("state") != state {
			writeCallbackPage(w, false)
			resultCh <- result{err: errors.New("state mismatch — possible CSRF, aborting")}
			return
		}
		code := q.Get("code")
		if code == "" {
			writeCallbackPage(w, false)
			resultCh <- result{err: errors.New("no code in callback")}
			return
		}
		writeCallbackPage(w, true)
		resultCh <- result{code: code}
	})
	srv := &http.Server{Handler: mux}
	go func() { _ = srv.Serve(ln) }()
	defer func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdownCtx)
	}()

	if err := openBrowser(authURL); err != nil {
		return SignInResult{}, fmt.Errorf("open browser: %w", err)
	}

	timeoutCtx, cancel := context.WithTimeout(ctx, loopbackTimeout)
	defer cancel()

	select {
	case <-timeoutCtx.Done():
		return SignInResult{}, fmt.Errorf("sign-in timed out or was canceled: %w", timeoutCtx.Err())
	case res := <-resultCh:
		if res.err != nil {
			return SignInResult{}, res.err
		}
		tok, err := cfg.Exchange(timeoutCtx, res.code,
			oauth2.SetAuthURLParam("code_verifier", pkce.Verifier),
		)
		if err != nil {
			return SignInResult{}, fmt.Errorf("token exchange: %w", err)
		}
		return SignInResult{Token: fromOAuthToken(tok)}, nil
	}
}

// TokenSource returns an oauth2.TokenSource that silently refreshes the
// access token using the stored refresh token, forever, via cfg's endpoint.
// The caller should persist the token again if it has changed (check
// tok.AccessToken / tok.Expiry after each use, or wrap with oauth2.ReuseTokenSource).
func TokenSource(ctx context.Context, tok GdriveToken) oauth2.TokenSource {
	// redirectURL is irrelevant for refresh — only ClientID/Secret/Endpoint matter.
	cfg := oauthConfig("")
	return cfg.TokenSource(ctx, tok.toOAuthToken())
}

func writeCallbackPage(w http.ResponseWriter, ok bool) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if ok {
		fmt.Fprint(w, `<html><body style="font:14px -apple-system,sans-serif;text-align:center;padding-top:80px;color:#333">
			<h2>AirDrive connected ✓</h2><p>You can close this tab and return to AirDrive.</p></body></html>`)
		return
	}
	w.WriteHeader(http.StatusBadRequest)
	fmt.Fprint(w, `<html><body style="font:14px -apple-system,sans-serif;text-align:center;padding-top:80px;color:#333">
		<h2>Sign-in failed</h2><p>Please return to AirDrive and try again.</p></body></html>`)
}
