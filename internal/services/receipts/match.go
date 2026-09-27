package receipts

// The matcher scores an expense against a scan, PaperNinja's way:
//
//	amount   40  within the tolerance; 15 if close (≤ 10× tolerance, at least 1)
//	date     25  within ±7 days, closer = more (10…25); 5 up to ±14 days
//	vendor   20  fuzzy ≥ 85 % against correspondent or title; 10 at ≥ 60 %
//	invoice  30  the expense's invoice number or number in field, title or OCR
//
// capped at 100; only suggestions from 40 on are shown. A factor that
// cannot apply (no invoice number) is left out and the score taken of
// the points still reachable: amount, date and vendor alone reach 100.
//
// A 1∶n combo (2–4 scans adding up to the expense) is looked for when no
// single scan has the exact amount; the scans it covers fold away. A
// match is sure when its best scan has the exact amount, 90 points or
// more and no second scan within 15. A 1∶n combo is a
// set of 2–4 scans whose amounts add up to the expense.

import (
	"math"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"andon/internal/sources"
)

const (
	dateWindow     = 7 // days
	amountTol      = 0.02
	minScore       = 40
	topN           = 5
	amountMax      = 40
	amountNear     = 15
	dateMax        = 25
	dateBase       = 10
	dateWide       = 5
	vendorMax      = 20
	vendorPart     = 10
	vendorStrong   = 85
	vendorSimilar  = 60
	invoiceMax     = 30
	minNeedle      = 3
	ocrScan        = 2000 // runes of OCR text searched for the invoice number
	titleShown     = 80
	comboMaxSize   = 4
	comboPool      = 20
	comboMaxFound  = 24
	comboTop       = 3
	nearTolFactor  = 10
	nearTolMinimum = 1.0
	sureScore      = 90
	sureGap        = 15
	levelHigh      = 80
	levelMid       = 60
)

// Factor keys.
const (
	FactorAmount  = "amount"
	FactorDate    = "date"
	FactorVendor  = "vendor"
	FactorInvoice = "invoice"
)

// Mapping says where the link lives on both sides. Slots are Invoice
// Ninja's custom_value1–4 (0 = unset), fields Paperless custom field ids.
type Mapping struct {
	InvoiceSlot, LinkSlot                              int
	FieldInvoice, FieldExpense, FieldLink, FieldAmount int64
	QueueTag                                           string
}

// Complete tells whether links can be written (the amount field and the
// queue tag are optional).
func (m Mapping) Complete() bool {
	return m.InvoiceSlot > 0 && m.LinkSlot > 0 && m.FieldInvoice > 0 && m.FieldExpense > 0 && m.FieldLink > 0
}

// Factor is one signal's points with the reason, as an i18n key
// ("receipts.why_amount_exact") and its params.
type Factor struct {
	Key         string
	Points, Max int
	Hit         bool
	Off         bool // cannot apply here, left out of the score
	Text        string
	Args        map[string]any
}

// Width is the factor's bar in percent.
func (f Factor) Width() int { return f.Points * full / max(f.Max, 1) }

// Candidate is one scan suggested for an expense.
type Candidate struct {
	Doc     sources.ReceiptDoc
	Amount  float64 // the scan's amount as found, 0 = none
	Score   int
	Factors []Factor
}

// Level is the score's band: "high" from 80, "mid" from 60, else "low".
func (c Candidate) Level() string { return level(c.Score) }

// Open tells whether the reasons unfold by themselves: weak scores only.
func (c Candidate) Open() bool { return c.Score < levelHigh-10 }

func level(score int) string {
	switch {
	case score >= levelHigh:
		return "high"
	case score >= levelMid:
		return "mid"
	}
	return "low"
}

// Combo is a set of scans whose amounts add up to an expense.
type Combo struct {
	Docs    []sources.ReceiptDoc
	Amounts []float64
	Sum     float64
	Score   int
	Factors []Factor
}

// Level is the score's band, as for a single scan.
func (c Combo) Level() string { return level(c.Score) }

// IDs lists the combo's document ids, "11,12".
func (c Combo) IDs() string {
	ids := make([]string, len(c.Docs))
	for i, d := range c.Docs {
		ids[i] = strconv.FormatInt(d.ID, 10)
	}
	return strings.Join(ids, ",")
}

