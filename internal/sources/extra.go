package sources

// Extra start-page sources for Dashy widgets: a remote image (inlined, so
// the browser never contacts the image host and CSP stays 'self') and
// ECB exchange rates.

import (
	"context"
	"encoding/base64"
	"io"
	"mime"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"andon/internal/drivers/httpclient"
)

const (
	imageTTL = time.Hour
	imageMax = 1 << 20
	ratesTTL = 6 * time.Hour

	imagePrefix  = "image/"
	defaultBase  = "EUR"
	symbolsLimit = 20
)

// ── image ──

// ImageResult is the image as a data: URI.
type ImageResult struct{ DataURI string }

var ImageSource = source{key: "image", ttl: imageTTL, fetch: fetchImageSource}

func fetchImageSource(ctx context.Context, sctx Ctx) (any, error) {
	return fetchImage(ctx, asStr(sctx.Params["url"]), imageMax)
}

// fetchImage downloads an image of at most limit bytes as a data: URI.
func fetchImage(ctx context.Context, target string, limit int) (*ImageResult, error) {
	resp, err := httpclient.Request(ctx, http.MethodGet, target, httpclient.Options{})
	if err != nil {
		return nil, newSourceError("%s", err.Error())
	}
	defer resp.Body.Close()

	if resp.StatusCode >= http.StatusBadRequest {
		return nil, newSourceError("HTTP %d", resp.StatusCode)
	}
	kind, _, _ := mime.ParseMediaType(resp.Header.Get("Content-Type"))
	if !strings.HasPrefix(kind, imagePrefix) {
		return nil, newSourceError("not an image")
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, int64(limit)+1))
	if err != nil || len(body) > limit {
		return nil, newSourceError("image too large")
	}
	return &ImageResult{DataURI: "data:" + kind + ";base64," + base64.StdEncoding.EncodeToString(body)}, nil
}

// ── exchange_rates ──

type Rate struct {
	Code    string
	Value   float64
	Inverse float64 // 1 Code in the base currency
	Change  float64 // % against the previous day (with "change")
}

// frankfurterBase is the ECB rates API (moved from api.frankfurter.app).
var frankfurterBase = "https://api.frankfurter.dev/v1"

// rateWeek is how far back the change looks for the previous day.
const rateWeek = 7

// RatesResult is one base currency against others (ECB reference rates).
type RatesResult struct {
	Base  string
	Day   string
	Rates []Rate
}

var RatesSource = source{key: "exchange_rates", ttl: ratesTTL, fetch: fetchRates}

func fetchRates(ctx context.Context, sctx Ctx) (any, error) {
	base := strings.ToUpper(asStr(sctx.Params["base"]))
	if base == "" {
		base = defaultBase
	}
	query := url.Values{"from": {base}}
	symbols, _ := sctx.Params["symbols"].([]string)
	if len(symbols) > symbolsLimit {
		symbols = symbols[:symbolsLimit]
	}
	if len(symbols) > 0 {
		query.Set("to", strings.ToUpper(strings.Join(symbols, ",")))
	}

	body, _, err := httpclient.GetJSON(ctx, frankfurterBase+"/latest", httpclient.Options{Params: query})
	if err != nil {
		return nil, newSourceError("%s", err.Error())
	}
	m := asMap(body)
	out := &RatesResult{Base: asStr(m["base"]), Day: asStr(m["date"])}
	for code, v := range asMap(m["rates"]) {
		r := Rate{Code: code, Value: asFloat(v)}
		if r.Value != 0 {
			r.Inverse = 1 / r.Value
		}
		out.Rates = append(out.Rates, r)
	}
	sort.Slice(out.Rates, func(i, j int) bool { return out.Rates[i].Code < out.Rates[j].Code })
	if asBool(sctx.Params["change"]) {
		ratesChange(ctx, out, query, asStr(sctx.Params["today"]))
	}
	return out, nil
}

// ratesChange reads the last week's series and sets each rate's change
// against the day before its latest; a failure leaves the changes at 0.
func ratesChange(ctx context.Context, out *RatesResult, query url.Values, today string) {
	day, err := time.Parse(time.DateOnly, today)
	if err != nil {
		day = time.Now().UTC()
	}
	body, _, err := httpclient.GetJSON(ctx, frankfurterBase+"/"+day.AddDate(0, 0, -rateWeek).Format(time.DateOnly)+"..", httpclient.Options{Params: query})
	if err != nil {
		return
	}
	series := asMap(asMap(body)["rates"])
	days := make([]string, 0, len(series))
	for d := range series {
		days = append(days, d)
	}
	sort.Strings(days)
	if len(days) < 2 {
		return
	}
	prev := asMap(series[days[len(days)-2]])
	for i, r := range out.Rates {
		if p := asFloat(prev[r.Code]); p != 0 {
			out.Rates[i].Change = (r.Value - p) / p * percent
		}
	}
}

func init() {
	Register(ImageSource)
	Register(RatesSource)
}
