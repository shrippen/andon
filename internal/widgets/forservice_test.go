package widgets_test

import (
	"slices"
	"testing"

	"andon/internal/enums"
	"andon/internal/widgets"
)

// TestForService: a service's templates start with its starter, then its
// own types, then tiles that read it beside others (Borg → "backups").
func TestForService(t *testing.T) {
	kimai := widgets.ForService(enums.ServiceKimai)
	if len(kimai) < 2 || kimai[0] != "kimai_week" {
		t.Fatalf("kimai: %v", kimai)
	}
	for _, key := range kimai {
		if kind, _ := widgets.Get(key); kind.Service != enums.ServiceKimai && kind.Service != "" {
			t.Fatalf("%s is no kimai template", key)
		}
	}

	borg := widgets.ForService(enums.ServiceBorgBackup)
	if !slices.Contains(borg, "backups") {
		t.Fatalf("borg lacks the backup overview: %v", borg)
	}
	if slices.Contains(widgets.ForService(enums.ServiceScrutiny), "backups") {
		t.Fatal("scrutiny is no backup tool")
	}
	if got := widgets.ForService(enums.ServiceJSONAPI); len(got) != 0 {
		t.Fatalf("json api needs setup first: %v", got)
	}
}
