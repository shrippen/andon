package sources

// Image updates of the containers:
//
//	What's Up Docker  GET api/containers → [{name, image{name, tag{value}}, result{tag}, updateAvailable}]
//	Watchtower        GET v1/metrics (Bearer, --http-api-metrics) → Prometheus text:
//	                  watchtower_containers_scanned 14, …_updated 2, …_failed 1, watchtower_scans_total 52

import (
	"bufio"
	"bytes"
	"context"
	"strconv"
	"strings"
	"time"

	"andon/internal/drivers/services"
	"andon/internal/enums"
	"andon/internal/sources/demoworld"
)

// ImageUpdate is a container and the newer tag found for it.
type ImageUpdate struct {
	Name, Image string
	Tag, NewTag string // NewTag "" = up to date
}

// WUDDataset is the containers What's Up Docker watches.
type WUDDataset struct {
	URL        string
	Containers []ImageUpdate
}

// Updates lists the containers with a newer tag.
func (d *WUDDataset) Updates() []ImageUpdate {
	var out []ImageUpdate
	for _, c := range d.Containers {
		if c.NewTag != "" {
			out = append(out, c)
		}
	}
	return out
}

// WatchtowerDataset is the last scan's numbers.
type WatchtowerDataset struct {
	URL                      string
	Scanned, Updated, Failed int
	Scans                    int
}

var (
	WUDData        = source{key: "wud.data", ttl: opsTTL, service: enums.ServiceWUD, fetch: fetchWUD}
	WatchtowerData = source{key: "watchtower.data", ttl: opsTTL, service: enums.ServiceWatchtower, fetch: fetchWatchtower}
)

func fetchWUD(ctx context.Context, sctx Ctx) (any, error) {
	if isDemo(sctx) {
		return DemoWUD(time.Now().UTC()), nil
	}
	raw, err := basicOrNone(sctx).Get(ctx, "api/containers", nil)
	if err != nil {
		return nil, fetchError(err)
	}
	data := &WUDDataset{URL: sctx.URL}
	for _, item := range asList(raw) {
		c := asMap(item)
		image := asMap(c["image"])
		u := ImageUpdate{Name: asStr(c["name"]), Image: asStr(image["name"]), Tag: asStr(asMap(image["tag"])["value"])}
		if up, _ := c["updateAvailable"].(bool); up {
			u.NewTag = asStr(asMap(c["result"])["tag"])
			if u.NewTag == "" {
				u.NewTag = "?" // a new digest of the same tag
			}
		}
		data.Containers = append(data.Containers, u)
	}
	return data, nil
}

// watchtowerMetrics are the metric names read.
var watchtowerMetrics = map[string]func(d *WatchtowerDataset, n int){
	"watchtower_containers_scanned": func(d *WatchtowerDataset, n int) { d.Scanned = n },
	"watchtower_containers_updated": func(d *WatchtowerDataset, n int) { d.Updated = n },
	"watchtower_containers_failed":  func(d *WatchtowerDataset, n int) { d.Failed = n },
	"watchtower_scans_total":        func(d *WatchtowerDataset, n int) { d.Scans = n },
}

func fetchWatchtower(ctx context.Context, sctx Ctx) (any, error) {
	if isDemo(sctx) {
		return DemoWatchtower(time.Now().UTC()), nil
	}
	secret, err := needSecret(sctx)
	if err != nil {
		return nil, err
	}
	body, _, err := services.BearerApi(sctx.URL, secret, sctx.TLS()).Bytes(ctx, "v1/metrics")
	if err != nil {
		return nil, fetchError(err)
	}
	data := &WatchtowerDataset{URL: sctx.URL}
	lines := bufio.NewScanner(bytes.NewReader(body))
	for lines.Scan() {
		name, value, ok := strings.Cut(strings.TrimSpace(lines.Text()), " ")
		set, known := watchtowerMetrics[name]
		if !ok || !known {
			continue
		}
		if n, err := strconv.ParseFloat(value, 64); err == nil {
			set(data, int(n))
		}
	}
	return data, nil
}

func DemoWUD(now time.Time) *WUDDataset {
	data := &WUDDataset{}
	demoworld.MustDecode("image_updates.wud", now, data)
	return data
}

func DemoWatchtower(now time.Time) *WatchtowerDataset {
	data := &WatchtowerDataset{}
	demoworld.MustDecode("image_updates.watchtower", now, data)
	return data
}

func init() {
	Register(WUDData)
	Register(WatchtowerData)
	Register(testOf{WUDData, func(d any) map[string]any { return map[string]any{"updates": len(d.(*WUDDataset).Updates())} }})
	Register(testOf{WatchtowerData, func(d any) map[string]any { return map[string]any{"scanned": d.(*WatchtowerDataset).Scanned} }})
}
