// Package hosts is the host page: what runs on one machine name, as the
// connections, Uptime Kuma monitors and certificate checks of the
// caller's spaces see it.
//
//	nas.lan ─┬─ TrueNAS connection   ok · 25.04.1
//	         ├─ Immich connection    failed · v1.131.0
//	         ├─ monitor "NAS"        down
//	         ├─ certificate          ends 06.10.2026
//	         ├─ Prometheus alert     HostHighCpuLoad (instance nas:9100)
//	         ├─ heartbeat            borg-nas (a check tagged with the host's first label: "nas")
//	         ├─ CVE                  CVE-2026-41207 hits gitea/gitea:1.24 (Docker or compose stack on it)
//	         └─ hints naming the host (host, node, guest, device param, by
//	            full name or first label), else hints of its connections
package hosts

import (
	"context"
	"database/sql"
	"errors"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"andon/internal/db"
	"andon/internal/enums"
	"andon/internal/metrics"
	"andon/internal/model"
	"andon/internal/repos/content"
	"andon/internal/rules"
	"andon/internal/services/access"
	"andon/internal/services/hints"
	"andon/internal/services/svcdata"
	"andon/internal/sources"
)

// ErrNotFound means no connection of the caller runs on that host.
var ErrNotFound = errors.New("hosts: not found")

// Service is one connection on a host.
type Service struct {
	ConnID    int64
	Name      string
	Service   enums.ServiceType
	OK        bool
	Error     string
	Version   string
	FetchedAt time.Time
}

// Monitor is an Uptime Kuma monitor aimed at the host.
type Monitor struct {
	Name string
	Up   bool
	MS   float64
}

// Host is one machine name with everything that points at it.
type Host struct {
	Name     string
	Services []Service
	Monitors []Monitor
	Certs    []sources.Cert
	Alerts   []sources.PromAlert // firing
	Beats    []sources.Heartbeat
	CVEs     []metrics.CVEMatch
	Hints    []hints.View
	Problems int // failed services, monitors down, failing certificates, open hints
}

// Found says whether the host has anything to look at: a monitor, a hint
// or a problem. The others fold away on the host list.
func (h Host) Found() bool {
	return len(h.Monitors) > 0 || len(h.Hints) > 0 || h.Problems > 0
}

// Split parts a host list into hosts with findings and quiet ones,
// keeping the order.
func Split(list []Host) (found, quiet []Host) {
	for _, h := range list {
		if h.Found() {
			found = append(found, h)
			continue
		}
		quiet = append(quiet, h)
	}
	return found, quiet
}

// List returns every host of the caller's connections, troubled first.
func List(ctx context.Context, d *sql.DB, who *access.Principal) ([]Host, error) {
	byName, err := collect(ctx, d, who)
	if err != nil {
		return nil, err
	}
	out := make([]Host, 0, len(byName))
	for _, h := range byName {
		out = append(out, *h)
	}
	sort.Slice(out, func(a, b int) bool {
		if out[a].Problems != out[b].Problems {
			return out[a].Problems > out[b].Problems
		}
		return out[a].Name < out[b].Name
	})
	return out, nil
}

// One returns a host with its open hints.
func One(ctx context.Context, d *sql.DB, who *access.Principal, name string) (Host, error) {
	byName, err := collect(ctx, d, who)
	if err != nil {
		return Host{}, err
	}
	h, ok := byName[strings.ToLower(name)]
	if !ok {
		return Host{}, ErrNotFound
	}
	return *h, nil
}

// collect groups the stored results of every visible connection by host,
// then adds the monitors and certificate checks aimed at those hosts.
func collect(ctx context.Context, d *sql.DB, who *access.Principal) (map[string]*Host, error) {
	var conns []*model.Connection
	err := db.WithRead(d, func(tx *sql.Tx) error {
		ids := make([]int64, 0, len(who.Spaces))
		for id := range who.Spaces {
			ids = append(ids, id)
		}
		var err error
		conns, err = content.Connections(tx, ids)
		return err
	})
	if err != nil {
		return nil, err
	}

	uid := who.UserID
	out := map[string]*Host{}
	var kuma []*sources.KumaDataset
	var certs []*sources.CertDataset
	var proms []*sources.PrometheusDataset
	var beats []*sources.HealthchecksDataset
	var nvd *sources.NVDDataset
	running := map[string]any{} // Docker and Gitea datasets, for the images in use
	for _, c := range conns {
		res, err := svcdata.Get(ctx, d, sources.DataKey(enums.ServiceType(c.Service)), nil, c, model.UserHolder(uid), svcdata.Stored)
		if err != nil || res.Pending {
			continue
		}
		switch data := res.Data.(type) {
		case *sources.KumaDataset:
			kuma = append(kuma, data)
		case *sources.CertDataset:
			certs = append(certs, data)
		case *sources.PrometheusDataset:
			proms = append(proms, data)
		case *sources.HealthchecksDataset:
			beats = append(beats, data)
		case *sources.NVDDataset:
			nvd = data
		case *sources.DockerDataset, *sources.GiteaDataset:
			running[strconv.FormatInt(c.ID, 10)] = data
		}
		name := rules.HostOf(c.URL)
		if name == "" {
			continue
		}
		h := hostOf(out, name)
		svc := Service{ConnID: c.ID, Name: c.Name, Service: enums.ServiceType(c.Service), OK: res.Ok(), Error: res.Error, FetchedAt: res.FetchedAt}
		svc.Version = versionOf(c.Service, res.Data)
		if !svc.OK {
			h.Problems++
		}
		h.Services = append(h.Services, svc)
	}

	for _, data := range kuma {
		for _, m := range data.Monitors {
			h, ok := out[rules.HostOf(m.Target)]
			if !ok {
				continue
			}
			up := m.Status != sources.KumaDown
			if !up {
				h.Problems++
			}
			h.Monitors = append(h.Monitors, Monitor{Name: m.Name, Up: up, MS: m.MS})
		}
	}
	for _, data := range certs {
		for _, c := range data.Certs {
			h, ok := out[rules.HostOf(c.Host)]
			if !ok {
				continue
			}
			if c.Error != "" {
				h.Problems++
			}
			h.Certs = append(h.Certs, c)
		}
	}
	addAlerts(out, proms)
	addBeats(out, beats)
	if nvd != nil {
		addCVEs(out, metrics.ImageCVEs(metrics.RunningImages(running), nvd.CVEs))
	}
	open, err := hints.Active(d, who, enums.SeverityInfo, nil, 0)
	if err != nil {
		return nil, err
	}
	addHints(out, open, connHosts(conns))
	return out, nil
}

