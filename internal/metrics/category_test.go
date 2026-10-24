package metrics

import (
	"testing"

	"andon/internal/enums"
	"andon/internal/sources"
)

// The first member present counts; a nil dataset is none.
func TestCategoryPick(t *testing.T) {
	sure := &sources.BankData{URL: "sure"}
	firefly := &sources.BankData{URL: "firefly", Service: enums.ServiceFirefly}
	if got, ok := BankOf(map[string]any{"firefly": firefly, "sure": sure}); !ok || got != sure {
		t.Errorf("BankOf both = %v, want Sure's", got)
	}
	if got, ok := BankOf(map[string]any{"sure": (*sources.BankData)(nil), "firefly": firefly}); !ok || got != firefly {
		t.Errorf("BankOf nil Sure = %v, want Firefly's", got)
	}
	if _, ok := BankOf(map[string]any{"kimai": sure}); ok {
		t.Error("BankOf without a member found one")
	}

	snipe := &sources.SnipeDataset{URL: "https://snipe.lan/", Assets: []sources.SnipeAsset{{ID: 12, Name: "NAS", DepMonths: 36}}}
	inv, ok := InventoryOf(map[string]any{string(enums.ServiceSnipeIT): snipe})
	if !ok || len(inv.Assets) != 1 {
		t.Fatalf("InventoryOf = %v, %v", inv, ok)
	}
	if a := inv.Assets[0]; a.ID != "12" || a.Link != "https://snipe.lan/hardware/12" || a.DepMonths != 36 || inv.Service != enums.ServiceSnipeIT {
		t.Errorf("asset = %+v (service %s)", a, inv.Service)
	}
}
