// Package learn keeps the learned models: one per purpose, connection
// and holder, stored, so a model does not start over at each page and
// keeps what it learned from data that is gone.
//
//	job (every Hours) ─► each Purpose.Teach(dataset) ─┐
//	user decides      ─► Learn(purpose, conn, teach) ─┼─► learned_models
//	pages             ─► Model(purpose, conn, holder) ◄┘  (+ today's data, in memory)
//	/admin/learning   ─► Overview, SaveSettings, TrainNow, Reset
//
// Purposes are registered in an init (Register).
package learn

import (
	"cmp"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"log/slog"
	"time"

	"andon/internal/bayes"
	"andon/internal/db"
	"andon/internal/enums"
	"andon/internal/metrics"
	"andon/internal/model"
	"andon/internal/repos/content"
	linkrepo "andon/internal/repos/links"
	"andon/internal/repos/misc"
	"andon/internal/repos/users"
	"andon/internal/services/access"
	"andon/internal/services/audit"
	"andon/internal/services/svcdata"
	"andon/internal/services/verbund"
	"andon/internal/sources"
)

// JobName is the background training's.
const JobName = "learn"

// PurposeEuer is the model proposing an expense's EÜR line.
const PurposeEuer = "euer"

// settingKey holds the settings in instance_settings.
const settingKey = "learn"

// ErrDenied: only an admin changes the instance's models.
var ErrDenied = errors.New("learn.denied")

// Mode says whether learned proposals are made.
type Mode string

const (
	ModeOn  Mode = "on"
	ModeOff Mode = "off" // only the vendor's last line; the job pauses
)

// Bounds of the settings.
const (
	defaultHours   = 6
	minHours       = 1
	maxHours       = 168
	minPercent     = 50
	maxPercent     = 99
	percent        = 100.0
	defaultPercent = int(metrics.EuerMinP * percent)
)

// Settings are the instance's.
type Settings struct {
	Mode       Mode
	MinPercent int // least probability of a learned proposal, in %
	Hours      int // training interval
}

// Load reads the settings, defaults where unset.
func Load(q db.Queryer) (Settings, error) {
	raw, err := misc.Setting(q, settingKey)
	if err != nil {
		return Settings{}, err
	}
	s := Settings{Mode: ModeOn, MinPercent: defaultPercent, Hours: defaultHours}
	if m, _ := raw["mode"].(string); m == string(ModeOff) {
		s.Mode = ModeOff
	}
	if p, ok := raw["min_percent"].(float64); ok {
		s.MinPercent = int(p)
	}
	if h, ok := raw["hours"].(float64); ok {
		s.Hours = int(h)
	}
	return s.clamped(), nil
}

func (s Settings) clamped() Settings {
	s.MinPercent = min(max(s.MinPercent, minPercent), maxPercent)
	s.Hours = min(max(s.Hours, minHours), maxHours)
	return s
}

// SaveSettings stores them. Admin only.
func SaveSettings(d *sql.DB, who *access.Principal, s Settings, ip string) error {
	if !who.IsAdmin() {
		return ErrDenied
	}
	s = s.clamped()
	if s.Mode != ModeOff {
		s.Mode = ModeOn
	}
	return db.WithTx(d, func(tx *sql.Tx) error {
		if err := misc.SetSetting(tx, settingKey, map[string]any{"mode": string(s.Mode), "min_percent": s.MinPercent, "hours": s.Hours}); err != nil {
			return err
		}
		return audit.Log(tx, &who.UserID, "learn.settings", string(s.Mode), ip, map[string]any{"min_percent": s.MinPercent, "hours": s.Hours})
	})
}

// Every is the job's interval, read before each wait.
func Every(d *sql.DB) time.Duration {
	s, err := Load(d)
	if err != nil {
		return defaultHours * time.Hour
	}
	return time.Duration(s.Hours) * time.Hour
}

// Purpose is one thing a model learns, registered in an init: by the
// service that uses it, or here for one several services use (EÜR).
type Purpose struct {
	Key     string            // "euer"; names the text learn.purpose_<key>
	Service enums.ServiceType // whose connections have a model

	// Teach learns from a connection's cached dataset and says whether the
	// model changed; nil for a purpose that learns only from what the user
	// decides (Learn).
	Teach func(m *bayes.Model, data any, conn *model.Connection) bool

	// Peer, if set, is a service whose cached dataset Teach reads too: the
	// one the Verbund pairs with the connection (the Kimai of a Dawarich).
	// Teach then gets Peered as data.
	Peer enums.ServiceType
}

// Peered is a connection's dataset with its partner's (Purpose.Peer);
// Peer is nil without one.
type Peered struct{ Data, Peer any }

var purposes = map[string]Purpose{}

// Register adds a purpose; a key twice is a programming error.
func Register(p Purpose) {
	if _, dup := purposes[p.Key]; dup {
		panic("learn: purpose registered twice: " + p.Key)
	}
	purposes[p.Key] = p
}

