package gdrive

import (
	"context"
	"fmt"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/radosny/airdrive/internal/provider"
)

func TestWhenTheCursorIsEmptyThenAFullWalkEnumeratesEverythingAndReturnsTheStartToken(t *testing.T) {
	t.Parallel()
	r := require.New(t)

	p, fd := newTestProvider(t, func(w http.ResponseWriter, req *http.Request) {
		switch {
		case req.Method == http.MethodGet && req.URL.Path == "/drive/v3/changes/startPageToken":
			fmt.Fprint(w, `{"startPageToken":"SPT1"}`)
		case req.Method == http.MethodGet && req.URL.Path == "/drive/v3/files":
			if req.URL.Query().Get("pageToken") == "" {
				fmt.Fprint(w, `{"files":[`+fileF1JSON+`],"nextPageToken":"W2"}`)
				return
			}
			fmt.Fprint(w, `{"files":[{"id":"F2","name":"b.txt","mimeType":"text/plain","parents":["ROOTID"],"version":"2"}]}`)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	})

	var events []provider.ChangeEvent
	cursor, err := p.SyncChanges(context.Background(), "loc1", "", func(batch []provider.ChangeEvent) {
		events = append(events, batch...)
	})
	r.NoError(err)
	r.Equal("SPT1", cursor, "full walk must return the start token captured before enumeration")
	r.Len(events, 2)
	r.Equal("F1", events[0].FileID)
	r.Equal("F2", events[1].FileID)
	r.Equal(provider.ChangeCreated, events[0].Kind)
	r.Equal(provider.ChangeCreated, events[1].Kind)
	r.NotNil(events[0].File)

	reqs := fd.methodReqs()
	r.Len(reqs, 3, "startPageToken + two list pages")
	r.Equal("trashed = false", reqs[1].Query.Get("q"))
	r.Equal("1000", reqs[1].Query.Get("pageSize"))
	r.Equal("W2", reqs[2].Query.Get("pageToken"))
}

func TestWhenTheCursorIsAChangesTokenThenRemovedTrashedAndLiveChangesMapToEvents(t *testing.T) {
	t.Parallel()
	r := require.New(t)

	p, fd := newTestProvider(t, func(w http.ResponseWriter, req *http.Request) {
		if req.Method == http.MethodGet && req.URL.Path == "/drive/v3/changes" {
			switch req.URL.Query().Get("pageToken") {
			case "CUR1":
				fmt.Fprint(w, `{"changes":[`+
					`{"fileId":"GONE","removed":true},`+
					`{"fileId":"F2","file":{"id":"F2","name":"x.txt","mimeType":"text/plain","trashed":true}},`+
					`{"fileId":"F3","file":{"id":"F3","name":"c.txt","mimeType":"text/plain","parents":["ROOTID"],"version":"9"}}`+
					`],"nextPageToken":"CH2"}`)
			case "CH2":
				fmt.Fprint(w, `{"changes":[{"fileId":"F4","file":{"id":"F4","name":"d.txt","mimeType":"text/plain","parents":["ROOTID"],"version":"1"}}],"newStartPageToken":"NSPT2"}`)
			default:
				w.WriteHeader(http.StatusNotFound)
			}
			return
		}
		w.WriteHeader(http.StatusNotFound)
	})

	var events []provider.ChangeEvent
	cursor, err := p.SyncChanges(context.Background(), "loc1", "CUR1", func(batch []provider.ChangeEvent) {
		events = append(events, batch...)
	})
	r.NoError(err)
	r.Equal("NSPT2", cursor)

	r.Len(events, 4)
	r.Equal(provider.ChangeEvent{FileID: "GONE", Kind: provider.ChangeDeleted}, events[0])
	r.Equal(provider.ChangeDeleted, events[1].Kind)
	r.Equal("F2", events[1].FileID)
	r.Equal(provider.ChangeCreated, events[2].Kind)
	r.Equal("F3", events[2].FileID)
	r.NotNil(events[2].File)
	r.Equal(provider.ChangeCreated, events[3].Kind)
	r.Equal("F4", events[3].FileID)

	reqs := fd.methodReqs()
	r.Len(reqs, 2, "two changes pages")
	r.Equal("true", reqs[0].Query.Get("includeRemoved"))
	r.Equal("CUR1", reqs[0].Query.Get("pageToken"))
	r.Equal("CH2", reqs[1].Query.Get("pageToken"))
}

func TestWhenTheChangesFeedFailsThenTheErrorPropagates(t *testing.T) {
	t.Parallel()
	r := require.New(t)

	p, _ := newTestProvider(t, func(w http.ResponseWriter, req *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	})

	_, err := p.SyncChanges(context.Background(), "loc1", "CUR1", func([]provider.ChangeEvent) {})
	r.Error(err)
}

func TestWhenARootFolderIsPickedThenTheFullWalkStaysInsideIt(t *testing.T) {
	t.Parallel()
	r := require.New(t)

	h := func(w http.ResponseWriter, req *http.Request) {
		switch {
		case req.URL.Path == "/drive/v3/changes/startPageToken":
			fmt.Fprint(w, `{"startPageToken":"SPT1"}`)
		case req.Method == http.MethodGet && req.URL.Path == "/drive/v3/files/PICKED":
			fmt.Fprint(w, `{"id":"PICKED","name":"Vacation","mimeType":"application/vnd.google-apps.folder","parents":["ROOTID"]}`)
		case req.Method == http.MethodGet && req.URL.Path == "/drive/v3/files":
			switch req.URL.Query().Get("q") {
			case "'PICKED' in parents and trashed = false":
				fmt.Fprint(w, `{"files":[`+
					`{"id":"F1","name":"a.png","mimeType":"image/png","parents":["PICKED"],"size":"123","version":"7"},`+
					`{"id":"SUB","name":"Nested","mimeType":"application/vnd.google-apps.folder","parents":["PICKED"],"version":"1"}`+
					`]}`)
			case "'SUB' in parents and trashed = false":
				fmt.Fprint(w, `{"files":[{"id":"F2","name":"b.txt","mimeType":"text/plain","parents":["SUB"],"version":"2"}]}`)
			default:
				fmt.Fprint(w, `{"files":[{"id":"OUTSIDE","name":"decoy.txt","mimeType":"text/plain"}]}`)
			}
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}
	p, fd := newTestProviderRoot(t, "PICKED", h)

	var events []provider.ChangeEvent
	cursor, err := p.SyncChanges(context.Background(), "loc1", "", func(batch []provider.ChangeEvent) {
		events = append(events, batch...)
	})
	r.NoError(err)
	r.Equal("SPT1", cursor)

	ids := map[string]provider.ChangeKind{}
	for _, e := range events {
		ids[e.FileID] = e.Kind
	}
	r.Equal(map[string]provider.ChangeKind{
		"F1":  provider.ChangeCreated,
		"SUB": provider.ChangeCreated,
		"F2":  provider.ChangeCreated,
	}, ids, "the root folder itself and anything outside it must not be emitted")

	for _, req2 := range fd.methodReqs() {
		if req2.Path == "/drive/v3/files" {
			r.Contains(req2.Query.Get("q"), "in parents", "a picked root must never trigger whole-Drive enumeration")
		}
	}
}

func TestWhenChangesFallOutsideThePickedSubtreeThenTheyAreDeleted(t *testing.T) {
	t.Parallel()
	r := require.New(t)

	h := func(w http.ResponseWriter, req *http.Request) {
		switch req.URL.Path {
		case "/drive/v3/files/PICKED":
			fmt.Fprint(w, `{"id":"PICKED","name":"Vacation","mimeType":"application/vnd.google-apps.folder","parents":["ROOTID"]}`)
		case "/drive/v3/changes":
			if req.URL.Query().Get("pageToken") == "CUR1" {
				fmt.Fprint(w, `{"changes":[`+
					`{"fileId":"IN1","file":{"id":"IN1","name":"in.txt","mimeType":"text/plain","parents":["PICKED"],"version":"3"}},`+
					`{"fileId":"DEEP","file":{"id":"DEEP","name":"deep.txt","mimeType":"text/plain","parents":["MID"],"version":"1"}},`+
					`{"fileId":"OUT1","file":{"id":"OUT1","name":"out.txt","mimeType":"text/plain","parents":["OTHER"],"version":"1"}},`+
					`{"fileId":"OUT2","file":{"id":"OUT2","name":"out2.txt","mimeType":"text/plain","parents":["OTHER"],"version":"1"}},`+
					`{"fileId":"GONE","removed":true},`+
					`{"fileId":"TRASH","file":{"id":"TRASH","name":"t.txt","mimeType":"text/plain","parents":["PICKED"],"trashed":true}}`+
					`],"newStartPageToken":"NSPT2"}`)
				return
			}
			w.WriteHeader(http.StatusNotFound)
		case "/drive/v3/files/MID":
			fmt.Fprint(w, `{"id":"MID","parents":["PICKED"]}`)
		case "/drive/v3/files/OTHER":
			fmt.Fprint(w, `{"id":"OTHER","parents":["TOP"]}`)
		case "/drive/v3/files/TOP":
			fmt.Fprint(w, `{"id":"TOP","parents":[]}`)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}
	p, fd := newTestProviderRoot(t, "PICKED", h)

	var events []provider.ChangeEvent
	cursor, err := p.SyncChanges(context.Background(), "loc1", "CUR1", func(batch []provider.ChangeEvent) {
		events = append(events, batch...)
	})
	r.NoError(err)
	r.Equal("NSPT2", cursor)

	kinds := map[string]provider.ChangeKind{}
	for _, e := range events {
		kinds[e.FileID] = e.Kind
	}
	r.Equal(map[string]provider.ChangeKind{
		"IN1":   provider.ChangeCreated,
		"DEEP":  provider.ChangeCreated,
		"OUT1":  provider.ChangeDeleted,
		"OUT2":  provider.ChangeDeleted,
		"GONE":  provider.ChangeDeleted,
		"TRASH": provider.ChangeDeleted,
	}, kinds)

	otherLookups := 0
	for _, req2 := range fd.methodReqs() {
		if req2.Method == http.MethodGet && req2.Path == "/drive/v3/files/OTHER" {
			otherLookups++
		}
	}
	r.Equal(1, otherLookups, "the ancestry walk must be memoised across changes")
}

func TestWhenARootSitsOutsideMyDriveThenSyncAlwaysRelists(t *testing.T) {
	t.Parallel()
	r := require.New(t)

	h := func(w http.ResponseWriter, req *http.Request) {
		switch {
		case req.URL.Path == "/drive/v3/files/PICKED":
			fmt.Fprint(w, `{"id":"PICKED","name":"MacBook Backups","mimeType":"application/vnd.google-apps.folder","parents":[]}`)
		case req.URL.Path == "/drive/v3/changes/startPageToken":
			fmt.Fprint(w, `{"startPageToken":"SPT1"}`)
		case req.Method == http.MethodGet && req.URL.Path == "/drive/v3/files" &&
			req.URL.Query().Get("q") == "'PICKED' in parents and trashed = false":
			fmt.Fprint(w, `{"files":[{"id":"F1","name":"a.png","mimeType":"image/png","parents":["PICKED"],"size":"99","version":"1"}]}`)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}
	p, fd := newTestProviderRoot(t, "PICKED", h)

	var events []provider.ChangeEvent
	cursor, err := p.SyncChanges(context.Background(), "loc1", "CUR123", func(batch []provider.ChangeEvent) {
		events = append(events, batch...)
	})
	r.NoError(err)
	r.Equal("SPT1", cursor)
	r.Len(events, 1)
	r.Equal("F1", events[0].FileID)

	for _, rr := range fd.methodReqs() {
		r.NotEqual("/drive/v3/changes", rr.Path, "roots outside My Drive are invisible to the changes feed — always re-list")
	}
}

func TestWhenARootSitsInsideMyDriveThenTheChangesFeedIsUsed(t *testing.T) {
	t.Parallel()
	r := require.New(t)

	h := func(w http.ResponseWriter, req *http.Request) {
		switch req.URL.Path {
		case "/drive/v3/files/PICKED":
			fmt.Fprint(w, `{"id":"PICKED","name":"Photos","mimeType":"application/vnd.google-apps.folder","parents":["ROOTID"]}`)
		case "/drive/v3/changes":
			fmt.Fprint(w, `{"changes":[{"fileId":"F1","file":{"id":"F1","name":"a.png","mimeType":"image/png","parents":["PICKED"],"version":"2"}}],"newStartPageToken":"NSPT"}`)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}
	p, _ := newTestProviderRoot(t, "PICKED", h)

	var events []provider.ChangeEvent
	cursor, err := p.SyncChanges(context.Background(), "loc1", "CUR1", func(batch []provider.ChangeEvent) {
		events = append(events, batch...)
	})
	r.NoError(err)
	r.Equal("NSPT", cursor, "an in-My-Drive root must keep using the changes feed")
	r.Len(events, 1)
	r.Equal("F1", events[0].FileID)
}

func TestWhenTheRootSitsOutsideMyDriveThenTheProviderReconciles(t *testing.T) {
	t.Parallel()
	r := require.New(t)

	outside := &Provider{syncRoot: "PICKED", syncRootInMyDrive: false}
	inDrive := &Provider{syncRoot: "F1", syncRootInMyDrive: true}

	r.True(outside.Reconciles(), "Computers-section roots need the crawl to diff deletions")
	r.False(inDrive.Reconciles(), "changes-feed providers manage deletions themselves")
}
