package outbound

// Homelable: Andon draws the IT docs onto a canvas of its own and keeps
// it in step (services/homelable builds the drawing). Hand-drawn
// canvases are never touched; Andon only changes the nodes and edges it
// created, found by the IDs it stored for each key:
//
//	drawing ─┐                    ┌─ POST   new key / gone by hand
//	         ├─► diff by key ─────┼─ PATCH  payload hash changed
//	state  ──┘   (IDs, hashes)    ├─ –      same hash: nothing sent
//	                              └─ DELETE key no longer drawn
//
// Positions are never sent: Homelable places a new node, the user's
// layout stays. Neither are ip or mac: Homelable would merge the node
// with a hand-drawn device of that address (inventory row).

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"slices"
	"time"

	"andon/internal/drivers/services"
	"andon/internal/metrics"
)

// HomelableAPI is what a sync needs of Homelable: reads and writes
// against /api/v1/<path>. The live driver and the demo's fake answer it.
type HomelableAPI interface {
	Get(ctx context.Context, path string) (any, error)
	Send(ctx context.Context, method, path string, body any) (any, error)
}

// HomelableLogin is a Homelable instance and its local user.
type HomelableLogin struct {
	URL, User, Password string
	VerifyTLS           bool
}

// HomelableLive talks to a real instance.
func HomelableLive(to HomelableLogin) HomelableAPI {
	return services.HomelableApi{URL: to.URL, User: to.User, Password: to.Password, Verify: to.VerifyTLS}
}

// HomelableSignIn checks the login (connection test).
func HomelableSignIn(ctx context.Context, to HomelableLogin) error {
	return services.HomelableApi{URL: to.URL, User: to.User, Password: to.Password, Verify: to.VerifyTLS}.Login(ctx)
}

// HomelableNode is a node as Homelable shows it; texts are translated.
type HomelableNode struct {
	Key, Parent string
	Kind        metrics.GraphKind
	Label       string
	Hostname    string
	Props       []HomelableProp
	Check       metrics.CheckMethod
	CheckTarget string
	Stale       bool
}

// HomelableProp is one line of a node's properties.
type HomelableProp struct{ Key, Value string }

// HomelableEdge connects two nodes by key.
type HomelableEdge struct {
	Key, From, To string
	Kind          metrics.EdgeKind
}

// HomelableDrawing is the canvas Andon wants to see.
type HomelableDrawing struct {
	Canvas string // name of the canvas, used to find or create it
	Nodes  []HomelableNode
	Edges  []HomelableEdge
}

// HomelableState is what Andon remembers of its canvas between syncs.
type HomelableState struct {
	Canvas string                  `json:"canvas"`
	Nodes  map[string]HomelableRef `json:"nodes"`
	Edges  map[string]HomelableRef `json:"edges"`
}

// HomelableRef is the Homelable ID of a key and the hash of what was
// sent for it last.
type HomelableRef struct {
	ID   string `json:"id"`
	Hash string `json:"hash"`
}

// HomelableLog counts what one sync did; Errors names what failed.
type HomelableLog struct {
	At                                   time.Time
	Created, Updated, Removed, Unchanged int
	Errors                               []string
}

// Node and edge types and colours of Homelable (frontend/src/types).
// The colours are Homelable's canvas palette, not Andon's GUI.
const (
	typeZone      = "groupRect"
	typeHost      = "docker_host"
	typeContainer = "docker_container"
	typeService   = "generic"
	typeNetwork   = "generic"
	edgeDepends   = "virtual"
	edgeNetwork   = "vlan"
	canvasType    = "network"
	canvasIcon    = "network"
	colorFaded    = "#484f58"
	colorStale    = "#d29922"
)

