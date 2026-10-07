// Package homelable draws the IT docs into Homelable (Phase 15): after
// every analysis run and on demand from the connection's record.
//
//	Gitea of the same Verbund ─► stacks + vault notes ─► metrics.BuildDocGraph
//	                                                          │ texts (option lang)
//	stored state (IDs, hashes) ─► outbound.SyncHomelable ◄────┘
//	                                   │
//	                    state + log ◄──┘  (record tab "Sync")
//
// Homelable only shows; Obsidian stays the source. A demo:// connection
// syncs into an in-memory stand-in.
package homelable

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"log/slog"
	"strconv"
	"strings"
	"sync"
	"time"

	"andon/internal/db"
	"andon/internal/enums"
	"andon/internal/i18n"
	"andon/internal/metrics"
	"andon/internal/model"
	"andon/internal/outbound"
	"andon/internal/repos/content"
	"andon/internal/repos/data"
	linkrepo "andon/internal/repos/links"
	"andon/internal/services/access"
	"andon/internal/services/connections"
	"andon/internal/services/svcdata"
	"andon/internal/services/verbund"
	"andon/internal/sources"
)

// JobName is the background job; the analysis run kicks it when done.
const JobName = "homelable"

// Connection options.
const (
	optVault = "vault" // Obsidian vault name for obsidian:// links
	optLang  = "lang"  // language of the labels on the canvas

	// optDocsRepo marks a Gitea connection that reads a vault (sources/itdocs.go).
	optDocsRepo = "docs_repo"
)

// Problems that stop a sync, as text keys.
const (
	ProblemNoDocs     = "homelable.problem_no_docs"
	ProblemIncomplete = "homelable.problem_incomplete"
	ProblemShared     = "homelable.problem_shared"
	ProblemFailed     = "homelable.problem_failed"
)

// Log is a connection's last sync, as its record shows it.
type Log struct {
	At                                   time.Time
	Created, Updated, Removed, Unchanged int
	Errors                               []string
	Problem                              string // text key; "" when the canvas was synced
	Detail                               string // the error behind ProblemFailed
	Source                               string // the Gitea connections drawn
}

// record is what a connection keeps between syncs.
type record struct {
	State outbound.HomelableState
	Log   Log
}

// running serialises syncs: the job and a click never write at once.
var running sync.Mutex

// SyncAll syncs every fixed Homelable connection (background job); one
// failing connection does not stop the others.
func SyncAll(ctx context.Context, d *sql.DB) error {
	var conns []*model.Connection
	err := db.WithRead(d, func(tx *sql.Tx) error {
		var err error
		conns, err = content.AllConnections(tx)
		return err
	})
	if err != nil {
		return err
	}
	for _, c := range conns {
		if c.Service != string(enums.ServiceHomelable) {
			continue
		}
		if l := syncConn(ctx, d, c, time.Now().UTC()); l.Problem != "" {
			slog.Warn("homelable: sync stopped", "connection", c.Name, "problem", l.Problem, "detail", l.Detail)
		}
	}
	return nil
}

// Sync syncs one connection now ("Jetzt abgleichen"); EDIT is needed.
func Sync(ctx context.Context, d *sql.DB, who *access.Principal, connID int64) (Log, error) {
	if err := connections.Writable(d, who, connID); err != nil {
		return Log{}, err
	}
	conn, err := connOf(d, connID)
	if err != nil {
		return Log{}, err
	}
	return syncConn(ctx, d, conn, time.Now().UTC()), nil
}

// Last is a connection's last sync; ok is false before the first. VIEW
// is needed.
func Last(d *sql.DB, who *access.Principal, connID int64) (Log, bool, error) {
	v, err := connections.Get(d, who, connID)
	if err != nil {
		return Log{}, false, err
	}
	if err := access.Need(v.Right, enums.RightView); err != nil {
		return Log{}, false, err
	}
	rec, err := load(d, connID)
	return rec.Log, !rec.Log.At.IsZero(), err
}

