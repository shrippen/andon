package sources_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"andon/internal/sources"
)

func TestSnipeDataNormalizesAssets(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/hardware", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"total": 1, "rows": [{
			"id": 1, "name": "Laptop 1", "asset_tag": "A-001",
			"model": {"name": "MacBook Pro"}, "category": {"name": "Laptop"},
			"status_label": {"name": "Deployed", "status_meta": "deployable"},
			"assigned_to": {"id": 5}, "purchase_date": {"date": "2025-01-01 00:00:00"},
			"purchase_cost": "1,999.00"
		}]}`))
	})
	mux.HandleFunc("/api/v1/licenses", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"total": 0, "rows": []}`))
	})
	mux.HandleFunc("/api/v1/consumables", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"total": 0, "rows": []}`))
	})
	mux.HandleFunc("/api/v1/hardware/audit/overdue", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"total": 0, "rows": []}`))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	src := sources.SnipeData
	out, err := src.Fetch(context.Background(), sources.Ctx{URL: srv.URL, Secret: "tok", VerifyTLS: true})
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	data := out.(*sources.SnipeDataset)

	if len(data.Assets) != 1 {
		t.Fatalf("expected 1 asset, got %d", len(data.Assets))
	}
	a := data.Assets[0]
	if a.Model != "MacBook Pro" || !a.Deployable || !a.Assigned || a.PurchaseCost != 1999.0 {
		t.Fatalf("unexpected asset: %+v", a)
	}
	if a.PurchaseDate != "2025-01-01" {
		t.Fatalf("expected trimmed date, got %q", a.PurchaseDate)
	}
}

// Snipe-IT formats costs by its own setting: "1,999.00", or with a
// German separator "265,00" and "1.234,56" (seen live: 265,00 read as
// 26500).
func TestSnipeMoney(t *testing.T) {
	for raw, want := range map[string]float64{"1,999.00": 1999, "265,00": 265, "1.234,56": 1234.56, "0,89": 0.89, "1200": 1200, "": 0} {
		if got := sources.SnipeMoney(raw); got != want {
			t.Errorf("SnipeMoney(%q) = %v, want %v", raw, got, want)
		}
	}
}

// The depreciation rule's months come with each asset.
func TestSnipeDepreciation(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/hardware", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"total": 1, "rows": [{"id": 1, "name": "Laptop", "purchase_cost": "1.200,00",
			"depreciation": {"id": 2, "name": "1 Jahr / PCs", "months": 12}}]}`))
	})
	for _, path := range []string{"/api/v1/licenses", "/api/v1/consumables", "/api/v1/hardware/audit/overdue"} {
		mux.HandleFunc(path, func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(`{"total": 0, "rows": []}`)) })
	}
	srv := httptest.NewServer(mux)
	defer srv.Close()

	out, err := sources.SnipeData.Fetch(context.Background(), sources.Ctx{URL: srv.URL, Secret: "tok", VerifyTLS: true})
	if err != nil {
		t.Fatal(err)
	}
	if a := out.(*sources.SnipeDataset).Assets[0]; a.DepMonths != 12 || a.PurchaseCost != 1200 {
		t.Errorf("asset: %+v", a)
	}
}
