package rules

// CI:
//
//	drone.failing          the default branch has been red for hours
//	cross.release_red_ci   a release went out while its repo's last build
//	                       before it was red (GitHub releases × CI history
//	                       of any provider, sources.CISource)
//	cross.ci_red_deployed  Komodo deployed a stack while the CI of its repo
//	                       was red for the deployed commit (or, without
//	                       one, for the last build before the deploy)
//
//	stack ──► repo: option ci_repos {stack: owner/repo}
//	                › the stack's git repo in Komodo
//	                › a CI repo named like the stack ("studio/showreel" ↔ showreel)

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
	Register("cross.ci_red_deployed", Cross, map[string]any{"days": 7.0}, ciRedDeployed)
}

// ciReposOption is the Komodo option naming each stack's repo when
// neither Komodo nor the names tell it: {ci_repos: {web: studio/website}}.
const ciReposOption = "ci_repos"

// minHash is the shortest commit prefix that counts as the same commit.
const minHash = 7

func ciRedDeployed(_ any, cfg map[string]any, env Env) []Finding {
	komodo, ok := env.Datasets[komodoSvc].(*sources.KomodoDataset)
	if !ok || len(komodo.Deployed) == 0 {
		return nil
	}
	history := ciHistory(env)
	since := time.Now().AddDate(0, 0, -int(cfgFloat(cfg, "days")))
	repoOf := stackRepos(komodo, env.Options[komodoSvc], history)

	var found []Finding
	for _, d := range komodo.Deployed {
		repo := repoOf[strings.ToLower(d.Stack)]
		if !d.OK || d.At.Before(since) || repo == "" {
			continue
		}
		run, ok := deployedRun(history[repo], d)
		if !ok || run.Status != sources.CIFailed {
			continue
		}
		found = append(found, Finding{Fingerprint: d.Stack + "@" + d.At.UTC().Format(time.RFC3339), Severity: enums.SeverityWarn,
			Message: "cross.ci_red_deployed", Sources: []string{komodoSvc},
			Params: map[string]any{"stack": d.Stack, "repo": repo, "day": Day(d.At), "build": run.Number, "by": d.By}})
	}
	return found
}

// stackRepos maps lower-case stack names to lower-case CI repo names.
func stackRepos(komodo *sources.KomodoDataset, options map[string]any, history map[string][]sources.CIRun) map[string]string {
	byName := map[string]string{} // "showreel" → "studio/showreel"
	for repo := range history {
		_, name, _ := strings.Cut(repo, "/")
		byName[name] = repo
	}
	mapped := map[string]string{}
	option, _ := options[ciReposOption].(map[string]any)
	for stack, repo := range option {
		if name, ok := repo.(string); ok {
			mapped[strings.ToLower(stack)] = strings.ToLower(strings.TrimSpace(name))
		}
	}

	out := map[string]string{}
	for _, s := range komodo.Stacks {
		key := strings.ToLower(s.Name)
		switch {
		case mapped[key] != "":
			out[key] = mapped[key]
		case s.Repo != "":
			out[key] = strings.ToLower(s.Repo)
		default:
			out[key] = byName[key]
		}
	}
	return out
}

// deployedRun is the run of the deployed commit, else the last run
// before the deploy.
func deployedRun(runs []sources.CIRun, d sources.KDeployed) (sources.CIRun, bool) {
	for _, r := range runs {
		if sameCommit(r.Commit, d.Commit) {
			return r, true
		}
	}
	return lastRunBefore(runs, d.At)
}

// sameCommit compares full and short hashes: "abc1234" = "abc1234f00…".
func sameCommit(a, b string) bool {
	if len(a) < minHash || len(b) < minHash {
		return false
	}
	if len(a) > len(b) {
		a, b = b, a
	}
	return strings.HasPrefix(strings.ToLower(b), strings.ToLower(a))
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
