package sources

// FRITZ!Box over TR-064 (services.TR064): the internet connection and,
// on DSL lines, its sync rates. A FRITZ!Box user with the right
// "FRITZ!Box settings" is needed; the secret is "user:password".
//
//	wanpppconn1 / wanipconnection1  GetInfo → NewConnectionStatus, NewUptime, NewLastConnectionError
//	wandslifconfig1                 GetInfo → NewDownstreamCurrRate, NewUpstreamCurrRate, …MaxRate (kbit/s)
//	deviceinfo                      GetInfo → NewModelName

import (
	"context"
	"strconv"
	"strings"
	"time"

	"andon/internal/drivers/services"
	"andon/internal/enums"
	"andon/internal/sources/demoworld"
)

// fritzConnected is TR-064's word for a working internet connection.
const fritzConnected = "Connected"

// FritzDataset is the box's line.
type FritzDataset struct {
	URL       string
	Model     string
	Status    string // Connected, Connecting, Disconnected …
	Uptime    int    // seconds since the connection came up
	LastError string
	// Sync and max rates of a DSL line in kbit/s; 0 on cable and fibre.
	DownSync, UpSync, DownMax, UpMax int
}

// Connected reports whether the internet connection is up.
func (d *FritzDataset) Connected() bool { return d.Status == fritzConnected }

var FritzData = source{key: "fritzbox.data", ttl: opsTTL, service: enums.ServiceFritzBox, fetch: fetchFritz}

func fetchFritz(ctx context.Context, sctx Ctx) (any, error) {
	if isDemo(sctx) {
		return DemoFritz(time.Now().UTC()), nil
	}
	secret, err := needSecret(sctx)
	if err != nil {
		return nil, err
	}
	user, pass, _ := strings.Cut(secret, ":")
	box := services.TR064{URL: sctx.URL, User: user, Password: pass, Mode: sctx.TLS()}

	// PPP lines (DSL) and IP connections (cable, fibre, LTE) answer on
	// different services.
	conn, err := box.Call(ctx, "wanpppconn1", "WANPPPConnection:1", "GetInfo")
	if err != nil || conn["NewConnectionStatus"] == "" {
		if conn, err = box.Call(ctx, "wanipconnection1", "WANIPConnection:1", "GetInfo"); err != nil {
			return nil, fetchError(err)
		}
	}
	data := &FritzDataset{URL: sctx.URL, Status: conn["NewConnectionStatus"], Uptime: atoiOr(conn["NewUptime"]), LastError: conn["NewLastConnectionError"]}
	if dsl, err := box.Call(ctx, "wandslifconfig1", "WANDSLInterfaceConfig:1", "GetInfo"); err == nil {
		data.DownSync, data.UpSync = atoiOr(dsl["NewDownstreamCurrRate"]), atoiOr(dsl["NewUpstreamCurrRate"])
		data.DownMax, data.UpMax = atoiOr(dsl["NewDownstreamMaxRate"]), atoiOr(dsl["NewUpstreamMaxRate"])
	}
	if info, err := box.Call(ctx, "deviceinfo", "DeviceInfo:1", "GetInfo"); err == nil {
		data.Model = info["NewModelName"]
	}
	return data, nil
}

func atoiOr(s string) int {
	n, _ := strconv.Atoi(strings.TrimSpace(s))
	return n
}

func DemoFritz(now time.Time) *FritzDataset {
	data := &FritzDataset{}
	demoworld.MustDecode("fritzbox", now, data)
	return data
}

// ── Technitium DNS ──

// GET api/dashboard/stats/get?token=…&type=LastDay → {status, response: {stats: {totalQueries, totalBlocked, totalClients}, topBlockedDomains: [{name, hits}]}}

var TechnitiumData = source{key: "technitium.data", ttl: opsTTL, service: enums.ServiceTechnitium, fetch: fetchTechnitium}

func fetchTechnitium(ctx context.Context, sctx Ctx) (any, error) {
	if isDemo(sctx) {
		return DemoTechnitium(time.Now().UTC()), nil
	}
	secret, err := needSecret(sctx)
	if err != nil {
		return nil, err
	}
	api := services.KeyedApi{URL: sctx.URL, Headers: map[string]string{"Accept": "application/json"}, Verify: sctx.VerifyTLS}
	raw, err := api.Get(ctx, "api/dashboard/stats/get", map[string][]string{"token": {secret}, "type": {"LastDay"}})
	if err != nil {
		return nil, fetchError(err)
	}
	answer := asMap(raw)
	if asStr(answer["status"]) != "ok" {
		return nil, newSourceError("technitium.refused")
	}
	resp := asMap(answer["response"])
	stats := asMap(resp["stats"])
	data := &DNSFilterDataset{URL: sctx.URL, Queries: int(asFloat(stats["totalQueries"])), Blocked: int(asFloat(stats["totalBlocked"])),
		Clients: int(asFloat(stats["totalClients"])), Enabled: true}
	if data.Queries > 0 {
		data.Percent = float64(data.Blocked) / float64(data.Queries) * 100
	}
	for _, raw := range asList(resp["topBlockedDomains"]) {
		d := asMap(raw)
		data.TopBlocked = append(data.TopBlocked, DNSDomain{Domain: asStr(d["name"]), Count: int(asFloat(d["hits"]))})
	}
	return data, nil
}

func DemoTechnitium(now time.Time) *DNSFilterDataset {
	var p struct {
		DNSFilterDataset
		TopBlocked []struct {
			Name  string
			Count int
		}
	}
	demoworld.MustDecode("dns_technitium", now, &p)
	data := &p.DNSFilterDataset
	data.TopBlocked = nil
	for _, d := range p.TopBlocked {
		data.TopBlocked = append(data.TopBlocked, DNSDomain{Domain: d.Name, Count: d.Count})
	}
	if data.Queries > 0 {
		data.Percent = float64(data.Blocked) / float64(data.Queries) * 100
	}
	return data
}

func init() {
	Register(FritzData)
	Register(testOf{FritzData, func(d any) map[string]any { return map[string]any{"status": d.(*FritzDataset).Status} }})
	Register(TechnitiumData)
	Register(testOf{TechnitiumData, func(d any) map[string]any { return map[string]any{"queries": d.(*DNSFilterDataset).Queries} }})
}
