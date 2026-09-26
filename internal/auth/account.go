package auth

import (
	"context"
	"fmt"

	"golang.org/x/oauth2"
	"google.golang.org/api/drive/v3"
	"google.golang.org/api/option"
)

// AccountInfo is the minimal identity we need after sign-in — just enough
// to give the location a human-readable name in the sidebar.
type AccountInfo struct {
	Email string
	Name  string
}

// FetchAccountInfo calls Drive's about.get to identify who just signed in.
// This is the cheapest possible authenticated call (no file listing) and
// doubles as a sanity check that the token actually works before we
// persist the location.
func FetchAccountInfo(ctx context.Context, ts oauth2.TokenSource) (AccountInfo, error) {
	svc, err := drive.NewService(ctx, option.WithTokenSource(ts))
	if err != nil {
		return AccountInfo{}, fmt.Errorf("drive service: %w", err)
	}
	about, err := svc.About.Get().Fields("user(displayName,emailAddress)").Do()
	if err != nil {
		return AccountInfo{}, fmt.Errorf("about.get: %w", err)
	}
	if about.User == nil {
		return AccountInfo{}, fmt.Errorf("about.get: no user in response")
	}
	return AccountInfo{Email: about.User.EmailAddress, Name: about.User.DisplayName}, nil
}
