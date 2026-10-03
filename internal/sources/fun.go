package sources

// Start-page sources without an own service (Dashy widgets): public
// holidays, jokes, crypto prices, stock quotes and any JSON API.
//
//	holidays   date.nager.at      /api/v3/PublicHolidays/{year}/{country}
//	jokes      v2.jokeapi.dev     /joke/{category}?lang=&safe-mode
//	crypto     api.coingecko.com  /api/v3/simple/price
//	stocks     query1.finance.yahoo.com /v8/finance/chart/AAPL?range=1mo
//	json_api   any URL, optional (sealed) headers

import (
	"context"
	"fmt"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/text/language"

	"andon/internal/drivers/httpclient"
)

const (
	holidaysTTL = 24 * time.Hour
	jokesTTL    = time.Hour
	cryptoTTL   = 10 * time.Minute
	stocksTTL   = 15 * time.Minute
	jsonAPITTL  = 5 * time.Minute

	maxSymbols = 20
	isoDay     = "2006-01-02"

	// Joke flags never shown on a shared dashboard.
	jokeBlacklist = "nsfw,religious,political,racist,sexist,explicit"
)

// Base URLs; tests point them at local servers.
var (
	nagerBase     = "https://date.nager.at"
	jokeBase      = "https://v2.jokeapi.dev"
	coingeckoBase = "https://api.coingecko.com"
	yahooBase     = "https://query1.finance.yahoo.com"
)

// ── holidays ──

// Holiday is one public holiday.
type Holiday struct {
	Day, Name string
}

// HolidaysResult lists upcoming holidays, soonest first.
type HolidaysResult struct{ Days []Holiday }

var HolidaysSource = source{key: "holidays", ttl: holidaysTTL, fetch: fetchHolidays}

// Fetch reads this and next year: in December the next holidays are in
// January. state ("DE-BY") adds regional holidays to the national ones.
func fetchHolidays(ctx context.Context, sctx Ctx) (any, error) {
	country := isoAlpha2(asStr(sctx.Params["country"]))
	state := strings.ToUpper(asStr(sctx.Params["state"]))
	if code, sub, ok := strings.Cut(state, "-"); ok {
		state = isoAlpha2(code) + "-" + sub
	}
	today := time.Now().UTC().Format(isoDay)
	year := time.Now().UTC().Year()

	out := &HolidaysResult{}
	for _, y := range []int{year, year + 1} {
		target := nagerBase + "/api/v3/PublicHolidays/" + strconv.Itoa(y) + "/" + url.PathEscape(country)
		body, _, err := httpclient.GetJSON(ctx, target, httpclient.Options{})
		if err != nil {
			return nil, newSourceError("%s", err.Error())
		}
		for _, raw := range asList(body) {
			h := asMap(raw)
			if asStr(h["date"]) < today || !holidayApplies(h, state) {
				continue
			}
			out.Days = append(out.Days, Holiday{Day: asStr(h["date"]), Name: asStr(h["localName"])})
		}
	}
	sort.SliceStable(out.Days, func(i, j int) bool { return out.Days[i].Day < out.Days[j].Day })
	return out, nil
}

// isoAlpha2 turns a country code into the two letters Nager.Date wants,
// e.g. "DEU" (Dashy) or "de" -> "DE"; unknown codes stay as given.
func isoAlpha2(code string) string {
	code = strings.ToUpper(strings.TrimSpace(code))
	region, err := language.ParseRegion(code)
	if err != nil || !region.IsCountry() {
		return code
	}
	return region.String()
}

// holidayApplies: national holidays always, regional ones for state only.
func holidayApplies(h map[string]any, state string) bool {
	if asBool(h["global"]) {
		return true
	}
	for _, c := range asList(h["counties"]) {
		if asStr(c) == state {
			return true
		}
	}
	return false
}

// ── jokes ──

// Joke is a one-liner (Setup only) or setup plus punchline.
type Joke struct {
	Setup, Delivery string
}

var JokesSource = source{key: "jokes", ttl: jokesTTL, fetch: fetchJokes}

func fetchJokes(ctx context.Context, sctx Ctx) (any, error) {
	category := asStr(sctx.Params["category"])
	if category == "" {
		category = "Any"
	}
	query := url.Values{"lang": {asStr(sctx.Params["lang"])}, "blacklistFlags": {jokeBlacklist}, "safe-mode": {""}}
	body, _, err := httpclient.GetJSON(ctx, jokeBase+"/joke/"+url.PathEscape(category), httpclient.Options{Params: query})
	if err != nil {
		return nil, newSourceError("%s", err.Error())
	}
	m := asMap(body)
	if asBool(m["error"]) {
		return nil, newSourceError("%s", asStr(m["message"]))
	}
	if asStr(m["type"]) == "single" {
		return &Joke{Setup: asStr(m["joke"])}, nil
	}
	return &Joke{Setup: asStr(m["setup"]), Delivery: asStr(m["delivery"])}, nil
}

