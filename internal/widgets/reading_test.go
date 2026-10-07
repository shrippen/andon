package widgets

import (
	"testing"

	"andon/internal/sources"
)

// TestReadingTurns: the sites take turns, each in its order; live
// channels take room from the posts; a site filter keeps one.
func TestReadingTurns(t *testing.T) {
	news := &sources.NewsDataset{Items: []sources.NewsItem{
		{Site: "hackernews", Title: "h1"}, {Site: "hackernews", Title: "h2"}, {Site: "hackernews", Title: "h3"},
		{Site: "reddit", Feed: "a", Title: "r1"}, {Site: "youtube", Feed: "Club", Title: "y1"},
	}}
	tw := &sources.TwitchDataset{Live: []sources.LiveChannel{{User: "small", Viewers: 2}, {User: "big", Viewers: 90}}}
	results := map[string]any{"news": news, "twitch": tw}

	v := readingView(ReadingConfig{Limit: 6}, results, ViewCtx{})
	lines, live := v["Lines"].([]ReadingLine), v["Live"].([]sources.LiveChannel)
	if len(lines) != 4 || lines[0].Title != "h1" || lines[1].Title != "r1" || lines[2].Title != "y1" || lines[3].Title != "h2" {
		t.Fatalf("lines: %+v", lines)
	}
	if live[0].User != "big" || lines[0].SourceKey != "reading.site.hackernews" || lines[1].SourceName != "r/a" || lines[2].SourceName != "Club" {
		t.Fatalf("live %+v lines %+v", live, lines)
	}

	only := readingItems(ReadingConfig{Site: "hackernews"}, results)
	if len(only) != 3 {
		t.Fatalf("filter: %+v", only)
	}
}