func connOf(d *sql.DB, connID int64) (*model.Connection, error) {
	var conn *model.Connection
	err := db.WithRead(d, func(tx *sql.Tx) error {
		var err error
		conn, err = content.Connection(tx, connID)
		return err
	})
	if err == nil && (conn == nil || conn.Service != string(enums.ServiceHomelable)) {
		err = connections.ErrNotFound
	}
	return conn, err
}

// syncConn runs one sync and stores its state and log.
func syncConn(ctx context.Context, d *sql.DB, conn *model.Connection, now time.Time) Log {
	running.Lock()
	defer running.Unlock()

	rec, err := load(d, conn.ID)
	if err != nil {
		return Log{At: now, Problem: ProblemFailed, Detail: err.Error()}
	}
	state, log := draw(ctx, d, conn, rec.State, now)
	rec.State, rec.Log = state, log
	if err := store(d, conn.ID, rec); err != nil {
		slog.Error("homelable: state not stored", "connection", conn.Name, "err", err)
	}
	svcdata.Forget(conn.ID) // the canvas list shows the new count
	return log
}

// draw builds the drawing and sends it; on a problem the state stays.
func draw(ctx context.Context, d *sql.DB, conn *model.Connection, prev outbound.HomelableState, now time.Time) (outbound.HomelableState, Log) {
	fail := func(problem string, detail string) (outbound.HomelableState, Log) {
		return prev, Log{At: now, Problem: problem, Detail: detail}
	}
	if conn.CredentialMode == enums.CredentialPersonal {
		return fail(ProblemShared, "")
	}

	docs, names, err := docsOf(ctx, d, conn)
	if err != nil {
		return fail(ProblemFailed, err.Error())
	}
	if len(names) == 0 {
		return fail(ProblemNoDocs, "")
	}
	vault := optStr(conn.Options, optVault)
	if vault == "" && len(docs.Notes) > 0 {
		vault = metrics.VaultOf(docs.Notes[0].URL)
	}
	graph, ok := metrics.BuildDocGraph(docs, vault, now)
	if !ok {
		return fail(ProblemIncomplete, "")
	}

	api, err := apiOf(d, conn)
	if err != nil {
		return fail(ProblemFailed, err.Error())
	}
	loc := i18n.Pick(optStr(conn.Options, optLang))
	state, sent, err := outbound.SyncHomelable(ctx, api, drawingOf(graph, loc), prev, now)
	if err != nil {
		return fail(ProblemFailed, err.Error())
	}
	return state, Log{At: now, Created: sent.Created, Updated: sent.Updated, Removed: sent.Removed, Unchanged: sent.Unchanged,
		Errors: sent.Errors, Source: strings.Join(names, ", ")}
}

// apiOf is the connection's Homelable: the live one with its login, or
// the demo's stand-in.
func apiOf(d *sql.DB, conn *model.Connection) (outbound.HomelableAPI, error) {
	if sources.IsDemo(conn.URL) {
		return outbound.HomelableDemo(strconv.FormatInt(conn.ID, 10)), nil
	}
	sctx, err := svcdata.SourceCtx(d, conn, model.NoHolder)
	if err != nil {
		return nil, err
	}
	if sctx.Secret == "" {
		return nil, errors.New("credential.missing")
	}
	user, password, _ := strings.Cut(sctx.Secret, ":")
	return outbound.HomelableLive(outbound.HomelableLogin{URL: conn.URL, User: user, Password: password, VerifyTLS: conn.VerifyTLS}), nil
}

