package metrics

// Image updates as cases of the update bench (services/updates): a newer
// tag What's Up Docker found, matched to the compose stack that pins the
// image, ranked by how urgent it is.
//
//	WUD: immich-server 1.131.0 → 1.132.3 ─────┐
//	FreshRSS: immich-app/immich v1.132.3 ──────┤ (pinned tags without WUD)
//	compose (Gitea): immich/compose.yaml ──────┼─► case: change the tag in a PR
//	NVD: CVE affects 1.131.0 ──────────────────┤   urgency: cve > public > normal
//	Pangolin: immich.example.org public ───────┘

import (
	"cmp"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"andon/internal/sources"
)

// Urgency ranks an update.
type Urgency int

const (
	UrgencyNormal Urgency = iota
	UrgencyPublic         // the service is published to the internet
	UrgencyCVE            // a recent high or critical CVE hits the running tag
)

// UpdateCase is one image with a newer tag.
type UpdateCase struct {
	Key         string // "u-immich-immich-server"
	Container   string
	Image       string // without tag: "ghcr.io/immich-app/immich-server"
	Tag, NewTag string
	Urgency     Urgency
	CVEs        []string // ids hitting Tag
	Stack       *sources.Stack
	Service     string // the stack's service with the image
	Pinned      bool   // the compose file names the tag literally, so a PR can change it
	Written     string // the image as the compose file writes it ("nginx" for "library/nginx")
	Release     string // the release page a feed announced, "" from WUD
}

// UpdateInputs is what the cases are built from; any may be nil.
type UpdateInputs struct {
	WUD    *sources.WUDDataset
	Feeds  *sources.FreshRSSDataset
	Gitea  *sources.GiteaDataset
	NVD    *sources.NVDDataset
	Public []string // names and domains published to the internet, lower case
}

// UpdateCases lists the images with a newer tag, most urgent first.
func UpdateCases(in UpdateInputs) []UpdateCase {
	var out []UpdateCase
	seen := map[string]bool{}
	var found []sources.ImageUpdate
	if in.WUD != nil {
		found = in.WUD.Updates()
	}
	for _, u := range found {
		// The same tag again is a new image behind a floating tag
		// ("latest"): no newer tag for a pull request. The hint and the
		// tile still count it (WUDDataset.Updates).
		if u.NewTag == u.Tag {
			continue
		}
		image, _ := splitTag(u.Image)
		c := UpdateCase{Container: u.Name, Image: image, Tag: u.Tag, NewTag: u.NewTag}
		c.Stack, c.Service, c.Written, c.Pinned = composeOf(in.Gitea, image, u.Tag)
		c.Key = subKey("", "u "+updateWhere(c))
		if c.Key == "" || seen[c.Key] {
			continue
		}
		seen[c.Key] = true
		out = append(out, rank(in, c))
	}
	for _, c := range feedCases(in) {
		if !seen[c.Key] {
			seen[c.Key] = true
			out = append(out, rank(in, c))
		}
	}
	slices.SortStableFunc(out, func(a, b UpdateCase) int {
		return cmp.Or(cmp.Compare(b.Urgency, a.Urgency), strings.Compare(a.Key, b.Key))
	})
	return out
}

// rank sets a case's urgency: a CVE on the running tag, else a public
// service.
func rank(in UpdateInputs, c UpdateCase) UpdateCase {
	c.CVEs = cvesOf(in.NVD, c.Image, c.Tag)
	switch {
	case len(c.CVEs) > 0:
		c.Urgency = UrgencyCVE
	case isPublic(in.Public, c.Image, c.Container):
		c.Urgency = UrgencyPublic
	}
	return c
}

// feedCases: a pinned compose image whose project announced a newer
// release in a feed. The new tag keeps the running tag's form: "v2.1"
// and release v2.2.0 → "v2.2"; "apache-2.40" → "apache-2.41".
func feedCases(in UpdateInputs) []UpdateCase {
	if in.Feeds == nil || in.Gitea == nil {
		return nil
	}
	var out []UpdateCase
	for i := range in.Gitea.Stacks {
		s := &in.Gitea.Stacks[i]
		for _, svc := range s.Services {
			image, tag := splitTag(svc.Image)
			rel, ok := releaseOf(in.Feeds.Releases, image)
			if !ok {
				continue
			}
			next, ok := newerTag(tag, rel.Version)
			if !ok {
				continue
			}
			c := UpdateCase{Container: svc.Name, Image: image, Tag: tag, NewTag: next, Stack: s, Service: svc.Name, Pinned: true, Written: image, Release: rel.Link}
			c.Key = subKey("", "u "+updateWhere(c))
			out = append(out, c)
		}
	}
	return out
}

