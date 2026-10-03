package widgets

// Detail dialogs of the network and security tiles.

import (
	"cmp"
	"fmt"
	"sort"
	"strings"
	"time"

	"andon/internal/metrics"
	"andon/internal/rules"
	"andon/internal/sources"
)

const (
	speedDetailDays = 30
	dnsTopDetail    = 10
	inactiveDays    = 90
)

// dnsDetail (AdGuard, Pi-hole): the filter's day, the busiest clients and
// blocked domains, queries per client over the last days.
func dnsDetail(_ DNSConfig, data *sources.DNSFilterDataset, ctx ViewCtx, results map[string]any) DetailView {
	now := todayOf(ctx)
	h := historyOf(results)
	state := Txt("detail.dns.enabled")
	if !data.Enabled {
		state = Txt("detail.dns.disabled")
	}
	body := &DetailBody{
		Side: []Fact{{Label: T("detail.dns.filter"), Value: state, State: stateIf(!data.Enabled, "bad")},
			{Label: T("detail.dns.queries"), Value: Num(float64(data.Queries), 0)}, {Label: T("detail.dns.blocked"), Value: Num(float64(data.Blocked), 0)},
			{Label: T("detail.dns.clients"), Value: data.Clients}},
		Facts: []Kpi{{Value: NumU(data.Percent, 1, "%"), Label: T("detail.dns.blocked_share")}, {Value: Num(float64(data.Queries), 0), Label: T("detail.dns.queries")},
			{Value: data.Clients, Label: T("detail.dns.clients")}},
	}
	if !data.ListsUpdated.IsZero() {
		body.Side = append(body.Side, Fact{Label: T("detail.dns.lists"), Value: agoOf(data.ListsUpdated)})
	}
	var clients [][]Cell
	var series []Series
	for i, c := range firstN(data.TopClients, dnsTopDetail) {
		name := c.Name
		if name == "" {
			name = c.IP
		}
		clients = append(clients, []Cell{{Value: name}, {Value: Num(float64(c.Queries), 0)}, {Value: Num(float64(c.Blocked), 0)}})
		if q := dailySeries(h, metrics.SampleKey("dns", "q", c.IP), now, speedDetailDays); hasValues(q) && len(series) < 4 {
			series = append(series, Series{Values: q, Class: dataClass(i), Label: name})
		}
	}
	var domains [][]Cell
	for _, dm := range firstN(data.TopBlocked, dnsTopDetail) {
		domains = append(domains, []Cell{{Value: dm.Domain}, {Value: Num(float64(dm.Count), 0)}})
	}
	pair := []Block{}
	if len(clients) > 0 {
		pair = append(pair, Block{Kind: BlockTable, Label: T("detail.dns.top_clients"), Data: Table{Head: []Text{T("detail.dns.client"), T("detail.dns.queries"), T("detail.dns.blocked")}, Rows: clients, Num: []int{1, 2}}})
	}
	if len(domains) > 0 {
		pair = append(pair, Block{Kind: BlockTable, Label: T("detail.dns.top_blocked"), Data: Table{Head: []Text{T("detail.dns.domain"), T("detail.dns.blocked")}, Rows: domains, Num: []int{1}}})
	}
	body.Blocks = append(body.Blocks, pairOf(pair)...)
	if len(series) > 0 {
		g := LineGraph(series...)
		g.Lo, g.Ticks = 0, spanTicks(now, speedDetailDays)
		body.Blocks = append(body.Blocks, Block{Kind: BlockGraph, Label: T("detail.dns.per_day"), Data: g})
	}
	body.Blocks = append(body.Blocks, hintsBlock(results)...)
	head := DetailHead{State: "ok", StateKey: "detail.dns.enabled"}
	if !data.Enabled {
		head.State, head.StateKey = "bad", "detail.dns.disabled"
	}
	return DetailView{Head: head, Body: body}
}

