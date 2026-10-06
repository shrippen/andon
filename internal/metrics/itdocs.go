package metrics

// IT docs against compose stacks (Phase 15). A note documents a stack
// when its Compose field links the stack's compose file or directory:
//
//	note Immich   Compose: …/docker-compose-regis/src/branch/main/immich/compose.yaml
//	                                         └ repo ┘                 └ file ─────────┘
//	stack regis/immich   Repo alex/docker-compose-regis, Path immich/compose.yaml
//
// Device and network notes list their infrastructure stacks the same way.

import (
	"net/url"
	"strings"

	"andon/internal/sources"
)

// refKinds are the Gitea link forms before the ref: /src/branch/main/…
var refKinds = map[string]bool{"src": true, "raw": true}

// DocLink is a Compose link of a note and the stack it names (nil: none).
type DocLink struct {
	Note  sources.DocNote
	Link  string
	Stack *sources.Stack
}

// DocsCheck is the comparison of notes and stacks.
type DocsCheck struct {
	Missing        []sources.Stack // linked by no active note
	Orphans        []DocLink       // active note, link into a compose repo, no such stack
	DeprecatedLive []DocLink       // deprecated note, its stack still exists
}

// CheckDocs compares; ok is false while stacks or notes are unknown.
func CheckDocs(data *sources.GiteaDataset) (DocsCheck, bool) {
	if data == nil || !data.StacksRead || !data.NotesRead {
		return DocsCheck{}, false
	}

	var out DocsCheck
	documented := map[int]bool{}
	for _, note := range data.Notes {
		for _, link := range note.Compose {
			repo, file, ok := ComposeRef(link)
			if !ok {
				continue
			}
			i := stackOf(data.Stacks, repo, file)
			switch {
			case i >= 0 && note.Deprecated:
				out.DeprecatedLive = append(out.DeprecatedLive, DocLink{Note: note, Link: link, Stack: &data.Stacks[i]})
			case i >= 0:
				documented[i] = true
			case !note.Deprecated && strings.HasPrefix(repoName(repo), sources.ComposePrefix):
				out.Orphans = append(out.Orphans, DocLink{Note: note, Link: link})
			}
		}
	}

	for i, s := range data.Stacks {
		if !documented[i] {
			out.Missing = append(out.Missing, s)
		}
	}
	return out, true
}

// MissingRepo is the repo of host's undocumented stacks, "" if none.
func (c DocsCheck) MissingRepo(host string) string {
	for _, s := range c.Missing {
		if s.Host == host {
			return s.Repo
		}
	}
	return ""
}

// ComposeRef reads a Gitea file link: "https://git.example/alex/
// docker-compose-regis/src/branch/main/immich/compose.yaml" →
// "alex/docker-compose-regis", "immich/compose.yaml".
func ComposeRef(link string) (repo, file string, ok bool) {
	u, err := url.Parse(strings.TrimSpace(link))
	if err != nil {
		return "", "", false
	}
	segs := strings.Split(strings.Trim(u.Path, "/"), "/")
	for i := 2; i+2 < len(segs); i++ {
		if !refKinds[segs[i]] {
			continue
		}
		rest, err := url.PathUnescape(strings.Join(segs[i+3:], "/"))
		if err != nil || rest == "" {
			return "", "", false
		}
		return segs[i-2] + "/" + segs[i-1], rest, true
	}
	return "", "", false
}

// stackOf finds the stack whose file or directory file names; -1 if none.
func stackOf(stacks []sources.Stack, repo, file string) int {
	file = strings.TrimSuffix(file, "/")
	for i, s := range stacks {
		if strings.EqualFold(s.Repo, repo) && (strings.EqualFold(s.Path, file) || strings.EqualFold(s.Name, file)) {
			return i
		}
	}
	return -1
}

// repoName is a repo's name without its owner.
func repoName(repo string) string {
	_, name, _ := strings.Cut(repo, "/")
	return name
}

// HostDocs is a host's documented share of its stacks.
type HostDocs struct {
	Host              string
	Documented, Total int
}

// ByHost counts documented stacks per host, hosts in stack order.
func (c DocsCheck) ByHost(stacks []sources.Stack) []HostDocs {
	missing := map[string]int{}
	for _, s := range c.Missing {
		missing[s.Host]++
	}

	var out []HostDocs
	at := map[string]int{}
	for _, s := range stacks {
		i, ok := at[s.Host]
		if !ok {
			i = len(out)
			at[s.Host] = i
			out = append(out, HostDocs{Host: s.Host, Documented: -missing[s.Host]})
		}
		out[i].Total++
		out[i].Documented++
	}
	return out
}
