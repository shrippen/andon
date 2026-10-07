package metrics

// The IT docs as a graph (Phase 15), drawn by Homelable. Obsidian stays
// the only source; the graph is derived from the vault's frontmatter and
// the compose stacks:
//
//	zone (Orte of a device) ─┬─ host (device note or compose host)
//	                         │    ├─ service   (note in Dienste, Gerät = host)
//	                         │    ├─ stack     (Compose of a device or network note)
//	                         │    └─ missing   (stack no active note links)
//	network (note) ──────────┴── every host running one of its stacks
//	service ──depends──► service, host or network  (abhängig von)
//
// Keys are stable across runs: a note's path, a stack's host/name, a
// host's folded name, so the drawing keeps its Homelable IDs.

import (
	"net/url"
	"path"
	"slices"
	"strings"
	"time"

	"andon/internal/sources"
)

// GraphKind is what a node of the docs graph stands for.
type GraphKind string

const (
	GraphZone    GraphKind = "zone"
	GraphHost    GraphKind = "host"
	GraphService GraphKind = "service"
	GraphStack   GraphKind = "stack"   // infrastructure stack of a device or network note
	GraphMissing GraphKind = "missing" // stack without docs
	GraphNetwork GraphKind = "network"
)

// EdgeKind is what an edge of the docs graph stands for.
type EdgeKind string

const (
	EdgeDepends EdgeKind = "depends" // abhängig von
	EdgeNetwork EdgeKind = "network" // a host takes part in a network
)

// PropKey names a node property; the caller translates it.
type PropKey string

const (
	PropURL     PropKey = "url"
	PropPorts   PropKey = "ports"
	PropBackup  PropKey = "backup"
	PropSSO     PropKey = "sso"
	PropChecked PropKey = "checked"
	PropNote    PropKey = "note"
	PropDocs    PropKey = "docs"
)

// PropValue is a value the caller translates; "" for a literal Value.
type PropValue string

const (
	ValueSSOActive   PropValue = "sso_active"
	ValueSSOPossible PropValue = "sso_possible"
	ValueDocsMissing PropValue = "docs_missing"
)

// GraphProp is one property of a node; Stale marks an old review date.
type GraphProp struct {
	Key   PropKey
	Value string
	Text  PropValue
	Stale bool
}

// CheckMethod is how Homelable probes a node: "https", "http", "tcp".
type CheckMethod string

const (
	CheckNone  CheckMethod = ""
	CheckHTTP  CheckMethod = "http"
	CheckHTTPS CheckMethod = "https"
	CheckTCP   CheckMethod = "tcp"
)

// GraphNode is one node; Parent is the key of the node it sits in.
type GraphNode struct {
	Key, Parent string
	Kind        GraphKind
	Label       string
	Hostname    string // hosts: the name compose repos and checks use
	Props       []GraphProp
	Check       CheckMethod
	CheckTarget string
	Stale       bool // letzte Prüfung older than StaleCheck
}

// GraphEdge connects two nodes by key.
type GraphEdge struct {
	Key, From, To string
	Kind          EdgeKind
}

// DocGraph is the drawing of the IT docs.
type DocGraph struct {
	Nodes []GraphNode
	Edges []GraphEdge
}

// StaleCheck is when a note's "letzte Prüfung" counts as old.
const StaleCheck = 365 * 24 * time.Hour

// Vault folders that say what a note describes (IT/Geräte/Regis.md).
const (
	dirDevices  = "Geräte"
	dirServices = "Dienste"
	dirPlaces   = "Orte"
)

// netDirs hold network notes: a note there with a Compose field is a
// network (Tailscale, Pangolin) spanning the hosts of its stacks.
var netDirs = []string{"Netzwerk", "Allgemeines"}

// Key prefixes of the graph.
const (
	keyZone  = "zone:"
	keyHost  = "host:"
	keyNote  = "note:"
	keyStack = "stack:"
	keyNet   = "net:"
)

// graphBuild collects the graph while the notes are read.
type graphBuild struct {
	data   *sources.GiteaDataset
	vault  string
	now    time.Time
	out    DocGraph
	nodes  map[string]int // key → index in out.Nodes
	edges  map[string]bool
	byName map[string]string // note name (folded) → node key, for abhängig von
	linked map[int]bool      // stacks an active note links
}