// gatewayDetail: the router, its uplinks and devices.
func gatewayDetail(_ GatewayConfig, data *sources.GatewayDataset, _ ViewCtx, results map[string]any) DetailView {
	up := 0
	var links [][]Cell
	for _, g := range data.Gateways {
		state := "bad"
		if g.Up {
			state, up = "ok", up+1
		}
		row := []Cell{{Value: g.Name}, {Value: Txt("detail.gateway.state_" + state), State: state}}
		if data.Kind != "openwrt" {
			row = append(row, Cell{Value: NumU(g.DelayMS, 0, "ms")}, Cell{Value: NumU(g.Loss, 1, "%"), State: stateIf(g.Loss > 0, "warn")})
		}
		links = append(links, row)
	}
	head := []Text{T("detail.gateway.link"), T("detail.state")}
	if data.Kind != "openwrt" {
		head = append(head, T("detail.gateway.delay"), T("detail.gateway.loss"))
	}
	online := 0
	var devs []LitRow
	for _, d := range data.Devices {
		state := "off"
		if d.Online {
			state, online = "ok", online+1
		}
		meta := any("")
		if d.Update {
			meta = Txt("detail.update_available")
		}
		devs = append(devs, LitRow{Name: d.Name, Meta: meta, State: state})
	}
	model := data.Kind
	if data.Model != "" {
		model += " · " + data.Model
	}
	body := &DetailBody{
		Side: []Fact{{Label: T("detail.gateway.router"), Value: model}, {Label: T("detail.gateway.version"), Value: data.Version},
			{Label: T("detail.gateway.updates"), Value: data.Updates, State: stateIf(data.Updates > 0, "warn")}, {Label: T("detail.gateway.clients"), Value: data.Clients}},
		Facts: []Kpi{{Value: fmt.Sprintf("%d / %d", up, len(data.Gateways)), Label: T("detail.gateway.links_up"), Tier: tierIf(up < len(data.Gateways), "red", "green")},
			{Value: data.Clients, Label: T("detail.gateway.clients")}},
		Blocks: []Block{{Kind: BlockTable, Label: T("detail.gateway.links"), Data: Table{Head: head, Rows: links, Num: []int{2, 3}}}},
	}
	if len(data.Devices) > 0 {
		body.Facts = append(body.Facts, Kpi{Value: fmt.Sprintf("%d / %d", online, len(data.Devices)), Label: T("detail.gateway.devices_up")})
		body.Blocks = append(body.Blocks, Block{Kind: BlockRows, Label: T("detail.gateway.devices"), Data: devs})
	}
	if len(data.ClientNames) > 0 {
		body.Blocks = append(body.Blocks, Block{Kind: BlockChips, Label: T("detail.gateway.client_names"), Meta: len(data.ClientNames), Data: data.ClientNames})
	}
	body.Blocks = append(body.Blocks, hintsBlock(results)...)
	h := DetailHead{State: "ok", StateKey: "detail.gateway.all_up"}
	if up < len(data.Gateways) {
		h.State, h.StateKey = "bad", "detail.gateway.link_down"
	}
	return DetailView{Head: h, Body: body}
}

