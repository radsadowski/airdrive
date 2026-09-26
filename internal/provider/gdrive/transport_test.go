package gdrive

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func newTestTransport(sleep func(time.Duration), maxAttempts int) *retryTransport {
	return &retryTransport{
		base:        http.DefaultTransport,
		sleep:       sleep,
		maxAttempts: maxAttempts,
		rand:        func() float64 { return 0 },
	}
}

func TestWhenServerReturns429ThenGetIsRetriedUntilSuccess(t *testing.T) {
	t.Parallel()
	r := require.New(t)

	var attempts int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if atomic.AddInt32(&attempts, 1) < 3 {
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		fmt.Fprint(w, "ok")
	}))
	defer srv.Close()

	client := &http.Client{Transport: newTestTransport(func(time.Duration) {}, 5)}
	resp, err := client.Get(srv.URL)
	r.NoError(err)
	defer resp.Body.Close()
	r.Equal(http.StatusOK, resp.StatusCode)
	r.Equal(int32(3), atomic.LoadInt32(&attempts))
}

func TestWhenServerReturns503ThenGetIsRetriedUntilSuccess(t *testing.T) {
	t.Parallel()
	r := require.New(t)

	var attempts int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if atomic.AddInt32(&attempts, 1) < 2 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		fmt.Fprint(w, "ok")
	}))
	defer srv.Close()

	client := &http.Client{Transport: newTestTransport(func(time.Duration) {}, 5)}
	resp, err := client.Get(srv.URL)
	r.NoError(err)
	defer resp.Body.Close()
	r.Equal(http.StatusOK, resp.StatusCode)
	r.Equal(int32(2), atomic.LoadInt32(&attempts))
}

func TestWhenServerReturns503ThenPostIsNotRetried(t *testing.T) {
	t.Parallel()
	r := require.New(t)

	var attempts int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		atomic.AddInt32(&attempts, 1)
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	client := &http.Client{Transport: newTestTransport(func(time.Duration) {}, 5)}
	resp, err := client.Post(srv.URL, "text/plain", strings.NewReader("x"))
	r.NoError(err)
	defer resp.Body.Close()
	r.Equal(http.StatusServiceUnavailable, resp.StatusCode, "mutating call must not be retried on 5xx")
	r.Equal(int32(1), atomic.LoadInt32(&attempts))
}

func TestWhenServerReturns429ThenPostIsRetriedWithReplayedBody(t *testing.T) {
	t.Parallel()
	r := require.New(t)

	var attempts int32
	var bodies []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		b, _ := io.ReadAll(req.Body)
		bodies = append(bodies, string(b))
		if atomic.AddInt32(&attempts, 1) < 2 {
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		fmt.Fprint(w, "created")
	}))
	defer srv.Close()

	client := &http.Client{Transport: newTestTransport(func(time.Duration) {}, 5)}
	resp, err := client.Post(srv.URL, "text/plain", strings.NewReader("payload"))
	r.NoError(err)
	defer resp.Body.Close()
	r.Equal(http.StatusOK, resp.StatusCode)
	r.Equal(int32(2), atomic.LoadInt32(&attempts))
	r.Equal([]string{"payload", "payload"}, bodies, "body must be replayed on retry")
}

func TestWhen429CarriesRetryAfterThenThePauseIsHonored(t *testing.T) {
	t.Parallel()
	r := require.New(t)

	var attempts int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if atomic.AddInt32(&attempts, 1) < 2 {
			w.Header().Set("Retry-After", "2")
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		fmt.Fprint(w, "ok")
	}))
	defer srv.Close()

	var slept []time.Duration
	client := &http.Client{Transport: newTestTransport(func(d time.Duration) { slept = append(slept, d) }, 5)}
	resp, err := client.Get(srv.URL)
	r.NoError(err)
	defer resp.Body.Close()
	r.Equal(http.StatusOK, resp.StatusCode)
	r.Equal([]time.Duration{2 * time.Second}, slept, "one pause, exactly the Retry-After duration")
}

func TestWhen429PersistsThenRetriesStopAtMaxAttempts(t *testing.T) {
	t.Parallel()
	r := require.New(t)

	var attempts int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		atomic.AddInt32(&attempts, 1)
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer srv.Close()

	client := &http.Client{Transport: newTestTransport(func(time.Duration) {}, 3)}
	resp, err := client.Get(srv.URL)
	r.NoError(err)
	defer resp.Body.Close()
	r.Equal(http.StatusTooManyRequests, resp.StatusCode, "429 surfaces after retries are exhausted")
	r.Equal(int32(3), atomic.LoadInt32(&attempts))
}
