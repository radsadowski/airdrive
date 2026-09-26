package auth

import (
	"errors"
	"io"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/require"
	"golang.org/x/oauth2"
)

// scriptedTS returns tokens in order, then repeats the last one.
type scriptedTS struct {
	tokens []*oauth2.Token
	calls  int
}

func (s *scriptedTS) Token() (*oauth2.Token, error) {
	i := s.calls
	if i >= len(s.tokens) {
		i = len(s.tokens) - 1
	}
	s.calls++
	return s.tokens[i], nil
}

func newTestPersisting(initial *oauth2.Token, inner oauth2.TokenSource, save func(GdriveToken) error) oauth2.TokenSource {
	return newPersistingTokenSource(initial, inner, save, slog.New(slog.NewTextHandler(io.Discard, nil)))
}

func TestWhenTheRefreshTokenRotatesThenItIsPersisted(t *testing.T) {
	t.Parallel()
	r := require.New(t)

	tok1 := &oauth2.Token{AccessToken: "A1", RefreshToken: "RT1"}
	tok2 := &oauth2.Token{AccessToken: "A2", RefreshToken: "RT2"}
	inner := &scriptedTS{tokens: []*oauth2.Token{tok2}}

	var saved []GdriveToken
	ts := newTestPersisting(tok1, inner, func(tok GdriveToken) error {
		saved = append(saved, tok)
		return nil
	})

	got, err := ts.Token()
	r.NoError(err)
	r.Equal("A2", got.AccessToken)
	r.Equal([]GdriveToken{fromOAuthToken(tok2)}, saved, "rotated refresh token must be persisted")
}

func TestWhenTheRefreshTokenIsUnchangedThenNothingIsSaved(t *testing.T) {
	t.Parallel()
	r := require.New(t)

	tok1 := &oauth2.Token{AccessToken: "A1", RefreshToken: "RT1"}
	tok2 := &oauth2.Token{AccessToken: "A2", RefreshToken: "RT1"} // new access token, same refresh token
	inner := &scriptedTS{tokens: []*oauth2.Token{tok2}}

	saved := 0
	ts := newTestPersisting(tok1, inner, func(GdriveToken) error {
		saved++
		return nil
	})

	_, err := ts.Token()
	r.NoError(err)
	r.Equal(0, saved, "same refresh token must not trigger a save")
}

func TestWhenTheNewTokenHasNoRefreshTokenThenNothingIsSaved(t *testing.T) {
	t.Parallel()
	r := require.New(t)

	tok1 := &oauth2.Token{AccessToken: "A1", RefreshToken: "RT1"}
	tok2 := &oauth2.Token{AccessToken: "A2"}
	inner := &scriptedTS{tokens: []*oauth2.Token{tok2}}

	saved := 0
	ts := newTestPersisting(tok1, inner, func(GdriveToken) error {
		saved++
		return nil
	})

	got, err := ts.Token()
	r.NoError(err)
	r.Equal("A2", got.AccessToken)
	r.Equal(0, saved, "empty refresh token must never overwrite the stored one")
}

func TestWhenSavingFailsThenTokenDeliveryStillSucceeds(t *testing.T) {
	t.Parallel()
	r := require.New(t)

	tok1 := &oauth2.Token{AccessToken: "A1", RefreshToken: "RT1"}
	tok2 := &oauth2.Token{AccessToken: "A2", RefreshToken: "RT2"}
	inner := &scriptedTS{tokens: []*oauth2.Token{tok2}}

	ts := newTestPersisting(tok1, inner, func(GdriveToken) error {
		return errors.New("keychain unavailable")
	})

	got, err := ts.Token()
	r.NoError(err, "save failure must not break token delivery")
	r.Equal("A2", got.AccessToken)
}
