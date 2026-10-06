package verbund

// Customers of a Verbund: which Kimai customer is which Invoice Ninja
// client. The view proposes the client of the most similar name; a link
// is stored only when someone confirms it or says there is none.
// Invoice Ninja holds the names: "align" writes its name to Kimai.
//
//	Kimai 12 "Acme"  ──confirmed──►  Ninja Kx9 "ACME GmbH"   (align → Kimai)
//	Kimai 15 "Intern" ─none───────►  –
//	Kimai 17 "Beta"   ┄suggested┄┄►  Ninja Zz1 "Beta KG"     (not stored)

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
	// ErrNoCustomers: the Verbund lacks a Kimai or an Invoice Ninja.
	ErrNoCustomers = errors.New("verbund.no_customers")
	// ErrClientTaken: that client belongs to another customer already.
	ErrClientTaken = errors.New("verbund.client_taken")
	// ErrNoData: the datasets have not been fetched yet.
	ErrNoData = errors.New("verbund.no_data")
	// ErrUnknownCustomer: the customer or client is not in the fetched data.
	ErrUnknownCustomer = errors.New("verbund.unknown_customer")
	// ErrDemo: demo connections take no writes.
	ErrDemo = errors.New("verbund.demo")
)

// CustomerState says how a customer's client is known.
type CustomerState string

const (
	CustomerConfirmed CustomerState = "confirmed"
	CustomerNone      CustomerState = "none"      // no client, said so
	CustomerSuggested CustomerState = "suggested" // most similar name, not stored
	CustomerOpen      CustomerState = "open"      // nothing similar
)

// CustomerRow is one Kimai customer with its client.
type CustomerRow struct {
	KimaiID     int64
	KimaiName   string
	ClientKey   string
	ClientName  string
	State       CustomerState
	NameDiffers bool // confirmed, but Kimai has another name than Ninja
}

// CustomerView is a Verbund's customers.
type CustomerView struct {
	Verbund  View
	Rows     []CustomerRow // open and suggested first, then by name
	Clients  []sources.NinjaClient
	CanAlign bool // Kimai takes names (caps)
}

// Suggested says whether a row waits for a confirmed suggestion.
func (v CustomerView) Suggested() bool {
	for _, r := range v.Rows {
		if r.State == CustomerSuggested && r.ClientKey != "" {
			return true
		}
	}
	return false
}

// members is a Verbund's Kimai and Ninja connections.
type members struct {
	kimai, ninja *model.Connection
}

func membersOf(q db.Queryer, v View) (members, error) {
	var out members
	for _, m := range v.Members {
		c, err := content.Connection(q, m.ConnID)
		if err != nil {
			return out, err
		}
		switch m.Service {
		case enums.ServiceKimai:
			out.kimai = c
		case enums.ServiceInvoiceNinja:
			out.ninja = c
		}
	}
	if out.kimai == nil || out.ninja == nil {
		return out, ErrNoCustomers
	}
	return out, nil
}

// datasets reads both members' data: cached, fetched when gone (a
// settings page, not a board; after a rename the cache is dropped).
func datasets(ctx context.Context, d *sql.DB, who *access.Principal, m members) (*sources.KimaiDataset, *sources.NinjaDataset, error) {
	holder := model.UserHolder(who.UserID)
	k, err := svcdata.Get(ctx, d, sources.DataKey(enums.ServiceKimai), nil, m.kimai, holder, svcdata.Cached)
	if err != nil {
		return nil, nil, err
	}
	n, err := svcdata.Get(ctx, d, sources.DataKey(enums.ServiceInvoiceNinja), nil, m.ninja, holder, svcdata.Cached)
	if err != nil {
		return nil, nil, err
	}
	kimai, ok1 := k.Data.(*sources.KimaiDataset)
	ninja, ok2 := n.Data.(*sources.NinjaDataset)
	if !ok1 || !ok2 {
		return nil, nil, ErrNoData
	}
	return kimai, ninja, nil
}

// Customers lists a Verbund's customers with their clients. Requires
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
	kimai, ninja, err := datasets(ctx, d, who, m)
	if err != nil {
		return CustomerView{}, err
	}
	links, err := clientMapOf(d, id, m.kimai.ID, m.ninja.ID)
	if err != nil {
		return CustomerView{}, err
	}

	out := CustomerView{Verbund: v, Clients: ninja.Clients,
		CanAlign: caps.Full(caps.HolderOf(enums.ServiceKimai)).Can(caps.Customers, caps.Update, "")}
	out.Rows = customerRows(kimai, ninja, links)
	return out, nil
}

