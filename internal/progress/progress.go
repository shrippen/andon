// Package progress tracks long work that spans several runs, e.g. the
// Dawarich tracks, read 300 per fetch:
//
//	sources ──Set(key, …)──► progress ◄──List()── services (maintenance page)
//	        ──Done(key)───►
//
// A leaf package, so every layer may report to it.
package progress

import (
	"slices"
	"strings"
	"sync"
	"time"
)

// Task is one piece of unfinished work; Label is a catalogue key, Params
// its parameters, e.g. ("task.dawarich_tracks", {"host": "geo.lan"}).
type Task struct {
	Key         string
	Label       string
	Params      map[string]any
	Done, Total int
	Updated     time.Time
}

// Percent is the finished share, 0–100.
func (t Task) Percent() int {
	if t.Total <= 0 {
		return 0
	}
	return min(100, t.Done*100/t.Total)
}

var (
	mu    sync.Mutex
	tasks = map[string]Task{}
)

// Set records the state of the task key.
func Set(key, label string, params map[string]any, done, total int) {
	mu.Lock()
	defer mu.Unlock()
	tasks[key] = Task{Key: key, Label: label, Params: params, Done: done, Total: total, Updated: time.Now().UTC()}
}

// Finish removes the task key once its work is complete.
func Finish(key string) {
	mu.Lock()
	defer mu.Unlock()
	delete(tasks, key)
}

// Get returns the task key, false if none is open.
func Get(key string) (Task, bool) {
	mu.Lock()
	defer mu.Unlock()
	t, ok := tasks[key]
	return t, ok
}

// List returns the open tasks, by key.
func List() []Task {
	mu.Lock()
	defer mu.Unlock()

	out := make([]Task, 0, len(tasks))
	for _, t := range tasks {
		out = append(out, t)
	}
	slices.SortFunc(out, func(a, b Task) int { return strings.Compare(a.Key, b.Key) })
	return out
}
