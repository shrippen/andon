package rules

import (
	"strings"

	"andon/internal/enums"
	"andon/internal/sources"
)

// Glances: a host's disks, load and swap. The sysinfo tile shows the
// values; these rules turn the lasting problems into hints.

// snapMounts are read-only squashfs images, always 100 % full.
const snapMounts = "/snap/"

func init() {
	svc := string(enums.ServiceGlances)

	Register("glances.disk_full", svc, map[string]any{"warn": 85.0, "critical": 95.0}, func(raw any, cfg map[string]any, env Env) []Finding {
		data, _ := raw.(*sources.GlancesResult)
		var found []Finding
		for _, d := range data.Disks {
			if strings.HasPrefix(d.Mount, snapMounts) || d.Percent < cfgFloat(cfg, "warn") {
				continue
			}
			level := enums.SeverityWarn
			if d.Percent >= cfgFloat(cfg, "critical") {
				level = enums.SeverityCritical
			}
			found = append(found, svcFinding(svc, "glances.disk_full", "disk:"+d.Mount, "glances.disk_full",
				level, data.URL, map[string]any{"mount": d.Mount, "percent": int(d.Percent)}))
		}
		return found
	})

	// Load per core over 5 minutes: 1 means every core is busy.
	Register("glances.load_high", svc, map[string]any{"per_core": 1.5}, func(raw any, cfg map[string]any, env Env) []Finding {
		data, _ := raw.(*sources.GlancesResult)
		if data.Cores == 0 {
			return nil
		}
		perCore := data.Load / float64(data.Cores)
		if perCore < cfgFloat(cfg, "per_core") {
			return nil
		}
		return []Finding{svcFinding(svc, "glances.load_high", "load", "glances.load_high",
			enums.SeverityWarn, data.URL, map[string]any{"load": Num(data.Load, 1), "cores": data.Cores})}
	})

	Register("glances.swap_high", svc, map[string]any{"percent": 80.0}, func(raw any, cfg map[string]any, env Env) []Finding {
		data, _ := raw.(*sources.GlancesResult)
		if data.Swap < cfgFloat(cfg, "percent") {
			return nil
		}
		return []Finding{svcFinding(svc, "glances.swap_high", "swap", "glances.swap_high",
			enums.SeverityWarn, data.URL, map[string]any{"percent": int(data.Swap)})}
	})
}
