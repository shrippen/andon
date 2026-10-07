package sources

// Datasets of services the cross checks read space-wide (several Docker
// hosts, Borg servers …) merge into one: "the space's containers" are
// every host's. Merge returns a new dataset; cached ones stay untouched.

import (
	"maps"
	"slices"
	"time"
)

// Merger is a dataset that can take another of its kind.
type Merger interface {
	// Merge returns the union of the receiver and o (same type), or the
	// receiver unchanged for another type.
	Merge(o any) any
}

func (d *DockerDataset) Merge(o any) any {
	x, ok := o.(*DockerDataset)
	if !ok {
		return d
	}
	return &DockerDataset{URL: d.URL, Containers: slices.Concat(d.Containers, x.Containers)}
}

func (d *BorgDataset) Merge(o any) any {
	x, ok := o.(*BorgDataset)
	if !ok {
		return d
	}
	out := *d
	out.Clients = slices.Concat(d.Clients, x.Clients)
	out.Failed24h += x.Failed24h
	out.Completed24h += x.Completed24h
	out.Running += x.Running
	out.UsedBytes += x.UsedBytes
	out.TotalBytes += x.TotalBytes
	out.AgentsOutdated += x.AgentsOutdated
	out.ServerUpdate = d.ServerUpdate || x.ServerUpdate
	if x.LastBackup.After(d.LastBackup) {
		out.LastBackup = x.LastBackup
	}
	return &out
}

func (d *ProxmoxDataset) Merge(o any) any {
	x, ok := o.(*ProxmoxDataset)
	if !ok {
		return d
	}
	backups := maps.Clone(d.Backups)
	if backups == nil && x.Backups != nil {
		backups = map[int64]time.Time{}
	}
	maps.Copy(backups, x.Backups)
	return &ProxmoxDataset{URL: d.URL, Nodes: slices.Concat(d.Nodes, x.Nodes), Guests: slices.Concat(d.Guests, x.Guests), Backups: backups}
}

func (d *TrueNASDataset) Merge(o any) any {
	x, ok := o.(*TrueNASDataset)
	if !ok {
		return d
	}
	out := *d
	out.Pools = slices.Concat(d.Pools, x.Pools)
	out.Alerts = slices.Concat(d.Alerts, x.Alerts)
	out.Apps = slices.Concat(d.Apps, x.Apps)
	out.Snapshots = slices.Concat(d.Snapshots, x.Snapshots)
	return &out
}

func (d *PGBackDataset) Merge(o any) any {
	x, ok := o.(*PGBackDataset)
	if !ok {
		return d
	}
	out := &PGBackDataset{URL: d.URL, Backups: slices.Concat(d.Backups, x.Backups), Unhealthy: slices.Concat(d.Unhealthy, x.Unhealthy), LastEvent: d.LastEvent}
	if x.LastEvent.After(out.LastEvent) {
		out.LastEvent = x.LastEvent
	}
	return out
}

func (d *KumaDataset) Merge(o any) any {
	x, ok := o.(*KumaDataset)
	if !ok {
		return d
	}
	return &KumaDataset{URL: d.URL, Monitors: slices.Concat(d.Monitors, x.Monitors)}
}

func (d *KomodoDataset) Merge(o any) any {
	x, ok := o.(*KomodoDataset)
	if !ok {
		return d
	}
	out := *d
	out.ServersTotal += x.ServersTotal
	out.ServersHealthy += x.ServersHealthy
	out.ServersProblem += x.ServersProblem
	out.Stacks = slices.Concat(d.Stacks, x.Stacks)
	out.Alerts = slices.Concat(d.Alerts, x.Alerts)
	out.Deployed = slices.Concat(d.Deployed, x.Deployed)
	return &out
}

func (d *CertDataset) Merge(o any) any {
	x, ok := o.(*CertDataset)
	if !ok {
		return d
	}
	return &CertDataset{Certs: slices.Concat(d.Certs, x.Certs)}
}
