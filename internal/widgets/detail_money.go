package widgets

// Detail dialogs of the money tiles: Invoice Ninja, Sure, Paperless, mail,
// Wallos.

import (
	"cmp"
	"sort"
	"strconv"
	"strings"
	"time"

	"andon/internal/metrics"
	"andon/internal/rules"
	"andon/internal/sources"
)

const (
	cashDetailDays    = 90
	cashPastDays      = 90
	cashEventsDetail  = 12
	rateDetailMonths  = 24
	moneyDetailMonths = 12
)

// cashflowDetail (timeline): the balance back and ahead, the large moves.
func cashflowDetail(cfg CashflowConfig, results map[string]any, ctx ViewCtx) DetailView {
	ninja, ok := results["data"].(*sources.NinjaDataset)
	if !ok {
		return DetailView{Body: &DetailBody{Blocks: hintsBlock(results)}}
	}
	today := todayOf(ctx)
	in := cashInputsOf(cfg, ninja, results, ctx)
	days := max(cfg.Days, cashDetailDays)
	points, events := metrics.Cashflow(in, today, days)
	currency := ninja.Currency
	if currency == "" {
		currency = "EUR"
	}
	body := &DetailBody{}
	if len(points) < minPoints {
		body.Blocks = []Block{{Kind: BlockText, Data: Txt("detail.cash.none")}}
		return DetailView{Body: body}
	}
	low, lowDay := metrics.CashLow(points)

	// The stored balance before today (Sure), then the forecast.
	past := dailySeries(historyOf(results), metrics.SampleKey("sure", "cash"), today, cashPastDays)
	ahead := make([]float64, len(past)+len(points)-1)
	back := make([]float64, len(ahead))
	for i := range ahead {
		ahead[i], back[i] = Gap, Gap
		if i < len(past) {
			back[i] = past[i]
		}
	}
	for i, p := range points {
		ahead[len(past)-1+i] = p.Balance
	}
	series := []Series{{Values: ahead, Class: "s4", Label: ""}}
	if in.Sure != nil && hasValues(past) {
		series = append([]Series{{Values: back, Class: "s1"}}, series...)
	}
	g := LineGraph(series...)
	g.Mark, g.Ticks = len(past)-1, []any{Day(today.AddDate(0, 0, -cashPastDays+1)), Txt("detail.today"), Day(today.AddDate(0, 0, days))}
	if cfg.MinBalance != 0 {
		g.Goal, g.HasGoal, g.GoalDanger = cfg.MinBalance, true, true
	}
	body.Line = []Fact{{Label: T("detail.cash.now"), Value: Money(points[0].Balance, currency)}, {Label: T("detail.cash.lowest"), Value: TxtA("detail.cash.at_day", "amount", Money(low, currency), "day", Day(lowDay))}}
	if cfg.MinBalance != 0 {
		body.Line = append(body.Line, Fact{Label: T("detail.cash.min"), Value: Money(cfg.MinBalance, currency)})
	}
	body.Blocks = append(body.Blocks, Block{Kind: BlockGraph, Label: T("detail.cash.balance"), Hero: true, Data: g})
	body.Facts = []Kpi{{Value: Money(points[0].Balance, currency), Label: T("detail.cash.now")}, {Value: Money(low, currency), Label: T("detail.cash.lowest"), Tier: tierIf(cfg.MinBalance != 0 && low < cfg.MinBalance, "red", "yellow")},
		{Value: Money(points[len(points)-1].Balance-points[0].Balance, currency), Label: textDays("detail.cash.change", days), Tier: tierIf(points[len(points)-1].Balance >= points[0].Balance, "green", "yellow")}}
	sort.SliceStable(events, func(a, b int) bool { return abs(events[a].Amount) > abs(events[b].Amount) })
	var moves []Event
	for _, e := range firstN(events, cashEventsDetail) {
		moves = append(moves, Event{At: e.Day, Title: e.Label, State: Money(e.Amount, currency), Tier: tierIf(e.Amount >= 0, "green", "yellow")})
	}
	sort.SliceStable(moves, func(a, b int) bool { return moves[a].At.Before(moves[b].At) })
	if len(moves) > 0 {
		body.Blocks = append(body.Blocks, Block{Kind: BlockTimeline, Label: T("detail.cash.moves"), Data: moves})
	}

	// What if one invoice is paid a month later: pick it, see the curve.
	var payments []LitRow
	for _, e := range events {
		if e.Ref != "" {
			payments = append(payments, LitRow{Name: e.Label, Meta: Money(e.Amount, currency), State: "info", Item: e.Ref})
		}
	}
	if late := pickedItem(results); late != "" {
		in.LateRef, in.LateDays = late, cashLateDays
		if alt, _ := metrics.Cashflow(in, today, days); len(alt) == len(points) {
			values := make([]float64, len(ahead))
			for i := range values {
				values[i] = Gap
			}
			for i, p := range alt {
				values[len(past)-1+i] = p.Balance
			}
			g.Series = append(g.Series, Series{Values: values, Class: "s2", Label: TxtA("detail.cash.if_late", "ref", late, "n", cashLateDays)})
			for i := range body.Blocks {
				if body.Blocks[i].Hero {
					body.Blocks[i].Data = g
				}
			}
			altLow, altDay := metrics.CashLow(alt)
			body.Line = append(body.Line, Fact{Label: textArgs("detail.cash.lowest_if", "ref", late), Value: TxtA("detail.cash.at_day", "amount", Money(altLow, currency), "day", Day(altDay)),
				State: stateIf(cfg.MinBalance != 0 && altLow < cfg.MinBalance, "bad")})
		}
	}
	if len(payments) > 0 {
		body.Blocks = append(body.Blocks, Block{Kind: BlockRows, Label: textArgs("detail.cash.what_if", "n", cashLateDays), Data: payments})
	}
	body.Blocks = append(body.Blocks, hintsBlock(results)...)
	return DetailView{Body: body}
}

