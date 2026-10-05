package rules

// Rules of the network, media and everyday integrations:
//
//	tailscale.key_expiry    device key expires within warn_days (expired: critical)
//	tailscale.offline       devices unseen for days
//	gateway.wan_down        a WAN gateway is down (critical)
//	gateway.devices_offline UniFi devices offline
//	gateway.updates         firmware updates waiting
//	mediaserver.update      Jellyfin update available
//	arr.health              Sonarr/Radarr health checks (error: warn)
//	arr.stuck               downloads stuck with a warning
//	vaultwarden.no_2fa      active users without two-factor login
//	speedtest.slow          below share of the expected speed
//	grocy.expired           expired products
//	grocy.missing           products below minimum stock
//	grocy.chores_overdue    chores past due
//	dwd.warning             weather warning (moderate: info … extreme: critical)
//	github.ci_failed        latest CI run on the default branch failed
//	github.review_waiting   a PR has waited N days for my review
//	github.stale_pr         my own PR has not moved for N days
//	kdestore.behind_github  a KDE Store entry still has an older version than its GitHub release
//	energy.cost_rising      last 7 days cost more than factor × the 7 before

import (
	"fmt"
	"path"
	"slices"
	"strings"

	"andon/internal/enums"
	"andon/internal/metrics"
	"andon/internal/sources"
)

const (
	arrError      = "error"
	weekDays      = 7
	costRuleID    = "energy.cost_rising"
	dwdWarningKey = "dwd.warning"
	storeBehindID = "kdestore.behind_github"
)

// dwdLevels maps warning ranks (metrics.WarningRank) to hint levels;
// unknown (0) and minor (1) warnings stay silent.
var dwdLevels = map[int]enums.Severity{2: enums.SeverityInfo, 3: enums.SeverityWarn, 4: enums.SeverityCritical}

// arrChecks are the Sonarr/Radarr health checks with a catalog title.
var arrChecks = map[string]bool{
	"IndexerStatusCheck": true, "IndexerLongTermStatusCheck": true, "IndexerRssCheck": true, "IndexerSearchCheck": true,
	"DownloadClientCheck": true, "DownloadClientStatusCheck": true, "RemovedMovieCheck": true, "RemovedSeriesCheck": true,
	"RootFolderCheck": true, "UpdateCheck": true, "ImportMechanismCheck": true, "ProxyCheck": true,
}

func init() {
	registerNetworkRules()
	registerMediaRules()
	registerEverydayRules()
	topicRules[TopicUpdates] = append(topicRules[TopicUpdates], "gateway.updates", "mediaserver.update")
}

func registerNetworkRules() {
	// A device offline longer than offline_days is left to
	// tailscale.offline: its key does not matter until it returns.
	Register("tailscale.key_expiry", tailscaleSvc, map[string]any{"warn_days": 14, "offline_days": 30}, on(keyExpiry))
	Register("tailscale.offline", tailscaleSvc, map[string]any{"days": 7}, on(tailscaleOffline))

	Register("gateway.wan_down", gatewaySvc, nil, on(wanDown))
	Register("gateway.devices_offline", gatewaySvc, nil, on(devicesOffline))
	Register("gateway.updates", gatewaySvc, nil, on(gatewayUpdates))
}

func keyExpiry(data *sources.TailscaleDataset, cfg map[string]any, env Env) []Finding {
	var found []Finding
	for _, d := range data.Devices {
		if !metrics.KeyExpiring(d, env.Today, cfgFloat(cfg, "warn_days")) || metrics.DeviceGone(d, env.Today, cfgFloat(cfg, "offline_days")) {
			continue
		}
		left := int(d.KeyExpiry.Sub(env.Today).Hours() / hoursPerDay)
		level, msg := enums.SeverityWarn, "tailscale.key_expiry"
		if left < 0 {
			level, msg = enums.SeverityCritical, "tailscale.key_expired"
		}
		found = append(found, svcFinding(tailscaleSvc, "tailscale.key_expiry", "key:"+d.Name, msg, level, data.URL,
			map[string]any{"name": d.Name, "day": Day(d.KeyExpiry)}))
	}
	return found
}

func tailscaleOffline(data *sources.TailscaleDataset, cfg map[string]any, env Env) []Finding {
	var names []string
	for _, d := range data.Devices {
		if metrics.DeviceGone(d, env.Today, cfgFloat(cfg, "days")) {
			names = append(names, d.Name)
		}
	}
	if len(names) == 0 {
		return nil
	}
	return []Finding{svcFinding(tailscaleSvc, "tailscale.offline", "offline", "tailscale.offline", enums.SeverityInfo, data.URL,
		map[string]any{"count": len(names), "names": shortList(names), "days": cfgInt(cfg, "days")})}
}

