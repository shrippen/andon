package outbound_test

import (
	"context"
	"testing"

	"andon/internal/outbound"
	"andon/internal/sources"
	"andon/internal/testkit/live"
)

// kindFeed is a FreshRSS test subscription in the write log.
const kindFeed = "feed"

// testFeed is a small, quiet release feed to subscribe for the test.
const testFeed = "https://github.com/shrippen/andon/releases.atom"

// A release feed subscribed under the test's name, found, and removed:
// the writes of the feed suggestions.
func TestFreshRSSLive(t *testing.T) {
	to := live.Target(t, live.FreshRSS)
	ctx := context.Background()
	name := live.Name(t)

	if feedOf(t, to, testFeed) != "" {
		t.Skipf("%s is subscribed already: not ours to touch", testFeed)
	}
	if err := outbound.FreshSubscribe(ctx, to, testFeed, name, ""); err != nil {
		t.Fatalf("subscribe: %v", err)
	}
	id := feedOf(t, to, testFeed)
	if id == "" {
		t.Fatal("subscribed feed not found")
	}
	live.Created(t, live.FreshRSS, kindFeed, id)

	live.Change(t, live.FreshRSS, live.Delete, kindFeed, id)
	if err := outbound.FreshUnsubscribe(ctx, to, id); err != nil {
		t.Fatalf("unsubscribe: %v", err)
	}
	if feedOf(t, to, testFeed) != "" {
		t.Error("feed still subscribed")
	}
}

// feedOf is the stream id of the subscription to feedURL, "" none.
func feedOf(t *testing.T, to outbound.Target, feedURL string) string {
	t.Helper()
	src, _ := sources.Get("freshrss.data")
	out, err := src.Fetch(context.Background(), sources.Ctx{URL: to.URL, Secret: to.Token, VerifyTLS: to.VerifyTLS})
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	for _, f := range out.(*sources.FreshRSSDataset).Feeds {
		if f.URL == feedURL {
			return f.ID
		}
	}
	return ""
}