// The payment-days dialog's what-ifs: a 2 % discount for fast payment,
// 9 % a year on late days (B2B late payment interest is 9 points over
// the base rate; the base rate is left out).
const (
	payDiscountPct = 2.0
	payInterestPct = 9.0
)

// flowLine: how many days money spends as open work and as an invoice.
func flowLine(kimai *sources.KimaiDataset, ninja *sources.NinjaDataset, today time.Time) []Fact {
	var line []Fact
	work, workOK, pay, payOK := metrics.FlowDays(kimai, ninja, today)
	if workOK {
		line = append(line, Fact{Label: T("detail.money.work_age"), Value: TxtA("detail.days", "n", int(work+0.5))})
	}
	if payOK {
		line = append(line, Fact{Label: T("detail.money.pay_days"), Value: TxtA("detail.days", "n", int(pay+0.5))})
	}
	return line
}

// receiptsPage is Andon's receipt matching (Invoice Ninja ↔ Paperless).
const receiptsPage = "/receipts"

// cashLateDays is how much later the scenario lets an invoice be paid.
const cashLateDays = 30

func abs(v float64) float64 {
	if v < 0 {
		return -v
	}
	return v
}

// textDays is a label with a day count.
func textDays(key string, days int) Text { return Text{Key: key, Args: map[string]any{"n": days}} }

