package metrics_test

import (
	"testing"

	"andon/internal/metrics"
	"andon/internal/sources"
)

const driftRepo = "https://git.example/alex/docker-compose-regis/src/branch/main/"

// driftData links one note per case to a stack of its own.
func driftData(notes ...sources.DocNote) *sources.GiteaDataset {
	stack := func(name string, svcs ...sources.ComposeService) sources.Stack {
		return sources.Stack{Host: "regis", Name: name, Repo: "alex/docker-compose-regis", Path: name + "/compose.yaml", Services: svcs}
	}
	return &sources.GiteaDataset{StacksRead: true, NotesRead: true, Stacks: []sources.Stack{
		stack("immich",
			sources.ComposeService{Name: "server", Image: "ghcr.io/immich-app/immich-server:v2.1", Ports: []string{"127.0.0.1:2283:2283/tcp"},
				Labels: map[string]string{"traefik.http.routers.im.rule": "Host(`fotos.example`) || Host(`photos.example`)", "traefik.http.routers.im.tls": "true"}},
			sources.ComposeService{Name: "db", Image: "docker.io/library/postgres:16@sha256:abc"}),
		stack("ninja", sources.ComposeService{Name: "app", Image: "invoiceninja/invoiceninja:${TAG}", Ports: []string{"8012:80", "53:53/udp", "9000"},
			Labels: map[string]string{"caddy": "rechnung.example, billing.example"}}),
		stack("plain", sources.ComposeService{Name: "app", Image: "nginx", Ports: []string{"${PORT}:80"}}),
	}, Notes: notes}
}

func note(name, stack string) sources.DocNote {
	return sources.DocNote{Path: "IT/Dienste/" + name + ".md", Name: name, Compose: []string{driftRepo + stack + "/compose.yaml"}}
}

// fieldsOf runs the check on one note and keys its fields "Field|From".
func fieldsOf(t *testing.T, n sources.DocNote, komodo *sources.KomodoDataset) map[string]metrics.DriftField {
	t.Helper()
	drifts, ok := metrics.CheckDrift(driftData(n), komodo)
	if !ok {
		t.Fatal("not checked")
	}
	out := map[string]metrics.DriftField{}
	for _, d := range drifts {
		for _, f := range d.Fields {
			out[f.Field+"|"+f.From] = f
		}
	}
	return out
}

// A note that agrees in every normalized form has no drift: URL with
// scheme and path, the port as published, the image without tag, the
// tag against a digest, docker.io/library/ dropped.
func TestDriftNormalizes(t *testing.T) {
	n := note("Immich", "immich")
	n.Web, n.ExternalPorts = "https://Photos.example/albums", []string{"2283"}
	n.Images = []string{"ghcr.io/immich-app/immich-server", "postgres:16", "postgres@sha256:abc"}
	if got := fieldsOf(t, n, nil); len(got) != 0 {
		t.Fatalf("drift: %+v", got)
	}

	// Caddy hosts, published ports only, udp kept; variables in the image
	// stop the image comparison.
	n = note("Ninja", "ninja")
	n.Web, n.ExternalPorts, n.Images = "billing.example", []string{"53/udp", "8012:80"}, []string{"invoiceninja/invoiceninja:5.11"}
	if got := fieldsOf(t, n, nil); len(got) != 0 {
		t.Fatalf("drift: %+v", got)
	}

	// Ports with variables and no proxy labels: nothing to compare.
	n = note("Plain", "plain")
	n.Web, n.ExternalPorts = "web.example", []string{"8080"}
	if got := fieldsOf(t, n, nil); len(got) != 0 {
		t.Fatalf("drift: %+v", got)
	}
}

