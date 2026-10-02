package widgets

// Detail dialogs of the homelab tiles (layouts as chosen in the popup
// review: record unless noted).

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"andon/internal/enums"
	"andon/internal/metrics"
	"andon/internal/sources"
)

const (
	backupDetailDays  = 14
	historyDetailDays = 90
	uptimeDetailDays  = 14
	monthsCompared    = 3
	gbPerTB           = 1024 * 1024 * 1024 * 1024
)

// backupsDetail: facts of the tools left; per object its days as a strip,
// the Borg repository's fill, failures and hints.
func backupsDetail(cfg BackupsConfig, results map[string]any, ctx ViewCtx) DetailView {
	borg, _ := results[string(enums.ServiceBorgBackup)].(*sources.BorgDataset)
	pg, _ := results[string(enums.ServicePGBackWeb)].(*sources.PGBackDataset)
	nas, _ := results[string(enums.ServiceTrueNAS)].(*sources.TrueNASDataset)
	if cfg.MaxHours == 0 {
		cfg.MaxHours = defaultBackupHours
	}
	now := time.Now().UTC()
	h := historyOf(results)
	rows := metrics.Backups(borg, pg, nas, now, time.Duration(cfg.MaxHours)*time.Hour)

	body := &DetailBody{}
	if borg != nil {
		body.Side = append(body.Side,
			Fact{Label: T("detail.backups.clients"), Value: len(borg.Clients)},
			Fact{Label: T("detail.backups.last24"), Value: fmt.Sprintf("%d / %d", borg.Completed24h, borg.Completed24h+borg.Failed24h)},
			Fact{Label: T("detail.backups.running"), Value: borg.Running})
		if borg.AgentsOutdated > 0 {
			body.Side = append(body.Side, Fact{Label: T("detail.backups.agents"), Value: borg.AgentsOutdated, State: "warn"})
		}
	}
	if pg != nil {
		body.Side = append(body.Side, Fact{Label: T("detail.backups.pg_event"), Value: agoOf(pg.LastEvent)})
	}
	if nas != nil {
		body.Side = append(body.Side, Fact{Label: T("detail.backups.snapshots"), Value: len(nas.Snapshots)})
	}
	body.Side = append(body.Side, Fact{Label: T("detail.backups.max_age"), Value: TxtA("detail.hours", "n", cfg.MaxHours)})

	ok, failed := 0, 0
	var strips []Strip
	var failures []Event
	for _, r := range rows {
		if r.State == metrics.BackupOK {
			ok++
		}
		if r.State == metrics.BackupFailed {
			failed++
		}
		strip := Strip{Name: r.Tool + " · " + r.Item, Value: agoOf(r.Last)}
		for _, mark := range metrics.BackupDays(h, r.Tool, r.Item, now, backupDetailDays) {
			switch mark {
			case 1:
				strip.States = append(strip.States, "ok")
			case 0:
				strip.States = append(strip.States, "bad")
			default:
				strip.States = append(strip.States, "off")
			}
		}
		strips = append(strips, strip)
		if r.State == metrics.BackupFailed || r.State == metrics.BackupOld {
			tier := "yellow"
			if r.State == metrics.BackupFailed {
				tier = "red"
			}
			failures = append(failures, Event{At: r.Last, Title: r.Tool + " · " + r.Item, State: Txt("backup." + string(r.State)), Tier: tier})
		}
	}
	tier := "green"
	if ok < len(rows) {
		tier = "yellow"
	}
	body.Facts = []Kpi{{Value: fmt.Sprintf("%d / %d", ok, len(rows)), Label: T("detail.backups.current"), Tier: tier}}
	if failed > 0 {
		body.Facts = append(body.Facts, Kpi{Value: failed, Label: T("detail.backups.failed"), Tier: "red"})
	}
	if borg != nil && borg.TotalBytes > 0 {
		body.Facts = append(body.Facts, Kpi{Value: Num(borg.UsedBytes/borg.TotalBytes*percentScale, 0), Label: T("detail.backups.repo_used")})
	}

	if len(strips) > 0 {
		body.Blocks = append(body.Blocks, Block{Kind: BlockStrips, Label: T("detail.backups.days"), Meta: len(strips), Ticks: spanTicks(now, backupDetailDays), Data: strips})
	}
	if used := dailySeries(h, metrics.SampleKey("borg", "used"), now, historyDetailDays); hasValues(used) {
		g := LineGraph(Series{Values: scaled(used, percentScale), Class: "s4"})
		g.Lo, g.Hi, g.Ticks = 0, percentScale, spanTicks(now, historyDetailDays)
		body.Blocks = append(body.Blocks, Block{Kind: BlockGraph, Label: T("detail.backups.repo_history"), Data: g})
	}
	var pair []Block
	if len(failures) > 0 {
		pair = append(pair, Block{Kind: BlockTimeline, Label: T("detail.backups.problems"), Data: failures})
	}
	pair = append(pair, hintsBlock(results)...)
	body.Blocks = append(body.Blocks, pairOf(pair)...)

	head := DetailHead{State: "ok", StateKey: "detail.backups.all_ok"}
	if ok < len(rows) {
		head.State, head.StateKey, head.StateArgs = "warn", "detail.backups.n_open", map[string]any{"n": len(rows) - ok}
	}
	if failed > 0 {
		head.State = "bad"
	}
	return DetailView{Head: head, Body: body}
}