// invoiceAgingDetail: the open invoices by overdue band and one by one.
func invoiceAgingDetail(cfg AgingConfig, data *sources.NinjaDataset, ctx ViewCtx, results map[string]any) DetailView {
	view := invoiceAgingView(cfg, data, ctx)
	bands, _ := view["Bands"].([]AgingBand)
	total, _ := view["Total"].(float64)
	today := todayOf(ctx)
	currency := data.Currency
	var bars []ShareBar
	for _, b := range bands {
		bars = append(bars, ShareBar{Name: TxtA("detail.invoices.band_"+b.Key, "from", b.From, "to", b.To, "over", b.Over), Pct: float64(b.Pct), Value: Money(b.Amount, currency), Tier: b.Tier})
	}
	var rows [][]Cell
	overdue, oldest := 0.0, 0
	for _, inv := range metrics.NinjaOpenInvoices(data, today) {
		state := ""
		if inv.OverdueDays > 0 {
			state, overdue = "warn", overdue+inv.Balance
			if inv.OverdueDays > cfg.Old {
				state = "bad"
			}
		}
		oldest = max(oldest, inv.OverdueDays)
		reminder := any("–")
		switch {
		case inv.Reminded != "":
			reminder = TxtA("detail.invoices.reminded", "day", DayS(inv.Reminded))
		case inv.OverdueDays > 0 && inv.NextSend != "":
			reminder = TxtA("detail.invoices.next_reminder", "day", DayS(inv.NextSend))
		case inv.OverdueDays > 0:
			reminder = Txt("detail.invoices.not_reminded")
		}
		rows = append(rows, []Cell{{Value: inv.Number}, {Value: inv.Client}, {Value: DayS(inv.DueDate)}, {Value: Money(inv.Balance, currency)},
			{Value: TxtA("detail.days", "n", max(inv.OverdueDays, 0)), State: state}, {Value: reminder, State: stateIf(inv.OverdueDays > 0 && inv.Reminded == "" && inv.NextSend == "", "warn")}})
	}
	body := &DetailBody{
		Side: []Fact{{Label: T("detail.invoices.open"), Value: TxtA("detail.invoices.n_of", "n", len(rows), "amount", Money(total, currency))},
			{Label: T("detail.invoices.overdue"), Value: Money(overdue, currency), State: stateIf(overdue > 0, "bad")}},
		Facts: []Kpi{{Value: Money(total, currency), Label: T("detail.invoices.open")}, {Value: Money(overdue, currency), Label: T("detail.invoices.overdue"), Tier: tierIf(overdue > 0, "red", "green")},
			{Value: TxtA("detail.days", "n", oldest), Label: T("detail.invoices.oldest"), Tier: tierIf(oldest > cfg.Old, "red", "")}},
		Blocks: []Block{{Kind: BlockBars, Label: T("detail.invoices.by_band"), Data: bars}},
	}
	if len(rows) > 0 {
		body.Blocks = append(body.Blocks, Block{Kind: BlockTable, Label: T("detail.invoices.list"), Data: Table{Head: []Text{T("detail.invoices.number"),
			T("detail.invoices.client"), T("detail.invoices.due"), T("detail.invoices.balance"), T("detail.invoices.late"), T("detail.invoices.reminder")}, Rows: rows, Num: []int{3, 4}}})
	}
	body.Blocks = append(body.Blocks, hintsBlock(results)...)
	return DetailView{Head: DetailHead{Actions: []DetailAction{{LabelKey: "detail.open_in", Href: data.URL, Primary: true}}}, Body: body}
}

// moneyFlowDetail: work to payment by stage, paid per month.
func moneyFlowDetail(cfg MoneyFlowConfig, results map[string]any, ctx ViewCtx) DetailView {
	ninja, ok := results["data"].(*sources.NinjaDataset)
	if !ok {
		return DetailView{Body: &DetailBody{}}
	}
	kimai, _ := results[peerKimai].(*sources.KimaiDataset)
	today := todayOf(ctx)
	f := metrics.MoneyFlowOf(kimai, ninja, today)
	currency := f.Currency
	stages := []struct {
		key    string
		amount float64
		tier   string
	}{{"unbilled", f.Unbilled, ""}, {"drafts", f.Drafts, ""}, {"sent", f.Sent, ""}, {"overdue", f.Overdue, "red"}}
	top := 1.0
	for _, s := range stages {
		top = max(top, s.amount)
	}
	var bars []ShareBar
	for _, s := range stages {
		bars = append(bars, ShareBar{Name: Txt("flow." + s.key), Pct: s.amount / top * percentScale, Value: Money(s.amount, currency), Tier: s.tier})
	}
	var paid []float64
	var ticks []any
	for _, m := range metrics.NinjaByMonth(ninja, today, moneyDetailMonths) {
		paid = append(paid, m.Net)
	}
	if len(paid) > 0 {
		ticks = []any{Txt("detail.money.year_ago"), Txt("detail.money.this_month")}
	}
	body := &DetailBody{
		Side:   []Fact{{Label: T("detail.money.on_way"), Value: Money(f.Total(), currency)}, {Label: T("detail.money.paid30"), Value: Money(f.Paid, currency)}},
		Line:   flowLine(kimai, ninja, today),
		Facts:  []Kpi{{Value: Money(f.Unbilled, currency), Label: T("flow.unbilled"), Tier: tierIf(f.Unbilled > 0, "yellow", "")}, {Value: Money(f.Sent, currency), Label: T("flow.sent")}, {Value: Money(f.Paid, currency), Label: T("detail.money.paid30"), Tier: "green"}},
		Blocks: []Block{{Kind: BlockBars, Label: T("detail.money.stages"), Data: bars}},
	}
	if len(paid) > 0 {
		g := ColGraph(paid, "s4")
		g.Ticks = ticks
		body.Blocks = append(body.Blocks, Block{Kind: BlockGraph, Label: T("detail.money.per_month"), Data: g})
	}
	return DetailView{Body: body}
}

