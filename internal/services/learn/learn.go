// Package learn keeps the learned models: one per purpose and Invoice
// Ninja, stored, so a model does not start over at each page and keeps
// what it learned from data that is gone.
//
//	job (every Hours) ─► Invoice Ninja dataset ─► metrics.EuerLearn ─► learned_models
//	/vat, /euer       ─► Euer(conn) ─► stored model (+ today's data, in memory)
//	/admin/learning   ─► Overview, SaveSettings, TrainNow, Reset
//
// Only the EÜR line learns so far (PurposeEuer).
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
	"andon/internal/repos/misc"
	"andon/internal/services/access"
	"andon/internal/services/audit"
	"andon/internal/services/svcdata"
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

// Euer is the EÜR model of an Invoice Ninja connection with the
// settings' threshold; none when switched off or not learned yet (the
// page then learns from its own data only).
func Euer(d *sql.DB, connID int64) metrics.EuerLearning {
	s, err := Load(d)
	if err != nil || s.Mode == ModeOff {
		return metrics.EuerLearning{}
	}
	l := metrics.EuerLearning{Model: bayes.New(), MinP: float64(s.MinPercent) / percent}
	stored, ok, err := misc.Learned(d, PurposeEuer, connID)
	if err != nil || !ok {
		return l
	}
	if err := json.Unmarshal([]byte(stored.Model), l.Model); err != nil {
		slog.Warn("learn: stored model unreadable, starting over", "conn", connID, "err", err)
		l.Model = bayes.New()
	}
	return l
}

// Train teaches each Invoice Ninja's model its current expenses, from
// the cached dataset; nothing when switched off.
func Train(ctx context.Context, d *sql.DB, now time.Time) error {
	s, err := Load(d)
	if err != nil || s.Mode == ModeOff {
		return err
	}
	var conns []*model.Connection
	var holders map[int64][]model.Holder
	err = db.WithRead(d, func(tx *sql.Tx) error {
		var err error
		if conns, err = content.AllConnections(tx); err != nil {
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
		if c.Service != string(enums.ServiceInvoiceNinja) {
			continue
		}
		errs = append(errs, trainOne(ctx, d, c, holderOf(c, holders[c.ID]), now))
	}
	return errors.Join(errs...)
}

// holderOf is whose credentials read a connection: anyone's for a
// shared one, the first owner's for a personal one.
func holderOf(c *model.Connection, holders []model.Holder) model.Holder {
	if c.CredentialMode != enums.CredentialPersonal || len(holders) == 0 {
		return model.NoHolder
	}
	return holders[0]
}

func trainOne(ctx context.Context, d *sql.DB, c *model.Connection, h model.Holder, now time.Time) error {
	res, err := svcdata.Get(ctx, d, sources.DataKey(enums.ServiceInvoiceNinja), nil, c, h, svcdata.Cached)
	if err != nil {
		return err
	}
	n, _ := res.Data.(*sources.NinjaDataset)
	if n == nil {
		return nil // not fetched yet or failing: the analysis reports it
	}
	slot := sources.CategoriesOf(c.Options).EuerOf(n.ExpenseFields)
	if slot == 0 {
		return nil
	}

	m := bayes.New()
	stored, ok, err := misc.Learned(d, PurposeEuer, c.ID)
	if err != nil {
		return err
	}
	if ok {
		if err := json.Unmarshal([]byte(stored.Model), m); err != nil {
			return err
		}
	}
	metrics.EuerLearn(m, n, slot)
	text, err := json.Marshal(m)
	if err != nil {
		return err
	}
	return db.WithTx(d, func(tx *sql.Tx) error {
		return misc.SetLearned(tx, misc.LearnedModel{Purpose: PurposeEuer, ConnID: c.ID, Model: string(text), TrainedAt: now.UTC()})
	})
}

// Row is one stored model on the settings page.
type Row struct {
	Purpose   string
	ConnID    int64
	Conn      string
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
		out = append(out, Row{Purpose: s.Purpose, ConnID: s.ConnID, Conn: cmp.Or(names[s.ConnID], "#"), Stats: m.Stats(), TrainedAt: s.TrainedAt})
	}
	return out, nil
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
func Reset(d *sql.DB, who *access.Principal, purpose string, connID int64, ip string) error {
	if !who.IsAdmin() {
		return ErrDenied
	}
	return db.WithTx(d, func(tx *sql.Tx) error {
		if err := misc.DeleteLearned(tx, purpose, connID); err != nil {
			return err
		}
		return audit.Log(tx, &who.UserID, "learn.reset", purpose, ip, map[string]any{"conn": connID})
	})
}
