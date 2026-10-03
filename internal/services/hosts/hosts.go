// Package hosts is the host page: what runs on one machine name, as the
// connections, Uptime Kuma monitors and certificate checks of the
// caller's spaces see it.
//
//	nas.lan ─┬─ TrueNAS connection   ok · 25.04.1
//	         ├─ Immich connection    failed · v1.131.0
//	         ├─ monitor "NAS"        down
//	         ├─ certificate          ends 06.10.2026
//	         └─ hints of these connections
package hosts

import (
	"context"
	"database/sql"
	"errors"
	"sort"
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
	Hints    []hints.View
	Problems int // failed services, monitors down, failing certificates
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

// One returns a host with the open hints of its connections.
func One(ctx context.Context, d *sql.DB, who *access.Principal, name string) (Host, error) {
	byName, err := collect(ctx, d, who)
	if err != nil {
		return Host{}, err
	}
	h, ok := byName[strings.ToLower(name)]
	if !ok {
		return Host{}, ErrNotFound
	}
	open, err := hints.Active(d, who, enums.SeverityInfo, nil, 0)
	if err != nil {
		return Host{}, err
	}
	conns := map[int64]bool{}
	for _, s := range h.Services {
		conns[s.ConnID] = true
	}
	for _, v := range open {
		if v.ConnectionID != nil && conns[*v.ConnectionID] {
			h.Hints = append(h.Hints, v)
		}
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
	for _, c := range conns {
		res, err := svcdata.Get(ctx, d, sources.DataKey(enums.ServiceType(c.Service)), nil, c, &uid, svcdata.Stored)
		if err != nil || res.Pending {
			continue
		}
		switch data := res.Data.(type) {
		case *sources.KumaDataset:
			kuma = append(kuma, data)
		case *sources.CertDataset:
			certs = append(certs, data)
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
	return out, nil
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
