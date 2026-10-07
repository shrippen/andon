package metrics

import (
	"andon/internal/enums"
	"andon/internal/sources"
)

// BankOf is the bank data of a scope: Sure's, else Firefly III's (both in
// Sure's shape); false without either.
func BankOf(datasets map[string]any) (*sources.SureDataset, bool) {
	for _, s := range []enums.ServiceType{enums.ServiceSure, enums.ServiceFirefly} {
		if d, ok := datasets[string(s)].(*sources.SureDataset); ok {
			return d, true
		}
	}
	return nil, false
}
