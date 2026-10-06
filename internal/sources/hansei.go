package sources

// Hansei has no API Andon could poll; it pushes its whole state to the
// connection's webhook URL whenever it changes, and Andon keeps the last:
//
//	POST /hooks/{id}/{sig}  {"state": {"review": 2, "feedback": 1, "done": 7,
//	                                   "conformity": 0.82, "claimed": ["docs.missing:regis/kometa"]}}
//
// claimed names the docs findings (GET /api/docs, field id) a batch
// already works on: their hints wait instead of nagging.

import (
	"context"
	"time"

	"andon/internal/enums"
)

// HanseiDataset is Hansei's last pushed state.
type HanseiDataset struct {
	URL                    string
	Review, Feedback, Done int     // batches by column
	Conformity             float64 // share of the vault following its rules, 0..1
	Claimed                []string
	Updated                time.Time // zero: nothing pushed yet
}

var HanseiData = stateSource{source{key: "hansei.data", ttl: time.Minute, service: enums.ServiceHansei, fetch: fetchHansei}}

func fetchHansei(_ context.Context, sctx Ctx) (any, error) {
	if isDemo(sctx) {
		return DemoHansei(time.Now()), nil
	}
	return foldHansei(sctx.URL, sctx.State, sctx.StateAt), nil
}

func foldHansei(base string, state map[string]any, at time.Time) *HanseiDataset {
	data := &HanseiDataset{URL: base}
	if state == nil {
		return data
	}
	data.Review, data.Feedback, data.Done = int(asFloat(state["review"])), int(asFloat(state["feedback"])), int(asFloat(state["done"]))
	data.Conformity, data.Updated = asFloat(state["conformity"]), at
	for _, id := range asList(state["claimed"]) {
		if s := asStr(id); s != "" {
			data.Claimed = append(data.Claimed, s)
		}
	}
	return data
}

func init() {
	Register(HanseiData)
	Register(testOf{HanseiData, func(d any) map[string]any { return map[string]any{"review": d.(*HanseiDataset).Review} }})
}
