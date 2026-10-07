package metrics

// A note's frontmatter against its compose files and what Komodo runs
// (docs.drift). Each field is compared only when the note names it and
// the other side knows it; a missing key is Hansei's check, not drift.
//
//	note Invoice Ninja   URL: rechnung.example     externe Ports: 8002
//	compose invoiceninja labels caddy: rechnungen.example   ports: 8012:80
//	                     └ URL: rechnung.example → rechnungen.example
//	                       externe Ports: 8002 → 8012
//
// Matching rules:
//   - URL: the host name only (no scheme, port or path, lower case),
//     against the hosts in the services' proxy labels: Traefik
//     "traefik.http.routers.<r>.rule: Host(`a`) || Host(`b`)" and
//     caddy-docker-proxy "caddy: a, b" / "caddy_0: …". No label, no
//     comparison.
//   - externe Ports: the published (host) ports of all linked stacks as a
//     set. "127.0.0.1:8080:80/tcp" → 8080, "8000-8002:8000-8002" →
//     8000-8002, "/tcp" dropped, "/udp" kept; a bare container port
//     ("80") is not published. In the note "8080" is the published port
//     itself. Ports with variables ("${PORT}:80") stop the comparison.
//     interne Ports are not compared: compose names no unpublished ports.
//   - Image: by repository (docker.io/ and library/ dropped), then tag; no
//     tag means latest. A note image without tag matches any tag; tag
//     against digest cannot be compared and matches; two digests must be
//     equal. Images with variables are skipped on the compose side; then
//     only Komodo's resolved image counts.
//   - Komodo: what a linked stack runs, image per service; reported only
//     when it differs from the note and is not already the compose
//     file's new value.
//
// Environment values are never read (sources.parseCompose), so they
// cannot drift.

import (
	"regexp"
	"slices"
	"strings"

	"andon/internal/sources"
)

// Frontmatter keys a drift names, as the vault spells them.
const (
	FieldURL   = "URL"
	FieldPorts = "externe Ports"
	FieldImage = "Image"
)

// Where a drift's new value comes from.
const (
	FromCompose = "compose"
	FromKomodo  = "komodo"
)

// noValue stands for a side without a value, e.g. no published port.
const noValue = "–"

// DriftField is one frontmatter field that differs: Old from the note,
// New from the compose file (From compose) or what runs (From komodo).
type DriftField struct {
	Field, Old, New, From string
}

// Drift is a note whose frontmatter differs from its stacks.
type Drift struct {
	Note   sources.DocNote
	Fields []DriftField
}

// CheckDrift compares each active note with the stacks it links; ok is
// false while stacks or notes are unknown. komodo may be nil.
func CheckDrift(data *sources.GiteaDataset, komodo *sources.KomodoDataset) ([]Drift, bool) {
	if data == nil || !data.StacksRead || !data.NotesRead {
		return nil, false
	}
	running := runningIndex(komodo)

	var out []Drift
	for _, note := range data.Notes {
		if note.Deprecated {
			continue
		}
		stacks := linkedStacks(data.Stacks, note)
		if len(stacks) == 0 {
			continue
		}

		var fields []DriftField
		fields = append(fields, urlDrift(note, stacks)...)
		fields = append(fields, portDrift(note, stacks)...)
		for _, image := range note.Images {
			fields = append(fields, imageDrift(image, stacks, running)...)
		}
		if len(fields) > 0 {
			out = append(out, Drift{Note: note, Fields: fields})
		}
	}
	return out, true
}

// Text lists the fields as the vault names them, what runs marked:
// "externe Ports: 8002 → 8012; Image (Komodo): a:1 → a:2".
func (d Drift) Text() string {
	parts := make([]string, 0, len(d.Fields))
	for _, f := range d.Fields {
		name := f.Field
		if f.From == FromKomodo {
			name += " (Komodo)"
		}
		parts = append(parts, name+": "+f.Old+" → "+f.New)
	}
	return strings.Join(parts, "; ")
}

// DriftID names a note that differs from its stacks.
func DriftID(d Drift) string { return "docs.drift:" + d.Note.Path }

