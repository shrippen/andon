// Package logbuf keeps the latest log records in memory for the
// maintenance page; every record still goes on to the next handler
// (stderr, docker logs).
package logbuf

import (
	"context"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"sync"
	"time"
)

// Max is the number of records kept.
const Max = 300

// Entry is one kept record, e.g. (WARN, "scheduler: job failed",
// "job=analysis err=timeout").
type Entry struct {
	At    time.Time
	Level slog.Level
	Msg   string
	Attrs string
}

var (
	mu   sync.Mutex
	ring []Entry // oldest first
)

// handler copies records into the ring, then passes them on.
type handler struct {
	next  slog.Handler
	attrs []slog.Attr
	group string
}

// Wrap returns a handler that keeps records and hands them to next.
func Wrap(next slog.Handler) slog.Handler {
	return handler{next: next}
}

func (h handler) Enabled(ctx context.Context, l slog.Level) bool {
	return h.next.Enabled(ctx, l)
}

func (h handler) Handle(ctx context.Context, r slog.Record) error {
	keep(Entry{At: r.Time.UTC(), Level: r.Level, Msg: r.Message, Attrs: h.format(r)})
	return h.next.Handle(ctx, r)
}

func (h handler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return handler{next: h.next.WithAttrs(attrs), attrs: append(slices.Clone(h.attrs), attrs...), group: h.group}
}

func (h handler) WithGroup(name string) slog.Handler {
	return handler{next: h.next.WithGroup(name), attrs: h.attrs, group: h.group + name + "."}
}

// format renders the attributes as key=value pairs.
func (h handler) format(r slog.Record) string {
	var parts []string
	add := func(a slog.Attr) bool {
		parts = append(parts, fmt.Sprintf("%s%s=%v", h.group, a.Key, a.Value.Resolve()))
		return true
	}
	for _, a := range h.attrs {
		add(a)
	}
	r.Attrs(add)
	return strings.Join(parts, " ")
}

func keep(e Entry) {
	mu.Lock()
	defer mu.Unlock()
	ring = append(ring, e)
	if len(ring) > Max {
		ring = slices.Delete(ring, 0, len(ring)-Max)
	}
}

// Since returns the kept records at or above min, newest first.
func Since(min slog.Level) []Entry {
	mu.Lock()
	defer mu.Unlock()

	var out []Entry
	for i := len(ring) - 1; i >= 0; i-- {
		if ring[i].Level >= min {
			out = append(out, ring[i])
		}
	}
	return out
}
