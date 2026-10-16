package sources

import (
	"encoding/json"
	"testing"
)

func TestReleaseOf(t *testing.T) {
	parse := func(s string) any {
		var v any
		if err := json.Unmarshal([]byte(s), &v); err != nil {
			t.Fatal(err)
		}
		return v
	}
	good := `{"format": 1, "projects": {"andon": {"version": "v0.7.0", "date": "2026-10-07", "url": "https://github.com/shrippen/andon/releases/tag/v0.7.0"}}}`
	r, err := releaseOf(parse(good), "andon")
	if err != nil || r.Version != "0.7.0" || r.Date != "2026-10-07" {
		t.Fatalf("release = %+v, %v", r, err)
	}

	for name, body := range map[string]string{
		"format 2":   `{"format": 2, "projects": {"andon": {"version": "0.7.0", "url": "https://x.y/r"}}}`,
		"no project": `{"format": 1, "projects": {"kader": {"version": "0.7.0", "url": "https://x.y/r"}}}`,
		"http link":  `{"format": 1, "projects": {"andon": {"version": "0.7.0", "url": "http://x.y/r"}}}`,
		"script":     `{"format": 1, "projects": {"andon": {"version": "0.7.0", "url": "javascript:alert(1)"}}}`,
		"no version": `{"format": 1, "projects": {"andon": {"url": "https://x.y/r"}}}`,
	} {
		if _, err := releaseOf(parse(body), "andon"); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}