// speedDetail (timeline): the stored daily results against the contract.
func speedDetail(_ SpeedConfig, data *sources.SpeedtestDataset, ctx ViewCtx, results map[string]any) DetailView {
	now := todayOf(ctx)
	h := historyOf(results)
	body := &DetailBody{Line: []Fact{{Label: T("detail.speed.last"), Value: agoOf(data.At)}}}
	if data.ExpectDown > 0 {
		body.Line = append(body.Line, Fact{Label: T("detail.speed.contract"), Value: fmt.Sprintf("%.0f / %.0f Mbit/s", data.ExpectDown, data.ExpectUp)})
	}
	down := dailySeries(h, metrics.SampleKey("speedtest", "down"), now, speedDetailDays)
	upS := dailySeries(h, metrics.SampleKey("speedtest", "up"), now, speedDetailDays)
	if hasValues(down) {
		g := LineGraph(Series{Values: down, Class: "s1", Label: "↓"}, Series{Values: upS, Class: "s2", Label: "↑"})
		g.Lo, g.Ticks = 0, spanTicks(now, speedDetailDays)
		if data.ExpectDown > 0 {
			g.Goal, g.HasGoal = data.ExpectDown, true
		}
		body.Blocks = append(body.Blocks, Block{Kind: BlockGraph, Label: T("detail.speed.history"), Hero: true, Data: g})
	}
	body.Facts = []Kpi{{Value: NumU(data.Down, 0, "Mbit/s"), Label: T("detail.speed.down"), Tier: speedTier(data.Down, data.ExpectDown, contractShare(ctx))},
		{Value: NumU(data.Up, 0, "Mbit/s"), Label: T("detail.speed.up"), Tier: speedTier(data.Up, data.ExpectUp, contractShare(ctx))},
		{Value: NumU(data.Ping, 0, "ms"), Label: T("detail.speed.ping")}}
	if data.Jitter > 0 {
		body.Facts = append(body.Facts, Kpi{Value: NumU(data.Jitter, 1, "ms"), Label: T("detail.speed.jitter")})
	}
	if data.ExpectDown > 0 {
		body.Facts = append(body.Facts, Kpi{Value: NumU(data.Down/data.ExpectDown*percentScale, 0, "%"), Label: T("detail.speed.of_contract")})
	}
	body.Blocks = append(body.Blocks, hintsBlock(results)...)
	return DetailView{Body: body}
}

func speedTier(got, want, share float64) string {
	if want <= 0 {
		return ""
	}
	return tierIf(metrics.BelowContract(got, want, share), "yellow", "green")
}

// speedHistoryDetail (grid): every stored day as a column, slow days marked.
func speedHistoryDetail(cfg SpeedHistoryConfig, results map[string]any, ctx ViewCtx) DetailView {
	days := speedDays
	if cfg.Days > 0 {
		days = cfg.Days
	}
	now := todayOf(ctx)
	h := historyOf(results)
	data, _ := results["data"].(*sources.SpeedtestDataset)
	down := dailySeries(h, metrics.SampleKey("speedtest", "down"), now, days)
	expect := 0.0
	if data != nil {
		expect = data.ExpectDown
	}
	states := make([]string, len(down))
	slow, sum, n, worst := 0, 0.0, 0, 0.0
	for i, v := range down {
		if v != v {
			continue
		}
		sum, n = sum+v, n+1
		if worst == 0 || v < worst {
			worst = v
		}
		if metrics.BelowContract(v, expect, contractShare(ctx)) {
			states[i], slow = "warn", slow+1
		}
	}
	body := &DetailBody{}
	if n > 0 {
		body.Facts = []Kpi{{Value: NumU(sum/float64(n), 0, "Mbit/s"), Label: T("detail.speed.mean")}, {Value: NumU(worst, 0, "Mbit/s"), Label: T("detail.speed.worst")},
			{Value: slow, Label: T("detail.speed.slow_days"), Tier: tierIf(slow > 0, "yellow", "green")}}
		g := ColGraph(down, "s1")
		g.States, g.Ticks = states, spanTicks(now, days)
		if expect > 0 {
			g.Goal, g.HasGoal = expect, true
		}
		body.Blocks = append(body.Blocks, Block{Kind: BlockGraph, Label: T("detail.speed.per_day"), Hero: true, Data: g})
	} else {
		body.Blocks = append(body.Blocks, Block{Kind: BlockText, Data: Txt("detail.speed.none")})
	}
	return DetailView{Body: body}
}

