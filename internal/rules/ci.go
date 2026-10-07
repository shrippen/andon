package rules

// CI:
//
//	drone.failing          the default branch has been red for hours
//	cross.release_red_ci   a release went out while its repo's last build
//	                       before it was red (GitHub releases × CI history
//	                       of any provider, sources.CISource)

import (
	"strings"
	"time"

	"andon/internal/enums"
	"andon/internal/sources"
)

var droneSvc = string(enums.ServiceDrone)

func init() {
	Register("drone.failing", droneSvc, map[string]any{"hours": 2.0}, on(droneFailing))
	Register("cross.release_red_ci", Cross, map[string]any{"days": 14.0}, releaseRedCI)
}

func droneFailing(data *sources.DroneDataset, cfg map[string]any, _ Env) []Finding {
	minAge := time.Duration(cfgFloat(cfg, "hours") * float64(time.Hour))
	var found []Finding
	for _, r := range data.CIRepos() {
		streak := 0
		for _, run := range r.Runs {
			if run.Status != sources.CIFailed {
				break
			}
			streak++
		}
		if streak == 0 {
			continue
		}
		first := r.Runs[streak-1].Started
		if first.IsZero() || time.Since(first) < minAge {
			continue
		}
		found = append(found, svcFinding(droneSvc, "drone.failing", "red:"+r.Repo, "drone.failing", enums.SeverityWarn, r.URL,
			map[string]any{"repo": r.Repo, "count": streak, "since": Day(first)}))
	}
	return found
}

func releaseRedCI(_ any, cfg map[string]any, env Env) []Finding {
	gh, ok := env.Datasets[string(enums.ServiceGitHub)].(*sources.GitHubDataset)
	if !ok {
		return nil
	}
	history := ciHistory(env)
	since := time.Now().AddDate(0, 0, -int(cfgFloat(cfg, "days")))

	var found []Finding
	for _, r := range gh.Repos {
		if r.Release == "" || r.ReleasedAt.Before(since) {
			continue
		}
		run, ok := lastRunBefore(history[strings.ToLower(r.Name)], r.ReleasedAt)
		if !ok || run.Status != sources.CIFailed {
			continue
		}
		found = append(found, Finding{Fingerprint: r.Name + "@" + r.Release, Severity: enums.SeverityWarn, Message: "cross.release_red_ci",
			Params:  map[string]any{"repo": r.Name, "release": r.Release, "day": Day(r.ReleasedAt), "build": run.Number},
			Sources: []string{string(enums.ServiceGitHub)}})
	}
	return found
}

// ciHistory collects every provider's runs by lower-case repo name.
func ciHistory(env Env) map[string][]sources.CIRun {
	out := map[string][]sources.CIRun{}
	for _, raw := range env.Datasets {
		src, ok := raw.(sources.CISource)
		if !ok {
			continue
		}
		for _, r := range src.CIRepos() {
			key := strings.ToLower(r.Repo)
			out[key] = append(out[key], r.Runs...)
		}
	}
	return out
}

// lastRunBefore is the newest run that started before t.
func lastRunBefore(runs []sources.CIRun, t time.Time) (sources.CIRun, bool) {
	var best sources.CIRun
	found := false
	for _, r := range runs {
		if !r.Started.IsZero() && r.Started.Before(t) && (!found || r.Started.After(best.Started)) {
			best, found = r, true
		}
	}
	return best, found
}
