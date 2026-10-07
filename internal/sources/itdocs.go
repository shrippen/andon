package sources

// IT docs from a vault in Gitea (Obsidian): only the folders named in
// docs_paths, only each note's frontmatter. The whole tree is never
// listed: Gitea cuts it at a few thousand entries, and other folders
// stay out of Andon's sight.
//
//	options  docs_repo: alex/vault   docs_paths: [IT/Dienste, IT/Geräte]
//
//	contents/IT              ─► Dienste (tree sha), Geräte (tree sha)
//	git/trees/<sha>?recursive ─► Regis/Immich.md (blob sha)
//	git/blobs/<sha>           ─► frontmatter, read once per sha

import (
	"bytes"
	"context"
	"fmt"
	"net/url"
	"path"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"gopkg.in/yaml.v3"

	"andon/internal/drivers/services"
)

// Connection options naming the vault.
const (
	docsRepoOption  = "docs_repo"
	docsPathsOption = "docs_paths"
	noteExt         = ".md"
)

// Frontmatter keys of the vault (IT/Design.md).
const (
	fmTags          = "tags"
	fmDeprecated    = "deprecated"
	fmDevice        = "Gerät"
	fmPlace         = "Ort"
	fmPlaces        = "Orte"
	fmCompose       = "Compose"
	fmURL           = "URL"
	fmURLs          = "URLs" // read when URL is missing: its first entry
	fmImage         = "Image"
	fmExternalPorts = "externe Ports"
	fmInternalPorts = "interne Ports"
	fmTailscalePort = "TailscalePort"
	fmSSOPossible   = "SSO möglich"
	fmSSOActive     = "SSO aktiv"
	fmBackup        = "Backup via"
	fmDependsOn     = "abhängig von"
	fmChecked       = "letzte Prüfung"
)

// DocNote is a note's frontmatter; links ("[[IT/Geräte/Eredin|E]]")
// are reduced to the note's name ("Eredin").
type DocNote struct {
	Path, Name, URL string // "IT/Dienste/Regis/Immich.md", "Immich", link in Gitea
	Deprecated      bool
	Tags            []string
	Devices         []string // Gerät
	Places          []string // Ort / Orte
	Compose         []string // URLs of compose files in Gitea
	Web             string   // URL, else the first of URLs
	Images          []string // Image: "ghcr.io/immich-app/immich-server:v2.1"
	ExternalPorts   []string
	InternalPorts   []string
	TailscalePorts  []string
	SSOPossible     bool
	SSOActive       bool
	Backup          []string
	DependsOn       []string
	Checked         string // letzte Prüfung, 2006-01-02; "" if none
}

var (
	notesMu   sync.Mutex
	notesSeen = map[string]DocNote{} // by blob sha; Path, Name, URL set per read

	wikiLink = regexp.MustCompile(`^\[\[([^\]|#]+)(?:[#|][^\]]*)?\]\]$`)
)

// loadNotes reads the notes below docs_paths of docs_repo. ok is false
// when no vault is set or a folder could not be read completely.
func loadNotes(ctx context.Context, api services.GiteaApi, sctx Ctx) ([]DocNote, bool) {
	repo := asStr(sctx.Options[docsRepoOption])
	var dirs []string
	for _, p := range asList(sctx.Options[docsPathsOption]) {
		if dir := strings.Trim(asStr(p), "/"); dir != "" {
			dirs = append(dirs, dir)
		}
	}
	if repo == "" || len(dirs) == 0 {
		return nil, false
	}

	meta, err := api.Get(ctx, "repos/"+repo, nil)
	if err != nil {
		return nil, false
	}
	base := strings.TrimRight(asStr(asMap(meta)["html_url"]), "/") + "/src/branch/" + url.PathEscape(asStr(asMap(meta)["default_branch"])) + "/"

	var notes []DocNote
	for _, dir := range dirs {
		list, err := dirNotes(ctx, api, repo, dir, base)
		if err != nil {
			return nil, false
		}
		notes = append(notes, list...)
	}
	sort.Slice(notes, func(i, j int) bool { return notes[i].Path < notes[j].Path })
	return notes, true
}

