package rules_test

import (
	"testing"
	"time"

	"andon/internal/enums"
	"andon/internal/i18n"
	"andon/internal/rules"
	"andon/internal/sources"
)

func day(s string) time.Time {
	t, err := time.Parse("2006-01-02", s)
	if err != nil {
		panic(err)
	}
	return t
}

func run(t *testing.T, id string, data any, env rules.Env) []rules.Finding {
	t.Helper()
	for _, spec := range rules.AllRules() {
		if spec.ID == id {
			cfg := rules.Config(spec, env.Settings)
			return spec.Run(data, cfg, env)
		}
	}
	t.Fatalf("rule not registered: %s", id)
	return nil
}

func TestKimaiTimerRunningLong(t *testing.T) {
	data := &sources.KimaiDataset{
		Active: []sources.KimaiSheet{{ID: 1, Begin: time.Now().UTC().Add(-11 * time.Hour).Format(time.RFC3339)}},
	}
	found := run(t, "kimai.timer_running_long", data, rules.Env{Today: day("2026-03-01")})
	if len(found) != 1 || found[0].Severity != enums.SeverityWarn {
		t.Fatalf("expected 1 warning, got %+v", found)
	}
}

func TestKimaiBudgetBurnCritical(t *testing.T) {
	data := &sources.KimaiDataset{
		Projects: []sources.KimaiProject{{ID: 1, Name: "Website", Budget: 1000, UsedMoney: 1100}},
	}
	found := run(t, "kimai.budget_burn", data, rules.Env{Today: day("2026-03-01"), Settings: map[string]any{}})
	if len(found) != 1 || found[0].Severity != enums.SeverityCritical {
		t.Fatalf("expected 1 critical finding, got %+v", found)
	}
}

func TestKimaiBudgetBurnNoFindingBelowThreshold(t *testing.T) {
	data := &sources.KimaiDataset{
		Projects: []sources.KimaiProject{{ID: 1, Name: "Website", Budget: 1000, UsedMoney: 100}},
	}
	found := run(t, "kimai.budget_burn", data, rules.Env{Today: day("2026-03-01"), Settings: map[string]any{}})
	if len(found) != 0 {
		t.Fatalf("expected no findings, got %+v", found)
	}
}

func TestKimaiUnbilledHoursConfigOverride(t *testing.T) {
	data := &sources.KimaiDataset{
		Timesheets: []sources.KimaiSheet{
			{ID: 1, Begin: "2026-01-01T09:00:00", End: "2026-01-01T11:00:00", Minutes: 120, Rate: 200,
				Billable: true, CustomerID: 1},
		},
		Customers: []sources.KimaiCustomer{{ID: 1, Name: "Acme"}},
	}
	env := rules.Env{Today: day("2026-01-20"), Settings: map[string]any{}} // only 19 days old
	if found := run(t, "kimai.unbilled_hours", data, env); len(found) != 0 {
		t.Fatalf("expected no finding at default warn_days=30, got %+v", found)
	}

	// Override warn_days down to 10 via space settings.
	env.Settings = map[string]any{"rules": map[string]any{
		"kimai.unbilled_hours": map[string]any{"warn_days": 10.0},
	}}
	found := run(t, "kimai.unbilled_hours", data, env)
	if len(found) != 1 || found[0].Params["customer"] != "Acme" {
		t.Fatalf("expected 1 finding with overridden warn_days, got %+v", found)
	}

	// Unbilled time is billed on Andon's billing page, not in Kimai.
	if found[0].ActionURL != "/billing#drafts" || found[0].ActionLabel != "open_drafts" {
		t.Fatalf("unbilled hint leads to %q (%q), want /billing#drafts", found[0].ActionURL, found[0].ActionLabel)
	}
}

func TestNinjaInvoiceOverdueDunning(t *testing.T) {
	data := &sources.NinjaDataset{
		Invoices: []sources.NinjaInvoice{
			{ID: 1, ClientID: 1, Status: "sent", DueDate: "2026-01-01", Balance: 500},
		},
		Clients: []sources.NinjaClient{{ID: 1, Name: "Acme"}},
	}
	env := rules.Env{Today: day("2026-02-01"), Settings: map[string]any{}} // 31 days overdue
	found := run(t, "in.invoice_overdue", data, env)
	if len(found) != 1 || found[0].Message != "in.overdue_dunning" || found[0].Severity != enums.SeverityCritical {
		t.Fatalf("expected dunning finding, got %+v", found)
	}
}

