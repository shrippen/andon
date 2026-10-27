package verbund

// Accounts of a Verbund: one real bank account across its members, and
// what it is used for. Invoice Ninja's bank feed leads; Sure and Firefly
// are columns. Andon proposes the account sharing the most bookings
// (else the most similar name); a link is stored only when confirmed.
//
//	Ninja "Girokonto ··4711" ── Sure "Geschäftskonto"   business
//	Ninja "Girokonto ··0815" ── Sure "Privat"           private
//	                            Sure "Tagesgeld"        private (no feed)
//
// The role (business, private) belongs to each account of each
// connection (option sources.OptAccountRoles), so a private account only
// Invoice Ninja knows has one too. Setting it on one side sets it on the
// linked accounts of every Verbund the connection is in.

import (
	"context"
	"database/sql"
	"errors"
	"maps"
	"slices"
	"sort"
	"strings"

	"andon/internal/caps"
	"andon/internal/db"
	"andon/internal/enums"
	"andon/internal/metrics"
	"andon/internal/model"
	"andon/internal/repos/content"
	linkrepo "andon/internal/repos/links"
	"andon/internal/services/access"
	"andon/internal/services/audit"
	"andon/internal/services/connections"
	"andon/internal/services/svcdata"
	"andon/internal/sources"
)

var (
	// ErrNoAccounts: no member of the Verbund knows bank accounts.
	ErrNoAccounts = errors.New("verbund.no_accounts")
	// ErrAccountTaken: that account belongs to another row already.
	ErrAccountTaken = errors.New("verbund.account_taken")
	// ErrUnknownAccount: the account is not in the fetched data.
	ErrUnknownAccount = errors.New("verbund.unknown_account")
	// ErrUnknownRole: no role of that name.
	ErrUnknownRole = errors.New("verbund.unknown_role")
)

// Role is what an account is for (sources.AccountRole): pages name it
// through this package, which owns setting it.
type Role = sources.AccountRole

// accountServices know bank accounts, the leader first.
var accountServices = []enums.ServiceType{enums.ServiceInvoiceNinja, enums.ServiceSure, enums.ServiceFirefly}

// minShared is how many shared bookings make an account a suggestion.
const minShared = 2

// BankAccount is an account as one member knows it; Suggest is the
// role its kind implies while none is set (Sure's depot: ignored).
type BankAccount struct {
	Key, Name     string
	Role, Suggest sources.AccountRole
}

// AccountColumn is a member with its accounts.
type AccountColumn struct {
	ConnID   int64
	Service  enums.ServiceType
	Name     string // the connection's
	Accounts []BankAccount
}

// AccountCell is a row's account in one column.
type AccountCell struct {
	Key, Name string
	State     CustomerState
	Role      sources.AccountRole
}

// AccountRow is one account of the leading member with its cells.
type AccountRow struct {
	BankAccount
	Cells []AccountCell
}

// Loose is an account of a column that no row holds (Sure's savings).
type Loose struct {
	ConnID  int64
	Service enums.ServiceType
	BankAccount
}

// AccountView is a Verbund's accounts.
type AccountView struct {
	Verbund View
	Hub     AccountColumn
	Columns []AccountColumn
	Rows    []AccountRow
	Loose   []Loose
	CanSet  bool // the caller may set roles (MANAGE on every member)
}

// Suggested says whether a cell waits for a confirmed suggestion.
func (v AccountView) Suggested() bool {
	for _, r := range v.Rows {
		if slices.ContainsFunc(r.Cells, func(c AccountCell) bool { return c.State == CustomerSuggested }) {
			return true
		}
	}
	return false
}

// HasAccounts reports whether a member of the Verbund knows accounts.
func (v View) HasAccounts() bool {
	return slices.ContainsFunc(accountServices, func(s enums.ServiceType) bool { _, ok := v.Has(s); return ok })
}

