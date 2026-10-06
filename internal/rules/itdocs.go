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

func docsMissing(data *sources.GiteaDataset, cfg map[string]any, env Env) []Finding {
	check, ok := metrics.CheckDocs(data)
	if !ok {
		return nil
	}

	byHost := map[string][]aged{}
	var hosts []string
	for _, s := range check.Missing {
		if _, seen := byHost[s.Host]; !seen {
			hosts = append(hosts, s.Host)
		}
		byHost[s.Host] = append(byHost[s.Host], aged{name: s.Name})
	}

	var found []Finding
	for _, host := range hosts {
		stacks := byHost[host]
		repo := strings.TrimRight(data.URL, "/") + "/" + check.MissingRepo(host)
		found = append(found, svcFinding(giteaSvc, "docs.missing", "missing:"+host, "docs.missing", enums.SeverityInfo, repo,
			map[string]any{"host": host, "count": len(stacks), "names": agedNames(stacks)}))
	}
	return found
}

func docsOrphan(data *sources.GiteaDataset, cfg map[string]any, env Env) []Finding {
	check, ok := metrics.CheckDocs(data)
	if !ok {
		return nil
	}

	var found []Finding
	for _, l := range check.Orphans {
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

	var found []Finding
	for _, l := range check.DeprecatedLive {
		found = append(found, svcFinding(giteaSvc, "docs.deprecated_live", "deprecated:"+l.Note.Path+"|"+l.Stack.Host+"/"+l.Stack.Name, "docs.deprecated_live", enums.SeverityWarn, l.Stack.URL,
			map[string]any{"note": l.Note.Name, "path": l.Note.Path, "host": l.Stack.Host, "stack": l.Stack.Name}))
	}
	return found
}
