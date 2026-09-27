// Package system owns start-up state and instance-wide settings:
//
//	Start()
//	  ├─ instance space
//	  └─ egress guard from the stored network policy
//
// plus the admin's generic settings (iframe origins, security, registration,
// shared location data).
package system

import (
	"database/sql"
	"errors"
	"net/url"
	"strings"
	"sync/atomic"

	"andon/internal/db"
	"andon/internal/enums"
	"andon/internal/model"
	"andon/internal/repos/content"
	"andon/internal/repos/misc"
	"andon/internal/services/access"
	"andon/internal/services/audit"
	"andon/internal/sources"
)

// Setting keys in the settings table.
const (
	NetworkKey      = "network"
	IframeKey       = "iframe"
	SecurityKey     = "security"
	RegistrationKey = "registration"
	LocationKey     = "dawarich_shared"
	instanceName    = "Instanz"
)

// NetworkPolicy and its modes, re-exported so the web layer never imports sources.
type (
	NetworkPolicy = sources.NetworkPolicy
	NetMode       = sources.NetMode
)

const (
	NetOpen      = sources.NetOpen
	NetAllowlist = sources.NetAllowlist
)

// ErrDenied means the caller is not an admin.
var ErrDenied = errors.New("error.denied")

// Start prepares instance-wide state once at boot.
func Start(d *sql.DB) error {
	iframeCache.Store(nil)
	if err := ensureInstanceSpace(d); err != nil {
		return err
	}
	policy, err := Network(d)
	if err != nil {
		return err
	}
	return sources.ApplyNetwork(policy)
}

func ensureInstanceSpace(d *sql.DB) error {
	return db.WithTx(d, func(tx *sql.Tx) error {
		existing, err := content.InstanceSpace(tx)
		if err != nil || existing != nil {
			return err
		}
		return content.AddSpace(tx, &model.Space{Kind: enums.SpaceInstance, Name: instanceName, Version: 1})
	})
}

// ── Network policy ──

// Network reads the stored outbound network policy (default: open).
func Network(q db.Queryer) (sources.NetworkPolicy, error) {
	raw, err := misc.Setting(q, NetworkKey)
	if err != nil {
		return sources.NetworkPolicy{}, err
	}
	policy := sources.NetworkPolicy{Mode: sources.NetOpen, Public: true}
	if mode, ok := raw["mode"].(string); ok && mode != "" {
		policy.Mode = sources.NetMode(mode)
	}
	if public, ok := raw["public"].(bool); ok {
		policy.Public = public
	}
	policy.Networks = stringList(raw["networks"])
	policy.Hosts = stringList(raw["hosts"])
	return policy, nil
}

// SetNetwork validates, stores and applies a new network policy.
func SetNetwork(d *sql.DB, who *access.Principal, policy sources.NetworkPolicy, ip string) error {
	if !who.IsAdmin() {
		return ErrDenied
	}
	if _, err := sources.ParseNetworks(policy.Networks); err != nil {
		return err
	}
	err := db.WithTx(d, func(tx *sql.Tx) error {
		value := map[string]any{
			"mode": string(policy.Mode), "networks": policy.Networks, "hosts": policy.Hosts, "public": policy.Public,
		}
		if err := misc.SetSetting(tx, NetworkKey, value); err != nil {
			return err
		}
		return audit.Log(tx, &who.UserID, "settings.network", string(policy.Mode), ip, nil)
	})
	if err != nil {
		return err
	}
	return sources.ApplyNetwork(policy)
}

// ── Generic settings ──

// Put stores one settings object. Admin only.
func Put(d *sql.DB, who *access.Principal, key string, value map[string]any, ip string) error {
	if !who.IsAdmin() {
		return ErrDenied
	}
	err := db.WithTx(d, func(tx *sql.Tx) error {
		if err := misc.SetSetting(tx, key, value); err != nil {
			return err
		}
		return audit.Log(tx, &who.UserID, "settings."+key, "", ip, nil)
	})
	if key == IframeKey {
		iframeCache.Store(nil)
	}
	return err
}

// Flag reads a boolean field of a settings object.
func Flag(q db.Queryer, key, field string) bool {
	raw, err := misc.Setting(q, key)
	if err != nil {
		return false
	}
	on, _ := raw[field].(bool)
	return on
}

// iframeCache holds IframeOrigins: every response's CSP needs it, it
// changes only when an admin saves the settings.
var iframeCache atomic.Pointer[[]string]

// IframeOrigins lists origins embedded pages may be loaded from (CSP frame-src).
func IframeOrigins(q db.Queryer) []string {
	if cached := iframeCache.Load(); cached != nil {
		return *cached
	}
	raw, err := misc.Setting(q, IframeKey)
	if err != nil {
		return nil
	}
	origins := stringList(raw["origins"])
	iframeCache.Store(&origins)
	return origins
}

// IsOrigin accepts a bare http(s) origin ("https://grafana.lan:3000"),
// nothing that could extend the CSP it lands in ("https://x; script-src *").
func IsOrigin(s string) bool {
	u, err := url.Parse(s)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
		return false
	}
	return s == u.Scheme+"://"+u.Host && !strings.ContainsAny(u.Host, " ;,'\"")
}

// stringList converts a JSON list (decoded as []any) to []string.
func stringList(v any) []string {
	list, _ := v.([]any)
	out := make([]string, 0, len(list))
	for _, item := range list {
		if s, ok := item.(string); ok && strings.TrimSpace(s) != "" {
			out = append(out, s)
		}
	}
	return out
}
