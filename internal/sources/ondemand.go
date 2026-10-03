package sources

// Shared parts of the sources a detail dialog fetches when it opens
// (Tile.DetailQueries): short-lived, several calls at once, nothing
// secret in what they return.

import (
	"context"
	"regexp"
	"sync"
	"time"
)

// detailTTL keeps a dialog's own data while it is reopened.
const detailTTL = 5 * time.Minute

// parallel runs work(0..n-1) with at most limit calls at once and waits
// for all; a cancelled ctx skips the ones not yet started.
func parallel(ctx context.Context, n, limit int, work func(i int)) {
	var wg sync.WaitGroup
	slots := make(chan struct{}, max(limit, 1))
	for i := range n {
		if ctx.Err() != nil {
			break
		}
		slots <- struct{}{}
		wg.Add(1)
		go func() {
			defer func() { <-slots; wg.Done() }()
			work(i)
		}()
	}
	wg.Wait()
}

// secretValue finds "password=…", "token: …", "api_key=…" and the like.
var secretValue = regexp.MustCompile(`(?i)\b(pass(word|wd)?|secret|token|api[_-]?key|authorization|bearer)\b(["']?\s*[:=]\s*["']?|\s+)(?:(?:bearer|basic)\s+)?[^\s"',;&]+`)

// MaskSecrets hides the value of anything that looks like a credential in
// a log line: "password=hunter2" → "password=***".
func MaskSecrets(line string) string {
	return secretValue.ReplaceAllString(line, "${1}${3}***")
}
