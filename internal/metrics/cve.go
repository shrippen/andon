package metrics

// Which CVEs hit a running image:
//
//	Docker containers (running) ─┐
//	compose stacks (Gitea)      ─┴─► RunningImage{Image, Where, Host}
//	                                     │ product: "ghcr.io/immich-app/immich-server:v1.131.0" → immichserver ↔ CPE "immich"
//	                                     │ version: tag 1.131.0 < fixed 1.132.0 → affected; no version in the tag → check
//	NVD CVEs ───────────────────────────►┴─► []CVEMatch, affected first, highest score first

import (
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"andon/internal/sources"
)

// CVEState says whether a running image is hit.
type CVEState string

const (
	CVEAffected CVEState = "affected" // its version lies in the range
	CVECheck    CVEState = "check"    // the tag tells no version
)

// minProduct keeps short product names ("go", "git") from matching every
// image that starts with them.
const minProduct = 4

// runningState is Docker's state of a container that runs.
const runningState = "running"

// RunningImage is an image in use: a container or a stack's service.
type RunningImage struct {
	Image string // "gitea/gitea:1.24"
	Where string // container or "stack/service"
	Host  string // host name, "" unknown
}

// CVEMatch is a CVE that hits a running image.
type CVEMatch struct {
	CVE     sources.CVE
	Product sources.CVEProduct
	Image   RunningImage
	State   CVEState
}

// RunningImages lists the images of running Docker containers and of the
// compose stacks' services.
func RunningImages(datasets map[string]any) []RunningImage {
	var out []RunningImage
	for _, raw := range datasets {
		switch d := raw.(type) {
		case *sources.DockerDataset:
			host := urlHost(d.URL)
			for _, c := range d.Containers {
				if c.State == runningState && c.Image != "" {
					out = append(out, RunningImage{Image: c.Image, Where: c.Name, Host: host})
				}
			}
		case *sources.GiteaDataset:
			for _, s := range d.Stacks {
				for _, svc := range s.Services {
					if svc.Image != "" {
						out = append(out, RunningImage{Image: svc.Image, Where: s.Name + "/" + svc.Name, Host: s.Host})
					}
				}
			}
		}
	}
	return out
}

// urlHost is a URL's lower-case host name.
func urlHost(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	return strings.ToLower(u.Hostname())
}

// ImageCVEs matches every image against every CVE's products.
func ImageCVEs(images []RunningImage, cves []sources.CVE) []CVEMatch {
	var out []CVEMatch
	for _, img := range images {
		name, tag := imageParts(img.Image)
		for _, c := range cves {
			for _, p := range c.Products {
				if !productMatches(name, p) {
					continue
				}
				state, hit := versionHit(tag, p)
				if hit {
					out = append(out, CVEMatch{CVE: c, Product: p, Image: img, State: state})
				}
				break
			}
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].State != out[j].State {
			return out[i].State == CVEAffected
		}
		return out[i].CVE.Score > out[j].CVE.Score
	})
	return out
}

// imageParts splits "ghcr.io/immich-app/immich-server:v1.131.0" into the
// name's last segment, letters and digits only ("immichserver"), and the
// tag ("v1.131.0"); a digest counts as no tag.
func imageParts(image string) (string, string) {
	image, _, _ = strings.Cut(image, "@")
	name, tag := image, ""
	if i := strings.LastIndex(image, ":"); i > strings.LastIndex(image, "/") {
		name, tag = image[:i], image[i+1:]
	}
	if i := strings.LastIndex(name, "/"); i >= 0 {
		name = name[i+1:]
	}
	return alnum(name), tag
}

var notAlnum = regexp.MustCompile(`[^a-z0-9]`)

func alnum(s string) string { return notAlnum.ReplaceAllString(strings.ToLower(s), "") }

// productMatches: the image name starts with the product (immichserver ↔
// immich, kimai2 ↔ kimai) or the product with the name (postgresql ↔
// postgres), both at least minProduct long.
func productMatches(name string, p sources.CVEProduct) bool {
	product := alnum(p.Product)
	if len(product) < minProduct || len(name) < minProduct {
		return false
	}
	return strings.HasPrefix(name, product) || strings.HasPrefix(product, name)
}

var versionPat = regexp.MustCompile(`\d+(\.\d+)*`)

// versionHit says whether a tag lies in the product's range: affected, or
// a check when the tag tells no version; false when it lies outside.
func versionHit(tag string, p sources.CVEProduct) (CVEState, bool) {
	v := versionPat.FindString(tag)
	if v == "" {
		return CVECheck, true
	}
	if p.From != "" && compareVersions(v, p.From) < 0 {
		return "", false
	}
	if p.To != "" {
		c := compareVersions(v, p.To)
		if c > 0 || (c == 0 && !p.ToIncluded) {
			return "", false
		}
	}
	return CVEAffected, true
}

// compareVersions compares dotted numbers part by part, a missing part
// as 0: "1.24" < "1.24.3", "2.40" = "2.40.0".
func compareVersions(a, b string) int {
	pa, pb := strings.Split(a, "."), strings.Split(versionPat.FindString(b), ".")
	for i := range max(len(pa), len(pb)) {
		x, y := partAt(pa, i), partAt(pb, i)
		if x != y {
			if x < y {
				return -1
			}
			return 1
		}
	}
	return 0
}

func partAt(parts []string, i int) int {
	if i >= len(parts) {
		return 0
	}
	n, _ := strconv.Atoi(parts[i])
	return n
}
