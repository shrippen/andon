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
	"context"
	"net/url"
	"time"

	"andon/internal/drivers/services"
	"andon/internal/enums"
)

// WallosSub is one subscription.
type WallosSub struct {
	Name, Next, Category, Payment, URL string
	Price, Monthly                     float64
	Inactive                           bool
}

// WallosDataset is every subscription of the key's user.
type WallosDataset struct {
	URL      string
	Currency string // main currency code, e.g. "EUR"
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

type WallosData struct{}

func (WallosData) Key() string                { return "wallos.data" }
func (WallosData) TTL() time.Duration         { return dataTTL }
func (WallosData) Service() enums.ServiceType { return enums.ServiceWallos }

func (WallosData) Fetch(ctx context.Context, sctx Ctx) (any, error) {
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
		for _, raw := range asList(asMap(cur)["currencies"]) {
			if c := asMap(raw); asInt64(c["id"]) == main {
				data.Currency = asStr(c["code"])
			}
		}
	}
	for _, raw := range asList(asMap(subs)["subscriptions"]) {
		s := asMap(raw)
		price := asFloat(s["price"])
		data.Subs = append(data.Subs, WallosSub{
			Name: asStr(s["name"]), Next: asStr(s["next_payment"]), Category: asStr(s["category_name"]),
			Payment: asStr(s["payment_method_name"]), URL: asStr(s["url"]), Price: price,
			Monthly:  wallosMonthly(price, int(asInt64(s["cycle"])), int(asInt64(s["frequency"]))),
			Inactive: asInt64(s["inactive"]) != 0,
		})
	}
	return data, nil
}

// DemoWallos is a small set of subscriptions; Sure's demo "Adobe Creative
// Cloud" is missing on purpose, for the cross.wallos_missing hint.
func DemoWallos(now time.Time) *WallosDataset {
	next := func(days int) string { return now.AddDate(0, 0, days).Format(time.DateOnly) }
	return &WallosDataset{URL: "https://wallos.demo", Currency: "EUR", Subs: []WallosSub{
		{Name: "Hetzner", Price: 38.2, Monthly: 38.2, Next: next(4), Category: "Hosting"},
		{Name: "Tibber", Price: 72, Monthly: 72, Next: next(18), Category: "Energie"},
		{Name: "Domain arianw.de", Price: 24, Monthly: 2, Next: next(150), Category: "Hosting"},
		{Name: "Spotify", Price: 10.99, Monthly: 10.99, Next: next(9), Category: "Unterhaltung"},
	}}
}

func init() {
	Register(WallosData{})
	Register(testOf{WallosData{}, func(d any) map[string]any { return map[string]any{"subscriptions": len(d.(*WallosDataset).Subs)} }})
}
