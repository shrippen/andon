package clicks_test

import (
	"errors"
	"testing"
	"time"

	"andon/internal/enums"
	"andon/internal/services/clicks"
	"andon/internal/services/history"
	"andon/internal/testkit"
)

// TestCount: clicks on a link add up per day; other tiles do not count.
func TestCount(t *testing.T) {
	d := testkit.DB(t)
	who, space := testkit.User(t, d, "a@b.c", enums.RoleUser)
	link := testkit.Place(t, d, who, space, "link", map[string]any{"url": "https://wiki.example.test"}, nil)
	now := time.Now().UTC()
	for range 2 {
		if err := clicks.Count(d, who, link, now); err != nil {
			t.Fatal(err)
		}
	}
	h, err := history.Load(d, space, 0, now)
	if err != nil {
		t.Fatal(err)
	}
	found := 0.0
	for _, k := range h.Keys("link.clicks.") {
		for _, p := range h.SeriesOf(k) {
			found += p.Value
		}
	}
	if found != 2 {
		t.Fatalf("clicks: %v (%v)", found, h.Keys("link."))
	}
	note := testkit.Place(t, d, who, space, "note", nil, nil)
	if err := clicks.Count(d, who, note, now); !errors.Is(err, clicks.ErrNotLink) {
		t.Fatalf("note: %v", err)
	}
}