// BuildDocGraph draws the docs of data; vault is the Obsidian vault's
// name for the notes' obsidian:// links ("" leaves them out). ok is
// false while stacks or notes are unknown: a partial graph would remove
// what is merely unread.
func BuildDocGraph(data *sources.GiteaDataset, vault string, now time.Time) (DocGraph, bool) {
	if data == nil || !data.StacksRead || !data.NotesRead {
		return DocGraph{}, false
	}
	b := &graphBuild{data: data, vault: vault, now: now, nodes: map[string]int{}, edges: map[string]bool{},
		byName: map[string]string{}, linked: map[int]bool{}}

	var active []sources.DocNote
	for _, n := range data.Notes {
		if !n.Deprecated {
			active = append(active, n)
		}
	}

	// Hosts first (in their zones), then what runs on them.
	for _, n := range active {
		if inDir(n.Path, dirDevices) {
			b.device(n)
		}
	}
	for _, n := range active {
		if inDir(n.Path, dirServices) {
			b.service(n)
		}
	}
	for _, n := range active {
		switch {
		case inDir(n.Path, dirDevices):
			b.infra(n)
		case isNetNote(n):
			b.network(n)
		}
	}
	b.missing()
	for _, n := range active {
		b.depends(n)
	}
	return b.out, true
}

// device adds a device note's host, inside the zone of its first place.
func (b *graphBuild) device(n sources.DocNote) {
	key := b.host(n.Name, n.Name)
	zone := ""
	if len(n.Places) > 0 {
		zone = b.zone(n.Places[0])
	}

	// Taken after the zone is added: adding may move the nodes.
	node := &b.out.Nodes[b.nodes[key]]
	node.Props = append(node.Props, b.noteProps(n)...)
	node.Stale = node.Stale || b.stale(n)
	if node.Parent == "" {
		node.Parent = zone
	}
	b.byName[foldName(n.Name)] = key
}

// host returns the key of a host, adding it on first sight.
func (b *graphBuild) host(name, label string) string {
	key := keyHost + foldName(name)
	if _, ok := b.nodes[key]; !ok {
		b.add(GraphNode{Key: key, Kind: GraphHost, Label: label, Hostname: foldName(name)})
		b.byName[foldName(name)] = key
	}
	return key
}

// zone returns the key of a place's zone, adding it on first sight;
// a note in Orte with the place's name gives it a link.
func (b *graphBuild) zone(place string) string {
	key := keyZone + foldName(place)
	if _, ok := b.nodes[key]; ok {
		return key
	}
	node := GraphNode{Key: key, Kind: GraphZone, Label: place}
	for _, n := range b.data.Notes {
		if inDir(n.Path, dirPlaces) && foldName(n.Name) == foldName(place) {
			node.Props = b.noteProps(n)
		}
	}
	b.add(node)
	return key
}

// service adds a service note on the host its Gerät (or its stack) names.
func (b *graphBuild) service(n sources.DocNote) {
	stacks := b.stacksOf(n)
	host := ""
	switch {
	case len(n.Devices) > 0:
		host = b.host(n.Devices[0], n.Devices[0])
	case len(stacks) > 0:
		host = b.host(stacks[0].Host, stacks[0].Host)
	}

	key := keyNote + n.Path
	node := GraphNode{Key: key, Parent: host, Kind: GraphService, Label: n.Name, Stale: b.stale(n)}
	ports := notePorts(n, stacks)
	if n.Web != "" {
		node.Props = append(node.Props, GraphProp{Key: PropURL, Value: n.Web})
	}
	if len(ports) > 0 {
		node.Props = append(node.Props, GraphProp{Key: PropPorts, Value: strings.Join(ports, ", ")})
	}
	if len(n.Backup) > 0 {
		node.Props = append(node.Props, GraphProp{Key: PropBackup, Value: strings.Join(n.Backup, ", ")})
	}
	if sso := ssoOf(n); sso != "" {
		node.Props = append(node.Props, GraphProp{Key: PropSSO, Text: sso})
	}
	node.Props = append(node.Props, b.noteProps(n)...)
	node.Check, node.CheckTarget = checkOf(n.Web, ports, b.hostnameOf(host))
	b.add(node)
	b.byName[foldName(n.Name)] = key
}

