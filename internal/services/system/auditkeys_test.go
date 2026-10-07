package system_test

import (
	"testing"

	"andon/internal/i18n"
	"andon/internal/services/system"
)

// TestSettingsAuditNamed: every settings object's audit action has a
// text; the audit page showed "audit_action.settings.map".
func TestSettingsAuditNamed(t *testing.T) {
	for _, key := range []string{system.NetworkKey, system.IframeKey, system.MapKey, system.SecurityKey,
		system.RegistrationKey, system.LocationKey, system.ServerKey} {
		if !i18n.Has("audit_action.settings." + key) {
			t.Errorf("no text for audit_action.settings.%s", key)
		}
	}
}
