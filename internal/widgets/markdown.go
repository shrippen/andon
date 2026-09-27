package widgets

// A small Markdown subset for notes, parsed into data so the template
// escapes every piece (no raw HTML ever reaches the page):
//
//	# Heading        → heading
//	- item / * item  → list
//	**bold** *italic* `code` [text](https://…)   inside any line
//	blank line       → new paragraph

import (
	"regexp"
	"strings"
)

// MDBlock is a paragraph, heading or list.
type MDBlock struct {
	Kind  string     // "p", "h", "ul"
	Lines [][]MDSpan // one per list item; paragraphs join theirs with a break
}

// MDSpan is a run of text with one style; URL makes it a link.
type MDSpan struct {
	Text, Style, URL string // Style: "", "b", "i", "code"
}

var mdInline = regexp.MustCompile("\\*\\*([^*]+)\\*\\*|\\*([^*]+)\\*|`([^`]+)`|\\[([^\\]]+)\\]\\((https?://[^\\s)]+)\\)")

// parseMarkdown splits text into blocks.
func parseMarkdown(text string) []MDBlock {
	var out []MDBlock
	var cur *MDBlock
	flush := func() {
		if cur != nil {
			out = append(out, *cur)
			cur = nil
		}
	}
	for _, raw := range strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n") {
		line := strings.TrimRight(raw, " \t")
		trimmed := strings.TrimSpace(line)
		switch {
		case trimmed == "":
			flush()
		case strings.HasPrefix(trimmed, "#"):
			flush()
			out = append(out, MDBlock{Kind: "h", Lines: [][]MDSpan{mdSpans(strings.TrimSpace(strings.TrimLeft(trimmed, "#")))}})
		case strings.HasPrefix(trimmed, "- ") || strings.HasPrefix(trimmed, "* "):
			if cur == nil || cur.Kind != "ul" {
				flush()
				cur = &MDBlock{Kind: "ul"}
			}
			cur.Lines = append(cur.Lines, mdSpans(strings.TrimSpace(trimmed[2:])))
		default:
			if cur == nil || cur.Kind != "p" {
				flush()
				cur = &MDBlock{Kind: "p"}
			}
			cur.Lines = append(cur.Lines, mdSpans(trimmed))
		}
	}
	flush()
	return out
}

// mdSpans cuts one line into styled runs.
func mdSpans(line string) []MDSpan {
	var out []MDSpan
	last := 0
	for _, m := range mdInline.FindAllStringSubmatchIndex(line, -1) {
		if m[0] > last {
			out = append(out, MDSpan{Text: line[last:m[0]]})
		}
		switch {
		case m[2] >= 0:
			out = append(out, MDSpan{Text: line[m[2]:m[3]], Style: "b"})
		case m[4] >= 0:
			out = append(out, MDSpan{Text: line[m[4]:m[5]], Style: "i"})
		case m[6] >= 0:
			out = append(out, MDSpan{Text: line[m[6]:m[7]], Style: "code"})
		default:
			out = append(out, MDSpan{Text: line[m[8]:m[9]], URL: line[m[10]:m[11]]})
		}
		last = m[1]
	}
	if last < len(line) {
		out = append(out, MDSpan{Text: line[last:]})
	}
	return out
}