// infra adds the stacks a device or network note lists, each on the host
// it runs on; it returns the hosts.
func (b *graphBuild) infra(n sources.DocNote) []string {
	var hosts []string
	for _, s := range b.stacksOf(n) {
		host := b.host(s.Host, s.Host)
		key := keyStack + s.Host + "/" + s.Name
		if _, ok := b.nodes[key]; !ok && !b.documented(s) {
			node := GraphNode{Key: key, Parent: host, Kind: GraphStack, Label: s.Name}
			ports := stackPorts(s)
			if len(ports) > 0 {
				node.Props = append(node.Props, GraphProp{Key: PropPorts, Value: strings.Join(ports, ", ")})
			}
			b.add(node)
		}
		if !slices.Contains(hosts, host) {
			hosts = append(hosts, host)
		}
	}
	return hosts
}

// network adds a network note and connects it with the hosts of its stacks.
func (b *graphBuild) network(n sources.DocNote) {
	key := keyNet + n.Path
	b.add(GraphNode{Key: key, Kind: GraphNetwork, Label: n.Name, Props: b.noteProps(n), Stale: b.stale(n)})
	b.byName[foldName(n.Name)] = key
	for _, host := range b.infra(n) {
		b.edge(key, host, EdgeNetwork)
	}
}

// missing adds the stacks no active note links, pale, on their host.
func (b *graphBuild) missing() {
	for i, s := range b.data.Stacks {
		if b.linked[i] {
			continue
		}
		host := b.host(s.Host, s.Host)
		node := GraphNode{Key: keyStack + s.Host + "/" + s.Name, Parent: host, Kind: GraphMissing, Label: s.Name,
			Props: []GraphProp{{Key: PropDocs, Text: ValueDocsMissing}}}
		if ports := stackPorts(s); len(ports) > 0 {
			node.Props = append(node.Props, GraphProp{Key: PropPorts, Value: strings.Join(ports, ", ")})
		}
		b.add(node)
	}
}

// depends connects a note with what its "abhängig von" names.
func (b *graphBuild) depends(n sources.DocNote) {
	from := b.byName[foldName(n.Name)]
	if from == "" {
		return
	}
	for _, name := range n.DependsOn {
		to := b.byName[foldName(name)]
		if to == "" || to == from {
			continue
		}
		b.edge(from, to, EdgeDepends)
	}
}

func (b *graphBuild) add(n GraphNode) {
	b.nodes[n.Key] = len(b.out.Nodes)
	b.out.Nodes = append(b.out.Nodes, n)
}

func (b *graphBuild) edge(from, to string, kind EdgeKind) {
	key := string(kind) + ":" + from + ">" + to
	if b.edges[key] {
		return
	}
	b.edges[key] = true
	b.out.Edges = append(b.out.Edges, GraphEdge{Key: key, From: from, To: to, Kind: kind})
}

// stacksOf finds the stacks a note's Compose field links and marks them
// documented.
func (b *graphBuild) stacksOf(n sources.DocNote) []sources.Stack {
	var out []sources.Stack
	for _, link := range n.Compose {
		repo, file, ok := ComposeRef(link)
		if !ok {
			continue
		}
		if i := stackOf(b.data.Stacks, repo, file); i >= 0 {
			b.linked[i] = true
			out = append(out, b.data.Stacks[i])
		}
	}
	return out
}

// documented reports whether a service note already draws stack s.
func (b *graphBuild) documented(s sources.Stack) bool {
	for _, n := range b.data.Notes {
		if n.Deprecated || !inDir(n.Path, dirServices) {
			continue
		}
		for _, link := range n.Compose {
			repo, file, ok := ComposeRef(link)
			if ok && stackOf([]sources.Stack{s}, repo, file) == 0 {
				return true
			}
		}
	}
	return false
}

func (b *graphBuild) hostnameOf(key string) string {
	i, ok := b.nodes[key]
	if !ok {
		return ""
	}
	return b.out.Nodes[i].Hostname
}

