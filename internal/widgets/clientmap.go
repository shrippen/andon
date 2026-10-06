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
