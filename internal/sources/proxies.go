package sources

// Reverse proxies and their routes, one shape for all of them:
//
//	Traefik  GET api/http/routers  → [{name, rule: "Host(`a`)", service, status}]
//	         GET api/http/services → [{name, serverStatus: {"http://ip:port": "UP"}}]
//	Caddy    GET config/apps/http/servers → {srv0: {routes: [{match: [{host: […]}], handle: [… reverse_proxy upstreams [{dial}] …]}]}}
//	         GET reverse_proxy/upstreams → [{address, fails}]
//	NPM      POST api/tokens {identity, secret} → {token}; GET api/nginx/proxy-hosts?expand=certificate

import (
	"context"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"andon/internal/drivers/services"
	"andon/internal/enums"
	"andon/internal/sources/demoworld"
)

// ProxyRoute is one host a proxy serves and where it sends it.
type ProxyRoute struct {
	Host       string    // "photos.example.org"
	Service    string    // the proxy's name for the target, often the container: "immich-server"
	Target     string    // "http://172.18.0.5:2283", "bookstack:80"
	Up         bool      // the target answers (as far as the proxy knows)
	CertExpiry time.Time // zero = unknown (NPM knows it)
}

// RoutesDataset is one proxy's routes.
type RoutesDataset struct {
	URL    string
	Tool   enums.ServiceType
	Routes []ProxyRoute
}

var (
	TraefikData = source{key: "traefik.data", ttl: opsTTL, service: enums.ServiceTraefik, fetch: fetchTraefik}
	CaddyData   = source{key: "caddy.data", ttl: opsTTL, service: enums.ServiceCaddy, fetch: fetchCaddy}
	NPMData     = source{key: "npm.data", ttl: opsTTL, service: enums.ServiceNPM, fetch: fetchNPM}
)

// ── Traefik ──

var traefikHosts = regexp.MustCompile("Host\\(([^)]*)\\)")
var backtick = regexp.MustCompile("`([^`]+)`")

func fetchTraefik(ctx context.Context, sctx Ctx) (any, error) {
	if isDemo(sctx) {
		return DemoRoutes(time.Now().UTC(), enums.ServiceTraefik), nil
	}
	api := basicOrNone(sctx)
	routers, err := api.Get(ctx, "api/http/routers", nil)
	if err != nil {
		return nil, fetchError(err)
	}
	svcs, err := api.Get(ctx, "api/http/services", nil)
	if err != nil {
		return nil, fetchError(err)
	}
	type target struct {
		url string
		up  bool
	}
	targets := map[string]target{}
	for _, raw := range asList(svcs) {
		s := asMap(raw)
		t := target{up: true}
		for addr, status := range asMap(s["serverStatus"]) {
			t.url = addr
			t.up = t.up && asStr(status) == "UP"
		}
		targets[asStr(s["name"])] = t
	}
	data := &RoutesDataset{URL: sctx.URL, Tool: enums.ServiceTraefik}
	for _, raw := range asList(routers) {
		r := asMap(raw)
		service := asStr(r["service"])
		if !strings.Contains(service, "@") {
			_, provider, _ := strings.Cut(asStr(r["name"]), "@")
			service += "@" + provider
		}
		t := targets[service]
		name, _, _ := strings.Cut(service, "@")
		for _, h := range traefikHosts.FindAllStringSubmatch(asStr(r["rule"]), -1) {
			for _, host := range backtick.FindAllStringSubmatch(h[1], -1) {
				data.Routes = append(data.Routes, ProxyRoute{Host: host[1], Service: name, Target: t.url, Up: t.up && asStr(r["status"]) == "enabled"})
			}
		}
	}
	return data, nil
}

// ── Caddy ──