// linkedStacks are the valid stacks a note's Compose field links.
func linkedStacks(stacks []sources.Stack, note sources.DocNote) []sources.Stack {
	var out []sources.Stack
	for _, link := range note.Compose {
		repo, file, ok := ComposeRef(link)
		if !ok {
			continue
		}
		if i := stackOf(stacks, repo, file); i >= 0 && !stacks[i].Invalid {
			out = append(out, stacks[i])
		}
	}
	return out
}

// runningIndex is Komodo's stacks by "host/name", folded like CheckDeploys.
func runningIndex(komodo *sources.KomodoDataset) map[string]sources.KStack {
	out := map[string]sources.KStack{}
	if komodo == nil {
		return out
	}
	for _, k := range komodo.Stacks {
		if host := hostKey(komodoHost(k)); host != "" {
			out[host+"/"+strings.ToLower(k.Name)] = k
		}
	}
	return out
}

// ── URL ──

var (
	traefikRule = regexp.MustCompile(`^traefik\.http\.routers\.[^.]+\.rule$`)
	caddyKey    = regexp.MustCompile(`^caddy(_\d+)?$`)
	hostCall    = regexp.MustCompile(`Host\(([^)]*)\)`)
	backticked  = regexp.MustCompile("`([^`]+)`")
)

func urlDrift(note sources.DocNote, stacks []sources.Stack) []DriftField {
	if note.Web == "" {
		return nil
	}
	hosts := proxyHosts(stacks)
	if len(hosts) == 0 || slices.Contains(hosts, addressHost(note.Web)) {
		return nil
	}
	return []DriftField{{Field: FieldURL, Old: note.Web, New: strings.Join(hosts, ", "), From: FromCompose}}
}

// proxyHosts are the host names the stacks' proxy labels route, sorted.
func proxyHosts(stacks []sources.Stack) []string {
	var out []string
	add := func(v string) {
		if h := addressHost(v); h != "" && !strings.ContainsAny(h, "${") && !slices.Contains(out, h) {
			out = append(out, h)
		}
	}
	for _, s := range stacks {
		for _, svc := range s.Services {
			for k, v := range svc.Labels {
				switch {
				case traefikRule.MatchString(k):
					for _, call := range hostCall.FindAllStringSubmatch(v, -1) {
						for _, name := range backticked.FindAllStringSubmatch(call[1], -1) {
							add(name[1])
						}
					}
				case caddyKey.MatchString(k):
					for _, name := range strings.FieldsFunc(v, func(r rune) bool { return r == ',' || r == ' ' }) {
						add(name)
					}
				}
			}
		}
	}
	slices.Sort(out)
	return out
}

// addressHost is an address's host name, with or without scheme:
// "Fotos.example:443/x" → "fotos.example".
func addressHost(v string) string {
	v = strings.TrimSpace(v)
	if !strings.Contains(v, "://") {
		v = "//" + v
	}
	return strings.TrimSuffix(urlHost(v), ".")
}

// ── Ports ──

func portDrift(note sources.DocNote, stacks []sources.Stack) []DriftField {
	if len(note.ExternalPorts) == 0 {
		return nil
	}
	var compose []string
	for _, s := range stacks {
		for _, svc := range s.Services {
			for _, p := range svc.Ports {
				if strings.Contains(p, "$") {
					return nil
				}
				compose = addPort(compose, publishedPort(p))
			}
		}
	}
	var noted []string
	for _, p := range note.ExternalPorts {
		noted = addPort(noted, notedPort(p))
	}
	if slices.Equal(noted, compose) {
		return nil
	}
	return []DriftField{{Field: FieldPorts, Old: portList(noted), New: portList(compose), From: FromCompose}}
}

// publishedPort is a mapping's host port: "127.0.0.1:8080:80/tcp" →
// "8080", "53:53/udp" → "53/udp"; "" when nothing is published ("80").
func publishedPort(p string) string {
	p, proto, _ := strings.Cut(strings.TrimSpace(p), "/")
	parts := strings.Split(p, ":")
	if len(parts) < 2 {
		return ""
	}
	return withProto(parts[len(parts)-2], proto)
}