// accountMembers is the Verbund's connections that know accounts.
func accountMembers(q db.Queryer, v View) ([]*model.Connection, error) {
	var out []*model.Connection
	for _, s := range accountServices {
		m, ok := v.Has(s)
		if !ok || !caps.Full(caps.HolderOf(s)).Can(caps.Accounts, caps.Read, "") {
			continue
		}
		c, err := content.Connection(q, m.ConnID)
		if err != nil {
			return nil, err
		}
		if c != nil {
			out = append(out, c)
		}
	}
	if len(out) == 0 {
		return nil, ErrNoAccounts
	}
	return out, nil
}

// accountData is each member's dataset, cached or fetched (a settings
// page, not a board).
func accountData(ctx context.Context, d *sql.DB, who *access.Principal, m []*model.Connection) ([]any, error) {
	holder := model.UserHolder(who.UserID)
	out := make([]any, len(m))
	for i, c := range m {
		r, err := svcdata.Get(ctx, d, sources.DataKey(enums.ServiceType(c.Service)), nil, c, holder, svcdata.Cached)
		if err != nil {
			return nil, err
		}
		if r.Data == nil {
			return nil, ErrNoData
		}
		out[i] = r.Data
	}
	return out, nil
}

// accountsOf lists a dataset's accounts by name.
func accountsOf(data any) []BankAccount {
	var out []BankAccount
	switch ds := data.(type) {
	case *sources.NinjaDataset:
		for _, a := range ds.BankAccounts {
			out = append(out, BankAccount{Key: a.Key, Name: a.Name, Role: a.Role})
		}
	case *sources.BankData:
		for _, a := range slices.Concat(ds.Accounts, ds.Ignored) {
			out = append(out, BankAccount{Key: a.ID, Name: a.Name, Role: a.Role, Suggest: a.Suggested()})
		}
	}
	sort.SliceStable(out, func(a, b int) bool { return strings.ToLower(out[a].Name) < strings.ToLower(out[b].Name) })
	return out
}

// Accounts lists a Verbund's accounts across its members. Requires VIEW
// on every member.
func Accounts(ctx context.Context, d *sql.DB, who *access.Principal, id int64) (AccountView, error) {
	v, err := Get(d, who, id)
	if err != nil {
		return AccountView{}, err
	}
	m, err := accountMembers(d, v)
	if err != nil {
		return AccountView{}, err
	}
	data, err := accountData(ctx, d, who, m)
	if err != nil {
		return AccountView{}, err
	}
	cols := make([]AccountColumn, len(m))
	for i, c := range m {
		cols[i] = AccountColumn{ConnID: c.ID, Service: enums.ServiceType(c.Service), Name: c.Name, Accounts: accountsOf(data[i])}
	}
	entries, err := linkrepo.Entries(d, id, string(caps.Accounts))
	if err != nil {
		return AccountView{}, err
	}

	out := AccountView{Verbund: v, Hub: cols[0], Columns: cols[1:], CanSet: true}
	for _, c := range m {
		if conn, err := connections.Get(d, who, c.ID); err != nil || conn.Right < enums.RightManage {
			out.CanSet = false
		}
	}
	shared := sharedOf(data)
	out.Rows = accountRows(out.Hub, out.Columns, entries, shared)
	out.Loose = loose(out.Columns, out.Rows)
	return out, nil
}

// sharedOf counts shared bookings of the leader (Invoice Ninja) with each
// column (Sure, Firefly), by column index.
func sharedOf(data []any) []map[string]map[string]int {
	out := make([]map[string]map[string]int, len(data))
	ninja, ok := data[0].(*sources.NinjaDataset)
	if !ok {
		return out
	}
	for i, ds := range data[1:] {
		bank, _ := ds.(*sources.BankData)
		out[i] = metrics.SharedBookings(ninja, bank)
	}
	return out
}

