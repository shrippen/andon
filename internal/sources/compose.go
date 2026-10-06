package sources

// Compose stacks from Gitea: each repo docker-compose-<host> holds one
// directory per stack with its compose file. They are the reference the
// IT docs are checked against (Phase 15).
//
//	repos/search?q=docker-compose-  ─► docker-compose-regis, …-ploetze
//	git/trees/<branch>?recursive    ─► immich/compose.yaml (blob sha)
//	git/blobs/<sha>                 ─► read once per sha, then remembered
//
// Only names, images, ports and labels are kept; environment values
// (passwords, tokens) are never decoded.

import (
	"context"
	"encoding/base64"
	"fmt"
	"net/url"
	"path"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"

	"gopkg.in/yaml.v3"

	"andon/internal/drivers/services"
)

const (
	ComposePrefix   = "docker-compose-" // repo name: prefix + host
	composeParallel = 8
	composeMemoMax  = 4096 // remembered compose files, then forgotten
	treePage        = 1000
)

// composeNames are the file names Compose looks for, in its order.
var composeNames = []string{"compose.yaml", "compose.yml", "docker-compose.yaml", "docker-compose.yml"}

// Stack is one compose stack: a directory in a docker-compose-<host> repo.
type Stack struct {
	Host, Name string // "regis", "immich"
	Repo, Path string // "alex/docker-compose-regis", "immich/compose.yaml"
	URL        string // the compose file in Gitea
	Invalid    bool   // the file is no valid YAML
	Services   []ComposeService
}

// ComposeService is a service of a stack, without its environment.
type ComposeService struct {
	Name, Image string
	Ports       []string          // "8080:80", "5433:5432/tcp"
	Labels      map[string]string // also from the list form "k=v"
}

// composeFile is what a compose file is decoded into; fields not named
// here (environment, volumes, secrets) are skipped by the decoder.
type composeFile struct {
	Services map[string]struct {
		Image  string `yaml:"image"`
		Ports  []any  `yaml:"ports"`
		Labels any    `yaml:"labels"`
	} `yaml:"services"`
}

// parsedStack is a decoded compose file, remembered by its blob sha.
type parsedStack struct {
	invalid  bool
	services []ComposeService
}

var (
	composeMu   sync.Mutex
	composeSeen = map[string]parsedStack{}
)

// loadStacks reads the stacks of all compose repos. ok is false when a
// repo could not be read completely: the stacks are then unknown, not
// missing.
func loadStacks(ctx context.Context, api services.GiteaApi) ([]Stack, bool) {
	found, err := api.Get(ctx, "repos/search", url.Values{"q": {ComposePrefix}, "limit": {strconv.Itoa(giteaPage)}})
	if err != nil {
		return nil, false
	}

	var stacks []Stack
	for _, raw := range asList(asMap(found)["data"]) {
		repo := asMap(raw)
		name := asStr(repo["name"])
		if !strings.HasPrefix(name, ComposePrefix) || asBool(repo["archived"]) {
			continue
		}
		list, err := repoStacks(ctx, api, repo, strings.TrimPrefix(name, ComposePrefix))
		if err != nil {
			return nil, false
		}
		stacks = append(stacks, list...)
	}

	sort.Slice(stacks, func(i, j int) bool {
		if stacks[i].Host != stacks[j].Host {
			return stacks[i].Host < stacks[j].Host
		}
		return stacks[i].Name < stacks[j].Name
	})
	return stacks, true
}