// paymentDaysDetail (list and detail): clients by typical days to pay, the
// chosen one with every invoice.
func paymentDaysDetail(cfg PaymentDaysConfig, data *sources.NinjaDataset, ctx ViewCtx, results map[string]any) DetailView {
	if cfg.Target == 0 {
		cfg.Target = defaultPayTarget
	}
	if cfg.Limit == 0 {
		cfg.Limit = defaultPayRows
	}
	view := paymentDaysView(cfg, data, ctx)
	rows, _ := view["Rows"].([]PayRow)
	center := metrics.CenterOf(ctx.Settings)
	gaps := metrics.NinjaPaymentGapsSince(data, time.Time{})
	list := &ObjList{Label: T("detail.paydays.clients")}
	names := make([]string, len(rows))
	for i, r := range rows {
		names[i] = r.Client
		list.Items = append(list.Items, LitRow{Name: r.Client, Meta: TxtA("detail.days", "n", r.Typical), State: tierIf(r.Late, "warn", "ok"), Item: r.Client})
	}
	body := &DetailBody{Line: []Fact{{Label: T("detail.paydays.target"), Value: TxtA("detail.days", "n", cfg.Target)}, {Label: T("detail.paydays.center"), Value: Txt("detail.center." + string(center))}}, List: list}
	if len(rows) > 0 {
		list.Sel = pickIndex(results, names)
		r := rows[list.Sel]
		list.Title, list.State, list.StateText = r.Client, tierIf(r.Late, "warn", "ok"), textDays("detail.paydays.typical", r.Typical)
		// A year of the client's invoices: what a discount for fast payment
		// would cost, what interest on the late days would bring.
		for _, c := range data.Clients {
			if c.Name != r.Client {
				continue
			}
			terms := metrics.NinjaPayTerms(data, c.ID, cfg.Target, todayOf(ctx).AddDate(-1, 0, 0))
			body.Line = append(body.Line, Fact{Label: T("detail.paydays.revenue_year"), Value: Money(terms.Revenue, data.Currency)},
				Fact{Label: textArgs("detail.paydays.discount", "pct", payDiscountPct), Value: Money(terms.Discount(payDiscountPct/percentScale), data.Currency)},
				Fact{Label: textArgs("detail.paydays.interest", "pct", payInterestPct), Value: Money(terms.Interest(payInterestPct/percentScale), data.Currency), State: stateIf(terms.LateAmounts > 0, "warn")})
		}
		var days []float64
		for id, l := range gaps {
			if metrics.NinjaClientName(data, id) == r.Client {
				for _, g := range l {
					days = append(days, float64(g))
				}
			}
		}
		states := make([]string, len(days))
		for i, d := range days {
			states[i] = tierIf(d > float64(cfg.Target), "warn", "ok")
		}
		g := ColGraph(days, "s4")
		g.States, g.Goal, g.HasGoal = states, float64(cfg.Target), true
		body.Facts = []Kpi{{Value: TxtA("detail.days", "n", r.Typical), Label: T("detail.paydays.typical_label"), Tier: tierIf(r.Late, "yellow", "green")}, {Value: r.Count, Label: T("detail.paydays.invoices")}}
		body.Blocks = append(body.Blocks, Block{Kind: BlockGraph, Label: T("detail.paydays.per_invoice"), Data: g})
	}
	body.Blocks = append(body.Blocks, hintsBlock(results)...)
	return DetailView{Body: body}
}

