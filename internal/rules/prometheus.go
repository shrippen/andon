package rules

// Prometheus alerts as hints:
//
//	prometheus.alert   one hint per firing alert, its level from the
//	                   severity label (critical, warning, else info)
//
// A critical alert is also an outage signal of its instance's host
// (outage.go): with a down monitor or failed connection there, the
// outage hint replaces it.

import (
	"strings"

	"andon/internal/enums"
	"andon/internal/sources"
)

var prometheusSvc = string(enums.ServicePrometheus)

const promAlertRule = "prometheus.alert"

// promLevels maps severity labels to hint levels; others are info.
var promLevels = map[string]enums.Severity{
	"critical": enums.SeverityCritical, "page": enums.SeverityCritical, "error": enums.SeverityCritical,
	"warning": enums.SeverityWarn, "warn": enums.SeverityWarn,
}

func init() {
	Register(promAlertRule, prometheusSvc, nil, on(promAlert))
}

func promAlert(data *sources.PrometheusDataset, _ map[string]any, _ Env) []Finding {
	var found []Finding
	for _, a := range data.Alerts {
		if !a.Firing() {
			continue
		}
		level, ok := promLevels[a.Severity]
		if !ok {
			level = enums.SeverityInfo
		}
		found = append(found, svcFinding(prometheusSvc, promAlertRule, a.Name+"@"+a.Instance, promAlertRule, level,
			strings.TrimRight(data.URL, "/")+"/alerts", map[string]any{"name": a.Name, "instance": a.Instance, "summary": a.Summary}))
	}
	return found
}

// criticalAlerts are the firing critical alerts, outage signals.
func criticalAlerts(env Env) []sources.PromAlert {
	data, ok := env.Datasets[prometheusSvc].(*sources.PrometheusDataset)
	if !ok {
		return nil
	}
	var out []sources.PromAlert
	for _, a := range data.Alerts {
		if a.Firing() && promLevels[a.Severity] == enums.SeverityCritical {
			out = append(out, a)
		}
	}
	return out
}
