package rules

import (
	"strings"

	"andon/internal/enums"
	"andon/internal/sources"
)

const gibibyte = 1 << 30

func init() {
	registerDNSFilter(enums.ServicePihole)
	registerDNSFilter(enums.ServiceAdGuard)
	registerNextcloud()
	registerSabnzbd()
	registerGluetun()
	registerDomains()
	registerMailAuth()
	registerBlacklist()
}

// registerDNSFilter: Pi-hole and AdGuard share their rules.
func registerDNSFilter(service enums.ServiceType) {
	svc := string(service)

	// Blocking switched off "for five minutes" is easily forgotten.
	Register(svc+".disabled", svc, nil, on(dnsDisabled(svc)))

	if service != enums.ServicePihole {
		return
	}
	Register("pihole.lists_old", svc, map[string]any{"days": 14.0}, on(listsOld))
}

// dnsDisabled is rule <svc>.disabled of one DNS filter service.
func dnsDisabled(svc string) func(*sources.DNSFilterDataset, map[string]any, Env) []Finding {
	return func(data *sources.DNSFilterDataset, cfg map[string]any, env Env) []Finding {
		if data.Enabled {
			return nil
		}
		return []Finding{svcFinding(svc, svc+".disabled", "disabled", "dnsfilter.disabled", enums.SeverityWarn, data.URL,
			map[string]any{"service": svc})}
	}
}

func listsOld(data *sources.DNSFilterDataset, cfg map[string]any, env Env) []Finding {
	if data.ListsUpdated.Unix() <= 0 {
		return nil
	}
	days := int(env.Today.Sub(data.ListsUpdated).Hours() / hoursPerDay)
	if float64(days) <= cfgFloat(cfg, "days") {
		return nil
	}
	return []Finding{svcFinding(piholeSvc, "pihole.lists_old", "gravity", "pihole.lists_old", enums.SeverityInfo, data.URL,
		map[string]any{"days": days})}
}

func registerNextcloud() {
	Register("nextcloud.disk_low", nextcloudSvc, map[string]any{"warn_gb": 20.0, "critical_gb": 5.0}, on(diskLow))

	Register("nextcloud.app_updates", nextcloudSvc, nil, on(nextcloudAppUpdates))
}

func diskLow(data *sources.NextcloudDataset, cfg map[string]any, env Env) []Finding {
	if data.FreeBytes < 0 {
		return nil
	}
	gb := data.FreeBytes / gibibyte
	level := enums.SeverityWarn
	if gb <= cfgFloat(cfg, "critical_gb") {
		level = enums.SeverityCritical
	} else if gb > cfgFloat(cfg, "warn_gb") {
		return nil
	}
	return []Finding{svcFinding(nextcloudSvc, "nextcloud.disk_low", "disk", "nextcloud.disk_low", level, data.URL,
		map[string]any{"gb": int(gb + 0.5)})}
}

func nextcloudAppUpdates(data *sources.NextcloudDataset, cfg map[string]any, env Env) []Finding {
	if data.AppUpdates == 0 {
		return nil
	}
	return []Finding{svcFinding(nextcloudSvc, "nextcloud.app_updates", "apps", "nextcloud.app_updates", enums.SeverityInfo,
		strings.TrimRight(data.URL, "/")+"/settings/apps/updates", map[string]any{"count": data.AppUpdates})}
}

func registerSabnzbd() {
	Register("sabnzbd.failed", sabnzbdSvc, map[string]any{"days": 7.0}, on(sabnzbdFailed))

	// A full download disk pauses every job.
	Register("sabnzbd.disk_low", sabnzbdSvc, map[string]any{"warn_gb": 25.0, "critical_gb": 5.0}, on(sabnzbdDiskLow))
}

func sabnzbdFailed(data *sources.SabnzbdDataset, cfg map[string]any, env Env) []Finding {
	var found []Finding
	for _, f := range data.Failures {
		if env.Today.Sub(f.At).Hours()/hoursPerDay > cfgFloat(cfg, "days") {
			continue
		}
		found = append(found, svcFinding(sabnzbdSvc, "sabnzbd.failed", "failed:"+f.Name, "sabnzbd.failed", enums.SeverityWarn,
			data.URL, map[string]any{"name": f.Name, "reason": f.Reason}))
	}
	return found
}

func sabnzbdDiskLow(data *sources.SabnzbdDataset, cfg map[string]any, env Env) []Finding {
	level := enums.SeverityWarn
	if data.FreeGB <= cfgFloat(cfg, "critical_gb") {
		level = enums.SeverityCritical
	} else if data.FreeGB > cfgFloat(cfg, "warn_gb") {
		return nil
	}
	return []Finding{svcFinding(sabnzbdSvc, "sabnzbd.disk_low", "disk", "sabnzbd.disk_low", level, data.URL,
		map[string]any{"gb": int(data.FreeGB + 0.5)})}
}

