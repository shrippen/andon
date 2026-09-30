package themes

import "testing"

// Every preset reaches WCAG AA for the contract's text pairs.
func TestPresetsContrast(t *testing.T) {
	for _, name := range Presets() {
		tokens := presets[name].tokens()
		for _, issue := range ContrastIssues(tokens, tokens) {
			if issue.Mode == ModeDark {
				t.Errorf("%s: %s on %s = %.2f", name, issue.FG, issue.BG, issue.Ratio)
			}
		}
	}
}

func TestDashyPalette(t *testing.T) {
	p, ok := DashyPalette("Nord", map[string]string{"primary": "#ff0000", "background": "url(x)"})
	if !ok || p.Accent != "#ff0000" || p.Bg != presets["nord"].Bg {
		t.Fatalf("palette: %+v", p)
	}
	if _, ok := DashyPalette("unknown", nil); ok {
		t.Fatal("unknown theme without colors used")
	}
}

// A preset colours Kante's link and focus role (--cyan) with its blue.
func TestPresetCyan(t *testing.T) {
	p := presets["nord"]
	tokens := p.tokens()
	if tokens["--cyan"] != p.Blue {
		t.Fatalf("--cyan = %s, want %s", tokens["--cyan"], p.Blue)
	}
	for _, name := range []string{"--cyan-n", "--cyan-tint", "--yellow-hi", "--yellow-lo"} {
		if !hexColor.MatchString(tokens[name]) {
			t.Errorf("%s = %q, want a colour", name, tokens[name])
		}
	}
}

// The text roles start from the warning and error roles, so a theme that
// repoints --warn gets a matching --warn-text.
func TestTextRolesFollowRoles(t *testing.T) {
	dark, _ := Contract()
	tokens := merge(dark, map[string]string{"--warn": "var(--purple)"})
	roles := TextRoles(tokens)
	if roles["--warn-text"] == roles["--danger-text"] || !hexColor.MatchString(roles["--warn-text"]) {
		t.Fatalf("warn-text %q, danger-text %q", roles["--warn-text"], roles["--danger-text"])
	}
	if resolveVar(tokens, "--warn") != tokens["--purple"] {
		t.Fatal("resolveVar did not follow --warn")
	}
}
