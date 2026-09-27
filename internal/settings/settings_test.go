package settings_test

import (
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"andon/internal/settings"
)

// Environment values override defaults; broken numbers and flags keep
// the default instead of zeroing it.
func TestLoadReadsEnvironment(t *testing.T) {
	t.Setenv("BASE_URL", "https://dash.example")
	t.Setenv("DATA_DIR", "/srv/dash")
	t.Setenv("ANDON_DEV", "yes please")
	t.Setenv("ANALYSIS_MINUTES", "ten")

	s := settings.Load()
	if s.BaseURL != "https://dash.example" || !s.SecureCookies() {
		t.Fatalf("base url: %+v", s)
	}
	if s.DBPath() != "/srv/dash/andon.db" || s.IconsDir() != "/srv/dash/icons" {
		t.Fatalf("paths: %q %q", s.DBPath(), s.IconsDir())
	}
	if s.Dev || s.AnalysisMinutes <= 0 {
		t.Fatalf("bad values must keep defaults: dev=%v minutes=%d", s.Dev, s.AnalysisMinutes)
	}
}

// TestSecretsFromFiles: the entrypoint hands secrets over as open file
// descriptors (MASTER_KEY_FD=3), so they never sit in the environment;
// NAME_FILE names a file.
func TestSecretsFromFiles(t *testing.T) {
	dir := t.TempDir()
	keyPath, oidcPath := filepath.Join(dir, "key"), filepath.Join(dir, "oidc")
	if err := os.WriteFile(keyPath, []byte("from-fd\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(oidcPath, []byte("from-file"), 0o600); err != nil {
		t.Fatal(err)
	}
	f, err := os.Open(keyPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("MASTER_KEY_FD", strconv.Itoa(int(f.Fd())))
	t.Setenv("OIDC_CLIENT_SECRET_FILE", oidcPath)
	t.Setenv("SMTP_PASSWORD", "plain")

	s := settings.Load()
	if s.MasterKey != "from-fd" || s.OIDCClientSecret != "from-file" || s.SMTPPassword != "plain" {
		t.Fatalf("secrets: %q %q %q", s.MasterKey, s.OIDCClientSecret, s.SMTPPassword)
	}
	if os.Getenv("MASTER_KEY_FD") != "" || os.Getenv("SMTP_PASSWORD") != "" {
		t.Fatal("secrets must leave the environment after loading")
	}
}