// repoStacks finds the compose file of each top-level directory and
// reads the ones not yet remembered.
func repoStacks(ctx context.Context, api services.GiteaApi, repo map[string]any, host string) ([]Stack, error) {
	full, branch := asStr(repo["full_name"]), asStr(repo["default_branch"])
	tree, err := api.Get(ctx, "repos/"+full+"/git/trees/"+url.PathEscape(branch), url.Values{"recursive": {"true"}, "per_page": {strconv.Itoa(treePage)}})
	if err != nil {
		return nil, err
	}
	if asBool(asMap(tree)["truncated"]) {
		return nil, fmt.Errorf("compose: tree of %s truncated", full)
	}

	// One file per directory, the first in Compose's order:
	// immich/compose.yaml wins over immich/docker-compose.yml.
	best := map[string]int{}
	shas := map[string]string{}
	for _, raw := range asList(asMap(tree)["tree"]) {
		entry := asMap(raw)
		dir, file := path.Split(asStr(entry["path"]))
		dir = strings.TrimSuffix(dir, "/")
		rank := slices.Index(composeNames, file)
		if asStr(entry["type"]) != "blob" || dir == "" || strings.Contains(dir, "/") || rank < 0 {
			continue
		}
		if prev, ok := best[dir]; ok && prev <= rank {
			continue
		}
		best[dir], shas[dir] = rank, asStr(entry["sha"])
	}

	stacks := make([]Stack, 0, len(best))
	for dir, rank := range best {
		file := dir + "/" + composeNames[rank]
		stacks = append(stacks, Stack{Host: host, Name: dir, Repo: full, Path: file,
			URL: strings.TrimRight(asStr(repo["html_url"]), "/") + "/src/branch/" + url.PathEscape(branch) + "/" + file})
	}

	var failed error
	var mu sync.Mutex
	parallel(ctx, len(stacks), composeParallel, func(i int) {
		parsed, err := readStack(ctx, api, full, shas[stacks[i].Name])
		if err != nil {
			mu.Lock()
			failed = err
			mu.Unlock()
			return
		}
		stacks[i].Invalid, stacks[i].Services = parsed.invalid, parsed.services
	})
	if failed != nil {
		return nil, failed
	}
	return stacks, ctx.Err()
}

// readStack decodes a compose file; a blob already read comes from memory.
func readStack(ctx context.Context, api services.GiteaApi, repo, sha string) (parsedStack, error) {
	composeMu.Lock()
	seen, ok := composeSeen[sha]
	composeMu.Unlock()
	if ok {
		return seen, nil
	}

	body, err := blobBody(ctx, api, repo, sha)
	if err != nil {
		return parsedStack{}, err
	}
	parsed := parseCompose(body)

	composeMu.Lock()
	defer composeMu.Unlock()
	if len(composeSeen) >= composeMemoMax {
		clear(composeSeen)
	}
	composeSeen[sha] = parsed
	return parsed, nil
}

// blobBody reads a file of repo by its blob sha.
func blobBody(ctx context.Context, api services.GiteaApi, repo, sha string) ([]byte, error) {
	blob, err := api.Get(ctx, "repos/"+repo+"/git/blobs/"+sha, nil)
	if err != nil {
		return nil, err
	}
	return base64.StdEncoding.DecodeString(strings.ReplaceAll(asStr(asMap(blob)["content"]), "\n", ""))
}

// parseCompose keeps each service's image, ports and labels, sorted by
// service name.
func parseCompose(body []byte) parsedStack {
	var file composeFile
	if err := yaml.Unmarshal(body, &file); err != nil {
		return parsedStack{invalid: true}
	}

	out := parsedStack{}
	for name, svc := range file.Services {
		s := ComposeService{Name: name, Image: svc.Image, Labels: composeLabels(svc.Labels)}
		for _, p := range svc.Ports {
			if port := composePort(p); port != "" {
				s.Ports = append(s.Ports, port)
			}
		}
		out.services = append(out.services, s)
	}
	sort.Slice(out.services, func(i, j int) bool { return out.services[i].Name < out.services[j].Name })
	return out
}

// composePort reads a port in short ("8080:80", 9000) or long form
// ({published: 5433, target: 5432, protocol: tcp} → "5433:5432/tcp").
func composePort(v any) string {
	switch p := v.(type) {
	case string:
		return p
	case int:
		return strconv.Itoa(p)
	case map[string]any:
		out := fmt.Sprint(p["target"])
		if pub, ok := p["published"]; ok {
			out = fmt.Sprint(pub) + ":" + out
		}
		if proto, ok := p["protocol"]; ok {
			out += "/" + fmt.Sprint(proto)
		}
		return out
	}
	return ""
}

// composeLabels reads labels as a map or as a list of "k=v".
func composeLabels(v any) map[string]string {
	out := map[string]string{}
	switch l := v.(type) {
	case map[string]any:
		for k, val := range l {
			out[k] = fmt.Sprint(val)
		}
	case []any:
		for _, item := range l {
			k, val, _ := strings.Cut(fmt.Sprint(item), "=")
			out[k] = val
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}
