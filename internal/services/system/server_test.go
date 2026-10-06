package system_test

import (
	"errors"
	"testing"

	"andon/internal/enums"
	"andon/internal/services/system"
	"andon/internal/settings"
	"andon/internal/testkit"
)

// Admins set server settings in the UI; the environment's are locked,
// secrets are never shown and an empty secret keeps the stored one.
func TestServerSettings(t *testing.T) {
	t.Setenv("SMTP_URL", "smtp://env.lan")
	env := settings.Load()
	d := testkit.DB(t)
	admin, _ := testkit.User(t, d, "admin@x.de", enums.RoleAdmin)
	user, _ := testkit.User(t, d, "user@x.de", enums.RoleUser)

	values := map[string]string{"SMTP_URL": "smtp://ui.lan", "ANTHROPIC_API_KEY": "sk-1", "ANALYSIS_MINUTES": "15"}
	if err := system.SetServer(d, user, env, values, ""); !errors.Is(err, system.ErrDenied) {
		t.Fatalf("user: %v", err)
	}
	if err := system.SetServer(d, admin, env, map[string]string{"ANALYSIS_MINUTES": "0"}, ""); err == nil {
		t.Fatal("invalid number accepted")
	}
	if err := system.SetServer(d, admin, env, values, ""); err != nil {
		t.Fatal(err)
	}
	live := settings.Live(env)
	if live.SMTPURL != "smtp://env.lan" || live.AnthropicAPIKey != "sk-1" || live.AnalysisMinutes != 15 {
		t.Fatalf("live: %+v", live)
	}

	// Saving again with an empty secret keeps it.
	if err := system.SetServer(d, admin, env, map[string]string{"ANALYSIS_MINUTES": "20"}, ""); err != nil {
		t.Fatal(err)
	}
	fields, err := system.Server(d, admin, env)
	if err != nil {
		t.Fatal(err)
	}
	byEnv := map[string]system.ServerField{}
	for _, f := range fields {
		byEnv[f.Env] = f
	}
	if f := byEnv["SMTP_URL"]; !f.Locked || f.Value != "smtp://env.lan" {
		t.Fatalf("smtp: %+v", f)
	}
	if f := byEnv["ANTHROPIC_API_KEY"]; f.Value != "" || !f.HasSecret {
		t.Fatalf("secret shown or lost: %+v", f)
	}
	if settings.Live(env).AnthropicAPIKey != "sk-1" || byEnv["ANALYSIS_MINUTES"].Value != "20" {
		t.Fatalf("after second save: %+v", settings.Live(env))
	}

	// A fresh start applies the stored values.
	settings.Publish(env)
	if err := system.ApplyServer(d, env); err != nil {
		t.Fatal(err)
	}
	if settings.Live(env).AnthropicAPIKey != "sk-1" {
		t.Fatal("stored secret not applied at start")
	}
}