// tailscaleDetail (grid): every device with its state, key and tags.
func tailscaleDetail(cfg TailscaleConfig, data *sources.TailscaleDataset, ctx ViewCtx, results map[string]any) DetailView {
	now := time.Now()
	warn := rules.Setting(ctx.Settings, "tailscale.key_expiry", "warn_days")
	online, expiring, updates := 0, 0, 0
	var cards []LitRow
	var rows [][]Cell
	for _, d := range data.Devices {
		state := "off"
		if d.Online {
			state, online = "ok", online+1
		}
		key := any(Txt("detail.tailscale.never"))
		keyState := ""
		if !d.KeyExpiry.IsZero() {
			key = Day(d.KeyExpiry)
			if metrics.KeyExpiring(d, now, warn) {
				keyState, expiring = "warn", expiring+1
				if state == "ok" {
					state = "warn"
				}
			}
		}
		if d.Update {
			updates++
		}
		cards = append(cards, LitRow{Name: d.Name, Meta: agoOf(d.LastSeen), State: state})
		rows = append(rows, []Cell{{Value: d.Name}, {Value: agoOf(d.LastSeen), State: stateIf(!d.Online, "bad")}, {Value: key, State: keyState}, {Value: strings.Join(d.Tags, ", ")}})
	}
	sort.SliceStable(cards, func(a, b int) bool { return stateRank(cards[a].State) < stateRank(cards[b].State) })
	body := &DetailBody{Facts: []Kpi{{Value: fmt.Sprintf("%d / %d", online, len(data.Devices)), Label: T("detail.tailscale.online"), Tier: tierIf(online < len(data.Devices), "yellow", "green")},
		{Value: expiring, Label: T("detail.tailscale.keys"), Tier: tierIf(expiring > 0, "yellow", "")}, {Value: updates, Label: T("detail.tailscale.updates"), Tier: tierIf(updates > 0, "cyan", "")}},
		Blocks: []Block{{Kind: BlockStatus, Label: T("detail.tailscale.devices"), Data: cards},
			{Kind: BlockTable, Data: Table{Head: []Text{T("detail.tailscale.device"), T("detail.tailscale.seen"), T("detail.tailscale.key"), T("detail.tailscale.tags")}, Rows: rows}}}}
	body.Blocks = append(body.Blocks, hintsBlock(results)...)
	sub := "Tailscale"
	if data.Headscale {
		sub = "Headscale"
	}
	return DetailView{Head: DetailHead{Sub: sub}, Body: body}
}

// vpnDetail: the tunnel, exit and own address, the leak check.
func vpnDetail(cfg VPNConfig, raw *sources.GluetunDataset, ctx ViewCtx, results map[string]any) DetailView {
	view := vpnView(cfg, raw, ctx)
	data, _ := view["Data"].(*sources.GluetunDataset)
	up, _ := view["Up"].(bool)
	leak, _ := view["Leak"].(bool)
	wrong, _ := view["WrongCountry"].(bool)
	body := &DetailBody{
		Side: []Fact{{Label: T("detail.vpn.status"), Value: data.Status, State: stateIf(!up, "bad")}, {Label: T("detail.vpn.exit"), Value: data.ExitIP},
			{Label: T("detail.vpn.country"), Value: data.Country, State: stateIf(wrong, "warn")}, {Label: T("detail.vpn.own"), Value: data.OwnIP}},
		Facts: []Kpi{{Value: data.Country, Label: T("detail.vpn.country"), Tier: tierIf(wrong, "yellow", "green")},
			{Value: Txt(map[bool]string{true: "detail.vpn.leak", false: "detail.vpn.no_leak"}[leak]), Label: T("detail.vpn.leak_check"), Tier: tierIf(leak, "red", "green")}},
	}
	if data.ExpectedCountry != "" {
		body.Side = append(body.Side, Fact{Label: T("detail.vpn.expected"), Value: data.ExpectedCountry})
	}
	body.Blocks = append(body.Blocks, hintsBlock(results)...)
	head := DetailHead{State: "ok", StateKey: "detail.vpn.connected"}
	switch {
	case !up || leak:
		head.State, head.StateKey = "bad", "detail.vpn.broken"
	case wrong:
		head.State, head.StateKey = "warn", "detail.vpn.wrong_country"
	}
	return DetailView{Head: head, Body: body}
}

