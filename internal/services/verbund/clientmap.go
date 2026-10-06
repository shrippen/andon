package verbund

import (
	"strconv"

	"andon/internal/caps"
	"andon/internal/db"
	"andon/internal/metrics"
	linkrepo "andon/internal/repos/links"
)

// LinkWith is the one stored Verbund holding every connection, 0 if
// none or several.
func LinkWith(q db.Queryer, connIDs ...int64) (int64, error) {
	all, err := linkrepo.All(q)
	if err != nil {
		return 0, err
	}
	var found []int64
	for _, l := range all {
		all := true
		for _, id := range connIDs {
			all = all && hasConn(l, id)
		}
		if all {
			found = append(found, l.ID)
		}
	}
	if len(found) != 1 {
		return 0, nil
	}
	return found[0], nil
}

// ClientMapFor is the customer links of the Verbund of a Kimai and an
// Invoice Ninja connection; empty (the same name decides) without one.
func ClientMapFor(q db.Queryer, kimaiConn, ninjaConn int64) (metrics.ClientMap, error) {
	if kimaiConn == 0 || ninjaConn == 0 {
		return nil, nil
	}
	id, err := LinkWith(q, kimaiConn, ninjaConn)
	if err != nil || id == 0 {
		return nil, err
	}
	return clientMapOf(q, id, kimaiConn, ninjaConn)
}

// clientMapOf reads the Verbund's customer entries: Kimai id → Ninja key,
// "" for no counterpart. Suggestions are not stored, so only confirmed
// and "none" keys count.
func clientMapOf(q db.Queryer, linkID, kimaiConn, ninjaConn int64) (metrics.ClientMap, error) {
	keys, err := keysBetween(q, linkID, kimaiConn, ninjaConn)
	if err != nil {
		return nil, err
	}
	out := metrics.ClientMap{}
	for kimai, ninja := range keys {
		if id, err := strconv.ParseInt(kimai, 10, 64); err == nil {
			out[id] = ninja
		}
	}
	return out, nil
}

// PayerMapFor is the customer links of the Verbund of a Sure and an
// Invoice Ninja connection: payer → client reference.
func PayerMapFor(q db.Queryer, sureConn, ninjaConn int64) (metrics.PayerMap, error) {
	if sureConn == 0 || ninjaConn == 0 {
		return nil, nil
	}
	id, err := LinkWith(q, sureConn, ninjaConn)
	if err != nil || id == 0 {
		return nil, err
	}
	keys, err := keysBetween(q, id, sureConn, ninjaConn)
	return metrics.PayerMap(keys), err
}

// DocsMapFor is the customer links of the Verbund of an Invoice Ninja and
// a Paperless connection: client reference → correspondent id.
func DocsMapFor(q db.Queryer, ninjaConn, paperlessConn int64) (metrics.DocsMap, error) {
	if ninjaConn == 0 || paperlessConn == 0 {
		return nil, nil
	}
	id, err := LinkWith(q, ninjaConn, paperlessConn)
	if err != nil || id == 0 {
		return nil, err
	}
	keys, err := keysBetween(q, id, ninjaConn, paperlessConn)
	if err != nil {
		return nil, err
	}
	out := metrics.DocsMap{}
	for ref, corr := range keys {
		if n, err := strconv.ParseInt(corr, 10, 64); err == nil {
			out[ref] = n
		}
	}
	return out, nil
}

// keysBetween reads the customer entries holding a key of connection a
// and one of b: a's key → b's key, "" where b has none.
func keysBetween(q db.Queryer, linkID, a, b int64) (map[string]string, error) {
	entries, err := linkrepo.Entries(q, linkID, string(caps.Customers))
	if err != nil {
		return nil, err
	}
	out := map[string]string{}
	for _, e := range entries {
		ka, okA := keyIn(e, a)
		kb, okB := keyIn(e, b)
		if !okA || !okB || ka.Key == "" {
			continue
		}
		out[ka.Key] = kb.Key
	}
	return out, nil
}