// ── crypto ──

// Coin is one cryptocurrency's price and 24 h change in percent.
type Coin struct {
	ID            string
	Price, Change float64
	Spark         []float64 // 7 days, with "spark"
}

// CryptoResult lists coins in the configured order.
type CryptoResult struct {
	Currency string
	Coins    []Coin
}

var CryptoSource = source{key: "crypto", ttl: cryptoTTL, fetch: fetchCrypto}

func fetchCrypto(ctx context.Context, sctx Ctx) (any, error) {
	ids := limitList(sctx.Params["coins"])
	currency := strings.ToLower(asStr(sctx.Params["currency"]))
	if asBool(sctx.Params["spark"]) {
		return cryptoMarkets(ctx, ids, currency)
	}
	query := url.Values{"ids": {strings.Join(ids, ",")}, "vs_currencies": {currency}, "include_24hr_change": {"true"}}
	body, _, err := httpclient.GetJSON(ctx, coingeckoBase+"/api/v3/simple/price", httpclient.Options{Params: query})
	if err != nil {
		return nil, newSourceError("%s", err.Error())
	}
	prices := asMap(body)
	out := &CryptoResult{Currency: strings.ToUpper(currency)}
	for _, id := range ids {
		p, ok := prices[id]
		if !ok {
			continue
		}
		m := asMap(p)
		out.Coins = append(out.Coins, Coin{ID: id, Price: asFloat(m[currency]), Change: asFloat(m[currency+"_24h_change"])})
	}
	return out, nil
}

// cryptoMarkets reads prices with their 7-day line (CoinGecko markets).
func cryptoMarkets(ctx context.Context, ids []string, currency string) (any, error) {
	query := url.Values{"vs_currency": {currency}, "ids": {strings.Join(ids, ",")}, "sparkline": {"true"}}
	body, _, err := httpclient.GetJSON(ctx, coingeckoBase+"/api/v3/coins/markets", httpclient.Options{Params: query})
	if err != nil {
		return nil, newSourceError("%s", err.Error())
	}
	byID := map[string]map[string]any{}
	for _, raw := range asList(body) {
		m := asMap(raw)
		byID[asStr(m["id"])] = m
	}
	out := &CryptoResult{Currency: strings.ToUpper(currency)}
	for _, id := range ids {
		m, ok := byID[id]
		if !ok {
			continue
		}
		c := Coin{ID: id, Price: asFloat(m["current_price"]), Change: asFloat(m["price_change_percentage_24h"])}
		for _, p := range asList(asMap(m["sparkline_in_7d"])["price"]) {
			c.Spark = append(c.Spark, asFloat(p))
		}
		out.Coins = append(out.Coins, c)
	}
	return out, nil
}

func limitList(v any) []string {
	list, _ := v.([]string)
	if len(list) > maxSymbols {
		list = list[:maxSymbols]
	}
	out := make([]string, 0, len(list))
	for _, s := range list {
		if s = strings.ToLower(strings.TrimSpace(s)); s != "" {
			out = append(out, s)
		}
	}
	return out
}

// ── stocks ──

// Quote is one stock's last price, its change against the previous
// close and against a week ago (in percent), and its recent daily closes.
type Quote struct {
	Symbol, Day, Currency string
	Close, Change         float64
	WeekChange            float64
	Closes                []float64 // oldest first, about a month
}

type StocksResult struct{ Quotes []Quote }

var StocksSource = source{key: "stocks", ttl: stocksTTL, fetch: fetchStocks}

// weekBack is how many trading days make a week.
const weekBack = 5

// browserAgent: Yahoo refuses requests without a browser-like agent.
const browserAgent = "Mozilla/5.0 (X11; Linux x86_64) Andon"

// yahooSuffix maps stooq's market suffixes to Yahoo's ("sap.de" → SAP.DE).
var yahooSuffix = map[string]string{"us": "", "de": ".DE", "uk": ".L", "jp": ".T", "hk": ".HK", "fr": ".PA", "nl": ".AS"}

// yahooSymbol turns a configured symbol into Yahoo's.
func yahooSymbol(s string) string {
	if name, market, found := strings.Cut(s, "."); found {
		if suffix, known := yahooSuffix[strings.ToLower(market)]; known {
			return strings.ToUpper(name) + suffix
		}
	}
	return strings.ToUpper(s)
}