// ExpenseMatch is an unlinked expense with its best scans; Covered are
// single scans the best combo already holds.
type ExpenseMatch struct {
	Expense    sources.ReceiptExpense
	Candidates []Candidate
	Combos     []Combo
	Covered    []Candidate
}

// Sure tells whether the best scan leaves no doubt: exact amount, a high
// score, no second scan close to it and no combo.
func (m ExpenseMatch) Sure() bool {
	if len(m.Candidates) == 0 || len(m.Combos) > 0 {
		return false
	}
	top := m.Candidates[0]
	if top.Score < sureScore || !exactAmount(top.Factors) {
		return false
	}
	return len(m.Candidates) == 1 || m.Candidates[1].Score <= top.Score-sureGap
}

func exactAmount(factors []Factor) bool {
	return slices.ContainsFunc(factors, func(f Factor) bool { return f.Key == FactorAmount && f.Points == amountMax })
}

// DocMatch is an unlinked scan with its best expenses, alone or as part
// of a combo.
type DocMatch struct {
	Doc    sources.ReceiptDoc
	Amount float64 // the scan's own amount, 0 = unknown
	Hits   []ExpenseHit
	Combos []ComboHit
}

// ComboHit is an expense a scan pays together with others.
type ComboHit struct {
	Expense sources.ReceiptExpense
	Combo
}

// ExpenseHit is one expense suggested for a scan.
type ExpenseHit struct {
	Expense sources.ReceiptExpense
	Candidate
}

type matcher struct {
	mapping Mapping
	aliases Aliases
}

// ── amounts ──

// amountPattern finds amounts like "42,50", "1.234,56", "1 234.56".
var amountPattern = regexp.MustCompile(`\d{1,3}(?:[.\s]\d{3})*[,.]\d{2}|\d+[,.]\d{2}`)

// parseAmount reads German and English notation: "1.234,56", "EUR 42.50".
func parseAmount(v any) (float64, bool) {
	switch t := v.(type) {
	case float64:
		return t, true
	case int:
		return float64(t), true
	case string:
		return parseAmountText(t)
	}
	return 0, false
}

func parseAmountText(text string) (float64, bool) {
	text = strings.Map(func(r rune) rune {
		if (r >= '0' && r <= '9') || r == ',' || r == '.' || r == '-' {
			return r
		}
		return -1
	}, text)
	if text == "" {
		return 0, false
	}
	comma, dot := strings.LastIndex(text, ","), strings.LastIndex(text, ".")
	switch {
	case comma >= 0 && dot >= 0 && comma > dot:
		text = strings.ReplaceAll(strings.ReplaceAll(text, ".", ""), ",", ".")
	case comma >= 0 && dot >= 0:
		text = strings.ReplaceAll(text, ",", "")
	case comma >= 0 && len(text)-comma-1 == 2:
		text = strings.ReplaceAll(text, ",", ".")
	case comma >= 0:
		text = strings.ReplaceAll(text, ",", "")
	}
	f, err := strconv.ParseFloat(text, 64)
	return f, err == nil
}

// amountsIn lists the amounts written in a text, digits on either side
// ruling a hit out ("142,501" is no amount).
func amountsIn(text string) []float64 {
	var out []float64
	for _, loc := range amountPattern.FindAllStringIndex(text, -1) {
		if (loc[0] > 0 && isDigit(text[loc[0]-1])) || (loc[1] < len(text) && isDigit(text[loc[1]])) {
			continue
		}
		if f, ok := parseAmountText(text[loc[0]:loc[1]]); ok {
			out = append(out, f)
		}
	}
	return out
}

func isDigit(b byte) bool { return b >= '0' && b <= '9' }

func money(v float64) map[string]any { return map[string]any{"$money": v} }

// text is a param shown as the text of an i18n key.
func text(key string) map[string]any { return map[string]any{"$t": key} }

func dayArg(v string) any {
	if v == "" {
		return "–"
	}
	return map[string]any{"$day": v}
}

// ── dates ──

func docDay(d sources.ReceiptDoc) string {
	if d.Day != "" {
		return d.Day
	}
	return d.Added
}

// daysApart is |a − b| in days; false if either is unknown.
func daysApart(a, b string) (int, bool) {
	ta, errA := time.Parse(time.DateOnly, a)
	tb, errB := time.Parse(time.DateOnly, b)
	if errA != nil || errB != nil {
		return 0, false
	}
	return int(math.Abs(ta.Sub(tb).Hours()) / 24), true
}

