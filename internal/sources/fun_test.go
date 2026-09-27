package sources_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"andon/internal/sources"
)

func fetch(t *testing.T, key string, params map[string]any) any {
	t.Helper()
	src, err := sources.Get(key)
	if err != nil {
		t.Fatal(err)
	}
	out, err := src.Fetch(context.Background(), sources.Ctx{Params: params})
	if err != nil {
		t.Fatalf("%s: %v", key, err)
	}
	return out
}

func publicAPIs(t *testing.T) {
	year := time.Now().UTC().Year()
	next := time.Now().UTC().AddDate(0, 0, 3).Format("2006-01-02")
	later := strconv.Itoa(year+1) + "-01-01"
	srv := jsonServer(t, map[string]any{
		"/api/v3/PublicHolidays/" + strconv.Itoa(year) + "/DE": []any{
			map[string]any{"date": "2000-01-01", "localName": "Past", "global": true},
			map[string]any{"date": next, "localName": "Regional", "global": false, "counties": []any{"DE-BY"}},
			map[string]any{"date": next, "localName": "Elsewhere", "global": false, "counties": []any{"DE-NW"}},
		},
		"/api/v3/PublicHolidays/" + strconv.Itoa(year+1) + "/DE": []any{map[string]any{"date": later, "localName": "Neujahr", "global": true}},
		"/joke/Programming":    map[string]any{"type": "twopart", "setup": "Q", "delivery": "A"},
		"/api/v3/simple/price": map[string]any{"bitcoin": map[string]any{"eur": 50000.0, "eur_24h_change": -2.5}},
		"/info.0.json":         map[string]any{"num": 42, "safe_title": "T", "alt": "alt", "img": "IMG"},
	}, nil)
	restore := sources.SetBases(srv.URL)
	t.Cleanup(restore)
}

// TestHolidaysTakeThreeLetterCodes: a Dashy import brings "DEU" and
// "DEU-BY"; Nager.Date only knows "DE" and "DE-BY".
func TestHolidaysTakeThreeLetterCodes(t *testing.T) {
	publicAPIs(t)

	days := fetch(t, "holidays", map[string]any{"country": "DEU", "state": "DEU-BY"}).(*sources.HolidaysResult).Days
	if len(days) != 2 || days[0].Name != "Regional" {
		t.Fatalf("holidays: %+v", days)
	}
}

func TestHolidaysJokesCrypto(t *testing.T) {
	publicAPIs(t)

	days := fetch(t, "holidays", map[string]any{"country": "de", "state": "de-by"}).(*sources.HolidaysResult).Days
	if len(days) != 2 || days[0].Name != "Regional" || days[1].Name != "Neujahr" {
		t.Fatalf("holidays: %+v", days)
	}

	joke := fetch(t, "jokes", map[string]any{"category": "Programming", "lang": "de"}).(*sources.Joke)
	if joke.Setup != "Q" || joke.Delivery != "A" {
		t.Fatalf("joke: %+v", joke)
	}

	coins := fetch(t, "crypto", map[string]any{"coins": []string{"Bitcoin", "unknown"}, "currency": "EUR"}).(*sources.CryptoResult)
	if coins.Currency != "EUR" || len(coins.Coins) != 1 || coins.Coins[0].Change != -2.5 {
		t.Fatalf("crypto: %+v", coins)
	}
}