func init() {
	Register(Purpose{Key: PurposeEuer, Service: enums.ServiceInvoiceNinja, Teach: teachEuer})
}

// teachEuer learns the EÜR lines of an Invoice Ninja's expenses.
func teachEuer(m *bayes.Model, data any, conn *model.Connection) bool {
	n, _ := data.(*sources.NinjaDataset)
	if n == nil {
		return false
	}
	slot := sources.CategoriesOf(conn.Options).EuerOf(n.ExpenseFields)
	return slot > 0 && metrics.EuerLearn(m, n, slot)
}

// Model is purpose's model of a connection, as h reads it (a personal
// login has a model per holder), with the settings' threshold; off
// (nil Model) when switched off, empty when not learned yet (the page
// then learns from its own data, in memory).
func Model(d *sql.DB, purpose string, conn *model.Connection, h model.Holder) bayes.Use {
	s, err := Load(d)
	if err != nil || s.Mode == ModeOff || conn == nil {
		return bayes.Use{}
	}
	u := bayes.Use{Model: bayes.New(), MinP: float64(s.MinPercent) / percent}
	stored, ok, err := misc.Learned(d, purpose, conn.ID, svcdata.HolderOf(conn, h))
	if err != nil || !ok {
		return u
	}
	if err := json.Unmarshal([]byte(stored.Model), u.Model); err != nil {
		slog.Warn("learn: stored model unreadable, starting over", "purpose", purpose, "conn", conn.ID, "err", err)
		u.Model = bayes.New()
	}
	return u
}

// Euer is the EÜR model (Model).
func Euer(d *sql.DB, conn *model.Connection, h model.Holder) bayes.Use {
	return Model(d, PurposeEuer, conn, h)
}

// Learn lets purpose's model of a connection learn what the user just
// decided (teach changes the model and says whether it did); nothing when
// switched off.
func Learn(d *sql.DB, purpose string, conn *model.Connection, h model.Holder, teach func(*bayes.Model) bool) error {
	s, err := Load(d)
	if err != nil || s.Mode == ModeOff || conn == nil {
		return err
	}
	return update(d, purpose, conn.ID, svcdata.HolderOf(conn, h), time.Now(), teach)
}

// Train teaches each registered purpose's models the current data of
// their connections, from the cached datasets; nothing when switched
// off. Models of logins taken back go.
func Train(ctx context.Context, d *sql.DB, now time.Time) error {
	s, err := Load(d)
	if err != nil || s.Mode == ModeOff {
		return err
	}
	var conns []*model.Connection
	var holders map[int64][]model.Holder
	var links []linkrepo.Link
	err = db.WithRead(d, func(tx *sql.Tx) error {
		var err error
		if conns, err = content.AllConnections(tx); err != nil {
			return err
		}
		if links, err = linkrepo.All(tx); err != nil {
			return err
		}
		holders, err = content.CredentialHolders(tx)
		return err
	})
	if err != nil {
		return err
	}

	var errs []error
	for _, c := range conns {
		keep := holdersOf(c, holders[c.ID])
		for _, p := range purposes {
			if p.Teach == nil || string(p.Service) != c.Service {
				continue
			}
			peer := peerOf(c, conns, links, p.Peer)
			for _, h := range keep {
				errs = append(errs, trainOne(ctx, d, p, c, peer, h, now))
			}
		}
		errs = append(errs, db.WithTx(d, func(tx *sql.Tx) error { return misc.PruneLearned(tx, c.ID, keep) }))
	}
	return errors.Join(errs...)
}

// holdersOf is whose data a connection has: one for a shared login, each
// holder's own for a personal one (a model each).
func holdersOf(c *model.Connection, holders []model.Holder) []model.Holder {
	if c.CredentialMode != enums.CredentialPersonal {
		return []model.Holder{model.NoHolder}
	}
	return holders
}

func trainOne(ctx context.Context, d *sql.DB, p Purpose, c, peer *model.Connection, h model.Holder, now time.Time) error {
	res, err := svcdata.Get(ctx, d, sources.DataKey(p.Service), nil, c, h, svcdata.Cached)
	if err != nil {
		return err
	}
	if res.Data == nil || res.Error != "" {
		return nil // not fetched yet or failing: the analysis reports it
	}
	data := res.Data
	if p.Peer != "" {
		data = Peered{Data: res.Data, Peer: peerData(ctx, d, p.Peer, peer, h)}
	}
	return update(d, p.Key, c.ID, h, now, func(m *bayes.Model) bool { return p.Teach(m, data, c) })
}