// Fetch reads each symbol's month of daily closes from Yahoo's chart API
// (stooq, the former source, stopped answering). Unknown symbols are
// skipped.
func fetchStocks(ctx context.Context, sctx Ctx) (any, error) {
	out := &StocksResult{}
	var failed error
	for _, sym := range limitList(sctx.Params["symbols"]) {
		q, err := yahooQuote(ctx, yahooSymbol(sym))
		if err != nil {
			failed = err
			continue
		}
		out.Quotes = append(out.Quotes, q)
	}
	if len(out.Quotes) == 0 && failed != nil {
		return nil, newSourceError("%s", failed.Error())
	}
	return out, nil
}

func yahooQuote(ctx context.Context, symbol string) (Quote, error) {
	body, _, err := httpclient.GetJSON(ctx, yahooBase+"/v8/finance/chart/"+url.PathEscape(symbol),
		httpclient.Options{Params: url.Values{"range": {"1mo"}, "interval": {"1d"}}, Headers: map[string]string{"User-Agent": browserAgent}})
	if err != nil {
		return Quote{}, err
	}
	results := asList(asMap(asMap(body)["chart"])["result"])
	if len(results) == 0 {
		return Quote{}, fmt.Errorf("%s: no quote", symbol)
	}
	r := asMap(results[0])
	meta := asMap(r["meta"])
	q := Quote{Symbol: symbol, Currency: asStr(meta["currency"]), Close: asFloat(meta["regularMarketPrice"])}
	if quotes := asList(asMap(r["indicators"])["quote"]); len(quotes) > 0 {
		for _, c := range asList(asMap(quotes[0])["close"]) {
			if f, ok := c.(float64); ok {
				q.Closes = append(q.Closes, f)
			}
		}
	}
	change := func(back int) float64 {
		if len(q.Closes) <= back {
			return 0
		}
		ref := q.Closes[len(q.Closes)-1-back]
		if ref == 0 {
			return 0
		}
		return (q.Closes[len(q.Closes)-1] - ref) / ref * percent
	}
	q.Change, q.WeekChange = change(1), change(weekBack)
	return q, nil
}

const percent = 100

// ── json_api ──

// JSONResult is any decoded JSON body; the widget picks fields from it.
type JSONResult struct{ Body any }

var JSONAPISource = source{key: "json_api", ttl: jsonAPITTL, fetch: fetchJSONAPISource}

func fetchJSONAPISource(ctx context.Context, sctx Ctx) (any, error) {
	headers, _ := sctx.Params["headers"].(map[string]string)
	body, _, err := httpclient.GetJSON(ctx, asStr(sctx.Params["url"]), httpclient.Options{Headers: headers})
	if err != nil {
		return nil, newSourceError("%s", err.Error())
	}
	return &JSONResult{Body: body}, nil
}

// coinHistoryDays is the span of the crypto dialog's lines.
const coinHistoryDays = 30

// CoinHistory is daily prices per coin, oldest first.
type CoinHistory struct{ ByID map[string][]float64 }

// CryptoHistorySource reads a month of daily prices when the dialog opens.
var CryptoHistorySource = source{key: "crypto.history", ttl: cryptoTTL, fetch: fetchCryptoHistory}

func fetchCryptoHistory(ctx context.Context, sctx Ctx) (any, error) {
	ids := limitList(sctx.Params["coins"])
	currency := strings.ToLower(asStr(sctx.Params["currency"]))
	out := &CoinHistory{ByID: map[string][]float64{}}
	var mu sync.Mutex
	parallel(ctx, len(ids), releasesParallel, func(i int) {
		query := url.Values{"vs_currency": {currency}, "days": {strconv.Itoa(coinHistoryDays)}, "interval": {"daily"}}
		body, _, err := httpclient.GetJSON(ctx, coingeckoBase+"/api/v3/coins/"+url.PathEscape(ids[i])+"/market_chart", httpclient.Options{Params: query})
		if err != nil {
			return
		}
		var prices []float64
		for _, p := range asList(asMap(body)["prices"]) {
			if pair := asList(p); len(pair) == 2 {
				prices = append(prices, asFloat(pair[1]))
			}
		}
		mu.Lock()
		out.ByID[ids[i]] = prices
		mu.Unlock()
	})
	return out, nil
}

func init() {
	Register(CryptoHistorySource)
	Register(HolidaysSource)
	Register(JokesSource)
	Register(CryptoSource)
	Register(StocksSource)
	Register(JSONAPISource)
}
