package metrics

// A category's data in a Verbund (caps.Category): the first member's
// dataset there, in the category's shape.
//
//	datasets{"snipeit": *SnipeDataset} ─► InventoryOf ─► *InventoryData
//	datasets{"firefly": *BankData}     ─► BankOf      ─► *BankData

import (
	"reflect"

	"andon/internal/caps"
	"andon/internal/sources"
)

// InventoryOf is the inventory of a scope; false without one.
func InventoryOf(datasets map[string]any) (*sources.InventoryData, bool) {
	src, ok := pick[sources.InventorySource](datasets, caps.Inventory)
	if !ok {
		return nil, false
	}
	return src.Inventory(), true
}

// BankOf is the bank data of a scope: Sure's, else Firefly III's; false
// without either.
func BankOf(datasets map[string]any) (*sources.BankData, bool) {
	src, ok := pick[sources.BankSource](datasets, caps.Bank)
	if !ok {
		return nil, false
	}
	return src.Bank(), true
}

// pick is the dataset of the category's first member present; a nil
// pointer counts as none.
func pick[S any](datasets map[string]any, c caps.Category) (S, bool) {
	for _, service := range caps.Members(c) {
		d, ok := datasets[string(service)].(S)
		if ok && !nilPtr(d) {
			return d, true
		}
	}
	var none S
	return none, false
}

// nilPtr reports whether v holds a nil pointer.
func nilPtr(v any) bool {
	rv := reflect.ValueOf(v)
	return rv.Kind() == reflect.Pointer && rv.IsNil()
}
