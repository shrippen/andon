package sources

// Sport: results, fixtures and tables from OpenLigaDB (free, no key),
// e.g. bl1 (Bundesliga), bl2, bl3, dfb (DFB-Pokal).
//
//	GET api.openligadb.de/getmatchdata/bl1                  current matchday
//	GET api.openligadb.de/getmatchdata/bl1/2026/Bayern      a team's season
//	GET api.openligadb.de/getbltable/bl1/2026               table

import (
	"context"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"andon/internal/drivers/httpclient"
)

const (
	sportsTTL = 5 * time.Minute

	// seasonStart is the month a football season begins: the 2026
	// season runs from August 2026 to May 2027.
	seasonStart = time.July
)

// What a sports tile shows.
const (
	SportsMatches = "matches"
	SportsTable   = "table"
)

var openLigaBase = "https://api.openligadb.de"

// SportsMatch is one fixture or result; the goals count once it started.
type SportsMatch struct {
	Kickoff              time.Time
	Home, Away           string
	HomeGoals, AwayGoals int
	Started, Finished    bool
}

// SportsRow is one line of a league table.
type SportsRow struct {
	Rank, Played, GoalDiff, Points int
	Team                           string
}

// SportsResult is a league's matches or table.
type SportsResult struct {
	League, Round string // "1. Fußball-Bundesliga 2026/2027", "5. Spieltag"
	Matches       []SportsMatch
	Table         []SportsRow
}

var SportsSource = source{key: "sports", ttl: sportsTTL, fetch: fetchSports}

func fetchSports(ctx context.Context, sctx Ctx) (any, error) {
	league := url.PathEscape(strings.ToLower(strings.TrimSpace(asStr(sctx.Params["league"]))))
	if league == "" {
		return nil, newSourceError("sports.no_league")
	}
	season := strconv.Itoa(seasonOf(time.Now()))
	team := strings.TrimSpace(asStr(sctx.Params["team"]))

	if asStr(sctx.Params["show"]) == SportsTable {
		return sportsTable(ctx, league, season)
	}
	path := "/getmatchdata/" + league
	if team != "" {
		path += "/" + season + "/" + url.PathEscape(team)
	}
	body, _, err := httpclient.GetJSON(ctx, openLigaBase+path, httpclient.Options{})
	if err != nil {
		return nil, newSourceError("%s", err.Error())
	}

	out := &SportsResult{}
	for _, raw := range asList(body) {
		m := asMap(raw)
		out.League = asStr(m["leagueName"])
		if team == "" {
			out.Round = asStr(asMap(m["group"])["groupName"])
		}
		out.Matches = append(out.Matches, sportsMatch(m))
	}
	slices.SortFunc(out.Matches, func(a, b SportsMatch) int { return a.Kickoff.Compare(b.Kickoff) })
	return out, nil
}

// seasonOf is the football season running on day: its starting year.
func seasonOf(day time.Time) int {
	if day.Month() >= seasonStart {
		return day.Year()
	}
	return day.Year() - 1
}

// sportsMatch reads one match; its latest result (half time, then full
// time) is the score.
func sportsMatch(m map[string]any) SportsMatch {
	kickoff, _ := time.Parse(time.RFC3339, asStr(m["matchDateTimeUTC"]))
	out := SportsMatch{Kickoff: kickoff, Home: teamName(m["team1"]), Away: teamName(m["team2"]), Finished: asBool(m["matchIsFinished"])}
	order := -1
	for _, raw := range asList(m["matchResults"]) {
		r := asMap(raw)
		if o := int(asFloat(r["resultOrderID"])); o > order {
			order = o
			out.HomeGoals, out.AwayGoals = int(asFloat(r["pointsTeam1"])), int(asFloat(r["pointsTeam2"]))
			out.Started = true
		}
	}
	return out
}

// teamName prefers the short name ("Dortmund" over "Borussia Dortmund").
func teamName(v any) string {
	t := asMap(v)
	return firstStr(asStr(t["shortName"]), asStr(t["teamName"]))
}

func sportsTable(ctx context.Context, league, season string) (any, error) {
	body, _, err := httpclient.GetJSON(ctx, openLigaBase+"/getbltable/"+league+"/"+season, httpclient.Options{})
	if err != nil {
		return nil, newSourceError("%s", err.Error())
	}
	out := &SportsResult{}
	for i, raw := range asList(body) {
		m := asMap(raw)
		out.Table = append(out.Table, SportsRow{
			Rank: i + 1, Team: firstStr(asStr(m["shortName"]), asStr(m["teamName"])),
			Played: int(asFloat(m["matches"])), GoalDiff: int(asFloat(m["goalDiff"])), Points: int(asFloat(m["points"])),
		})
	}
	return out, nil
}

func init() {
	Register(SportsSource)
}
