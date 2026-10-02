package widgets

// "exposure": what Pangolin makes public, riskiest first: without login,
// with pending updates or a certificate about to run out.

import (
	"sort"

	"andon/internal/enums"
	"andon/internal/metrics"
	"andon/internal/sources"
)

// ExposedRow is one public resource as drawn.
type ExposedRow struct {
	Name, Domain string
	Login        bool
	Updates      int
	CertDays     int // -1 unknown
	Risk         int
}

// ExposureConfig is the "exposure" widget's config.
type ExposureConfig struct{ OnlyOpen bool }

func init() {
	Tile[ExposureConfig]{Key: "exposure", Detail: exposureDetail, Category: CategoryInsight, Topic: TopicSecurity, Service: enums.ServicePangolin, RefreshS: 1800,
		Fields:  []Field{{Key: "only_problems", Input: InputCheck}},
		Renames: []rename{{from: "only_open", to: "only_problems"}},
		Decode:  func(r Raw) ExposureConfig { return ExposureConfig{OnlyOpen: r.Bool("only_problems")} },
		Queries: func(ExposureConfig) []Query { return append(dataQuery(nil), homelabQueries(TableExposure)...) },
		View:    exposureView,
		Calm:    func(v map[string]any) bool { return v["Total"] != nil && v["Total"] != 0 && v["Open"] == 0 }}.add()
}

// exposureView needs the peers besides the data, so it is no dataView.
func exposureView(cfg ExposureConfig, results map[string]any, ctx ViewCtx) map[string]any {
	data, ok := results["data"].(*sources.PangolinDataset)
	if !ok {
		return map[string]any{}
	}
	certs, _ := results[string(enums.ServiceCerts)].(*sources.CertDataset)
	updates := metrics.PendingUpdates(peerDatasets(results, updatePeers))
	var rows []ExposedRow
	open := 0
	all := metrics.Exposure(data, certs, updates, todayOf(ctx))
	for _, r := range all {
		if !r.Login {
			open++
		} else if cfg.OnlyOpen {
			continue
		}
		rows = append(rows, ExposedRow{Name: r.Name, Domain: r.Domain, Login: r.Login, Updates: len(r.Updates), CertDays: r.CertDays, Risk: r.Risk})
	}
	sort.SliceStable(rows, func(i, j int) bool { return rows[i].Risk > rows[j].Risk })
	return map[string]any{"Rows": rows, "Open": open, "Total": len(all)}
}