// rateTrendDetail (record without the facts column): the effective rate
// over two years against the target.
func rateTrendDetail(cfg RateTrendConfig, results map[string]any, ctx ViewCtx) DetailView {
	ninja, ok := results["data"].(*sources.NinjaDataset)
	kimai, ok2 := results[peerKimai].(*sources.KimaiDataset)
	if !ok || !ok2 {
		return DetailView{Body: &DetailBody{Blocks: hintsBlock(results)}}
	}
	lastMonth := metrics.AddMonths(todayOf(ctx), -1)
	kind := metrics.HoursAll
	if cfg.BillableOnly {
		kind = metrics.HoursBillable
	}
	hours := kimaiMonthHoursOf(kimai, lastMonth, rateDetailMonths, kind)
	var rates, revenue []float64
	for i, m := range metrics.NinjaByMonth(ninja, lastMonth, rateDetailMonths) {
		rate := Gap
		if i < len(hours) && hours[i] > 0 {
			rate = m.Net / hours[i]
		}
		rates, revenue = append(rates, rate), append(revenue, m.Net)
	}
	body := &DetailBody{Line: []Fact{{Label: T("detail.rate.month"), Value: lastMonth.Format("01/2006")}}}
	if cfg.Target > 0 {
		body.Line = append(body.Line, Fact{Label: T("detail.rate.target"), Value: Money(cfg.Target, ninja.Currency)})
	}
	if n := len(rates); n > 0 && rates[n-1] == rates[n-1] {
		body.Facts = []Kpi{{Value: Money(rates[n-1], ninja.Currency), Label: T("detail.rate.now"), Tier: tierIf(cfg.Target > 0 && rates[n-1] < cfg.Target, "yellow", "green")}}
		if n > 12 && rates[n-13] == rates[n-13] {
			body.Facts = append(body.Facts, Kpi{Value: Money(rates[n-13], ninja.Currency), Label: T("detail.rate.year_ago")})
		}
	}
	if hasValues(rates) {
		g := LineGraph(Series{Values: rates, Class: "s1"})
		g.Ticks = []any{Txt("detail.rate.two_years"), lastMonth.Format("01/2006")}
		if cfg.Target > 0 {
			g.Goal, g.HasGoal = cfg.Target, true
		}
		body.Blocks = append(body.Blocks, Block{Kind: BlockGraph, Label: T("detail.rate.per_month"), Hero: true, Data: g})
		cols := ColGraph(revenue, "s4")
		cols.Ticks = g.Ticks
		body.Blocks = append(body.Blocks, Block{Kind: BlockGraph, Label: T("detail.rate.revenue"), Data: cols})
		var fixed [][]Cell
		for _, f := range metrics.FixedRates(kimai) {
			fixed = append(fixed, []Cell{{Value: f.Project}, {Value: Money(f.Budget, ninja.Currency)}, {Value: NumU(f.Hours, 1, "h")},
				{Value: Money(f.Rate, ninja.Currency), State: stateIf(cfg.Target > 0 && f.Rate < cfg.Target, "warn")}})
		}
		if len(fixed) > 0 {
			body.Blocks = append(body.Blocks, Block{Kind: BlockTable, Label: T("detail.rate.fixed"),
				Data: Table{Head: []Text{T("detail.rate.project"), T("detail.rate.budget"), T("detail.rate.hours"), T("detail.rate.per_hour")}, Rows: fixed, Num: []int{1, 2, 3}}})
		}
	} else {
		body.Blocks = append(body.Blocks, Block{Kind: BlockText, Data: Txt("detail.rate.none")})
	}
	body.Blocks = append(body.Blocks, hintsBlock(results)...)
	return DetailView{Body: body}
}

