package settings

import (
	"log/slog"
	"testing"
)

// The environment wins; the UI fills what it left unset; bad values are
// ignored.
func TestWithEnvWins(t *testing.T) {
	t.Setenv("SMTP_URL", "smtp://env.lan")
	t.Setenv("ANTHROPIC_API_KEY", "env-key")
	s := Load()
	if !s.FromEnv("SMTP_URL") || !s.FromEnv("ANTHROPIC_API_KEY") || s.FromEnv("APPRISE_API_URL") {
		t.Fatalf("env: %v", s.env)
	}

	got := s.With(map[string]string{
		"SMTP_URL": "smtp://ui.lan", "ANTHROPIC_API_KEY": "ui-key", "APPRISE_API_URL": "https://apprise.lan",
		"ANALYSIS_MINUTES": "15", "SESSION_IDLE_MINUTES": "0", "LOG_LEVEL": "debug",
	})
	if got.SMTPURL != "smtp://env.lan" || got.AnthropicAPIKey != "env-key" {
		t.Fatalf("env lost: %+v", got)
	}
	if got.AppriseAPIURL != "https://apprise.lan" || got.AnalysisMinutes != 15 || got.LogLevel != "DEBUG" {
		t.Fatalf("ui values not applied: %+v", got)
	}
	if got.SessionIdleMinutes != s.SessionIdleMinutes {
		t.Fatal("invalid number applied")
	}
}

func TestPublishSetsLevel(t *testing.T) {
	s := Load()
	s.LogLevel = "WARN"
	Publish(s)
	t.Cleanup(func() { live.Store(nil); Level.Set(slog.LevelInfo) })

	if Level.Level() != slog.LevelWarn || Live(Settings{}).LogLevel != "WARN" {
		t.Fatalf("level %v", Level.Level())
	}
}
