package sources

// Homelab sources: Scrutiny (disk health), Immich (photos), Umami
// (web analytics). Each registers "<service>.data" and "<service>.test".

import (
	"context"
	"sort"
	"strconv"
	"strings"
	"time"

	"andon/internal/drivers/services"
	"andon/internal/enums"
)

const (
	umamiWindow = 7 * 24 * time.Hour
	msPerSecond = 1000
)

// testOf turns a data source into its connection check.
type testOf struct {
	data    Source
	summary func(any) map[string]any
}

func (t testOf) Key() string                { return strings.TrimSuffix(t.data.Key(), ".data") + ".test" }
func (t testOf) TTL() time.Duration         { return testTTL }
func (t testOf) Service() enums.ServiceType { return t.data.Service() }

func (t testOf) Fetch(ctx context.Context, sctx Ctx) (any, error) {
	out, err := t.data.Fetch(ctx, sctx)
	if err != nil {
		return nil, err
	}
	result := map[string]any{"version": nil}
	for k, v := range t.summary(out) {
		result[k] = v
	}
	return result, nil
}

// ── Scrutiny ──

// Scrutiny device_status: 0 passed, 1 SMART failed, 2 Scrutiny failed, 3 both.
const ScrutinyPassed = 0

type Disk struct {
	Name, Model string
	Status      int
	Temp        float64
	Hours       int
	Seen        time.Time // last collector run
	WWN         string
	Failing     string // failed disks: flagged attributes, "Reallocated Sectors Count 8"
}

type ScrutinyDataset struct {
	URL   string
	Disks []Disk
}

var ScrutinyData = source{key: "scrutiny.data", ttl: opsTTL, service: enums.ServiceScrutiny, fetch: fetchScrutiny}

func fetchScrutiny(ctx context.Context, sctx Ctx) (any, error) {
	if isDemo(sctx) {
		return DemoScrutiny(time.Now()), nil
	}
	api := services.ScrutinyApi{URL: sctx.URL, Verify: sctx.VerifyTLS}
	body, err := api.Summary(ctx)
	if err != nil {
		return nil, fetchError(err)
	}
	data := parseScrutiny(sctx.URL, body)

	// A failed disk says why: its details name the flagged attributes.
	for i, d := range data.Disks {
		if d.Status == ScrutinyPassed || d.WWN == "" {
			continue
		}
		if details, err := api.Details(ctx, d.WWN); err == nil {
			data.Disks[i].Failing = failingAttrs(details)
		}
	}
	return data, nil
}

// failingAttrs lists the attributes of a disk's latest SMART result that
// SMART or Scrutiny flag (status ≠ 0), by attribute id, with raw values.
func failingAttrs(details any) string {
	body := asMap(details)
	results := asList(asMap(body["data"])["smart_results"])
	if len(results) == 0 {
		return ""
	}
	names := asMap(body["metadata"])
	type attr struct {
		id   int
		text string
	}
	var flagged []attr
	for key, raw := range asMap(asMap(results[0])["attrs"]) {
		a := asMap(raw)
		if asFloat(a["status"]) == 0 {
			continue
		}
		name := asStr(asMap(names[key])["display_name"])
		if name == "" {
			name = "SMART " + key
		}
		flagged = append(flagged, attr{int(asFloat(a["attribute_id"])), name + " " + strconv.FormatFloat(asFloat(a["raw_value"]), 'f', -1, 64)})
	}
	sort.Slice(flagged, func(i, j int) bool { return flagged[i].id < flagged[j].id })
	texts := make([]string, len(flagged))
	for i, a := range flagged {
		texts[i] = a.text
	}
	return strings.Join(texts, ", ")
}

func parseScrutiny(base string, body any) *ScrutinyDataset {
	data := &ScrutinyDataset{URL: base}
	for _, raw := range asMap(asMap(asMap(body)["data"])["summary"]) {
		entry := asMap(raw)
		dev, smart := asMap(entry["device"]), asMap(entry["smart"])
		seen, _ := time.Parse(time.RFC3339, asStr(smart["collector_date"]))
		data.Disks = append(data.Disks, Disk{
			Name: asStr(dev["device_name"]), Model: asStr(dev["model_name"]),
			Status: int(asFloat(dev["device_status"])), Temp: asFloat(smart["temp"]),
			Hours: int(asFloat(smart["power_on_hours"])), Seen: seen.UTC(), WWN: asStr(dev["wwn"]),
		})
	}
	sort.Slice(data.Disks, func(i, j int) bool { return data.Disks[i].Name < data.Disks[j].Name })
	return data
}

// ── Immich ──

type ImmichDataset struct {
	URL           string
	Photos        int
	Videos        int
	DiskPercent   float64
	DiskAvailable string
	FailedJobs    map[string]int // queue → failed count; nil if unreadable
	Version       string         // "v1.132.3"
	Latest        string         // newest release, "" if unknown
}

var ImmichData = source{key: "immich.data", ttl: opsTTL, service: enums.ServiceImmich, fetch: fetchImmich}

