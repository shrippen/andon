package metrics

// Releases newer than what runs: a watched repo (GitHub, options repos)
// and a running image of the same product, its tag below the release.
//
//	immich-app/immich v1.132.3 ↔ ghcr.io/immich-app/immich-server:v1.131.0 → behind

import "andon/internal/sources"

// ImageBehind is a running image older than its project's release.
type ImageBehind struct {
	Image   RunningImage
	Repo    string
	Release string
}

// ImagesBehind matches running images to repos by name (the repo's last
// segment as product) and compares versions; tags without one are left out.
func ImagesBehind(images []RunningImage, repos []sources.GitRepo) []ImageBehind {
	var out []ImageBehind
	for _, img := range images {
		name, tag := imageParts(img.Image)
		version := versionPat.FindString(tag)
		if version == "" {
			continue
		}
		for _, r := range repos {
			if r.Release == "" || versionPat.FindString(r.Release) == "" {
				continue
			}
			product := r.Name[lastSlash(r.Name)+1:]
			if !productMatches(name, sources.CVEProduct{Product: product}) {
				continue
			}
			if compareVersions(version, r.Release) < 0 {
				out = append(out, ImageBehind{Image: img, Repo: r.Name, Release: r.Release})
			}
			break
		}
	}
	return out
}

func lastSlash(s string) int {
	for i := len(s) - 1; i >= 0; i-- {
		if s[i] == '/' {
			return i
		}
	}
	return -1
}
