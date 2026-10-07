package metrics_test

import (
	"testing"
	"time"

	"andon/internal/metrics"
	"andon/internal/sources"
)

const repoURL = "https://git.example/alex/docker-compose-"

func link(host, stack string) string {
	return repoURL + host + "/src/branch/main/" + stack + "/compose.yaml"
}

func stack(host, name string, ports ...string) sources.Stack {
	return sources.Stack{Host: host, Name: name, Repo: "alex/docker-compose-" + host, Path: name + "/compose.yaml",
		Services: []sources.ComposeService{{Name: name, Ports: ports}}}
}

// graphData is a small homelab: Regis in the cellar with Immich and a
// Tailscale stack, Plötze without a device note, one stack undocumented.
func graphData() *sources.GiteaDataset {
	return &sources.GiteaDataset{StacksRead: true, NotesRead: true,
		Stacks: []sources.Stack{
			stack("regis", "immich", "2283:2283"),
			stack("regis", "tailscale-regis"),
			stack("regis", "kometa", "127.0.0.1:9000:9000/tcp"),
			stack("ploetze", "tailscale-ploetze"),
			stack("ploetze", "tdarr"),
		},
		Notes: []sources.DocNote{
			{Path: "IT/Geräte/Regis.md", Name: "Regis", Places: []string{"Keller"}},
			{Path: "IT/Orte/Keller.md", Name: "Keller"},
			{Path: "IT/Dienste/Regis/Immich.md", Name: "Immich", Devices: []string{"Regis"}, Compose: []string{link("regis", "immich")},
				Web: "fotos.example.org", Backup: []string{"Borg"}, SSOActive: true, Checked: "2025-01-02", DependsOn: []string{"Postgres", "Regis"}},
			{Path: "IT/Dienste/Regis/Postgres.md", Name: "Postgres", Devices: []string{"Regis"}, ExternalPorts: []string{"5432"}, Checked: "2026-09-30"},
			{Path: "IT/Netzwerk/Tailscale.md", Name: "Tailscale", Compose: []string{link("regis", "tailscale-regis"), link("ploetze", "tailscale-ploetze")}},
			{Path: "IT/Dienste/deprecated/Tdarr.md", Name: "Tdarr", Deprecated: true, Compose: []string{link("ploetze", "tdarr")}},
		},
	}
}

var graphNow = time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)

func nodeOf(t *testing.T, g metrics.DocGraph, key string) metrics.GraphNode {
	t.Helper()
	for _, n := range g.Nodes {
		if n.Key == key {
			return n
		}
	}
	t.Fatalf("no node %q in %+v", key, g.Nodes)
	return metrics.GraphNode{}
}

func propOf(n metrics.GraphNode, key metrics.PropKey) (metrics.GraphProp, bool) {
	for _, p := range n.Props {
		if p.Key == key {
			return p, true
		}
	}
	return metrics.GraphProp{}, false
}

func hasEdge(g metrics.DocGraph, from, to string, kind metrics.EdgeKind) bool {
	for _, e := range g.Edges {
		if e.From == from && e.To == to && e.Kind == kind {
			return true
		}
	}
	return false
}