// Each differing field old → new, one drift per note.
func TestDriftFields(t *testing.T) {
	n := note("Immich", "immich")
	n.Web, n.ExternalPorts = "bilder.example", []string{"2283", "8080"}
	n.Images = []string{"ghcr.io/immich-app/immich-server:v2.0", "redis:7", "postgres@sha256:def"}
	got := fieldsOf(t, n, nil)

	if f := got["URL|compose"]; f.Old != "bilder.example" || f.New != "fotos.example, photos.example" {
		t.Fatalf("url: %+v", got)
	}
	if f := got["externe Ports|compose"]; f.Old != "2283, 8080" || f.New != "2283" {
		t.Fatalf("ports: %+v", got)
	}
	drifts, _ := metrics.CheckDrift(driftData(n), nil)
	if len(drifts) != 1 || len(drifts[0].Fields) != 5 || metrics.DriftID(drifts[0]) != "docs.drift:IT/Dienste/Immich.md" {
		t.Fatalf("drifts: %+v", drifts)
	}
	images := map[string]string{}
	for _, f := range drifts[0].Fields {
		if f.Field == metrics.FieldImage {
			images[f.Old] = f.New
		}
	}
	if images["ghcr.io/immich-app/immich-server:v2.0"] != "ghcr.io/immich-app/immich-server:v2.1" ||
		images["redis:7"] != "ghcr.io/immich-app/immich-server:v2.1, docker.io/library/postgres:16@sha256:abc" ||
		images["postgres@sha256:def"] != "docker.io/library/postgres:16@sha256:abc" {
		t.Fatalf("images: %v", images)
	}
}

// Komodo adds what runs when it is news: an older image than the note
// and the compose file name, or one behind a variable in compose.
func TestDriftKomodo(t *testing.T) {
	komodo := &sources.KomodoDataset{Stacks: []sources.KStack{
		{Name: "immich", Repo: "alex/docker-compose-regis", Images: map[string]string{"server": "ghcr.io/immich-app/immich-server:v2.0"}},
		{Name: "ninja", Server: "Regis", Images: map[string]string{"app": "invoiceninja/invoiceninja:5.12"}},
	}}

	n := note("Immich", "immich")
	n.Images = []string{"ghcr.io/immich-app/immich-server:v2.1"}
	got := fieldsOf(t, n, komodo)
	if f := got["Image|komodo"]; len(got) != 1 || f.New != "ghcr.io/immich-app/immich-server:v2.0" {
		t.Fatalf("running: %+v", got)
	}

	// Note old, compose new, Komodo runs the note's: both differ.
	n.Images = []string{"ghcr.io/immich-app/immich-server:v1.9"}
	if got := fieldsOf(t, n, komodo); len(got) != 2 {
		t.Fatalf("both: %+v", got)
	}

	// Komodo runs what compose names: only the compose drift.
	komodo.Stacks[0].Images["server"] = "ghcr.io/immich-app/immich-server:v2.1"
	if got := fieldsOf(t, n, komodo); len(got) != 1 || got["Image|compose"].New != "ghcr.io/immich-app/immich-server:v2.1" {
		t.Fatalf("compose only: %+v", got)
	}

	n = note("Ninja", "ninja")
	n.Images = []string{"invoiceninja/invoiceninja:5.11"}
	if got := fieldsOf(t, n, komodo); len(got) != 1 || got["Image|komodo"].New != "invoiceninja/invoiceninja:5.12" {
		t.Fatalf("variable: %+v", got)
	}
}

// Deprecated notes, unlinked notes and unread data have no drift.
func TestDriftSkips(t *testing.T) {
	n := note("Immich", "immich")
	n.Web, n.Deprecated = "bilder.example", true
	gone := note("Gone", "gone")
	gone.Web = "x.example"
	if drifts, ok := metrics.CheckDrift(driftData(n, gone), nil); !ok || len(drifts) != 0 {
		t.Fatalf("drifts: %+v", drifts)
	}
	data := driftData()
	data.NotesRead = false
	if _, ok := metrics.CheckDrift(data, nil); ok {
		t.Fatal("unread checked")
	}
}
