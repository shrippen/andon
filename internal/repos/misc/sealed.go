package misc

// Sealed values: ciphertext sits in BLOB columns named *_enc, and inside
// JSON under keys named *_enc as base64 (tile secrets, the OIDC secret):
//
//	connections.secret_enc          <blob>
//	widgets.config                  {"url": …, "api_key_enc": "q3…"}
//
// Key rotation finds them all here, so a new place is not forgotten.

import (
	"fmt"
	"strings"

	"andon/internal/db"
)

// Column names one table column, e.g. connections.secret_enc.
type Column struct {
	Table, Name string
}

func (c Column) String() string { return c.Table + "." + c.Name }

// sealedSuffix ends every column and JSON key holding ciphertext.
const sealedSuffix = "_enc"

// quote makes an identifier from the schema safe in SQL.
func quote(name string) string { return `"` + strings.ReplaceAll(name, `"`, `""`) + `"` }

// columns lists the columns of every table, e.g. users → [id email …].
func columns(q db.Queryer) ([]Column, []string, error) {
	rows, err := q.Query(`SELECT m.name, p.name, p.type FROM sqlite_master m
		JOIN pragma_table_info(m.name) p WHERE m.type = 'table' AND m.name NOT LIKE 'sqlite_%'`)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	var cols []Column
	var types []string
	for rows.Next() {
		var c Column
		var kind string
		if err := rows.Scan(&c.Table, &c.Name, &kind); err != nil {
			return nil, nil, err
		}
		cols = append(cols, c)
		types = append(types, strings.ToUpper(kind))
	}
	return cols, types, rows.Err()
}

// SealedColumns lists every *_enc column of the schema.
func SealedColumns(q db.Queryer) ([]Column, error) {
	cols, _, err := columns(q)
	if err != nil {
		return nil, err
	}
	var out []Column
	for _, c := range cols {
		if strings.HasSuffix(c.Name, sealedSuffix) {
			out = append(out, c)
		}
	}
	return out, nil
}

// SealedJSON lists the TEXT columns where some row holds a *_enc key,
// e.g. widgets.config.
func SealedJSON(q db.Queryer) ([]Column, error) {
	cols, types, err := columns(q)
	if err != nil {
		return nil, err
	}
	var out []Column
	for i, c := range cols {
		if types[i] != "TEXT" {
			continue
		}
		var hit int
		err := q.QueryRow(fmt.Sprintf(`SELECT COUNT(*) FROM %s WHERE %s LIKE '%%\%s"%%' ESCAPE '\'`,
			quote(c.Table), quote(c.Name), sealedSuffix)).Scan(&hit)
		if err != nil {
			return nil, err
		}
		if hit > 0 {
			out = append(out, c)
		}
	}
	return out, nil
}

// row is one rowid and its value.
type row[V any] struct {
	id    int64
	value V
}

// readAll reads a column's non-empty values by rowid.
func readAll[V any](q db.Queryer, c Column) ([]row[V], error) {
	rows, err := q.Query(fmt.Sprintf(`SELECT rowid, %s FROM %s WHERE %s IS NOT NULL AND length(%s) > 0`,
		quote(c.Name), quote(c.Table), quote(c.Name), quote(c.Name)))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []row[V]
	for rows.Next() {
		var r row[V]
		if err := rows.Scan(&r.id, &r.value); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// write stores one value by rowid.
func write(q db.Queryer, c Column, id int64, value any) error {
	_, err := q.Exec(fmt.Sprintf(`UPDATE %s SET %s = ? WHERE rowid = ?`, quote(c.Table), quote(c.Name)), value, id)
	return err
}

// Reseal passes every non-empty value of a sealed column through fn and
// stores the result; it returns how many it rewrote.
func Reseal(q db.Queryer, c Column, fn func(blob []byte) ([]byte, error)) (int, error) {
	list, err := readAll[[]byte](q, c)
	if err != nil {
		return 0, err
	}
	for _, r := range list {
		blob, err := fn(r.value)
		if err != nil {
			return 0, fmt.Errorf("%s row %d: %w", c, r.id, err)
		}
		if err := write(q, c, r.id, blob); err != nil {
			return 0, err
		}
	}
	return len(list), nil
}

// RewriteJSON passes every row's JSON of a column to fn and stores it
// when fn changed it; it returns how many rows changed.
func RewriteJSON(q db.Queryer, c Column, fn func(v any) (any, bool, error)) (int, error) {
	list, err := readAll[string](q, c)
	if err != nil {
		return 0, err
	}
	count := 0
	for _, r := range list {
		var v any
		if err := db.FromJSON(r.value, &v); err != nil {
			continue // not JSON: nothing sealed inside
		}
		next, changed, err := fn(v)
		if err != nil {
			return 0, fmt.Errorf("%s row %d: %w", c, r.id, err)
		}
		if !changed {
			continue
		}
		text, err := db.ToJSON(next)
		if err != nil {
			return 0, err
		}
		if err := write(q, c, r.id, text); err != nil {
			return 0, err
		}
		count++
	}
	return count, nil
}