// inWindow keeps pairs within twice the date window; unknown dates pass.
func inWindow(e sources.ReceiptExpense, d sources.ReceiptDoc) bool {
	apart, ok := daysApart(e.Day, docDay(d))
	return !ok || apart <= 2*dateWindow
}

// datePoints: 10…25 within the window (closer = more), 5 up to twice it.
func datePoints(apart int) int {
	switch {
	case apart <= dateWindow:
		return dateBase + int(float64(dateMax-dateBase)*(1-float64(apart)/dateWindow))
	case apart <= 2*dateWindow:
		return dateWide
	}
	return 0
}

// ── scoring ──

func (x matcher) score(e sources.ReceiptExpense, d sources.ReceiptDoc) Candidate {
	factors := []Factor{x.amountFactor(e, d), dateFactor(e, d), x.vendorFactor(e, []sources.ReceiptDoc{d}, false), x.invoiceFactor(e, d)}
	amount, _, _ := x.docAmount(e, d)
	return Candidate{Doc: d, Amount: amount, Score: total(factors), Factors: factors}
}

// total adds the points, taken of the reachable ones: 100 at most, and
// factors that are off do not count against it.
func total(factors []Factor) int {
	sum, reachable := 0, 0
	for _, f := range factors {
		sum += f.Points
		if !f.Off {
			reachable += f.Max
		}
	}
	if reachable = min(reachable, full); reachable == 0 {
		return 0
	}
	return min(int(math.Round(float64(sum*full)/float64(reachable))), full)
}

// docAmount is the scan's amount: the amount field, else the first amount
// in title or OCR text close to the expense.
func (x matcher) docAmount(e sources.ReceiptExpense, d sources.ReceiptDoc) (float64, string, bool) {
	if x.mapping.FieldAmount > 0 {
		if v, ok := parseAmount(d.Custom[x.mapping.FieldAmount]); ok {
			return v, "receipts.src_field", true
		}
	}
	near := max(amountTol*nearTolFactor, nearTolMinimum)
	for _, v := range amountsIn(d.Title + "\n" + d.Content) {
		switch delta := math.Abs(v - e.Amount); {
		case delta <= amountTol:
			return v, "receipts.src_ocr", true
		case delta <= near:
			return v, "receipts.src_ocr_near", true
		}
	}
	return 0, "", false
}

func (x matcher) amountFactor(e sources.ReceiptExpense, d sources.ReceiptDoc) Factor {
	f := Factor{Key: FactorAmount, Max: amountMax}
	found, source, ok := x.docAmount(e, d)
	if !ok {
		f.Text, f.Args = "receipts.why_amount_missing", map[string]any{"expense": money(e.Amount)}
		return f
	}
	delta := math.Abs(found - e.Amount)
	f.Args = map[string]any{"expense": money(e.Amount), "doc": money(found), "delta": money(delta), "source": text(source)}
	switch {
	case delta <= amountTol:
		f.Points, f.Hit, f.Text = amountMax, true, "receipts.why_amount_exact"
	case delta <= max(amountTol*nearTolFactor, nearTolMinimum):
		f.Points, f.Hit, f.Text = amountNear, true, "receipts.why_amount_near"
	default:
		f.Text = "receipts.why_amount_far"
	}
	return f
}

func dateFactor(e sources.ReceiptExpense, d sources.ReceiptDoc) Factor {
	f := Factor{Key: FactorDate, Max: dateMax, Args: map[string]any{"expense": dayArg(e.Day), "doc": dayArg(docDay(d))}}
	apart, ok := daysApart(e.Day, docDay(d))
	if !ok {
		f.Text = "receipts.why_date_missing"
		return f
	}
	f.Args["days"], f.Points = apart, datePoints(apart)
	f.Hit = f.Points > 0
	switch {
	case apart <= dateWindow:
		f.Text = "receipts.why_date_close"
	case f.Hit:
		f.Text = "receipts.why_date_wide"
	default:
		f.Text = "receipts.why_date_far"
	}
	return f
}