// customerRows pairs each Kimai customer: stored link first, else the
// free client of the most similar name.
func customerRows(kimai *sources.KimaiDataset, ninja *sources.NinjaDataset, links map[int64]string) []CustomerRow {
	byKey := map[string]sources.NinjaClient{}
	taken := map[string]bool{}
	for _, c := range ninja.Clients {
		byKey[c.Ref()] = c
	}
	for _, key := range links {
		taken[key] = true
	}

	var out []CustomerRow
	for _, cust := range kimai.Customers {
		row := CustomerRow{KimaiID: cust.ID, KimaiName: cust.Name, State: CustomerOpen}
		key, stored := links[cust.ID]
		switch {
		case stored && key == "":
			row.State = CustomerNone
		case stored:
			c := byKey[key]
			row.State, row.ClientKey, row.ClientName = CustomerConfirmed, key, c.Name
			row.NameDiffers = c.Name != "" && c.Name != cust.Name
		default:
			if c, ok := similar(cust.Name, ninja.Clients, taken); ok {
				row.State, row.ClientKey, row.ClientName = CustomerSuggested, c.Ref(), c.Name
			}
		}
		out = append(out, row)
	}
	sort.SliceStable(out, func(a, b int) bool {
		if open(out[a]) != open(out[b]) {
			return open(out[a])
		}
		return strings.ToLower(out[a].KimaiName) < strings.ToLower(out[b].KimaiName)
	})
	return out
}

func open(r CustomerRow) bool { return r.State == CustomerOpen || r.State == CustomerSuggested }

// legalForms are left out when names are compared: "Acme GmbH" ~ "ACME".
var legalForms = map[string]bool{"gmbh": true, "co": true, "kg": true, "ag": true, "ug": true, "mbh": true, "ek": true,
	"ohg": true, "gbr": true, "ltd": true, "inc": true, "llc": true, "se": true, "haftungsbeschränkt": true}

// minSimilar is the share of name words two names must share.
const minSimilar = 0.5

// similar is the free client whose name shares the most words with name.
func similar(name string, clients []sources.NinjaClient, taken map[string]bool) (sources.NinjaClient, bool) {
	want := words(name)
	var best sources.NinjaClient
	bestScore := 0.0
	for _, c := range clients {
		if taken[c.Ref()] {
			continue
		}
		if score := overlap(want, words(c.Name)); score > bestScore {
			best, bestScore = c, score
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

// LinkCustomer stores a Kimai customer's client; clientKey "" says it
// has none. Requires EDIT on every member.
func LinkCustomer(ctx context.Context, d *sql.DB, who *access.Principal, id, customerID int64, clientKey, ip string) error {
	if err := known(ctx, d, who, id, customerID, clientKey); err != nil {
		return err
	}
	return change(d, who, id, func(tx *sql.Tx, l *linkrepo.Link) error {
		v, _, err := viewOf(tx, who, *l)
		if err != nil {
			return err
		}
		m, err := membersOf(tx, v)
		if err != nil {
			return err
		}
		if err := putCustomer(tx, id, m, customerID, clientKey); err != nil {
			return err
		}
		return audit.Log(tx, &who.UserID, "verbund.customer_linked", l.Name, ip, map[string]any{"customer": customerID, "client": clientKey})
	})
}

// known checks the customer and the client ("" = none) against the
// fetched data: a link to something the services do not have is no link.
func known(ctx context.Context, d *sql.DB, who *access.Principal, id, customerID int64, clientKey string) error {
	v, err := Get(d, who, id)
	if err != nil {
		return err
	}
	m, err := membersOf(d, v)
	if err != nil {
		return err
	}
	kimai, ninja, err := datasets(ctx, d, who, m)
	if err != nil {
		return err
	}
	customer := slices.ContainsFunc(kimai.Customers, func(c sources.KimaiCustomer) bool { return c.ID == customerID })
	client := clientKey == "" || slices.ContainsFunc(ninja.Clients, func(c sources.NinjaClient) bool { return c.Ref() == clientKey })
	if !customer || !client {
		return ErrUnknownCustomer
	}
	return nil
}

// putCustomer writes one customer's entry, replacing an earlier one.
func putCustomer(tx *sql.Tx, id int64, m members, customerID int64, clientKey string) error {
	ninja := linkrepo.Key{ConnID: m.ninja.ID, Key: clientKey, State: linkrepo.KeyConfirmed}
	if clientKey == "" {
		ninja.State = linkrepo.KeyNone
	}
	domain := string(caps.Customers)
	entries, err := linkrepo.Entries(tx, id, domain)
	if err != nil {
		return err
	}
	kimaiKey := strconv.FormatInt(customerID, 10)
	for _, e := range entries {
		if keyOf(e, m.kimai.ID) == kimaiKey {
			if err := linkrepo.DeleteEntry(tx, e.ID); err != nil {
				return err
			}
		}
	}
	_, err = linkrepo.PutEntry(tx, id, domain, []linkrepo.Key{{ConnID: m.kimai.ID, Key: kimaiKey, State: linkrepo.KeyConfirmed}, ninja}, time.Now().UTC())
	if errors.Is(err, linkrepo.ErrKeyTaken) {
		return ErrClientTaken
	}
	return err
}

func keyOf(e linkrepo.Entry, connID int64) string {
	for _, k := range e.Keys {
		if k.ConnID == connID {
			return k.Key
		}
	}
	return ""
}

// UnlinkCustomer forgets a customer's stored client: the name decides
// again. Requires EDIT on every member.
func UnlinkCustomer(d *sql.DB, who *access.Principal, id, customerID int64, ip string) error {
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
			if keyOf(e, m.kimai.ID) == strconv.FormatInt(customerID, 10) {
				if err := linkrepo.DeleteEntry(tx, e.ID); err != nil {
					return err
				}
			}
		}
		return audit.Log(tx, &who.UserID, "verbund.customer_unlinked", l.Name, ip, map[string]any{"customer": customerID})
	})
}

