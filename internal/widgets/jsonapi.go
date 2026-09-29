package widgets

// "jsonapi": the key figures and list an own integration's YAML options
// define (see sources/jsonapi.go).

import (
	"strings"

	"andon/internal/enums"
	"andon/internal/sources"
)

// jsonAPIRefresh: seconds between reloads, matching the source TTL.
const jsonAPIRefresh = 300

// jsonFieldLevel marks a field for the template: "", "warn", "fail".
func jsonFieldLevel(f sources.JSONField) string {
	switch {
	case !f.Numeric:
		return ""
	case f.Critical > 0 && f.Value >= f.Critical:
		return "fail"
	case f.Warn > 0 && f.Value >= f.Warn:
		return "warn"
	}
	return ""
}

// JSONFieldView is one key figure as shown.
type JSONFieldView struct {
	sources.JSONField
	Level, Unit string
}

// JSONAPIConfig is the "jsonapi" widget's config: thresholds and units on
// top of what the integration defines.
type JSONAPIConfig struct {
	Thresholds []Threshold
	Units      map[string]string
}

func decodeJSONAPI(raw map[string]any) any {
	return JSONAPIConfig{Thresholds: parseThresholds(asString(raw["thresholds"])), Units: parsePairs(asString(raw["units"]))}
}

func jsonAPIView(cfgAny any, results map[string]any, _ ViewCtx) map[string]any {
	cfg, _ := cfgAny.(JSONAPIConfig)
	data, ok := results["data"].(*sources.JSONAPIDataset)
	if !ok {
		return map[string]any{}
	}
	fields := make([]JSONFieldView, 0, len(data.Fields))
	for _, f := range data.Fields {
		view := JSONFieldView{JSONField: f, Level: jsonFieldLevel(f), Unit: cfg.Units[strings.ToLower(f.Label)]}
		if f.Numeric && f.Found {
			if own := levelOf(f.Label, f.Value, cfg.Thresholds); own != "" {
				view.Level = own
			}
		}
		fields = append(fields, view)
	}
	return map[string]any{"Fields": fields, "Columns": data.Columns, "Rows": data.Rows}
}

func init() {
	Register(WidgetType{Key: "jsonapi", Decode: decodeJSONAPI, Category: CategoryInsight,
		Service: enums.ServiceJSONAPI, RefreshS: jsonAPIRefresh, Queries: dataQuery, View: jsonAPIView})
}
