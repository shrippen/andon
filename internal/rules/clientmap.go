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

// PayerMapDataset holds the Verbund's payer links (metrics.PayerMap):
// Sure payer → Invoice Ninja client.
const PayerMapDataset = "payermap"

// payerMap is the env's payer links, nil without a Verbund.
func payerMap(env Env) metrics.PayerMap {
	m, _ := env.Datasets[PayerMapDataset].(metrics.PayerMap)
	return m
}
