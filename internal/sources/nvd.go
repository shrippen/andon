package sources

// NVD (National Vulnerability Database): the high and critical CVEs
// published in the last days, each with the products and versions it
// affects. Which of them hit a running image is a cross check
// (rules: cross.image_cve), as only it sees Docker and the compose stacks.
// An API key is optional; without one the NVD allows 5 requests in 30 s.
//
//	GET rest/json/cves/2.0?pubStartDate=…&pubEndDate=…&cvssV3Severity=HIGH&startIndex=0
//	    → {"totalResults", "vulnerabilities": [{"cve": {id, published, descriptions, metrics, configurations}}]}

import (
	"context"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"andon/internal/drivers/services"
	"andon/internal/enums"
	"andon/internal/sources/demoworld"
)

const (
	nvdPath       = "rest/json/cves/2.0"
	nvdPageSize   = 2000
	nvdMaxPages   = 10
	nvdDays       = 30
	nvdMaxDays    = 120 // the NVD's longest date range
	nvdTTL        = 6 * time.Hour
	nvdSummaryMax = 300
	nvdTime       = "2006-01-02T15:04:05.000"
	nvdAppPart    = "a" // CPE part of applications (not o: systems, h: hardware)
)

// nvdSeverities are the CVSS levels read; lower ones would flood the list.
var nvdSeverities = []string{"CRITICAL", "HIGH"}

// CVE is one vulnerability.
type CVE struct {
	ID        string
	Score     float64 // CVSS base score, 0 = unknown
	Published time.Time
	Summary   string
	Products  []CVEProduct
}

// CVEProduct is a product range a CVE affects: vendor and product as the
// CPE names them, versions From (included) up to To.
type CVEProduct struct {
	Vendor, Product string
	From, To        string // "" = open
	ToIncluded      bool   // To is affected too (versionEndIncluding)
}

// NVDDataset is the CVEs of the window.
type NVDDataset struct {
	URL  string
	Days int
	CVEs []CVE
}

var NVDData = source{key: "nvd.data", ttl: nvdTTL, service: enums.ServiceNVD, fetch: fetchNVD}

func fetchNVD(ctx context.Context, sctx Ctx) (any, error) {
	if isDemo(sctx) {
		return DemoNVD(time.Now().UTC()), nil
	}
	days := int(asFloat(sctx.Options["days"]))
	if days <= 0 {
		days = nvdDays
	}
	days = min(days, nvdMaxDays)
	api := services.KeyedApi{URL: sctx.URL, Headers: map[string]string{"Accept": "application/json"}, Verify: sctx.VerifyTLS}
	if sctx.Secret != "" {
		api = services.HeaderApi(sctx.URL, "apiKey", sctx.Secret, sctx.TLS())
	}

	end := time.Now().UTC()
	start := end.AddDate(0, 0, -days)
	data := &NVDDataset{URL: sctx.URL, Days: days}
	for _, level := range nvdSeverities {
		for page := range nvdMaxPages {
			params := url.Values{"pubStartDate": {start.Format(nvdTime)}, "pubEndDate": {end.Format(nvdTime)}, "cvssV3Severity": {level},
				"resultsPerPage": {strconv.Itoa(nvdPageSize)}, "startIndex": {strconv.Itoa(page * nvdPageSize)}}
			raw, err := api.Get(ctx, nvdPath, params)
			if err != nil {
				return nil, fetchError(err)
			}
			answer := asMap(raw)
			list := asList(answer["vulnerabilities"])
			for _, item := range list {
				if c := cveOf(asMap(asMap(item)["cve"])); len(c.Products) > 0 {
					data.CVEs = append(data.CVEs, c)
				}
			}
			if len(list) == 0 || (page+1)*nvdPageSize >= int(asFloat(answer["totalResults"])) {
				break
			}
		}
	}
	return data, nil
}

// cveOf reads one CVE with its affected applications.
func cveOf(c map[string]any) CVE {
	out := CVE{ID: asStr(c["id"])}
	// The NVD writes UTC without a zone: 2026-10-01T10:00:00.000.
	if t, err := time.Parse(nvdTime, asStr(c["published"])); err == nil {
		out.Published = t
	}
	for _, d := range asList(c["descriptions"]) {
		if m := asMap(d); asStr(m["lang"]) == "en" {
			out.Summary = cut(asStr(m["value"]), nvdSummaryMax)
		}
	}
	metrics := asMap(c["metrics"])
	for _, key := range []string{"cvssMetricV31", "cvssMetricV30", "cvssMetricV40"} {
		if list := asList(metrics[key]); len(list) > 0 {
			out.Score = asFloat(asMap(asMap(list[0])["cvssData"])["baseScore"])
			break
		}
	}
	for _, conf := range asList(c["configurations"]) {
		for _, node := range asList(asMap(conf)["nodes"]) {
			for _, raw := range asList(asMap(node)["cpeMatch"]) {
				m := asMap(raw)
				if vulnerable, _ := m["vulnerable"].(bool); !vulnerable {
					continue
				}
				// cpe:2.3:a:gitea:gitea:*:… → part a, vendor gitea, product gitea
				parts := strings.Split(asStr(m["criteria"]), ":")
				if len(parts) < 6 || parts[2] != nvdAppPart {
					continue
				}
				p := CVEProduct{Vendor: parts[3], Product: parts[4], From: asStr(m["versionStartIncluding"]), To: asStr(m["versionEndExcluding"])}
				if p.To == "" {
					p.To, p.ToIncluded = asStr(m["versionEndIncluding"]), true
				}
				if p.To == "" && parts[5] != "*" && parts[5] != "-" {
					p.To, p.ToIncluded = parts[5], true // one exact version
				}
				if !slices.Contains(out.Products, p) {
					out.Products = append(out.Products, p)
				}
			}
		}
	}
	return out
}

// cut shortens text to n bytes at a word, with an ellipsis.
func cut(text string, n int) string {
	if len(text) <= n {
		return text
	}
	if i := strings.LastIndex(text[:n], " "); i > 0 {
		n = i
	}
	return text[:n] + " …"
}

// DemoNVD is the CVEs for the images Studio Weber runs.
func DemoNVD(now time.Time) *NVDDataset {
	var p struct {
		URL  string
		CVEs []struct {
			ID, Product, Fixed, Summary string
			CVSS                        float64
			Published                   time.Time
		}
	}
	demoworld.MustDecode("vulnerabilities", now, &p)
	data := &NVDDataset{URL: p.URL, Days: nvdDays}
	for _, c := range p.CVEs {
		data.CVEs = append(data.CVEs, CVE{ID: c.ID, Score: c.CVSS, Published: c.Published, Summary: c.Summary,
			Products: []CVEProduct{{Vendor: c.Product, Product: c.Product, To: c.Fixed}}})
	}
	return data
}

func init() {
	Register(NVDData)
	Register(testOf{NVDData, func(d any) map[string]any { return map[string]any{"cves": len(d.(*NVDDataset).CVEs)} }})
}
