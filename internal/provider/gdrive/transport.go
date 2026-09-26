package gdrive

import (
	"io"
	"math/rand/v2"
	"net/http"
	"strconv"
	"time"
)

// The generated drive/v3 client has no default retry on regular calls
// (verified in api@v0.299.0), so a single 429 mid-crawl would fail the
// whole crawl of a large Drive.

const (
	retryBase   = 250 * time.Millisecond
	retryMax    = 30 * time.Second
	maxAttempts = 5
)

// retryTransport retries 429 on any verb (the request was refused before
// execution) and 502/503/504 or network errors on GETs only, so a mutation
// can never be applied twice. Body-bearing requests retry only when
// http.Request.GetBody allows replay.
type retryTransport struct {
	base        http.RoundTripper
	sleep       func(time.Duration) // injectable for tests
	rand        func() float64      // jitter source, injectable for tests
	maxAttempts int
}

func newRetryTransport() *retryTransport {
	return &retryTransport{
		base:        http.DefaultTransport,
		sleep:       time.Sleep,
		rand:        rand.Float64,
		maxAttempts: maxAttempts,
	}
}

func (t *retryTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	base := t.base
	if base == nil {
		base = http.DefaultTransport
	}
	max := t.maxAttempts
	if max <= 0 {
		max = maxAttempts
	}
	for attempt := 0; ; attempt++ {
		if attempt > 0 && req.GetBody != nil {
			body, err := req.GetBody()
			if err != nil {
				return nil, err
			}
			req.Body = body
		}
		resp, err := base.RoundTrip(req)
		if attempt >= max-1 || !t.canRetry(req, resp, err) {
			return resp, err
		}
		if resp != nil && resp.Body != nil {
			_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
			resp.Body.Close()
		}
		t.sleep(t.pause(attempt, resp))
	}
}

// canRetry reports whether the failed attempt may be retried safely.
func (t *retryTransport) canRetry(req *http.Request, resp *http.Response, err error) bool {
	replayable := req.Body == nil || req.GetBody != nil
	if resp != nil {
		switch resp.StatusCode {
		case http.StatusTooManyRequests:
			return replayable
		case http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout:
			return replayable && req.Method == http.MethodGet
		}
		return false
	}
	return err != nil && req.Method == http.MethodGet
}

// pause returns the server's Retry-After when present, otherwise
// exponential backoff with ±50% jitter.
func (t *retryTransport) pause(attempt int, resp *http.Response) time.Duration {
	if resp != nil {
		if ra := resp.Header.Get("Retry-After"); ra != "" {
			if secs, err := strconv.Atoi(ra); err == nil && secs > 0 {
				d := time.Duration(secs) * time.Second
				if d > retryMax {
					d = retryMax
				}
				return d
			}
		}
	}
	d := retryBase << attempt
	if d <= 0 || d > retryMax {
		d = retryMax
	}
	if t.rand != nil {
		d = d/2 + time.Duration(float64(d)*t.rand()/2)
	}
	return d
}