// disksDetail (list and detail): the disks, the worst one chosen.
func disksDetail(cfg DisksConfig, results map[string]any, ctx ViewCtx) DetailView {
	data, _ := results["data"].(*sources.ScrutinyDataset)
	if data == nil {
		return DetailView{Body: &DetailBody{Blocks: hintsBlock(results)}}
	}
	if cfg.TempWarn == 0 {
		cfg.TempWarn = tempWarn
	}
	view := disksView(cfg, data, ctx)
	rows, _ := view["Rows"].([]DiskRow)
	byName := map[string]sources.Disk{}
	for _, d := range data.Disks {
		byName[d.Name] = d
	}

	list := &ObjList{Label: T("detail.disks.list")}
	for _, r := range rows {
		state := "ok"
		switch {
		case !r.OK && !r.Stale:
			state = "bad"
		case r.Stale:
			state = "off"
		case r.TempTier != "":
			state = "warn"
		}
		list.Items = append(list.Items, LitRow{Name: r.Name + " · " + r.Model, Meta: NumU(r.Temp, 0, "°C"), State: state})
	}
	if len(rows) > 0 {
		chosen := byName[rows[0].Name]
		list.Title, list.Sub = chosen.Name+" · "+chosen.Model, chosen.WWN
		list.State, list.StateText = list.Items[0].State, T("detail.disks.state_"+list.Items[0].State)
	}
	body := &DetailBody{List: list}
	if len(rows) > 0 {
		chosen := byName[rows[0].Name]
		body.Facts = []Kpi{
			{Value: NumU(chosen.Temp, 0, "°C"), Label: T("detail.disks.temp"), Tier: tempTier(chosen.Temp, cfg.TempWarn)},
			{Value: Num(float64(chosen.Hours)/hoursPerDay/365, 1), Label: T("detail.disks.years")},
		}
		facts := Table{Head: []Text{T("detail.disks.attr"), T("detail.disks.value")}}
		facts.Rows = append(facts.Rows, []Cell{{Value: Txt("detail.disks.hours")}, {Value: Num(float64(chosen.Hours), 0)}})
		facts.Rows = append(facts.Rows, []Cell{{Value: Txt("detail.disks.seen")}, {Value: agoOf(chosen.Seen)}})
		if chosen.Failing != "" {
			facts.Rows = append(facts.Rows, []Cell{{Value: Txt("detail.disks.failing")}, {Value: chosen.Failing, State: "bad"}})
		}
		body.Blocks = append(body.Blocks, Block{Kind: BlockTable, Label: T("detail.disks.smart"), Data: facts})
	}
	body.Blocks = append(body.Blocks, hintsBlock(results)...)

	healthy, _ := view["Healthy"].(int)
	head := DetailHead{State: "ok", StateKey: "detail.disks.all_ok"}
	if healthy < len(data.Disks) {
		head.State, head.StateKey, head.StateArgs = "bad", "detail.disks.n_bad", map[string]any{"n": len(data.Disks) - healthy}
	}
	return DetailView{Head: head, Body: body}
}

func tempTier(t, warn float64) string {
	switch {
	case t >= warn+tempHigh-tempWarn:
		return "red"
	case t >= warn:
		return "yellow"
	}
	return ""
}

// dockerDetail (tabs): overview with the striking containers, problems,
// all containers.
func dockerDetail(cfg DockerConfig, data *sources.DockerDataset, ctx ViewCtx, results map[string]any) DetailView {
	cfg.OnlyProblems = false
	view := dockerView(cfg, data, ctx)
	rows, _ := view["Rows"].([]DockerRow)
	running, _ := view["Running"].(int)

	var cards, problems []LitRow
	var all [][]Cell
	unhealthy, exited := 0, 0
	for _, c := range data.Containers {
		state := "ok"
		switch {
		case metrics.ContainerCrashed(c):
			state, exited = "bad", exited+1
		case metrics.ContainerUnhealthy(c):
			state, unhealthy = "warn", unhealthy+1
		case c.State != sources.StateRunning:
			state = "off"
		}
		cards = append(cards, LitRow{Name: c.Name, Meta: c.Status, State: state})
		if state == "bad" || state == "warn" {
			problems = append(problems, LitRow{Name: c.Name, Meta: c.Status, State: state})
		}
	}
	sort.SliceStable(cards, func(a, b int) bool { return stateRank(cards[a].State) < stateRank(cards[b].State) })
	for _, r := range rows {
		all = append(all, []Cell{{Value: r.Name}, {Value: r.Image}, {Value: r.Status, State: tierState(r.Tier)}})
	}

	body := &DetailBody{Facts: []Kpi{{Value: fmt.Sprintf("%d / %d", running, len(data.Containers)), Label: T("detail.docker.running"), Tier: tierIf(running < len(data.Containers), "yellow", "green")}}}
	if exited > 0 {
		body.Facts = append(body.Facts, Kpi{Value: exited, Label: T("detail.docker.exited"), Tier: "red"})
	}
	if unhealthy > 0 {
		body.Facts = append(body.Facts, Kpi{Value: unhealthy, Label: T("detail.docker.unhealthy"), Tier: "yellow"})
	}
	overview := []Block{{Kind: BlockStatus, Label: T("detail.docker.by_state"), Data: cards}}
	if len(problems) > 0 {
		overview = append([]Block{{Kind: BlockRows, Label: T("detail.docker.striking"), Data: problems}}, overview...)
	}
	overview = append(overview, hintsBlock(results)...)
	body.Tabs = []Tab{
		{Label: T("detail.tab.overview"), Blocks: overview},
		{Label: T("detail.tab.problems"), Count: len(problems), Blocks: []Block{{Kind: BlockRows, Data: problems}}},
		{Label: T("detail.tab.all"), Count: len(data.Containers), Blocks: []Block{{Kind: BlockTable, Data: Table{Head: []Text{T("detail.docker.name"), T("detail.docker.image"), T("detail.docker.status")}, Rows: all}}}},
	}
	head := DetailHead{State: "ok", StateKey: "detail.docker.all_ok"}
	switch {
	case exited > 0:
		head.State, head.StateKey, head.StateArgs = "bad", "detail.docker.n_exited", map[string]any{"n": exited}
	case unhealthy > 0:
		head.State, head.StateKey, head.StateArgs = "warn", "detail.docker.n_unhealthy", map[string]any{"n": unhealthy}
	}
	return DetailView{Head: head, Body: body}
}

