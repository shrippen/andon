package widgets_test

import (
	"slices"
	"testing"

	"andon/internal/widgets"
)

// Tiles that read partner services offer the Verbund field; others not.
// The form keeps a picked Verbund and drops "automatic".
func TestVerbundField(t *testing.T) {
	has := func(key string) bool {
		return slices.ContainsFunc(widgets.FieldsOf(key), func(f widgets.Field) bool { return f.Key == widgets.VerbundKey })
	}
	if !has("kpi") {
		t.Fatal("kpi reads Kimai as peer, no Verbund field")
	}
	if has("clock") {
		t.Fatal("clock has a Verbund field")
	}

	form := map[string]string{widgets.FormPrefix + widgets.VerbundKey: "7"}
	cfg := widgets.ParseForm("kpi", func(name string) string { return form[name] })
	if cfg[widgets.VerbundKey] != 7.0 {
		t.Fatalf("picked: %+v", cfg)
	}
	form[widgets.FormPrefix+widgets.VerbundKey] = ""
	if cfg := widgets.ParseForm("kpi", func(name string) string { return form[name] }); cfg[widgets.VerbundKey] != nil {
		t.Fatalf("automatic stored: %+v", cfg)
	}
}
