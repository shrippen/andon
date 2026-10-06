package widgets

// "github_trending": new repositories with the most stars (GitHub search).
// "sports": a league's matchday, a team's matches or the table
// (OpenLigaDB). Both replace Dashy widgets of the same purpose.

import (
	"strconv"
	"strings"
	"time"

	"andon/internal/sources"
)

const (
	// maxTrendRepos and maxSportsRows bound a tile's list.
	maxTrendRepos = 30
	maxSportsRows = 20

	defaultLeague = "bl1"
	sportsRefresh = 5 * time.Minute
	dayMonth      = "02.01."
)

type TrendingConfig struct {
	Since, Language string
	Limit           int
}

func decodeTrending(r Raw) TrendingConfig {
	return TrendingConfig{Since: r.Pick("since"), Language: strings.TrimSpace(r.String("language")), Limit: r.Int("limit")}
}

type SportsConfig struct {
	League, Team, Show string
	Limit              int
}

func decodeSports(r Raw) SportsConfig {
	return SportsConfig{League: strings.TrimSpace(textOr(r, "league")), Team: strings.TrimSpace(r.String("team")), Show: r.Pick("show"), Limit: r.Int("limit")}
}

// MatchRow is one match as shown: kick-off in the clock's zone, score once
// started ("2:1"), Live while it runs.
type MatchRow struct {
	Kickoff        time.Time
	Day, Time      string
	Home, Away     string
	Score          string
	Live, Finished bool
}

// TableRow is one table line; Mine marks the configured team.
type TableRow struct {
	sources.SportsRow
	Mine bool
}

func sportsView(cfg SportsConfig, results map[string]any, _ ViewCtx) map[string]any {
	data, ok := results["sports"].(*sources.SportsResult)
	if !ok {
		return map[string]any{}
	}
	if cfg.Show == sources.SportsTable {
		return map[string]any{"League": data.League, "Table": leagueRows(cfg, data.Table)}
	}
	return map[string]any{"League": data.League, "Round": data.Round, "Matches": matchRows(cfg.Limit, data.Matches)}
}

// matchRows keeps a window of limit matches around the next one: the
// latest results before it, then the fixtures. A matchday fits whole.
func matchRows(limit int, matches []sources.SportsMatch) []MatchRow {
	next := len(matches)
	for i, m := range matches {
		if !m.Finished {
			next = i
			break
		}
	}
	start := max(0, min(next-limit/2, len(matches)-limit))

	zone := clockZone()
	var rows []MatchRow
	for _, m := range matches[start:min(start+limit, len(matches))] {
		at := m.Kickoff.In(zone)
		row := MatchRow{Kickoff: at, Day: at.Format(dayMonth), Time: at.Format(timeOfDay), Home: m.Home, Away: m.Away,
			Live: m.Started && !m.Finished, Finished: m.Finished}
		if m.Started {
			row.Score = strconv.Itoa(m.HomeGoals) + ":" + strconv.Itoa(m.AwayGoals)
		}
		rows = append(rows, row)
	}
	return rows
}

// leagueRows shows the top limit; the configured team is marked and added
// below when it ranks lower.
func leagueRows(cfg SportsConfig, table []sources.SportsRow) []TableRow {
	team := strings.ToLower(cfg.Team)
	var rows []TableRow
	for _, r := range table {
		mine := team != "" && strings.Contains(strings.ToLower(r.Team), team)
		if len(rows) < cfg.Limit || mine {
			rows = append(rows, TableRow{SportsRow: r, Mine: mine})
		}
	}
	return rows
}

func init() {
	Tile[TrendingConfig]{Key: "github_trending", Category: CategoryStart, Topic: TopicDev, RefreshS: int(time.Hour / time.Second),
		Fields: []Field{sel("since", sources.TrendWeekly, sources.TrendDaily, sources.TrendWeekly, sources.TrendMonthly),
			{Key: "language", Input: InputText}, {Key: "limit", Input: InputNumber, Default: defaultListLimit, Min: "1", Max: strconv.Itoa(maxTrendRepos)}},
		Decode: decodeTrending,
		Queries: one("repos", "github.trending", func(cfg TrendingConfig) map[string]any {
			return map[string]any{"since": cfg.Since, "language": cfg.Language, "limit": float64(cfg.Limit)}
		})}.add()

	Tile[SportsConfig]{Key: "sports", Category: CategoryStart, Topic: TopicWorld, RefreshS: int(sportsRefresh / time.Second),
		Fields: []Field{{Key: "league", Input: InputText, Default: defaultLeague}, {Key: "team", Input: InputText},
			sel("show", sources.SportsMatches, sources.SportsMatches, sources.SportsTable),
			{Key: "limit", Input: InputNumber, Default: defaultListLimit, Min: "1", Max: strconv.Itoa(maxSportsRows)}},
		Decode: decodeSports, View: sportsView,
		Queries: one("sports", "sports", func(cfg SportsConfig) map[string]any {
			return map[string]any{"league": cfg.League, "team": cfg.Team, "show": cfg.Show}
		})}.add()
}
