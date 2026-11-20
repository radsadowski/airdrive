package logstream

import (
	"io"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/require"

	ev "github.com/radosny/airdrive/internal/events"
)

func newTestLogger(sink *Sink) (*slog.Logger, *[]map[string]any) {
	var got []map[string]any
	sink.SetEmit(func(name string, data ...any) {
		if name != ev.LogLine {
			return
		}
		payload, _ := data[0].(map[string]any)
		got = append(got, payload)
	})
	logger := slog.New(sink.Handler(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: sink.Level()})))
	return logger, &got
}

func TestWhenDebugIsOffThenRecordsStayOutOfTheUIStream(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	sink := NewSink()
	logger, got := newTestLogger(sink)

	logger.Debug("hidden")
	logger.Info("also hidden from the stream")

	r.Empty(*got)
}

func TestWhenDebugIsOnThenRecordsAndAttrsReachTheUIStream(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	sink := NewSink()
	sink.SetDebug(true)
	logger, got := newTestLogger(sink)

	logger.With("location", "L1").Debug("file op item", "path", "folder/file.txt", "count", 3)

	r.Len(*got, 1)
	payload := (*got)[0]
	r.Equal("DEBUG", payload["level"])
	r.Equal("file op item", payload["msg"])
	attrs, ok := payload["attrs"].(map[string]any)
	r.True(ok)
	r.Equal("L1", attrs["location"])
	r.Equal("folder/file.txt", attrs["path"])
	r.Equal("3", attrs["count"])
}

func TestWhenDebugIsTurnedOffAgainThenTheStreamStops(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	sink := NewSink()
	sink.SetDebug(true)
	logger, got := newTestLogger(sink)

	logger.Debug("first")
	sink.SetDebug(false)
	logger.Debug("second")

	r.Len(*got, 1)
	r.Equal("first", (*got)[0]["msg"])
}
