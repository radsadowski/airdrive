package s3compat

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/minio/minio-go/v7"
	"github.com/stretchr/testify/require"

	"github.com/radosny/airdrive/internal/provider"
)

type s3Req struct {
	Method string
	Path   string
	Query  string
	Body   string
	Header http.Header
}

type fakeObject struct {
	data []byte
	etag string
	mod  time.Time
}

// fakeS3 is an in-memory single-bucket S3: listings, HEAD/GET/PUT/DELETE and
// server-side copies are applied to a real object map so tests assert
// behavior, not just request shapes.
type fakeS3 struct {
	srv     *httptest.Server
	mu      sync.Mutex
	reqs    []s3Req
	objects map[string]fakeObject
	bucket  string
}

func newFakeS3(t *testing.T, seed map[string]string) *fakeS3 {
	t.Helper()
	f := &fakeS3{objects: map[string]fakeObject{}, bucket: "content"}
	now := time.Date(2024, 5, 6, 7, 8, 9, 0, time.UTC)
	for k, v := range seed {
		f.objects[k] = fakeObject{data: []byte(v), etag: `"etag-${k}"`, mod: now}
	}
	f.srv = httptest.NewServer(http.HandlerFunc(f.handle))
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fakeS3) handle(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	f.mu.Lock()
	f.reqs = append(f.reqs, s3Req{Method: r.Method, Path: r.URL.Path, Query: r.URL.RawQuery, Body: string(body), Header: r.Header.Clone()})
	f.mu.Unlock()

	if r.URL.Path == "/" {
		if r.Method == http.MethodGet {
			fmt.Fprint(w, `<?xml version="1.0" encoding="UTF-8"?>
<ListAllMyBucketsResult xmlns="http://s3.amazonaws.com/doc/2006-03-01/">
  <Owner><ID>1</ID></Owner>
  <Buckets><Bucket><Name>content</Name><CreationDate>2024-01-01T00:00:00.000Z</CreationDate></Bucket></Buckets>
</ListAllMyBucketsResult>`)
			return
		}
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	rest := strings.TrimPrefix(r.URL.Path, "/"+f.bucket+"/")
	if rest == r.URL.Path {
		w.WriteHeader(http.StatusNotFound)
		return
	}

	switch r.Method {
	case http.MethodGet:
		if r.URL.Query().Get("list-type") == "2" {
			f.listObjects(w, r)
			return
		}
		f.mu.Lock()
		obj, ok := f.objects[rest]
		f.mu.Unlock()
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("ETag", obj.etag)
		w.Header().Set("Last-Modified", obj.mod.UTC().Format(http.TimeFormat))
		_, _ = w.Write(obj.data)
	case http.MethodHead:
		f.mu.Lock()
		obj, ok := f.objects[rest]
		f.mu.Unlock()
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("ETag", obj.etag)
		w.Header().Set("Last-Modified", obj.mod.UTC().Format(http.TimeFormat))
		w.Header().Set("Content-Length", fmt.Sprint(len(obj.data)))
		w.WriteHeader(http.StatusOK)
	case http.MethodPut:
		src := r.Header.Get("x-amz-copy-source")
		f.mu.Lock()
		if src != "" {
			srcKey := copySourceKey(src, f.bucket)
			if obj, ok := f.objects[srcKey]; ok {
				f.objects[rest] = fakeObject{data: obj.data, etag: obj.etag, mod: time.Now()}
			}
		} else {
			f.objects[rest] = fakeObject{data: decodeChunked(body), etag: `"up-${rest}"`, mod: time.Now()}
		}
		f.mu.Unlock()
		w.Header().Set("ETag", `"copied"`)
		fmt.Fprint(w, `<?xml version="1.0" encoding="UTF-8"?>
<CopyObjectResult xmlns="http://s3.amazonaws.com/doc/2006-03-01/">
  <LastModified>2024-05-06T07:08:09.000Z</LastModified><ETag>"copied"</ETag>
</CopyObjectResult>`)
	case http.MethodDelete:
		f.mu.Lock()
		delete(f.objects, rest)
		f.mu.Unlock()
		w.WriteHeader(http.StatusNoContent)
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

// copySourceKey decodes the x-amz-copy-source header ("/bucket/key" or
// "bucket/key", URL-encoded or not) down to the raw object key.
func copySourceKey(src, bucket string) string {
	if dec, err := url.QueryUnescape(src); err == nil {
		src = dec
	}
	src = strings.TrimPrefix(src, "/"+bucket+"/")
	src = strings.TrimPrefix(src, bucket+"/")
	return src
}

// decodeChunked strips aws-chunked framing from streaming PUT bodies.
func decodeChunked(b []byte) []byte {
	if !bytes.Contains(b, []byte("chunk-signature=")) {
		return b
	}
	var out []byte
	for len(b) > 0 {
		nl := bytes.Index(b, []byte("\r\n"))
		if nl < 0 {
			break
		}
		sz := string(b[:nl])
		if i := strings.Index(sz, ";"); i >= 0 {
			sz = sz[:i]
		}
		n, err := strconv.ParseInt(strings.TrimSpace(sz), 16, 64)
		if err != nil || n == 0 {
			break
		}
		b = b[nl+2:]
		if int64(len(b)) < n {
			out = append(out, b...)
			break
		}
		out = append(out, b[:n]...)
		b = b[n:]
		if bytes.HasPrefix(b, []byte("\r\n")) {
			b = b[2:]
		}
	}
	return out
}

// listObjects derives a real delimiter listing from the object map.
func (f *fakeS3) listObjects(w http.ResponseWriter, r *http.Request) {
	prefix := r.URL.Query().Get("prefix")
	delimiter := r.URL.Query().Get("delimiter")

	f.mu.Lock()
	defer f.mu.Unlock()
	var files []string
	dirs := map[string]bool{}
	for k := range f.objects {
		if !strings.HasPrefix(k, prefix) {
			continue
		}
		rest := k[len(prefix):]
		if delimiter != "" {
			if i := strings.Index(rest, delimiter); i >= 0 {
				dirs[prefix+rest[:i+1]] = true
				continue
			}
		}
		files = append(files, k)
	}
	sort.Strings(files)
	dirList := make([]string, 0, len(dirs))
	for d := range dirs {
		dirList = append(dirList, d)
	}
	sort.Strings(dirList)

	var b strings.Builder
	b.WriteString(`<?xml version="1.0" encoding="UTF-8"?>`)
	b.WriteString(`<ListBucketResult xmlns="http://s3.amazonaws.com/doc/2006-03-01/">`)
	fmt.Fprintf(&b, "<Name>%s</Name><Prefix>%s</Prefix><KeyCount>%d</KeyCount><MaxKeys>1000</MaxKeys>", f.bucket, prefix, len(files)+len(dirList))
	if delimiter != "" {
		fmt.Fprintf(&b, "<Delimiter>%s</Delimiter>", delimiter)
	}
	b.WriteString("<IsTruncated>false</IsTruncated>")
	for _, k := range files {
		o := f.objects[k]
		fmt.Fprintf(&b, "<Contents><Key>%s</Key><LastModified>%s</LastModified><ETag>%s</ETag><Size>%d</Size><StorageClass>STANDARD</StorageClass></Contents>",
			k, o.mod.UTC().Format("2006-01-02T15:04:05.000Z"), o.etag, len(o.data))
	}
	for _, d := range dirList {
		fmt.Fprintf(&b, "<CommonPrefixes><Prefix>%s</Prefix></CommonPrefixes>", d)
	}
	b.WriteString(`</ListBucketResult>`)
	_, _ = w.Write([]byte(b.String()))
}

func (f *fakeS3) objectKeys() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]string, 0, len(f.objects))
	for k := range f.objects {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func (f *fakeS3) objectData(key string) ([]byte, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	o, ok := f.objects[key]
	return o.data, ok
}

func newTestProvider(t *testing.T, seed map[string]string) (*Provider, *fakeS3) {
	t.Helper()
	f := newFakeS3(t, seed)
	p, err := New("L1", Config{Endpoint: f.srv.URL, Region: "auto", Bucket: "content", PathStyle: true}, Credentials{AccessKeyID: "AK", SecretAccessKey: "SK"}, nil)
	require.NoError(t, err)
	return p, f
}

func TestWhenADirectoryIsListedThenFilesAndPrefixesComeBack(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	p, _ := newTestProvider(t, map[string]string{
		"photos/2024/a.png": "PNGDATA",
		"photos/2024/b.png": "MORE",
		"photos/video.mp4":  "VID",
		"readme.txt":        "hi",
	})

	page, err := p.List(context.Background(), "L1", "", provider.SortSpec{Field: provider.SortByName, Asc: true}, "", 0)
	r.NoError(err)

	byID := map[string]provider.FileInfo{}
	for _, it := range page.Items {
		byID[it.ID] = it
	}
	r.Len(byID, 2, "root lists readme.txt + the photos/ prefix")
	r.Equal(provider.FileKindFile, byID["readme.txt"].Kind)
	r.Equal("", byID["readme.txt"].ParentID)
	r.Equal(provider.FileKindDir, byID["photos/"].Kind)
	r.Equal("", byID["photos/"].ParentID)
	r.Equal("photos", byID["photos/"].Name)

	page, err = p.List(context.Background(), "L1", "photos/", provider.SortSpec{Field: provider.SortByName, Asc: true}, "", 0)
	r.NoError(err)
	byID = map[string]provider.FileInfo{}
	for _, it := range page.Items {
		byID[it.ID] = it
	}
	r.Len(byID, 2, "photos/ lists video.mp4 + 2024/ prefix")
	r.Equal("photos/", byID["photos/2024/"].ParentID)
}

func TestWhenAFileIsStattedThenItsMetadataMapsToFileInfo(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	p, _ := newTestProvider(t, map[string]string{"photos/a.png": "PNGDATA"})

	fi, err := p.Stat(context.Background(), "L1", "photos/a.png")
	r.NoError(err)
	r.Equal("photos/a.png", fi.ID)
	r.Equal("photos/", fi.ParentID)
	r.Equal("a.png", fi.Name)
	r.Equal(provider.FileKindFile, fi.Kind)
	r.Equal(int64(7), fi.Size)
	r.Equal("image/png", fi.MIME)
	r.NotEmpty(fi.Etag)
	r.False(fi.ModifiedAt.IsZero())
}

func TestWhenMkdirRunsThenAMarkerObjectIsCreated(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	p, f := newTestProvider(t, nil)

	fi, err := p.Mkdir(context.Background(), "L1", "", "backup")
	r.NoError(err)
	r.Equal("backup/", fi.ID)
	r.Equal(provider.FileKindDir, fi.Kind)

	data, ok := f.objectData("backup/")
	r.True(ok, "the dir is a zero-byte marker object")
	r.Empty(data)
}

func TestWhenAFileIsRenamedThenItIsCopiedAndRemoved(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	p, f := newTestProvider(t, map[string]string{"photos/a.png": "PNGDATA"})

	fi, err := p.Rename(context.Background(), "L1", "photos/a.png", "b.png")
	r.NoError(err)
	r.Equal("photos/b.png", fi.ID)

	_, ok := f.objectData("photos/b.png")
	r.True(ok, "renamed object exists")
	_, ok = f.objectData("photos/a.png")
	r.False(ok, "old object removed")
}

func TestWhenADirectoryIsRenamedThenItsContentsMove(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	p, f := newTestProvider(t, map[string]string{
		"photos/2024/":      "",
		"photos/2024/a.png": "PNGDATA",
	})

	_, err := p.Rename(context.Background(), "L1", "photos/2024/", "2025")
	r.NoError(err)

	data, ok := f.objectData("photos/2025/a.png")
	r.True(ok, "nested object moved under the new prefix")
	r.Equal("PNGDATA", string(data))
	_, ok = f.objectData("photos/2024/a.png")
	r.False(ok, "old prefix contents removed")
}

func TestWhenFilesAreMovedThenTheyAreCopiedAndRemoved(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	p, f := newTestProvider(t, map[string]string{"a.png": "A", "b.txt": "B"})

	err := p.MoveWithin(context.Background(), "L1", []string{"a.png", "b.txt"}, "sub/")
	r.NoError(err)

	_, ok := f.objectData("sub/a.png")
	r.True(ok)
	_, ok = f.objectData("a.png")
	r.False(ok)
	_, ok = f.objectData("b.txt")
	r.False(ok)
}

func TestWhenAFileIsDeletedThenItMovesToTheInBucketTrash(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	p, f := newTestProvider(t, map[string]string{"photos/a.png": "PNGDATA"})

	err := p.Delete(context.Background(), "L1", []string{"photos/a.png"})
	r.NoError(err)

	_, ok := f.objectData("photos/a.png")
	r.False(ok, "original removed")

	trashed := f.objectKeys()
	r.Len(trashed, 1)
	r.True(strings.HasPrefix(trashed[0], ".airdrive-trash/"), "trashed under the prefix: %s", trashed[0])
	r.True(strings.HasSuffix(trashed[0], "/photos/a.png"), "original key preserved under the timestamp")
	data, _ := f.objectData(trashed[0])
	r.Equal("PNGDATA", string(data))
}

func TestWhenADirectoryIsDeletedThenItsContentsAreTrashed(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	p, f := newTestProvider(t, map[string]string{
		"photos/2024/":      "",
		"photos/2024/a.png": "PNGDATA",
	})

	err := p.Delete(context.Background(), "L1", []string{"photos/2024/"})
	r.NoError(err)

	var trashedPNG string
	for _, k := range f.objectKeys() {
		if strings.HasSuffix(k, "/photos/2024/a.png") {
			trashedPNG = k
		}
	}
	r.NotEmpty(trashedPNG, "the nested object lives under the trash prefix with its original key")
	data, ok := f.objectData(trashedPNG)
	r.True(ok)
	r.Equal("PNGDATA", string(data))
	for _, k := range f.objectKeys() {
		r.True(strings.HasPrefix(k, ".airdrive-trash/"), "everything moved to trash: %s", k)
	}
}

func TestWhenAFileIsDownloadedThenTheStreamAndInfoComeBack(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	p, _ := newTestProvider(t, map[string]string{"photos/a.png": "PNGDATA"})

	rc, fi, err := p.Download(context.Background(), "L1", "photos/a.png")
	r.NoError(err)
	defer rc.Close()
	b, err := io.ReadAll(rc)
	r.NoError(err)
	r.Equal("PNGDATA", string(b))
	r.Equal("a.png", fi.Name)
}

func TestWhenAFileIsUploadedThenItLandsWithAMime(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	p, f := newTestProvider(t, nil)

	fi, err := p.Upload(context.Background(), "L1", "photos", "pic.png", bytes.NewReader([]byte("DATA")), 4)
	r.NoError(err)
	r.Equal("photos/pic.png", fi.ID)
	r.Equal("image/png", fi.MIME)

	data, ok := f.objectData("photos/pic.png")
	r.True(ok)
	r.Equal("DATA", string(data))
}

func TestWhenTheBucketProviderIsAskedThenItAlwaysReconciles(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	p, _ := newTestProvider(t, nil)
	r.True(p.Reconciles())
}

func TestWhenTheErrorIsAMinioResponseThenItIsDescribedWithStatusAndRequestID(t *testing.T) {
	t.Parallel()
	r := require.New(t)

	resp := minio.ErrorResponse{
		Code:       "AccessDenied",
		Message:    "Access Denied.",
		StatusCode: http.StatusForbidden,
		RequestID:  "tx-abc-123",
	}
	r.Equal("AccessDenied (HTTP 403, request tx-abc-123)", DescribeError(resp))
	r.Equal("AccessDenied (HTTP 403, request tx-abc-123)", DescribeError(&minio.ErrorResponse{
		Code: "AccessDenied", StatusCode: http.StatusForbidden, RequestID: "tx-abc-123",
	}))

	plain := errors.New("plain failure")
	r.Equal("plain failure", DescribeError(plain))
}
