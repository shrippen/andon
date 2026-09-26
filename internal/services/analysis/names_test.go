package analysis

import (
	"testing"

	"andon/internal/enums"
	"andon/internal/i18n"
)

// TestConnectorRuleHasName: the outage hint's rule is set here, not
// registered with the rules, so the rules' own name check misses it; its
// group header showed the raw key "rule_name.system.connector_down".
func TestConnectorRuleHasName(t *testing.T) {
	for _, locale := range []enums.Locale{enums.LocaleDE, enums.LocaleEN} {
		for _, rule := range []string{connectorRule, backupRule} {
			if key := "rule_name." + rule; i18n.T(key, locale, nil) == key {
				t.Errorf("%s: no name for %s", locale, rule)
			}
		}
	}
}
