package verbund

// Customers of a Verbund: one customer across its members. Invoice Ninja
// holds the names (caps.NameSource), so its clients are the rows; each
// other member (Kimai, Sure, Paperless) is a column. The view proposes
// the free entry of the most similar name; a link is stored only when
// someone confirms it or says there is none. Without Invoice Ninja,
// Kimai leads.
//
//	Ninja Kx9 "ACME GmbH" ── Kimai 12 "Acme" (align → "ACME GmbH")
//	                      ── Sure "ACME GMBH" (payer)
//	                      ── Paperless 7 "ACME" (correspondent)
//	Ninja Zz1 "Beta KG"   ┄┄ Kimai 17 "Beta" (suggested, not stored)

import (
	"context"
	"database/sql"
	"errors"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"andon/internal/caps"
	"andon/internal/db"
	"andon/internal/enums"
	"andon/internal/metrics"
	"andon/internal/model"
	"andon/internal/outbound"
	"andon/internal/repos/content"
	linkrepo "andon/internal/repos/links"
	"andon/internal/services/access"
	"andon/internal/services/audit"
	"andon/internal/services/svcdata"
	"andon/internal/sources"
)

var (
	// ErrNoCustomers: the Verbund has fewer than two members that know
	// customers.
	ErrNoCustomers = errors.New("verbund.no_customers")
	// ErrClientTaken: that entry belongs to another customer already.
	ErrClientTaken = errors.New("verbund.client_taken")
	// ErrNoData: the datasets have not been fetched yet.
	ErrNoData = errors.New("verbund.no_data")
	// ErrUnknownCustomer: the customer or entry is not in the fetched data.
	ErrUnknownCustomer = errors.New("verbund.unknown_customer")
	// ErrDemo: demo connections take no writes.
	ErrDemo = errors.New("verbund.demo")
)

// customerServices know customers, in the order that picks the rows:
// Invoice Ninja holds the names.
var customerServices = []enums.ServiceType{enums.ServiceInvoiceNinja, enums.ServiceKimai, enums.ServiceSure, enums.ServicePaperless}

// CustomerState says how a customer is known in a member.
type CustomerState string

const (
	CustomerConfirmed CustomerState = "confirmed"
	CustomerNone      CustomerState = "none"      // no counterpart, said so
	CustomerSuggested CustomerState = "suggested" // most similar name, not stored
	CustomerOpen      CustomerState = "open"      // nothing similar
)

// Party is a customer as one member knows it: a Kimai customer, a Ninja
// client, a Sure payer, a Paperless correspondent.
type Party struct {
	Key, Name string
}

// Column is a member with the customers it knows.
type Column struct {
	ConnID  int64
	Service enums.ServiceType
	Name    string // the connection's
	Parties []Party
}

// Cell is a row's customer in one column.
type Cell struct {
	Key, Name   string
	State       CustomerState
	NameDiffers bool // Kimai, confirmed, with another name than Ninja's
}

// CustomerRow is one customer of the leading member with its cells, in
// the order of CustomerView.Columns.
type CustomerRow struct {
	Key, Name string
	Cells     []Cell
}

// Orphan is a stored customer of a column that has no counterpart in the
// leading member, e.g. Kimai "Intern" without an Invoice Ninja client.
type Orphan struct {
	EntryID int64
	Service enums.ServiceType
	Name    string
}

// CustomerView is a Verbund's customers.
type CustomerView struct {
	Verbund  View
	Hub      Column
	Columns  []Column
	Rows     []CustomerRow // rows with open or suggested cells first, then by name
	Orphans  []Orphan
	CanAlign bool // Kimai takes names from Invoice Ninja (caps)
}

// Suggested says whether a cell waits for a confirmed suggestion.
func (v CustomerView) Suggested() bool {
	for _, r := range v.Rows {
		for _, c := range r.Cells {
			if c.State == CustomerSuggested {
				return true
			}
		}
	}
	return false
}

// members is a Verbund's connections that know customers, leader first.
type members []*model.Connection