func TestDocGraphPlacesHostsAndServices(t *testing.T) {
	g, ok := metrics.BuildDocGraph(graphData(), "Vault", graphNow)
	if !ok {
		t.Fatal("graph not built")
	}

	regis := nodeOf(t, g, "host:regis")
	if regis.Parent != "zone:keller" || regis.Kind != metrics.GraphHost || regis.Label != "Regis" {
		t.Errorf("regis = %+v", regis)
	}
	if p, _ := propOf(nodeOf(t, g, "zone:keller"), metrics.PropNote); p.Value != "obsidian://open?vault=Vault&file=IT%2FOrte%2FKeller" {
		t.Errorf("zone link = %q", p.Value)
	}
	if ploetze := nodeOf(t, g, "host:ploetze"); ploetze.Parent != "" || ploetze.Hostname != "ploetze" {
		t.Errorf("ploetze = %+v", ploetze)
	}

	immich := nodeOf(t, g, "note:IT/Dienste/Regis/Immich.md")
	if immich.Parent != "host:regis" || immich.Kind != metrics.GraphService {
		t.Errorf("immich = %+v", immich)
	}
	if immich.Check != metrics.CheckHTTPS || immich.CheckTarget != "https://fotos.example.org" {
		t.Errorf("immich check = %s %s", immich.Check, immich.CheckTarget)
	}
	if p, _ := propOf(immich, metrics.PropPorts); p.Value != "2283" {
		t.Errorf("immich ports = %q", p.Value)
	}
	if p, _ := propOf(immich, metrics.PropSSO); p.Text != metrics.ValueSSOActive {
		t.Errorf("immich sso = %q", p.Text)
	}
	if p, _ := propOf(immich, metrics.PropChecked); !p.Stale || !immich.Stale {
		t.Errorf("immich review of 2025-01-02 not stale: %+v", p)
	}

	postgres := nodeOf(t, g, "note:IT/Dienste/Regis/Postgres.md")
	if postgres.Check != metrics.CheckTCP || postgres.CheckTarget != "regis:5432" || postgres.Stale {
		t.Errorf("postgres = %+v", postgres)
	}
	if _, ok := propOf(postgres, metrics.PropSSO); ok {
		t.Error("SSO claimed without an SSO field")
	}
	if !hasEdge(g, immich.Key, postgres.Key, metrics.EdgeDepends) || !hasEdge(g, immich.Key, "host:regis", metrics.EdgeDepends) {
		t.Errorf("depends edges missing: %+v", g.Edges)
	}
}

func TestDocGraphNetworksAndGaps(t *testing.T) {
	g, _ := metrics.BuildDocGraph(graphData(), "", graphNow)

	if n := nodeOf(t, g, "stack:regis/tailscale-regis"); n.Kind != metrics.GraphStack || n.Parent != "host:regis" {
		t.Errorf("infra stack = %+v", n)
	}
	net := nodeOf(t, g, "net:IT/Netzwerk/Tailscale.md")
	if !hasEdge(g, net.Key, "host:regis", metrics.EdgeNetwork) || !hasEdge(g, net.Key, "host:ploetze", metrics.EdgeNetwork) {
		t.Errorf("network edges = %+v", g.Edges)
	}
	if _, ok := propOf(net, metrics.PropNote); ok {
		t.Error("note link without a vault")
	}

	kometa := nodeOf(t, g, "stack:regis/kometa")
	if kometa.Kind != metrics.GraphMissing || kometa.Parent != "host:regis" || kometa.Check != metrics.CheckNone {
		t.Errorf("kometa = %+v", kometa)
	}
	if p, _ := propOf(kometa, metrics.PropPorts); p.Value != "9000" {
		t.Errorf("kometa ports = %q", p.Value)
	}
	// A deprecated note documents nothing: its stack is a gap, the note no node.
	if n := nodeOf(t, g, "stack:ploetze/tdarr"); n.Kind != metrics.GraphMissing {
		t.Errorf("tdarr = %+v", n)
	}
	for _, n := range g.Nodes {
		if n.Key == "note:IT/Dienste/deprecated/Tdarr.md" || n.Key == "stack:regis/immich" {
			t.Errorf("unexpected node %q", n.Key)
		}
	}
}

func TestDocGraphNeedsCompleteData(t *testing.T) {
	data := graphData()
	data.NotesRead = false
	if _, ok := metrics.BuildDocGraph(data, "", graphNow); ok {
		t.Error("graph built from unread notes")
	}
}

func TestVaultOf(t *testing.T) {
	if got := metrics.VaultOf("https://git.example/alex/ObsidianPrivat/src/branch/main/IT/Dienste/Immich.md"); got != "ObsidianPrivat" {
		t.Errorf("vault = %q", got)
	}
}