func wanDown(data *sources.GatewayDataset, _ map[string]any, _ Env) []Finding {
	var found []Finding
	for _, g := range data.Gateways {
		if !g.Up {
			found = append(found, svcFinding(gatewaySvc, "gateway.wan_down", "wan:"+g.Name, "gateway.wan_down", enums.SeverityCritical, data.URL,
				map[string]any{"name": g.Name, "loss": Num(g.Loss, 0)}))
		}
	}
	return found
}

func devicesOffline(data *sources.GatewayDataset, _ map[string]any, _ Env) []Finding {
	var names []string
	for _, d := range data.Devices {
		if !d.Online {
			names = append(names, d.Name)
		}
	}
	if len(names) == 0 {
		return nil
	}
	return []Finding{svcFinding(gatewaySvc, "gateway.devices_offline", "devices", "gateway.devices_offline", enums.SeverityWarn, data.URL,
		map[string]any{"count": len(names), "names": shortList(names)})}
}

func gatewayUpdates(data *sources.GatewayDataset, _ map[string]any, _ Env) []Finding {
	if data.Updates == 0 {
		return nil
	}
	return []Finding{svcFinding(gatewaySvc, "gateway.updates", "updates", "gateway.updates", enums.SeverityInfo, data.URL,
		map[string]any{"count": data.Updates, "version": data.Version})}
}

func registerMediaRules() {
	Register("mediaserver.update", mediaSvc, nil, on(mediaserverUpdate))

	// Known checks get a short translated title (catalog arr_check.<source>);
	// the English message, often with ids, moves to the explanation.
	Register("arr.health", arrSvc, nil, on(arrHealth))
	Register("arr.stuck", arrSvc, nil, on(arrStuck))
}

func mediaserverUpdate(data *sources.MediaServerDataset, _ map[string]any, _ Env) []Finding {
	if !data.Update {
		return nil
	}
	return []Finding{svcFinding(mediaSvc, "mediaserver.update", "update", "mediaserver.update", enums.SeverityInfo, data.URL,
		map[string]any{"version": data.Version})}
}

func arrHealth(data *sources.ArrDataset, _ map[string]any, _ Env) []Finding {
	var found []Finding
	for _, h := range data.Health {
		level := enums.SeverityInfo
		if h.Level == arrError {
			level = enums.SeverityWarn
		}
		msg, params := "arr.health", map[string]any{"app": data.App, "message": h.Message}
		if arrChecks[h.Source] {
			msg, params["check"] = "arr.check", map[string]any{"$t": "arr_check." + h.Source}
		}
		found = append(found, svcFinding(arrSvc, "arr.health", "health:"+h.Message, msg, level, data.URL, params))
	}
	return found
}

func arrStuck(data *sources.ArrDataset, _ map[string]any, _ Env) []Finding {
	if len(data.Stuck) == 0 {
		return nil
	}
	return []Finding{svcFinding(arrSvc, "arr.stuck", "stuck", "arr.stuck", enums.SeverityWarn, strings.TrimRight(data.URL, "/")+"/activity/queue",
		map[string]any{"app": data.App, "count": len(data.Stuck), "names": shortList(append([]string(nil), data.Stuck...))})}
}

func registerEverydayRules() {
	Register("vaultwarden.no_2fa", vaultwardenSvc, nil, on(no2fa))

	Register("speedtest.slow", speedtestSvc, map[string]any{"share": 0.5}, on(speedtestSlow))

	Register("grocy.expired", grocySvc, nil, on(grocyExpired))
	Register("grocy.missing", grocySvc, nil, on(grocyMissing))
	Register("grocy.chores_overdue", grocySvc, nil, on(choresOverdue))

	Register(dwdWarningKey, dwdSvc, nil, on(dwdWarning))

	Register("github.review_waiting", githubSvc, map[string]any{"days": 2.0}, on(githubReviewWaiting))
	Register("github.stale_pr", githubSvc, map[string]any{"days": 14.0}, on(githubStalePr))
	Register("github.ci_failed", githubSvc, nil, on(ciFailed))

	// The store learns nothing of a release: its upload is easy to forget.
	Register(storeBehindID, Cross, map[string]any{"days": 1.0}, storeBehind)
	Needs(storeBehindID, githubSvc, kdestoreSvc)

	Register(costRuleID, tibberSvc, map[string]any{"factor": 1.3}, on(costRising))
}

