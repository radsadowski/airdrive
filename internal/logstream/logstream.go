// Package logstream adds an opt-in UI log stream to a slog handler: records
// go to stderr as usual and, while debug logging is on, also reach the app as
// log:line events.
package logstream

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	ev "github.com/radosny/airdrive/internal/events"
)

// Sink is shared by the root logger and every handler derived from it, so
// SetEmit and SetDebug take effect everywhere.
type Sink struct {
	enabled atomic.Bool
	level   slog.LevelVar

	mu   sync.RWMutex
	emit func(name string, data ...any)
}

func NewSink() *Sink {
	return &Sink{}
}

// Level is the level variable handed to the stderr handler, so enabling debug
// also lowers the stderr threshold.
func (s *Sink) Level() *slog.LevelVar { return &s.level }

// SetDebug turns the UI stream on or off and moves the stderr threshold with it.
func (s *Sink) SetDebug(on bool) {
	s.enabled.Store(on)
	if on {
		s.level.Set(slog.LevelDebug)
		return
	}
	s.level.Set(slog.LevelInfo)
}

// SetEmit wires the event emitter once the app exists.
func (s *Sink) SetEmit(emit func(name string, data ...any)) {
	s.mu.Lock()
	s.emit = emit
	s.mu.Unlock()
}

func (s *Sink) emitter() func(name string, data ...any) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.emit
}

// Handler wraps next so records also reach the UI while debug is enabled.
func (s *Sink) Handler(next slog.Handler) slog.Handler {
	return &handler{next: next, sink: s}
}

type handler struct {
	next   slog.Handler
	sink   *Sink
	attrs  []slog.Attr
	groups []string
}

func (h *handler) Enabled(ctx context.Context, level slog.Level) bool {
	if h.sink.enabled.Load() {
		return level >= slog.LevelDebug
	}
	return h.next.Enabled(ctx, level)
}

func (h *handler) Handle(ctx context.Context, r slog.Record) error {
	if h.sink.enabled.Load() {
		if emit := h.sink.emitter(); emit != nil {
			attrs := map[string]any{}
			for _, a := range h.attrs {
				attrs[h.key(a.Key)] = fmt.Sprint(a.Value.Any())
			}
			r.Attrs(func(a slog.Attr) bool {
				attrs[h.key(a.Key)] = fmt.Sprint(a.Value.Any())
				return true
			})
			emit(ev.LogLine, map[string]any{
				"time":  r.Time.UTC().Format(time.RFC3339Nano),
				"level": r.Level.String(),
				"msg":   r.Message,
				"attrs": attrs,
			})
		}
	}
	return h.next.Handle(ctx, r)
}

func (h *handler) key(k string) string {
	if len(h.groups) == 0 {
		return k
	}
	return strings.Join(h.groups, ".") + "." + k
}

func (h *handler) WithAttrs(attrs []slog.Attr) slog.Handler {
	next := make([]slog.Attr, 0, len(h.attrs)+len(attrs))
	next = append(next, h.attrs...)
	next = append(next, attrs...)
	return &handler{next: h.next.WithAttrs(attrs), sink: h.sink, attrs: next, groups: h.groups}
}

func (h *handler) WithGroup(name string) slog.Handler {
	groups := make([]string, 0, len(h.groups)+1)
	groups = append(groups, h.groups...)
	groups = append(groups, name)
	return &handler{next: h.next.WithGroup(name), sink: h.sink, attrs: h.attrs, groups: groups}
}
