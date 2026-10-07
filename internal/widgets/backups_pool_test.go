package widgets

import (
	"testing"
	"time"

	"andon/internal/sources"
)

// TestBackupsDetailStores: the dialog lists PBS's datastores with their
// fill and, where the option pools names it, the TrueNAS pool's fill.
func TestBackupsDetailStores(t *testing.T) {
	results := map[string]any{"pbs": sources.DemoPBS(time.Now()), "truenas": sources.DemoTrueNAS()}
	body := backupsDetail(BackupsConfig{}, results, ViewCtx{}).Body.(*DetailBody)

	var table *Table
	for _, b := range body.Blocks {
		if b.Label.Key == "detail.backups.stores" {
			tb := b.Data.(Table)
			table = &tb
		}
	}
	if table == nil || len(table.Rows) != 2 {
		t.Fatalf("blocks: %+v", body.Blocks)
	}
	nebel, archiv := table.Rows[0], table.Rows[1]
	if nebel[0].Value != "nebelhorn" || nebel[2].Value != "–" || archiv[0].Value != "archiv" || archiv[2].Value != "tank" {
		t.Fatalf("rows: %+v", table.Rows)
	}
	if archiv[3].Value.(map[string]any)["$num"] != 88.0 {
		t.Fatalf("pool fill: %+v", archiv[3])
	}
}
