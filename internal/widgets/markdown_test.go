package widgets

import "testing"

func TestParseMarkdown(t *testing.T) {
	blocks := parseMarkdown("# Heute\n\n**Wichtig**: `make check` vor dem Push\nsiehe [Doku](https://example.org/x)\n\n- eins\n* zwei <b>\n\njavascript:[x](javascript:alert(1))")
	if len(blocks) != 4 || blocks[0].Kind != "h" || blocks[1].Kind != "p" || blocks[2].Kind != "ul" || len(blocks[2].Lines) != 2 {
		t.Fatalf("blocks: %+v", blocks)
	}
	p := blocks[1].Lines
	if p[0][0].Style != "b" || p[0][2].Style != "code" || p[1][1].URL != "https://example.org/x" || p[1][1].Text != "Doku" {
		t.Fatalf("spans: %+v", p)
	}
	if blocks[2].Lines[1][0].Text != "zwei <b>" {
		t.Fatalf("text stays text: %+v", blocks[2].Lines[1])
	}
	for _, s := range blocks[3].Lines[0] {
		if s.URL != "" {
			t.Fatalf("only http(s) links: %+v", blocks[3])
		}
	}
}