// komodoDetail (tabs, like the containers): servers and stacks, updates,
// alerts, rollouts.
func komodoDetail(cfg KomodoConfig, data *sources.KomodoDataset, ctx ViewCtx, results map[string]any) DetailView {
	stopped := metrics.KomodoStopped(ctx.Options)
	var cards []LitRow
	var updates [][]Cell
	down := 0
	for _, s := range data.Stacks {
		if !matchesAny(s.Name, cfg.Only) {
			continue
		}
		state := map[string]string{"ok": "ok", "bad": "bad", "mid": "warn"}[stackState(s.State)]
		if stopped[strings.ToLower(s.Name)] && state != "ok" {
			state = "off"
		}
		if state == "bad" {
			down++
		}
		cards = append(cards, LitRow{Name: s.Name, Meta: s.State, State: state})
		if len(s.Updates) > 0 {
			updates = append(updates, []Cell{{Value: s.Name}, {Value: strings.Join(s.Updates, ", ")}})
		}
	}
	sort.SliceStable(cards, func(a, b int) bool { return stateRank(cards[a].State) < stateRank(cards[b].State) })
	var alerts []LitRow
	for _, a := range data.Alerts {
		alerts = append(alerts, LitRow{Name: a.Name + " · " + a.Kind, Meta: agoOf(a.At), State: alertState(a.Level)})
	}
	var rollouts []Event
	if h := historyOf(results); h != nil {
		for _, e := range h.Events {
			if e.Kind == metrics.EventUpdate && hasStack(data, e.Subject) {
				rollouts = append(rollouts, Event{At: e.At, Title: e.Subject, Sub: e.Detail, State: Txt("detail.komodo.rolled_out"), Tier: "cyan"})
			}
		}
	}

	body := &DetailBody{Facts: []Kpi{
		{Value: fmt.Sprintf("%d / %d", data.ServersHealthy, data.ServersTotal), Label: T("detail.komodo.servers"), Tier: tierIf(data.ServersHealthy < data.ServersTotal, "red", "green")},
		{Value: fmt.Sprintf("%d / %d", len(cards)-down, len(cards)), Label: T("detail.komodo.stacks"), Tier: tierIf(down > 0, "red", "")},
		{Value: len(updates), Label: T("detail.komodo.updates"), Tier: tierIf(len(updates) > 0, "cyan", "")},
		{Value: len(alerts), Label: T("detail.komodo.alerts"), Tier: tierIf(len(alerts) > 0, "yellow", "")},
	}}
	overview := []Block{{Kind: BlockStatus, Label: T("detail.komodo.by_state"), Data: cards}}
	overview = append(overview, hintsBlock(results)...)
	body.Tabs = []Tab{
		{Label: T("detail.tab.overview"), Blocks: overview},
		{Label: T("detail.komodo.updates"), Count: len(updates), Blocks: []Block{{Kind: BlockTable, Data: Table{Head: []Text{T("detail.komodo.stack"), T("detail.komodo.images")}, Rows: updates}}}},
		{Label: T("detail.komodo.alerts"), Count: len(alerts), Blocks: []Block{{Kind: BlockRows, Data: alerts}}},
		{Label: T("detail.komodo.rollouts"), Blocks: []Block{{Kind: BlockTimeline, Data: rollouts}}},
	}
	head := DetailHead{State: "ok", StateKey: "detail.komodo.all_ok"}
	switch {
	case down > 0:
		head.State, head.StateKey, head.StateArgs = "bad", "detail.komodo.n_down", map[string]any{"n": down}
	case len(updates) > 0:
		head.State, head.StateKey, head.StateArgs = "warn", "detail.komodo.n_updates", map[string]any{"n": len(updates)}
	}
	return DetailView{Head: head, Body: body}
}

func hasStack(data *sources.KomodoDataset, name string) bool {
	for _, s := range data.Stacks {
		if s.Name == name {
			return true
		}
	}
	return false
}

func alertState(level string) string {
	switch strings.ToLower(level) {
	case "critical", "error":
		return "bad"
	case "warning", "warn":
		return "warn"
	}
	return "info"
}

