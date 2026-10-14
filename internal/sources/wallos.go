package sources

// Wallos tracks subscriptions. Andon reads them with prices converted to
// the main currency and works out the monthly price the way Wallos does:
//
//	cycle 1 day   price × 30 / frequency
//	cycle 2 week  price × 4.35 / frequency
//	cycle 3 month price / frequency
//	cycle 4 year  price / (12 × frequency)
//	cycle 5 once  0

import (
	"andon/internal/caps"
	"context"
	"net/url"
	"time"

	"andon/internal/drivers/services"
	"andon/internal/enums"
	"andon/internal/sources/demoworld"
)

// WallosSub is one subscription.
type WallosSub struct {
	Name, Next, Category, Payment, URL string
	Price, Monthly                     float64
	Inactive                           bool

	// For writes: the id, the price's currency and the cycle (cycle 3,
	// frequency 2 = every two months).
	ID, CurrencyID   int64
	Cycle, Frequency int
}

// WallosDataset is every subscription of the key's user.
type WallosDataset struct {
	URL      string
	Currency string // main currency code, e.g. "EUR"
	Main     int64  // its id; a price in another currency is converted
	Subs     []WallosSub
}

// Wallos' billing cycles.
const (
	wallosDay   = 1
	wallosWeek  = 2
	wallosMonth = 3
	wallosYear  = 4

	daysPerMonth   = 30
	weeksPerMonth  = 4.35
	monthsPerYear  = 12
	wallosSubsPath = "api/subscriptions/get_subscriptions.php"
	wallosCurPath  = "api/currencies/get_currencies.php"
)

// wallosMonthly is Wallos' getPricePerMonth.
func wallosMonthly(price float64, cycle, frequency int) float64 {
	f := float64(max(frequency, 1))
	switch cycle {
	case wallosDay:
		return price * daysPerMonth / f
	case wallosWeek:
		return price * weeksPerMonth / f
	case wallosMonth:
		return price / f
	case wallosYear:
		return price / (monthsPerYear * f)
	}
	return 0
}

// WallosCycle is Wallos' cycle for a payment every months months (0:
// weekly): cycle and frequency, e.g. 3 → month, 3; 12 → year, 1.
func WallosCycle(months int) (cycle, frequency int) {
	switch {
	case months <= 0:
		return wallosWeek, 1
	case months%monthsPerYear == 0:
		return wallosYear, months / monthsPerYear
	}
	return wallosMonth, months
}

// Months is how many months lie between two payments; 0 for weekly or
// daily ones.
func (s WallosSub) Months() int {
	f := max(s.Frequency, 1)
	switch s.Cycle {
	case wallosMonth:
		return f
	case wallosYear:
		return f * monthsPerYear
	}
	return 0
}

// CapSet: Wallos's capabilities need nothing beyond the token.
func (d *WallosDataset) CapSet() caps.Set { return caps.Full(caps.HolderOf(enums.ServiceWallos)) }

var WallosData = source{key: "wallos.data", ttl: dataTTL, service: enums.ServiceWallos, fetch: fetchWallos}

func fetchWallos(ctx context.Context, sctx Ctx) (any, error) {
	if isDemo(sctx) {
		return DemoWallos(time.Now()), nil
	}
	secret, err := needSecret(sctx)
	if err != nil {
		return nil, err
	}
	api := services.WallosApi{URL: sctx.URL, Key: secret, Verify: sctx.VerifyTLS}
	subs, err := api.Get(ctx, wallosSubsPath, url.Values{"convert_currency": {"true"}})
	if err != nil {
		return nil, fetchError(err)
	}
	data := &WallosDataset{URL: sctx.URL}
	if cur, err := api.Get(ctx, wallosCurPath, nil); err == nil {
		main := asInt64(asMap(cur)["main_currency"])
		data.Main = main
		for _, raw := range asList(asMap(cur)["currencies"]) {
			if c := asMap(raw); asInt64(c["id"]) == main {
				data.Currency = asStr(c["code"])
			}
		}
	}
	for _, raw := range asList(asMap(subs)["subscriptions"]) {
		s := asMap(raw)
		price := asFloat(s["price"])
		cycle, frequency := int(asInt64(s["cycle"])), int(asInt64(s["frequency"]))
		data.Subs = append(data.Subs, WallosSub{
			ID: asInt64(s["id"]), CurrencyID: asInt64(s["currency_id"]), Cycle: cycle, Frequency: frequency,
			Name: asStr(s["name"]), Next: asStr(s["next_payment"]), Category: asStr(s["category_name"]),
			Payment: asStr(s["payment_method_name"]), URL: asStr(s["url"]), Price: price,
			Monthly:  wallosMonthly(price, cycle, frequency),
			Inactive: asInt64(s["inactive"]) != 0,
		})
	}
	return data, nil
}

// demoWallosCurrency is the demo's main currency id.
const demoWallosCurrency = 1

// DemoWallos is a small set of subscriptions; Sure's demo "Adobe Creative
// Cloud" is missing on purpose, for the cross.wallos_missing hint.
// DemoWallos is the demo Wallos dataset: the studio's suppliers from the
// demo world plus its domain; the software subscription in Sure is missing.
func DemoWallos(now time.Time) *WallosDataset {
	var data struct {
		URL, Currency string
		Subs          []struct {
			WallosSub
			Vendor string
		}
	}
	demoworld.MustDecode("subscriptions", now, &data)
	out := &WallosDataset{URL: data.URL, Currency: data.Currency, Main: demoWallosCurrency}
	for i, s := range data.Subs {
		if s.Vendor != "" {
			v := demoWorld.Vendor(s.Vendor)
			s.Name, s.Price, s.Monthly, s.Category = v.Name, v.Monthly, v.Monthly, v.Kind.DE()
		}
		s.ID, s.CurrencyID, s.Cycle, s.Frequency = int64(i+1), demoWallosCurrency, wallosMonth, 1
		if s.Monthly > 0 && s.Price/s.Monthly >= monthsPerYear {
			s.Cycle = wallosYear
		}
		out.Subs = append(out.Subs, s.WallosSub)
	}
	return out
}

func init() {
	Register(WallosData)
	Register(testOf{WallosData, func(d any) map[string]any { return map[string]any{"subscriptions": len(d.(*WallosDataset).Subs)} }})
}