// vendorFactor compares the expense's vendor, and every name learned for
// it, with the scans' correspondents (token sets) and titles (partial).
func (x matcher) vendorFactor(e sources.ReceiptExpense, docs []sources.ReceiptDoc, combo bool) Factor {
	f := Factor{Key: FactorVendor, Max: vendorMax}
	vendor := strings.TrimSpace(e.Vendor)
	if vendor == "" {
		f.Text = "receipts.why_vendor_missing"
		return f
	}
	best, against, alias := 0.0, "", ""
	names := x.aliases.equivalents(vendor)
	for _, d := range docs {
		corr := x.aliases.equivalents(d.Correspondent)
		title := normalize(d.Title)
		for _, name := range names {
			for _, c := range corr {
				if r := tokenSetRatio(name, c); r >= best {
					best, against, alias = r, d.Correspondent, aliasOf(name, vendor, c, d.Correspondent)
				}
			}
			if title == "" {
				continue
			}
			if r := partialRatio(name, title); r >= best {
				best, against, alias = r, shorten(d.Title), aliasOf(name, vendor, "", "")
			}
		}
	}

	pct := int(math.Round(best))
	f.Args = map[string]any{"vendor": vendor, "against": against, "pct": pct, "alias": alias}
	switch {
	case pct >= vendorStrong:
		f.Points, f.Hit = vendorMax, true
	case pct >= vendorSimilar:
		f.Points, f.Hit = vendorPart, true
	}
	switch {
	case combo:
		f.Text = "receipts.why_vendor_combo"
	case against == "":
		f.Text = "receipts.why_vendor_none"
	case f.Hit && alias != "":
		f.Text = "receipts.why_vendor_alias"
	case f.Hit:
		f.Text = "receipts.why_vendor_hit"
	default:
		f.Text = "receipts.why_vendor_weak"
	}
	return f
}

// aliasOf is the learned name a comparison used on either side, "" if
// it compared the names themselves.
func aliasOf(name, vendor, corr, correspondent string) string {
	switch {
	case name != normalize(vendor):
		return name
	case corr != "" && corr != normalize(correspondent):
		return corr
	}
	return ""
}

func shorten(s string) string {
	if r := []rune(s); len(r) > titleShown {
		return string(r[:titleShown]) + "…"
	}
	return s
}

// invoiceFactor looks for the expense's invoice number, or its own
// number, in the scan's invoice field, title and OCR text. Without an
// invoice number the factor is off; the own number then only adds.
func (x matcher) invoiceFactor(e sources.ReceiptExpense, d sources.ReceiptDoc) Factor {
	f := Factor{Key: FactorInvoice, Max: invoiceMax}
	invoice := ""
	if x.mapping.InvoiceSlot > 0 {
		invoice = e.Custom[x.mapping.InvoiceSlot-1]
	}
	var needles []string
	if invoice != "" {
		needles = append(needles, invoice)
	}
	if e.Number != "" {
		needles = append(needles, e.Number)
	}
	field := ""
	if x.mapping.FieldInvoice > 0 {
		field, _ = d.Custom[x.mapping.FieldInvoice].(string)
	}
	ocr := []rune(d.Content)
	places := []struct{ text, where string }{
		{field, "receipts.where_field"}, {d.Title, "receipts.where_title"}, {string(ocr[:min(len(ocr), ocrScan)]), "receipts.where_ocr"},
	}
	for _, needle := range needles {
		if len([]rune(needle)) < minNeedle {
			continue
		}
		for _, p := range places {
			if p.text != "" && strings.Contains(strings.ToLower(p.text), strings.ToLower(needle)) {
				f.Points, f.Hit, f.Text = invoiceMax, true, "receipts.why_invoice_hit"
				f.Args = map[string]any{"needle": needle, "where": text(p.where)}
				return f
			}
		}
	}
	if invoice == "" {
		f.Off, f.Text = true, "receipts.why_invoice_none"
		return f
	}
	f.Text, f.Args = "receipts.why_invoice_miss", map[string]any{"needles": invoice}
	return f
}

// ── 1∶n combos ──

// comboAmount is one amount per scan: the amount field, else the OCR
// text's amount if it names only one.
func (x matcher) comboAmount(d sources.ReceiptDoc) (float64, bool) {
	if x.mapping.FieldAmount > 0 {
		if v, ok := parseAmount(d.Custom[x.mapping.FieldAmount]); ok {
			return v, true
		}
	}
	found := amountsIn(d.Title + "\n" + d.Content)
	for i := range found {
		found[i] = math.Round(found[i]*100) / 100
	}
	slices.Sort(found)
	if found = slices.Compact(found); len(found) == 1 {
		return found[0], true
	}
	return 0, false
}