// truenasDetail: host facts, pools and their fill over the last months,
// snapshot tasks, alerts and apps.
func truenasDetail(cfg TrueNASConfig, results map[string]any, ctx ViewCtx) DetailView {
	data, _ := results["data"].(*sources.TrueNASDataset)
	if data == nil {
		return DetailView{Body: &DetailBody{Blocks: hintsBlock(results)}}
	}
	if cfg.WarnPct == 0 {
		cfg.WarnPct = loadWarn
	}
	cfg.Forecast = true
	view := truenasView(cfg, results, ctx)
	pools, _ := view["Pools"].([]PoolBar)
	now := todayOf(ctx)
	h := historyOf(results)

	updates := 0
	for _, a := range data.Apps {
		if a.Update {
			updates++
		}
	}
	snapErrors := 0
	var snaps [][]Cell
	for _, s := range data.Snapshots {
		state := "ok"
		if s.State == "ERROR" {
			state, snapErrors = "bad", snapErrors+1
		}
		snaps = append(snaps, []Cell{{Value: s.Dataset}, {Value: agoOf(s.Last)}, {Value: s.State, State: state}})
	}
	body := &DetailBody{Side: []Fact{
		{Label: T("detail.truenas.host"), Value: data.Host},
		{Label: T("detail.truenas.version"), Value: data.Version},
		{Label: T("detail.truenas.pools"), Value: len(data.Pools)},
		{Label: T("detail.truenas.alerts"), Value: len(data.Alerts), State: stateIf(len(data.Alerts) > 0, "warn")},
		{Label: T("detail.truenas.apps"), Value: fmt.Sprintf("%d · %d ↑", len(data.Apps), updates)},
		{Label: T("detail.truenas.snapshots"), Value: fmt.Sprintf("%d · %d ✕", len(data.Snapshots), snapErrors), State: stateIf(snapErrors > 0, "bad")},
	}}
	var bars []ShareBar
	var series []Series
	for i, p := range pools {
		value := NumU(float64(p.W), 0, "%")
		if p.FullIn >= 0 {
			value = TxtA("detail.full_in", "n", p.FullIn)
		}
		bars = append(bars, ShareBar{Name: p.Label, Pct: float64(p.W), Value: value, Tier: p.Tier})
		name := data.Pools[i].Name
		if used := dailySeries(h, metrics.SampleKey("truenas", "pool", name, "used"), now, historyDetailDays); hasValues(used) {
			series = append(series, Series{Values: scaled(used, percentScale), Class: dataClass(i), Label: name})
		}
		body.Facts = append(body.Facts, Kpi{Value: NumU(float64(p.W), 0, "%"), Label: Text{Key: "detail.truenas.pool_used", Args: map[string]any{"pool": name}}, Tier: p.Tier})
	}
	body.Blocks = append(body.Blocks, Block{Kind: BlockBars, Label: T("detail.truenas.pools"), Data: bars})
	if len(series) > 0 {
		g := LineGraph(series...)
		g.Lo, g.Hi, g.Goal, g.HasGoal, g.Ticks = 0, percentScale, cfg.WarnPct, true, spanTicks(now, historyDetailDays)
		body.Blocks = append(body.Blocks, Block{Kind: BlockGraph, Label: T("detail.truenas.history"), Data: g})
	}
	var alerts []LitRow
	for _, a := range data.Alerts {
		alerts = append(alerts, LitRow{Name: a.Text, Meta: a.Level, State: alertState(a.Level)})
	}
	for _, a := range data.Apps {
		if a.Update {
			alerts = append(alerts, LitRow{Name: a.Name, Meta: Txt("detail.update_available"), State: "info"})
		}
	}
	pair := []Block{}
	if len(snaps) > 0 {
		pair = append(pair, Block{Kind: BlockTable, Label: T("detail.truenas.snapshots"), Data: Table{Head: []Text{T("detail.truenas.dataset"), T("detail.last_run"), T("detail.state")}, Rows: snaps}})
	}
	if len(alerts) > 0 {
		pair = append(pair, Block{Kind: BlockRows, Label: T("detail.truenas.alerts_apps"), Data: alerts})
	}
	body.Blocks = append(body.Blocks, pairOf(pair)...)
	body.Blocks = append(body.Blocks, hintsBlock(results)...)

	head := DetailHead{State: "ok", StateKey: "detail.truenas.healthy"}
	for _, p := range data.Pools {
		if !p.Healthy {
			head.State, head.StateKey = "bad", "detail.truenas.unhealthy"
		}
	}
	return DetailView{Head: head, Body: body}
}

// storageDetail (wall): one card per store with its last 30 days.
func storageDetail(cfg StorageConfig, results map[string]any, ctx ViewCtx) DetailView {
	view := storageView(cfg, results, ctx)
	rows, _ := view["Rows"].([]StorageRow)
	now := todayOf(ctx)
	h := historyOf(results)
	var keys []string
	if h != nil {
		for _, f := range metrics.StorageForecasts(h, now) {
			if matchesAny(f.Label, cfg.Only) {
				keys = append(keys, f.Key)
			}
		}
	}
	body := &DetailBody{}
	var cards []Card
	for i, r := range rows {
		card := Card{Label: Plain(r.Label), Value: NumU(r.Used, 0, "%"), Tier: r.Tier,
			Sub: Txt("detail.storage.not_filling")}
		if r.FullIn >= 0 {
			card.Sub = TxtA("detail.storage.full_on", "day", DayS(r.FullOn))
		}
		if i < len(keys) {
			if s := dailySeries(h, keys[i], now, storageAhead); hasValues(s) {
				card.Spark = filled(scaled(s, percentScale))
			}
		}
		cards = append(cards, card)
	}
	if len(cards) == 0 {
		body.Blocks = append(body.Blocks, Block{Kind: BlockText, Data: Txt("detail.storage.none")})
	} else {
		body.Blocks = append(body.Blocks, Block{Kind: BlockWall, Data: cards})
	}
	body.Blocks = append(body.Blocks, hintsBlock(results)...)
	return DetailView{Body: body}
}