// productNames lists the products' names.
func productNames(list []sources.Product) []string {
	out := make([]string, 0, len(list))
	for _, p := range list {
		out = append(out, p.Name)
	}
	return out
}

// githubFinding is a finding about one pull request.
func githubFinding(rule string, level enums.Severity, i sources.Issue, days int) Finding {
	return svcFinding(githubSvc, rule, fmt.Sprintf("%s#%d", i.Repo, i.Number), rule, level, i.URL,
		map[string]any{"repo": i.Repo, "number": i.Number, "title": i.Title, "days": days})
}

func no2fa(data *sources.VaultwardenDataset, _ map[string]any, _ Env) []Finding {
	var names []string
	for _, u := range data.Users {
		if metrics.No2FA(u) {
			names = append(names, u.Email)
		}
	}
	if len(names) == 0 {
		return nil
	}
	return []Finding{svcFinding(vaultwardenSvc, "vaultwarden.no_2fa", "no_2fa", "vaultwarden.no_2fa", enums.SeverityWarn,
		strings.TrimRight(data.URL, "/")+"/admin/users/overview", map[string]any{"count": len(names), "names": shortList(names)})}
}

func speedtestSlow(data *sources.SpeedtestDataset, cfg map[string]any, _ Env) []Finding {
	share := cfgFloat(cfg, "share")
	slowDown := data.ExpectDown > 0 && data.Down < data.ExpectDown*share
	slowUp := data.ExpectUp > 0 && data.Up < data.ExpectUp*share
	if data.At.IsZero() || (!slowDown && !slowUp) {
		return nil
	}
	return []Finding{svcFinding(speedtestSvc, "speedtest.slow", "slow", "speedtest.slow", enums.SeverityWarn, data.URL,
		map[string]any{"down": Num(data.Down, 0), "up": Num(data.Up, 0), "expect_down": Num(data.ExpectDown, 0), "expect_up": Num(data.ExpectUp, 0)})}
}

func grocyExpired(data *sources.GrocyDataset, _ map[string]any, _ Env) []Finding {
	past := metrics.GrocyPastDue(data)
	if len(past) == 0 {
		return nil
	}
	return []Finding{svcFinding(grocySvc, "grocy.expired", "expired", "grocy.expired", enums.SeverityWarn, strings.TrimRight(data.URL, "/")+"/stockoverview",
		map[string]any{"count": len(past), "names": shortList(productNames(past))})}
}

func grocyMissing(data *sources.GrocyDataset, _ map[string]any, _ Env) []Finding {
	if len(data.Missing) == 0 {
		return nil
	}
	return []Finding{svcFinding(grocySvc, "grocy.missing", "missing", "grocy.missing", enums.SeverityInfo, strings.TrimRight(data.URL, "/")+"/shoppinglist",
		map[string]any{"count": len(data.Missing), "names": shortList(productNames(data.Missing))})}
}

func choresOverdue(data *sources.GrocyDataset, _ map[string]any, env Env) []Finding {
	var due []string
	for _, c := range metrics.GrocyLateChores(data, env.Today) {
		due = append(due, c.Name)
	}
	if len(due) == 0 {
		return nil
	}
	return []Finding{svcFinding(grocySvc, "grocy.chores_overdue", "chores", "grocy.chores_overdue", enums.SeverityInfo, strings.TrimRight(data.URL, "/")+"/choresoverview",
		map[string]any{"count": len(due), "names": shortList(due)})}
}

func dwdWarning(data *sources.DWDDataset, _ map[string]any, _ Env) []Finding {
	var found []Finding
	for _, w := range data.Warnings {
		level, ok := dwdLevels[metrics.WarningRank(w.Severity)]
		if !ok {
			continue
		}
		found = append(found, Finding{Fingerprint: "warn:" + w.ID, Rule: dwdWarningKey, Severity: level, Message: dwdWarningKey,
			Params: map[string]any{"event": w.Headline, "place": data.Place, "until": Day(w.Expire)}, Sources: []string{dwdSvc}})
	}
	return found
}

func githubReviewWaiting(data *sources.GitHubDataset, cfg map[string]any, env Env) []Finding {
	var found []Finding
	for _, pr := range data.Reviews {
		if days := int(env.Today.Sub(pr.Updated).Hours() / hoursPerDay); days >= cfgInt(cfg, "days") {
			found = append(found, githubFinding("github.review_waiting", enums.SeverityWarn, pr, days))
		}
	}
	return found
}

