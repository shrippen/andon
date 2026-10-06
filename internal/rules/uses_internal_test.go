package rules

import "testing"

// Every rule named in Uses is registered.
func TestUsesNameRules(t *testing.T) {
	for id := range uses {
		if _, ok := registry[id]; !ok {
			t.Errorf("Uses names unknown rule %s", id)
		}
	}
}
