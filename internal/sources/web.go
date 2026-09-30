// Package sources: generic web sources shared by start widgets — status
// checks, feeds, weather, system stats, public IP.
package sources

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"andon/internal/drivers/httpclient"
	"andon/internal/drivers/services"
	"andon/internal/enums"
)

const (
	httpStatusTTL = 5 * time.Minute
	feedTTL       = 30 * time.Minute
	weatherTTL    = 30 * time.Minute
	glancesTTL    = time.Minute
	publicIPTTL   = time.Hour

	httpOKMin       = 200
	httpOKMax       = 399
	pageAccept      = "text/html,application/xhtml+xml,*/*;q=0.8"
	feedLimitMax    = 50
	publicIPURL     = "https://api.ipify.org"
	forecastDays    = 4
	maxForecastDays = 8
	forecastHrs     = 24
)

// openMeteoURL is a var so tests can point it at a local server.
var openMeteoURL = "https://api.open-meteo.com/v1/forecast"

// ── http_status ──

// HTTPStatusResult is one link's reachability check.
type HTTPStatusResult struct {
	Up    bool
	Code  int
	Ms    int
	Error string // "" if the request itself succeeded (Up may still be false)
}

// Outcome is (up, response ms) for background bookkeeping; ms only
// counts when a response came back.
func (r *HTTPStatusResult) Outcome() (bool, int) {
	if r.Error != "" || !r.Up {
		return false, 0
	}
	return true, r.Ms
}

var HTTPStatusSource = source{key: "http_status", ttl: httpStatusTTL, fetch: fetchHTTPStatus}

// Fetch checks a URL's reachability. A failed check is data, not an error
// — the widget shows "down", it doesn't fail to load.
func fetchHTTPStatus(ctx context.Context, sctx Ctx) (any, error) {
	target := asStr(sctx.Params["url"])
	accept, _ := sctx.Params["accept"].([]int)
	insecure, _ := sctx.Params["insecure"].(bool)
	headers, _ := sctx.Params["headers"].(map[string]string)

	// Ask for a page like a browser: login proxies (mod_auth_openidc) answer
	// other requests with 401 instead of the login redirect.
	withAccept := map[string]string{"Accept": pageAccept}
	for k, v := range headers {
		if strings.EqualFold(k, "Accept") {
			k = "Accept" // the link's own Accept wins
		}
		withAccept[k] = v
	}
	headers = withAccept

	method := http.MethodGet
	if strings.EqualFold(asStr(sctx.Params["method"]), http.MethodHead) {
		method = http.MethodHead
	}
	opts := httpclient.Options{SkipVerify: insecure, Headers: headers}
	if s := asFloat(sctx.Params["timeout"]); s > 0 {
		opts.Timeout = time.Duration(s * float64(time.Second))
	}
	started := time.Now()
	resp, err := httpclient.Request(ctx, method, target, opts)
	if err != nil {
		msg := "egress"
		var denied httpclient.EgressDenied
		if !errors.As(err, &denied) {
			msg = err.Error()
		}
		return &HTTPStatusResult{Error: msg}, nil
	}
	defer resp.Body.Close()

	ms := int(time.Since(started).Milliseconds())
	code := resp.StatusCode
	// accept adds codes to 2xx/3xx, e.g. 401 for a login wall (Dashy's
	// statusCheckAcceptCodes means the same).
	up := code >= httpOKMin && code <= httpOKMax || slices.Contains(accept, code)
	return &HTTPStatusResult{Up: up, Code: code, Ms: ms}, nil
}

// ── rss ──

// FeedItem is one entry of a parsed feed.
type FeedItem struct {
	Title     string
	Link      string
	Published string // ISO 8601, "" if unknown
	Summary   string
	Image     string // data: URI, only when asked for
	imageURL  string
}

// Feed pictures: how many items get one, and how large one may be.
const (
	feedImages   = 6
	feedImageMax = 150 << 10
)

// FeedResult is a parsed RSS/Atom feed.
type FeedResult struct {
	Title string
	Items []FeedItem
}

var FeedSource = source{key: "rss", ttl: feedTTL, fetch: fetchFeedSource}