// homelabCostDetail (wall): the month's bill by kind, power per service.
func homelabCostDetail(cfg CostConfig, results map[string]any, ctx ViewCtx) DetailView {
	cfg.Yearly, cfg.PowerSplit = false, true
	view := homelabCostView(cfg, results, ctx)
	bill, _ := view["Bill"].(metrics.HomelabBill)
	if bill.Total == 0 {
		return DetailView{Body: &DetailBody{Blocks: []Block{{Kind: BlockText, Data: Txt("cost.none")}}}}
	}
	currency := "EUR"
	cards := []Card{{Label: T("detail.cost.month"), Value: Money(bill.Total, currency), Sub: TxtA("detail.per_year", "amount", Money(bill.Total*monthsPerYearF, currency))}}
	for _, it := range bill.Items {
		cards = append(cards, Card{Label: T("cost." + it.Key), Value: Money(it.Monthly, currency), Sub: it.Name})
	}
	if bill.Cloud > 0 {
		tier := tierIf(bill.Total < bill.Cloud, "green", "yellow")
		cards = append(cards, Card{Label: T("detail.cost.cloud"), Value: Money(bill.Total-bill.Cloud, currency), Tier: tier, Sub: Money(bill.Cloud, currency)})
	}
	if pct, ok := view["SharePct"].(float64); ok {
		cards = append(cards, Card{Label: T("detail.cost.business"), Value: NumU(pct, 0, "%"), Tier: "cyan", Sub: TxtA("detail.per_year", "amount", Money(asF(view["BusinessYearly"]), currency))})
	}
	body := &DetailBody{Blocks: []Block{{Kind: BlockWall, Data: cards}}}
	if services, ok := view["Services"].([]metrics.ServiceCost); ok && len(services) > 0 {
		var bars []ShareBar
		for _, s := range services {
			bars = append(bars, ShareBar{Name: s.Name, Pct: s.Share * percentScale, Value: Money(s.Monthly, currency)})
		}
		body.Blocks = append(body.Blocks, Block{Kind: BlockBars, Label: T("detail.cost.power_split"), Meta: NumU(asF(view["Watts"]), 0, "W"), Data: bars})
	}
	return DetailView{Body: body}
}

// updatesDetail: the open updates with their age, recent rollouts.
func updatesDetail(cfg HintsConfig, results map[string]any, ctx ViewCtx) DetailView {
	list, _ := results[DetailHintsSlot].([]DetailHint)
	now := time.Now().UTC()
	var rows [][]Cell
	oldest := 0
	for _, h := range list {
		days := int(now.Sub(h.FirstSeen).Hours() / hoursPerDay)
		oldest = max(oldest, days)
		rows = append(rows, []Cell{{Value: h.Title}, {Value: h.Why}, {Value: TxtA("detail.days", "n", days), State: stateIf(days > updateOldDays, "warn")}})
	}
	var rolled []Event
	if h := historyOf(results); h != nil {
		for _, e := range h.Events {
			if e.Kind == metrics.EventUpdate && len(rolled) < listShown {
				rolled = append(rolled, Event{At: e.At, Title: e.Subject, Sub: e.Detail, State: Txt("detail.updates.done"), Tier: "green"})
			}
		}
	}
	body := &DetailBody{Facts: []Kpi{{Value: len(list), Label: T("detail.updates.open"), Tier: tierIf(len(list) > 0, "yellow", "green")}}}
	if len(list) > 0 {
		body.Facts = append(body.Facts, Kpi{Value: TxtA("detail.days", "n", oldest), Label: T("detail.updates.oldest"), Tier: tierIf(oldest > updateOldDays, "yellow", "")})
		body.Blocks = append(body.Blocks, Block{Kind: BlockTable, Label: T("detail.updates.open"), Data: Table{Head: []Text{T("detail.updates.what"), T("detail.updates.why"), T("detail.updates.since")}, Rows: rows}})
	}
	if len(rolled) > 0 {
		body.Blocks = append(body.Blocks, Block{Kind: BlockTimeline, Label: T("detail.updates.recent"), Data: rolled})
	}
	head := DetailHead{State: "ok", StateKey: "detail.updates.none"}
	if len(list) > 0 {
		head.State, head.StateKey, head.StateArgs = "warn", "detail.updates.n_open", map[string]any{"n": len(list)}
	}
	return DetailView{Head: head, Body: body}
}

// updateOldDays marks an update left lying for longer.
const updateOldDays = 14

