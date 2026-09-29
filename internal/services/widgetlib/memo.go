package widgetlib

import (
	"context"
	"database/sql"
	"sync"

	"andon/internal/db"
	"andon/internal/model"
	"andon/internal/repos/content"
	"andon/internal/services/access"
	"andon/internal/services/hints"
	"andon/internal/widgets"
)

// memo shares lookups among the tiles of one render: each link tile
// looked up every space's connections for its host, each tile with a
// connection loaded all open hints for its badge, each down-links tile
// every link of its space. A 240-tile board repeated them per tile.
//
//	boards.Fragments ─ WithMemo(ctx) ─► Load × tiles ─► memo (once per key)
//
// Tiles load in parallel, hence the lock. A nil memo looks everything up.
type memo struct {
	mu     sync.Mutex
	conns  map[int64][]*model.Connection // by space
	badges map[int64]hints.Badge         // by connection, nil until loaded
	down   map[int64][]widgets.DownLink  // by space
}

type memoKey struct{}

// WithMemo marks ctx as one render of many tiles for the same viewer:
// Loads that share it look shared data up once.
func WithMemo(ctx context.Context) context.Context {
	return context.WithValue(ctx, memoKey{}, &memo{conns: map[int64][]*model.Connection{}, down: map[int64][]widgets.DownLink{}})
}

func memoOf(ctx context.Context) *memo {
	m, _ := ctx.Value(memoKey{}).(*memo)
	return m
}

// connections are a space's connections.
func (m *memo) connections(q db.Queryer, spaceID int64) ([]*model.Connection, error) {
	if m == nil {
		return content.Connections(q, []int64{spaceID})
	}
	m.mu.Lock()
	defer m.mu.Unlock()

	if list, ok := m.conns[spaceID]; ok {
		return list, nil
	}
	list, err := content.Connections(q, []int64{spaceID})
	if err != nil {
		return nil, err
	}
	m.conns[spaceID] = list
	return list, nil
}

// badge is a connection's open hints.
func (m *memo) badge(d *sql.DB, who *access.Principal, connID int64) (hints.Badge, error) {
	if m == nil {
		count, top, err := hints.CountFor(d, who, connID)
		return hints.Badge{Count: count, Top: top}, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.badges == nil {
		all, err := hints.Badges(d, who)
		if err != nil {
			return hints.Badge{}, err
		}
		m.badges = all
	}
	return m.badges[connID], nil
}

// linksDown is a space's failing link tiles.
func (m *memo) linksDown(ctx context.Context, d *sql.DB, who *access.Principal, spaceID int64) ([]widgets.DownLink, error) {
	if m == nil {
		return linksDown(ctx, d, who, spaceID)
	}
	m.mu.Lock()
	defer m.mu.Unlock()

	if links, ok := m.down[spaceID]; ok {
		return links, nil
	}
	links, err := linksDown(ctx, d, who, spaceID)
	if err != nil {
		return nil, err
	}
	m.down[spaceID] = links
	return links, nil
}
