package boards

import (
	"net/url"
	"strings"

	"andon/internal/widgets"
)

// markTwins flags tiles a reader could confuse: the same widget placed
// twice (editors see "2×") and links sharing a title, which show their
// host ("Nextcloud · cloud.lan", "Nextcloud · nc.example.org").
func markTwins(sections []SectionView) {
	placed := map[int64]int{}
	titles := map[string]int{}
	for _, s := range sections {
		for _, t := range s.Tiles {
			placed[t.WidgetID]++
			if t.Type == linkType {
				titles[strings.ToLower(t.Title)]++
			}
		}
	}

	for i := range sections {
		for j := range sections[i].Tiles {
			t := &sections[i].Tiles[j]
			t.Placed = placed[t.WidgetID]
			if t.Type != linkType || titles[strings.ToLower(t.Title)] < 2 {
				continue
			}
			t.Host = linkHost(t.Config)
		}
	}
}

func linkHost(cfg any) string {
	link, ok := cfg.(widgets.LinkConfig)
	if !ok {
		return ""
	}
	u, err := url.Parse(link.URL)
	if err != nil {
		return ""
	}
	return u.Hostname()
}