func githubStalePr(data *sources.GitHubDataset, cfg map[string]any, env Env) []Finding {
	var found []Finding
	for _, pr := range data.MyPRs {
		if days := int(env.Today.Sub(pr.Updated).Hours() / hoursPerDay); days >= cfgInt(cfg, "days") {
			found = append(found, githubFinding("github.stale_pr", enums.SeverityInfo, pr, days))
		}
	}
	return found
}

func ciFailed(data *sources.GitHubDataset, _ map[string]any, _ Env) []Finding {
	var found []Finding
	for _, r := range data.Repos {
		if metrics.RedCI(r) {
			found = append(found, Finding{Fingerprint: "ci:" + r.Name, Severity: enums.SeverityWarn,
				Message: "github.ci_failed", Params: map[string]any{"repo": r.Name}, ActionURL: r.CIURL, ActionLabel: "open_in_github", Sources: []string{githubSvc}})
		}
	}
	return found
}

func costRising(data *sources.TibberDataset, cfg map[string]any, _ Env) []Finding {
	if len(data.Days) < 2*weekDays {
		return nil
	}
	n := len(data.Days)
	recent, _ := metrics.EnergyTotals(data.Days[n-weekDays:])
	before, _ := metrics.EnergyTotals(data.Days[n-2*weekDays : n-weekDays])
	if before <= 0 || recent < before*cfgFloat(cfg, "factor") {
		return nil
	}
	return []Finding{{Fingerprint: "cost", Rule: costRuleID, Severity: enums.SeverityInfo, Message: costRuleID,
		Params: map[string]any{"recent": Money(recent, data.Currency), "before": Money(before, data.Currency)}, Sources: []string{tibberSvc}}}
}

func init() {
	// A device on the network that was never there before: a guest, a
	// new gadget or someone who should not be there.
	Register("gateway.new_device", Cross, map[string]any{"days": 1.0}, gatewayNewDevice)
}

func gatewayNewDevice(_ any, cfg map[string]any, env Env) []Finding {
	var found []Finding
	for _, dev := range metrics.NewLeases(historyOf(env), env.Today, cfgInt(cfg, "days")) {
		found = append(found, Finding{Fingerprint: "device:" + dev.Name, Severity: enums.SeverityInfo, Message: "gateway.new_device",
			Params: map[string]any{"name": dev.Name, "day": Day(dev.First)}, Sources: []string{gatewaySvc}})
	}
	return found
}

// storeBehind finds store entries whose repo released a newer version at
// least days ago. An entry's repo is the one its description links, else
// the one of its name: "Plasmai" is shrippen/Plasmai.
func storeBehind(_ any, cfg map[string]any, env Env) []Finding {
	github, ok1 := env.Datasets[githubSvc].(*sources.GitHubDataset)
	store, ok2 := env.Datasets[kdestoreSvc].(*sources.KDEStoreDataset)
	if !ok1 || !ok2 || github == nil || store == nil {
		return nil
	}
	since := env.Today.AddDate(0, 0, -cfgInt(cfg, "days"))

	var found []Finding
	for _, item := range store.Items {
		repo, ok := storeRepo(item, github.Repos)
		if !ok || metrics.Today(repo.ReleasedAt).After(since) || !newerVersion(repo.Release, item.Version) {
			continue
		}
		found = append(found, Finding{Fingerprint: fmt.Sprintf("store:%d:%s", item.ID, repo.Release), Severity: enums.SeverityWarn,
			Message: storeBehindID, Params: map[string]any{"item": item.Name, "github": repo.Release, "store": item.Version, "day": Day(repo.ReleasedAt)},
			ActionURL: item.URL, ActionLabel: "open_in_kdestore", Sources: []string{kdestoreSvc, githubSvc}})
	}
	return found
}

// storeRepo is the released repo of a store entry, if any.
func storeRepo(item sources.StoreItem, repos []sources.GitRepo) (sources.GitRepo, bool) {
	byName := -1
	for i, r := range repos {
		if r.Release == "" {
			continue
		}
		name := strings.ToLower(r.Name)
		if slices.Contains(item.Repos, name) {
			return r, true
		}
		if byName < 0 && strings.EqualFold(path.Base(name), item.Name) {
			byName = i
		}
	}
	if byName < 0 {
		return sources.GitRepo{}, false
	}
	return repos[byName], true
}