// SyncHomelable brings Andon's canvas in line with d and returns the
// new state. err is set when the canvas itself is out of reach; single
// nodes that fail are named in the log and tried again next time.
func SyncHomelable(ctx context.Context, api HomelableAPI, d HomelableDrawing, prev HomelableState, now time.Time) (HomelableState, HomelableLog, error) {
	log := HomelableLog{At: now}
	s := syncer{ctx: ctx, api: api, log: &log, next: HomelableState{Nodes: map[string]HomelableRef{}, Edges: map[string]HomelableRef{}}}

	canvas, err := s.canvas(d.Canvas, prev.Canvas)
	if err != nil {
		return prev, log, err
	}
	s.next.Canvas = canvas
	liveNodes, err := s.live("nodes", canvas)
	if err != nil {
		return prev, log, err
	}
	liveEdges, err := s.live("edges", canvas)
	if err != nil {
		return prev, log, err
	}

	// Parents before children, so a child knows its parent's ID.
	for _, n := range byDepth(d.Nodes) {
		s.node(n, prev.Nodes[n.Key], liveNodes)
	}
	for _, e := range d.Edges {
		s.edge(e, prev.Edges[e.Key], liveEdges)
	}

	// What is no longer drawn goes: edges first, then the nodes.
	s.remove("edges", prev.Edges, s.next.Edges, liveEdges)
	s.remove("nodes", prev.Nodes, s.next.Nodes, liveNodes)
	return s.next, log, nil
}

type syncer struct {
	ctx  context.Context
	api  HomelableAPI
	log  *HomelableLog
	next HomelableState
}

// canvas finds Andon's canvas by its stored ID, else by name, else
// creates it.
func (s *syncer) canvas(name, known string) (string, error) {
	raw, err := s.api.Get(s.ctx, "designs")
	if err != nil {
		return "", err
	}
	byName := ""
	for _, item := range asItems(raw) {
		id, _ := item["id"].(string)
		if id != "" && id == known {
			return id, nil
		}
		if item["name"] == name && byName == "" {
			byName = id
		}
	}
	if byName != "" {
		return byName, nil
	}

	created, err := s.api.Send(s.ctx, http.MethodPost, "designs", map[string]any{"name": name, "design_type": canvasType, "icon": canvasIcon})
	if err != nil {
		return "", err
	}
	id, _ := asItem(created)["id"].(string)
	return id, nil
}

// live lists the IDs of the canvas' nodes or edges.
func (s *syncer) live(kind, canvas string) (map[string]bool, error) {
	raw, err := s.api.Get(s.ctx, kind)
	if err != nil {
		return nil, err
	}
	out := map[string]bool{}
	for _, item := range asItems(raw) {
		if id, _ := item["id"].(string); id != "" && item["design_id"] == canvas {
			out[id] = true
		}
	}
	return out, nil
}

// node creates or updates one node; an unchanged one sends nothing.
func (s *syncer) node(n HomelableNode, was HomelableRef, live map[string]bool) {
	parent := ""
	if n.Parent != "" {
		parent = s.next.Nodes[n.Parent].ID
	}
	body := nodeBody(n, parent)
	hash := hashOf(body)

	switch {
	case was.ID != "" && live[was.ID] && was.Hash == hash:
		s.log.Unchanged++
		s.next.Nodes[n.Key] = was
	case was.ID != "" && live[was.ID]:
		if _, err := s.api.Send(s.ctx, http.MethodPatch, "nodes/"+was.ID, body); err != nil {
			s.fail(n.Label, err)
			s.next.Nodes[n.Key] = HomelableRef{ID: was.ID}
			return
		}
		s.log.Updated++
		s.next.Nodes[n.Key] = HomelableRef{ID: was.ID, Hash: hash}
	default:
		body["design_id"] = s.next.Canvas
		created, err := s.api.Send(s.ctx, http.MethodPost, "nodes", body)
		id, _ := asItem(created)["id"].(string)
		if err != nil || id == "" {
			s.fail(n.Label, err)
			return
		}
		s.log.Created++
		s.next.Nodes[n.Key] = HomelableRef{ID: id, Hash: hash}
	}
}

// edge creates one edge; Homelable cannot move an edge's ends, so a
// changed one is replaced.
func (s *syncer) edge(e HomelableEdge, was HomelableRef, live map[string]bool) {
	from, to := s.next.Nodes[e.From].ID, s.next.Nodes[e.To].ID
	if from == "" || to == "" {
		return // an end failed; its error is logged
	}
	body := map[string]any{"source": from, "target": to, "type": edgeType(e.Kind)}
	hash := hashOf(body)
	if was.ID != "" && live[was.ID] && was.Hash == hash {
		s.log.Unchanged++
		s.next.Edges[e.Key] = was
		return
	}

	replaced := was.ID != "" && live[was.ID]
	if replaced {
		if _, err := s.api.Send(s.ctx, http.MethodDelete, "edges/"+was.ID, nil); err != nil && services.HomelableStatus(err) != http.StatusNotFound {
			s.fail(e.Key, err)
			s.next.Edges[e.Key] = HomelableRef{ID: was.ID}
			return
		}
	}
	body["design_id"] = s.next.Canvas
	created, err := s.api.Send(s.ctx, http.MethodPost, "edges", body)
	id, _ := asItem(created)["id"].(string)
	if err != nil || id == "" {
		s.fail(e.Key, err)
		return
	}
	if replaced {
		s.log.Updated++
	} else {
		s.log.Created++
	}
	s.next.Edges[e.Key] = HomelableRef{ID: id, Hash: hash}
}

