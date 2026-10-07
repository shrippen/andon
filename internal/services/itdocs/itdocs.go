// Package itdocs hands the docs.* findings to Hansei, one per stack or
// note (the hints group them per host): what to document, which link is
// broken, which deprecated note still has a stack.
//
//	Gitea connections of the caller ─► stored dataset ─► metrics.CheckDocs
//	Komodo of the same space ──────────────────────────► metrics.CheckDeploys
//	                                                         │
//	           Report{Complete, Findings: rule, host, stack, note, compose excerpt}
//
// Complete is false while a connection's stacks or notes, or a Komodo of
// its space, are unknown: then a missing finding does not mean the docs
// were fixed.
package itdocs

import (
	"context"
	"database/sql"
	"maps"
	"slices"

	"andon/internal/db"
	"andon/internal/enums"
	"andon/internal/metrics"
	"andon/internal/model"
	"andon/internal/repos/content"
	"andon/internal/services/access"
	"andon/internal/services/svcdata"
	"andon/internal/sources"
)

// Rules of the findings (internal/rules/itdocs.go).
const (
	RuleMissing        = "docs.missing"
	RuleOrphan         = "docs.orphan"
	RuleDeprecatedLive = "docs.deprecated_live"
	RuleNotDeployed    = "docs.not_deployed"
	RuleUnknown        = "docs.deployed_unknown"
	RuleDrift          = "docs.drift"
)

// Report is every finding of the caller's Gitea connections.
type Report struct {
	Complete bool
	Findings []Finding
}

// Finding is one stack or note to fix; Compose is the compose file's
// link, Services its excerpt (no environment, it is never read). A stack
// only Komodo knows has no Compose; its Services are what Komodo runs.
type Finding struct {
	ID               string // what Hansei claims in its pushed state
	Rule             string
	Host, Stack      string // "" for an orphan link
	Note, Path, Link string // note name, vault path, its Compose link; "" for a missing note
	NoteURL, Compose string
	Services         []sources.ComposeService
	Changes          []metrics.DriftField // docs.drift: field, note's value, compose's or Komodo's
}

// Findings collects the findings of every Gitea connection the caller sees.
func Findings(ctx context.Context, d *sql.DB, who *access.Principal) (Report, error) {
	var conns []*model.Connection
	err := db.WithRead(d, func(tx *sql.Tx) error {
		ids := make([]int64, 0, len(who.Spaces))
		for id := range who.Spaces {
			ids = append(ids, id)
		}
		var err error
		conns, err = content.Connections(tx, ids)
		return err
	})
	if err != nil {
		return Report{}, err
	}

	out := Report{}
	komodo, komodoOK := komodoBySpace(ctx, d, who, conns)
	read, unread := 0, 0
	for _, c := range conns {
		if c.Service != string(enums.ServiceGitea) {
			continue
		}
		data, err := stored[*sources.GiteaDataset](ctx, d, who, c)
		check, ok := metrics.CheckDocs(data)
		if err != nil || !ok {
			unread++
			continue
		}
		read++
		out.Findings = append(out.Findings, findingsOf(check)...)

		if !komodoOK[c.SpaceID] {
			unread++
		}
		if deploys, ok := metrics.CheckDeploys(data, komodo[c.SpaceID]); ok {
			out.Findings = append(out.Findings, deployFindings(deploys)...)
		}
		drifts, _ := metrics.CheckDrift(data, komodo[c.SpaceID])
		out.Findings = append(out.Findings, driftFindings(drifts)...)
	}
	out.Complete = read > 0 && unread == 0
	return out, nil
}

// stored is a connection's stored dataset of type D; nil if none.
func stored[D any](ctx context.Context, d *sql.DB, who *access.Principal, c *model.Connection) (D, error) {
	res, err := svcdata.Get(ctx, d, sources.DataKey(enums.ServiceType(c.Service)), nil, c, model.UserHolder(who.UserID), svcdata.Stored)
	data, _ := res.Data.(D)
	return data, err
}

// komodoBySpace merges each space's Komodo datasets, as the rules see
// them; ok is false for a space whose Komodo could not be read.
func komodoBySpace(ctx context.Context, d *sql.DB, who *access.Principal, conns []*model.Connection) (map[int64]*sources.KomodoDataset, map[int64]bool) {
	data, ok := map[int64]*sources.KomodoDataset{}, map[int64]bool{}
	for _, c := range conns {
		if _, seen := ok[c.SpaceID]; !seen {
			ok[c.SpaceID] = true
		}
		if c.Service != string(enums.ServiceKomodo) {
			continue
		}
		k, err := stored[*sources.KomodoDataset](ctx, d, who, c)
		switch {
		case err != nil || k == nil:
			ok[c.SpaceID] = false
		case data[c.SpaceID] == nil:
			data[c.SpaceID] = k
		default:
			data[c.SpaceID] = data[c.SpaceID].Merge(k).(*sources.KomodoDataset)
		}
	}
	return data, ok
}

// findingsOf turns a comparison into findings.
func findingsOf(check metrics.DocsCheck) []Finding {
	var out []Finding
	for _, s := range check.Missing {
		out = append(out, Finding{ID: metrics.MissingID(s), Rule: RuleMissing, Host: s.Host, Stack: s.Name, Compose: s.URL, Services: s.Services})
	}
	for _, l := range check.Orphans {
		out = append(out, Finding{ID: metrics.OrphanID(l), Rule: RuleOrphan, Note: l.Note.Name, Path: l.Note.Path, Link: l.Link, NoteURL: l.Note.URL})
	}
	for _, l := range check.DeprecatedLive {
		out = append(out, Finding{ID: metrics.DeprecatedID(l), Rule: RuleDeprecatedLive, Host: l.Stack.Host, Stack: l.Stack.Name, Note: l.Note.Name, Path: l.Note.Path,
			Link: l.Link, NoteURL: l.Note.URL, Compose: l.Stack.URL, Services: l.Stack.Services})
	}
	return out
}

// deployFindings turns the Komodo comparison into findings: the compose
// excerpt of a stack not deployed, the images Komodo runs of one no repo
// holds.
func deployFindings(check metrics.DeployCheck) []Finding {
	var out []Finding
	for _, s := range check.NotDeployed {
		out = append(out, Finding{ID: metrics.NotDeployedID(s), Rule: RuleNotDeployed, Host: s.Host, Stack: s.Name, Compose: s.URL, Services: s.Services})
	}
	for _, u := range check.Unknown {
		f := Finding{ID: metrics.UnknownID(u), Rule: RuleUnknown, Host: u.Host, Stack: u.Stack.Name}
		for _, name := range slices.Sorted(maps.Keys(u.Stack.Images)) {
			f.Services = append(f.Services, sources.ComposeService{Name: name, Image: u.Stack.Images[name]})
		}
		out = append(out, f)
	}
	return out
}

// driftFindings names each note that differs from its stacks, with the
// fields old → new.
func driftFindings(drifts []metrics.Drift) []Finding {
	var out []Finding
	for _, d := range drifts {
		out = append(out, Finding{ID: metrics.DriftID(d), Rule: RuleDrift, Note: d.Note.Name, Path: d.Note.Path, NoteURL: d.Note.URL, Changes: d.Fields})
	}
	return out
}
