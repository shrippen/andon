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
	entries, err := linkrepo.Entries(q, linkID, string(caps.Customers))
	if err != nil {
		return nil, err
	}
	out := metrics.ClientMap{}
	for _, e := range entries {
		kimai, ninja, ok := pairKeys(e, kimaiConn, ninjaConn)
		if !ok {
			continue
		}
		id, err := strconv.ParseInt(kimai, 10, 64)
		if err != nil {
			continue
		}
		out[id] = ninja
	}
	return out, nil
}

// pairKeys reads an entry's Kimai id and Ninja key.
func pairKeys(e linkrepo.Entry, kimaiConn, ninjaConn int64) (string, string, bool) {
	var kimai, ninja string
	var haveKimai, haveNinja bool
	for _, k := range e.Keys {
		switch k.ConnID {
		case kimaiConn:
			kimai, haveKimai = k.Key, k.Key != ""
		case ninjaConn:
			ninja, haveNinja = k.Key, true
			if k.State == linkrepo.KeyNone {
				ninja = ""
			}
		}
	}
	return kimai, ninja, haveKimai && haveNinja
}