func registerGluetun() {
	Register("gluetun.vpn", gluetunSvc, nil, on(gluetunVPN))
}

func gluetunVPN(data *sources.GluetunDataset, cfg map[string]any, env Env) []Finding {
	var found []Finding
	if data.Status != "running" {
		found = append(found, svcFinding(gluetunSvc, "gluetun.vpn", "down", "gluetun.down", enums.SeverityCritical, data.URL,
			map[string]any{"status": data.Status}))
	}

	// Same exit IP as the dashboard's own: traffic leaves unprotected.
	if data.ExitIP != "" && data.ExitIP == data.OwnIP {
		found = append(found, svcFinding(gluetunSvc, "gluetun.vpn", "leak", "gluetun.leak", enums.SeverityCritical, data.URL,
			map[string]any{"ip": data.ExitIP}))
	}
	if data.ExpectedCountry != "" && data.Country != "" && !strings.EqualFold(data.ExpectedCountry, data.Country) {
		found = append(found, svcFinding(gluetunSvc, "gluetun.vpn", "country", "gluetun.country", enums.SeverityWarn, data.URL,
			map[string]any{"country": data.Country, "expected": data.ExpectedCountry}))
	}
	return found
}

func registerDomains() {
	Register("domains.expiring", domainsSvc, map[string]any{"info_days": 60.0, "warn_days": 30.0, "critical_days": 7.0}, on(domainsExpiring))
}

func domainsExpiring(data *sources.DomainsDataset, cfg map[string]any, env Env) []Finding {
	var found []Finding
	for _, d := range data.Domains {
		if d.Expires.IsZero() {
			continue // registry without a date, e.g. .de
		}
		daysLeft := int(d.Expires.Sub(env.Today).Hours() / hoursPerDay)
		level, ok := expiryLevel(daysLeft, cfg)
		if !ok {
			continue
		}
		f := svcFinding(domainsSvc, "domains.expiring", "domain:"+d.Name, "domains.expiring", level, "",
			map[string]any{"domain": d.Name, "day": Day(d.Expires), "days": daysLeft})
		// Name what breaks with the domain: resources, monitors, tiles.
		if chain, ok := chainOf(env, data, d.Name); ok && chain.Dependents() > 0 {
			f.Message = "domains.expiring_deps"
			f.Params["deps"] = chain.Dependents()
			f.Params["names"] = shortList(append(append(append([]string(nil), chain.Resources...), chain.Monitors...), chain.Tiles...))
		}
		f.Due = d.Expires.Format("2006-01-02")
		found = append(found, f)
	}
	return found
}

// registerMailAuth: a domain without SPF or DMARC lets anyone send mail
// in its name; parked domains need "v=spf1 -all" and p=reject too.
func registerMailAuth() {
	Register("domains.mail_auth", domainsSvc, nil, on(mailAuth))
}

func mailAuth(data *sources.DomainsDataset, cfg map[string]any, env Env) []Finding {
	var found []Finding
	for _, d := range data.Domains {
		var missing []string
		if !d.SPF {
			missing = append(missing, "SPF")
		}
		if !d.DMARC {
			missing = append(missing, "DMARC")
		}
		if !d.MailChecked || len(missing) == 0 {
			continue
		}
		found = append(found, svcFinding(domainsSvc, "domains.mail_auth", "mailauth:"+d.Name, "domains.mail_auth",
			enums.SeverityWarn, "", map[string]any{"domain": d.Name, "missing": strings.Join(missing, ", ")}))
	}
	return found
}

func registerBlacklist() {
	Register("blacklist.listed", blacklistSvc, nil, on(blacklistListed))
}

func blacklistListed(data *sources.BlacklistDataset, cfg map[string]any, env Env) []Finding {
	var found []Finding
	for _, l := range data.Listings {
		found = append(found, svcFinding(blacklistSvc, "blacklist.listed", "listed:"+l.IP+":"+l.Zone, "blacklist.listed",
			enums.SeverityCritical, "", map[string]any{"ip": l.IP, "zone": l.Zone}))
	}
	if len(data.Refused) > 0 {
		found = append(found, svcFinding(blacklistSvc, "blacklist.listed", "refused", "blacklist.refused", enums.SeverityInfo, "",
			map[string]any{"zones": shortList(append([]string(nil), data.Refused...))}))
	}
	return found
}
