package gdrive

import (
	"context"
	"fmt"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/api/option"

	"github.com/radosny/airdrive/internal/provider"
)

func TestWhenFoldersAreBrowsedThenOnlySubfoldersAreListed(t *testing.T) {
	t.Parallel()
	r := require.New(t)

	fd := newFakeDrive(t, func(w http.ResponseWriter, req *http.Request) {
		if req.Method == http.MethodGet && req.URL.Path == "/drive/v3/files" {
			fmt.Fprint(w, `{"files":[`+
				`{"id":"D1","name":"Photos","mimeType":"application/vnd.google-apps.folder","parents":["ROOTID"],"version":"2"},`+
				`{"id":"D2","name":"Docs","mimeType":"application/vnd.google-apps.folder","parents":["ROOTID"],"version":"5"}`+
				`]}`)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	})
	b, err := NewBrowseClient(context.Background(), staticTS{}, option.WithEndpoint(fd.URL()+"/drive/v3/"))
	r.NoError(err)

	folders, err := b.Folders(context.Background(), "")
	r.NoError(err)
	r.Len(folders, 2)
	r.Equal("D1", folders[0].ID)
	r.Equal(provider.FileKindDir, folders[0].Kind)
	r.Equal("ROOTID", folders[0].ParentID, "the picker navigates by ID — parents stay verbatim")

	reqs := fd.methodReqs()
	r.Len(reqs, 1)
	r.Contains(reqs[0].Query.Get("q"), "'root' in parents")
	r.Contains(reqs[0].Query.Get("q"), "application/vnd.google-apps.folder")
}

func TestWhenASpecificFolderIsBrowsedThenTheQueryScopesToIt(t *testing.T) {
	t.Parallel()
	r := require.New(t)

	fd := newFakeDrive(t, func(w http.ResponseWriter, req *http.Request) {
		if req.Method == http.MethodGet && req.URL.Path == "/drive/v3/files" {
			fmt.Fprint(w, `{"files":[{"id":"D9","name":"Nested","mimeType":"application/vnd.google-apps.folder","parents":["D1"],"version":"1"}]}`)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	})
	b, err := NewBrowseClient(context.Background(), staticTS{}, option.WithEndpoint(fd.URL()+"/drive/v3/"))
	r.NoError(err)

	folders, err := b.Folders(context.Background(), "D1")
	r.NoError(err)
	r.Len(folders, 1)
	r.Equal("D9", folders[0].ID)
	r.Equal("D1", folders[0].ParentID)
	r.Contains(fd.methodReqs()[0].Query.Get("q"), "'D1' in parents")
}

func TestWhenAFolderIsResolvedThenItsMetadataComesBack(t *testing.T) {
	t.Parallel()
	r := require.New(t)

	fd := newFakeDrive(t, func(w http.ResponseWriter, req *http.Request) {
		if req.Method == http.MethodGet && req.URL.Path == "/drive/v3/files/PICKED" {
			fmt.Fprint(w, `{"id":"PICKED","name":"Vacation","mimeType":"application/vnd.google-apps.folder","parents":["ROOTID"],"version":"3"}`)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	})
	b, err := NewBrowseClient(context.Background(), staticTS{}, option.WithEndpoint(fd.URL()+"/drive/v3/"))
	r.NoError(err)

	fi, err := b.Folder(context.Background(), "PICKED")
	r.NoError(err)
	r.Equal("PICKED", fi.ID)
	r.Equal("Vacation", fi.Name)
	r.Equal(provider.FileKindDir, fi.Kind)
}

func TestWhenStarredFoldersAreListedThenTheQueryFiltersToThem(t *testing.T) {
	t.Parallel()
	r := require.New(t)

	fd := newFakeDrive(t, func(w http.ResponseWriter, req *http.Request) {
		if req.Method == http.MethodGet && req.URL.Path == "/drive/v3/files" {
			fmt.Fprint(w, `{"files":[`+
				`{"id":"S1","name":"MacBook Backups","mimeType":"application/vnd.google-apps.folder","parents":["DEVICEROOT"],"starred":true,"version":"1"},`+
				`{"id":"S2","name":"Photos","mimeType":"application/vnd.google-apps.folder","parents":["ROOTID"],"starred":true,"version":"4"}`+
				`]}`)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	})
	b, err := NewBrowseClient(context.Background(), staticTS{}, option.WithEndpoint(fd.URL()+"/drive/v3/"))
	r.NoError(err)

	folders, err := b.StarredFolders(context.Background())
	r.NoError(err)
	r.Len(folders, 2)
	r.Equal("S1", folders[0].ID)
	r.Equal("DEVICEROOT", folders[0].ParentID, "starred Computers-section folders keep their device parents")

	q := fd.methodReqs()[0].Query.Get("q")
	r.Contains(q, "starred = true")
	r.Contains(q, "application/vnd.google-apps.folder")
	r.Contains(q, "trashed = false")
}
