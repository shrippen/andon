package widgets

import (
	"testing"
	"time"

	"andon/internal/sources"
)

// TestMatchWindow: a team's season shows the latest results before the
// next match, then the fixtures.
func TestMatchWindow(t *testing.T) {
	start := time.Date(2026, 8, 22, 13, 30, 0, 0, time.UTC)
	var season []sources.SportsMatch
	for i := range 10 {
		m := sources.SportsMatch{Kickoff: start.AddDate(0, 0, 7*i), Home: string(rune('A' + i)), Away: "X"}
		if i < 6 {
			m.Finished, m.Started, m.HomeGoals = true, true, i
		}
		season = append(season, m)
	}

	rows := matchRows(4, season)
	if len(rows) != 4 || rows[0].Home != "E" || rows[2].Home != "G" || rows[1].Score != "5:0" || rows[2].Score != "" {
		t.Fatalf("rows: %+v", rows)
	}
	if rows[0].Day != "19.09." || rows[0].Time != "15:30" {
		t.Errorf("kick-off in the clock's zone: %s %s", rows[0].Day, rows[0].Time)
	}

	// At the end of the season the window ends with the last match.
	for i := range season {
		season[i].Finished = true
	}
	if rows := matchRows(4, season); rows[3].Home != "J" {
		t.Fatalf("season end: %+v", rows)
	}
}

// TestLeagueRows: the top of the table, plus the configured team below.
func TestLeagueRows(t *testing.T) {
	table := []sources.SportsRow{{Rank: 1, Team: "Dortmund"}, {Rank: 2, Team: "Leipzig"}, {Rank: 3, Team: "Bayern"}, {Rank: 4, Team: "Köln"}}
	rows := leagueRows(SportsConfig{Team: "köln", Limit: 2}, table)
	if len(rows) != 3 || rows[2].Team != "Köln" || !rows[2].Mine || rows[0].Mine {
		t.Fatalf("rows: %+v", rows)
	}
}
