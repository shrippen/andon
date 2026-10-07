package widgets

// "routes": every host the space's reverse proxies (Traefik, Caddy,
// Nginx Proxy Manager) serve, broken ones first; the dialog shows target,
// state and certificate per route.

import (
	"sort"
	"strings"

	"andon/internal/enums"
	"andon/internal/sources"
)

// RoutesConfig is the "routes" widget's config.
type RoutesConfig struct{ OnlyProblems bool }

var routeServices = []enums.ServiceType{enums.ServiceTraefik, enums.ServiceCaddy, enums.ServiceNPM}

func init() {
	Tile[RoutesConfig]{Key: "routes", Detail: routesDetail, Category: CategoryInsight, Topic: TopicNetwork, RefreshS: 600,
		Fields: []Field{{Key: "only_problems", Input: InputCheck}},
		Decode: func(r Raw) RoutesConfig { return RoutesConfig{OnlyProblems: r.Bool("only_problems")} },
		Queries: func(RoutesConfig) []Query {
			var out []Query
			for _, s := range routeServices {
				out = append(out, Query{Name: string(s), Source: "data", Conn: ConnPeer, Service: s})
			}
			return out
		},
		View: routesView,
		Calm: func(v map[string]any) bool { return v["Total"] != nil && v["Total"] != 0 && v["Down"] == 0 }}.add()
}

// RouteLine is a route with its proxy.
type RouteLine struct {
	sources.ProxyRoute
	Tool enums.ServiceType
}

func allRoutes(results map[string]any) []RouteLine {
	var out []RouteLine
	for _, s := range routeServices {
		if d, ok := results[string(s)].(*sources.RoutesDataset); ok {
			for _, r := range d.Routes {
				out = append(out, RouteLine{ProxyRoute: r, Tool: d.Tool})
			}
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Up != out[j].Up {
			return !out[i].Up
		}
		return strings.ToLower(out[i].Host) < strings.ToLower(out[j].Host)
	})
	return out
}

func routesView(cfg RoutesConfig, results map[string]any, _ ViewCtx) map[string]any {
	routes := allRoutes(results)
	down := 0
	var lines []RouteLine
	for _, r := range routes {
		if !r.Up {
			down++
		}
		if cfg.OnlyProblems && r.Up {
			continue
		}
		lines = append(lines, r)
	}
	return map[string]any{"Total": len(routes), "Down": down, "Lines": firstN(lines, routesShown)}
}

const routesShown = 8

func routesDetail(_ RoutesConfig, results map[string]any, _ ViewCtx) DetailView {
	routes := allRoutes(results)
	var rows [][]Cell
	down := 0
	for _, r := range routes {
		if !r.Up {
			down++
		}
		cert := any("–")
		if !r.CertExpiry.IsZero() {
			cert = Day(r.CertExpiry)
		}
		rows = append(rows, []Cell{{Value: r.Host, Href: "https://" + r.Host}, {Value: Txt("service." + string(r.Tool))}, {Value: orNone(r.Service)},
			{Value: orNone(r.Target)}, {Value: Txt(map[bool]string{true: "routes.up", false: "routes.down"}[r.Up]), State: stateIf(!r.Up, "bad")}, {Value: cert}})
	}
	body := &DetailBody{Facts: []Kpi{{Value: len(routes), Label: T("routes.total")},
		{Value: down, Label: T("routes.failing"), Tier: tierIf(down > 0, "red", "green")}}}
	if len(rows) > 0 {
		body.Blocks = append(body.Blocks, Block{Kind: BlockTable, Label: T("routes.all"), Data: Table{
			Head: []Text{T("routes.col.host"), T("routes.col.proxy"), T("routes.col.service"), T("routes.col.target"), T("routes.col.state"), T("routes.col.cert")},
			Rows: rows}})
	}
	body.Blocks = append(body.Blocks, hintsBlock(results)...)
	return DetailView{Body: body}
}
