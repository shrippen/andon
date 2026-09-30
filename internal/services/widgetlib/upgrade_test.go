package widgetlib_test

import (
	"reflect"
	"testing"
	"time"

	"andon/internal/model"
	"andon/internal/repos/content"
	"andon/internal/services/widgetlib"
)

// TestUpgradeConfigs: widgets stored under old keys are rewritten once
// under the current ones; current configs stay as they are.
func TestUpgradeConfigs(t *testing.T) {
	d := openTestDB(t)
	u := addUser(t, d, "upgrade@b.c")
	space, _ := content.PersonalSpace(d, u.ID)

	old := &model.Widget{SpaceID: space.ID, Key: "k", Type: "komodo_stacks", Config: map[string]any{"only_issues": true},
		Version: 3, UpdatedAt: time.Now().UTC()}
	current := &model.Widget{SpaceID: space.ID, Key: "h", Type: "hints", Config: map[string]any{"sort": "age"},
		Version: 1, UpdatedAt: time.Now().UTC()}
	for _, w := range []*model.Widget{old, current} {
		if err := content.AddWidget(d, w); err != nil {
			t.Fatal(err)
		}
	}

	n, err := widgetlib.UpgradeConfigs(d)
	if err != nil || n != 1 {
		t.Fatalf("upgraded %d, %v", n, err)
	}
	got, _ := content.Widget(d, old.ID)
	if !reflect.DeepEqual(got.Config, map[string]any{"only_problems": true}) || got.Version != 3 {
		t.Fatalf("widget: %+v", got)
	}
	if n, _ := widgetlib.UpgradeConfigs(d); n != 0 {
		t.Fatalf("second run upgraded %d", n)
	}
}