func TestNinjaMissingVATDomestic(t *testing.T) {
	data := &sources.NinjaDataset{
		HomeCountryID: "276",
		Invoices: []sources.NinjaInvoice{
			{ID: 1, ClientID: 1, Status: "paid", Date: "2026-01-01", Taxes: 0},
		},
		Clients: []sources.NinjaClient{{ID: 1, Name: "Acme", CountryID: "276"}},
	}
	found := run(t, "in.missing_vat", data, rules.Env{Today: day("2026-01-10"), Settings: map[string]any{}})
	if len(found) != 1 || found[0].Message != "in.missing_vat_domestic" {
		t.Fatalf("expected domestic missing-VAT finding, got %+v", found)
	}
}

func TestNinjaMissingVATEUWithVATNumberIsFine(t *testing.T) {
	data := &sources.NinjaDataset{
		HomeCountryID: "276",
		Invoices: []sources.NinjaInvoice{
			{ID: 1, ClientID: 1, Status: "paid", Date: "2026-01-01", Taxes: 0},
		},
		Clients: []sources.NinjaClient{{ID: 1, Name: "Acme FR", CountryID: "250", VATNumber: "FR123"}},
	}
	found := run(t, "in.missing_vat", data, rules.Env{Today: day("2026-01-10"), Settings: map[string]any{}})
	if len(found) != 0 {
		t.Fatalf("expected no finding for EU client with VAT number, got %+v", found)
	}
}

func TestSnipeGWGHintNetOfVAT(t *testing.T) {
	data := &sources.SnipeDataset{
		Assets: []sources.SnipeAsset{{ID: 1, Name: "Server", PurchaseDate: "2026-02-01", PurchaseCost: 1200}},
	}
	// 1200 gross / 1.19 ≈ 1008.4 net, above the 800 GWG limit.
	found := run(t, "inventory.gwg_hint", data, rules.Env{Today: day("2026-03-01"), Settings: map[string]any{}})
	if len(found) != 1 {
		t.Fatalf("expected 1 GWG hint, got %+v", found)
	}
}

func TestSnipeWarrantyExpiringSeverityByThreshold(t *testing.T) {
	data := &sources.SnipeDataset{
		Assets: []sources.SnipeAsset{
			{ID: 1, Name: "Soon", Tag: "A1", WarrantyExpires: "2026-03-10"},  // 9 days: warn
			{ID: 2, Name: "Later", Tag: "A2", WarrantyExpires: "2026-04-20"}, // 50 days: info
		},
	}
	found := run(t, "inventory.warranty_expiring", data, rules.Env{Today: day("2026-03-01"), Settings: map[string]any{}})
	if len(found) != 2 {
		t.Fatalf("expected 2 findings, got %d", len(found))
	}
	bySeverity := map[enums.Severity]int{}
	for _, f := range found {
		bySeverity[f.Severity]++
	}
	if bySeverity[enums.SeverityWarn] != 1 || bySeverity[enums.SeverityInfo] != 1 {
		t.Fatalf("expected one warn and one info, got %+v", bySeverity)
	}
}

func TestCrossVisitWithoutTime(t *testing.T) {
	kimai := &sources.KimaiDataset{URL: "https://kimai.example", Customers: []sources.KimaiCustomer{{ID: 1, Name: "Acme"}}}
	geo := &sources.DawarichDataset{
		Areas:  []sources.DawarichArea{{ID: 1, Name: "Acme Office", Lat: 52.5, Lon: 13.4, Radius: 100}},
		Visits: []sources.DawarichVisit{{ID: 1, AreaID: 1, Start: "2026-02-20T09:00:00Z", End: "2026-02-20T12:00:00Z", Minutes: 180}},
	}
	env := rules.Env{
		Today: day("2026-03-01"), Settings: map[string]any{},
		Datasets: map[string]any{"kimai": kimai, "dawarich": geo},
		Options: map[string]map[string]any{
			"dawarich": {"areas": map[string]any{"Acme Office": map[string]any{"customer_id": float64(1)}}},
		},
	}
	found := run(t, "geo.visit_without_time", nil, env)
	if len(found) != 1 || found[0].Params["customer"] != "Acme" {
		t.Fatalf("expected 1 unbooked-visit finding, got %+v", found)
	}
}