// remove deletes what was synced before and is no longer drawn; one
// already gone by hand is just forgotten.
func (s *syncer) remove(kind string, prev, next map[string]HomelableRef, live map[string]bool) {
	for key, ref := range prev {
		if _, kept := next[key]; kept {
			continue
		}
		if !live[ref.ID] {
			continue
		}
		_, err := s.api.Send(s.ctx, http.MethodDelete, kind+"/"+ref.ID, nil)
		if err != nil && services.HomelableStatus(err) != http.StatusNotFound {
			s.fail(key, err)
			next[key] = HomelableRef{ID: ref.ID}
			continue
		}
		s.log.Removed++
	}
}

func (s *syncer) fail(what string, err error) {
	msg := "no id"
	if err != nil {
		msg = err.Error()
	}
	s.log.Errors = append(s.log.Errors, what+": "+msg)
}

// nodeBody is everything Andon sets on a node; never a position.
func nodeBody(n HomelableNode, parent string) map[string]any {
	body := map[string]any{"label": n.Label, "parent_id": nilIfEmpty(parent)}
	if n.Kind == metrics.GraphZone {
		body["type"] = typeZone
		return body
	}

	props := make([]map[string]any, 0, len(n.Props))
	for _, p := range n.Props {
		props = append(props, map[string]any{"key": p.Key, "value": p.Value, "icon": nil, "visible": true})
	}
	body["type"] = nodeType(n.Kind)
	body["properties"] = props
	body["hostname"] = nilIfEmpty(n.Hostname)
	body["check_method"] = nilIfEmpty(string(n.Check))
	body["check_target"] = nilIfEmpty(n.CheckTarget)
	body["container_mode"] = n.Kind == metrics.GraphHost
	body["custom_colors"] = colorsOf(n)
	return body
}

func nodeType(kind metrics.GraphKind) string {
	switch kind {
	case metrics.GraphHost:
		return typeHost
	case metrics.GraphStack, metrics.GraphMissing:
		return typeContainer
	case metrics.GraphNetwork:
		return typeNetwork
	}
	return typeService
}

func edgeType(kind metrics.EdgeKind) string {
	if kind == metrics.EdgeNetwork {
		return edgeNetwork
	}
	return edgeDepends
}

// colorsOf pales a stack without docs and marks an old review.
func colorsOf(n HomelableNode) any {
	switch {
	case n.Kind == metrics.GraphMissing:
		return map[string]any{"border": colorFaded}
	case n.Stale:
		return map[string]any{"border": colorStale}
	}
	return nil
}

// byDepth orders nodes so a parent comes before its children.
func byDepth(nodes []HomelableNode) []HomelableNode {
	parent := map[string]string{}
	for _, n := range nodes {
		parent[n.Key] = n.Parent
	}
	depth := func(key string) int {
		d := 0
		for p := parent[key]; p != "" && d < len(nodes); p = parent[p] {
			d++
		}
		return d
	}
	out := slices.Clone(nodes)
	slices.SortStableFunc(out, func(a, b HomelableNode) int { return depth(a.Key) - depth(b.Key) })
	return out
}

func hashOf(body map[string]any) string {
	raw, _ := json.Marshal(body) // map keys are sorted
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:8])
}

func nilIfEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func asItems(raw any) []map[string]any {
	list, _ := raw.([]any)
	out := make([]map[string]any, 0, len(list))
	for _, item := range list {
		if m, ok := item.(map[string]any); ok {
			out = append(out, m)
		}
	}
	return out
}

func asItem(raw any) map[string]any {
	m, _ := raw.(map[string]any)
	return m
}