// accountRows gives each leading account its cells: stored keys first,
// else the free account sharing the most bookings, else the most
// similar name.
func accountRows(hub AccountColumn, cols []AccountColumn, entries []linkrepo.Entry, shared []map[string]map[string]int) []AccountRow {
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

	out := make([]AccountRow, 0, len(hub.Accounts))
	for _, a := range hub.Accounts {
		row := AccountRow{BankAccount: a, Cells: make([]AccountCell, len(cols))}
		e, stored := byHub[a.Key]
		for i, c := range cols {
			cell := AccountCell{State: CustomerOpen}
			k, has := keyIn(e, c.ConnID)
			switch {
			case stored && has && k.State == linkrepo.KeyNone:
				cell.State = CustomerNone
			case stored && has:
				cell.State, cell.Key = CustomerConfirmed, k.Key
			default:
				if s, ok := twinOf(a, c.Accounts, taken[i], shared[i][a.Key]); ok {
					cell.State, cell.Key = CustomerSuggested, s.Key
					taken[i][s.Key] = true
				}
			}
			if acc, ok := accountIn(c.Accounts, cell.Key); ok {
				cell.Name, cell.Role = acc.Name, acc.Role
			}
			row.Cells[i] = cell
		}
		out = append(out, row)
	}
	return out
}

// twinOf is the free account sharing the most bookings with a, else the
// one of the most similar name.
func twinOf(a BankAccount, list []BankAccount, taken map[string]bool, shared map[string]int) (BankAccount, bool) {
	var best BankAccount
	most := 0
	for _, b := range list {
		if !taken[b.Key] && shared[b.Key] > most {
			best, most = b, shared[b.Key]
		}
	}
	if most >= minShared {
		return best, true
	}
	parties := make([]Party, 0, len(list))
	for _, b := range list {
		parties = append(parties, Party{b.Key, b.Name})
	}
	p, ok := similar(a.Name, parties, taken)
	if !ok {
		return BankAccount{}, false
	}
	b, _ := accountIn(list, p.Key)
	return b, true
}

func accountIn(list []BankAccount, key string) (BankAccount, bool) {
	i := slices.IndexFunc(list, func(b BankAccount) bool { return b.Key == key })
	if i < 0 || key == "" {
		return BankAccount{}, false
	}
	return list[i], true
}

// loose are the column accounts no row holds, confirmed or suggested.
func loose(cols []AccountColumn, rows []AccountRow) []Loose {
	var out []Loose
	for i, c := range cols {
		for _, a := range c.Accounts {
			held := slices.ContainsFunc(rows, func(r AccountRow) bool { return r.Cells[i].Key == a.Key })
			if !held {
				out = append(out, Loose{ConnID: c.ConnID, Service: c.Service, BankAccount: a})
			}
		}
	}
	return out
}

// LinkAccount stores the account of column connID for the leading
// account hubKey; key "" says the column has none. Requires EDIT on
// every member.
func LinkAccount(ctx context.Context, d *sql.DB, who *access.Principal, id int64, hubKey string, connID int64, key, ip string) error {
	view, err := Accounts(ctx, d, who, id)
	if err != nil {
		return err
	}
	i := slices.IndexFunc(view.Columns, func(c AccountColumn) bool { return c.ConnID == connID })
	if _, ok := accountIn(view.Hub.Accounts, hubKey); !ok || i < 0 {
		return ErrUnknownAccount
	}
	if _, ok := accountIn(view.Columns[i].Accounts, key); key != "" && !ok {
		return ErrUnknownAccount
	}
	return change(d, who, id, func(tx *sql.Tx, l *linkrepo.Link) error {
		if err := putCell(tx, id, caps.Accounts, view.Hub.ConnID, hubKey, connID, key); err != nil {
			return takenAccount(err)
		}
		return audit.Log(tx, &who.UserID, "verbund.account_linked", l.Name, ip, map[string]any{"account": hubKey, "connection": connID, "key": key})
	})
}

// takenAccount names a taken key as an account, not a customer.
func takenAccount(err error) error {
	if errors.Is(err, ErrClientTaken) {
		return ErrAccountTaken
	}
	return err
}

