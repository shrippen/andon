package metrics

import "andon/internal/sources"

// ClientMap ties Kimai customers to Invoice Ninja clients by id, as a
// Verbund stores them (CAPABILITIES.md, "Kunden"): Kimai customer id →
// Ninja client reference (NinjaClient.Ref, "Kx9"); "" says there is no
// counterpart. A customer
// missing in it falls back to the client of the same name.
type ClientMap map[int64]string

// ClientOf is the Ninja client of a Kimai customer.
func (m ClientMap) ClientOf(ninja *sources.NinjaDataset, customerID int64, name string) (sources.NinjaClient, bool) {
	if ninja == nil {
		return sources.NinjaClient{}, false
	}
	key, stored := m[customerID]
	for _, c := range ninja.Clients {
		if stored && key != "" && c.Ref() == key {
			return c, true
		}
		if !stored && name != "" && nameKey(c.Name) == nameKey(name) {
			return c, true
		}
	}
	return sources.NinjaClient{}, false
}

// Customers maps each Ninja client id to its Kimai customer.
func (m ClientMap) Customers(kimai *sources.KimaiDataset, ninja *sources.NinjaDataset) map[int64]int64 {
	out := map[int64]int64{}
	if kimai == nil || ninja == nil {
		return out
	}
	for _, c := range kimai.Customers {
		if client, ok := m.ClientOf(ninja, c.ID, c.Name); ok {
			out[client.ID] = c.ID
		}
	}
	return out
}
