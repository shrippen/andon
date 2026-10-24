package sources_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"andon/internal/caps"
	"andon/internal/enums"
	"andon/internal/sources"
)

// keptFake answers like Kept 1.12 for a write token in one location with
// the MCP module: a bought laptop (life field, an attachment, a Paperless
// link), a fire extinguisher without purchase, one agenda item.
func keptFake(t *testing.T, mcp bool) *httptest.Server {
	t.Helper()
	modules := `["money","warranties"]`
	if mcp {
		modules = `["money","warranties","mcp"]`
	}
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer kpt_a_b" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		switch r.URL.Path {
		case "/api/v1/locations":
			io.WriteString(w, `{"locations":[{"id":"loc1","name":"Büro","modules":`+modules+`}]}`)
		case "/api/v1/things/sheet":
			if r.URL.Query().Get("locationId") != "loc1" || r.URL.Query().Get("limit") == "" {
				t.Errorf("sheet query %s", r.URL.RawQuery)
			}
			io.WriteString(w, `{"items":[{"id":"t1","shortCode":"H6ZQ46","name":"Laptop","model":"X1","serial":"SN1","type":{"name":"Laptop"},`+
				`"purchase":{"purchasedOn":"2026-03-15","price":"1499","currency":"EUR"},"warranty":{"endsOn":"2028-03-14","lifetime":false}},`+
				`{"id":"t2","shortCode":"FL0SCH","name":"Feuerlöscher","type":{"name":null,"builtinKey":"fire_extinguisher"},"expiresOn":"2026-11-20"}],"next_cursor":null}`)
		case "/api/v1/things/t1":
			io.WriteString(w, `{"id":"t1","custom":{"nutzungsdauer":36},"attachmentsCount":1,"rowVersion":2}`)
		case "/api/v1/things/t1/attachments":
			io.WriteString(w, `{"items":[{"role":"receipt","url":"https://docs.lan/documents/7"}],"next_cursor":null}`)
		case "/api/v1/paperless-links":
			io.WriteString(w, `{"connected":true,"url":"https://docs.lan","items":[{"documentId":42,"kind":"invoice","openUrl":"https://docs.lan/documents/42/details"}]}`)
		case "/api/v1/agenda":
			io.WriteString(w, `{"items":[{"sourceType":"thing_expiry","state":"expiring","dueOn":"2026-11-20","title":"Feuerlöscher","subject":{"name":"Feuerlöscher","shortCode":"FL0SCH"}}],"next_cursor":null}`)
		case "/mcp":
			body, _ := io.ReadAll(r.Body)
			if !strings.Contains(string(body), `"tools/list"`) {
				t.Errorf("mcp body %s", body)
			}
			io.WriteString(w, "event: message\ndata: {\"result\":{\"tools\":[{\"name\":\"get_thing\"},{\"name\":\"add_warranty\"}]},\"jsonrpc\":\"2.0\",\"id\":1}\n\n")
		default:
			t.Errorf("unexpected %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
}

// TestKept: things with purchase, life, receipts and expiry, the agenda,
// the inventory shape and the write capabilities (token + MCP module).
func TestKept(t *testing.T) {
	srv := keptFake(t, true)
	defer srv.Close()

	raw, err := sources.KeptData.Fetch(context.Background(), sources.Ctx{URL: srv.URL, Secret: "kpt_a_b"})
	if err != nil {
		t.Fatal(err)
	}
	data := raw.(*sources.KeptDataset)
	if len(data.Things) != 2 || len(data.Agenda) != 1 || !data.Writable {
		t.Fatalf("data %+v", data)
	}
	laptop, ext := data.Things[0], data.Things[1]
	if laptop.Life != 36 || laptop.Price != 1499 || laptop.Bought != "2026-03-15" || laptop.WarrantyEnds != "2028-03-14" || laptop.Location != "Büro" {
		t.Errorf("laptop %+v", laptop)
	}
	if len(laptop.Receipts) != 2 || laptop.Receipts[1].DocID != 42 || laptop.Receipts[1].Paperless != "https://docs.lan" || laptop.Receipts[0].DocID != 0 {
		t.Errorf("receipts %+v", laptop.Receipts)
	}
	if ext.Expires != "2026-11-20" || ext.Type != "fire_extinguisher" || ext.Receipts != nil {
		t.Errorf("extinguisher %+v", ext)
	}
	if d := data.Agenda[0]; d.Kind != "thing_expiry" || d.State != "expiring" || d.Code != "FL0SCH" {
		t.Errorf("agenda %+v", d)
	}

	inv := data.Inventory()
	if a := inv.Assets[0]; inv.Service != enums.ServiceKept || a.Link != srv.URL+"/t/H6ZQ46" || a.DepMonths != 36 || a.PurchaseCost != 1499 {
		t.Errorf("asset %+v", a)
	}
	if !data.CapSet().Can(caps.Assets, caps.Update, "warranty") {
		t.Errorf("caps %+v", data.CapSet())
	}
}

// TestKeptWithoutMCP: without the module Andon cannot tell a write token
// and offers no writes; reading works as before.
func TestKeptWithoutMCP(t *testing.T) {
	srv := keptFake(t, false)
	defer srv.Close()

	raw, err := sources.KeptData.Fetch(context.Background(), sources.Ctx{URL: srv.URL, Secret: "kpt_a_b", Options: map[string]any{sources.KeptLifeOption: "life"}})
	if err != nil {
		t.Fatal(err)
	}
	data := raw.(*sources.KeptDataset)
	if data.Writable || data.CapSet().Can(caps.Assets, caps.Create, "") || !data.CapSet().Can(caps.Assets, caps.Read, "") {
		t.Errorf("caps %+v", data.CapSet())
	}
	if data.Things[0].Life != 0 {
		t.Errorf("life from another field: %d", data.Things[0].Life)
	}
}

// TestDemoKept: the demo reads Studio Weber's things in Kept.
func TestDemoKept(t *testing.T) {
	data := sources.DemoKept(time.Now())
	if len(data.Things) < 5 || len(data.Agenda) == 0 || data.Things[0].Life == 0 || len(data.Things[0].Receipts) == 0 {
		t.Fatalf("demo %+v", data)
	}
}

// A failed detail read leaves the receipt unknown: no "no receipt" hint
// for a thing whose invoice Kept has but did not answer for.
func TestKeptReadErrorKeepsReceiptUnknown(t *testing.T) {
	inner := keptFake(t, true)
	defer inner.Close()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/paperless-links" {
			w.WriteHeader(http.StatusBadGateway)
			return
		}
		proxyTo(t, inner.URL, w, r)
	}))
	defer srv.Close()

	raw, err := sources.KeptData.Fetch(context.Background(), sources.Ctx{URL: srv.URL, Secret: "kpt_a_b"})
	if err != nil {
		t.Fatal(err)
	}
	if a := raw.(*sources.KeptDataset).Inventory().Assets[0]; a.Receipt != sources.ReceiptUnknown {
		t.Errorf("receipt after a failed read: %q", a.Receipt)
	}
}

// proxyTo forwards a request to the fake Kept.
func proxyTo(t *testing.T, base string, w http.ResponseWriter, r *http.Request) {
	t.Helper()
	req, _ := http.NewRequest(r.Method, base+r.URL.RequestURI(), r.Body)
	req.Header = r.Header.Clone()
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	w.WriteHeader(resp.StatusCode)
	io.Copy(w, resp.Body)
}

// Without a connected Paperless the links stay unread: a bought thing
// without an attached receipt has an unknown receipt, not none.
func TestKeptPaperlessOffKeepsReceiptUnknown(t *testing.T) {
	data := &sources.KeptDataset{Things: []sources.KeptThing{
		{ID: "a", Name: "Monitor", Bought: "2026-08-25", Price: 890, PaperlessMissing: true},
		{ID: "b", Name: "Laptop", Bought: "2026-08-25", Price: 1200, PaperlessMissing: true, Receipts: []sources.KeptReceipt{{Role: "invoice", URL: "https://x/7"}}},
	}}
	assets := data.Inventory().Assets
	if assets[0].Receipt != sources.ReceiptUnknown || assets[1].Receipt != sources.ReceiptFound {
		t.Errorf("receipts %q %q", assets[0].Receipt, assets[1].Receipt)
	}
}
