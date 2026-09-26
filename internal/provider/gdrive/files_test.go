package gdrive

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/radosny/airdrive/internal/provider"
)

const fileF1JSON = `{"id":"F1","name":"a.png","mimeType":"image/png","parents":["ROOTID"],"size":"123","version":"7","modifiedTime":"2024-05-06T07:08:09Z"}`

func TestWhenRootIsListedThenItQueriesTheRootAliasAndPaginates(t *testing.T) {
	t.Parallel()
	r := require.New(t)

	p, fd := newTestProvider(t, func(w http.ResponseWriter, req *http.Request) {
		if req.Method == http.MethodGet && req.URL.Path == "/drive/v3/files" {
			if req.URL.Query().Get("pageToken") == "" {
				fmt.Fprint(w, `{"files":[`+fileF1JSON+`],"nextPageToken":"PT2"}`)
				return
			}
			fmt.Fprint(w, `{"files":[{"id":"F2","name":"b.txt","mimeType":"text/plain","parents":["ROOTID"],"version":"2"}]}`)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	})

	page, err := p.List(context.Background(), "loc1", "", provider.SortSpec{Field: provider.SortByName, Asc: true}, "", 0)
	r.NoError(err)
	r.Equal("PT2", page.Cursor)
	r.Len(page.Items, 1)
	r.Equal("F1", page.Items[0].ID)
	r.Equal("a.png", page.Items[0].Name)
	r.Equal("image", page.Items[0].PreviewKind)

	reqs := fd.methodReqs()
	r.Len(reqs, 1)
	r.Equal("'root' in parents and trashed = false", reqs[0].Query.Get("q"))
	r.Equal("folder,name asc", reqs[0].Query.Get("orderBy"))
	r.Equal("1000", reqs[0].Query.Get("pageSize"))

	page, err = p.List(context.Background(), "loc1", "", provider.SortSpec{Field: provider.SortByName, Asc: true}, "PT2", 0)
	r.NoError(err)
	r.Empty(page.Cursor)
	r.Len(page.Items, 1)
	r.Equal("F2", page.Items[0].ID)
	reqs = fd.methodReqs()
	r.Len(reqs, 2)
	r.Equal("PT2", reqs[1].Query.Get("pageToken"))
}

func TestWhenANestedDirIsListedThenQueryAndPageSizeMapOver(t *testing.T) {
	t.Parallel()
	r := require.New(t)

	p, fd := newTestProvider(t, func(w http.ResponseWriter, req *http.Request) {
		if req.Method == http.MethodGet && req.URL.Path == "/drive/v3/files" {
			fmt.Fprint(w, `{"files":[]}`)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	})

	_, err := p.List(context.Background(), "loc1", "FOLDERX", provider.SortSpec{Field: provider.SortByModifiedAt, Asc: false}, "", 50)
	r.NoError(err)

	reqs := fd.methodReqs()
	r.Len(reqs, 1)
	r.Equal("'FOLDERX' in parents and trashed = false", reqs[0].Query.Get("q"))
	r.Equal("folder,modifiedTime desc", reqs[0].Query.Get("orderBy"))
	r.Equal("50", reqs[0].Query.Get("pageSize"))
}

func TestWhenAFileIsStattedThenItsMetadataMapsToFileInfo(t *testing.T) {
	t.Parallel()
	r := require.New(t)

	p, _ := newTestProvider(t, func(w http.ResponseWriter, req *http.Request) {
		if req.Method == http.MethodGet && req.URL.Path == "/drive/v3/files/F1" {
			fmt.Fprint(w, fileF1JSON)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	})

	fi, err := p.Stat(context.Background(), "loc1", "F1")
	r.NoError(err)
	r.Equal("F1", fi.ID)
	r.Equal("a.png", fi.Name)
	r.Equal(provider.FileKindFile, fi.Kind)
	r.Equal(int64(123), fi.Size)
	r.Equal("7", fi.Etag)
}

func TestWhenMkdirRunsThenADriveFolderIsCreatedWithParentBody(t *testing.T) {
	t.Parallel()
	r := require.New(t)

	p, fd := newTestProvider(t, func(w http.ResponseWriter, req *http.Request) {
		if req.Method == http.MethodPost && req.URL.Path == "/drive/v3/files" {
			fmt.Fprint(w, `{"id":"N1","name":"New Folder","mimeType":"application/vnd.google-apps.folder","parents":["FOLDERX"],"version":"1"}`)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	})

	fi, err := p.Mkdir(context.Background(), "loc1", "FOLDERX", "New Folder")
	r.NoError(err)
	r.Equal(provider.FileKindDir, fi.Kind)
	r.Equal("N1", fi.ID)
	r.Equal("FOLDERX", fi.ParentID)

	body := fd.methodReqs()[0].Body
	r.Contains(body, `"mimeType":"application/vnd.google-apps.folder"`)
	r.Contains(body, `"name":"New Folder"`)
	r.Contains(body, `"parents":["FOLDERX"]`)
}

func TestWhenMkdirTargetsRootThenTheResolvedRootIDIsUsed(t *testing.T) {
	t.Parallel()
	r := require.New(t)

	p, fd := newTestProvider(t, func(w http.ResponseWriter, req *http.Request) {
		if req.Method == http.MethodPost && req.URL.Path == "/drive/v3/files" {
			fmt.Fprint(w, `{"id":"N2","name":"R","mimeType":"application/vnd.google-apps.folder","parents":["ROOTID"],"version":"1"}`)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	})

	_, err := p.Mkdir(context.Background(), "loc1", "", "R")
	r.NoError(err)
	r.Contains(fd.methodReqs()[0].Body, `"parents":["ROOTID"]`, "empty parent must map to the resolved root ID, not the 'root' alias")
}

func TestWhenARenameRunsThenTheNameIsPatched(t *testing.T) {
	t.Parallel()
	r := require.New(t)

	p, fd := newTestProvider(t, func(w http.ResponseWriter, req *http.Request) {
		if req.Method == http.MethodPatch && req.URL.Path == "/drive/v3/files/F1" {
			fmt.Fprint(w, `{"id":"F1","name":"renamed.png","mimeType":"image/png","parents":["ROOTID"],"version":"8"}`)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	})

	fi, err := p.Rename(context.Background(), "loc1", "F1", "renamed.png")
	r.NoError(err)
	r.Equal("renamed.png", fi.Name)
	r.Equal("8", fi.Etag)
	r.Contains(fd.methodReqs()[0].Body, `"name":"renamed.png"`)
}

func TestWhenFilesAreMovedThenParentsAreFetchedAndPatched(t *testing.T) {
	t.Parallel()
	r := require.New(t)

	p, fd := newTestProvider(t, func(w http.ResponseWriter, req *http.Request) {
		switch {
		case req.Method == http.MethodGet && req.URL.Path == "/drive/v3/files/M1":
			fmt.Fprint(w, `{"id":"M1","parents":["OLD"]}`)
		case req.Method == http.MethodPatch && req.URL.Path == "/drive/v3/files/M1":
			fmt.Fprint(w, `{}`)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	})

	err := p.MoveWithin(context.Background(), "loc1", []string{"M1"}, "NEWDEST")
	r.NoError(err)

	reqs := fd.methodReqs()
	r.Len(reqs, 2, "get parents, then patch")
	r.Equal("NEWDEST", reqs[1].Query.Get("addParents"))
	r.Equal("OLD", reqs[1].Query.Get("removeParents"))
}

func TestWhenAFileIsMovedToRootThenTheResolvedRootIDIsUsed(t *testing.T) {
	t.Parallel()
	r := require.New(t)

	p, fd := newTestProvider(t, func(w http.ResponseWriter, req *http.Request) {
		switch {
		case req.Method == http.MethodGet && req.URL.Path == "/drive/v3/files/M1":
			fmt.Fprint(w, `{"id":"M1","parents":["OLD"]}`)
		case req.Method == http.MethodPatch && req.URL.Path == "/drive/v3/files/M1":
			fmt.Fprint(w, `{}`)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	})

	err := p.MoveWithin(context.Background(), "loc1", []string{"M1"}, "")
	r.NoError(err)
	r.Equal("ROOTID", fd.methodReqs()[1].Query.Get("addParents"))
}

func TestWhenAFileIsDeletedThenItIsTrashed(t *testing.T) {
	t.Parallel()
	r := require.New(t)

	p, fd := newTestProvider(t, func(w http.ResponseWriter, req *http.Request) {
		if req.Method == http.MethodPatch && req.URL.Path == "/drive/v3/files/F1" {
			fmt.Fprint(w, `{}`)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	})

	err := p.Delete(context.Background(), "loc1", []string{"F1"})
	r.NoError(err)
	r.Contains(fd.methodReqs()[0].Body, `"trashed":true`)
}

func TestWhenABinaryIsDownloadedThenMetadataAndStreamAreReturned(t *testing.T) {
	t.Parallel()
	r := require.New(t)

	p, fd := newTestProvider(t, func(w http.ResponseWriter, req *http.Request) {
		if req.Method == http.MethodGet && req.URL.Path == "/drive/v3/files/B1" {
			if req.URL.Query().Get("alt") == "media" {
				fmt.Fprint(w, "PNGDATA")
				return
			}
			fmt.Fprint(w, `{"id":"B1","name":"pic.png","mimeType":"image/png","parents":["ROOTID"],"size":"7","version":"3"}`)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	})

	rc, fi, err := p.Download(context.Background(), "loc1", "B1")
	r.NoError(err)
	defer rc.Close()
	b, err := io.ReadAll(rc)
	r.NoError(err)
	r.Equal("PNGDATA", string(b))
	r.Equal("pic.png", fi.Name)
	r.Len(fd.methodReqs(), 2, "metadata + media")
}

func TestWhenAGoogleNativeFileIsDownloadedThenItFailsFastWithoutFetchingMedia(t *testing.T) {
	t.Parallel()
	r := require.New(t)

	p, fd := newTestProvider(t, func(w http.ResponseWriter, req *http.Request) {
		if req.Method == http.MethodGet && req.URL.Path == "/drive/v3/files/D1" {
			fmt.Fprint(w, `{"id":"D1","name":"Report","mimeType":"application/vnd.google-apps.document","parents":["ROOTID"],"version":"12"}`)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	})

	rc, _, err := p.Download(context.Background(), "loc1", "D1")
	r.Error(err)
	r.Nil(rc)
	r.Contains(err.Error(), "not supported")
	r.Contains(err.Error(), "Report")
	r.Len(fd.methodReqs(), 1, "must not fetch media for Google-native files")
}

func TestWhenAShortcutIsDownloadedThenItFailsFast(t *testing.T) {
	t.Parallel()
	r := require.New(t)

	p, _ := newTestProvider(t, func(w http.ResponseWriter, req *http.Request) {
		if req.Method == http.MethodGet && req.URL.Path == "/drive/v3/files/S1" {
			fmt.Fprint(w, `{"id":"S1","name":"link","mimeType":"application/vnd.google-apps.shortcut","parents":["ROOTID"]}`)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	})

	rc, _, err := p.Download(context.Background(), "loc1", "S1")
	r.Error(err)
	r.Nil(rc)
}

func TestWhenADirectoryIsDownloadedThenItFailsFast(t *testing.T) {
	t.Parallel()
	r := require.New(t)

	p, _ := newTestProvider(t, func(w http.ResponseWriter, req *http.Request) {
		if req.Method == http.MethodGet && req.URL.Path == "/drive/v3/files/D9" {
			fmt.Fprint(w, `{"id":"D9","name":"Folder","mimeType":"application/vnd.google-apps.folder","parents":["ROOTID"]}`)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	})

	rc, _, err := p.Download(context.Background(), "loc1", "D9")
	r.Error(err)
	r.Nil(rc)
	r.True(strings.Contains(err.Error(), "director"), err.Error())
}

func TestWhenAFileIsUploadedThenAMultipartCreateIsSentWithDerivedMime(t *testing.T) {
	t.Parallel()
	r := require.New(t)

	p, fd := newTestProvider(t, func(w http.ResponseWriter, req *http.Request) {
		if req.Method == http.MethodPost && req.URL.Path == "/upload/drive/v3/files" {
			fmt.Fprint(w, `{"id":"U1","name":"pic.png","mimeType":"image/png","parents":["FOLDERX"],"size":"4","version":"1"}`)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	})

	fi, err := p.Upload(context.Background(), "loc1", "FOLDERX", "pic.png", strings.NewReader("DATA"), 4)
	r.NoError(err)
	r.Equal("U1", fi.ID)
	r.Equal("image/png", fi.MIME)

	reqs := fd.methodReqs()
	r.Len(reqs, 1)
	r.Equal("multipart", reqs[0].Query.Get("uploadType"))
	r.Contains(reqs[0].Body, `"name":"pic.png"`)
	r.Contains(reqs[0].Body, `"mimeType":"image/png"`)
	r.Contains(reqs[0].Body, "DATA")
}