// mailInvoicesDetail (list and detail): the invoice mails, the newest open
// one chosen with its attachments.
func mailInvoicesDetail(cfg MailConfig, results map[string]any, ctx ViewCtx) DetailView {
	data, ok := results["data"].(*sources.MailDataset)
	if !ok {
		return DetailView{Body: &DetailBody{Blocks: hintsBlock(results)}}
	}
	sent, _ := results[ForwardedSlot].(map[uint32]bool)
	mails := append([]sources.MailInvoice(nil), data.Invoices...)
	sort.Slice(mails, func(a, b int) bool { return mails[a].Date.After(mails[b].Date) })
	list := &ObjList{Label: T("detail.mail.invoices")}
	open := 0
	sum := 0.0
	chosen := -1
	for i, m := range mails {
		state := "ok"
		if !sent[m.UID] {
			state, open = "warn", open+1
			if chosen < 0 {
				chosen = i
			}
		}
		sum += m.Amount
		list.Items = append(list.Items, LitRow{Name: m.Sender, Meta: Day(m.Date), State: state, Item: strconv.FormatUint(uint64(m.UID), 10)})
	}
	body := &DetailBody{Line: []Fact{{Label: T("detail.mail.box"), Value: data.Mailbox}, {Label: T("detail.mail.scanned"), Value: data.Scanned}}}
	if len(mails) == 0 {
		body.Blocks = []Block{{Kind: BlockText, Data: Txt("detail.mail.none")}}
		return DetailView{Body: body}
	}
	if chosen < 0 {
		chosen = 0
	}
	for i, m := range mails {
		if strconv.FormatUint(uint64(m.UID), 10) == pickedItem(results) {
			chosen = i
		}
	}
	m := mails[chosen]
	list.Sel, list.Title, list.Sub = chosen, m.Subject, m.Addr
	list.State, list.StateText = list.Items[chosen].State, T("detail.mail.state_"+list.Items[chosen].State)
	body.List = list
	amount := any("–")
	if m.Amount > 0 {
		amount = Money(m.Amount, "EUR")
	}
	body.Facts = []Kpi{{Value: open, Label: T("detail.mail.open"), Tier: tierIf(open > 0, "yellow", "green")}, {Value: amount, Label: T("detail.mail.amount")}}
	body.Blocks = append(body.Blocks, Block{Kind: BlockTable, Data: Table{Head: []Text{T("detail.exposure.what"), T("detail.exposure.value")}, Rows: [][]Cell{
		{{Value: Txt("detail.mail.from")}, {Value: m.Sender + " · " + m.Addr}}, {{Value: Txt("detail.mail.date")}, {Value: Day(m.Date)}},
		{{Value: Txt("detail.mail.files")}, {Value: strings.Join(m.Attachments, ", ")}}}}})
	// The first attachment as the mail brought it (a PDF the browser shows).
	if len(m.Attachments) > 0 {
		body.Blocks = append(body.Blocks, Block{Kind: BlockFrame, Label: Plain(m.Attachments[0]), Data: Embed{File: "uid=" + strconv.FormatUint(uint64(m.UID), 10) + "&n=0"}})
	}
	body.Blocks = append(body.Blocks, hintsBlock(results)...)
	return DetailView{Body: body}
}

