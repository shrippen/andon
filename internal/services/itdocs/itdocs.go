// Package itdocs hands the docs.* findings to Hansei, one per stack or
// note (the hints group them per host): what to document, which link is
// broken, which deprecated note still has a stack.
//
//	Gitea connections of the caller ─► stored dataset ─► metrics.CheckDocs
//	                                                         │
//	           Report{Complete, Findings: rule, host, stack, note, compose excerpt}
//
// Complete is false while a connection's stacks or notes are unknown:
// then a missing finding does not mean the docs were fixed.
package itdocs

import (
	"context"
	"database/sql"

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
)

// Report is every finding of the caller's Gitea connections.
type Report struct {
	Complete bool
	Findings []Finding
}

// Finding is one stack or note to fix; Compose is the compose file's
// link, Services its excerpt (no environment, it is never read).
type Finding struct {
	Rule             string
	Host, Stack      string // "" for an orphan link
	Note, Path, Link string // note name, vault path, its Compose link; "" for a missing note
	NoteURL, Compose string
	Services         []sources.ComposeService
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
	read, unread := 0, 0
	for _, c := range conns {
		if c.Service != string(enums.ServiceGitea) {
			continue
		}
		res, err := svcdata.Get(ctx, d, sources.DataKey(enums.ServiceGitea), nil, c, model.UserHolder(who.UserID), svcdata.Stored)
		data, _ := res.Data.(*sources.GiteaDataset)
		check, ok := metrics.CheckDocs(data)
		if err != nil || !ok {
			unread++
			continue
		}
		read++
		out.Findings = append(out.Findings, findingsOf(check)...)
	}
	out.Complete = read > 0 && unread == 0
	return out, nil
}

// findingsOf turns a comparison into findings.
func findingsOf(check metrics.DocsCheck) []Finding {
	var out []Finding
	for _, s := range check.Missing {
		out = append(out, Finding{Rule: RuleMissing, Host: s.Host, Stack: s.Name, Compose: s.URL, Services: s.Services})
	}
	for _, l := range check.Orphans {
		out = append(out, Finding{Rule: RuleOrphan, Note: l.Note.Name, Path: l.Note.Path, Link: l.Link, NoteURL: l.Note.URL})
	}
	for _, l := range check.DeprecatedLive {
		out = append(out, Finding{Rule: RuleDeprecatedLive, Host: l.Stack.Host, Stack: l.Stack.Name, Note: l.Note.Name, Path: l.Note.Path,
			Link: l.Link, NoteURL: l.Note.URL, Compose: l.Stack.URL, Services: l.Stack.Services})
	}
	return out
}