// UnlinkAccount forgets the column's account of a leading account.
// Requires EDIT on every member.
func UnlinkAccount(d *sql.DB, who *access.Principal, id int64, hubKey string, connID int64, ip string) error {
	return change(d, who, id, func(tx *sql.Tx, l *linkrepo.Link) error {
		v, _, err := viewOf(tx, who, *l)
		if err != nil {
			return err
		}
		m, err := accountMembers(tx, v)
		if err != nil {
			return err
		}
		entries, err := linkrepo.Entries(tx, id, string(caps.Accounts))
		if err != nil {
			return err
		}
		for _, e := range entries {
			if hk, ok := keyIn(e, m[0].ID); ok && hk.State == linkrepo.KeyConfirmed && hk.Key == hubKey {
				if err := linkrepo.DeleteKey(tx, e.ID, connID); err != nil {
					return err
				}
			}
		}
		return audit.Log(tx, &who.UserID, "verbund.account_unlinked", l.Name, ip, map[string]any{"account": hubKey, "connection": connID})
	})
}

// ConfirmAccounts stores every suggested cell. Requires EDIT on every
// member.
func ConfirmAccounts(ctx context.Context, d *sql.DB, who *access.Principal, id int64, ip string) (int, error) {
	view, err := Accounts(ctx, d, who, id)
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
				if err := putCell(tx, id, caps.Accounts, view.Hub.ConnID, r.Key, view.Columns[i].ConnID, c.Key); err != nil {
					return takenAccount(err)
				}
				n++
			}
		}
		return audit.Log(tx, &who.UserID, "verbund.account_linked", l.Name, ip, map[string]any{"confirmed": n})
	})
	return n, err
}

// SetRole names what an account of a connection is used for, and the
// same for its linked accounts in every stored Verbund of the
// connection. Requires MANAGE on each connection written.
func SetRole(d *sql.DB, who *access.Principal, connID int64, key string, role sources.AccountRole, ip string) error {
	if key == "" {
		return ErrUnknownAccount
	}
	if !role.Known() {
		return ErrUnknownRole
	}
	targets := map[int64]string{connID: key}
	err := db.WithRead(d, func(tx *sql.Tx) error {
		all, err := linkrepo.All(tx)
		if err != nil {
			return err
		}
		for _, l := range all {
			if !slices.ContainsFunc(l.Members, func(m linkrepo.Member) bool { return m.ConnID == connID }) {
				continue
			}
			entries, err := linkrepo.Entries(tx, l.ID, string(caps.Accounts))
			if err != nil {
				return err
			}
			for _, e := range entries {
				if k, ok := keyIn(e, connID); !ok || k.Key != key {
					continue
				}
				for _, other := range e.Keys {
					if other.Key != "" {
						targets[other.ConnID] = other.Key
					}
				}
			}
		}
		return nil
	})
	if err != nil {
		return err
	}

	for conn, k := range targets {
		if err := setOption(d, who, conn, k, role); err != nil {
			return err
		}
	}
	return audit.Log(d, &who.UserID, "verbund.account_role", key, ip, map[string]any{"role": string(role), "connections": len(targets)})
}

// setOption writes one account's role into a connection's options; the
// unknown role removes it.
func setOption(d *sql.DB, who *access.Principal, connID int64, key string, role sources.AccountRole) error {
	conn, err := connections.ByID(d, connID)
	if err != nil {
		return err
	}
	if conn == nil {
		return ErrUnknownAccount
	}
	options := maps.Clone(conn.Options)
	if options == nil {
		options = map[string]any{}
	}
	roles := map[string]any{}
	if old, ok := options[sources.OptAccountRoles].(map[string]any); ok {
		maps.Copy(roles, old)
	}
	if role == sources.RoleUnknown {
		delete(roles, key)
	} else {
		roles[key] = string(role)
	}
	options[sources.OptAccountRoles] = roles
	return connections.SetOptions(d, who, connID, options)
}
