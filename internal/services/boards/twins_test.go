package boards

import (
	"testing"

	"andon/internal/widgets"
)

// Same-titled links show their host; a widget placed twice is flagged.
func TestMarkTwins(t *testing.T) {
	sections := []SectionView{
		{Tiles: []Tile{
			{WidgetID: 1, Type: linkType, Title: "Nextcloud", Config: widgets.LinkConfig{URL: "https://cloud.lan/apps"}},
			{WidgetID: 3, Type: "kpi", Title: "Gitea"},
		}},
		{Tiles: []Tile{
			{WidgetID: 2, Type: linkType, Title: "nextcloud", Config: widgets.LinkConfig{URL: "https://nc.example.org"}},
			{WidgetID: 3, Type: "kpi", Title: "Gitea"},
			{WidgetID: 4, Type: linkType, Title: "Plex", Config: widgets.LinkConfig{URL: "https://plex.lan"}},
		}},
	}
	markTwins(sections)

	a, b, plex := sections[0].Tiles[0], sections[1].Tiles[0], sections[1].Tiles[2]
	if a.Host != "cloud.lan" || b.Host != "nc.example.org" || plex.Host != "" {
		t.Fatalf("hosts: %q %q %q", a.Host, b.Host, plex.Host)
	}
	if sections[0].Tiles[1].Placed != 2 || a.Placed != 1 {
		t.Fatal("repeat flag")
	}
}