// TestStocksYahoo: stooq no longer answers (every quote URL says "does
// not exist"), so quotes come from Yahoo's chart API. Stooq-style symbols
// keep working ("aapl.us" is AAPL, "sap.de" SAP.DE); the day's and the
// week's change come from the daily closes, which also draw the line.
func TestStocksYahoo(t *testing.T) {
	chart := func(symbol string, closes ...float64) string {
		list := ""
		for i, c := range closes {
			if i > 0 {
				list += ","
			}
			list += strconv.FormatFloat(c, 'f', -1, 64)
		}
		return `{"chart":{"result":[{"meta":{"symbol":"` + symbol + `","currency":"USD","regularMarketPrice":` +
			strconv.FormatFloat(closes[len(closes)-1], 'f', -1, 64) + `},"indicators":{"quote":[{"close":[` + list + `]}]}}],"error":null}}`
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v8/finance/chart/AAPL":
			w.Write([]byte(chart("AAPL", 100, 101, 102, 103, 104, 105, 110)))
		case "/v8/finance/chart/SAP.DE":
			w.Write([]byte(chart("SAP.DE", 200, 180)))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	t.Cleanup(sources.SetBases(srv.URL))

	quotes := fetch(t, "stocks", map[string]any{"symbols": []string{"aapl.us", "sap.de", "xx.us"}}).(*sources.StocksResult).Quotes
	if len(quotes) != 2 {
		t.Fatalf("quotes: %+v", quotes)
	}
	a := quotes[0]
	if a.Symbol != "AAPL" || a.Close != 110 || a.Change < 4.7 || a.Change > 4.8 || a.WeekChange < 8.9 || a.WeekChange > 8.92 || len(a.Closes) != 7 || a.Currency != "USD" {
		t.Fatalf("AAPL: %+v", a)
	}
	if quotes[1].Symbol != "SAP.DE" || quotes[1].Change != -10 {
		t.Fatalf("SAP: %+v", quotes[1])
	}
}

func TestJSONAPIWithHeaders(t *testing.T) {
	srv := jsonServer(t, map[string]any{"/stats": map[string]any{"a": map[string]any{"b": []any{1.0, 2.0}}}},
		func(r *http.Request) bool { return r.Header.Get("X-Key") == "k" })
	out := fetch(t, "json_api", map[string]any{"url": srv.URL + "/stats", "headers": map[string]string{"X-Key": "k"}}).(*sources.JSONResult)
	if out.Body == nil {
		t.Fatal("empty body")
	}
}

func TestTransitByName(t *testing.T) {
	when := time.Now().UTC().Add(5 * time.Minute).Format(time.RFC3339)
	srv := jsonServer(t, map[string]any{
		"/locations": []any{map[string]any{"id": "8000261", "name": "München Hbf"}},
		"/stops/8000261/departures": map[string]any{"departures": []any{
			map[string]any{"when": when, "delay": 120, "line": map[string]any{"name": "S 1"}, "direction": "Freising", "platform": "2"},
		}},
	}, nil)
	t.Cleanup(sources.SetBases(srv.URL))

	board := fetch(t, "transit", map[string]any{"stop": "München Hbf", "results": 5.0}).(*sources.BoardResult)
	if board.Stop != "München Hbf" || len(board.Movements) != 1 || board.Movements[0].Delay != 2 || board.Movements[0].Line != "S 1" {
		t.Fatalf("board: %+v", board)
	}
}

func TestFlightsNeedKey(t *testing.T) {
	srv := jsonServer(t, map[string]any{}, nil)
	t.Cleanup(sources.SetBases(srv.URL))
	src, _ := sources.Get("flights")
	if _, err := src.Fetch(context.Background(), sources.Ctx{Params: map[string]any{"airport": "MUC"}}); err == nil {
		t.Fatal("missing key accepted")
	}
}

// TestRatesChange: with "change" the source reads the last week's series
// and gives each rate its change against the previous day; every rate
// also carries its inverse.
func TestRatesChange(t *testing.T) {
	srv := jsonServer(t, map[string]any{
		"/v1/latest": map[string]any{"base": "EUR", "date": "2026-09-25", "rates": map[string]any{"USD": 1.1}},
		"/v1/2026-09-18..": map[string]any{"base": "EUR", "rates": map[string]any{
			"2026-09-24": map[string]any{"USD": 1.0}, "2026-09-25": map[string]any{"USD": 1.1}}},
	}, nil)
	t.Cleanup(sources.SetFrankfurter(srv.URL + "/v1"))
	r := fetch(t, "exchange_rates", map[string]any{"base": "EUR", "symbols": []string{"USD"}, "change": true, "today": "2026-09-25"}).(*sources.RatesResult)
	if len(r.Rates) != 1 || r.Rates[0].Change < 9.9 || r.Rates[0].Change > 10.1 || r.Rates[0].Inverse < 0.9 || r.Rates[0].Inverse > 0.91 {
		t.Fatalf("rates: %+v", r.Rates)
	}
}

// TestCryptoSpark: with "spark" the source reads CoinGecko's markets with
// the 7-day line.
func TestCryptoSpark(t *testing.T) {
	srv := jsonServer(t, map[string]any{
		"/api/v3/coins/markets": []any{map[string]any{"id": "bitcoin", "current_price": 50000.0, "price_change_percentage_24h": 1.5,
			"sparkline_in_7d": map[string]any{"price": []any{48000.0, 49000.0, 50000.0}}}},
	}, nil)
	t.Cleanup(sources.SetBases(srv.URL))
	c := fetch(t, "crypto", map[string]any{"coins": []string{"bitcoin"}, "currency": "EUR", "spark": true}).(*sources.CryptoResult)
	if len(c.Coins) != 1 || c.Coins[0].Price != 50000 || c.Coins[0].Change != 1.5 || len(c.Coins[0].Spark) != 3 {
		t.Fatalf("crypto: %+v", c)
	}
}
