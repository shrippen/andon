package sources

// Homelable is no source of data for Andon: it only shows what Andon
// draws (outbound/homelable). Reading its canvases checks the login and
// keeps the connection's health like any other.
//
//	GET /api/v1/designs  ─► name and node count of each canvas

import (
	"context"
	"strings"
	"time"

	"andon/internal/drivers/services"
	"andon/internal/enums"
)

// HomelableDataset is the canvases of a Homelable instance.
type HomelableDataset struct {
	URL      string
	Canvases []HomelableCanvas
}

// HomelableCanvas is one canvas (Homelable: design).
type HomelableCanvas struct {
	ID, Name string
	Nodes    int
}

var HomelableData = source{key: "homelable.data", ttl: opsTTL, service: enums.ServiceHomelable, fetch: fetchHomelable}

func fetchHomelable(ctx context.Context, sctx Ctx) (any, error) {
	if isDemo(sctx) {
		return DemoHomelable(time.Now()), nil
	}
	secret, err := needSecret(sctx)
	if err != nil {
		return nil, err
	}
	user, password, _ := strings.Cut(secret, ":")
	api := services.HomelableApi{URL: sctx.URL, User: user, Password: password, Verify: sctx.VerifyTLS}
	raw, err := api.Get(ctx, "designs")
	if err != nil {
		return nil, fetchError(err)
	}

	data := &HomelableDataset{URL: sctx.URL}
	for _, item := range asList(raw) {
		d := asMap(item)
		data.Canvases = append(data.Canvases, HomelableCanvas{ID: asStr(d["id"]), Name: asStr(d["name"]), Nodes: int(asFloat(d["node_count"]))})
	}
	return data, nil
}

func init() {
	Register(HomelableData)
	Register(testOf{HomelableData, func(d any) map[string]any { return map[string]any{"canvases": len(d.(*HomelableDataset).Canvases)} }})
}