// publicIPDetail (timeline): both addresses and the last change.
func publicIPDetail(cfg PublicIPConfig, results map[string]any, ctx ViewCtx) DetailView {
	ip, ok := results["ip"].(*sources.PublicIPResult)
	if !ok {
		return DetailView{Body: &DetailBody{}}
	}
	body := &DetailBody{Line: []Fact{{Label: T("detail.ip.v4"), Value: ip.IP}}}
	if ip.IPv6 != "" {
		body.Line = append(body.Line, Fact{Label: T("detail.ip.v6"), Value: ip.IPv6})
	}
	if seen, ok := results[IPSeenSlot].(IPSeen); ok && seen.Prev != "" {
		body.Blocks = append(body.Blocks, Block{Kind: BlockTimeline, Label: T("detail.ip.changes"), Hero: true,
			Data: []Event{{At: seen.Since, Title: seen.Prev + " → " + seen.IP, State: agoOf(seen.Since), Tier: "yellow"}}})
	} else {
		body.Blocks = append(body.Blocks, Block{Kind: BlockText, Data: Txt("detail.ip.no_change")})
	}
	return DetailView{Body: body}
}

// authentikDetail (tabs): logins, failures, applications, accounts.
func authentikDetail(cfg AuthentikConfig, data *sources.AuthentikDataset, ctx ViewCtx, results map[string]any) DetailView {
	loginRows := func(list []sources.AKLogin) [][]Cell {
		var rows [][]Cell
		for _, l := range list {
			place := strings.Trim(l.City+", "+l.Country, ", ")
			rows = append(rows, []Cell{{Value: agoOf(l.At)}, {Value: l.User}, {Value: place}, {Value: l.IP}})
		}
		return rows
	}
	head := []Text{T("detail.authentik.when"), T("detail.authentik.user"), T("detail.authentik.place"), T("detail.authentik.ip")}
	apps := []ShareBar{}
	most := 1
	for _, a := range data.Apps {
		most = max(most, a.Events)
	}
	for _, a := range data.Apps {
		apps = append(apps, ShareBar{Name: a.Name, Pct: float64(a.Events) * percentScale / float64(most), Value: a.Events})
	}
	staleDays := rules.Setting(ctx.Settings, "authentik.stale_users", "days")
	var users [][]Cell
	for _, u := range data.Users {
		users = append(users, []Cell{{Value: u.Name}, {Value: agoOf(u.LastLogin), State: stateIf(metrics.StaleUser(u, time.Now(), staleDays), "warn")}})
	}
	version := data.Version
	if data.Outdated && data.Latest != "" {
		version += " → " + data.Latest
	}
	body := &DetailBody{Facts: []Kpi{{Value: data.Logins7d, Label: T("detail.authentik.logins7")}, {Value: data.Failed7d, Label: T("detail.authentik.failed7"), Tier: tierIf(data.Failed7d > 0, "yellow", "")},
		{Value: data.Logins24h, Label: T("detail.authentik.logins24")}, {Value: version, Label: T("detail.authentik.version"), Tier: tierIf(data.Outdated, "yellow", "")}}}
	overview := []Block{{Kind: BlockTable, Label: T("detail.authentik.latest"), Data: Table{Head: head, Rows: loginRows(data.Logins)}}}
	overview = append(overview, hintsBlock(results)...)
	body.Tabs = []Tab{
		{Label: T("detail.authentik.logins"), Blocks: overview},
		{Label: T("detail.authentik.failures"), Count: len(data.Failures), Blocks: []Block{{Kind: BlockTable, Data: Table{Head: head, Rows: loginRows(data.Failures)}}}},
		{Label: T("detail.authentik.apps"), Count: len(data.Apps), Blocks: []Block{{Kind: BlockBars, Data: apps}}},
		{Label: T("detail.authentik.accounts"), Count: len(data.Users), Blocks: []Block{{Kind: BlockTable, Data: Table{Head: []Text{T("detail.authentik.user"), T("detail.authentik.last_login")}, Rows: users}}}},
	}
	h := DetailHead{State: "ok", StateKey: "detail.authentik.current"}
	if data.Outdated || data.Outposts {
		h.State, h.StateKey = "warn", "detail.update_available"
	}
	return DetailView{Head: h, Body: body}
}