func fetchCaddy(ctx context.Context, sctx Ctx) (any, error) {
	if isDemo(sctx) {
		return DemoRoutes(time.Now().UTC(), enums.ServiceCaddy), nil
	}
	api := basicOrNone(sctx)
	servers, err := api.Get(ctx, "config/apps/http/servers", nil)
	if err != nil {
		return nil, fetchError(err)
	}
	failing := map[string]bool{}
	if ups, err := api.Get(ctx, "reverse_proxy/upstreams", nil); err == nil {
		for _, raw := range asList(ups) {
			u := asMap(raw)
			failing[asStr(u["address"])] = asFloat(u["fails"]) > 0
		}
	}
	data := &RoutesDataset{URL: sctx.URL, Tool: enums.ServiceCaddy}
	for _, srv := range asMap(servers) {
		for _, raw := range asList(asMap(srv)["routes"]) {
			route := asMap(raw)
			var hosts []string
			for _, m := range asList(route["match"]) {
				for _, h := range asList(asMap(m)["host"]) {
					hosts = append(hosts, asStr(h))
				}
			}
			dial := firstDial(route["handle"])
			for _, h := range hosts {
				name, _, _ := strings.Cut(dial, ":")
				data.Routes = append(data.Routes, ProxyRoute{Host: h, Service: name, Target: dial, Up: dial == "" || !failing[dial]})
			}
		}
	}
	return data, nil
}

// firstDial finds the first reverse_proxy upstream in nested handlers.
func firstDial(node any) string {
	for _, raw := range asList(node) {
		h := asMap(raw)
		if asStr(h["handler"]) == "reverse_proxy" {
			for _, u := range asList(h["upstreams"]) {
				return asStr(asMap(u)["dial"])
			}
		}
		for _, sub := range asList(h["routes"]) {
			if d := firstDial(asMap(sub)["handle"]); d != "" {
				return d
			}
		}
	}
	return ""
}

// ── Nginx Proxy Manager ──

// npmTime is how NPM writes certificate dates: "2026-11-01 10:00:00".
const npmTime = "2006-01-02 15:04:05"

func fetchNPM(ctx context.Context, sctx Ctx) (any, error) {
	if isDemo(sctx) {
		return DemoRoutes(time.Now().UTC(), enums.ServiceNPM), nil
	}
	secret, err := needSecret(sctx)
	if err != nil {
		return nil, err
	}
	user, pass, _ := strings.Cut(secret, ":")
	login := services.KeyedApi{URL: sctx.URL, Headers: map[string]string{}, Verify: sctx.VerifyTLS}
	raw, err := login.Post(ctx, "api/tokens", map[string]any{"identity": user, "secret": pass})
	if err != nil {
		return nil, fetchError(err)
	}
	token := asStr(asMap(raw)["token"])
	if token == "" {
		return nil, fetchError(services.ErrLogin)
	}
	hosts, err := services.BearerApi(sctx.URL, token, sctx.TLS()).Get(ctx, "api/nginx/proxy-hosts", map[string][]string{"expand": {"certificate"}})
	if err != nil {
		return nil, fetchError(err)
	}
	data := &RoutesDataset{URL: sctx.URL, Tool: enums.ServiceNPM}
	for _, item := range asList(hosts) {
		h := asMap(item)
		target := asStr(h["forward_scheme"]) + "://" + asStr(h["forward_host"]) + ":" + strconv.Itoa(int(asFloat(h["forward_port"])))
		expiry, _ := time.Parse(npmTime, asStr(asMap(h["certificate"])["expires_on"]))
		enabled := asBool(h["enabled"]) || asFloat(h["enabled"]) == 1
		for _, d := range asList(h["domain_names"]) {
			data.Routes = append(data.Routes, ProxyRoute{Host: asStr(d), Service: asStr(h["forward_host"]), Target: target, Up: enabled, CertExpiry: expiry})
		}
	}
	return data, nil
}

// DemoRoutes is the studio's routes of one proxy.
func DemoRoutes(now time.Time, tool enums.ServiceType) *RoutesDataset {
	data := &RoutesDataset{Tool: tool}
	demoworld.MustDecode("proxy_routes."+string(tool), now, data)
	sort.SliceStable(data.Routes, func(i, j int) bool { return !data.Routes[i].Up && data.Routes[j].Up })
	return data
}

func init() {
	for _, s := range []source{TraefikData, CaddyData, NPMData} {
		Register(s)
		Register(testOf{s, func(d any) map[string]any { return map[string]any{"routes": len(d.(*RoutesDataset).Routes)} }})
	}
}