type pooled struct {
	doc    sources.ReceiptDoc
	amount float64
}

// combos finds sets of 2–4 smaller scans adding up to the expense,
// the 20 largest candidates tried, the best three kept.
func (x matcher) combos(e sources.ReceiptExpense, docs []sources.ReceiptDoc) []Combo {
	target := e.Amount
	var pool []pooled
	for _, d := range docs {
		if !inWindow(e, d) {
			continue
		}
		v, ok := x.comboAmount(d)
		if !ok || v <= 0 || math.Abs(v-target) <= amountTol || v > target+amountTol {
			continue
		}
		pool = append(pool, pooled{d, v})
	}
	sort.SliceStable(pool, func(i, j int) bool { return pool[i].amount > pool[j].amount })
	pool = pool[:min(len(pool), comboPool)]

	var found [][]pooled
	var walk func(start int, chosen []pooled, sum float64)
	walk = func(start int, chosen []pooled, sum float64) {
		if len(found) >= comboMaxFound {
			return
		}
		if len(chosen) >= 2 && math.Abs(sum-target) <= amountTol {
			found = append(found, slices.Clone(chosen))
			return
		}
		if len(chosen) >= comboMaxSize {
			return
		}
		for i := start; i < len(pool); i++ {
			if next := sum + pool[i].amount; next <= target+amountTol {
				walk(i+1, append(chosen, pool[i]), next)
			}
		}
	}
	walk(0, nil, 0)

	var out []Combo
	for _, members := range found {
		if c := x.scoreCombo(e, members); c.Score >= minScore {
			out = append(out, c)
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Score != out[j].Score {
			return out[i].Score > out[j].Score
		}
		return len(out[i].Docs) < len(out[j].Docs)
	})
	return out[:min(len(out), comboTop)]
}

func (x matcher) scoreCombo(e sources.ReceiptExpense, members []pooled) Combo {
	c := Combo{}
	for _, m := range members {
		c.Docs, c.Amounts, c.Sum = append(c.Docs, m.doc), append(c.Amounts, m.amount), c.Sum+m.amount
	}
	c.Sum = math.Round(c.Sum*100) / 100
	amount := Factor{Key: FactorAmount, Max: amountMax, Points: amountMax, Hit: true, Text: "receipts.why_combo_amount",
		Args: map[string]any{"sum": money(c.Sum), "count": len(c.Docs), "expense": money(e.Amount)}}

	date := Factor{Key: FactorDate, Max: dateMax, Text: "receipts.why_date_missing", Args: map[string]any{"expense": dayArg(e.Day), "doc": "–"}}
	var points []int
	for _, d := range c.Docs {
		if apart, ok := daysApart(e.Day, docDay(d)); ok {
			points = append(points, datePoints(apart))
		}
	}
	if len(points) > 0 {
		sum := 0
		for _, p := range points {
			sum += p
		}
		date.Points = int(math.Round(float64(sum) / float64(len(points))))
		date.Hit, date.Text, date.Args = date.Points > 0, "receipts.why_combo_date", map[string]any{"count": len(c.Docs)}
	}

	invoice := Factor{Key: FactorInvoice, Max: invoiceMax, Off: true, Text: "receipts.why_combo_invoice"}
	c.Factors = []Factor{amount, date, x.vendorFactor(e, c.Docs, true), invoice}
	c.Score = total(c.Factors)
	return c
}

// ── lists ──

// Matches suggests scans for each expense, best-matched expenses first.
func (x matcher) Matches(expenses []sources.ReceiptExpense, docs []sources.ReceiptDoc) []ExpenseMatch {
	out := make([]ExpenseMatch, 0, len(expenses))
	for _, e := range expenses {
		out = append(out, x.match(e, docs))
	}
	best := func(m ExpenseMatch) (int, int) {
		combo, single := -1, -1
		if len(m.Combos) > 0 {
			combo = m.Combos[0].Score
		}
		if len(m.Candidates) > 0 {
			single = m.Candidates[0].Score
		}
		return combo, single
	}
	sort.SliceStable(out, func(i, j int) bool {
		ci, si := best(out[i])
		cj, sj := best(out[j])
		if ci != cj {
			return ci > cj
		}
		return si > sj
	})
	return out
}