func membersOf(q db.Queryer, v View) (members, error) {
	byService := map[enums.ServiceType]*model.Connection{}
	for _, m := range v.Members {
		c, err := content.Connection(q, m.ConnID)
		if err != nil {
			return nil, err
		}
		if c != nil {
			byService[m.Service] = c
		}
	}
	var out members
	for _, s := range customerServices {
		if c := byService[s]; c != nil && caps.Full(caps.HolderOf(s)).Can(caps.Customers, caps.Read, "") {
			out = append(out, c)
		}
	}
	if len(out) < 2 {
		return nil, ErrNoCustomers
	}
	return out, nil
}

func (m members) hub() *model.Connection { return m[0] }

// of is the member of service, nil if none.
func (m members) of(s enums.ServiceType) *model.Connection {
	for _, c := range m {
		if c.Service == string(s) {
			return c
		}
	}
	return nil
}

// columnsOf reads every member's customers: cached data, fetched when
// gone (a settings page, not a board).
func columnsOf(ctx context.Context, d *sql.DB, who *access.Principal, m members) ([]Column, error) {
	holder := model.UserHolder(who.UserID)
	out := make([]Column, 0, len(m))
	for _, c := range m {
		service := enums.ServiceType(c.Service)
		r, err := svcdata.Get(ctx, d, sources.DataKey(service), nil, c, holder, svcdata.Cached)
		if err != nil {
			return nil, err
		}
		parties, ok := partiesOf(r.Data)
		if !ok {
			return nil, ErrNoData
		}
		out = append(out, Column{ConnID: c.ID, Service: service, Name: c.Name, Parties: parties})
	}
	return out, nil
}

// partiesOf lists a dataset's customers, sorted by name; false for a
// dataset that is not there (yet).
func partiesOf(data any) ([]Party, bool) {
	var out []Party
	switch ds := data.(type) {
	case *sources.NinjaDataset:
		for _, c := range ds.Clients {
			out = append(out, Party{c.Ref(), c.Name})
		}
	case *sources.KimaiDataset:
		for _, c := range ds.Customers {
			out = append(out, Party{strconv.FormatInt(c.ID, 10), c.Name})
		}
	case *sources.SureDataset:
		seen := map[string]bool{}
		for _, t := range ds.Transactions {
			if p := metrics.Payer(t); t.Amount > 0 && p != "" && !seen[p] {
				seen[p] = true
				out = append(out, Party{p, p})
			}
		}
	case *sources.PaperlessDataset:
		for _, c := range ds.Correspondents {
			out = append(out, Party{strconv.FormatInt(c.ID, 10), c.Name})
		}
	default:
		return nil, false
	}
	sort.SliceStable(out, func(a, b int) bool { return strings.ToLower(out[a].Name) < strings.ToLower(out[b].Name) })
	return out, true
}

// Customers lists a Verbund's customers across its members. Requires
// VIEW on every member.
func Customers(ctx context.Context, d *sql.DB, who *access.Principal, id int64) (CustomerView, error) {
	v, err := Get(d, who, id)
	if err != nil {
		return CustomerView{}, err
	}
	m, err := membersOf(d, v)
	if err != nil {
		return CustomerView{}, err
	}
	cols, err := columnsOf(ctx, d, who, m)
	if err != nil {
		return CustomerView{}, err
	}
	entries, err := linkrepo.Entries(d, id, string(caps.Customers))
	if err != nil {
		return CustomerView{}, err
	}
	out := CustomerView{Verbund: v, Hub: cols[0], Columns: cols[1:],
		CanAlign: m.hub().Service == string(enums.ServiceInvoiceNinja) && m.of(enums.ServiceKimai) != nil &&
			caps.Full(caps.HolderOf(enums.ServiceKimai)).Can(caps.Customers, caps.Update, "")}
	out.Rows = customerRows(out.Hub, out.Columns, entries)
	out.Orphans = orphans(out.Hub, out.Columns, entries)
	return out, nil
}

