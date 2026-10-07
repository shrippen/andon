package web

import (
	"strconv"
	"strings"
	"testing"
	"time"
)

// tzMark marks a time formatted on the server with its zone, hidden;
// andon.js shows it where the browser is in another zone. Without a
// name it is the server's zone, else the named one (a tile's setting).
func TestTZMark(t *testing.T) {
	loc, _ := time.LoadLocation("Asia/Tokyo")
	_, offset := time.Now().In(loc).Zone()
	got := string(tzMark("Asia/Tokyo"))
	if !strings.Contains(got, `class="tz-mark"`) || !strings.Contains(got, `data-offset="`+strconv.Itoa(offset/60)+`"`) || !strings.Contains(got, " JST") || !strings.Contains(got, " hidden") {
		t.Fatalf("tokyo: %s", got)
	}
	_, here := time.Now().Zone()
	if got := string(tzMark()); !strings.Contains(got, `data-offset="`+strconv.Itoa(here/60)+`"`) {
		t.Fatalf("server: %s", got)
	}
	if got := string(tzMark("Local")); !strings.Contains(got, `data-offset="`+strconv.Itoa(here/60)+`"`) {
		t.Fatalf("Local: %s", got)
	}
	if z := zoneOf("Asia/Tokyo"); z.Name != "JST" || z.Offset != offset/60 {
		t.Fatalf("zone: %+v", z)
	}
}
