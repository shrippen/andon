package feeds_test

import (
	"context"
	"errors"
	"testing"

	"andon/internal/enums"
	"andon/internal/metrics"
	"andon/internal/services/feeds"
	"andon/internal/services/workbench"
	"andon/internal/testkit"
)

// The demo's pinned images lack release feeds: a subscription reaches
// the review with a GitHub address, another address is refused, and
// the demo takes no writes.
func TestFeeds(t *testing.T) {
	d := testkit.DB(t)
	who, space := testkit.User(t, d, "owner@x.de", enums.RoleUser)
	for _, s := range []enums.ServiceType{enums.ServiceGitea, enums.ServiceFreshRSS} {
		testkit.Conn(t, d, who, space, s, "demo://"+string(s))
	}
	ctx := context.Background()

	page, err := feeds.Open(ctx, d, who, "")
	if err != nil || len(page.Cases) == 0 || page.Cases[0].Action != metrics.FeedAdd {
		t.Fatalf("open: %+v %v", page, err)
	}
	c := page.Cases[0]
	steps := []workbench.Step{
		{Case: c.Key, Action: feeds.ActSubscribe, Fields: map[string]string{feeds.FieldURL: c.URL}},
		{Case: c.Key, Action: feeds.ActSubscribe, Fields: map[string]string{feeds.FieldURL: "https://example.org/feed"}},
		{Case: c.Key, Action: feeds.ActUnsubscribe},
	}
	plan, err := feeds.Review(ctx, d, who, "", steps)
	if err != nil || plan.Writes() != 1 || plan.Steps[1].Problem != feeds.ProblemURL || plan.Steps[2].Problem != feeds.ProblemGone {
		t.Fatalf("review: %+v %v", plan, err)
	}
	if _, err := feeds.Apply(ctx, d, who, "", steps[:1], ""); !errors.Is(err, feeds.ErrDemo) {
		t.Fatalf("demo write: %v", err)
	}
}
