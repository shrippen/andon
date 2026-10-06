package rules

import (
	"strings"

	"andon/internal/enums"
	"andon/internal/metrics"
	"andon/internal/sources"
)

func init() {
	registerDocs()
}

// IT docs against compose stacks (Phase 15): the findings are the list
// of notes to write or fix; Hansei picks them up.
func registerDocs() {
	// One hint per host: the stacks no active note links.
	Register("docs.missing", giteaSvc, nil, on(docsMissing))

	// An active note links a compose file that is gone.
	Register("docs.orphan", giteaSvc, nil, on(docsOrphan))

	// A note is deprecated, its stack still lies in the repo.
	Register("docs.deprecated_live", giteaSvc, nil, on(docsDeprecatedLive))
}

// claimedOf is the finding ids Hansei works on, from its pushed state.
func claimedOf(env Env) map[string]bool {
	h, _ := env.Datasets[string(enums.ServiceHansei)].(*sources.HanseiDataset)
	return metrics.Claimed(h)
}

func docsMissing(data *sources.GiteaDataset, cfg map[string]any, env Env) []Finding {
	check, ok := metrics.CheckDocs(data)
	if !ok {
		return nil
	}
	claimed := claimedOf(env)

	// Per host: the gaps nobody works on, and how many Hansei has.
	byHost := map[string][]aged{}
	inWork := map[string]int{}
	seen := map[string]bool{}
	var hosts []string
	for _, s := range check.Missing {
		if !seen[s.Host] {
			seen[s.Host] = true
			hosts = append(hosts, s.Host)
		}
		if claimed[metrics.MissingID(s)] {
			inWork[s.Host]++
			continue
		}
		byHost[s.Host] = append(byHost[s.Host], aged{name: s.Name})
	}

	var found []Finding
	for _, host := range hosts {
		stacks := byHost[host]
		if len(stacks) == 0 {
			continue
		}
		msg, params := "docs.missing", map[string]any{"host": host, "count": len(stacks), "names": agedNames(stacks)}
		if inWork[host] > 0 {
			msg, params["claimed"] = "docs.missing_claimed", inWork[host]
		}
		repo := strings.TrimRight(data.URL, "/") + "/" + check.MissingRepo(host)
		found = append(found, svcFinding(giteaSvc, "docs.missing", "missing:"+host, msg, enums.SeverityInfo, repo, params))
	}
	return found
}

func docsOrphan(data *sources.GiteaDataset, cfg map[string]any, env Env) []Finding {
	check, ok := metrics.CheckDocs(data)
	if !ok {
		return nil
	}
	claimed := claimedOf(env)

	var found []Finding
	for _, l := range check.Orphans {
		if claimed[metrics.OrphanID(l)] {
			continue
		}
		found = append(found, svcFinding(giteaSvc, "docs.orphan", "orphan:"+l.Note.Path+"|"+l.Link, "docs.orphan", enums.SeverityWarn, l.Note.URL,
			map[string]any{"note": l.Note.Name, "path": l.Note.Path, "link": l.Link}))
	}
	return found
}

func docsDeprecatedLive(data *sources.GiteaDataset, cfg map[string]any, env Env) []Finding {
	check, ok := metrics.CheckDocs(data)
	if !ok {
		return nil
	}
	claimed := claimedOf(env)

	var found []Finding
	for _, l := range check.DeprecatedLive {
		if claimed[metrics.DeprecatedID(l)] {
			continue
		}
		found = append(found, svcFinding(giteaSvc, "docs.deprecated_live", "deprecated:"+l.Note.Path+"|"+l.Stack.Host+"/"+l.Stack.Name, "docs.deprecated_live", enums.SeverityWarn, l.Stack.URL,
			map[string]any{"note": l.Note.Name, "path": l.Note.Path, "host": l.Stack.Host, "stack": l.Stack.Name}))
	}
	return found
}