// ConfirmSuggestions stores every suggested client. Requires EDIT on
// every member.
func ConfirmSuggestions(ctx context.Context, d *sql.DB, who *access.Principal, id int64, ip string) (int, error) {
	view, err := Customers(ctx, d, who, id)
	if err != nil {
		return 0, err
	}
	n := 0
	err = change(d, who, id, func(tx *sql.Tx, l *linkrepo.Link) error {
		m, err := membersOf(tx, view.Verbund)
		if err != nil {
			return err
		}
		for _, r := range view.Rows {
			if r.State != CustomerSuggested || r.ClientKey == "" {
				continue
			}
			if err := putCustomer(tx, id, m, r.KimaiID, r.ClientKey); err != nil {
				return err
			}
			n++
		}
		return audit.Log(tx, &who.UserID, "verbund.customers_confirmed", l.Name, ip, map[string]any{"count": n})
	})
	return n, err
}

// AlignName writes the client's name (Invoice Ninja holds the names) to
// the Kimai customer. Requires EDIT on every member.
func AlignName(ctx context.Context, d *sql.DB, who *access.Principal, id, customerID int64, ip string) error {
	view, err := Customers(ctx, d, who, id)
	if err != nil {
		return err
	}
	if !view.Verbund.CanEdit {
		return access.ErrDenied
	}
	var row *CustomerRow
	for i := range view.Rows {
		if view.Rows[i].KimaiID == customerID && view.Rows[i].State == CustomerConfirmed {
			row = &view.Rows[i]
		}
	}
	if row == nil || !row.NameDiffers {
		return nil
	}
	m, err := membersOf(d, view.Verbund)
	if err != nil {
		return err
	}
	if sources.IsDemo(m.kimai.URL) {
		return ErrDemo
	}
	sctx, err := svcdata.SourceCtx(d, m.kimai, model.UserHolder(who.UserID))
	if err != nil {
		return err
	}
	to := outbound.Target{URL: m.kimai.URL, Token: sctx.Secret, VerifyTLS: m.kimai.VerifyTLS}
	if err := outbound.KimaiRenameCustomer(ctx, to, customerID, row.ClientName); err != nil {
		return err
	}
	svcdata.Forget(m.kimai.ID)
	return db.WithTx(d, func(tx *sql.Tx) error {
		return audit.Log(tx, &who.UserID, "verbund.customer_renamed", row.ClientName, ip, map[string]any{"customer": customerID, "was": row.KimaiName})
	})
}
