package rules

// CVEs against what runs:
//
//	cross.image_cve   a recent high or critical CVE (NVD) hits an image of a
//	                  running container or compose stack (metrics.ImageCVEs):
//	                    affected, CVSS ≥ 9  → critical      affected → warning
//	                    tag without version → info (check)
//	                  one level up when Pangolin publishes the service

import (
	"strings"

	"andon/internal/enums"
	"andon/internal/metrics"
	"andon/internal/sources"
)

var nvdSvc = string(enums.ServiceNVD)

const (
	cveCritical = 9.0 // CVSS from which an affected image is critical
	nvdDetail   = "https://nvd.nist.gov/vuln/detail/"
)

func init() {
	Register("cross.image_cve", Cross, nil, imageCVE)
}

func imageCVE(_ any, _ map[string]any, env Env) []Finding {
	nvd, ok := env.Datasets[nvdSvc].(*sources.NVDDataset)
	if !ok {
		return nil
	}
	public := publishedNames(env)

	var found []Finding
	for _, m := range metrics.ImageCVEs(metrics.RunningImages(env.Datasets), nvd.CVEs) {
		level, msg := enums.SeverityInfo, "cross.image_cve_check"
		if m.State == metrics.CVEAffected {
			level, msg = enums.SeverityWarn, "cross.image_cve"
			if m.CVE.Score >= cveCritical {
				level = enums.SeverityCritical
			}
		}
		params := map[string]any{"id": m.CVE.ID, "image": m.Image.Image, "where": m.Image.Where, "score": Num(m.CVE.Score, 1),
			"fixed": orDash(m.Product.To), "summary": m.CVE.Summary, "public": ""}
		if published(public, m) {
			level = min(level+severityStep, enums.SeverityCritical)
			params["public"] = map[string]any{"$t": "cve.public"}
		}
		found = append(found, Finding{Fingerprint: m.CVE.ID + "@" + m.Image.Where, Severity: level, Message: msg, Params: params,
			ActionURL: nvdDetail + m.CVE.ID, ActionLabel: "open_in_nvd", Sources: []string{nvdSvc}})
	}
	return found
}

// severityStep is one hint level (info → warning → critical).
const severityStep = enums.SeverityWarn - enums.SeverityInfo

func orDash(s string) string {
	if s == "" {
		return "–"
	}
	return s
}

// publishedNames are the names and domains Pangolin serves to the
// internet, lower case.
func publishedNames(env Env) []string {
	pg, ok := env.Datasets[string(enums.ServicePangolin)].(*sources.PangolinDataset)
	if !ok {
		return nil
	}
	var out []string
	for _, r := range pg.Resources {
		if r.Enabled {
			out = append(out, strings.ToLower(r.Name), strings.ToLower(r.Domain))
		}
	}
	return out
}

// published: a public resource names the product ("Immich" ↔ immich).
func published(public []string, m metrics.CVEMatch) bool {
	product := strings.ToLower(m.Product.Product)
	for _, name := range public {
		if product != "" && strings.Contains(name, product) {
			return true
		}
	}
	return false
}