// match is how a hint found its host.
type match int

const (
	byNone  match = iota
	byConn        // the host of the hint's connection
	byName        // an item with the host's full name: "nas.lan"
	byLabel       // an item with the host's first label: guest "nas" → nas.lan
)

// counted are rules whose hints repeat a check the host already counts:
// the failed connection, and (when the item names the host in full) the
// failing certificate and the firing alert.
var counted = map[string]match{
	"system.connector_down": byConn,
	"certs.unreachable":     byName,
	"prometheus.alert":      byName,
}

// addHints puts each open hint on the host of the first item it names,
// else on its connection's host; a hint is a problem unless counted.
func addHints(out map[string]*Host, open []hints.View, conns map[int64]string) {
	labels := map[string][]*Host{}
	for name, h := range out {
		label, _, _ := strings.Cut(name, ".")
		labels[label] = append(labels[label], h)
	}

	for _, v := range open {
		h, how := hostFor(out, labels, v.Items)
		if h == nil && v.ConnectionID != nil {
			h, how = out[conns[*v.ConnectionID]], byConn
		}
		if h == nil {
			continue
		}
		h.Hints = append(h.Hints, v)
		if counted[v.Rule] != how {
			h.Problems++
		}
	}
}

// hostFor is the host the first matching item names: by full name, else
// by first label when only one host carries it.
func hostFor(out map[string]*Host, labels map[string][]*Host, items []string) (*Host, match) {
	for _, item := range items {
		name := rules.HostOf(item)
		if h, ok := out[name]; ok {
			return h, byName
		}
		label, _, _ := strings.Cut(name, ".")
		if same := labels[label]; label != "" && len(same) == 1 {
			return same[0], byLabel
		}
	}
	return nil, byNone
}

// connHosts maps each connection to the host of its URL.
func connHosts(conns []*model.Connection) map[int64]string {
	out := make(map[int64]string, len(conns))
	for _, c := range conns {
		out[c.ID] = rules.HostOf(c.URL)
	}
	return out
}

// addAlerts puts each firing alert on the host of its instance label.
func addAlerts(out map[string]*Host, proms []*sources.PrometheusDataset) {
	for _, data := range proms {
		for _, a := range data.Alerts {
			h, ok := out[rules.HostOf(a.Instance)]
			if !ok || !a.Firing() {
				continue
			}
			h.Problems++
			h.Alerts = append(h.Alerts, a)
		}
	}
}

// addBeats puts each check on the hosts whose first label is one of its
// tags: tag "nas" → nas.lan.
func addBeats(out map[string]*Host, beats []*sources.HealthchecksDataset) {
	for name, h := range out {
		label, _, _ := strings.Cut(name, ".")
		for _, data := range beats {
			for _, c := range data.Checks {
				if !slices.ContainsFunc(c.Tags, func(t string) bool { return strings.EqualFold(t, label) }) {
					continue
				}
				if c.Status == sources.HeartbeatDown {
					h.Problems++
				}
				h.Beats = append(h.Beats, c)
			}
		}
	}
}

// addCVEs puts each match on the host its image runs on (same first label:
// a stack of host "boje" → boje.lan); an affected image is a problem.
func addCVEs(out map[string]*Host, matches []metrics.CVEMatch) {
	for name, h := range out {
		label, _, _ := strings.Cut(name, ".")
		for _, m := range matches {
			imgLabel, _, _ := strings.Cut(m.Image.Host, ".")
			if imgLabel == "" || !strings.EqualFold(imgLabel, label) {
				continue
			}
			if m.State == metrics.CVEAffected {
				h.Problems++
			}
			h.CVEs = append(h.CVEs, m)
		}
	}
}

func hostOf(all map[string]*Host, name string) *Host {
	if h, ok := all[name]; ok {
		return h
	}
	h := &Host{Name: name}
	all[name] = h
	return h
}

// versionOf is the version a dataset reports, "" if none.
func versionOf(service string, data any) string {
	for _, v := range metrics.Read(metrics.Scope{Datasets: map[string]any{service: data}}, time.Now()).Versions {
		if !strings.HasPrefix(v, "pending:") {
			return v
		}
	}
	return ""
}