// noteProps are the review date and the obsidian:// link of a note.
func (b *graphBuild) noteProps(n sources.DocNote) []GraphProp {
	var out []GraphProp
	if n.Checked != "" {
		out = append(out, GraphProp{Key: PropChecked, Value: n.Checked, Stale: b.stale(n)})
	}
	if link := ObsidianLink(b.vault, n.Path); link != "" {
		out = append(out, GraphProp{Key: PropNote, Value: link})
	}
	return out
}

// stale reports whether a note's review date is older than StaleCheck.
func (b *graphBuild) stale(n sources.DocNote) bool {
	day, err := time.Parse(time.DateOnly, n.Checked)
	return err == nil && b.now.Sub(day) > StaleCheck
}

// ObsidianLink opens a note in the vault: "obsidian://open?vault=IT&
// file=IT%2FDienste%2FImmich"; "" without a vault.
func ObsidianLink(vault, notePath string) string {
	if vault == "" {
		return ""
	}
	file := strings.TrimSuffix(notePath, path.Ext(notePath))
	return "obsidian://open?vault=" + url.PathEscape(vault) + "&file=" + url.PathEscape(file)
}

// VaultOf is the vault's name from a note's link in Gitea
// (".../alex/ObsidianPrivat/src/branch/main/IT/…" → "ObsidianPrivat").
func VaultOf(noteURL string) string {
	u, err := url.Parse(noteURL)
	if err != nil {
		return ""
	}
	segs := strings.Split(strings.Trim(u.Path, "/"), "/")
	for i := 1; i < len(segs); i++ {
		if refKinds[segs[i]] {
			return segs[i-1]
		}
	}
	return ""
}

func ssoOf(n sources.DocNote) PropValue {
	switch {
	case n.SSOActive:
		return ValueSSOActive
	case n.SSOPossible:
		return ValueSSOPossible
	}
	return "" // not documented: no claim either way
}

// notePorts are the note's external ports, else the published ports of
// its stacks.
func notePorts(n sources.DocNote, stacks []sources.Stack) []string {
	if len(n.ExternalPorts) > 0 {
		return n.ExternalPorts
	}
	var out []string
	for _, s := range stacks {
		out = append(out, stackPorts(s)...)
	}
	return out
}

// stackPorts are the host ports a stack publishes, in service order:
// "127.0.0.1:8080:80" → "8080", "5433:5432/tcp" → "5433"; a container
// port alone ("9000") is published nowhere fixed.
func stackPorts(s sources.Stack) []string {
	var out []string
	for _, svc := range s.Services {
		for _, p := range svc.Ports {
			p, _, _ = strings.Cut(p, "/")
			parts := strings.Split(p, ":")
			if len(parts) < 2 || parts[len(parts)-2] == "" || slices.Contains(out, parts[len(parts)-2]) {
				continue
			}
			out = append(out, parts[len(parts)-2])
		}
	}
	return out
}

// checkOf is how Homelable probes a service: its URL, else its first
// port on the host ("regis:8080").
func checkOf(web string, ports []string, hostname string) (CheckMethod, string) {
	if web != "" {
		if !strings.Contains(web, "://") {
			web = "https://" + web
		}
		if strings.HasPrefix(web, "http://") {
			return CheckHTTP, web
		}
		return CheckHTTPS, web
	}
	if len(ports) > 0 && hostname != "" {
		return CheckTCP, hostname + ":" + ports[0]
	}
	return CheckNone, ""
}

// inDir reports whether a vault path lies below a folder of that name.
func inDir(notePath, dir string) bool {
	return slices.Contains(strings.Split(path.Dir(notePath), "/"), dir)
}

func isNetNote(n sources.DocNote) bool {
	if len(n.Compose) == 0 {
		return false
	}
	for _, dir := range netDirs {
		if inDir(n.Path, dir) {
			return true
		}
	}
	return false
}

// foldName makes host and note names comparable: "Plötze" and the repo
// docker-compose-ploetze both become "ploetze".
func foldName(name string) string {
	return strings.NewReplacer("ä", "ae", "ö", "oe", "ü", "ue", "ß", "ss", " ", "-").Replace(strings.ToLower(strings.TrimSpace(name)))
}
