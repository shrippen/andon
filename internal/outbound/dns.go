package outbound

// Pausing a DNS filter for a while: a site that breaks behind the blocker
// works again, the filter comes back on its own.

import (
	"context"
	"time"

	"andon/internal/drivers/services"
)

// DNSKind names the filter behind a target.
type DNSKind string

const (
	DNSPihole  DNSKind = "pihole"
	DNSAdGuard DNSKind = "adguard"
)

// DNSPause switches blocking off for d; the filter turns it on again.
func DNSPause(ctx context.Context, to Target, kind DNSKind, d time.Duration) error {
	if kind == DNSAdGuard {
		return services.AdGuardApi{URL: to.URL, Secret: to.Token, Verify: to.VerifyTLS}.Post(ctx, "protection",
			map[string]any{"enabled": false, "duration": d.Milliseconds()})
	}
	session, err := services.PiholeApi{URL: to.URL, Password: to.Token, Verify: to.VerifyTLS}.Open(ctx)
	if err != nil {
		return err
	}
	defer session.Close(context.WithoutCancel(ctx))
	if session.Legacy() {
		return session.DisableV5(ctx, int(d.Seconds()))
	}
	return session.Post(ctx, "dns/blocking", map[string]any{"blocking": false, "timer": int(d.Seconds())})
}
