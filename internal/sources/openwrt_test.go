package sources_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"andon/internal/sources"
)

// fakeUbus answers OpenWrt's /ubus JSON-RPC like rpcd does: a login gives
// a session, calls with it return [0, data], an unknown object [6].
func fakeUbus(t *testing.T) *httptest.Server {
	answers := map[string]string{
		"system board": `{"hostname": "router", "model": "GL.iNet GL-MT6000",
			"release": {"distribution": "OpenWrt", "version": "24.10.2", "description": "OpenWrt 24.10.2 r28739"}}`,
		"system info": `{"uptime": 86400, "load": [65536, 32768, 0], "memory": {"total": 1000000, "free": 400000}}`,
		"network.interface dump": `{"interface": [
			{"interface": "lan", "up": true, "route": []},
			{"interface": "wan", "up": true, "uptime": 3600, "route": [{"target": "0.0.0.0", "mask": 0, "nexthop": "192.0.2.1"}]},
			{"interface": "wan6", "up": false, "route": [{"target": "::", "mask": 0}]}]}`,
		"luci-rpc getDHCPLeases": `{"dhcp_leases": [{"hostname": "nas"}, {"hostname": "laptop"}], "dhcp6_leases": [{"hostname": "nas"}]}`,
	}
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/ubus" || r.Method != http.MethodPost {
			http.NotFound(w, r)
			return
		}
		var req struct {
			ID     int   `json:"id"`
			Params []any `json:"params"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil || len(req.Params) != 4 {
			t.Errorf("bad request: %v %+v", err, req)
			return
		}
		session, object, method := req.Params[0].(string), req.Params[1].(string), req.Params[2].(string)
		result := `[6]`
		switch {
		case object == "session" && method == "login":
			args := req.Params[3].(map[string]any)
			if args["username"] == "root" && args["password"] == "pw" {
				result = `[0, {"ubus_rpc_session": "s1", "timeout": 300}]`
			}
		case session == "s1" && answers[object+" "+method] != "":
			result = `[0, ` + answers[object+" "+method] + `]`
		}
		w.Write([]byte(`{"jsonrpc": "2.0", "id": 1, "result": ` + result + `}`))
	}))
}

// TestOpenWrtGateway: version and model from system board, WAN links
// are the interfaces with a default route, clients from the DHCP leases.
// A secret without "user:" logs in as root.
func TestOpenWrtGateway(t *testing.T) {
	srv := fakeUbus(t)
	defer srv.Close()
	for _, secret := range []string{"root:pw", "pw"} {
		out, err := sources.GatewayData{}.Fetch(t.Context(), sources.Ctx{URL: srv.URL, Secret: secret, Options: map[string]any{"kind": "openwrt"}})
		if err != nil {
			t.Fatalf("%s: %v", secret, err)
		}
		d := out.(*sources.GatewayDataset)
		if d.Kind != "openwrt" || d.Version != "24.10.2" || d.Model != "GL.iNet GL-MT6000" || d.Clients != 2 || len(d.ClientNames) != 2 {
			t.Fatalf("dataset: %+v", d)
		}
		if len(d.Gateways) != 2 || d.Gateways[0].Name != "wan" || !d.Gateways[0].Up || d.Gateways[1].Up {
			t.Fatalf("links: %+v", d.Gateways)
		}
	}
	if _, err := (sources.GatewayData{}).Fetch(t.Context(), sources.Ctx{URL: srv.URL, Secret: "root:wrong", Options: map[string]any{"kind": "openwrt"}}); err == nil {
		t.Fatal("wrong password accepted")
	}
}