// expiryDetail (timeline): certificates and domains by days left.
func expiryDetail(cfg ExpiryConfig, results map[string]any, ctx ViewCtx) DetailView {
	today := todayOf(ctx)
	var events []Event
	var doms [][]Cell
	tier := func(left int, failed string, warn, info float64) string {
		if failed != "" {
			return "red"
		}
		return cmp.Or(dueTier(left, warn, info), "cyan")
	}
	if certs, ok := results["data"].(*sources.CertDataset); ok && cfg.Kinds != "domains" {
		warn, info := expiryLimits(ctx.Settings, "certs.expiring")
		for _, c := range certs.Certs {
			left := int(c.NotAfter.Sub(today).Hours() / hoursPerDay)
			sub := any(c.Issuer)
			if c.Error != "" {
				sub = c.Error
			}
			events = append(events, Event{At: c.NotAfter, Title: c.Host, Sub: sub, State: TxtA("detail.days", "n", left), Tier: tier(left, c.Error, warn, info)})
		}
	}
	if d, ok := results[peerDomains].(*sources.DomainsDataset); ok && cfg.Kinds != "certs" {
		warn, info := expiryLimits(ctx.Settings, "domains.expiring")
		for _, dm := range d.Domains {
			left := int(dm.Expires.Sub(today).Hours() / hoursPerDay)
			if !dm.Expires.IsZero() {
				events = append(events, Event{At: dm.Expires, Title: dm.Name, Sub: Txt("detail.expiry.domain"), State: TxtA("detail.days", "n", left), Tier: tier(left, dm.Error, warn, info)})
			}
			mail := func(ok bool) Cell {
				if !dm.MailChecked {
					return Cell{Value: "–"}
				}
				return Cell{Value: Txt(map[bool]string{true: "detail.answer_yes", false: "detail.answer_no"}[ok]), State: map[bool]string{true: "ok", false: "bad"}[ok]}
			}
			doms = append(doms, []Cell{{Value: dm.Name}, mail(dm.SPF), mail(dm.DMARC), {Value: dayOf(dm.Expires)}})
		}
	}
	sort.SliceStable(events, func(a, b int) bool { return events[a].At.Before(events[b].At) })
	body := &DetailBody{}
	if len(events) > 0 {
		next := int(events[0].At.Sub(today).Hours() / hoursPerDay)
		body.Line = []Fact{{Label: T("detail.expiry.next"), Value: events[0].Title}, {Label: T("detail.expiry.in"), Value: TxtA("detail.days", "n", next)}}
	}
	body.Blocks = append(body.Blocks, Block{Kind: BlockTimeline, Label: T("detail.expiry.list"), Hero: true, Data: events})
	if len(doms) > 0 {
		body.Blocks = append(body.Blocks, Block{Kind: BlockTable, Label: T("detail.expiry.domains"), Data: Table{Head: []Text{T("detail.expiry.domain"), T("detail.expiry.spf"), T("detail.expiry.dmarc"), T("detail.expiry.until")}, Rows: doms}})
	}
	body.Blocks = append(body.Blocks, hintsBlock(results)...)
	return DetailView{Body: body}
}

