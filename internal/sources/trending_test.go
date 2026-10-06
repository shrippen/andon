package sources_test

import (
	"net/http"
	"strconv"
	"testing"
	"time"

	"andon/internal/sources"
)

// TestTrendingSearch: the period becomes a creation date, the language a
// qualifier; repositories come back by stars.
func TestTrendingSearch(t *testing.T) {
	var q string
	srv := jsonServer(t, map[string]any{"/search/repositories": map[string]any{"items": []any{
		map[string]any{"full_name": "a/one", "html_url": "https://github.com/a/one", "description": "First", "language": "Go", "stargazers_count": 900},
		map[string]any{"full_name": "b/two", "html_url": "https://github.com/b/two", "stargazers_count": 40},
	}}}, func(r *http.Request) bool {
		q = r.URL.Query().Get("q")
		return r.URL.Query().Get("sort") == "stars"
	})
	t.Cleanup(sources.SetBases(srv.URL))

	out := fetchConn(t, "github.trending", sources.Ctx{Params: map[string]any{"since": "weekly", "language": "go", "limit": 5.0}}).(*sources.TrendingResult)
	since := time.Now().UTC().AddDate(0, 0, -7).Format(time.DateOnly)
	if q != "created:>="+since+" language:go" {
		t.Errorf("q = %q", q)
	}
	if len(out.Repos) != 2 || out.Repos[0].Name != "a/one" || out.Repos[0].Stars != 900 || out.Repos[1].Description != "" {
		t.Fatalf("repos: %+v", out.Repos)
	}
}

// TestSportsMatchday: the current matchday, sorted by kick-off, with the
// latest result as score and no score before kick-off.
func TestSportsMatchday(t *testing.T) {
	team := func(short string) map[string]any {
		return map[string]any{"teamName": "FC " + short, "shortName": short}
	}
	srv := jsonServer(t, map[string]any{"/getmatchdata/bl1": []any{
		map[string]any{"matchDateTimeUTC": "2026-10-10T13:30:00Z", "team1": team("Köln"), "team2": team("Mainz"), "matchResults": []any{},
			"leagueName": "1. Bundesliga", "group": map[string]any{"groupName": "5. Spieltag"}},
		map[string]any{"matchDateTimeUTC": "2026-10-09T18:30:00Z", "team1": team("Dortmund"), "team2": team("Bayern"), "matchIsFinished": true,
			"matchResults": []any{
				map[string]any{"resultOrderID": 2, "pointsTeam1": 2, "pointsTeam2": 1},
				map[string]any{"resultOrderID": 1, "pointsTeam1": 1, "pointsTeam2": 0},
			}, "leagueName": "1. Bundesliga", "group": map[string]any{"groupName": "5. Spieltag"}},
	}}, nil)
	t.Cleanup(sources.SetBases(srv.URL))

	out := fetchConn(t, "sports", sources.Ctx{Params: map[string]any{"league": "BL1", "show": "matches"}}).(*sources.SportsResult)
	if out.Round != "5. Spieltag" || len(out.Matches) != 2 {
		t.Fatalf("result: %+v", out)
	}
	first, second := out.Matches[0], out.Matches[1]
	if first.Home != "Dortmund" || first.HomeGoals != 2 || first.AwayGoals != 1 || !first.Finished || !first.Started {
		t.Errorf("first: %+v", first)
	}
	if second.Home != "Köln" || second.Started {
		t.Errorf("second: %+v", second)
	}
}

// TestSportsTeamSeason: a team's matches come from its season, which
// starts in July.
func TestSportsTeamSeason(t *testing.T) {
	srv := jsonServer(t, map[string]any{"/getmatchdata/bl1/" + season() + "/Bayern": []any{}}, nil)
	t.Cleanup(sources.SetBases(srv.URL))

	if _, err := sources.SportsSource.Fetch(t.Context(), sources.Ctx{Params: map[string]any{"league": "bl1", "team": "Bayern"}}); err != nil {
		t.Fatalf("team season: %v", err)
	}
}

func TestSportsTable(t *testing.T) {
	srv := jsonServer(t, map[string]any{"/getbltable/bl1/" + season(): []any{
		map[string]any{"teamName": "Borussia Dortmund", "shortName": "Dortmund", "matches": 4, "goalDiff": 7, "points": 12},
		map[string]any{"teamName": "FC Bayern München", "shortName": "", "matches": 4, "goalDiff": 5, "points": 10},
	}}, nil)
	t.Cleanup(sources.SetBases(srv.URL))

	out := fetchConn(t, "sports", sources.Ctx{Params: map[string]any{"league": "bl1", "show": "table"}}).(*sources.SportsResult)
	if len(out.Table) != 2 || out.Table[0].Rank != 1 || out.Table[0].Points != 12 || out.Table[1].Team != "FC Bayern München" {
		t.Fatalf("table: %+v", out.Table)
	}
}

func TestSportsNeedsLeague(t *testing.T) {
	if _, err := sources.SportsSource.Fetch(t.Context(), sources.Ctx{}); err == nil || err.Error() != "sports.no_league" {
		t.Fatalf("err = %v", err)
	}
}

// season is the football season running today, e.g. "2026" from July
// 2026 to June 2027.
func season() string {
	now := time.Now()
	if now.Month() < time.July {
		return strconv.Itoa(now.Year() - 1)
	}
	return strconv.Itoa(now.Year())
}