func fetchImmich(ctx context.Context, sctx Ctx) (any, error) {
	if isDemo(sctx) {
		return DemoImmich(), nil
	}
	secret, err := needSecret(sctx)
	if err != nil {
		return nil, err
	}
	api := services.ImmichApi{URL: sctx.URL, Key: secret, Verify: sctx.VerifyTLS}

	storage, err := api.Get(ctx, "server/storage")
	if err != nil {
		return nil, fetchError(err)
	}
	s := asMap(storage)
	data := &ImmichDataset{URL: sctx.URL, DiskPercent: asFloat(s["diskUsagePercentage"]), DiskAvailable: asStr(s["diskAvailable"])}

	// Admin-only endpoints: without an admin key they stay empty.
	if stats, err := api.Get(ctx, "server/statistics"); err == nil {
		data.Photos, data.Videos = int(asFloat(asMap(stats)["photos"])), int(asFloat(asMap(stats)["videos"]))
	}
	if jobs, err := api.Get(ctx, "jobs"); err == nil {
		data.FailedJobs = map[string]int{}
		for queue, raw := range asMap(jobs) {
			if n := int(asFloat(asMap(asMap(raw)["jobCounts"])["failed"])); n > 0 {
				data.FailedJobs[queue] = n
			}
		}
	}
	if v, err := api.Get(ctx, "server/version"); err == nil {
		m := asMap(v)
		data.Version = "v" + strconv.Itoa(int(asFloat(m["major"]))) + "." + strconv.Itoa(int(asFloat(m["minor"]))) + "." + strconv.Itoa(int(asFloat(m["patch"])))
	}
	if check, err := api.Get(ctx, "server/version-check"); err == nil {
		data.Latest = asStr(asMap(check)["releaseVersion"])
	}
	return data, nil
}

// ── Umami ──

// Site is one website's last 7 days against the 7 before.
type Site struct {
	ID, Name, Domain     string
	Views, Visitors      int
	PrevViews, PrevVisit int
}

type UmamiDataset struct {
	URL   string
	Sites []Site
}

var UmamiData = source{key: "umami.data", ttl: opsTTL, service: enums.ServiceUmami, fetch: fetchUmami}

func fetchUmami(ctx context.Context, sctx Ctx) (any, error) {
	if isDemo(sctx) {
		return DemoUmami(), nil
	}
	secret, err := needSecret(sctx)
	if err != nil {
		return nil, err
	}
	session, err := services.UmamiApi{URL: sctx.URL, Secret: secret, Verify: sctx.VerifyTLS}.Open(ctx)
	if err != nil {
		return nil, fetchError(err)
	}
	data, err := loadUmami(ctx, session, sctx.URL, time.Now())
	if err != nil {
		return nil, fetchError(err)
	}
	return data, nil
}

func loadUmami(ctx context.Context, session services.UmamiSession, base string, now time.Time) (*UmamiDataset, error) {
	sites, err := session.Get(ctx, "websites", nil)
	if err != nil {
		return nil, err
	}
	// v2 pages ({"data": [...]}), older versions answer a plain list.
	list := asList(sites)
	if list == nil {
		list = asList(asMap(sites)["data"])
	}

	data := &UmamiDataset{URL: base}
	end := now.UnixMilli()
	start := now.Add(-umamiWindow).UnixMilli()
	for _, raw := range list {
		w := asMap(raw)
		site := Site{ID: asStr(w["id"]), Name: asStr(w["name"]), Domain: asStr(w["domain"])}
		stats, err := session.Get(ctx, "websites/"+site.ID+"/stats", urlValues("startAt", start, "endAt", end))
		if err != nil {
			return nil, err
		}
		site.Views, site.PrevViews = umamiStat(stats, "pageviews")
		site.Visitors, site.PrevVisit = umamiStat(stats, "visitors")
		data.Sites = append(data.Sites, site)
	}
	return data, nil
}

// umamiStat reads a metric in both shapes: v2 {"value", "prev"} and v3
// plain numbers with a "comparison" block.
func umamiStat(stats any, name string) (int, int) {
	m := asMap(stats)
	if v, ok := m[name].(map[string]any); ok {
		return int(asFloat(v["value"])), int(asFloat(v["prev"]))
	}
	return int(asFloat(m[name])), int(asFloat(asMap(m["comparison"])[name]))
}

func urlValues(kv ...any) map[string][]string {
	out := map[string][]string{}
	for i := 0; i+1 < len(kv); i += 2 {
		out[kv[i].(string)] = []string{strconv.FormatInt(kv[i+1].(int64), 10)}
	}
	return out
}

func init() {
	Register(ScrutinyData)
	Register(testOf{ScrutinyData, func(d any) map[string]any { return map[string]any{"disks": len(d.(*ScrutinyDataset).Disks)} }})
	Register(ImmichData)
	Register(testOf{ImmichData, func(d any) map[string]any { return map[string]any{"version": d.(*ImmichDataset).Version} }})
	Register(UmamiData)
	Register(testOf{UmamiData, func(d any) map[string]any { return map[string]any{"sites": len(d.(*UmamiDataset).Sites)} }})
}
