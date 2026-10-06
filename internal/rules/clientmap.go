package rules

import "andon/internal/metrics"

// ClientMapDataset holds the Verbund's customer links (metrics.ClientMap):
// Kimai customer → Invoice Ninja client; absent, the same name decides.
const ClientMapDataset = "clientmap"

// clientMap is the env's customer links, nil without a Verbund.
func clientMap(env Env) metrics.ClientMap {
	m, _ := env.Datasets[ClientMapDataset].(metrics.ClientMap)
	return m
}