func TestCrossVisitWithoutTimeSkippedWhenBooked(t *testing.T) {
	kimai := &sources.KimaiDataset{
		URL:       "https://kimai.example",
		Customers: []sources.KimaiCustomer{{ID: 1, Name: "Acme"}},
		Timesheets: []sources.KimaiSheet{
			{ID: 1, Begin: "2026-02-20T09:00:00", CustomerID: 1},
		},
	}
	geo := &sources.DawarichDataset{
		Areas:  []sources.DawarichArea{{ID: 1, Name: "Acme Office", Lat: 52.5, Lon: 13.4, Radius: 100}},
		Visits: []sources.DawarichVisit{{ID: 1, AreaID: 1, Start: "2026-02-20T09:00:00Z", End: "2026-02-20T12:00:00Z", Minutes: 180}},
	}
	env := rules.Env{
		Today: day("2026-03-01"), Settings: map[string]any{},
		Datasets: map[string]any{"kimai": kimai, "dawarich": geo},
		Options: map[string]map[string]any{
			"dawarich": {"areas": map[string]any{"Acme Office": map[string]any{"customer_id": float64(1)}}},
		},
	}
	found := run(t, "geo.visit_without_time", nil, env)
	if len(found) != 0 {
		t.Fatalf("expected no finding once the day is booked, got %+v", found)
	}
}

func TestTaxDeadlinesVATReturnAmount(t *testing.T) {
	ninja := &sources.NinjaDataset{
		Invoices: []sources.NinjaInvoice{
			{ID: 1, ClientID: 1, Status: "paid", Date: "2026-02-15", Taxes: 190, Amount: 1190},
		},
		Payments: []sources.NinjaPayment{{ID: 1, ClientID: 1, Date: "2026-02-20", Amount: 1190}},
	}
	env := rules.Env{
		Today: day("2026-03-01"),
		Settings: map[string]any{"tax": map[string]any{
			"vat": map[string]any{"return_interval": "monthly", "method": "soll"},
		}},
		Datasets: map[string]any{"invoiceninja": ninja},
	}
	found := run(t, "tax.deadlines", nil, env)
	if len(found) == 0 {
		t.Fatalf("expected at least one deadline, got none")
	}
	hasAmount := false
	for _, f := range found {
		if f.Message == "tax.vat_return_amount" {
			hasAmount = true
			if f.Params["amount"] == nil {
				t.Fatalf("expected amount param, got %+v", f.Params)
			}
		}
	}
	if !hasAmount {
		t.Fatalf("expected a vat_return_amount finding, got %+v", found)
	}
}

func TestConfigOnlyOverridesKnownKeys(t *testing.T) {
	spec := rules.Spec{ID: "x", Defaults: map[string]any{"a": 1.0}}
	cfg := rules.Config(spec, map[string]any{"rules": map[string]any{
		"x": map[string]any{"a": 2.0, "unknown": "nope"},
	}})
	if cfg["a"] != 2.0 {
		t.Fatalf("expected override to apply, got %+v", cfg)
	}
	if _, ok := cfg["unknown"]; ok {
		t.Fatalf("expected unknown key to be dropped, got %+v", cfg)
	}
}

func TestEveryRuleHasLabels(t *testing.T) {
	for _, spec := range rules.AllRules() {
		for _, locale := range []enums.Locale{enums.LocaleDE, enums.LocaleEN} {
			if key := "rule_name." + spec.ID; i18n.T(key, locale, nil) == key {
				t.Errorf("%s: missing %s", locale, key)
			}
			for param := range spec.Defaults {
				if key := "param." + param; param != rules.Enabled && i18n.T(key, locale, nil) == key {
					t.Errorf("%s: missing %s", locale, key)
				}
			}
		}
	}
}

// TestNinjaDunningMinimum: an overdue invoice below the minimum amount is
// no critical reminder (0,50 € left open is no emergency).
func TestNinjaDunningMinimum(t *testing.T) {
	data := &sources.NinjaDataset{
		Invoices: []sources.NinjaInvoice{
			{ID: 1, ClientID: 1, Status: "sent", DueDate: "2026-01-01", Balance: 0.5},
			{ID: 2, ClientID: 1, Status: "sent", DueDate: "2026-01-01", Balance: 500},
		},
		Clients: []sources.NinjaClient{{ID: 1, Name: "Acme"}},
	}
	env := rules.Env{Today: day("2026-02-01"), Settings: map[string]any{}}
	levels := map[any]enums.Severity{}
	for _, f := range run(t, "in.invoice_overdue", data, env) {
		levels[f.Fingerprint] = f.Severity
	}
	if levels["overdue:1"] != enums.SeverityInfo || levels["overdue:2"] != enums.SeverityCritical {
		t.Fatalf("levels: %+v", levels)
	}
}

