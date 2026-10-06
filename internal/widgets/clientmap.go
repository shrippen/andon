package widgets

import "andon/internal/metrics"

// ClientMapSlot holds the customer links of the tile's Kimai and Invoice
// Ninja (metrics.ClientMap), filled by widgetlib when both are in one
// Verbund; absent, the same name decides.
const ClientMapSlot = "clientmap"

// clientMapOf is the tile's customer links, nil without a Verbund.
func clientMapOf(results map[string]any) metrics.ClientMap {
	m, _ := results[ClientMapSlot].(metrics.ClientMap)
	return m
}

// PayerMapSlot holds the payer links of the tile's Sure and Invoice Ninja
// (metrics.PayerMap), filled by widgetlib when both are in one Verbund.
const PayerMapSlot = "payermap"

// payerMapOf is the tile's payer links, nil without a Verbund.
func payerMapOf(results map[string]any) metrics.PayerMap {
	m, _ := results[PayerMapSlot].(metrics.PayerMap)
	return m
}
