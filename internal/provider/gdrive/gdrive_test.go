package gdrive

import (
	"context"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"google.golang.org/api/drive/v3"
	"google.golang.org/api/option"

	"github.com/radosny/airdrive/internal/provider"
)

func TestWhenTheProviderIsBuiltThenItResolvesTheRealRootIDAndAuthenticates(t *testing.T) {
	t.Parallel()
	r := require.New(t)

	fd := newFakeDrive(t, func(w http.ResponseWriter, req *http.Request) {
		t.Errorf("unexpected request: %s %s", req.Method, req.URL.Path)
	})
	p, err := New(context.Background(), "loc1", staticTS{}, discardLogger(), "", option.WithEndpoint(fd.URL()+"/drive/v3/"))
	r.NoError(err)
	r.Equal("ROOTID", p.rootID)
	r.Equal("loc1", p.ID())
	r.Equal(provider.KindGDrive, p.Kind())

	reqs := fd.requests()
	r.Len(reqs, 1, "New should issue exactly one root-resolution request")
	r.Equal("Bearer test-token", reqs[0].Header.Get("Authorization"))
}

func TestWhenNewIsGivenARootFolderThenItIsUsedAsTheSyncRoot(t *testing.T) {
	t.Parallel()
	r := require.New(t)

	fd := newFakeDrive(t, func(w http.ResponseWriter, req *http.Request) {
		if req.Method == http.MethodGet && req.URL.Path == "/drive/v3/files/PICKED" {
			fmt.Fprint(w, `{"id":"PICKED","name":"Vacation","mimeType":"application/vnd.google-apps.folder"}`)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	})
	p, err := New(context.Background(), "loc1", staticTS{}, discardLogger(), "PICKED", option.WithEndpoint(fd.URL()+"/drive/v3/"))
	r.NoError(err)
	r.Equal("PICKED", p.rootID)
}

func TestWhenNewIsGivenAnUnknownRootFolderThenItErrors(t *testing.T) {
	t.Parallel()
	r := require.New(t)

	fd := newFakeDrive(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	})
	_, err := New(context.Background(), "loc1", staticTS{}, discardLogger(), "NOPE", option.WithEndpoint(fd.URL()+"/drive/v3/"))
	r.Error(err)
}

func TestWhenADriveFileIsMappedThenItBecomesAFileInfo(t *testing.T) {
	t.Parallel()
	p := &Provider{locationID: "loc1", rootID: "ROOTID"}
	modAt, _ := time.Parse(time.RFC3339, "2024-05-06T07:08:09Z")

	tests := []struct {
		name string
		in   *drive.File
		want provider.FileInfo
	}{
		{
			name: "when the file is a folder in root then parentID is empty",
			in: &drive.File{
				Id: "F1", Name: "Photos",
				MimeType:     "application/vnd.google-apps.folder",
				Parents:      []string{"ROOTID"},
				Version:      3,
				ModifiedTime: "2024-05-06T07:08:09Z",
			},
			want: provider.FileInfo{
				ID: "F1", LocationID: "loc1", ParentID: "", Name: "Photos",
				Kind: provider.FileKindDir, Size: 0, MIME: "application/vnd.google-apps.folder",
				Etag: "3", PreviewKind: "binary", ModifiedAt: modAt,
			},
		},
		{
			name: "when the folder is nested then parentID is its parent",
			in: &drive.File{
				Id: "F2", Name: "Nested", MimeType: "application/vnd.google-apps.folder",
				Parents: []string{"FOLDERX"},
			},
			want: provider.FileInfo{
				ID: "F2", LocationID: "loc1", ParentID: "FOLDERX", Name: "Nested",
				Kind: provider.FileKindDir, MIME: "application/vnd.google-apps.folder", Etag: "0", PreviewKind: "binary",
			},
		},
		{
			name: "when the file is a regular binary then version becomes the etag",
			in: &drive.File{
				Id: "B1", Name: "a.png", MimeType: "image/png",
				Parents: []string{"FOLDERX"}, Size: 123, Version: 7,
				ModifiedTime: "2024-05-06T07:08:09Z",
			},
			want: provider.FileInfo{
				ID: "B1", LocationID: "loc1", ParentID: "FOLDERX", Name: "a.png",
				Kind: provider.FileKindFile, Size: 123, MIME: "image/png",
				Etag: "7", PreviewKind: "image", ModifiedAt: modAt,
			},
		},
		{
			name: "when the file is a Google-native document then it gets no preview",
			in: &drive.File{
				Id: "D1", Name: "Report", MimeType: "application/vnd.google-apps.document",
				Parents: []string{"ROOTID"}, Version: 12,
			},
			want: provider.FileInfo{
				ID: "D1", LocationID: "loc1", ParentID: "", Name: "Report",
				Kind: provider.FileKindFile, MIME: "application/vnd.google-apps.document",
				Etag: "12", PreviewKind: "binary",
			},
		},
		{
			name: "when the file is a shortcut then it gets no preview",
			in: &drive.File{
				Id: "S1", Name: "link", MimeType: "application/vnd.google-apps.shortcut",
				Parents: []string{"ROOTID"},
			},
			want: provider.FileInfo{
				ID: "S1", LocationID: "loc1", ParentID: "", Name: "link",
				Kind: provider.FileKindFile, MIME: "application/vnd.google-apps.shortcut",
				Etag: "0", PreviewKind: "binary",
			},
		},
		{
			name: "when the file has no parents then parentID is empty",
			in: &drive.File{
				Id: "O1", Name: "orphan", MimeType: "text/plain",
			},
			want: provider.FileInfo{
				ID: "O1", LocationID: "loc1", ParentID: "", Name: "orphan",
				Kind: provider.FileKindFile, MIME: "text/plain", Etag: "0", PreviewKind: "text",
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			r := require.New(t)
			r.Equal(tt.want, p.toFileInfo(tt.in))
		})
	}
}