// TestRegisterRejectsDuplicates: a second rule with the same id must not
// silently replace the first.
func TestRegisterRejectsDuplicates(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("expected panic for a duplicate rule id")
		}
	}()
	rules.Register("kimai.timer_running_long", "kimai", nil, nil)
}

// A host whose clock is off by more than a minute breaks TOTP,
// certificates and backup schedules; a few seconds are normal.
func TestClockSkew(t *testing.T) {
	env := rules.Env{Datasets: map[string]any{rules.ClockDataset: []rules.Clock{
		{Name: "NAS", Host: "nas.lan", Seconds: -300}, {Name: "Kimai", Host: "kimai.lan", Seconds: 3},
	}}}
	found := run(t, "system.clock_skew", nil, env)
	if len(found) != 1 || found[0].Fingerprint != "clock:nas.lan" {
		t.Fatalf("findings: %+v", found)
	}
}

// TestNinjaOverdueBundled: from three overdue invoices on, a client gets
// one hint with count, sum and the oldest age instead of one per invoice.
func TestNinjaOverdueBundled(t *testing.T) {
	data := &sources.NinjaDataset{
		Invoices: []sources.NinjaInvoice{
			{ID: 1, Number: "R1", ClientID: 1, Status: "sent", DueDate: "2026-01-01", Balance: 100},
			{ID: 2, Number: "R2", ClientID: 1, Status: "sent", DueDate: "2026-01-20", Balance: 200},
			{ID: 3, Number: "R3", ClientID: 1, Status: "sent", DueDate: "2026-01-25", Balance: 300},
			{ID: 4, Number: "R4", ClientID: 2, Status: "sent", DueDate: "2026-01-25", Balance: 50},
		},
		Clients: []sources.NinjaClient{{ID: 1, Name: "Nivre"}, {ID: 2, Name: "Acme"}},
	}
	env := rules.Env{Today: day("2026-02-01"), Settings: map[string]any{}}
	byPrint := map[string]rules.Finding{}
	for _, f := range run(t, "in.invoice_overdue", data, env) {
		byPrint[f.Fingerprint] = f
	}
	bundle, ok := byPrint["overdue_client:1"]
	if len(byPrint) != 2 || !ok || byPrint["overdue:4"].Rule == "" {
		t.Fatalf("findings: %+v", byPrint)
	}
	if bundle.Message != "in.overdue_client" || bundle.Severity != enums.SeverityCritical ||
		bundle.Params["count"] != 3 || bundle.Params["days"] != 31 || bundle.Params["numbers"] != "R1, R2, R3" {
		t.Fatalf("bundle: %+v", bundle)
	}
}

// A vendor abroad without tax is reverse charge (§ 13b), not missing
// input VAT: the hint says so and leads to the VAT check of its period.
func TestExpenseFromAbroadIsReverseCharge(t *testing.T) {
	data := &sources.NinjaDataset{
		HomeCountryID: "276",
		Expenses: []sources.NinjaExpense{
			{ID: 7, Number: "AU-0101", Date: "2026-08-28", Amount: 145.24, VendorKey: "zsa"},
			{ID: 8, Number: "AU-0102", Date: "2026-09-02", Amount: 80, VendorKey: "eu"},
			{ID: 9, Number: "AU-0103", Date: "2026-09-03", Amount: 90, VendorKey: "home"}},
		Vendors: []sources.NinjaVendor{{Key: "zsa", Name: "ZSA", CountryID: "158"}, {Key: "eu", Name: "Hostinger", CountryID: "440"},
			{Key: "home", Name: "Hetzner", CountryID: "276"}},
	}
	quarterly := map[string]any{"tax": map[string]any{"vat": map[string]any{"return_interval": "quarterly"}}}
	found := run(t, "in.expense_no_input_vat", data, rules.Env{Today: day("2026-09-28"), Settings: quarterly})
	byNumber := map[any]rules.Finding{}
	for _, f := range found {
		byNumber[f.Params["number"]] = f
	}
	if len(found) != 3 {
		t.Fatalf("found: %+v", found)
	}
	if f := byNumber["AU-0101"]; f.Message != "in.expense_reverse_charge" || f.Params["kz"] != "84/85" || f.ActionURL != "/vat?span=2026-Q3#elster" {
		t.Errorf("outside the EU: %+v", f)
	}
	if f := byNumber["AU-0102"]; f.Message != "in.expense_reverse_charge" || f.Params["kz"] != "46/47" {
		t.Errorf("EU: %+v", f)
	}
	if f := byNumber["AU-0103"]; f.Message != "in.expense_no_vat" {
		t.Errorf("home vendor: %+v", f)
	}
}

