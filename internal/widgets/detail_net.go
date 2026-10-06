package widgets

// Detail dialogs of the network and security tiles.

import (
	"cmp"
	"fmt"
	"maps"
	"slices"
	"sort"
	"strings"
	"time"

	"andon/internal/enums"
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
	if len(data.Hourly) >= minPoints {
		total, blocked := make([]float64, len(data.Hourly)), make([]float64, len(data.Hourly))
		for i, n := range data.Hourly {
			total[i] = float64(n)
			if i < len(data.HourlyBlocked) {
				blocked[i] = float64(data.HourlyBlocked[i])
			}
		}
		g := LineGraph(Series{Values: total, Class: "s1", Label: Txt("detail.dns.queries")}, Series{Values: blocked, Class: "s2", Label: Txt("detail.dns.blocked")})
		g.Lo, g.Ticks = 0, []any{TxtA("detail.hours_ago", "n", len(total)), Txt("detail.now")}
		body.Blocks = append(body.Blocks, Block{Kind: BlockGraph, Label: T("detail.dns.per_hour"), Hero: true, Data: g})
	}
	if data.Enabled {
		body.Blocks = append(body.Blocks, Block{Kind: BlockTasks, Data: Tasks{Label: T("detail.dns.pause_label"), Items: []Task{{
			Text: Txt("detail.dns.pause_what"), Meta: Txt("detail.dns.pause_back"), State: "info", Action: T("detail.dns.pause"), Do: "pause"}}}})
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
func gatewayDetail(_ GatewayConfig, data *sources.GatewayDataset, ctx ViewCtx, results map[string]any) DetailView {
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
	hist, now := historyOf(results), todayOf(ctx)
	var series []Series
	for i, g := range data.Gateways {
		if ms := dailySeries(hist, metrics.SampleKey("gateway", "ms", g.Name), now, linkDays); hasValues(ms) {
			series = append(series, Series{Values: ms, Class: dataClass(i), Label: g.Name})
		}
	}
	if len(series) > 0 {
		g := LineGraph(series...)
		g.Lo, g.Ticks = 0, spanTicks(now, linkDays)
		body.Blocks = append(body.Blocks, Block{Kind: BlockGraph, Label: T("detail.gateway.delay_days"), Meta: "ms", Data: g})
	}
	if fresh := metrics.NewLeases(hist, now, newLeaseDays); len(fresh) > 0 {
		var rows []LitRow
		for _, dev := range fresh {
			rows = append(rows, LitRow{Name: dev.Name, Meta: Day(dev.First), State: "info"})
		}
		body.Blocks = append(body.Blocks, Block{Kind: BlockRows, Label: textArgs("detail.gateway.new_devices", "n", newLeaseDays), Data: rows})
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
		g.Lo, g.Ticks, g.Unit = 0, spanTicks(now, speedDetailDays), "Mbit/s"
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
	if more, ok := results[openName].(*sources.SpeedResults); ok && len(more.List) > 0 {
		body.Blocks = append(body.Blocks, speedResultBlocks(more.List, data, contractShare(ctx), todayOf(ctx))...)
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
		g.States, g.Ticks, g.Unit = states, spanTicks(now, days), "Mbit/s"
		if expect > 0 {
			g.Goal, g.HasGoal = expect, true
		}
		body.Blocks = append(body.Blocks, Block{Kind: BlockGraph, Label: T("detail.speed.per_day"), Hero: true, Data: g})
	} else {
		body.Blocks = append(body.Blocks, Block{Kind: BlockText, Data: Txt("detail.speed.none")})
	}
	if more, ok := results[openName].(*sources.SpeedResults); ok && expect > 0 {
		if heat, found := speedHourHeat(more.List, expect); found {
			body.Blocks = append(body.Blocks, Block{Kind: BlockHeat, Label: T("detail.speed.by_hour"), Meta: Txt("detail.speed.by_hour_scale"), Data: heat})
		}
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
	var strips []Strip
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
		client := any("–")
		if d.Client != "" {
			client = strings.TrimSpace(d.OS + " " + d.Client)
		}
		rows = append(rows, []Cell{{Value: d.Name}, {Value: agoOf(d.LastSeen), State: stateIf(!d.Online, "bad")}, {Value: key, State: keyState},
			{Value: client, State: stateIf(d.Update, "warn")}, {Value: strings.Join(d.Tags, ", ")}})
		strip := Strip{Name: d.Name}
		for _, share := range metrics.OnlineDays(historyOf(results), "tailscale", d.Name, now, uptimeDetailDays) {
			strip.States = append(strip.States, shareState(share))
		}
		strips = append(strips, strip)
	}
	sort.SliceStable(cards, func(a, b int) bool { return stateRank(cards[a].State) < stateRank(cards[b].State) })
	body := &DetailBody{Facts: []Kpi{{Value: fmt.Sprintf("%d / %d", online, len(data.Devices)), Label: T("detail.tailscale.online"), Tier: tierIf(online < len(data.Devices), "yellow", "green")},
		{Value: expiring, Label: T("detail.tailscale.keys"), Tier: tierIf(expiring > 0, "yellow", "")}, {Value: updates, Label: T("detail.tailscale.updates"), Tier: tierIf(updates > 0, "cyan", "")}},
		Blocks: []Block{{Kind: BlockStatus, Label: T("detail.tailscale.devices"), Data: cards},
			{Kind: BlockStrips, Label: T("detail.tailscale.online_days"), Ticks: spanTicks(now, uptimeDetailDays), Data: strips},
			{Kind: BlockTable, Label: T("detail.tailscale.devices"), Data: Table{Head: []Text{T("detail.tailscale.device"), T("detail.tailscale.seen"), T("detail.tailscale.key"), T("detail.tailscale.client"), T("detail.tailscale.tags")}, Rows: rows}}}}
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
	port := any(Txt("detail.vpn.no_port"))
	if data.Port > 0 {
		port = data.Port
	}
	body.Side = append(body.Side, Fact{Label: T("detail.vpn.port"), Value: port})
	var changes []Event
	if h := historyOf(results); h != nil {
		for _, e := range h.Events {
			if e.Kind == metrics.EventChange && (e.Subject == metrics.SubjectVPN || e.Subject == metrics.SubjectVPNExit) {
				changes = append(changes, Event{At: e.At, Title: e.Subject, Sub: e.Detail, State: agoOf(e.At), Tier: tierIf(e.Subject == metrics.SubjectVPN, "yellow", "cyan")})
			}
		}
	}
	if len(changes) > 0 {
		body.Blocks = append(body.Blocks, Block{Kind: BlockTimeline, Label: T("detail.vpn.changes"), Data: firstN(changes, ipChangesShown)})
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
	var changes []Event
	if h := historyOf(results); h != nil {
		for _, e := range h.Events {
			if e.Kind == metrics.EventChange && e.Subject == metrics.SubjectIP {
				changes = append(changes, Event{At: e.At, Title: e.Detail, State: agoOf(e.At), Tier: "yellow"})
			}
		}
	}
	if len(changes) == 0 {
		if seen, ok := results[IPSeenSlot].(IPSeen); ok && seen.Prev != "" {
			changes = []Event{{At: seen.Since, Title: seen.Prev + " → " + seen.IP, State: agoOf(seen.Since), Tier: "yellow"}}
		}
	}
	if len(changes) >= minPoints {
		// Newest first: the mean gap is the line's forced reconnect rhythm.
		span := changes[0].At.Sub(changes[len(changes)-1].At).Hours() / hoursPerDay
		body.Line = append(body.Line, Fact{Label: T("detail.ip.every"), Value: TxtA("detail.days", "n", int(span/float64(len(changes)-1)+0.5))})
	}
	if len(changes) > 0 {
		body.Blocks = append(body.Blocks, Block{Kind: BlockTimeline, Label: T("detail.ip.changes"), Hero: true, Data: firstN(changes, ipChangesShown)})
	} else {
		body.Blocks = append(body.Blocks, Block{Kind: BlockText, Data: Txt("detail.ip.no_change")})
	}
	if dns, ok := results[openName].(*sources.DomainsResolved); ok && len(dns.Hosts) > 0 {
		var rows [][]Cell
		for _, hst := range dns.Hosts {
			state, note := "bad", any(Txt("detail.ip.dns_other"))
			switch {
			case hst.Error != "":
				state, note = "warn", hst.Error
			case slices.Contains(hst.Addrs, ip.IP) || (ip.IPv6 != "" && slices.Contains(hst.Addrs, ip.IPv6)):
				state, note = "ok", Txt("detail.ip.dns_here")
			}
			rows = append(rows, []Cell{{Value: hst.Host}, {Value: strings.Join(hst.Addrs, ", ")}, {Value: note, State: state}})
		}
		body.Blocks = append(body.Blocks, Block{Kind: BlockTable, Label: T("detail.ip.dns"),
			Data: Table{Head: []Text{T("detail.ip.host"), T("detail.ip.addrs"), T("detail.state")}, Rows: rows}})
	}
	return DetailView{Body: body}
}

// renewalDue: Let's Encrypt and most ACME clients renew 30 days ahead;
// below 25 days left the renewal is late.
const renewalDue = 25

// The gateway dialog's spans: latency days, days a device counts as new.
const (
	linkDays     = 30
	newLeaseDays = 7
)

// ipChangesShown is how many address changes the dialog lists.
const ipChangesShown = 12

// domainsResolve reads the space's domains as DNS answers when the dialog opens.
func domainsResolve(PublicIPConfig) []Query {
	return []Query{{Name: openName, Source: "domains.resolve", Conn: ConnPeer, Service: enums.ServiceDomains}}
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

	// Where logins (green) and failures (red) came from; a place without
	// coordinates stays off the map.
	var marks []MapMark
	add := func(list []sources.AKLogin, state string) {
		for _, l := range list {
			if l.Lat != 0 || l.Lon != 0 {
				marks = append(marks, Pin(GeoPoint{l.Lat, l.Lon}, strings.Trim(l.User+" · "+l.City, " ·"), state))
			}
		}
	}
	add(data.Logins, "ok")
	add(data.Failures, "bad")
	if m, ok := NewMap(nil, marks); ok {
		overview = append([]Block{{Kind: BlockMap, Label: T("detail.authentik.map"), Data: m}}, overview...)
	}
	overview = append(overview, hintsBlock(results)...)
	body.Tabs = []Tab{
		{Label: T("detail.authentik.logins"), Blocks: overview},
		{Label: T("detail.authentik.failures"), Count: len(data.Failures), Blocks: []Block{{Kind: BlockTable, Data: Table{Head: head, Rows: loginRows(data.Failures)}}}},
		{Label: T("detail.authentik.apps"), Count: len(data.Apps), Blocks: []Block{{Kind: BlockBars, Data: apps}}},
		{Label: T("detail.authentik.accounts"), Count: len(data.Users), Blocks: []Block{{Kind: BlockTable, Data: Table{Head: []Text{T("detail.authentik.user"), T("detail.authentik.last_login")}, Rows: users}}}},
	}
	if len(data.Days) >= minPoints {
		logins, failed := make([]float64, len(data.Days)), make([]float64, len(data.Days))
		for i, d := range data.Days {
			logins[i], failed[i] = float64(d.Logins), float64(d.Failed)
		}
		g := LineGraph(Series{Values: logins, Class: "s1", Label: Txt("detail.authentik.logins")}, Series{Values: failed, Class: "s2", Label: Txt("detail.authentik.failures")})
		g.Lo, g.Ticks = 0, []any{DayS(data.Days[0].Day), DayS(data.Days[len(data.Days)-1].Day)}
		body.Blocks = append([]Block{{Kind: BlockGraph, Label: T("detail.authentik.per_day"), Hero: true, Data: g}}, body.Blocks...)
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
	var renewals [][]Cell
	if certs, ok := results["data"].(*sources.CertDataset); ok && cfg.Kinds != "domains" {
		warn, info := expiryLimits(ctx.Settings, "certs.expiring")
		for _, c := range certs.Certs {
			left := int(c.NotAfter.Sub(today).Hours() / hoursPerDay)
			sub := any(c.Issuer)
			if c.Error != "" {
				sub = c.Error
			}
			events = append(events, Event{At: c.NotAfter, Title: c.Host, Sub: sub, State: TxtA("detail.days", "n", left), Tier: tier(left, c.Error, warn, info)})
			// Automatic renewal runs a month ahead; a certificate this
			// close without one waits for a renewal that hangs.
			renewed := metrics.LastRenewal(historyOf(results), c.Host)
			late := c.Error == "" && left < renewalDue
			renewals = append(renewals, []Cell{{Value: c.Host}, {Value: dayOf(renewed)},
				{Value: Txt(map[bool]string{true: "detail.expiry.renewal_late", false: "detail.expiry.renewal_ok"}[late]), State: stateIf(late, "warn")}})
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
	if len(renewals) > 0 {
		body.Blocks = append(body.Blocks, Block{Kind: BlockTable, Label: T("detail.expiry.renewals"),
			Data: Table{Head: []Text{T("detail.expiry.host"), T("detail.expiry.renewed"), T("detail.state")}, Rows: renewals}})
	}
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
	names := make([]string, len(rows))
	for i, r := range rows {
		names[i] = r.Name
		list.Items = append(list.Items, LitRow{Name: r.Name, Meta: Txt("detail.exposure.risk_" + riskState[min(r.Risk, 2)]), State: riskState[min(r.Risk, 2)], Item: r.Name})
	}
	body := &DetailBody{List: list}
	if len(rows) > 0 {
		list.Sel = pickIndex(results, names)
		r := rows[list.Sel]
		list.Title, list.Sub, list.State, list.StateText = r.Name, r.Domain, riskState[min(r.Risk, 2)], T("detail.exposure.risk_"+riskState[min(r.Risk, 2)])
		cert := any("–")
		if r.CertDays >= 0 {
			cert = TxtA("detail.days", "n", r.CertDays)
		}
		login := Cell{Value: Txt("detail.answer_no"), State: "bad"}
		if r.Login {
			login = Cell{Value: Txt("detail.answer_yes"), State: "ok"}
		}
		facts := [][]Cell{{{Value: Txt("detail.exposure.login")}, login}, {{Value: Txt("detail.exposure.updates")}, {Value: r.Updates, State: stateIf(r.Updates > 0, "warn")}},
			{{Value: Txt("detail.exposure.cert")}, {Value: cert}}}
		if access, ok := results[openName].(*sources.PangolinAccess); ok {
			if a, found := access.ByResource[r.Name]; found {
				var countries []string
				for _, c := range a.Countries {
					countries = append(countries, fmt.Sprintf("%s %d", c.Name, c.N))
				}
				facts = append(facts, []Cell{{Value: Txt("detail.exposure.requests")}, {Value: Num(float64(a.Requests), 0)}},
					[]Cell{{Value: Txt("detail.exposure.blocked")}, {Value: Num(float64(a.Blocked), 0), State: stateIf(a.Blocked > 0, "warn")}},
					[]Cell{{Value: Txt("detail.exposure.from")}, {Value: cmp.Or(strings.Join(countries, " · "), "–")}})
			}
		}
		body.Blocks = append(body.Blocks, Block{Kind: BlockTable, Data: Table{Head: []Text{T("detail.exposure.what"), T("detail.exposure.value")}, Rows: facts}})
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
	byOrg := map[string]int{}
	for _, u := range data.Users {
		for _, o := range u.Orgs {
			byOrg[o]++
		}
	}
	if len(byOrg) > 0 {
		var orgs [][]Cell
		for _, name := range slices.Sorted(maps.Keys(byOrg)) {
			orgs = append(orgs, []Cell{{Value: name}, {Value: byOrg[name]}})
		}
		body.Blocks = append(body.Blocks, Block{Kind: BlockTable, Label: T("detail.vault.orgs"),
			Data: Table{Head: []Text{T("detail.vault.org"), T("detail.vault.members")}, Rows: orgs, Num: []int{1}}})
	}
	body.Blocks = append(body.Blocks, hintsBlock(results)...)
	return DetailView{Head: DetailHead{Sub: "Vaultwarden " + data.Version}, Body: body}
}

// speedResultBlocks: today's measurements, and the ones below the
// contract as a list for the provider (§ 57 TKG wants measurements on
// several days).
func speedResultBlocks(list []sources.SpeedResult, data *sources.SpeedtestDataset, share float64, today time.Time) []Block {
	zone := clockZone()
	var day [][]Cell
	var below [][]Cell
	days := map[string]bool{}
	for _, r := range list {
		local := r.At.In(zone)
		if local.Format(isoDate) == today.Format(isoDate) {
			day = append(day, []Cell{{Value: local.Format(timeOfDay)}, {Value: NumU(r.Down, 0, "Mbit/s"), State: stateIf(metrics.BelowContract(r.Down, data.ExpectDown, share), "warn")},
				{Value: NumU(r.Up, 0, "Mbit/s")}, {Value: NumU(r.Ping, 0, "ms")}})
		}
		if metrics.BelowContract(r.Down, data.ExpectDown, share) {
			days[local.Format(isoDate)] = true
			below = append(below, []Cell{{Value: Day(local)}, {Value: local.Format(timeOfDay)}, {Value: NumU(r.Down, 0, "Mbit/s")},
				{Value: NumU(r.Down/data.ExpectDown*percentScale, 0, "%"), State: "warn"}})
		}
	}
	var out []Block
	head := []Text{T("detail.speed.time"), T("detail.speed.down"), T("detail.speed.up"), T("detail.speed.ping")}
	if len(day) > 0 {
		out = append(out, Block{Kind: BlockTable, Label: T("detail.speed.today"), Data: Table{Head: head, Rows: reversed(day), Num: []int{1, 2, 3}}})
	}
	if len(below) > 0 {
		out = append(out, Block{Kind: BlockTable, Label: T("detail.speed.below"), Meta: TxtA("detail.speed.below_count", "n", len(below), "days", len(days)),
			Data: Table{Head: []Text{T("detail.speed.day"), T("detail.speed.time"), T("detail.speed.down"), T("detail.speed.of_contract")}, Rows: reversed(below), Num: []int{2, 3}}})
	}
	return out
}

// speedHourHeat: the mean shortfall against the contract per weekday
// (rows) and hour (columns); darker is slower.
func speedHourHeat(list []sources.SpeedResult, expect float64) (Heat, bool) {
	var sum, n [7][24]float64
	for _, r := range list {
		local := r.At.In(clockZone())
		wd, h := (int(local.Weekday())+6)%weekDays, local.Hour()
		sum[wd][h] += max(1-r.Down/expect, 0)
		n[wd][h]++
	}
	heat := Heat{Rows: weekDays, Ticks: []any{"00:00", "12:00", "23:00"}}
	found := false
	for h := range hoursPerDay {
		for wd := range weekDays {
			level := 0
			if n[wd][h] > 0 {
				found = true
				level = min(int(sum[wd][h]/n[wd][h]*heatSteps*2)+1, heatSteps)
			}
			heat.Levels = append(heat.Levels, level)
		}
	}
	return heat, found
}
