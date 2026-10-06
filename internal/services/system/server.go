package system

// Server settings: environment variables an admin may also set in the
// UI (settings.Fields). Stored under ServerKey by variable name, secrets
// encrypted as "<NAME>_enc"; the environment always wins.

import (
	"database/sql"
	"encoding/base64"
	"errors"
	"strings"

	"andon/internal/crypto"
	"andon/internal/db"
	"andon/internal/repos/misc"
	"andon/internal/services/access"
	"andon/internal/services/assist"
	"andon/internal/services/audit"
	"andon/internal/services/mail"
	"andon/internal/services/summary"
	"andon/internal/settings"
)

// ServerKey stores the UI's server settings.
const ServerKey = "server"

// encSuffix marks a sealed value (maintenance.sealedPurpose).
const encSuffix = "_enc"

// ErrInvalidSetting means a value does not fit its field.
var ErrInvalidSetting = errors.New("error.invalid_setting")

// ServerField is one setting as the form shows it; secrets have no
// Value, only HasSecret.
type ServerField struct {
	Env       string
	Kind      settings.FieldKind
	Value     string
	Locked    bool // set by the environment
	HasSecret bool
}

// Server lists the settings with their effective values. Admin only.
func Server(d *sql.DB, who *access.Principal, env settings.Settings) ([]ServerField, error) {
	if !who.IsAdmin() {
		return nil, ErrDenied
	}
	stored, err := misc.Setting(d, ServerKey)
	if err != nil {
		return nil, err
	}

	live := settings.Live(env)
	out := make([]ServerField, 0, len(settings.Fields))
	for _, f := range settings.Fields {
		field := ServerField{Env: f.Env, Kind: f.Kind, Locked: env.FromEnv(f.Env), Value: f.Value(live)}
		if f.Kind == settings.KindSecret {
			field.HasSecret = field.Value != "" || stored[f.Env+encSuffix] != nil
			field.Value = ""
		}
		out = append(out, field)
	}
	return out, nil
}

// SetServer stores the unlocked fields of values and applies them. An
// empty secret keeps the stored one; an empty value restores the
// default. Admin only.
func SetServer(d *sql.DB, who *access.Principal, env settings.Settings, values map[string]string, ip string) error {
	if !who.IsAdmin() {
		return ErrDenied
	}
	for _, f := range settings.Fields {
		v := strings.TrimSpace(values[f.Env])
		if v != "" && !f.Valid(v) {
			return ErrInvalidSetting
		}
	}

	err := db.WithTx(d, func(tx *sql.Tx) error {
		stored, err := misc.Setting(tx, ServerKey)
		if err != nil {
			return err
		}
		next, changed, err := serverValues(stored, env, values)
		if err != nil || len(changed) == 0 {
			return err
		}
		if err := misc.SetSetting(tx, ServerKey, next); err != nil {
			return err
		}
		return audit.Log(tx, &who.UserID, "settings.server", "", ip, changed)
	})
	if err != nil {
		return err
	}
	return ApplyServer(d, env)
}

// serverValues merges the form into stored and names what changed;
// secrets show as "changed", never with their value.
func serverValues(stored map[string]any, env settings.Settings, values map[string]string) (map[string]any, map[string]any, error) {
	next := map[string]any{}
	for k, v := range stored {
		next[k] = v
	}

	changed := map[string]any{}
	for _, f := range settings.Fields {
		if env.FromEnv(f.Env) {
			continue
		}
		v := strings.TrimSpace(values[f.Env])
		if f.Kind != settings.KindSecret {
			if old, _ := next[f.Env].(string); old != v {
				changed[f.Env] = v
			}
			next[f.Env] = v
			continue
		}
		if v == "" {
			continue // keep the stored secret
		}
		sealed, err := crypto.Encrypt(v, crypto.PurposeSetting, nil)
		if err != nil {
			return nil, nil, err
		}
		next[f.Env+encSuffix] = base64.StdEncoding.EncodeToString(sealed)
		changed[f.Env] = "changed"
	}
	return next, changed, nil
}

// ApplyServer publishes env with the stored settings, at start and
// after every save, and hands them to the services that keep a copy.
func ApplyServer(d *sql.DB, env settings.Settings) error {
	stored, err := misc.Setting(d, ServerKey)
	if err != nil {
		return err
	}

	values := map[string]string{}
	for _, f := range settings.Fields {
		if f.Kind != settings.KindSecret {
			values[f.Env], _ = stored[f.Env].(string)
			continue
		}
		sealed, _ := stored[f.Env+encSuffix].(string)
		if sealed == "" {
			continue
		}
		blob, err := base64.StdEncoding.DecodeString(sealed)
		if err != nil {
			return err
		}
		if values[f.Env], err = crypto.Decrypt(blob, crypto.PurposeSetting); err != nil {
			return err
		}
	}

	live := env.With(values)
	settings.Publish(live)
	mail.Init(live)
	summary.Init(live)
	assist.Init(live)
	return nil
}