// An expense without input VAT names its vendor and number, so it can be
// found without opening Invoice Ninja.
func TestExpenseNoVATNamesVendor(t *testing.T) {
	data := &sources.NinjaDataset{
		Expenses: []sources.NinjaExpense{{ID: 7, Number: "A-0042", Date: "2026-09-20", Amount: 297.5, VendorKey: "v1"}},
		Vendors:  []sources.NinjaVendor{{Key: "v1", Name: "Hetzner"}},
	}
	found := run(t, "in.expense_no_input_vat", data, rules.Env{Today: day("2026-09-28"), Settings: map[string]any{}})
	if len(found) != 1 || found[0].Params["vendor"] != "Hetzner" || found[0].Params["number"] != "A-0042" {
		t.Fatalf("found: %+v", found)
	}
}

// Links open the entry in Invoice Ninja 5: its own path (no "#/", which
// lands on the dashboard) and its hashed key, not a number.
func TestNinjaLinksUseKeys(t *testing.T) {
	data := &sources.NinjaDataset{URL: "https://in.example/",
		Expenses: []sources.NinjaExpense{{ID: 7, Key: "eX7", Number: "A-1", Date: "2026-09-20", Amount: 297.5}},
		Invoices: []sources.NinjaInvoice{{ID: 9, Key: "iN9", Number: "R-9", Status: "draft", Date: "2026-08-01", Amount: 100, Balance: 100}},
	}
	env := rules.Env{Today: day("2026-09-28"), Settings: map[string]any{}}
	if f := run(t, "in.expense_no_input_vat", data, env); len(f) != 1 || f[0].ActionURL != "https://in.example/expenses/eX7/edit" {
		t.Errorf("expense link: %+v", f)
	}
	if f := run(t, "in.draft_stale", data, env); len(f) != 1 || f[0].ActionURL != "https://in.example/invoices/iN9/edit" {
		t.Errorf("invoice link: %+v", f)
	}
}

// A connection runs its service's rules and its category's: Firefly the
// bank rules, Sure also its own sync check.
func TestForServiceAddsCategory(t *testing.T) {
	ids := func(service string) map[string]bool {
		out := map[string]bool{}
		for _, s := range rules.ForService(service) {
			out[s.ID] = true
		}
		return out
	}
	firefly, sure, snipe := ids("firefly"), ids("sure"), ids("snipeit")
	if !firefly["bank.low_balance"] || firefly["sure.sync_failed"] {
		t.Errorf("firefly runs %v", firefly)
	}
	if !sure["bank.low_balance"] || !sure["sure.sync_failed"] {
		t.Errorf("sure runs %v", sure)
	}
	if !snipe["inventory.warranty_expiring"] || !snipe["snipe.license_seats"] {
		t.Errorf("snipeit runs %v", snipe)
	}
}

