package sources

// File backup tools, each a BackupSource for the backup tile and rules:
//
//	Kopia      GET api/v1/sources (basic auth of the server UI)
//	           → [{source{host,userName,path}, lastSnapshot{startTime,endTime,incomplete,stats{errorCount},rootEntry{summ{numFailed}}}}]
//	Duplicati  services.DuplicatiApi: [{Backup{ID, Name, Metadata{LastBackupDate, LastErrorDate, LastErrorMessage}}}]
//	Backrest   POST v1.Backrest/GetSummaryDashboard {} (basic auth)
//	           → {planSummaries: [{id, recentBackups{timestampMs[], status[]}}]}
//	UrBackup   services.UrBackupApi: {status: [{name, online, lastbackup, lastbackup_image, file_ok, image_ok}]}

import (
	"context"
	"strings"
	"time"

	"andon/internal/drivers/services"
	"andon/internal/enums"
	"andon/internal/sources/demoworld"
)

// ToolDataset is one file backup tool's jobs; the tool tells which.
type ToolDataset struct {
	URL  string
	Tool enums.ServiceType
	Jobs []BackupJob
}

func (d *ToolDataset) BackupTool() string      { return string(d.Tool) }
func (d *ToolDataset) BackupJobs() []BackupJob { return d.Jobs }

var (
	KopiaData     = source{key: "kopia.data", ttl: opsTTL, service: enums.ServiceKopia, fetch: fetchKopia}
	DuplicatiData = source{key: "duplicati.data", ttl: opsTTL, service: enums.ServiceDuplicati, fetch: fetchDuplicati}
	BackrestData  = source{key: "backrest.data", ttl: opsTTL, service: enums.ServiceBackrest, fetch: fetchBackrest}
	UrBackupData  = source{key: "urbackup.data", ttl: opsTTL, service: enums.ServiceUrBackup, fetch: fetchUrBackup}
)

// basicOrNone logs in with "user:pass" when given.
func basicOrNone(sctx Ctx) services.KeyedApi {
	if sctx.Secret == "" {
		return services.KeyedApi{URL: sctx.URL, Headers: map[string]string{"Accept": "application/json"}, Verify: sctx.VerifyTLS}
	}
	return services.BasicApi(sctx.URL, sctx.Secret, sctx.TLS())
}

// ── Kopia ──

func fetchKopia(ctx context.Context, sctx Ctx) (any, error) {
	if isDemo(sctx) {
		return DemoKopia(time.Now().UTC()), nil
	}
	raw, err := basicOrNone(sctx).Get(ctx, "api/v1/sources", nil)
	if err != nil {
		return nil, fetchError(err)
	}
	data := &ToolDataset{URL: sctx.URL, Tool: enums.ServiceKopia}
	for _, item := range asList(asMap(raw)["sources"]) {
		s := asMap(item)
		src := asMap(s["source"])
		name := asStr(src["host"]) + ":" + asStr(src["path"])
		last := asMap(s["lastSnapshot"])
		job := BackupJob{Item: name, Last: parseTime(last["endTime"])}
		failed := asFloat(asMap(last["stats"])["errorCount"]) + asFloat(asMap(asMap(last["rootEntry"])["summ"])["numFailed"])
		job.Note = asStr(last["incomplete"])
		job.Failed = job.Note != "" || failed > 0
		data.Jobs = append(data.Jobs, job)
	}
	return data, nil
}

// ── Duplicati ──

// duplicatiTime is how Duplicati writes its dates: 20251007T020000Z.
const duplicatiTime = "20060102T150405Z"

func fetchDuplicati(ctx context.Context, sctx Ctx) (any, error) {
	if isDemo(sctx) {
		return DemoDuplicati(time.Now().UTC()), nil
	}
	secret, err := needSecret(sctx)
	if err != nil {
		return nil, err
	}
	raw, err := services.DuplicatiApi{URL: sctx.URL, Password: secret, Mode: sctx.TLS()}.Backups(ctx)
	if err != nil {
		return nil, fetchError(err)
	}
	data := &ToolDataset{URL: sctx.URL, Tool: enums.ServiceDuplicati}
	for _, item := range asList(raw) {
		b := asMap(asMap(item)["Backup"])
		meta := asMap(b["Metadata"])
		name := asStr(b["Name"])
		last, _ := time.Parse(duplicatiTime, asStr(meta["LastBackupDate"]))
		lastErr, _ := time.Parse(duplicatiTime, asStr(meta["LastErrorDate"]))
		job := BackupJob{Item: name, Last: last, Failed: lastErr.After(last)}
		if job.Failed {
			job.Note = asStr(meta["LastErrorMessage"])
		}
		data.Jobs = append(data.Jobs, job)
	}
	return data, nil
}

// ── Backrest ──

const backrestOK, backrestError = "STATUS_SUCCESS", "STATUS_ERROR"

