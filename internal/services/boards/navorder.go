package boards

import (
	"database/sql"

	"andon/internal/db"
	"andon/internal/repos/users"
	"andon/internal/services/access"
)

// navKey holds the viewer's own board order in User.Prefs:
//
//	{"order": [3, 1, 7], "hidden": [7]}
const navKey = "nav_boards"

// Move is a direction in the board order.
type Move int

const (
	MoveUp Move = iota
	MoveDown
)

type navPrefs struct {
	order  []int64
	hidden map[int64]bool
}

func loadNav(q db.Queryer, userID int64) (navPrefs, error) {
	p := navPrefs{hidden: map[int64]bool{}}
	u, err := users.Get(q, userID)
	if err != nil || u == nil {
		return p, err
	}
	raw, _ := u.Prefs[navKey].(map[string]any)
	p.order = idList(raw["order"])
	for _, id := range idList(raw["hidden"]) {
		p.hidden[id] = true
	}
	return p, nil
}

func idList(v any) []int64 {
	list, _ := v.([]any)
	var out []int64
	for _, x := range list {
		if f, ok := x.(float64); ok {
			out = append(out, int64(f))
		}
	}
	return out
}

func saveNav(q db.Queryer, userID int64, p navPrefs) error {
	u, err := users.Get(q, userID)
	if err != nil || u == nil {
		return err
	}
	hidden := []any{}
	for _, id := range p.order {
		if p.hidden[id] {
			hidden = append(hidden, float64(id))
		}
	}
	order := make([]any, len(p.order))
	for i, id := range p.order {
		order[i] = float64(id)
	}
	if u.Prefs == nil {
		u.Prefs = map[string]any{}
	}
	u.Prefs[navKey] = map[string]any{"order": order, "hidden": hidden}
	return users.Update(q, u)
}

// arrange puts refs in the saved order; boards it does not know follow
// in their usual order.
func (p navPrefs) arrange(refs []BoardRef) []BoardRef {
	byID := map[int64]BoardRef{}
	for _, r := range refs {
		byID[r.ID] = r
	}
	out := make([]BoardRef, 0, len(refs))
	placed := map[int64]bool{}
	for _, id := range p.order {
		if r, ok := byID[id]; ok && !placed[id] {
			placed[id] = true
			out = append(out, r)
		}
	}
	for _, r := range refs {
		if !placed[r.ID] {
			out = append(out, r)
		}
	}
	for i := range out {
		out[i].Hidden = p.hidden[out[i].ID]
	}
	return out
}

// Listed is Visible in the viewer's own order, hidden boards marked.
func Listed(d *sql.DB, who *access.Principal) ([]BoardRef, error) {
	refs, err := Visible(d, who)
	if err != nil {
		return nil, err
	}
	p, err := loadNav(d, who.UserID)
	if err != nil {
		return nil, err
	}
	return p.arrange(refs), nil
}

// Nav is what the header lists: Listed without the hidden boards.
func Nav(d *sql.DB, who *access.Principal) ([]BoardRef, error) {
	refs, err := Listed(d, who)
	if err != nil {
		return nil, err
	}
	out := refs[:0]
	for _, r := range refs {
		if !r.Hidden {
			out = append(out, r)
		}
	}
	return out, nil
}

// changeNav rewrites the saved order from the current full list.
func changeNav(d *sql.DB, who *access.Principal, change func(order []int64, p *navPrefs) []int64) error {
	refs, err := Visible(d, who)
	if err != nil {
		return err
	}
	return db.WithTx(d, func(tx *sql.Tx) error {
		p, err := loadNav(tx, who.UserID)
		if err != nil {
			return err
		}
		arranged := p.arrange(refs)
		order := make([]int64, len(arranged))
		for i, r := range arranged {
			order[i] = r.ID
		}
		p.order = change(order, &p)
		return saveNav(tx, who.UserID, p)
	})
}

// MoveNav moves one board a place up or down in the viewer's order.
func MoveNav(d *sql.DB, who *access.Principal, boardID int64, move Move) error {
	return changeNav(d, who, func(order []int64, _ *navPrefs) []int64 {
		for i, id := range order {
			if id != boardID {
				continue
			}
			j := i - 1
			if move == MoveDown {
				j = i + 1
			}
			if j >= 0 && j < len(order) {
				order[i], order[j] = order[j], order[i]
			}
			break
		}
		return order
	})
}

// ToggleNav shows or hides one board in the viewer's navigation.
func ToggleNav(d *sql.DB, who *access.Principal, boardID int64) error {
	return changeNav(d, who, func(order []int64, p *navPrefs) []int64 {
		p.hidden[boardID] = !p.hidden[boardID]
		return order
	})
}