// notedPort is a note's port: a bare "8080" is the published port
// itself, "8080:80" a mapping.
func notedPort(p string) string {
	bare, proto, _ := strings.Cut(strings.TrimSpace(p), "/")
	if !strings.Contains(bare, ":") {
		return withProto(bare, proto)
	}
	return publishedPort(p)
}

// withProto adds a protocol other than tcp: "53", "udp" → "53/udp".
func withProto(port, proto string) string {
	if port == "" || proto == "" || strings.EqualFold(proto, "tcp") {
		return port
	}
	return port + "/" + strings.ToLower(proto)
}

// addPort adds a port to a sorted set.
func addPort(set []string, p string) []string {
	if p == "" || slices.Contains(set, p) {
		return set
	}
	set = append(set, p)
	slices.SortFunc(set, comparePorts)
	return set
}

// comparePorts orders "443" before "8080": shorter numbers first.
func comparePorts(a, b string) int {
	if len(a) != len(b) {
		return len(a) - len(b)
	}
	return strings.Compare(a, b)
}

func portList(set []string) string {
	if len(set) == 0 {
		return noValue
	}
	return strings.Join(set, ", ")
}

// ── Image ──

// imageRef is an image split for comparison.
type imageRef struct {
	repo, tag, digest string
}

// parseImage splits "docker.io/library/nginx:1.27@sha256:ab" into
// nginx, 1.27, sha256:ab.
func parseImage(s string) imageRef {
	s = strings.ToLower(strings.TrimSpace(s))
	s, digest, _ := strings.Cut(s, "@")
	ref := imageRef{repo: s, digest: digest}
	if i := strings.LastIndex(s, ":"); i > strings.LastIndex(s, "/") {
		ref.repo, ref.tag = s[:i], s[i+1:]
	}
	for _, prefix := range []string{"docker.io/", "index.docker.io/"} {
		ref.repo = strings.TrimPrefix(ref.repo, prefix)
	}
	ref.repo = strings.TrimPrefix(ref.repo, "library/")
	return ref
}

// matches reports whether a noted image fits one in use (see the rules
// at the top): same repository assumed.
func (noted imageRef) matches(used imageRef) bool {
	switch {
	case noted.digest != "" && used.digest != "":
		return noted.digest == used.digest
	case noted.tag == "":
		return true
	case used.tag == "" && used.digest != "":
		return true
	case used.tag == "":
		return noted.tag == "latest"
	}
	return noted.tag == used.tag
}

func imageDrift(image string, stacks []sources.Stack, running map[string]sources.KStack) []DriftField {
	noted := parseImage(image)

	// The compose file: a service of the same repository, else all images.
	var same, all []string
	unresolved := false
	for _, s := range stacks {
		for _, svc := range s.Services {
			switch {
			case strings.Contains(svc.Image, "$"):
				unresolved = true
			case svc.Image != "":
				all = append(all, svc.Image)
				if parseImage(svc.Image).repo == noted.repo {
					same = append(same, svc.Image)
				}
			}
		}
	}
	var out []DriftField
	composeNew := ""
	switch {
	case len(same) > 0 && !anyMatch(noted, same):
		composeNew = same[0]
	case len(same) == 0 && !unresolved:
		composeNew = noValue
		if len(all) > 0 {
			composeNew = strings.Join(all, ", ")
		}
	}
	if composeNew != "" {
		out = append(out, DriftField{Field: FieldImage, Old: image, New: composeNew, From: FromCompose})
	}

	// What Komodo runs of the linked stacks, when it says something new.
	var runs []string
	for _, s := range stacks {
		k := running[hostKey(s.Host)+"/"+strings.ToLower(s.Name)]
		for _, img := range k.Images {
			if parseImage(img).repo == noted.repo {
				runs = append(runs, img)
			}
		}
	}
	slices.Sort(runs)
	if len(runs) > 0 && !anyMatch(noted, runs) && runs[0] != composeNew && !anyMatch(parseImage(runs[0]), same) {
		out = append(out, DriftField{Field: FieldImage, Old: image, New: runs[0], From: FromKomodo})
	}
	return out
}

// anyMatch reports whether noted fits one of images.
func anyMatch(noted imageRef, images []string) bool {
	for _, img := range images {
		if noted.matches(parseImage(img)) {
			return true
		}
	}
	return false
}