func fetchBackrest(ctx context.Context, sctx Ctx) (any, error) {
	if isDemo(sctx) {
		return DemoBackrest(time.Now().UTC()), nil
	}
	raw, err := basicOrNone(sctx).Post(ctx, "v1.Backrest/GetSummaryDashboard", map[string]any{})
	if err != nil {
		return nil, fetchError(err)
	}
	data := &ToolDataset{URL: sctx.URL, Tool: enums.ServiceBackrest}
	for _, item := range asList(asMap(raw)["planSummaries"]) {
		p := asMap(item)
		recent := asMap(p["recentBackups"])
		data.addPlan(asStr(p["id"]), asList(recent["timestampMs"]), asList(recent["status"]))
	}
	return data, nil
}

// addPlan takes a plan's recent runs: the newest success is its last
// backup, the newest run's status says whether it failed.
func (d *ToolDataset) addPlan(id string, stamps, states []any) {
	job := BackupJob{Item: id}
	var newest time.Time
	for i, s := range stamps {
		at := time.UnixMilli(int64(asFloat(s))).UTC()
		state := ""
		if i < len(states) {
			state = asStr(states[i])
		}
		if state == backrestOK && at.After(job.Last) {
			job.Last = at
		}
		if at.After(newest) {
			newest, job.Failed = at, state == backrestError
		}
	}
	d.Jobs = append(d.Jobs, job)
}

// ── UrBackup ──

func fetchUrBackup(ctx context.Context, sctx Ctx) (any, error) {
	if isDemo(sctx) {
		return DemoUrBackup(time.Now().UTC()), nil
	}
	user, pass, _ := strings.Cut(sctx.Secret, ":")
	raw, err := services.UrBackupApi{URL: sctx.URL, User: user, Password: pass, Mode: sctx.TLS()}.Status(ctx)
	if err != nil {
		return nil, fetchError(err)
	}
	data := &ToolDataset{URL: sctx.URL, Tool: enums.ServiceUrBackup}
	for _, item := range asList(asMap(raw)["status"]) {
		data.addClient(asMap(item))
	}
	return data, nil
}

// addClient takes a client: its newer backup of files and image; failed
// when UrBackup marks both as not ok.
func (d *ToolDataset) addClient(c map[string]any) {
	name := asStr(c["name"])
	last := unixTime(c["lastbackup"])
	if image := unixTime(c["lastbackup_image"]); image.After(last) {
		last = image
	}
	fileOK, _ := c["file_ok"].(bool)
	imageOK, _ := c["image_ok"].(bool)
	d.Jobs = append(d.Jobs, BackupJob{Item: name, Last: last, Failed: !fileOK && !imageOK})
}

// ── Demo ──

func DemoKopia(now time.Time) *ToolDataset {
	var p struct {
		URL     string
		Sources []struct {
			Host, Path string
			End        time.Time
			Failed     int
		}
	}
	demoworld.MustDecode("file_backups.kopia", now, &p)
	data := &ToolDataset{URL: p.URL, Tool: enums.ServiceKopia}
	for _, s := range p.Sources {
		data.Jobs = append(data.Jobs, BackupJob{Item: s.Host + ":" + s.Path, Last: s.End, Failed: s.Failed > 0})
	}
	return data
}

func DemoDuplicati(now time.Time) *ToolDataset {
	var p struct {
		URL     string
		Backups []struct {
			Name, Message   string
			Last, LastError time.Time
		}
	}
	demoworld.MustDecode("file_backups.duplicati", now, &p)
	data := &ToolDataset{URL: p.URL, Tool: enums.ServiceDuplicati}
	for _, b := range p.Backups {
		job := BackupJob{Item: b.Name, Last: b.Last, Failed: b.LastError.After(b.Last)}
		if job.Failed {
			job.Note = b.Message
		}
		data.Jobs = append(data.Jobs, job)
	}
	return data
}

func DemoBackrest(now time.Time) *ToolDataset {
	var p struct {
		URL   string
		Plans []struct {
			ID   string
			Runs []struct {
				At     time.Time
				Status string
			}
		}
	}
	demoworld.MustDecode("file_backups.backrest", now, &p)
	data := &ToolDataset{URL: p.URL, Tool: enums.ServiceBackrest}
	for _, plan := range p.Plans {
		var stamps, states []any
		for _, r := range plan.Runs {
			stamps, states = append(stamps, float64(r.At.UnixMilli())), append(states, r.Status)
		}
		data.addPlan(plan.ID, stamps, states)
	}
	return data
}

func DemoUrBackup(now time.Time) *ToolDataset {
	var p struct {
		URL     string
		Clients []struct {
			Name                        string
			Online, FileOK, ImageOK     bool
			Lastbackup, LastbackupImage time.Time
		}
	}
	demoworld.MustDecode("file_backups.urbackup", now, &p)
	data := &ToolDataset{URL: p.URL, Tool: enums.ServiceUrBackup}
	for _, c := range p.Clients {
		data.addClient(map[string]any{"name": c.Name, "online": c.Online, "file_ok": c.FileOK, "image_ok": c.ImageOK,
			"lastbackup": float64(c.Lastbackup.Unix()), "lastbackup_image": float64(c.LastbackupImage.Unix())})
	}
	return data
}

func init() {
	for _, s := range []source{KopiaData, DuplicatiData, BackrestData, UrBackupData} {
		Register(s)
		registerBackup(s.service)
		Register(testOf{s, func(d any) map[string]any { return map[string]any{"jobs": len(d.(*ToolDataset).Jobs)} }})
	}
}