func fetchFeedSource(ctx context.Context, sctx Ctx) (any, error) {
	targets := []string{asStr(sctx.Params["url"])}
	switch more := sctx.Params["urls"].(type) {
	case []string:
		targets = append(targets, more...)
	case []any:
		for _, u := range more {
			targets = append(targets, asStr(u))
		}
	}
	targets = slices.DeleteFunc(targets[1:], func(u string) bool { return u == "" })
	targets = append([]string{asStr(sctx.Params["url"])}, targets...)
	var merged *FeedResult
	for i, target := range targets {
		parsed, err := fetchFeed(ctx, target)
		if err != nil {
			if i == 0 {
				return nil, err
			}
			continue // a further feed failing costs only its items
		}
		if merged == nil {
			merged = parsed
			continue
		}
		merged.Items = append(merged.Items, parsed.Items...)
	}
	if len(targets) > 1 {
		// Newest first across feeds; undated items last.
		sort.SliceStable(merged.Items, func(a, b int) bool {
			pa, pb := merged.Items[a].Published, merged.Items[b].Published
			if (pa == "") != (pb == "") {
				return pb == ""
			}
			return pa > pb
		})
	}
	if days := asFloat(sctx.Params["max_age"]); days > 0 {
		cut := time.Now().UTC().Add(-time.Duration(days * 24 * float64(time.Hour))).Format(time.RFC3339)
		kept := merged.Items[:0]
		for _, it := range merged.Items {
			if it.Published == "" || it.Published >= cut {
				kept = append(kept, it)
			}
		}
		merged.Items = kept
	}

	limit := int(asFloat(sctx.Params["limit"]))
	if limit <= 0 {
		limit = 8
	}
	if limit > feedLimitMax {
		limit = feedLimitMax
	}
	if len(merged.Items) > limit {
		merged.Items = merged.Items[:limit]
	}
	if asBool(sctx.Params["images"]) {
		for i := range merged.Items[:min(len(merged.Items), feedImages)] {
			if u := merged.Items[i].imageURL; u != "" {
				if img, err := fetchImage(ctx, u, feedImageMax); err == nil {
					merged.Items[i].Image = img.DataURI
				}
			}
		}
	}
	return merged, nil
}

func fetchFeed(ctx context.Context, target string) (*FeedResult, error) {
	resp, err := httpclient.Request(ctx, "GET", target, httpclient.Options{})
	if err != nil {
		return nil, newSourceError("%s", err.Error())
	}
	defer resp.Body.Close()
	parsed, err := parseFeed(resp.Body)
	if err != nil {
		return nil, newSourceError("invalid feed")
	}
	return parsed, nil
}

// ── open_meteo ──

// WeatherDay is one forecast day.
type WeatherDay struct {
	Day      string
	Code     int
	Max, Min float64
}

// WeatherHour is one hour ahead: temperature and rain probability (0–100).
type WeatherHour struct {
	At         string // local "2006-01-02T15:04"
	Temp, Rain float64
}

// WeatherResult is the current conditions plus a short forecast.
type WeatherResult struct {
	Temp, Wind float64
	Code       int
	IsDay      bool
	Days       []WeatherDay
	Hours      []WeatherHour // the next forecastHrs hours
}

var WeatherSource = source{key: "open_meteo", ttl: weatherTTL, fetch: fetchWeather}

func fetchWeather(ctx context.Context, sctx Ctx) (any, error) {
	params := url.Values{
		"latitude":       {strconv.FormatFloat(asFloat(sctx.Params["lat"]), 'f', -1, 64)},
		"longitude":      {strconv.FormatFloat(asFloat(sctx.Params["lon"]), 'f', -1, 64)},
		"current":        {"temperature_2m,weather_code,wind_speed_10m,is_day"},
		"daily":          {"weather_code,temperature_2m_max,temperature_2m_min"},
		"hourly":         {"temperature_2m,precipitation_probability"},
		"forecast_hours": {strconv.Itoa(forecastHrs)},
		"timezone":       {"auto"},
		// A tile may want up to a week after today.
		"forecast_days": {strconv.Itoa(min(max(forecastDays, int(asFloat(sctx.Params["days"]))+1), maxForecastDays))},
	}
	body, _, err := httpclient.GetJSON(ctx, openMeteoURL, httpclient.Options{Params: params})
	if err != nil {
		return nil, newSourceError("%s", err.Error())
	}

	raw := asMap(body)
	current := asMap(raw["current"])
	daily := asMap(raw["daily"])
	days := asList(daily["time"])
	codes := asList(daily["weather_code"])
	highs := asList(daily["temperature_2m_max"])
	lows := asList(daily["temperature_2m_min"])

	var forecast []WeatherDay
	for i := range days {
		if i >= len(codes) || i >= len(highs) || i >= len(lows) {
			break
		}
		forecast = append(forecast, WeatherDay{
			Day: asStr(days[i]), Code: int(asFloat(codes[i])), Max: asFloat(highs[i]), Min: asFloat(lows[i]),
		})
	}

	hourly := asMap(raw["hourly"])
	times, temps, rains := asList(hourly["time"]), asList(hourly["temperature_2m"]), asList(hourly["precipitation_probability"])
	var hours []WeatherHour
	for i := range times {
		if i >= len(temps) || i >= len(rains) {
			break
		}
		hours = append(hours, WeatherHour{At: asStr(times[i]), Temp: asFloat(temps[i]), Rain: asFloat(rains[i])})
	}

	isDay := true
	if v, ok := current["is_day"]; ok {
		isDay = asFloat(v) != 0
	}
	return &WeatherResult{
		Temp: asFloat(current["temperature_2m"]), Wind: asFloat(current["wind_speed_10m"]),
		Code: int(asFloat(current["weather_code"])), IsDay: isDay, Days: forecast, Hours: hours,
	}, nil
}

