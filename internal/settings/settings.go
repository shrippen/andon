// Package settings reads operating settings from environment variables and
// Docker secrets. Everything a user configures (boards, connections, themes,
// OIDC mapping) lives in the database instead.
package settings

import (
	"io"
	"net/netip"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

const secretsDir = "/run/secrets"

// Settings holds the container's operating configuration.
type Settings struct {
	BaseURL     string
	DataDir     string
	DatabaseURL string

	// Development: a guessable MASTER_KEY is accepted (ANDON_DEV).
	Dev     bool
	Demo    bool
	Testing bool

	MasterKey    string
	SMTPURL      string
	SMTPFrom     string
	SMTPPassword string
	// AnalysisMinutes is the interval of the integration check.
	AnalysisMinutes int
	// AnthropicAPIKey enables the optional weekly summary; "" = off.
	AnthropicAPIKey string

	// AppriseAPIURL points at an existing Apprise API instance (e.g.
	// https://apprise.example.lan). There is no Apprise library for Go, so
	// notify sends an HTTP POST to /notify there with the user's own
	// apprise:// URLs in the request body.
	AppriseAPIURL string

	OIDCIssuer       string
	OIDCClientID     string
	OIDCClientSecret string

	SeedFile         string
	SchedulerEnabled bool
	LogLevel         string

	SessionIdleMinutes   int
	SessionAbsoluteHours int
	OIDCSessionHours     int

	// TrustedProxies are the reverse proxies whose X-Forwarded-For names
	// the client (TRUSTED_PROXIES="172.18.0.0/16,10.0.0.1").
	TrustedProxies []netip.Prefix

	// env names the Fields the environment set (FromEnv).
	env map[string]bool
}

// DBPath returns the sqlite file path when DatabaseURL is unset.
func (s Settings) DBPath() string {
	if s.DatabaseURL != "" {
		return s.DatabaseURL
	}
	return filepath.Join(s.DataDir, "andon.db")
}

// SecureCookies reports whether BaseURL is https, so cookies get Secure set.
func (s Settings) SecureCookies() bool {
	return strings.HasPrefix(s.BaseURL, "https://")
}

// IconsDir is where uploaded icons are stored.
func (s Settings) IconsDir() string { return filepath.Join(s.DataDir, "icons") }

// BackupsDir holds the daily copies of the database.
func (s Settings) BackupsDir() string { return filepath.Join(s.DataDir, "backups") }

// ThemesDir is where user-made themes are stored.
func (s Settings) ThemesDir() string { return filepath.Join(s.DataDir, "themes") }

func envStr(key, def string) string {
	if v, ok := os.LookupEnv(key); ok {
		return v
	}
	return def
}

func envBool(key string, def bool) bool {
	v, ok := os.LookupEnv(key)
	if !ok {
		return def
	}
	b, err := strconv.ParseBool(v)
	if err != nil {
		return def
	}
	return b
}

func envInt(key string, def int) int {
	v, ok := os.LookupEnv(key)
	if !ok {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return def
	}
	return n
}

func readSecret(name string) string {
	return readFile(filepath.Join(secretsDir, name))
}

// Suffixes of env vars that point to a secret instead of holding it:
// a file path, or an open file descriptor. A descriptor is read as is;
// reopening it via /dev/fd/N would check permissions the unprivileged
// process lacks.
const (
	fileSuffix = "_FILE"
	fdSuffix   = "_FD"
)

func readFD(fd string) string {
	n, err := strconv.Atoi(fd)
	if err != nil || n < 3 {
		return ""
	}
	f := os.NewFile(uintptr(n), "secret")
	if f == nil {
		return ""
	}
	defer f.Close()
	data, err := io.ReadAll(f)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

// prefixes reads "10.0.0.0/8, 172.18.0.1": addresses count as /32 or
// /128; what does not parse is left out.
func prefixes(list string) []netip.Prefix {
	var out []netip.Prefix
	for _, part := range strings.Split(list, ",") {
		part = strings.TrimSpace(part)
		if p, err := netip.ParsePrefix(part); err == nil {
			out = append(out, p)
			continue
		}
		if a, err := netip.ParseAddr(part); err == nil {
			out = append(out, netip.PrefixFrom(a, a.BitLen()))
		}
	}
	return out
}

func readFile(path string) string {
	if path == "" {
		return ""
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

// Load reads Settings from the environment, letting Docker secrets win over
// plain environment variables.
func Load() Settings {
	s := Settings{
		BaseURL:              envStr("BASE_URL", "http://localhost:8080"),
		DataDir:              envStr("DATA_DIR", "/data"),
		DatabaseURL:          envStr("DATABASE_URL", ""),
		Dev:                  envBool("ANDON_DEV", false),
		Demo:                 demoMode(),
		Testing:              envBool("ANDON_TESTING", false),
		MasterKey:            envStr("MASTER_KEY", ""),
		SMTPURL:              envStr("SMTP_URL", ""),
		SMTPFrom:             envStr("SMTP_FROM", "Andon <andon@localhost>"),
		SMTPPassword:         envStr("SMTP_PASSWORD", ""),
		AnthropicAPIKey:      envStr("ANTHROPIC_API_KEY", ""),
		OIDCIssuer:           envStr("OIDC_ISSUER", ""),
		OIDCClientID:         envStr("OIDC_CLIENT_ID", ""),
		OIDCClientSecret:     envStr("OIDC_CLIENT_SECRET", ""),
		AppriseAPIURL:        envStr("APPRISE_API_URL", ""),
		SeedFile:             envStr("SEED_FILE", ""),
		SchedulerEnabled:     envBool("SCHEDULER_ENABLED", true),
		LogLevel:             envStr("LOG_LEVEL", "INFO"),
		TrustedProxies:       prefixes(envStr("TRUSTED_PROXIES", "")),
		SessionIdleMinutes:   envInt("SESSION_IDLE_MINUTES", 60*24*7),
		SessionAbsoluteHours: envInt("SESSION_ABSOLUTE_HOURS", 24*30),
		OIDCSessionHours:     envInt("OIDC_SESSION_HOURS", 12),
		AnalysisMinutes:      max(1, envInt("ANALYSIS_MINUTES", 5)),
	}

	// Secrets in order of precedence: Docker secret file, NAME_FD (a file
	// descriptor the entrypoint opened as root, e.g. 3), NAME_FILE, NAME.
	// None of them stays in the environment once read.
	for name, dst := range map[string]*string{
		"master_key":         &s.MasterKey,
		"smtp_password":      &s.SMTPPassword,
		"oidc_client_secret": &s.OIDCClientSecret,
		"anthropic_api_key":  &s.AnthropicAPIKey,
	} {
		env := strings.ToUpper(name)
		if v := readFile(os.Getenv(env + fileSuffix)); v != "" {
			*dst = v
		}
		if v := readFD(os.Getenv(env + fdSuffix)); v != "" {
			*dst = v
		}
		if v := readSecret(name); v != "" {
			*dst = v
		}
		_ = os.Unsetenv(env)
		_ = os.Unsetenv(env + fileSuffix)
		_ = os.Unsetenv(env + fdSuffix)
	}

	s.env = envSet(s)
	return s
}