// customerRows gives each leading customer its cells: stored keys first,
// else the free customer of the most similar name.
func customerRows(hub Column, cols []Column, entries []linkrepo.Entry) []CustomerRow {
	byHub := map[string]linkrepo.Entry{}
	taken := make([]map[string]bool, len(cols))
	for i := range cols {
		taken[i] = map[string]bool{}
	}
	for _, e := range entries {
		if k, ok := keyIn(e, hub.ConnID); ok && k.State == linkrepo.KeyConfirmed {
			byHub[k.Key] = e
		}
		for i, c := range cols {
			if k, ok := keyIn(e, c.ConnID); ok && k.Key != "" {
				taken[i][k.Key] = true
			}
		}
	}

	out := make([]CustomerRow, 0, len(hub.Parties))
	for _, p := range hub.Parties {
		row := CustomerRow{Key: p.Key, Name: p.Name, Cells: make([]Cell, len(cols))}
		e, stored := byHub[p.Key]
		for i, c := range cols {
			cell := Cell{State: CustomerOpen}
			k, has := keyIn(e, c.ConnID)
			switch {
			case stored && has && k.State == linkrepo.KeyNone:
				cell.State = CustomerNone
			case stored && has:
				cell.State, cell.Key, cell.Name = CustomerConfirmed, k.Key, nameOf(c.Parties, k.Key)
				cell.NameDiffers = c.Service == enums.ServiceKimai && hub.Service == enums.ServiceInvoiceNinja &&
					cell.Name != "" && cell.Name != p.Name
			default:
				if s, ok := similar(p.Name, c.Parties, taken[i]); ok {
					cell.State, cell.Key, cell.Name = CustomerSuggested, s.Key, s.Name
					taken[i][s.Key] = true // one suggestion per customer
				}
			}
			row.Cells[i] = cell
		}
		out = append(out, row)
	}
	sort.SliceStable(out, func(a, b int) bool {
		if open(out[a]) != open(out[b]) {
			return open(out[a])
		}
		return strings.ToLower(out[a].Name) < strings.ToLower(out[b].Name)
	})
	return out
}

func open(r CustomerRow) bool {
	return slices.ContainsFunc(r.Cells, func(c Cell) bool { return c.State == CustomerOpen || c.State == CustomerSuggested })
}

// orphans are stored entries without a leading customer: what a column
// marked as having none there (Kimai "Intern": no Ninja client).
func orphans(hub Column, cols []Column, entries []linkrepo.Entry) []Orphan {
	var out []Orphan
	for _, e := range entries {
		if k, ok := keyIn(e, hub.ConnID); ok && k.State == linkrepo.KeyConfirmed {
			continue
		}
		for _, c := range cols {
			if k, ok := keyIn(e, c.ConnID); ok && k.Key != "" {
				out = append(out, Orphan{EntryID: e.ID, Service: c.Service, Name: nameOf(c.Parties, k.Key)})
			}
		}
	}
	return out
}

func nameOf(list []Party, key string) string {
	for _, p := range list {
		if p.Key == key {
			return p.Name
		}
	}
	return key
}

func keyIn(e linkrepo.Entry, connID int64) (linkrepo.Key, bool) {
	for _, k := range e.Keys {
		if k.ConnID == connID {
			return k, true
		}
	}
	return linkrepo.Key{}, false
}

// legalForms are left out when names are compared: "Acme GmbH" ~ "ACME".
var legalForms = map[string]bool{"gmbh": true, "co": true, "kg": true, "ag": true, "ug": true, "mbh": true, "ek": true,
	"ohg": true, "gbr": true, "ltd": true, "inc": true, "llc": true, "se": true, "haftungsbeschränkt": true}

// minSimilar is the share of name words two names must share.
const minSimilar = 0.5

// similar is the free party whose name shares the most words with name.
func similar(name string, list []Party, taken map[string]bool) (Party, bool) {
	want := words(name)
	var best Party
	bestScore := 0.0
	for _, p := range list {
		if taken[p.Key] {
			continue
		}
		if score := overlap(want, words(p.Name)); score > bestScore {
			best, bestScore = p, score
		}
	}
	return best, bestScore >= minSimilar
}

