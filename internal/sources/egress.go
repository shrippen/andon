package sources

import (
	"net"
	"strings"
	"time"

	"andon/internal/drivers/httpclient"
)

// NetMode is the instance's outbound network mode.
type NetMode string

const (
	NetOpen      NetMode = "open"
	NetAllowlist NetMode = "allowlist"
)

// NetworkPolicy restricts which hosts sources may reach:
//
//	open        everything but local targets (loopback, link-local such as
//	            169.254.169.254, unspecified, multicast) unless listed
//	allowlist   listed hosts, listed networks, public addresses (if Public)
type NetworkPolicy struct {
	Mode     NetMode
	Networks []string // CIDR, e.g. 192.168.10.0/24
	Hosts    []string
	Public   bool
}

// ParseNetworks validates CIDR entries ("10.0.0.0/8").
func ParseNetworks(cidrs []string) ([]*net.IPNet, error) {
	out := make([]*net.IPNet, 0, len(cidrs))
	for _, c := range cidrs {
		_, n, err := net.ParseCIDR(strings.TrimSpace(c))
		if err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	return out, nil
}

// ApplyNetwork installs policy as the process-wide egress guard.
func ApplyNetwork(policy NetworkPolicy) error {
	nets, err := ParseNetworks(policy.Networks)
	if err != nil {
		return err
	}
	hosts := make(map[string]bool, len(policy.Hosts))
	for _, h := range policy.Hosts {
		hosts[strings.ToLower(strings.TrimSpace(h))] = true
	}

	allowed := allowedAddr
	if policy.Mode != NetAllowlist {
		allowed = notLocal
	}
	httpclient.SetGuard(func(host string, addrs []net.IP) bool {
		if hosts[strings.ToLower(host)] {
			return true
		}
		for _, addr := range addrs {
			if !allowed(addr, nets, policy) {
				return false
			}
		}
		return true
	})
	return nil
}

// notLocal is the open mode: any address except local targets, which
// only a listed network unlocks (e.g. 127.0.0.0/8 for a sidecar).
func notLocal(addr net.IP, nets []*net.IPNet, _ NetworkPolicy) bool {
	for _, n := range nets {
		if n.Contains(addr) {
			return true
		}
	}
	return !isLocal(addr)
}

// isLocal: Andon itself or the host's metadata service, never a service
// an invited user should reach.
func isLocal(ip net.IP) bool {
	return ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() ||
		ip.IsUnspecified() || ip.IsMulticast()
}

func allowedAddr(addr net.IP, nets []*net.IPNet, policy NetworkPolicy) bool {
	if policy.Public && isGlobal(addr) {
		return true
	}
	for _, n := range nets {
		if n.Contains(addr) {
			return true
		}
	}
	return false
}

// isGlobal: routable on the internet, not private, loopback or link-local.
func isGlobal(ip net.IP) bool {
	return ip.IsGlobalUnicast() && !ip.IsPrivate() && !ip.IsLoopback() && !ip.IsLinkLocalUnicast()
}

// ClockSkew is how far a service host's clock was off at its last answer
// (negative: behind); ok=false before the first answer with a Date header.
func ClockSkew(host string) (time.Duration, bool) {
	return httpclient.ClockSkew(host)
}