// updateWindowDetail: the factors for and against now, the waiting updates.
func updateWindowDetail(cfg WindowConfig, results map[string]any, ctx ViewCtx) DetailView {
	view := updateWindowView(cfg, results, ctx)
	w, _ := view["Window"].(metrics.Window)
	good, _ := view["Good"].(bool)
	against := map[string]bool{}
	for _, b := range w.Blockers {
		against[b] = true
	}
	var factors []LitRow
	for _, f := range windowFactorOrder {
		if cfg.Ignore[f] {
			continue
		}
		row := LitRow{Name: Txt("detail.window.ok_" + f), State: "ok"}
		if against[f] {
			row = LitRow{Name: Txt("window." + f), State: "warn"}
		}
		factors = append(factors, row)
	}
	if against[WindowOutside] {
		factors = append(factors, LitRow{Name: Txt("window." + WindowOutside), State: "warn"})
	}
	var ups [][]Cell
	for _, u := range w.Updates {
		ups = append(ups, []Cell{{Value: u.Service}, {Value: u.What}})
	}
	var span any = Txt("detail.window.always")
	if cfg.From != cfg.To {
		span = fmt.Sprintf("%02d:%02d–%02d:%02d", cfg.From/minutesPerHour, cfg.From%minutesPerHour, cfg.To/minutesPerHour, cfg.To%minutesPerHour)
	}
	body := &DetailBody{
		Side: []Fact{{Label: T("detail.window.span"), Value: span}, {Label: T("detail.window.zone"), Value: cfg.Timezone},
			{Label: T("detail.window.backup"), Value: TxtA("detail.hours_ago", "n", int(w.BackupAge.Hours()))}},
		Facts:  []Kpi{{Value: len(w.Updates), Label: T("detail.window.updates")}, {Value: len(w.Blockers), Label: T("detail.window.against"), Tier: tierIf(len(w.Blockers) > 0, "yellow", "green")}},
		Blocks: []Block{{Kind: BlockRows, Label: T("detail.window.factors"), Data: factors}},
	}
	if len(ups) > 0 {
		body.Blocks = append(body.Blocks, Block{Kind: BlockTable, Label: T("detail.window.waiting"), Data: Table{Head: []Text{T("detail.window.service"), T("detail.window.update")}, Rows: ups}})
	}
	head := DetailHead{State: "warn", StateKey: "window.wait"}
	if good {
		head.State, head.StateKey = "ok", "window.good"
	}
	return DetailView{Head: head, Body: body}
}

// windowFactorOrder lists the update window's factors as the dialog shows them.
var windowFactorOrder = []string{"no_backup", "streaming", "working", "meeting", "expensive"}

// immichDetail: library growth and disk from the history, failed jobs.
func immichDetail(_ struct{}, data *sources.ImmichDataset, ctx ViewCtx, results map[string]any) DetailView {
	now := todayOf(ctx)
	h := historyOf(results)
	failed := 0
	var jobs []ShareBar
	most := 1
	for _, n := range data.FailedJobs {
		failed += n
		most = max(most, n)
	}
	for q, n := range data.FailedJobs {
		if n > 0 {
			jobs = append(jobs, ShareBar{Name: q, Pct: float64(n) * percentScale / float64(most), Value: n, Tier: "yellow"})
		}
	}
	sort.Slice(jobs, func(a, b int) bool { return jobs[a].Pct > jobs[b].Pct })
	version := data.Version
	if data.Latest != "" && data.Latest != data.Version {
		version += " → " + data.Latest
	}
	body := &DetailBody{
		Side: []Fact{{Label: T("detail.immich.version"), Value: version, State: stateIf(data.Latest != "" && data.Latest != data.Version, "warn")},
			{Label: T("detail.immich.photos"), Value: Num(float64(data.Photos), 0)}, {Label: T("detail.immich.videos"), Value: Num(float64(data.Videos), 0)},
			{Label: T("detail.immich.disk"), Value: data.DiskAvailable}},
		Facts: []Kpi{{Value: Num(float64(data.Photos+data.Videos), 0), Label: T("detail.immich.items")},
			{Value: NumU(data.DiskPercent, 0, "%"), Label: T("detail.immich.disk_used"), Tier: immichDiskTier(data, ctx)},
			{Value: failed, Label: T("detail.immich.failed"), Tier: tierIf(failed > 0, "yellow", "")}},
	}
	if items := dailySeries(h, metrics.SampleKey("immich", "items"), now, historyDetailDays); hasValues(items) {
		g := LineGraph(Series{Values: items, Class: "s1"})
		g.Ticks = spanTicks(now, historyDetailDays)
		body.Blocks = append(body.Blocks, Block{Kind: BlockGraph, Label: T("detail.immich.growth"), Hero: true, Data: g})
	}
	if len(jobs) > 0 {
		body.Blocks = append(body.Blocks, Block{Kind: BlockBars, Label: T("detail.immich.jobs"), Data: jobs})
	}
	body.Blocks = append(body.Blocks, hintsBlock(results)...)
	head := DetailHead{State: "ok", StateKey: "detail.immich.current"}
	if data.Latest != "" && data.Latest != data.Version {
		head.State, head.StateKey = "warn", "detail.update_available"
	}
	return DetailView{Head: head, Body: body}
}

// umamiDetail (list and detail): the sites, the biggest change chosen.
func umamiDetail(cfg UmamiConfig, data *sources.UmamiDataset, ctx ViewCtx, results map[string]any) DetailView {
	drop, minPrev := umamiLimits(ctx)
	var sites []sources.Site
	for _, s := range data.Sites {
		if matchesAny(s.Name, cfg.Only) || matchesAny(s.Domain, cfg.Only) {
			sites = append(sites, s)
		}
	}
	change := func(s sources.Site) int {
		share, _ := metrics.VisitorChange(s)
		return int(share * pctFull)
	}
	sort.SliceStable(sites, func(a, b int) bool { return change(sites[a]) < change(sites[b]) })
	list := &ObjList{Label: T("detail.umami.sites")}
	for _, s := range sites {
		state := "ok"
		if metrics.VisitorDrop(s, drop, minPrev) {
			state = "bad"
		}
		list.Items = append(list.Items, LitRow{Name: s.Name, Meta: fmt.Sprintf("%d · %+d %%", s.Visitors, change(s)), State: state})
	}
	body := &DetailBody{List: list}
	if len(sites) > 0 {
		s := sites[0]
		list.Title, list.Sub, list.State, list.StateText = s.Name, s.Domain, list.Items[0].State, Text{Key: "detail.umami.change", Args: map[string]any{"n": change(s)}}
		body.Facts = []Kpi{{Value: s.Views, Label: T("detail.umami.views")}, {Value: s.Visitors, Label: T("detail.umami.visitors")},
			{Value: s.PrevVisit, Label: T("detail.umami.prev")}}
		g := ColGraph([]float64{float64(s.PrevViews), float64(s.Views)}, "s1")
		g.Ticks = []any{Txt("detail.umami.week_before"), Txt("detail.umami.week")}
		body.Blocks = append(body.Blocks, Block{Kind: BlockGraph, Label: T("detail.umami.views"), Data: g})
	}
	body.Blocks = append(body.Blocks, hintsBlock(results)...)
	return DetailView{Body: body}
}