// words is a name's words without punctuation and legal forms.
func words(name string) map[string]bool {
	out := map[string]bool{}
	clean := strings.Map(func(r rune) rune {
		if strings.ContainsRune(".,&()-/+", r) {
			return ' '
		}
		return r
	}, strings.ToLower(name))
	for _, w := range strings.Fields(clean) {
		if !legalForms[w] {
			out[w] = true
		}
	}
	return out
}

// overlap is the Jaccard share of two word sets.
func overlap(a, b map[string]bool) float64 {
	if len(a) == 0 || len(b) == 0 {
		return 0
	}
	both := 0
	for w := range a {
		if b[w] {
			both++
		}
	}
	return float64(both) / float64(len(a)+len(b)-both)
}

// LinkCustomer stores the customer of column connID for the leading
// customer hubKey; key "" says the column has none. Only customers the
// services have are taken. Requires EDIT on every member.
func LinkCustomer(ctx context.Context, d *sql.DB, who *access.Principal, id int64, hubKey string, connID int64, key, ip string) error {
	view, err := Customers(ctx, d, who, id)
	if err != nil {
		return err
	}
	col, ok := view.column(connID)
	if !ok || !hasParty(view.Hub.Parties, hubKey) || (key != "" && !hasParty(col.Parties, key)) {
		return ErrUnknownCustomer
	}
	return change(d, who, id, func(tx *sql.Tx, l *linkrepo.Link) error {
		if err := putCell(tx, id, view.Hub.ConnID, hubKey, connID, key); err != nil {
			return err
		}
		return audit.Log(tx, &who.UserID, "verbund.customer_linked", l.Name, ip, map[string]any{"customer": hubKey, "service": col.Service, "key": key})
	})
}

func (v CustomerView) column(connID int64) (Column, bool) {
	for _, c := range v.Columns {
		if c.ConnID == connID {
			return c, true
		}
	}
	return Column{}, false
}

func hasParty(list []Party, key string) bool {
	return slices.ContainsFunc(list, func(p Party) bool { return p.Key == key })
}

// putCell sets one column's key in the leading customer's entry. A key
// that sat in an orphan entry moves here.
func putCell(tx *sql.Tx, id, hubConn int64, hubKey string, connID int64, key string) error {
	domain := string(caps.Customers)
	entries, err := linkrepo.Entries(tx, id, domain)
	if err != nil {
		return err
	}
	var entryID int64
	for _, e := range entries {
		hk, hubbed := keyIn(e, hubConn)
		anchored := hubbed && hk.State == linkrepo.KeyConfirmed
		if anchored && hk.Key == hubKey {
			entryID = e.ID
			continue
		}
		if k, ok := keyIn(e, connID); ok && key != "" && k.Key == key && !anchored {
			if err := linkrepo.DeleteEntry(tx, e.ID); err != nil {
				return err
			}
		}
	}
	k := linkrepo.Key{ConnID: connID, Key: key, State: linkrepo.KeyConfirmed}
	if key == "" {
		k.State = linkrepo.KeyNone
	}
	if entryID != 0 {
		err = linkrepo.SetKey(tx, entryID, id, domain, k)
	} else {
		_, err = linkrepo.PutEntry(tx, id, domain, []linkrepo.Key{{ConnID: hubConn, Key: hubKey, State: linkrepo.KeyConfirmed}, k}, time.Now().UTC())
	}
	if errors.Is(err, linkrepo.ErrKeyTaken) {
		return ErrClientTaken
	}
	return err
}

// UnlinkCustomer forgets the column's key of a leading customer: the
// name decides again. Requires EDIT on every member.
func UnlinkCustomer(d *sql.DB, who *access.Principal, id int64, hubKey string, connID int64, ip string) error {
	return change(d, who, id, func(tx *sql.Tx, l *linkrepo.Link) error {
		v, _, err := viewOf(tx, who, *l)
		if err != nil {
			return err
		}
		m, err := membersOf(tx, v)
		if err != nil {
			return err
		}
		entries, err := linkrepo.Entries(tx, id, string(caps.Customers))
		if err != nil {
			return err
		}
		for _, e := range entries {
			if hk, ok := keyIn(e, m.hub().ID); ok && hk.State == linkrepo.KeyConfirmed && hk.Key == hubKey {
				// The trigger drops the entry once only the hub's key is left.
				if err := linkrepo.DeleteKey(tx, e.ID, connID); err != nil {
					return err
				}
			}
		}
		return audit.Log(tx, &who.UserID, "verbund.customer_unlinked", l.Name, ip, map[string]any{"customer": hubKey, "connection": connID})
	})
}

