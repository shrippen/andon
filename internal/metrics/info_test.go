package metrics

import (
	"math"
	"testing"
	"time"

	"andon/internal/sources"
)

// The Kimai tile's "h today" counts the running timer, as Kimai Lite
// does: a day with a timer running since the morning is not "0.0 h".
func TestKimaiInfoCountsRunning(t *testing.T) {
	now := time.Now().UTC()
	today := Today(now)
	data := &sources.KimaiDataset{
		Timesheets: []sources.KimaiSheet{{ID: 1, Begin: today.Format(time.RFC3339), Minutes: 60}},
		Active:     []sources.KimaiSheet{{ID: 2, Begin: now.Add(-30 * time.Minute).Format(time.RFC3339)}},
	}
	running := math.Min(30, now.Sub(today).Minutes())
	want := (60 + math.Floor(running)) / 60
	got := KimaiInfo(data, today)[0].Params["hours"].(map[string]any)["$num"].(float64)
	if math.Abs(got-want) > 1.0/60 {
		t.Fatalf("today: %.2f h, want %.2f h", got, want)
	}
}
