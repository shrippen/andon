package settings

// Settings an admin may also set in the UI (Admin → Settings → Server).
// The environment wins: a field set there is shown locked.
//
//	env ──┐
//	      ├─► With(stored) ──► Publish ──► Live(base) / Level
//	UI ───┘   (env first)

import (
	"log/slog"
	"os"
	"strconv"
	"strings"
	"sync/atomic"
)

// FieldKind says how a field is edited and checked.
type FieldKind string

const (
	KindText   FieldKind = "text"
	KindSecret FieldKind = "secret" // stored encrypted, never shown
	KindNumber FieldKind = "number" // whole number ≥ 1
	KindLevel  FieldKind = "level"  // DEBUG, INFO, WARN, ERROR
)

// Field is one setting by its environment variable name.
type Field struct {
	Env  string
	Kind FieldKind
	get  func(Settings) string
	set  func(*Settings, string)
}

// Fields lists the settings the UI offers, in display order.
var Fields = []Field{
	text("SMTP_URL", func(s *Settings) *string { return &s.SMTPURL }),
	text("SMTP_FROM", func(s *Settings) *string { return &s.SMTPFrom }),
	secret("SMTP_PASSWORD", func(s *Settings) *string { return &s.SMTPPassword }),
	text("APPRISE_API_URL", func(s *Settings) *string { return &s.AppriseAPIURL }),
	secret("ANTHROPIC_API_KEY", func(s *Settings) *string { return &s.AnthropicAPIKey }),
	number("ANALYSIS_MINUTES", func(s *Settings) *int { return &s.AnalysisMinutes }),
	number("SESSION_IDLE_MINUTES", func(s *Settings) *int { return &s.SessionIdleMinutes }),
	number("SESSION_ABSOLUTE_HOURS", func(s *Settings) *int { return &s.SessionAbsoluteHours }),
	number("OIDC_SESSION_HOURS", func(s *Settings) *int { return &s.OIDCSessionHours }),
	{Env: "LOG_LEVEL", Kind: KindLevel,
		get: func(s Settings) string { return s.LogLevel },
		set: func(s *Settings, v string) { s.LogLevel = strings.ToUpper(v) }},
}

func text(env string, at func(*Settings) *string) Field {
	return Field{Env: env, Kind: KindText,
		get: func(s Settings) string { return *at(&s) },
		set: func(s *Settings, v string) { *at(s) = v }}
}

func secret(env string, at func(*Settings) *string) Field {
	f := text(env, at)
	f.Kind = KindSecret
	return f
}

func number(env string, at func(*Settings) *int) Field {
	return Field{Env: env, Kind: KindNumber,
		get: func(s Settings) string { return strconv.Itoa(*at(&s)) },
		set: func(s *Settings, v string) { *at(s), _ = strconv.Atoi(v) }}
}

// Valid reports whether v fits the field, e.g. "15" for a number.
func (f Field) Valid(v string) bool {
	switch f.Kind {
	case KindNumber:
		n, err := strconv.Atoi(v)
		return err == nil && n >= 1
	case KindLevel:
		_, ok := parseLevel(v)
		return ok
	}
	return true
}

// Value is the field's current value in s.
func (f Field) Value(s Settings) string { return f.get(s) }

// FromEnv reports whether the environment (or a Docker secret) set env.
func (s Settings) FromEnv(env string) bool { return s.env[env] }

// envSet notes which fields the environment set; secrets are already
// unset from it, so a value means it came from there.
func envSet(s Settings) map[string]bool {
	out := map[string]bool{}
	for _, f := range Fields {
		_, ok := os.LookupEnv(f.Env)
		if ok || (f.Kind == KindSecret && f.get(s) != "") {
			out[f.Env] = true
		}
	}
	return out
}

// With applies stored UI values to every field the environment left
// unset; empty or invalid values keep the default.
func (s Settings) With(stored map[string]string) Settings {
	for _, f := range Fields {
		v := strings.TrimSpace(stored[f.Env])
		if s.FromEnv(f.Env) || v == "" || !f.Valid(v) {
			continue
		}
		f.set(&s, v)
	}
	return s
}

// live is the published effective settings.
var live atomic.Pointer[Settings]

// Level is the log level of the published settings (slog handlers).
var Level = new(slog.LevelVar)

// Publish makes s the settings Live returns and applies its log level.
func Publish(s Settings) {
	live.Store(&s)
	if l, ok := parseLevel(s.LogLevel); ok {
		Level.Set(l)
	}
}

// Live returns base with the Fields of the published settings; base
// alone before any Publish (tests).
func Live(base Settings) Settings {
	p := live.Load()
	if p == nil {
		return base
	}
	for _, f := range Fields {
		f.set(&base, f.get(*p))
	}
	return base
}

// parseLevel reads "info", "WARN" & co.
func parseLevel(v string) (slog.Level, bool) {
	var l slog.Level
	if err := l.UnmarshalText([]byte(strings.ToUpper(strings.TrimSpace(v)))); err != nil {
		return 0, false
	}
	return l, true
}
