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

type diskFullCfg struct {
	Warn     float64 `json:"warn"`
	Critical float64 `json:"critical"`
}

// loadCfg: load per core over 5 minutes; 1 means every core is busy.
type loadCfg struct {
	PerCore float64 `json:"per_core"`
}

type swapCfg struct {
	Percent float64 `json:"percent"`
}

func init() {
	svc := enums.ServiceGlances

	registerTyped("glances.disk_full", svc, diskFullCfg{Warn: 85, Critical: 95}, func(data *sources.GlancesResult, cfg diskFullCfg, env Env) []Finding {
		var found []Finding
		for _, d := range data.Disks {
			if strings.HasPrefix(d.Mount, snapMounts) || d.Percent < cfg.Warn {
				continue
			}
			level := enums.SeverityWarn
			if d.Percent >= cfg.Critical {
				level = enums.SeverityCritical
			}
			found = append(found, svcFinding(string(svc), "glances.disk_full", "disk:"+d.Mount, "glances.disk_full",
				level, data.URL, map[string]any{"mount": d.Mount, "percent": int(d.Percent)}))
		}
		return found
	})

	registerTyped("glances.load_high", svc, loadCfg{PerCore: 1.5}, func(data *sources.GlancesResult, cfg loadCfg, env Env) []Finding {
		if data.Cores == 0 || data.Load/float64(data.Cores) < cfg.PerCore {
			return nil
		}
		return []Finding{svcFinding(string(svc), "glances.load_high", "load", "glances.load_high",
			enums.SeverityWarn, data.URL, map[string]any{"load": Num(data.Load, 1), "cores": data.Cores})}
	})

	registerTyped("glances.swap_high", svc, swapCfg{Percent: 80}, func(data *sources.GlancesResult, cfg swapCfg, env Env) []Finding {
		if data.Swap < cfg.Percent {
			return nil
		}
		return []Finding{svcFinding(string(svc), "glances.swap_high", "swap", "glances.swap_high",
			enums.SeverityWarn, data.URL, map[string]any{"percent": int(data.Swap)})}
	})
}
