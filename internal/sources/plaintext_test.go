package sources

import "testing"

// TestPlainTextInline: inline tags join their text as the browser shows
// it; only blocks and breaks part words. Mastodon splits a link into
// spans, which read "https:// example.org/andon" before.
func TestPlainTextInline(t *testing.T) {
	for raw, want := range map[string]string{
		`<p>Hallo <a href="https://example.org/andon"><span class="invisible">https://</span><span class="">example.org/andon</span><span class="invisible"></span></a></p><p>Zweiter</p>`: "Hallo https://example.org/andon Zweiter",
		`Zeile<br>neue<br/>Zeile`:               "Zeile neue Zeile",
		`<b>fett</b>gedruckt und <i>schräg</i>`: "fettgedruckt und schräg",
		`<li>eins</li><li>zwei</li>`:            "eins zwei",
	} {
		if got := plainText(raw, 280); got != want {
			t.Errorf("%s\n got %q\nwant %q", raw, got, want)
		}
	}
}
