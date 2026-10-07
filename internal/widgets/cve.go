package widgets

// "vulnerabilities": recent CVEs (NVD) that hit a running image of the
// space, affected ones first; the dialog lists every match with its
// image, host, score and fixed version. The check is the same as the
// hint's (rules: cross.image_cve).

import (
	"andon/internal/enums"
	"andon/internal/metrics"
	"andon/internal/sources"
)

// VulnConfig is the "vulnerabilities" widget's config.
type VulnConfig struct{ Limit int }

const vulnShown = 5

func init() {
	Tile[VulnConfig]{Key: "vulnerabilities", Detail: vulnDetail, Category: CategoryInsight, Topic: TopicSecurity, Service: enums.ServiceNVD, RefreshS: 3600,
		Fields: []Field{{Key: "limit", Input: InputNumber, Default: vulnShown, Min: "1", Max: "20"}},
		Decode: func(r Raw) VulnConfig { return VulnConfig{Limit: r.Int("limit")} },
		Queries: func(VulnConfig) []Query {
			out := dataQuery(nil)
			for _, s := range []enums.ServiceType{enums.ServiceDocker, enums.ServiceGitea, enums.ServicePangolin} {
				out = append(out, Query{Name: string(s), Source: "data", Conn: ConnPeer, Service: s})
			}
			return out
		},
		View: vulnView,
		Calm: func(v map[string]any) bool { return v["Read"] == true && v["Affected"] == 0 }}.add()
}

// vulnMatches checks the space's running images against the NVD data.
func vulnMatches(results map[string]any) ([]metrics.CVEMatch, *sources.NVDDataset, int) {
	nvd, ok := results[dataName].(*sources.NVDDataset)
	if !ok {
		return nil, nil, 0
	}
	images := metrics.RunningImages(results)
	return metrics.ImageCVEs(images, nvd.CVEs), nvd, len(images)
}

func vulnView(cfg VulnConfig, results map[string]any, _ ViewCtx) map[string]any {
	matches, nvd, images := vulnMatches(results)
	if nvd == nil {
		return map[string]any{}
	}
	affected := 0
	for _, m := range matches {
		if m.State == metrics.CVEAffected {
			affected++
		}
	}
	shown := matches[:min(len(matches), cfg.Limit)]
	return map[string]any{"Read": true, "Affected": affected, "Check": len(matches) - affected, "Lines": shown,
		"More": len(matches) - len(shown), "Images": images, "Days": nvd.Days}
}

func vulnDetail(_ VulnConfig, results map[string]any, _ ViewCtx) DetailView {
	matches, nvd, images := vulnMatches(results)
	if nvd == nil {
		return DetailView{Body: &DetailBody{}}
	}
	affected := 0
	var rows [][]Cell
	for _, m := range matches {
		state := "warn"
		if m.State == metrics.CVEAffected {
			affected++
			state = "bad"
		}
		rows = append(rows, []Cell{{Value: m.CVE.ID, Href: "https://nvd.nist.gov/vuln/detail/" + m.CVE.ID}, {Value: m.Image.Image},
			{Value: m.Image.Where}, {Value: orNone(m.Image.Host)}, {Value: Num(m.CVE.Score, 1)},
			{Value: Txt("cve.state." + string(m.State)), State: state}, {Value: orNone(m.Product.To)}})
	}
	body := &DetailBody{
		Facts: []Kpi{{Value: affected, Label: T("cve.affected"), Tier: tierIf(affected > 0, "red", "green")},
			{Value: len(matches) - affected, Label: T("cve.check"), Tier: tierIf(len(matches) > affected, "yellow", "")},
			{Value: len(nvd.CVEs), Label: textArgs("cve.read", "days", nvd.Days)}, {Value: images, Label: T("cve.images")}},
	}
	if len(rows) > 0 {
		body.Blocks = append(body.Blocks, Block{Kind: BlockTable, Label: T("cve.matches"), Data: Table{
			Head: []Text{T("cve.col.id"), T("cve.col.image"), T("cve.col.where"), T("cve.col.host"), T("cve.col.score"), T("cve.col.state"), T("cve.col.fixed")},
			Rows: rows, Num: []int{4}}})
	} else {
		body.Blocks = append(body.Blocks, Block{Kind: BlockText, Data: Txt("cve.none")})
	}
	body.Blocks = append(body.Blocks, hintsBlock(results)...)
	return DetailView{Body: body}
}

func orNone(s string) any {
	if s == "" {
		return "–"
	}
	return s
}
