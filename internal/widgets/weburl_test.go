package widgets

import "testing"

// TestScriptURLsDropped: javascript:, vbscript: and data: never become a
// link (also in sub-links and the frame's title link); other schemes
// a homelab uses (ssh:, smb:, obsidian:) and paths stay.
func TestScriptURLsDropped(t *testing.T) {
	for raw, want := range map[string]string{
		"javascript:alert(1)": "", " JavaScript:alert(1)": "", "java\tscript:alert(1)": "", "vbscript:x": "",
		"data:text/html,<script>": "", "https://git.lan": "https://git.lan", "ssh://nas": "ssh://nas", "/boards/2": "/boards/2",
	} {
		if got := webURL(raw); got != want {
			t.Errorf("%q: %q, want %q", raw, got, want)
		}
	}
	link := decodeOf[LinkConfig]("link", map[string]any{"url": "javascript:x", "items": []any{map[string]any{"url": "javascript:y"}}})
	if link.URL != "" || len(link.Items) != 0 {
		t.Errorf("link: %+v", link)
	}
	if f := FrameOf(map[string]any{"frame_link": "javascript:z"}); f.Link != "" {
		t.Errorf("frame link: %q", f.Link)
	}
}