// releaseOf finds the release of an image's project:
//
//	same name, owners alike      freikin/dawarich ~ freika/dawarich, caddy ~ caddyserver/caddy
//	same name, repackaged        linuxserver/sonarr ~ sonarr/sonarr
//	same owner, name after repo  ghcr.io/immich-app/immich-server ~ immich-app/immich
//	official image as owner      nextcloud ~ nextcloud/server
//
// A name alone is not enough: "server" and "core" are many projects'.
func releaseOf(releases []sources.FeedRelease, image string) (sources.FeedRelease, bool) {
	parts := strings.Split(strings.ToLower(image), "/")
	name := alnum(parts[len(parts)-1])
	owner := ""
	if len(parts) >= 2 {
		owner = alnum(parts[len(parts)-2])
	}
	official := owner == "" || owner == "library"
	for _, r := range releases {
		rOwner, rName, _ := strings.Cut(r.Repo, "/")
		rOwner, rName = alnum(rOwner), alnum(rName)
		switch {
		case rName == name && (official || alike(owner, rOwner) || repackagers[owner]):
			return r, true
		case !official && owner == rOwner && strings.HasPrefix(name, rName):
			return r, true
		case official && rOwner == name:
			return r, true
		}
	}
	return sources.FeedRelease{}, false
}

// repackagers publish other projects' software under their own name.
var repackagers = map[string]bool{"linuxserver": true}

// alike: two owner names start alike (freikin ~ freika, caddy ~
// caddyserver).
func alike(a, b string) bool {
	n := minProduct
	return len(a) >= n && len(b) >= n && a[:n] == b[:n]
}

// versionRun is the first run of numbers in a tag.
var versionRun = regexp.MustCompile(`\d+(\.\d+)*`)

// newerTag is tag with its version replaced by release's, cut to as
// many parts; ok when that is newer.
func newerTag(tag, release string) (string, bool) {
	loc := versionRun.FindStringIndex(tag)
	want := versionRun.FindString(release)
	if loc == nil || want == "" {
		return "", false
	}
	have := strings.Split(tag[loc[0]:loc[1]], ".")
	next := strings.Split(want, ".")
	if len(next) < len(have) {
		return "", false
	}
	next = next[:len(have)]
	if compareParts(next, have) <= 0 {
		return "", false
	}
	return tag[:loc[0]] + strings.Join(next, ".") + tag[loc[1]:], true
}

func compareParts(a, b []string) int {
	for i := range min(len(a), len(b)) {
		x, _ := strconv.Atoi(a[i])
		y, _ := strconv.Atoi(b[i])
		if c := cmp.Compare(x, y); c != 0 {
			return c
		}
	}
	return cmp.Compare(len(a), len(b))
}

// updateWhere names a case: the stack and service, else the container.
func updateWhere(c UpdateCase) string {
	if c.Stack != nil {
		return c.Stack.Host + " " + c.Stack.Name + " " + c.Service
	}
	return c.Container
}

// splitTag cuts "name:tag" (a registry port is no tag).
func splitTag(image string) (string, string) {
	image, _, _ = strings.Cut(image, "@")
	if i := strings.LastIndex(image, ":"); i > strings.LastIndex(image, "/") {
		return image[:i], image[i+1:]
	}
	return image, ""
}

// composeOf finds the compose service running image, with the image as
// the file writes it ("nginx" for "library/nginx"); pinned says the file
// names tag literally ("immich:1.131.0", not "immich:${VERSION}").
func composeOf(g *sources.GiteaDataset, image, tag string) (*sources.Stack, string, string, bool) {
	if g == nil {
		return nil, "", "", false
	}
	var found *sources.Stack
	var service, written string
	for i := range g.Stacks {
		s := &g.Stacks[i]
		for _, svc := range s.Services {
			name, t := splitTag(svc.Image)
			if !sameImage(name, image) {
				continue
			}
			if t == tag {
				return s, svc.Name, name, true
			}
			if found == nil {
				found, service, written = s, svc.Name, name
			}
		}
	}
	return found, service, written, false
}

// sameImage: Docker Hub's short form names the same image
// ("immich-app/immich-server" ~ "docker.io/immich-app/immich-server").
func sameImage(a, b string) bool {
	trim := func(s string) string {
		s = strings.ToLower(s)
		for _, p := range []string{"docker.io/", "index.docker.io/", "library/"} {
			s = strings.TrimPrefix(s, p)
		}
		return s
	}
	return trim(a) == trim(b)
}

// cvesOf are the CVEs that hit the image at tag (affected, not merely
// to check).
func cvesOf(nvd *sources.NVDDataset, image, tag string) []string {
	if nvd == nil {
		return nil
	}
	var out []string
	for _, m := range ImageCVEs([]RunningImage{{Image: image + ":" + tag}}, nvd.CVEs) {
		if m.State == CVEAffected && !slices.Contains(out, m.CVE.ID) {
			out = append(out, m.CVE.ID)
		}
	}
	return out
}

// isPublic: a published name contains the image's or the container's
// name ("immich.example.org" ~ immich-server).
func isPublic(public []string, image, container string) bool {
	name, _ := imageParts(image)
	for _, p := range public {
		p = alnum(p)
		for _, n := range []string{alnum(container), name} {
			short := n[:min(len(n), minProduct+2)]
			if len(short) >= minProduct && strings.Contains(p, short) {
				return true
			}
		}
	}
	return false
}