// peerOf is c's partner of service in its space, as the analysis pairs
// them (verbund.Groups: no user, only this space); nil if none or
// ambiguous.
func peerOf(c *model.Connection, conns []*model.Connection, links []linkrepo.Link, service enums.ServiceType) *model.Connection {
	if service == "" {
		return nil
	}
	var here []*model.Connection
	for _, o := range conns {
		if o.SpaceID == c.SpaceID {
			here = append(here, o)
		}
	}
	groups, _ := verbund.Groups(here, links)
	for _, g := range groups {
		if own := g.Conns[c.Service]; own != nil && own.ID == c.ID {
			return g.Conns[string(service)]
		}
	}
	return nil
}

// peerData is the partner's cached dataset, nil without one or when it
// fails: Teach then learns what the connection alone holds.
func peerData(ctx context.Context, d *sql.DB, service enums.ServiceType, peer *model.Connection, h model.Holder) any {
	if peer == nil {
		return nil
	}
	res, err := svcdata.Get(ctx, d, sources.DataKey(service), nil, peer, svcdata.HolderOf(peer, h), svcdata.Cached)
	if err != nil || res.Error != "" {
		return nil
	}
	return res.Data
}

// update reads, teaches and writes one model in one write transaction:
// a reset meanwhile waits and is not overwritten by the model read
// before it. A model that learned nothing new is not written: its time
// says when it last learned something.
func update(d *sql.DB, purpose string, connID int64, h model.Holder, now time.Time, teach func(*bayes.Model) bool) error {
	return db.WithTx(d, func(tx *sql.Tx) error {
		m := bayes.New()
		stored, ok, err := misc.Learned(tx, purpose, connID, h)
		if err != nil {
			return err
		}
		if ok {
			if err := json.Unmarshal([]byte(stored.Model), m); err != nil {
				slog.Warn("learn: stored model unreadable, starting over", "purpose", purpose, "conn", connID, "err", err)
				m, ok = bayes.New(), false
			}
		}
		if !teach(m) && ok {
			return nil // nothing new: kept as it was
		}
		if m.Stats().Examples == 0 && !ok {
			return nil // nothing to keep
		}
		text, err := json.Marshal(m)
		if err != nil {
			return err
		}
		return misc.SetLearned(tx, misc.LearnedModel{Purpose: purpose, ConnID: connID, Holder: h, Model: string(text), TrainedAt: now.UTC()})
	})
}

// Row is one stored model on the settings page.
type Row struct {
	Purpose   string
	ConnID    int64
	Holder    model.Holder
	Conn      string
	Whose     string // the holder's name, "" for a shared login
	Stats     bayes.Stats
	TrainedAt time.Time
}

// Overview lists the stored models. Admin only.
func Overview(d *sql.DB, who *access.Principal) ([]Row, error) {
	if !who.IsAdmin() {
		return nil, ErrDenied
	}
	var stored []misc.LearnedModel
	names := map[int64]string{}
	err := db.WithRead(d, func(tx *sql.Tx) error {
		var err error
		if stored, err = misc.AllLearned(tx); err != nil {
			return err
		}
		conns, err := content.AllConnections(tx)
		for _, c := range conns {
			names[c.ID] = c.Name
		}
		return err
	})
	if err != nil {
		return nil, err
	}

	out := make([]Row, 0, len(stored))
	for _, s := range stored {
		m := bayes.New()
		if err := json.Unmarshal([]byte(s.Model), m); err != nil {
			slog.Warn("learn: stored model unreadable", "conn", s.ConnID, "err", err)
		}
		out = append(out, Row{Purpose: s.Purpose, ConnID: s.ConnID, Holder: s.Holder, Conn: cmp.Or(names[s.ConnID], "#"),
			Whose: holderName(d, s.Holder), Stats: m.Stats(), TrainedAt: s.TrainedAt})
	}
	return out, nil
}

// holderName names a model's holder: a user, a team; "" shared.
func holderName(d *sql.DB, h model.Holder) string {
	if u := h.User(); u > 0 {
		if user, err := users.Get(d, u); err == nil {
			return cmp.Or(user.Name, user.Email)
		}
	}
	if tm := h.Team(); tm > 0 {
		if team, err := users.Team(d, tm); err == nil {
			return team.Name
		}
	}
	return ""
}

// TrainNow trains at once. Admin only.
func TrainNow(ctx context.Context, d *sql.DB, who *access.Principal, ip string) error {
	if !who.IsAdmin() {
		return ErrDenied
	}
	if err := Train(ctx, d, time.Now()); err != nil {
		return err
	}
	return audit.Log(d, &who.UserID, "learn.train", "", ip, nil)
}

// Reset forgets one model; it learns again from the data there is.
// Admin only.
func Reset(d *sql.DB, who *access.Principal, purpose string, connID int64, h model.Holder, ip string) error {
	if !who.IsAdmin() {
		return ErrDenied
	}
	return db.WithTx(d, func(tx *sql.Tx) error {
		if err := misc.DeleteLearned(tx, purpose, connID, h); err != nil {
			return err
		}
		return audit.Log(tx, &who.UserID, "learn.reset", purpose, ip, map[string]any{"conn": connID, "holder": int64(h)})
	})
}
