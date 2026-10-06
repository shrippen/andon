// Package links stores Verbünde (connections that work together) and
// their entries: things that exist in several members, one key each.
package links

import (
	"database/sql"
	"errors"
	"strings"
	"time"

	"andon/internal/db"
)

// Link is a Verbund with its members.
type Link struct {
	ID        int64
	Name      string
	CreatedBy *int64
	CreatedAt time.Time
	Members   []Member
}

// Member is one connection of a Verbund.
type Member struct {
	ConnID  int64
	Service string
}

// Entry is one thing in several members, e.g. a customer.
type Entry struct {
	ID     int64
	Domain string
	Keys   []Key
}

// KeyState says how sure an entry's key is.
type KeyState string

const (
	KeySuggested KeyState = "suggested"
	KeyConfirmed KeyState = "confirmed"
	KeyNone      KeyState = "none" // no counterpart in this member
)

// Key is a member's id in an entry ("" with KeyNone).
type Key struct {
	ConnID int64
	Key    string
	State  KeyState
}

var (
	// ErrServiceTaken: the Verbund has a member of that service already.
	ErrServiceTaken = errors.New("links.service_taken")
	// ErrKeyTaken: the id is in another entry of the Verbund already.
	ErrKeyTaken = errors.New("links.key_taken")
)

// unique turns a UNIQUE violation into want.
func unique(err error, want error) error {
	if err != nil && strings.Contains(err.Error(), "UNIQUE constraint failed") {
		return want
	}
	return err
}

// Add creates an empty Verbund.
func Add(q db.Queryer, name string, by *int64, at time.Time) (int64, error) {
	res, err := q.Exec("INSERT INTO links (name, created_by, created_at) VALUES (?, ?, ?)", name, by, db.TimeStr(at))
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// Rename changes a Verbund's name.
func Rename(q db.Queryer, id int64, name string) error {
	_, err := q.Exec("UPDATE links SET name = ? WHERE id = ?", name, id)
	return err
}

// Delete removes a Verbund with its members and entries.
func Delete(q db.Queryer, id int64) error {
	_, err := q.Exec("DELETE FROM links WHERE id = ?", id)
	return err
}

// AddMember puts a connection into a Verbund.
func AddMember(q db.Queryer, id, connID int64, service string) error {
	_, err := q.Exec("INSERT INTO link_members (link_id, connection_id, service) VALUES (?, ?, ?)", id, connID, service)
	return unique(err, ErrServiceTaken)
}

// RemoveMember takes a connection out; below two members the Verbund
// goes (trigger link_members_gone), its keys of the connection too.
func RemoveMember(q db.Queryer, id, connID int64) error {
	if _, err := q.Exec("DELETE FROM link_keys WHERE link_id = ? AND connection_id = ?", id, connID); err != nil {
		return err
	}
	_, err := q.Exec("DELETE FROM link_members WHERE link_id = ? AND connection_id = ?", id, connID)
	return err
}

// All lists every Verbund with its members, by name.
func All(q db.Queryer) ([]Link, error) {
	rows, err := q.Query("SELECT id, name, created_by, created_at FROM links ORDER BY name, id")
	if err != nil {
		return nil, err
	}
	var out []Link
	for rows.Next() {
		var l Link
		var at string
		if err := rows.Scan(&l.ID, &l.Name, &l.CreatedBy, &at); err != nil {
			rows.Close()
			return nil, err
		}
		l.CreatedAt, _ = db.ParseTime(at)
		out = append(out, l)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return withMembers(q, out)
}

// Get returns one Verbund, nil if there is none.
func Get(q db.Queryer, id int64) (*Link, error) {
	var l Link
	var at string
	err := q.QueryRow("SELECT id, name, created_by, created_at FROM links WHERE id = ?", id).Scan(&l.ID, &l.Name, &l.CreatedBy, &at)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	l.CreatedAt, _ = db.ParseTime(at)
	list, err := withMembers(q, []Link{l})
	if err != nil {
		return nil, err
	}
	return &list[0], nil
}

// withMembers fills the members of list.
func withMembers(q db.Queryer, list []Link) ([]Link, error) {
	rows, err := q.Query("SELECT link_id, connection_id, service FROM link_members ORDER BY service, connection_id")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	at := map[int64]int{}
	for i, l := range list {
		at[l.ID] = i
	}
	for rows.Next() {
		var id int64
		var m Member
		if err := rows.Scan(&id, &m.ConnID, &m.Service); err != nil {
			return nil, err
		}
		if i, ok := at[id]; ok {
			list[i].Members = append(list[i].Members, m)
		}
	}
	return list, rows.Err()
}

// Entries lists a Verbund's entries of a domain with their keys.
func Entries(q db.Queryer, id int64, domain string) ([]Entry, error) {
	rows, err := q.Query(`SELECT e.id, k.connection_id, k.key, k.state FROM link_entries e
		JOIN link_keys k ON k.entry_id = e.id WHERE e.link_id = ? AND e.domain = ? ORDER BY e.id, k.connection_id`, id, domain)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Entry
	for rows.Next() {
		var entryID int64
		var k Key
		if err := rows.Scan(&entryID, &k.ConnID, &k.Key, &k.State); err != nil {
			return nil, err
		}
		if len(out) == 0 || out[len(out)-1].ID != entryID {
			out = append(out, Entry{ID: entryID, Domain: domain})
		}
		out[len(out)-1].Keys = append(out[len(out)-1].Keys, k)
	}
	return out, rows.Err()
}

// PutEntry stores an entry with its keys (at least two).
func PutEntry(q db.Queryer, id int64, domain string, keys []Key, at time.Time) (int64, error) {
	res, err := q.Exec("INSERT INTO link_entries (link_id, domain, updated_at) VALUES (?, ?, ?)", id, domain, db.TimeStr(at))
	if err != nil {
		return 0, err
	}
	entryID, err := res.LastInsertId()
	if err != nil {
		return 0, err
	}
	for _, k := range keys {
		if err := SetKey(q, entryID, id, domain, k); err != nil {
			return 0, err
		}
	}
	return entryID, nil
}

// SetKey sets one member's key of an entry.
func SetKey(q db.Queryer, entryID, id int64, domain string, k Key) error {
	_, err := q.Exec(`INSERT INTO link_keys (entry_id, link_id, domain, connection_id, key, state) VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT (entry_id, connection_id) DO UPDATE SET key = excluded.key, state = excluded.state`,
		entryID, id, domain, k.ConnID, k.Key, string(k.State))
	return unique(err, ErrKeyTaken)
}

// DeleteKey removes one member's key of an entry; an entry left with one
// key goes too (trigger link_keys_gone).
func DeleteKey(q db.Queryer, entryID, connID int64) error {
	_, err := q.Exec("DELETE FROM link_keys WHERE entry_id = ? AND connection_id = ?", entryID, connID)
	return err
}

// DeleteEntry removes an entry with its keys.
func DeleteEntry(q db.Queryer, entryID int64) error {
	_, err := q.Exec("DELETE FROM link_entries WHERE id = ?", entryID)
	return err
}
