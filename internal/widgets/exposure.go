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

func decodeExposure(raw map[string]any) any {
	return ExposureConfig{OnlyOpen: asBool(raw["only_problems"])}
}

func exposureView(cfgAny any, results map[string]any, ctx ViewCtx) map[string]any {
	cfg, _ := cfgAny.(ExposureConfig)
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

func init() {
	Register(WidgetType{Key: "exposure", Decode: decodeExposure, Category: CategoryInsight,
		Service: enums.ServicePangolin, RefreshS: 1800, View: exposureView,
		Queries: func(any) []Query { return append(dataQuery(nil), homelabQueries(TableExposure)...) }})
}