// match scores the scans of one expense; combos only when no single
// scan has the exact amount, and the scans of the best combo fold away.
func (x matcher) match(e sources.ReceiptExpense, docs []sources.ReceiptDoc) ExpenseMatch {
	m := ExpenseMatch{Expense: e}
	exact := false
	for _, d := range docs {
		if !inWindow(e, d) {
			continue
		}
		c := x.score(e, d)
		exact = exact || exactAmount(c.Factors)
		if c.Score >= minScore {
			m.Candidates = append(m.Candidates, c)
		}
	}
	sort.SliceStable(m.Candidates, func(i, j int) bool { return m.Candidates[i].Score > m.Candidates[j].Score })
	if !exact {
		m.Combos = x.combos(e, docs)
	}
	if len(m.Combos) > 0 {
		inCombo := m.Combos[0].Docs
		m.Candidates = slices.DeleteFunc(m.Candidates, func(c Candidate) bool {
			if !slices.ContainsFunc(inCombo, func(d sources.ReceiptDoc) bool { return d.ID == c.Doc.ID }) {
				return false
			}
			m.Covered = append(m.Covered, c)
			return true
		})
	}
	m.Candidates = m.Candidates[:min(len(m.Candidates), topN)]
	return m
}

// Reverse suggests expenses for each scan, best-matched scans first; a
// scan that pays an expense together with others of pool (the year's
// unlinked scans) gets that combo.
func (x matcher) Reverse(docs []sources.ReceiptDoc, expenses []sources.ReceiptExpense, pool []sources.ReceiptDoc) []DocMatch {
	combos := map[int64][]ComboHit{}
	for _, e := range expenses {
		m := x.match(e, pool)
		for _, c := range m.Combos[:min(len(m.Combos), 1)] {
			for _, d := range c.Docs {
				combos[d.ID] = append(combos[d.ID], ComboHit{Expense: e, Combo: c})
			}
		}
	}
	out := make([]DocMatch, 0, len(docs))
	for _, d := range docs {
		m := DocMatch{Doc: d, Combos: combos[d.ID]}
		m.Amount, _ = x.comboAmount(d)
		for _, e := range expenses {
			if !inWindow(e, d) {
				continue
			}
			paid := slices.ContainsFunc(m.Combos, func(c ComboHit) bool { return c.Expense.Key == e.Key })
			if c := x.score(e, d); c.Score >= minScore && !paid {
				m.Hits = append(m.Hits, ExpenseHit{Expense: e, Candidate: c})
			}
		}
		sort.SliceStable(m.Hits, func(i, j int) bool { return m.Hits[i].Score > m.Hits[j].Score })
		m.Hits = m.Hits[:min(len(m.Hits), topN)]
		out = append(out, m)
	}
	top := func(m DocMatch) int {
		best := -1
		if len(m.Hits) > 0 {
			best = m.Hits[0].Score
		}
		if len(m.Combos) > 0 {
			best = max(best, m.Combos[0].Score)
		}
		return best
	}
	sort.SliceStable(out, func(i, j int) bool { return top(out[i]) > top(out[j]) })
	return out
}

// ── filters ──

// unlinkedExpenses keeps expenses without a scan link.
func (m Mapping) unlinkedExpenses(list []sources.ReceiptExpense) []sources.ReceiptExpense {
	if m.LinkSlot == 0 {
		return list
	}
	var out []sources.ReceiptExpense
	for _, e := range list {
		if e.Custom[m.LinkSlot-1] == "" {
			out = append(out, e)
		}
	}
	return out
}

// unlinkedDocs keeps scans without an expense number.
func (m Mapping) unlinkedDocs(list []sources.ReceiptDoc) []sources.ReceiptDoc {
	if m.FieldExpense == 0 {
		return list
	}
	var out []sources.ReceiptDoc
	for _, d := range list {
		if !filled(d.Custom[m.FieldExpense]) {
			out = append(out, d)
		}
	}
	return out
}

// filled tells whether a custom field holds a value ("" and null don't).
func filled(v any) bool {
	if v == nil {
		return false
	}
	s, ok := v.(string)
	return !ok || strings.TrimSpace(s) != ""
}

// inYear keeps expenses of one year; undated ones drop out.
func inYear(list []sources.ReceiptExpense, year int) []sources.ReceiptExpense {
	prefix := strconv.Itoa(year) + "-"
	var out []sources.ReceiptExpense
	for _, e := range list {
		if strings.HasPrefix(e.Day, prefix) {
			out = append(out, e)
		}
	}
	return out
}