// glancesDetail: the chosen metric over the kept samples, its peak and
// the time above the warning line.
func glancesDetail(cfg GlancesChartConfig, results map[string]any, ctx ViewCtx) DetailView {
	data, ok := results["history"].(*sources.GlancesHistory)
	if !ok || len(data.Samples) < 2 {
		return DetailView{Body: &DetailBody{Blocks: hintsBlock(results)}}
	}
	values := make([]float64, len(data.Samples))
	peak, sum, above := 0.0, 0.0, 0
	for i, s := range data.Samples {
		values[i] = s.Value
		peak, sum = max(peak, s.Value), sum+s.Value
		if cfg.Warn > 0 && s.Value >= cfg.Warn {
			above++
		}
	}
	g := LineGraph(Series{Values: values, Class: "s1"})
	if data.Metric != "load" {
		g.Lo, g.Hi = 0, pctFull
	}
	if cfg.Warn > 0 {
		g.Goal, g.HasGoal = cfg.Warn, true
	}
	g.Ticks = []any{data.Samples[0].At, data.Samples[len(data.Samples)-1].At}
	unit := "%"
	if data.Metric == "load" {
		unit = ""
	}
	body := &DetailBody{
		Side: []Fact{{Label: T("detail.glances.metric"), Value: Txt("sys." + data.Metric)}, {Label: T("detail.glances.points"), Value: len(values)}},
		Facts: []Kpi{{Value: NumU(values[len(values)-1], 1, unit), Label: T("detail.now")}, {Value: NumU(peak, 1, unit), Label: T("detail.glances.peak"), Tier: tierIf(cfg.Warn > 0 && peak >= cfg.Warn, "yellow", "")},
			{Value: NumU(sum/float64(len(values)), 1, unit), Label: T("detail.glances.mean")}},
		Blocks: []Block{{Kind: BlockGraph, Label: T("sys." + data.Metric), Hero: true, Data: g}},
	}
	if cfg.Warn > 0 {
		body.Facts = append(body.Facts, Kpi{Value: above, Label: T("detail.glances.above"), Tier: tierIf(above > 0, "yellow", "")})
	}
	return DetailView{Body: body}
}

// sysinfoDetail (wall): every reading as a card.
func sysinfoDetail(cfg SysinfoConfig, results map[string]any, ctx ViewCtx) DetailView {
	s, ok := results["stats"].(*sources.GlancesResult)
	if !ok {
		return DetailView{Body: &DetailBody{}}
	}
	if cfg.Warn == 0 {
		cfg.Warn = loadWarn
	}
	card := func(key string, v float64) Card {
		return Card{Label: T("sys." + key), Value: NumU(v, 0, "%"), Tier: meterTierName(v, cfg.Warn)}
	}
	cards := []Card{card("cpu", s.CPU), card("mem", s.Mem), card("swap", s.Swap)}
	load := Card{Label: T("detail.sys.load"), Value: Num(s.Load, 2)}
	if s.Cores > 0 {
		load.Sub = Text{Key: "detail.sys.per_core", Args: map[string]any{"n": s.Cores}}
		load.Value = Num(s.Load/float64(s.Cores), 2)
	}
	cards = append(cards, load)
	for _, d := range s.Disks {
		cards = append(cards, Card{Label: Plain(d.Mount), Value: NumU(d.Percent, 0, "%"), Tier: meterTierName(d.Percent, cfg.Warn)})
	}
	return DetailView{Body: &DetailBody{Blocks: []Block{{Kind: BlockWall, Data: cards}}}}
}

func meterTierName(v, warn float64) string {
	switch meterTier(v, warn) {
	case "red", "bad":
		return "red"
	case "yellow", "mid":
		return "yellow"
	}
	return ""
}