// docsOf merges the stacks and notes of the Gitea connections that work
// with conn (its Verbund in its space); names lists them.
func docsOf(ctx context.Context, d *sql.DB, conn *model.Connection) (*sources.GiteaDataset, []string, error) {
	var list []*model.Connection
	var links []linkrepo.Link
	err := db.WithRead(d, func(tx *sql.Tx) error {
		var err error
		if list, err = content.Connections(tx, []int64{conn.SpaceID}); err != nil {
			return err
		}
		links, err = linkrepo.All(tx)
		return err
	})
	if err != nil {
		return nil, nil, err
	}

	merged := &sources.GiteaDataset{StacksRead: true, NotesRead: true}
	var names []string
	groups, _ := verbund.Groups(list, links)
	for _, g := range groups {
		if g.Conns[conn.Service] == nil || g.Conns[conn.Service].ID != conn.ID {
			continue
		}
		gitea := g.Fan[string(enums.ServiceGitea)]
		if c := g.Conns[string(enums.ServiceGitea)]; c != nil {
			gitea = []*model.Connection{c}
		}
		for _, c := range gitea {
			if c.CredentialMode == enums.CredentialPersonal {
				continue
			}
			res, err := svcdata.Get(ctx, d, sources.DataKey(enums.ServiceGitea), nil, c, model.NoHolder, svcdata.Cached)
			ds, _ := res.Data.(*sources.GiteaDataset)
			vault := optStr(c.Options, optDocsRepo) != ""
			switch {
			case (ds == nil || !ds.NotesRead) && !vault:
				continue // reads no vault: no docs to draw
			case err != nil || ds == nil:
				merged.StacksRead, merged.NotesRead = false, false
				names = append(names, c.Name)
				continue
			}
			merged.Stacks = append(merged.Stacks, ds.Stacks...)
			merged.Notes = append(merged.Notes, ds.Notes...)
			merged.StacksRead = merged.StacksRead && ds.StacksRead
			merged.NotesRead = merged.NotesRead && ds.NotesRead
			names = append(names, c.Name)
		}
		break
	}
	return merged, names, nil
}

// drawingOf turns the graph into Homelable's nodes, texts in loc.
func drawingOf(g metrics.DocGraph, loc enums.Locale) outbound.HomelableDrawing {
	t := func(key string, params map[string]any) string { return i18n.T("homelable."+key, loc, params) }
	out := outbound.HomelableDrawing{Canvas: t("canvas", nil)}
	for _, n := range g.Nodes {
		node := outbound.HomelableNode{Key: n.Key, Parent: n.Parent, Kind: n.Kind, Label: n.Label, Hostname: n.Hostname,
			Check: n.Check, CheckTarget: n.CheckTarget, Stale: n.Stale}
		for _, p := range n.Props {
			value := p.Value
			switch {
			case p.Text != "":
				value = t(string(p.Text), nil)
			case p.Stale:
				value = t("stale", map[string]any{"day": p.Value})
			}
			node.Props = append(node.Props, outbound.HomelableProp{Key: t("prop_"+string(p.Key), nil), Value: value})
		}
		out.Nodes = append(out.Nodes, node)
	}
	for _, e := range g.Edges {
		out.Edges = append(out.Edges, outbound.HomelableEdge{Key: e.Key, From: e.From, To: e.To, Kind: e.Kind})
	}
	return out
}

func load(d *sql.DB, connID int64) (record, error) {
	var raw map[string]any
	err := db.WithRead(d, func(tx *sql.Tx) error {
		var err error
		raw, err = data.SyncState(tx, connID)
		return err
	})
	if err != nil {
		return record{}, err
	}
	var rec record
	text, err := json.Marshal(raw)
	if err == nil {
		err = json.Unmarshal(text, &rec)
	}
	return rec, err
}

func store(d *sql.DB, connID int64, rec record) error {
	text, err := json.Marshal(rec)
	if err != nil {
		return err
	}
	var raw map[string]any
	if err := json.Unmarshal(text, &raw); err != nil {
		return err
	}
	return db.WithTx(d, func(tx *sql.Tx) error { return data.SetSyncState(tx, connID, raw) })
}

func optStr(options map[string]any, key string) string {
	s, _ := options[key].(string)
	return strings.TrimSpace(s)
}

// Opens reports whether a connection's address opens in a browser: a
// demo:// one has nothing to show.
func Opens(rawURL string) bool { return !sources.IsDemo(rawURL) }
