package widgetlib_test

import (
	"context"
	"testing"

	"andon/internal/enums"
	"andon/internal/repos/content"
	"andon/internal/rules"
	"andon/internal/services/access"
	"andon/internal/services/hints"
	"andon/internal/services/svcdata"
	"andon/internal/services/widgetlib"
)

// The hints tile can drop its level bar, sort by the amount a hint names
// and offer done/later on each line.
func TestHintsTileOptions(t *testing.T) {
	d := openTestDB(t)
	u := addUser(t, d, "opts@b.c")
	who, _ := access.Load(d, u.ID)
	space, _ := content.PersonalSpace(d, u.ID)
	findings := []rules.Finding{
		{Fingerprint: "small", Rule: "ninja.overdue", Severity: enums.SeverityCritical, Message: "ninja.overdue",
			Params: map[string]any{"amount": map[string]any{"$money": 50.0, "currency": "EUR"}}},
		{Fingerprint: "big", Rule: "ninja.overdue", Severity: enums.SeverityWarn, Message: "ninja.overdue",
			Params: map[string]any{"amount": map[string]any{"$money": 5000.0, "currency": "EUR"}}},
	}
	if _, err := hints.Sync(d, space.ID, nil, nil, []string{"ninja.overdue"}, findings); err != nil {
		t.Fatalf("sync: %v", err)
	}
	id, err := widgetlib.Create(d, who, space.ID, "hints", "Lage", map[string]any{"show_levels": false, "sort": "value", "show_buttons": true}, nil, nil)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	w, _, _ := widgetlib.Detail(d, who, id)
	frag, err := widgetlib.Load(context.Background(), d, who, w, svcdata.Cached)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	views := frag.View["Hints"].([]hints.View)
	if _, levels := frag.View["Levels"]; levels || frag.View["Buttons"] != true || len(views) != 2 || views[0].Value != 5000 {
		t.Fatalf("view: %+v", frag.View)
	}
}
