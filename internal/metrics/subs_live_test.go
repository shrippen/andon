package metrics_test

import (
	"context"
	"testing"
	"time"

	"andon/internal/enums"
	"andon/internal/metrics"
	"andon/internal/sources"
	"andon/internal/testkit/live"
)

// The subscription cases from the real Sure, Wallos and Invoice Ninja:
// the parsers and the matching agree with what the services send. Only
// reads; the cases are logged for a look.
func TestSubCasesLive(t *testing.T) {
	in := metrics.SubInputs{Today: time.Now()}
	for _, inst := range live.Instances(t) {
		svc := enums.ServiceType(inst.Service)
		if svc != enums.ServiceSure && svc != enums.ServiceWallos && svc != enums.ServiceInvoiceNinja {
			continue
		}
		if inst.Err != nil {
			t.Fatalf("%s login: %v", svc, inst.Err)
		}
		src, err := sources.Get(sources.DataKey(svc))
		if err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		out, err := src.Fetch(ctx, inst.Ctx)
		cancel()
		if err != nil {
			t.Fatalf("%s: %v", inst.Name, err)
		}
		switch d := out.(type) {
		case *sources.SureDataset:
			in.Bank = d
		case *sources.WallosDataset:
			in.Wallos = d
		case *sources.NinjaDataset:
			in.Ninja = d
		}
	}
	if in.Bank == nil {
		t.Skip("no live Sure instance")
	}

	t.Logf("Sure recurring: %d, Wallos: %v, Ninja recurring expenses: %v", len(in.Bank.Recurring), in.Wallos != nil, in.Ninja != nil)
	if in.Wallos != nil {
		for _, s := range in.Wallos.Subs {
			if s.ID == 0 || s.CurrencyID == 0 {
				t.Logf("wallos sub without id or currency: %+v", s)
			}
		}
	}
	if in.Ninja != nil {
		t.Logf("ninja recurring expenses: %+v", in.Ninja.RecurringExpenses)
	}
	for _, c := range metrics.SubCases(in) {
		t.Logf("%-14s %-30s amount %.2f old %.2f months %d next %s keep %q", c.Action, c.Name, c.Amount, c.Old, c.Months, c.Next, c.Keep)
	}
}