// uptimeMonthDetail: per monitor the days of the month, the last months.
func uptimeMonthDetail(cfg UptimeMonthConfig, results map[string]any, ctx ViewCtx) DetailView {
	view := uptimeMonthView(cfg, results, ctx)
	rows, _ := view["Rows"].([]UptimeRow)
	h := historyOf(results)
	today := todayOf(ctx)
	start := metrics.MonthStart(today)
	days := int(today.Sub(start).Hours()/hoursPerDay) + 1
	now := time.Now().UTC()

	var strips []Strip
	var months [][]Cell
	below, sum := 0, 0.0
	longest := 0.0
	for _, r := range rows {
		strip := Strip{Name: r.Name, Value: fmt.Sprintf("%.2f %%", r.SharePct)}
		for _, share := range metrics.UptimeDays(h, r.Name, now, days) {
			strip.States = append(strip.States, shareState(share))
		}
		strips = append(strips, strip)
		if r.Tier != "ok" {
			below++
		}
		sum += r.SharePct
		longest = max(longest, r.DownHours)
		row := []Cell{{Value: r.Name}, {Value: NumU(r.SharePct, 2, "%"), State: tierState(r.Tier)}}
		for m := 1; m < monthsCompared; m++ {
			from := start.AddDate(0, -m, 0)
			if share, ok := metrics.Uptime(h, r.Name, from, from.AddDate(0, 1, -1)); ok {
				row = append(row, Cell{Value: NumU(share*pctFull, 2, "%")})
			} else {
				row = append(row, Cell{Value: "–"})
			}
		}
		months = append(months, row)
	}
	body := &DetailBody{Side: []Fact{{Label: T("detail.uptime.month"), Value: TxtA("detail.uptime.days_of", "n", days)}}}
	if cfg.SLA > 0 {
		body.Side = append(body.Side, Fact{Label: T("detail.uptime.sla"), Value: NumU(cfg.SLA, 1, "%")})
	}
	if len(rows) > 0 {
		body.Facts = []Kpi{{Value: NumU(sum/float64(len(rows)), 2, "%"), Label: T("detail.uptime.mean")},
			{Value: below, Label: T("detail.uptime.below"), Tier: tierIf(below > 0, "yellow", "green")},
			{Value: TxtA("detail.hours", "n", int(longest)), Label: T("detail.uptime.longest")}}
	}
	body.Blocks = []Block{{Kind: BlockStrips, Label: T("detail.uptime.days"), Ticks: []any{Day(start), Txt("detail.today")}, Data: strips},
		{Kind: BlockTable, Label: T("detail.uptime.months"), Data: Table{Head: []Text{T("detail.uptime.monitor"), T("detail.uptime.this_month"), T("detail.uptime.month_1"), T("detail.uptime.month_2")}, Rows: months, Num: []int{1, 2, 3}}}}
	return DetailView{Body: body}
}

// shareState: a day's up share as a strip state; -1 (no runs) off.
func shareState(share float64) string {
	switch {
	case share < 0:
		return "off"
	case share >= uptimeOK:
		return "ok"
	case share >= uptimeWarn:
		return "warn"
	}
	return "bad"
}

// monitorsDetail (grid): every monitor now, the last 14 days, response time.
func monitorsDetail(cfg MonitorsConfig, results map[string]any, ctx ViewCtx) DetailView {
	data, ok := results["data"].(*sources.KumaDataset)
	if !ok {
		return DetailView{Body: &DetailBody{}}
	}
	h := historyOf(results)
	now := time.Now().UTC()
	var cards []LitRow
	var strips []Strip
	up := 0
	msDays := make([][]float64, 0)
	for _, m := range data.Monitors {
		if !matchesAny(m.Name, cfg.Only) {
			continue
		}
		state := map[int]string{sources.KumaUp: "ok", sources.KumaDown: "bad", sources.KumaPending: "warn", sources.KumaMaintenance: "off"}[m.Status]
		if m.Status == sources.KumaUp {
			up++
		}
		meta := any(Txt("kuma." + kumaStates[m.Status][1]))
		if m.MS > 0 {
			meta = NumU(m.MS, 0, "ms")
		}
		cards = append(cards, LitRow{Name: m.Name, Meta: meta, State: state})
		strip := Strip{Name: m.Name}
		if m.CertDays >= 0 {
			strip.Value = fmt.Sprintf("TLS %d d", m.CertDays)
		}
		for _, share := range metrics.UptimeDays(h, m.Name, now, uptimeDetailDays) {
			strip.States = append(strip.States, shareState(share))
		}
		strips = append(strips, strip)
		msDays = append(msDays, dailySeries(h, metrics.SampleKey("kuma", "ms", m.Name), now, uptimeDetailDays))
	}
	sort.SliceStable(cards, func(a, b int) bool { return stateRank(cards[a].State) < stateRank(cards[b].State) })
	body := &DetailBody{Facts: []Kpi{{Value: fmt.Sprintf("%d / %d", up, len(cards)), Label: T("detail.monitors.up"), Tier: tierIf(up < len(cards), "red", "green")}},
		Blocks: []Block{{Kind: BlockStatus, Label: T("detail.monitors.now"), Data: cards},
			{Kind: BlockStrips, Label: T("detail.monitors.days"), Ticks: spanTicks(now, uptimeDetailDays), Data: strips}}}
	if mean := meanSeries(msDays); hasValues(mean) {
		g := LineGraph(Series{Values: mean, Class: "s1"})
		g.Goal, g.HasGoal, g.GoalDanger, g.Lo, g.Ticks = slowMs, true, true, 0, spanTicks(now, uptimeDetailDays)
		body.Blocks = append(body.Blocks, Block{Kind: BlockGraph, Label: T("detail.monitors.ms"), Data: g})
	}
	body.Blocks = append(body.Blocks, hintsBlock(results)...)
	head := DetailHead{State: "ok", StateKey: "detail.monitors.all_up"}
	if up < len(cards) {
		head.State, head.StateKey, head.StateArgs = "bad", "detail.monitors.n_down", map[string]any{"n": len(cards) - up}
	}
	return DetailView{Head: head, Body: body}
}

// slowMs is the response time that counts as slow.
const slowMs = 1000