// RemoveOrphan forgets an orphan entry. Requires EDIT on every member.
func RemoveOrphan(d *sql.DB, who *access.Principal, id, entryID int64, ip string) error {
	return change(d, who, id, func(tx *sql.Tx, l *linkrepo.Link) error {
		entries, err := linkrepo.Entries(tx, id, string(caps.Customers))
		if err != nil {
			return err
		}
		if !slices.ContainsFunc(entries, func(e linkrepo.Entry) bool { return e.ID == entryID }) {
			return ErrUnknownCustomer
		}
		if err := linkrepo.DeleteEntry(tx, entryID); err != nil {
			return err
		}
		return audit.Log(tx, &who.UserID, "verbund.customer_unlinked", l.Name, ip, map[string]any{"entry": entryID})
	})
}

// ConfirmSuggestions stores every suggested cell. Requires EDIT on every
// member.
func ConfirmSuggestions(ctx context.Context, d *sql.DB, who *access.Principal, id int64, ip string) (int, error) {
	view, err := Customers(ctx, d, who, id)
	if err != nil {
		return 0, err
	}
	n := 0
	err = change(d, who, id, func(tx *sql.Tx, l *linkrepo.Link) error {
		for _, r := range view.Rows {
			for i, c := range r.Cells {
				if c.State != CustomerSuggested {
					continue
				}
				if err := putCell(tx, id, view.Hub.ConnID, r.Key, view.Columns[i].ConnID, c.Key); err != nil {
					return err
				}
				n++
			}
		}
		return audit.Log(tx, &who.UserID, "verbund.customers_confirmed", l.Name, ip, map[string]any{"count": n})
	})
	return n, err
}

// AlignName writes the Invoice Ninja client's name to its linked Kimai
// customer. Requires EDIT on every member.
func AlignName(ctx context.Context, d *sql.DB, who *access.Principal, id int64, hubKey string, ip string) error {
	view, err := Customers(ctx, d, who, id)
	if err != nil {
		return err
	}
	if !view.Verbund.CanEdit {
		return access.ErrDenied
	}
	if !view.CanAlign {
		return nil
	}
	var cell Cell
	var name string
	for _, r := range view.Rows {
		if r.Key != hubKey {
			continue
		}
		for i, c := range r.Cells {
			if view.Columns[i].Service == enums.ServiceKimai && c.State == CustomerConfirmed && c.NameDiffers {
				cell, name = c, r.Name
			}
		}
	}
	if name == "" {
		return nil
	}
	customerID, err := strconv.ParseInt(cell.Key, 10, 64)
	if err != nil {
		return ErrUnknownCustomer
	}
	m, err := membersOf(d, view.Verbund)
	if err != nil {
		return err
	}
	kimai := m.of(enums.ServiceKimai)
	if sources.IsDemo(kimai.URL) {
		return ErrDemo
	}
	sctx, err := svcdata.SourceCtx(d, kimai, model.UserHolder(who.UserID))
	if err != nil {
		return err
	}
	to := outbound.Target{URL: kimai.URL, Token: sctx.Secret, VerifyTLS: kimai.VerifyTLS}
	if err := outbound.KimaiRenameCustomer(ctx, to, customerID, name); err != nil {
		return err
	}
	svcdata.Forget(kimai.ID)
	return db.WithTx(d, func(tx *sql.Tx) error {
		return audit.Log(tx, &who.UserID, "verbund.customer_renamed", name, ip, map[string]any{"customer": customerID, "was": cell.Name})
	})
}
