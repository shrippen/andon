package sources_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"andon/internal/caps"
	"andon/internal/enums"
	"andon/internal/sources"
)

// Every service that declares capabilities reports them from its data
// source, so the record and the stores can see them (agent.md rule).
func TestDeclaredServicesReportCaps(t *testing.T) {
	for _, h := range caps.Holders() {
		if h == caps.Andon {
			continue
		}
		src, err := sources.Get(sources.DataKey(enums.ServiceType(h)))
		if err != nil {
			t.Errorf("%s: no data source: %v", h, err)
			continue
		}
		out, err := src.Fetch(context.Background(), sources.Ctx{URL: "demo://" + string(h)})
		if err != nil {
			t.Errorf("%s: demo fetch: %v", h, err)
			continue
		}
		r, ok := out.(caps.Reporter)
		if !ok {
			t.Errorf("%s: %T reports no capabilities", h, out)
			continue
		}
		if set := r.CapSet(); set.Holder != h || len(set.Missing) != 0 {
			t.Errorf("%s: demo set %+v", h, set)
		}
	}
}

// Without the custom fields API (old Paperless) receipts are read-only
// at best: upload works, reading and filling fields do not.
func TestPaperlessWithoutCustomFields(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/custom_fields/" {
			http.NotFound(w, r)
			return
		}
		w.Write([]byte(`{"count": 0, "results": []}`))
	}))
	defer srv.Close()

	out, err := sources.PaperlessTest.Fetch(context.Background(), sources.Ctx{URL: srv.URL, Secret: "tok", VerifyTLS: true})
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	set, _ := out.(map[string]any)[sources.TestCaps].(caps.Set)
	if !set.Can(caps.Receipts, caps.Create, "") || set.Can(caps.Receipts, caps.Read, "") || len(set.Partial()) != 2 {
		t.Fatalf("set %+v", set)
	}

	data, err := sources.PaperlessData.Fetch(context.Background(), sources.Ctx{URL: srv.URL, Secret: "tok", VerifyTLS: true})
	if err != nil {
		t.Fatalf("data: %v", err)
	}
	if got := data.(caps.Reporter).CapSet(); got.Can(caps.Receipts, caps.Update, "") {
		t.Fatalf("data set %+v", got)
	}
}