// paperlessDetail: the inbox with its newest documents, contracts with a
// notice deadline, the document count over time.
func paperlessDetail(cfg PaperlessConfig, data *sources.PaperlessDataset, ctx ViewCtx, results map[string]any) DetailView {
	today := todayOf(ctx)
	body := &DetailBody{Side: []Fact{{Label: T("detail.paperless.docs"), Value: Num(float64(data.Total), 0)}, {Label: T("detail.paperless.inbox"), Value: data.Inbox, State: stateIf(data.Inbox > 0, "warn")}}}
	oldest := 0
	if added, ok := metrics.ParseDay(data.OldestAdded); ok && data.Inbox > 0 {
		oldest = int(today.Sub(added).Hours() / hoursPerDay)
		body.Side = append(body.Side, Fact{Label: T("detail.paperless.oldest"), Value: TxtA("detail.paperless.oldest_doc", "title", data.OldestTitle, "n", oldest)})
	}
	late := float64(oldest) > rules.Setting(ctx.Settings, "paperless.inbox", "warn_days")
	body.Facts = []Kpi{{Value: data.Inbox, Label: T("detail.paperless.inbox"), Tier: tierIf(data.Inbox > 0, "yellow", "green")},
		{Value: TxtA("detail.days", "n", oldest), Label: T("detail.paperless.oldest_label"), Tier: tierIf(late, "yellow", "")}}
	warn, info := expiryLimits(ctx.Settings, "paperless.contract_notice")
	var newest [][]Cell
	for _, d := range data.Newest {
		newest = append(newest, []Cell{{Value: DayS(d.Added)}, {Value: d.Title}})
	}
	var deadlines []Event
	for _, c := range data.Contracts {
		if c.Deadline.IsZero() {
			continue
		}
		left := int(c.Deadline.Sub(today).Hours() / hoursPerDay)
		deadlines = append(deadlines, Event{At: c.Deadline, Title: c.Title, Sub: c.Correspondent, State: TxtA("detail.days", "n", left),
			Tier: cmp.Or(dueTier(left, warn, info), "cyan")})
	}
	sort.Slice(deadlines, func(a, b int) bool { return deadlines[a].At.Before(deadlines[b].At) })
	pair := []Block{}
	if len(newest) > 0 {
		pair = append(pair, Block{Kind: BlockTable, Label: T("detail.paperless.newest"), Data: Table{Head: []Text{T("detail.paperless.added"), T("detail.paperless.title")}, Rows: newest}})
	}
	if len(deadlines) > 0 {
		pair = append(pair, Block{Kind: BlockTimeline, Label: T("detail.paperless.notice"), Data: deadlines})
	}
	if thumbs, ok := results[openName].(*sources.PaperlessThumbs); ok && len(thumbs.List) > 0 {
		var pics []Image
		for _, t := range thumbs.List {
			pics = append(pics, Image{DataURI: t.DataURI, Alt: t.Title, Caption: t.Title})
		}
		body.Blocks = append(body.Blocks, Block{Kind: BlockThumbs, Label: T("detail.paperless.inbox_pages"), Data: pics})
	}
	body.Blocks = append(body.Blocks, pairOf(pair)...)
	if docs := dailySeries(historyOf(results), metrics.SampleKey("paperless", "docs"), today, historyDetailDays); hasValues(docs) {
		g := LineGraph(Series{Values: docs, Class: "s1"})
		g.Ticks = spanTicks(today, historyDetailDays)
		body.Blocks = append(body.Blocks, Block{Kind: BlockGraph, Label: T("detail.paperless.growth"), Data: g})
	}
	body.Blocks = append(body.Blocks, hintsBlock(results)...)
	return DetailView{Head: DetailHead{Actions: []DetailAction{{LabelKey: "detail.open_in", Href: data.URL, Primary: true}}}, Body: body}
}

// receiptsDetail (list and detail): expenses without a receipt, the
// largest chosen with a search in Paperless.
func receiptsDetail(cfg ReceiptsConfig, results map[string]any, ctx ViewCtx) DetailView {
	if cfg.Days == 0 {
		cfg.Days = defaultReceiptDays
	}
	cfg.Limit = 1 << 10
	view := receiptsView(cfg, results, ctx)
	rows, _ := view["Rows"].([]ReceiptRow)
	currency, _ := view["Currency"].(string)
	if view["Setup"] == true {
		return DetailView{Body: &DetailBody{Blocks: []Block{{Kind: BlockText, Data: Txt("close.receipts_setup")}}}}
	}
	list := &ObjList{Label: T("detail.receipts.missing")}
	keys := make([]string, len(rows))
	for i, r := range rows {
		keys[i] = r.Date + " " + r.Name
		list.Items = append(list.Items, LitRow{Name: r.Name, Meta: Money(r.Amount, currency), State: "warn", Item: keys[i]})
	}
	body := &DetailBody{Line: []Fact{{Label: T("detail.receipts.span"), Value: TxtA("detail.days", "n", cfg.Days)}, {Label: T("detail.receipts.sum"), Value: Money(asF(view["Sum"]), currency)}}}
	if len(rows) == 0 {
		body.Blocks = []Block{{Kind: BlockText, Data: Txt("detail.receipts.none")}}
		return DetailView{Body: body}
	}
	list.Sel = pickIndex(results, keys)
	r := rows[list.Sel]
	list.Title, list.Sub, list.State, list.StateText = r.Name, r.Account, "warn", T("detail.receipts.no_receipt")
	body.List = list
	tasks := Tasks{Label: T("detail.receipts.found"), Total: 2, Items: []Task{
		{Text: TxtA("detail.receipts.look", "name", r.Name), Meta: DayS(r.Date), State: "warn", Action: T("detail.receipts.search"), Href: r.Search},
		{Text: Txt("detail.receipts.match"), Meta: Txt("detail.receipts.match_how"), State: "info", Action: T("detail.receipts.match_open"), Href: receiptsPage}}}
	body.Facts = []Kpi{{Value: Money(r.Amount, currency), Label: T("detail.receipts.amount")}, {Value: DayS(r.Date), Label: T("detail.receipts.date")}}
	body.Blocks = append(body.Blocks, Block{Kind: BlockTasks, Data: tasks})
	body.Blocks = append(body.Blocks, hintsBlock(results)...)
	return DetailView{Body: body}
}

