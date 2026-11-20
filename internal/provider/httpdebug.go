package provider

import (
	"log/slog"
	"net/http"
	"time"
)

// NewLogTransport wraps base with one debug line per HTTP exchange: method,
// URL, status, duration and error. Debug lines vanish unless debug logging is
// enabled, so the wrapper is always safe to install.
func NewLogTransport(base http.RoundTripper, logger *slog.Logger) http.RoundTripper {
	if base == nil {
		base = http.DefaultTransport
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &logTransport{base: base, logger: logger}
}

type logTransport struct {
	base   http.RoundTripper
	logger *slog.Logger
}

func (t *logTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	start := time.Now()
	resp, err := t.base.RoundTrip(req)
	attrs := []any{
		"method", req.Method,
		"url", req.URL.String(),
		"durationMs", time.Since(start).Milliseconds(),
	}
	if err != nil {
		attrs = append(attrs, "err", err.Error())
		t.logger.Debug("http", attrs...)
		return nil, err
	}
	attrs = append(attrs, "status", resp.StatusCode)
	t.logger.Debug("http", attrs...)
	return resp, nil
}
