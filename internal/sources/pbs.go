package sources

// Proxmox Backup Server: datastores with their fill, the backup groups
// (one per VM or container) with their newest snapshot, and the verify
// jobs. A read-only API token (Datastore.Audit) is enough; the secret is
// "user@realm!token:secret".
//
//	GET api2/json/admin/datastore                  → [{store}]
//	GET api2/json/admin/datastore/<store>/groups   → [{backup-type, backup-id, last-backup, backup-count, comment}]
//	GET api2/json/status/datastore-usage           → [{store, total, used}]
//	GET api2/json/nodes/localhost/tasks?typefilter=verificationjob → [{worker_id, status, starttime}]

import (
	"context"
	"net/url"
	"strings"
	"time"

	"andon/internal/drivers/services"
	"andon/internal/enums"
	"andon/internal/sources/demoworld"
)

const (
	pbsBase     = "api2/json/"
	pbsTaskOK   = "OK"
	pbsVerifies = "50"
)

// PBSStore is a datastore with its fill.
type PBSStore struct {
	Store       string
	Total, Used float64 // bytes
}

// PBSGroup is the backups of one VM, container or host.
type PBSGroup struct {
	Store   string
	Type    string // vm, ct, host
	ID      string // "101"
	Comment string // usually the guest's name
	Last    time.Time
	Count   int
}

// PBSVerify is one verify run.
type PBSVerify struct {
	Store  string
	Status string // "OK" or an error text
	At     time.Time
}

// OK reports whether the run found nothing wrong.
func (v PBSVerify) OK() bool { return v.Status == pbsTaskOK }

// PBSDataset is the server's stores, groups and verify runs.
type PBSDataset struct {
	URL      string
	Stores   []PBSStore
	Groups   []PBSGroup
	Verifies []PBSVerify // newest first
}

var PBSData = source{key: "pbs.data", ttl: opsTTL, service: enums.ServicePBS, fetch: fetchPBS}

func fetchPBS(ctx context.Context, sctx Ctx) (any, error) {
	if isDemo(sctx) {
		return DemoPBS(time.Now().UTC()), nil
	}
	secret, err := needSecret(sctx)
	if err != nil {
		return nil, err
	}
	api := services.HeaderApi(strings.TrimRight(sctx.URL, "/")+"/"+pbsBase, "Authorization", "PBSAPIToken="+secret, sctx.TLS())
	data := &PBSDataset{URL: sctx.URL}

	usage, err := api.Get(ctx, "status/datastore-usage", nil)
	if err != nil {
		return nil, fetchError(err)
	}
	for _, raw := range asList(asMap(usage)["data"]) {
		s := asMap(raw)
		data.Stores = append(data.Stores, PBSStore{Store: asStr(s["store"]), Total: asFloat(s["total"]), Used: asFloat(s["used"])})
	}
	for _, s := range data.Stores {
		groups, err := api.Get(ctx, "admin/datastore/"+url.PathEscape(s.Store)+"/groups", nil)
		if err != nil {
			return nil, fetchError(err)
		}
		for _, raw := range asList(asMap(groups)["data"]) {
			g := asMap(raw)
			data.Groups = append(data.Groups, PBSGroup{Store: s.Store, Type: asStr(g["backup-type"]), ID: asStr(g["backup-id"]),
				Comment: asStr(g["comment"]), Last: unixTime(g["last-backup"]), Count: int(asFloat(g["backup-count"]))})
		}
	}
	tasks, err := api.Get(ctx, "nodes/localhost/tasks", url.Values{"typefilter": {"verificationjob"}, "limit": {pbsVerifies}})
	if err != nil {
		return nil, fetchError(err)
	}
	for _, raw := range asList(asMap(tasks)["data"]) {
		t := asMap(raw)
		store, _, _ := strings.Cut(asStr(t["worker_id"]), ":") // "nebelhorn:v-…"
		data.Verifies = append(data.Verifies, PBSVerify{Store: store, Status: asStr(t["status"]), At: unixTime(t["starttime"])})
	}
	return data, nil
}

// unixTime reads epoch seconds; zero for none.
func unixTime(v any) time.Time {
	if n := asFloat(v); n > 0 {
		return time.Unix(int64(n), 0).UTC()
	}
	return time.Time{}
}

// Item is how a group shows in the backup list: its comment (the guest's
// name) or type/id.
func (g PBSGroup) Item() string {
	if g.Comment != "" {
		return g.Comment
	}
	return g.Type + "/" + g.ID
}

func (d *PBSDataset) BackupTool() string { return string(enums.ServicePBS) }

// BackupJobs are the groups; PBS keeps no failed runs (the client's
// backup fails before a snapshot exists).
func (d *PBSDataset) BackupJobs() []BackupJob {
	out := make([]BackupJob, 0, len(d.Groups))
	for _, g := range d.Groups {
		out = append(out, BackupJob{Item: g.Item(), Last: g.Last})
	}
	return out
}

// DemoPBS is Studio Weber's backup server.
func DemoPBS(now time.Time) *PBSDataset {
	var p struct {
		URL        string
		Datastores []PBSStore
		Groups     []PBSGroup
		Verify     []PBSVerify
	}
	demoworld.MustDecode("backup_server", now, &p)
	data := &PBSDataset{URL: p.URL, Stores: p.Datastores, Groups: p.Groups}
	for i := len(p.Verify) - 1; i >= 0; i-- {
		data.Verifies = append(data.Verifies, p.Verify[i])
	}
	return data
}

func init() {
	Register(PBSData)
	registerBackup(enums.ServicePBS)
	Register(testOf{PBSData, func(d any) map[string]any { return map[string]any{"groups": len(d.(*PBSDataset).Groups)} }})
}