// ── glances ──

// GlancesDisk is one mounted filesystem's usage.
type GlancesDisk struct {
	Mount   string
	Percent float64
}

// GlancesResult is a host's current load, reported by its Glances agent.
type GlancesResult struct {
	URL                  string
	CPU, Mem, Swap, Load float64
	Cores                int // logical cores, to judge Load (0 = unknown)
	Disks                []GlancesDisk
}

var GlancesSource = source{key: "glances", ttl: glancesTTL, service: enums.ServiceGlances, fetch: fetchGlances}

func fetchGlances(ctx context.Context, sctx Ctx) (any, error) {
	if isDemo(sctx) {
		return DemoGlances(), nil
	}
	api := services.GlancesApi{URL: sctx.URL, Token: sctx.Secret, Verify: sctx.VerifyTLS, Version: int(asFloat(sctx.Options["api_version"]))}

	quick, err := api.Get(ctx, "quicklook")
	if err != nil {
		return nil, newSourceError("%s", err.Error())
	}
	disks, err := api.Get(ctx, "fs")
	if err != nil {
		return nil, newSourceError("%s", err.Error())
	}
	load, err := api.Get(ctx, "load")
	if err != nil {
		return nil, newSourceError("%s", err.Error())
	}

	quickM, loadM := asMap(quick), asMap(load)
	var mounts []GlancesDisk
	for _, d := range asList(disks) {
		m := asMap(d)
		mounts = append(mounts, GlancesDisk{Mount: asStr(m["mnt_point"]), Percent: asFloat(m["percent"])})
	}
	return &GlancesResult{
		URL: sctx.URL, CPU: asFloat(quickM["cpu"]), Mem: asFloat(quickM["mem"]), Swap: asFloat(quickM["swap"]),
		Load: asFloat(loadM["min5"]), Cores: int(asFloat(loadM["cpucore"])), Disks: mounts,
	}, nil
}

// ── public_ip ──

// PublicIPResult is the container's outbound public IP; IPv6 only when
// asked for and the host has it.
type PublicIPResult struct{ IP, IPv6 string }

// publicIPv6URL answers over IPv6 only; a var so tests can replace it.
var publicIPv6URL = "https://api6.ipify.org"

var PublicIPSource = source{key: "public_ip", ttl: publicIPTTL, fetch: fetchPublicIP}

func fetchPublicIP(ctx context.Context, sctx Ctx) (any, error) {
	body, _, err := httpclient.GetJSON(ctx, ownIPURL, httpclient.Options{Params: url.Values{"format": {"json"}}})
	if err != nil {
		return nil, newSourceError("%s", err.Error())
	}
	out := &PublicIPResult{IP: asStr(asMap(body)["ip"])}
	if asBool(sctx.Params["v6"]) {
		// Best-effort: without IPv6 the request just fails.
		if v6, _, err := httpclient.GetJSON(ctx, publicIPv6URL, httpclient.Options{Params: url.Values{"format": {"json"}}}); err == nil {
			out.IPv6 = asStr(asMap(v6)["ip"])
		}
	}
	return out, nil
}

func init() {
	Register(HTTPStatusSource)
	Register(FeedSource)
	Register(WeatherSource)
	Register(GlancesSource)
	Register(PublicIPSource)
}