// dirNotes reads the notes of one folder and its subfolders.
func dirNotes(ctx context.Context, api services.GiteaApi, repo, dir, base string) ([]DocNote, error) {
	parent, name := path.Split(dir)
	listing, err := api.Get(ctx, "repos/"+repo+"/contents/"+escapePath(strings.TrimSuffix(parent, "/")), nil)
	if err != nil {
		return nil, err
	}
	sha := ""
	for _, raw := range asList(listing) {
		entry := asMap(raw)
		if asStr(entry["name"]) == name && asStr(entry["type"]) == "dir" {
			sha = asStr(entry["sha"])
		}
	}
	if sha == "" {
		return nil, fmt.Errorf("docs: %s not found", dir)
	}

	tree, err := api.Get(ctx, "repos/"+repo+"/git/trees/"+sha, url.Values{"recursive": {"true"}, "per_page": {strconv.Itoa(treePage)}})
	if err != nil {
		return nil, err
	}
	if asBool(asMap(tree)["truncated"]) {
		return nil, fmt.Errorf("docs: tree of %s truncated", dir)
	}

	var notes []DocNote
	var shas []string
	for _, raw := range asList(asMap(tree)["tree"]) {
		entry := asMap(raw)
		file := asStr(entry["path"])
		if asStr(entry["type"]) != "blob" || !strings.HasSuffix(file, noteExt) {
			continue
		}
		full := dir + "/" + file
		notes = append(notes, DocNote{Path: full, Name: strings.TrimSuffix(path.Base(file), noteExt), URL: base + escapePath(full)})
		shas = append(shas, asStr(entry["sha"]))
	}

	var failed error
	var mu sync.Mutex
	parallel(ctx, len(notes), composeParallel, func(i int) {
		note, err := readNote(ctx, api, repo, shas[i])
		if err != nil {
			mu.Lock()
			failed = err
			mu.Unlock()
			return
		}
		note.Path, note.Name, note.URL = notes[i].Path, notes[i].Name, notes[i].URL
		notes[i] = note
	})
	if failed != nil {
		return nil, failed
	}
	return notes, ctx.Err()
}

// readNote decodes a note's frontmatter; a blob already read comes from
// memory.
func readNote(ctx context.Context, api services.GiteaApi, repo, sha string) (DocNote, error) {
	notesMu.Lock()
	seen, ok := notesSeen[sha]
	notesMu.Unlock()
	if ok {
		return seen, nil
	}

	body, err := blobBody(ctx, api, repo, sha)
	if err != nil {
		return DocNote{}, err
	}
	note := parseNote(body)

	notesMu.Lock()
	defer notesMu.Unlock()
	if len(notesSeen) >= composeMemoMax {
		clear(notesSeen)
	}
	notesSeen[sha] = note
	return note, nil
}

// parseNote reads the frontmatter between the leading "---" lines; the
// body is dropped. A note without valid frontmatter has none.
func parseNote(body []byte) DocNote {
	fm, ok := frontmatter(body)
	if !ok {
		return DocNote{}
	}

	web := fmText(fm[fmURL])
	if urls := fmList(fm[fmURLs]); web == "" && len(urls) > 0 {
		web = urls[0]
	}
	return DocNote{
		Deprecated:     fmBool(fm[fmDeprecated]),
		Tags:           fmList(fm[fmTags]),
		Devices:        fmList(fm[fmDevice]),
		Places:         append(fmList(fm[fmPlace]), fmList(fm[fmPlaces])...),
		Compose:        fmList(fm[fmCompose]),
		Web:            web,
		Images:         fmList(fm[fmImage]),
		ExternalPorts:  fmList(fm[fmExternalPorts]),
		InternalPorts:  fmList(fm[fmInternalPorts]),
		TailscalePorts: fmList(fm[fmTailscalePort]),
		SSOPossible:    fmBool(fm[fmSSOPossible]),
		SSOActive:      fmBool(fm[fmSSOActive]),
		Backup:         fmList(fm[fmBackup]),
		DependsOn:      fmList(fm[fmDependsOn]),
		Checked:        fmText(fm[fmChecked]),
	}
}

// frontmatter decodes the YAML between a note's leading "---" lines.
func frontmatter(body []byte) (map[string]any, bool) {
	const fence = "---"
	rest, ok := bytes.CutPrefix(body, []byte(fence+"\n"))
	if !ok {
		return nil, false
	}
	head, _, ok := bytes.Cut(rest, []byte("\n"+fence))
	if !ok {
		return nil, false
	}
	var fm map[string]any
	if err := yaml.Unmarshal(head, &fm); err != nil {
		return nil, false
	}
	return fm, true
}

// fmList reads a value or a list of values; empty entries are dropped.
func fmList(v any) []string {
	items, ok := v.([]any)
	if !ok {
		items = []any{v}
	}
	var out []string
	for _, item := range items {
		if s := fmText(item); s != "" {
			out = append(out, s)
		}
	}
	return out
}

// fmText reads one value as text: a link becomes the note's name
// ("[[IT/Geräte/Eredin|E]]" → "Eredin"), a date "2006-01-02".
func fmText(v any) string {
	switch t := v.(type) {
	case nil:
		return ""
	case time.Time:
		return t.Format(time.DateOnly)
	case string:
		s := strings.TrimSpace(t)
		if m := wikiLink.FindStringSubmatch(s); m != nil {
			return strings.TrimSuffix(path.Base(strings.TrimSpace(m[1])), noteExt)
		}
		return s
	}
	return fmt.Sprint(v)
}

func fmBool(v any) bool {
	b, _ := v.(bool)
	return b
}

// escapePath escapes each segment of a repo path ("IT/Geräte" →
// "IT/Ger%C3%A4te").
func escapePath(p string) string {
	parts := strings.Split(p, "/")
	for i, s := range parts {
		parts[i] = url.PathEscape(s)
	}
	return strings.Join(parts, "/")
}
