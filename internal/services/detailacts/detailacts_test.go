package detailacts_test

import (
	"context"
	"errors"
	"net/url"
	"testing"
	"time"

	"andon/internal/enums"
	"andon/internal/metrics"
	data "andon/internal/repos/data"
	"andon/internal/services/boards"
	"andon/internal/services/detailacts"
	"andon/internal/testkit"
)

// TestRestoreTested: marking a restore test writes the event the rule
// reads; an unknown system or act is refused.
func TestRestoreTested(t *testing.T) {
	d := testkit.DB(t)
	who, space := testkit.User(t, d, "a@b.c", enums.RoleUser)
	tile := testkit.Place(t, d, who, space, "backups", nil, nil)
	ctx := context.Background()

	if err := detailacts.Run(ctx, d, who, tile, "restore_tested", url.Values{"system": {"borgbackup"}}, ""); err != nil {
		t.Fatal(err)
	}
	events, err := data.EventsSince(d, []int64{space}, 0, time.Now().Add(-time.Hour), 10)
	if err != nil || len(events) != 1 || events[0].Kind != metrics.EventRestore || events[0].Subject != "borgbackup" {
		t.Fatalf("events %+v, %v", events, err)
	}
	if err := detailacts.Run(ctx, d, who, tile, "restore_tested", url.Values{"system": {"kimai"}}, ""); !errors.Is(err, detailacts.ErrBadMark) {
		t.Fatalf("bad system: %v", err)
	}
	if err := detailacts.Run(ctx, d, who, tile, "nope", nil, ""); !errors.Is(err, detailacts.ErrUnknownAct) {
		t.Fatalf("unknown act: %v", err)
	}
}

// TestAddAPIField: a clicked path becomes a field of the tile.
func TestAddAPIField(t *testing.T) {
	d := testkit.DB(t)
	who, space := testkit.User(t, d, "a@b.c", enums.RoleUser)
	tile := testkit.Place(t, d, who, space, "custom_api", map[string]any{"url": "https://api.example/x", "fields": "Wind = wind.speed"}, nil)

	if err := detailacts.Run(context.Background(), d, who, tile, "add_field", url.Values{"path": {"main.temp"}}, ""); err != nil {
		t.Fatal(err)
	}
	w, err := boards.PlacedWidget(d, who, tile)
	if err != nil || w.Config["fields"] != "Wind = wind.speed\ntemp = main.temp" || w.Version != 2 {
		t.Fatalf("widget %+v, %v", w, err)
	}
}
