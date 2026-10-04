package widgets

import (
	"testing"

	"andon/internal/sources"
)

// A picked entry is read; the others list with their link as item.
func TestRssDetailPicksEntry(t *testing.T) {
	feed := &sources.FeedResult{Items: []sources.FeedItem{
		{Title: "A", Link: "https://x/a"}, {Title: "B", Link: "https://x/b"}}}
	results := map[string]any{"feed": feed, DetailItemSlot: "https://x/b"}

	body := rssDetail(RssConfig{}, results, ViewCtx{}).Body.(*DetailBody)

	var read Reading
	var rows []LitRow
	for _, b := range body.Blocks {
		switch d := b.Data.(type) {
		case Reading:
			read = d
		case []LitRow:
			rows = d
		}
	}
	if read.Title != "B" {
		t.Fatalf("read %q, want B", read.Title)
	}
	if len(rows) != 1 || rows[0].Item != "https://x/a" {
		t.Fatalf("rows %+v, want A only", rows)
	}
}