// kept.due: Kept's agenda states become hints with their due date;
// upcoming items and returns (not chosen by default) stay out.
func TestKeptDue(t *testing.T) {
	data := &sources.KeptDataset{URL: "https://kept.lan", Agenda: []sources.KeptDue{
		{Kind: "thing_expiry", State: "expiring", Due: "2026-11-20", Title: "Feuerlöscher", Subject: "Feuerlöscher", Code: "FL0SCH"},
		{Kind: "schedule", State: "overdue", Due: "2026-10-20", Title: "Filter", Subject: "NAS", Code: "N4S2HB"},
		{Kind: "warranty", State: "upcoming", Due: "2027-05-01", Title: "Garantie", Subject: "Monitor"},
		{Kind: "loan", State: "due", Due: "2026-10-24", Title: "Rückgabe", Subject: "Funkstrecke"},
		{Kind: "document", State: "due", Due: "2026-10-30", Title: "Hausrat", Subject: "Studio"},
	}}
	got := run(t, "kept.due", data, rules.Env{Today: day("2026-10-24"), Settings: map[string]any{}})
	if len(got) != 3 {
		t.Fatalf("findings %+v", got)
	}
	ext, nas, doc := got[0], got[1], got[2]
	// Warranties are inventory.warranty_expiring's; kept.due leaves them out.
	data.Agenda[2].State = "expiring"
	if again := run(t, "kept.due", data, rules.Env{Today: day("2026-10-24"), Settings: map[string]any{}}); len(again) != 3 {
		t.Errorf("warranty counted twice: %+v", again)
	}
	if ext.Due != "2026-11-20" || ext.Severity != enums.SeverityInfo || ext.ActionURL != "https://kept.lan/t/FL0SCH" || ext.Message != "kept.due_expiring" {
		t.Errorf("extinguisher %+v", ext)
	}
	if nas.Severity != enums.SeverityCritical || doc.Severity != enums.SeverityWarn || doc.ActionURL != "https://kept.lan/expiring" {
		t.Errorf("nas %+v doc %+v", nas, doc)
	}
}

// inventory.split: Kept counts; a bought NAS only Snipe-IT knows is named,
// one in both (by serial) is not.
func TestInventorySplit(t *testing.T) {
	kept := &sources.KeptDataset{Things: []sources.KeptThing{{ID: "a", Name: "Laptop", Serial: "SN1", Bought: "2026-01-02", Price: 1200}}}
	snipe := &sources.SnipeDataset{Assets: []sources.SnipeAsset{
		{ID: 1, Name: "Schnitt-Laptop", Serial: "sn1", PurchaseDate: "2026-01-02", PurchaseCost: 1200},
		{ID: 2, Name: "NAS", PurchaseDate: "2024-05-01", PurchaseCost: 650},
		{ID: 3, Name: "Kabel"},
	}}
	env := rules.Env{Today: day("2026-10-24"), Settings: map[string]any{}, Datasets: map[string]any{"kept": kept, "snipeit": snipe}}
	got := run(t, "inventory.split", nil, env)
	if len(got) != 1 || got[0].Params["count"] != 1 || got[0].Params["names"] != "NAS" {
		t.Fatalf("split %+v", got)
	}
	// The same laptop, serial only in Kept: matched by name.
	snipe.Assets[0].Serial, snipe.Assets[0].Name = "", "Laptop"
	if got := run(t, "inventory.split", nil, env); len(got) != 1 || got[0].Params["count"] != 1 {
		t.Errorf("serial on one side only: %+v", got)
	}

	env.Datasets = map[string]any{"snipeit": snipe}
	if got := run(t, "inventory.split", nil, env); len(got) != 0 {
		t.Errorf("one inventory: %+v", got)
	}
}

// inventory.receipt_missing: a recent Kept purchase without receipt or
// invoice; Snipe-IT keeps no receipts and says nothing.
func TestReceiptMissing(t *testing.T) {
	kept := &sources.KeptDataset{URL: "https://kept.lan", Things: []sources.KeptThing{
		{ID: "a", Code: "AAAAAA", Name: "Monitor", Bought: "2026-08-25", Price: 890},
		{ID: "b", Name: "Laptop", Bought: "2026-09-24", Price: 1200, Receipts: []sources.KeptReceipt{{Role: "invoice", DocID: 4}}},
		{ID: "c", Name: "Kabel", Bought: "2026-09-24", Price: 9},
		{ID: "d", Name: "Feuerlöscher"},
	}}
	env := rules.Env{Today: day("2026-10-24"), Settings: map[string]any{}}
	got := run(t, "inventory.receipt_missing", kept, env)
	if len(got) != 1 || got[0].Params["asset"] != "Monitor" || got[0].ActionURL != "https://kept.lan/t/AAAAAA" {
		t.Fatalf("receipts %+v", got)
	}
	snipe := &sources.SnipeDataset{Assets: []sources.SnipeAsset{{ID: 1, Name: "Monitor", PurchaseDate: "2026-08-25", PurchaseCost: 890}}}
	if got := run(t, "inventory.receipt_missing", snipe, env); len(got) != 0 {
		t.Errorf("snipe %+v", got)
	}
}