// subsDetail: the subscriptions with their next debit, by category.
func subsDetail(cfg SubsConfig, results map[string]any, ctx ViewCtx) DetailView {
	cfg.Limit = 1 << 10
	view := subsView(cfg, results, ctx)
	rows, _ := view["Rows"].([]SubRow)
	currency, _ := view["Currency"].(string)
	if currency == "" {
		currency = "EUR"
	}
	if len(rows) == 0 {
		return DetailView{Body: &DetailBody{Blocks: []Block{{Kind: BlockText, Data: Txt("detail.subs.none")}}}}
	}
	byCat := map[string]float64{}
	total := 0.0
	var table [][]Cell
	for _, r := range rows {
		cat := r.Category
		if cat == "" {
			cat = "–"
		}
		byCat[cat] += r.Monthly
		total += r.Monthly
		table = append(table, []Cell{{Value: r.Name}, {Value: cat}, {Value: dayOrDash(r.Next)}, {Value: Money(r.Monthly, currency)}})
	}
	cats := make([]string, 0, len(byCat))
	for c := range byCat {
		cats = append(cats, c)
	}
	sort.Slice(cats, func(a, b int) bool { return byCat[cats[a]] > byCat[cats[b]] })
	var bars []ShareBar
	for _, c := range cats {
		bars = append(bars, ShareBar{Name: c, Pct: byCat[c] / max(byCat[cats[0]], 1) * percentScale, Value: Money(byCat[c], currency)})
	}
	body := &DetailBody{
		Side:  []Fact{{Label: T("detail.subs.count"), Value: len(rows)}, {Label: T("detail.subs.monthly"), Value: Money(total, currency)}, {Label: T("detail.subs.yearly"), Value: Money(total*monthsPerYearF, currency)}},
		Facts: []Kpi{{Value: Money(total, currency), Label: T("detail.subs.monthly")}, {Value: Money(total*monthsPerYearF, currency), Label: T("detail.subs.yearly")}},
		Blocks: []Block{{Kind: BlockTable, Label: T("detail.subs.list"), Data: Table{Head: []Text{T("detail.subs.name"), T("detail.subs.category"), T("detail.subs.next"), T("detail.subs.per_month")}, Rows: table, Num: []int{3}}},
			{Kind: BlockBars, Label: T("detail.subs.by_category"), Data: bars}},
	}
	if missing, _ := view["Missing"].(string); missing != "" {
		body.Side = append(body.Side, Fact{Label: T("detail.subs.not_in_wallos"), Value: missing, State: "warn"})
	}
	var changes []Event
	if h := historyOf(results); h != nil {
		for _, e := range h.Events {
			if e.Kind == metrics.EventChange && strings.HasPrefix(e.Subject, metrics.SubscriptionSubject("")) {
				changes = append(changes, Event{At: e.At, Title: strings.TrimPrefix(e.Subject, metrics.SubscriptionSubject("")), Sub: e.Detail, State: agoOf(e.At), Tier: "yellow"})
			}
		}
	}
	if len(changes) > 0 {
		body.Blocks = append(body.Blocks, Block{Kind: BlockTimeline, Label: T("detail.subs.price_changes"), Data: changes})
	}
	body.Blocks = append(body.Blocks, hintsBlock(results)...)
	return DetailView{Body: body}
}

// dayOrDash is a "2026-10-01" date as a typed value, "–" for none.
func dayOrDash(day string) any {
	if day == "" {
		return "–"
	}
	return DayS(day)
}