// exposureDetail (list and detail): public resources by risk.
func exposureDetail(cfg ExposureConfig, results map[string]any, ctx ViewCtx) DetailView {
	cfg.OnlyOpen = false
	view := exposureView(cfg, results, ctx)
	rows, _ := view["Rows"].([]ExposedRow)
	data, _ := results["data"].(*sources.PangolinDataset)
	list := &ObjList{Label: T("detail.exposure.resources")}
	riskState := []string{"ok", "warn", "bad"}
	for _, r := range rows {
		list.Items = append(list.Items, LitRow{Name: r.Name, Meta: Txt("detail.exposure.risk_" + riskState[min(r.Risk, 2)]), State: riskState[min(r.Risk, 2)]})
	}
	body := &DetailBody{List: list}
	if len(rows) > 0 {
		r := rows[0]
		list.Title, list.Sub, list.State, list.StateText = r.Name, r.Domain, riskState[min(r.Risk, 2)], T("detail.exposure.risk_"+riskState[min(r.Risk, 2)])
		cert := any("–")
		if r.CertDays >= 0 {
			cert = TxtA("detail.days", "n", r.CertDays)
		}
		login := Cell{Value: Txt("detail.answer_no"), State: "bad"}
		if r.Login {
			login = Cell{Value: Txt("detail.answer_yes"), State: "ok"}
		}
		body.Blocks = append(body.Blocks, Block{Kind: BlockTable, Data: Table{Head: []Text{T("detail.exposure.what"), T("detail.exposure.value")},
			Rows: [][]Cell{{{Value: Txt("detail.exposure.login")}, login}, {{Value: Txt("detail.exposure.updates")}, {Value: r.Updates, State: stateIf(r.Updates > 0, "warn")}},
				{{Value: Txt("detail.exposure.cert")}, {Value: cert}}}}})
	}
	if data != nil {
		var sites []LitRow
		for _, s := range data.Sites {
			state := "ok"
			if s.Online != nil && !*s.Online {
				state = "bad"
			}
			sites = append(sites, LitRow{Name: s.Name + " · " + s.Type, Meta: fmt.Sprintf("↓ %.0f MB · ↑ %.0f MB", s.MBIn, s.MBOut), State: state})
		}
		body.Blocks = append(body.Blocks, Block{Kind: BlockRows, Label: T("detail.exposure.sites"), Data: sites})
	}
	body.Blocks = append(body.Blocks, hintsBlock(results)...)
	open, _ := view["Open"].(int)
	h := DetailHead{State: "ok", StateKey: "detail.exposure.all_login"}
	if open > 0 {
		h.State, h.StateKey, h.StateArgs = "bad", "detail.exposure.n_open", map[string]any{"n": open}
	}
	return DetailView{Head: h, Body: body}
}

// vaultDetail (tasks): accounts without a second factor, the update.
func vaultDetail(cfg VaultConfig, data *sources.VaultwardenDataset, _ ViewCtx, results map[string]any) DetailView {
	stale := time.Now().AddDate(0, 0, -inactiveDays)
	tasks := Tasks{Label: T("detail.vault.with_2fa")}
	var done []string
	for _, u := range data.Users {
		if !u.Enabled {
			continue
		}
		tasks.Total++
		if u.TwoFactor {
			tasks.Done++
			done = append(done, u.Email)
			continue
		}
		meta := agoOf(u.LastActive)
		item := Task{Text: TxtA("detail.vault.set_up", "who", u.Email), Meta: meta, State: "warn"}
		if !u.LastActive.IsZero() && u.LastActive.Before(stale) {
			item.Text = TxtA("detail.vault.check", "who", u.Email)
		}
		tasks.Items = append(tasks.Items, item)
	}
	if len(done) > 0 {
		tasks.Items = append(tasks.Items, Task{Text: strings.Join(done, ", "), Meta: Txt("detail.vault.have_2fa"), State: "ok"})
	}
	body := &DetailBody{Blocks: []Block{{Kind: BlockTasks, Data: tasks}}}
	body.Blocks = append(body.Blocks, hintsBlock(results)...)
	return DetailView{Head: DetailHead{Sub: "Vaultwarden " + data.Version}, Body: body}
}