// cross.kept_paperless: a link into another Paperless, a document gone,
// a location without Paperless; a link to an existing one is fine.
func TestKeptPaperless(t *testing.T) {
	kept := &sources.KeptDataset{Things: []sources.KeptThing{
		{Name: "Laptop", Bought: "2026-01-01", Receipts: []sources.KeptReceipt{{Role: "invoice", DocID: 4, Paperless: "https://docs.lan"}}},
		{Name: "Phone", Bought: "2026-01-01", Receipts: []sources.KeptReceipt{{Role: "invoice", DocID: 999, Paperless: "https://DOCS.lan/"}}},
		{Name: "Drucker", Bought: "2026-01-01", Receipts: []sources.KeptReceipt{{Role: "invoice", DocID: 4, Paperless: "https://old.lan"}}},
		{Name: "Sofa", Location: "Wohnung", Bought: "2026-01-01", PaperlessMissing: true},
	}}
	paperless := &sources.PaperlessDataset{URL: "https://docs.lan", IDs: []int64{4, 5}}
	env := rules.Env{Today: day("2026-10-24"), Settings: map[string]any{}, Datasets: map[string]any{"kept": kept, "paperless": paperless}}
	got := run(t, "cross.kept_paperless", nil, env)
	msgs := map[string]any{}
	for _, f := range got {
		msgs[f.Message] = f.Params["names"]
	}
	if len(got) != 3 || msgs["cross.kept_paperless_gone"] != "Phone" || msgs["cross.kept_paperless_elsewhere"] != "Drucker" || msgs["cross.kept_paperless_off"] != "Wohnung" {
		t.Fatalf("paperless %+v", got)
	}
	paperless.IDs = nil
	if got := run(t, "cross.kept_paperless", nil, env); len(got) != 2 {
		t.Errorf("unknown ids: %+v", got)
	}
}

// inventory.expense_missing with Kept: the expense found for a purchase
// links another Paperless document than Kept's receipt; the same
// document, or no link on one side, says nothing.
func TestExpenseReceiptDiffers(t *testing.T) {
	kept := &sources.KeptDataset{URL: "https://kept.lan", Things: []sources.KeptThing{
		{ID: "a", Code: "AAAAAA", Name: "Monitor", Bought: "2026-08-25", Price: 890, Receipts: []sources.KeptReceipt{{Role: "invoice", DocID: 4}}},
		{ID: "b", Name: "Laptop", Bought: "2026-09-01", Price: 1200, Receipts: []sources.KeptReceipt{{Role: "receipt", DocID: 7}}},
		{ID: "c", Name: "Drucker", Bought: "2026-09-10", Price: 300, Receipts: []sources.KeptReceipt{{Role: "manual", DocID: 9}}},
	}}
	ninja := &sources.NinjaDataset{Expenses: []sources.NinjaExpense{
		{Number: "EX-0040", Date: "2026-08-26", Amount: 890, Custom: [sources.NinjaSlots]string{"", "https://docs.lan/documents/5/"}},
		{Number: "EX-0041", Date: "2026-09-01", Amount: 1200, Custom: [sources.NinjaSlots]string{"", "https://docs.lan/documents/3/ https://docs.lan/documents/7/"}},
		{Number: "EX-0042", Date: "2026-09-10", Amount: 300, Custom: [sources.NinjaSlots]string{"", "https://docs.lan/documents/8/"}},
	}}
	env := rules.Env{Today: day("2026-10-24"), Settings: map[string]any{}, Datasets: map[string]any{"kept": kept, "invoiceninja": ninja},
		Options: map[string]map[string]any{"invoiceninja": {"receipt_link_slot": 2.0}}}
	got := run(t, "inventory.expense_missing", nil, env)
	if len(got) != 1 || got[0].Message != "inventory.receipt_differs" || got[0].Params["asset"] != "Monitor" ||
		got[0].Params["number"] != "EX-0040" || got[0].ActionURL != "https://kept.lan/t/AAAAAA" {
		t.Fatalf("findings %+v", got)
	}

	// Without the link field nothing tells which document an expense has.
	env.Options = nil
	if got := run(t, "inventory.expense_missing", nil, env); len(got) != 0 {
		t.Errorf("no link field: %+v", got)
	}
}
