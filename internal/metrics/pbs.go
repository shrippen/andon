package metrics

// PBS datastores against the TrueNAS pool they live on (PBS option
// pools: {archiv: tank}). PBS counts its store's fill; the pool can be
// fuller, because other datasets share it.
//
//	PBSStore{archiv, 1.2 of 3.1 TB, pool tank} + Pool{tank, 14.1 of 16 TB}
//	  ─► PoolFill{Store: archiv, StorePct: 39, Pool: tank, PoolPct: 88}

import (
	"strings"

	"andon/internal/sources"
)

// PoolFill is a datastore's fill next to its pool's.
type PoolFill struct {
	Store, Pool       string // Pool as TrueNAS names it
	StorePct, PoolPct float64
}

// PBSPools are the stores with a pool that TrueNAS knows, in PBS's order.
func PBSPools(pbs *sources.PBSDataset, nas *sources.TrueNASDataset) []PoolFill {
	if pbs == nil || nas == nil {
		return nil
	}
	pools := map[string]sources.Pool{}
	for _, p := range nas.Pools {
		pools[strings.ToLower(p.Name)] = p
	}

	var out []PoolFill
	for _, s := range pbs.Stores {
		p, ok := pools[strings.ToLower(s.Pool)]
		if s.Pool == "" || !ok || s.Total <= 0 || p.Size <= 0 {
			continue
		}
		out = append(out, PoolFill{Store: s.Store, Pool: p.Name, StorePct: s.Used / s.Total * percentScale, PoolPct: p.Allocated / p.Size * percentScale})
	}
	return out
}
