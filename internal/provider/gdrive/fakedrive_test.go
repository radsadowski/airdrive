package gdrive

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync"
	"testing"

	"golang.org/x/oauth2"
	"google.golang.org/api/option"
)

// staticTS hands out a fixed access token so no OAuth refresh traffic
// happens in tests.
type staticTS struct{}

func (staticTS) Token() (*oauth2.Token, error) {
	return &oauth2.Token{AccessToken: "test-token", TokenType: "Bearer"}, nil
}

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// fakeReq is one request as seen by the fake Drive server.
type fakeReq struct {
	Method string
	Path   string
	Query  url.Values
	Body   string
	Header http.Header
}

type fakeDrive struct {
	srv  *httptest.Server
	mu   sync.Mutex
	reqs []fakeReq
}

// newFakeDrive serves a fake Drive API: every request is recorded, the
// root stub is always served, everything else routes to the per-test handler h.
func newFakeDrive(t *testing.T, h http.HandlerFunc) *fakeDrive {
	t.Helper()
	fd := &fakeDrive{}
	fd.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		r.Body = io.NopCloser(bytes.NewReader(body))
		fd.mu.Lock()
		fd.reqs = append(fd.reqs, fakeReq{
			Method: r.Method,
			Path:   r.URL.Path,
			Query:  r.URL.Query(),
			Body:   string(body),
			Header: r.Header.Clone(),
		})
		fd.mu.Unlock()
		if r.Method == http.MethodGet && r.URL.Path == "/drive/v3/files/root" {
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"id":"ROOTID"}`)
			return
		}
		h(w, r)
	}))
	t.Cleanup(fd.srv.Close)
	return fd
}

func (fd *fakeDrive) URL() string { return fd.srv.URL }

func (fd *fakeDrive) requests() []fakeReq {
	fd.mu.Lock()
	defer fd.mu.Unlock()
	out := make([]fakeReq, len(fd.reqs))
	copy(out, fd.reqs)
	return out
}

// methodReqs returns recorded requests excluding New's root bootstrap call,
// so indices refer to the method under test.
func (fd *fakeDrive) methodReqs() []fakeReq {
	var out []fakeReq
	for _, r := range fd.requests() {
		if r.Path == "/drive/v3/files/root" {
			continue
		}
		out = append(out, r)
	}
	return out
}

// newTestProvider builds a Provider against a fake Drive server driven by h.
func newTestProvider(t *testing.T, h http.HandlerFunc) (*Provider, *fakeDrive) {
	t.Helper()
	return newTestProviderRoot(t, "", h)
}

// newTestProviderRoot is newTestProvider with an explicit sync root folder ID.
func newTestProviderRoot(t *testing.T, rootID string, h http.HandlerFunc) (*Provider, *fakeDrive) {
	t.Helper()
	fd := newFakeDrive(t, h)
	p, err := New(context.Background(), "loc1", staticTS{}, discardLogger(), rootID, option.WithEndpoint(fd.URL()+"/drive/v3/"))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return p, fd
}
