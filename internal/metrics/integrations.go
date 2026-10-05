package metrics

// Info lines and views of the network, media and everyday integrations.

import (
	"strings"
	"time"

	"andon/internal/enums"
	"andon/internal/sources"
)

// CheapHours is the default window length for flexible loads.
const CheapHours = 3

// TailscaleInfo: "5/7 online".
func TailscaleInfo(data *sources.TailscaleDataset) []InfoPart {
	online := 0
	for _, d := range data.Devices {
		if d.Online {
			online++
		}
	}
	return []InfoPart{part("tailscale.online", map[string]any{"online": online, "count": len(data.Devices)})}
}

// GatewayInfo: "WAN ok" or "WAN down", plus pending updates.
func GatewayInfo(data *sources.GatewayDataset) []InfoPart {
	var found []InfoPart
	if len(data.Gateways) > 0 {
		key := "gateway.wan_ok"
		for _, g := range data.Gateways {
			if !g.Up {
				key = "gateway.wan_down"
			}
		}
		found = append(found, part(key, nil))
	}
	if len(data.Devices) > 0 {
		found = append(found, part("gateway.devices", map[string]any{"count": len(data.Devices)}))
	}
	if data.Updates > 0 {
		found = append(found, part("gateway.updates", map[string]any{"count": data.Updates}))
	}
	return found
}

// MediaServerInfo: "2 streams · 1204 movies".
func MediaServerInfo(data *sources.MediaServerDataset) []InfoPart {
	return []InfoPart{
		part("mediaserver.streams", map[string]any{"count": len(data.Streams)}),
		part("mediaserver.movies", map[string]any{"count": data.Movies}),
	}
}

// ArrInfo: "3 in queue · 12 missing".
func ArrInfo(data *sources.ArrDataset) []InfoPart {
	return []InfoPart{
		part("arr.queue", map[string]any{"count": data.Queue}),
		part("arr.missing", map[string]any{"count": data.Missing}),
	}
}

// VaultwardenInfo: "4 users · 1 without 2FA".
func VaultwardenInfo(data *sources.VaultwardenDataset) []InfoPart {
	without := 0
	for _, u := range data.Users {
		if No2FA(u) {
			without++
		}
	}
	found := []InfoPart{part("vaultwarden.users", map[string]any{"count": len(data.Users)})}
	if without > 0 {
		found = append(found, part("vaultwarden.no_2fa", map[string]any{"count": without}))
	}
	return found
}

// SpeedtestInfo: "↓ 243 ↑ 41 Mbit/s · 12 ms".
func SpeedtestInfo(data *sources.SpeedtestDataset) []InfoPart {
	return []InfoPart{part("speedtest.result", map[string]any{"down": int(data.Down + 0.5), "up": int(data.Up + 0.5), "ping": int(data.Ping + 0.5)})}
}

// GrocyInfo: "1 expired · 2 due soon · 1 missing".
func GrocyInfo(data *sources.GrocyDataset) []InfoPart {
	var found []InfoPart
	if n := len(GrocyPastDue(data)); n > 0 {
		found = append(found, part("grocy.expired", map[string]any{"count": n}))
	}
	soon := data.SoonWithin(sources.GrocySoonDays, time.Now())
	found = append(found, part("grocy.soon", map[string]any{"count": len(soon)}))
	if len(data.Missing) > 0 {
		found = append(found, part("grocy.missing", map[string]any{"count": len(data.Missing)}))
	}
	return found
}

// DWDInfo: "no warnings" or "2 warnings".
func DWDInfo(data *sources.DWDDataset) []InfoPart {
	if len(data.Warnings) == 0 {
		return []InfoPart{part("dwd.none", nil)}
	}
	return []InfoPart{part("dwd.count", map[string]any{"count": len(data.Warnings)})}
}

// GitHubInfo: "3 PRs · 1 CI failed".
func GitHubInfo(data *sources.GitHubDataset) []InfoPart {
	prs, failed := 0, 0
	for _, r := range data.Repos {
		prs += r.PRs
		if RedCI(r) {
			failed++
		}
	}
	found := []InfoPart{part("github.prs", map[string]any{"count": prs})}
	if failed > 0 {
		found = append(found, part("github.ci_failed", map[string]any{"count": failed}))
	}
	return found
}

// TibberInfo: "0,31 € / kWh".
func TibberInfo(data *sources.TibberDataset) []InfoPart {
	return []InfoPart{part("tibber.price", map[string]any{"price": map[string]any{"$money": data.Current, "currency": data.Currency}})}
}

// CheapWindow finds the cheapest run of `hours` consecutive prices from
// now on; ok is false with too few prices left.
func CheapWindow(prices []sources.PricePoint, now time.Time, hours int) (start time.Time, avg float64, ok bool) {
	var ahead []sources.PricePoint
	for _, p := range prices {
		if !p.At.Add(time.Hour).Before(now) {
			ahead = append(ahead, p)
		}
	}
	best := -1.0
	for i := 0; i+hours <= len(ahead); i++ {
		sum := 0.0
		for _, p := range ahead[i : i+hours] {
			sum += p.Total
		}
		if best < 0 || sum < best {
			best, start = sum, ahead[i].At
		}
	}
	if best < 0 {
		return time.Time{}, 0, false
	}
	return start, best / float64(hours), true
}

