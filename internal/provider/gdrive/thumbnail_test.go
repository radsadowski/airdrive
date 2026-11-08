package gdrive

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/radosny/airdrive/internal/provider"

	"google.golang.org/api/option"
)

func TestWhenAThumbnailIsRequestedThenItIsFetchedAuthenticated(t *testing.T) {
	t.Parallel()
	r := require.New(t)

	var fd *fakeDrive
	var thumbAuth string
	fd = newFakeDrive(t, func(w http.ResponseWriter, req *http.Request) {
		switch req.URL.Path {
		case "/drive/v3/files/F1":
			fmt.Fprintf(w, `{"id":"F1","thumbnailLink":%q}`, fd.URL()+"/thumbdata")
		case "/thumbdata":
			thumbAuth = req.Header.Get("Authorization")
			fmt.Fprint(w, "THUMBBYTES")
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	})
	p, err := New(context.Background(), "loc1", staticTS{}, discardLogger(), "", option.WithEndpoint(fd.URL()+"/drive/v3/"))
	r.NoError(err)

	b, err := p.Thumbnail(context.Background(), "loc1", "F1", provider.ThumbSmall)
	r.NoError(err)
	r.Equal([]byte("THUMBBYTES"), b)
	r.Equal("Bearer test-token", thumbAuth, "thumbnailLink fetch must carry the OAuth token")
}

func TestWhenTheFileHasNoThumbnailLinkThenThumbnailErrors(t *testing.T) {
	t.Parallel()
	r := require.New(t)

	p, _ := newTestProvider(t, func(w http.ResponseWriter, req *http.Request) {
		if req.Method == http.MethodGet && req.URL.Path == "/drive/v3/files/D1" {
			fmt.Fprint(w, `{"id":"D1","name":"empty"}`)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	})

	_, err := p.Thumbnail(context.Background(), "loc1", "D1", provider.ThumbSmall)
	r.Error(err)
}

func TestWhenTheThumbnailFetchFailsThenTheErrorPropagates(t *testing.T) {
	t.Parallel()
	r := require.New(t)

	var fd *fakeDrive
	fd = newFakeDrive(t, func(w http.ResponseWriter, req *http.Request) {
		if req.Method == http.MethodGet && req.URL.Path == "/drive/v3/files/F1" {
			fmt.Fprintf(w, `{"id":"F1","thumbnailLink":%q}`, fd.URL()+"/thumbdata")
			return
		}
		if req.URL.Path == "/thumbdata" {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	})
	p, err := New(context.Background(), "loc1", staticTS{}, discardLogger(), "", option.WithEndpoint(fd.URL()+"/drive/v3/"))
	r.NoError(err)

	_, err = p.Thumbnail(context.Background(), "loc1", "F1", provider.ThumbSmall)
	r.Error(err)
}

func TestWhenTheThumbnailLinkCarriesASizeTokenThenItIsRewrittenToTheRequestedSize(t *testing.T) {
	t.Parallel()
	r := require.New(t)

	var fd *fakeDrive
	var gotURI, thumbAuth string
	fd = newFakeDrive(t, func(w http.ResponseWriter, req *http.Request) {
		switch req.URL.Path {
		case "/drive/v3/files/F1":
			fmt.Fprintf(w, `{"id":"F1","thumbnailLink":%q}`, fd.URL()+"/thumbdata=s220")
		default:
			if !strings.HasPrefix(req.URL.Path, "/thumbdata") {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			gotURI = req.URL.String()
			thumbAuth = req.Header.Get("Authorization")
			fmt.Fprint(w, "THUMBBYTES")
		}
	})
	p, err := New(context.Background(), "loc1", staticTS{}, discardLogger(), "", option.WithEndpoint(fd.URL()+"/drive/v3/"))
	r.NoError(err)

	b, err := p.Thumbnail(context.Background(), "loc1", "F1", provider.ThumbLarge)
	r.NoError(err)
	r.Equal([]byte("THUMBBYTES"), b)
	r.Equal("/thumbdata=s1024", gotURI, "the =s220 size token must be rewritten to the requested size")
	r.Equal("Bearer test-token", thumbAuth)
}
