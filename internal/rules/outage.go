package rules

// Outages: several signals on one host become one hint.
//
//	connection nas.lan failed ─┐
//	kuma monitor "NAS" down    ├─ host nas.lan ─► system.outage (critical)
//	kuma monitor "SMB" down   ─┘                  single hints suppressed
//
// A host that runs as a Proxmox guest belongs to its guest, and a guest to
// its node: when the node is offline (or the guest stopped), the signals
// of all hosts on it gather under the node (or guest) as the one cause.
//
//	node pve1 offline ─► guest nas ─► nas.lan signals ─► outage "pve1"

import (
	"net/url"
	"sort"
	"strings"

	"andon/internal/enums"
	"andon/internal/sources"
)

// FailedDataset is the Env.Datasets key of connections that failed to load.
const FailedDataset = "failed"

// Failed is one connection whose fetch failed.
type Failed struct {
	Service, Name, Host string
}

const (
	outageRule    = "system.outage"
	minOutageHits = 2
	kumaDownRule  = "kuma.monitor_down"
)

// HostOf returns the lower-case host of a URL or bare host name.
func HostOf(raw string) string {
	raw = strings.TrimSpace(raw)
	if !strings.Contains(raw, "://") {
		raw = "//" + raw
	}
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	return strings.ToLower(u.Hostname())
}

// Outages maps each cause (a host, or a Proxmox node or guest that is
// down) with at least two failure signals to the names of what failed.
func Outages(env Env) map[string][]string {
	roots := downRoots(env)
	signals := map[string][]string{}
	add := func(host, name string) {
		if host == "" {
			return
		}
		key := rootOf(roots, host)
		signals[key] = append(signals[key], name)
	}
	if failed, ok := env.Datasets[FailedDataset].([]Failed); ok {
		for _, f := range failed {
			add(f.Host, f.Name)
		}
	}
	if kuma, ok := env.Datasets[string(enums.ServiceUptimeKuma)].(*sources.KumaDataset); ok {
		for _, m := range kuma.Monitors {
			if m.Status == sources.KumaDown {
				add(HostOf(m.Target), m.Name)
			}
		}
	}
	// A down node or guest is a signal itself, once, when something on it failed.
	for key, names := range signals {
		if cause, ok := roots.causes[key]; ok {
			signals[key] = append(names, cause)
		}
	}
	out := map[string][]string{}
	for key, names := range signals {
		if len(names) >= minOutageHits {
			out[key] = names
		}
	}
	return out
}

// OutageRoot is the key a host's failures gather under in Outages: the
// Proxmox node or guest it runs on when that is down, else the host.
func OutageRoot(env Env, host string) string {
	return rootOf(downRoots(env), host)
}

// roots maps a host label ("nas" of nas.lan) to the down node or guest it
// depends on, and each such root to its own signal ("pve1 offline").
type roots struct {
	byLabel map[string]string
	causes  map[string]string
}

func downRoots(env Env) roots {
	r := roots{byLabel: map[string]string{}, causes: map[string]string{}}
	pve, ok := env.Datasets[string(enums.ServiceProxmox)].(*sources.ProxmoxDataset)
	if !ok {
		return r
	}
	offline := map[string]bool{}
	for _, n := range pve.Nodes {
		if !n.Online {
			offline[n.Name] = true
		}
	}
	for _, g := range pve.Guests {
		label := strings.ToLower(g.Name)
		switch {
		case offline[g.Node]:
			r.byLabel[label] = g.Node
			r.causes[g.Node] = "Proxmox " + g.Node
		case !g.Running && !g.Template:
			r.byLabel[label] = label
			r.causes[label] = "Proxmox " + g.Name
		}
	}
	return r
}

func rootOf(r roots, host string) string {
	label, _, _ := strings.Cut(host, ".")
	if root, ok := r.byLabel[label]; ok {
		return root
	}
	return host
}

// Suppressed reports whether a finding is covered by an outage hint.
func Suppressed(f Finding, env Env, outages map[string][]string) bool {
	if f.Rule != kumaDownRule || len(outages) == 0 {
		return false
	}
	kuma, ok := env.Datasets[string(enums.ServiceUptimeKuma)].(*sources.KumaDataset)
	if !ok {
		return false
	}
	for _, m := range kuma.Monitors {
		if f.Params["monitor"] == m.Name {
			_, down := outages[OutageRoot(env, HostOf(m.Target))]
			return down
		}
	}
	return false
}

func init() {
	Register(outageRule, Cross, nil, func(_ any, cfg map[string]any, env Env) []Finding {
		outages := Outages(env)
		hosts := make([]string, 0, len(outages))
		for h := range outages {
			hosts = append(hosts, h)
		}
		sort.Strings(hosts)

		var found []Finding
		for _, host := range hosts {
			found = append(found, Finding{Fingerprint: "outage:" + host, Rule: outageRule, Severity: enums.SeverityCritical,
				Message: "system.outage", Params: map[string]any{"host": host, "count": len(outages[host]), "names": shortList(outages[host])},
				Sources: []string{"system"}})
		}
		return found
	})
}