// GrocyPastDue are the products past their date: expired (use-by) and
// overdue (best-before), as Grocy's stock overview marks both.
func GrocyPastDue(d *sources.GrocyDataset) []sources.Product {
	return append(append([]sources.Product(nil), d.Expired...), d.Overdue...)
}

// NotOnList are the products Grocy misses (below minimum stock) that are
// not on Tandoor's shopping list, matched by name ignoring case.
func NotOnList(grocy *sources.GrocyDataset, tandoor *sources.TandoorDataset) []sources.Product {
	listed := map[string]bool{}
	for _, it := range tandoor.Items {
		listed[strings.ToLower(strings.TrimSpace(it.Food))] = true
	}
	var out []sources.Product
	for _, p := range grocy.Missing {
		if !listed[strings.ToLower(strings.TrimSpace(p.Name))] {
			out = append(out, p)
		}
	}
	return out
}

// GrocyLateChores are the chores due before today.
func GrocyLateChores(d *sources.GrocyDataset, today time.Time) []sources.Chore {
	var out []sources.Chore
	for _, c := range d.Chores {
		if c.Due.Before(today) {
			out = append(out, c)
		}
	}
	return out
}

// No2FA tells an active account without a second factor.
func No2FA(u sources.VaultUser) bool { return u.Enabled && !u.TwoFactor }

// RedCI tells a repo whose latest run on the default branch failed.
func RedCI(r sources.GitRepo) bool { return r.CI == ciFailure }

// ciFailure is GitHub's conclusion of a failed run.
const ciFailure = "failure"

// DeviceGone tells a device unseen for more than days; it no longer
// counts for key or update checks.
func DeviceGone(d sources.TailDevice, today time.Time, days float64) bool {
	return !d.Online && !d.LastSeen.IsZero() && today.Sub(d.LastSeen).Hours()/hoursPerDay > days
}

// KeyExpiring tells a device key that runs out within days (or already has).
func KeyExpiring(d sources.TailDevice, today time.Time, days float64) bool {
	return !d.KeyExpiry.IsZero() && d.KeyExpiry.Sub(today).Hours()/hoursPerDay <= days
}

// StaleUser tells an account without a login for more than days (or never).
func StaleUser(u sources.AKUser, today time.Time, days float64) bool {
	return u.LastLogin.IsZero() || today.Sub(u.LastLogin).Hours()/hoursPerDay > days
}

// FeedSilent tells a feed without a new entry for more than days.
func FeedSilent(f sources.Feed, today time.Time, days float64) bool {
	return !f.Newest.IsZero() && today.Sub(f.Newest).Hours()/hoursPerDay > days
}

// EnergyTotals adds up the cost and consumption of days.
func EnergyTotals(days []sources.EnergyDay) (cost, kwh float64) {
	for _, d := range days {
		cost, kwh = cost+d.Cost, kwh+d.KWh
	}
	return cost, kwh
}

// WarningRank orders weather warning severities: minor 1 … extreme 4,
// 0 for an unknown one.
func WarningRank(severity string) int {
	return map[string]int{sources.WarnMinor: 1, sources.WarnModerate: 2, sources.WarnSevere: 3, sources.WarnExtreme: 4}[strings.ToLower(severity)]
}

// DownloadsKey is an item's series of download totals:
// ("github", "studio/website") → "github.downloads.studio/website".
func DownloadsKey(service enums.ServiceType, id string) string {
	return key(string(service), "downloads", id)
}

// DailyGains turns day totals into what each day added, oldest first.
// The first total adds nothing it can be measured against; a drop
// (deleted assets) counts as 0:
//
//	totals 100, 140, 135, 160 → gains 40, 0, 25
func DailyGains(totals []Point) []Point {
	var out []Point
	for i := 1; i < len(totals); i++ {
		out = append(out, Point{Day: totals[i].Day, Value: max(totals[i].Value-totals[i-1].Value, 0)})
	}
	return out
}

// GitHub and the KDE Store record each item's download total once a
// day: they only count, the history makes the trend. Earlier totals
// (demo only) fill their own days.
func init() {
	Record(func(d *sources.GitHubDataset, _ time.Time, r *Readings) {
		recordDownloads(enums.ServiceGitHub, d.Downloads(), r)
	})
	Record(func(d *sources.KDEStoreDataset, _ time.Time, r *Readings) {
		recordDownloads(enums.ServiceKDEStore, d.Downloads(), r)
	})
}

func recordDownloads(service enums.ServiceType, items []sources.DownloadItem, r *Readings) {
	for _, it := range items {
		for _, past := range it.Days {
			r.SetOn(past.Day, DownloadsKey(service, it.ID), float64(past.Total))
		}
		if it.Total > 0 {
			r.Set(DownloadsKey(service, it.ID), float64(it.Total))
		}
	}
}
