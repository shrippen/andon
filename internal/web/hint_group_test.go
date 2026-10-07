package web

import (
	"testing"

	"andon/internal/enums"
	"andon/internal/services/hints"
)

// By client, a client's hints of different rules stand together; hints
// without a client stay grouped by rule.
func TestGroupHintsByClient(t *testing.T) {
	views := []hints.View{
		{ID: 1, Rule: "in.invoice_overdue", Client: "Nivre"},
		{ID: 2, Rule: "in.slow_payer", Client: "Nivre"},
		{ID: 3, Rule: "scrutiny.failing"},
	}
	byRule := groupHints(views, groupRule)
	byClient := groupHints(views, groupClient)
	if len(byRule) != 3 || len(byClient) != 2 || byClient[0].Client != "Nivre" || byClient[0].Count() != 2 || byClient[1].Rule != "scrutiny.failing" {
		t.Fatalf("by rule %d, by client %+v", len(byRule), byClient)
	}
}

// Only a rule with two or more hints forms a group (head and bulk bar);
// single hints of neighbouring rules stand together as plain rows.
func TestGroupsNeedTwoHints(t *testing.T) {
	views := []hints.View{
		{ID: 1, Rule: "a"}, {ID: 2, Rule: "a"},
		{ID: 3, Rule: "b"},
		{ID: 4, Rule: "c"},
		{ID: 5, Rule: "d"}, {ID: 6, Rule: "d"},
		{ID: 7, Rule: "e"},
	}
	got := loosen(groupHints(views, groupRule))
	if len(got) != 4 {
		t.Fatalf("want 4 sections (a, b+c, d, e), got %+v", got)
	}
	if got[0].Loose || got[0].Count() != 2 || !got[1].Loose || got[1].Count() != 2 || got[2].Loose || !got[3].Loose || got[3].Count() != 1 {
		t.Fatalf("sections %+v", got)
	}
	if got[1].Shown[0].ID != 3 || got[1].Shown[1].ID != 4 {
		t.Fatalf("loose rows out of order: %+v", got[1].Shown)
	}
}

// One list of rows in the "single" layout, in the page's order.
func TestSingleLayoutIsOneList(t *testing.T) {
	views := []hints.View{{ID: 1, Rule: "a"}, {ID: 2, Rule: "b"}, {ID: 3, Rule: "a"}}
	got := arrange(views, groupRule, hints.LayoutSingle)
	if len(got) != 1 || !got[0].Loose || got[0].Count() != 3 || got[0].Shown[2].ID != 3 {
		t.Fatalf("single layout %+v", got)
	}
	if grouped := arrange(views, groupRule, hints.LayoutGrouped); len(grouped) != 2 || grouped[0].Loose || !grouped[1].Loose {
		t.Fatalf("grouped layout %+v", grouped)
	}
}

// The filter side counts each level under the service filter and each
// service under the level filter; the top services show, the rest fold.
func TestHintSideCounts(t *testing.T) {
	var views []hints.View
	add := func(n int, sev enums.Severity, src string) {
		for range n {
			views = append(views, hints.View{ID: int64(len(views) + 1), Severity: sev, Sources: []string{src}})
		}
	}
	add(2, enums.SeverityCritical, "kimai")
	add(3, enums.SeverityWarn, "kimai")
	add(1, enums.SeverityInfo, "borg")
	for i, src := range []string{"s1", "s2", "s3", "s4", "s5", "s6"} {
		add(i+1, enums.SeverityInfo, src)
	}

	side := sideOf(views, hintFilter{})
	if side.LevelAll != len(views) || side.SourceAll != len(views) || side.Active != 0 {
		t.Fatalf("totals %+v", side)
	}
	if len(side.Sources) != sideTop || len(side.More) != 8-sideTop || side.MoreOpen {
		t.Fatalf("top %+v, more %+v", side.Sources, side.More)
	}

	side = sideOf(views, hintFilter{Source: "kimai", Level: "warn"})
	if side.LevelAll != 5 || side.SourceAll != 3 || side.Active != 2 {
		t.Fatalf("filtered totals %+v", side)
	}
	if side.Levels[0].Key != "critical" || side.Levels[0].N != 2 || side.Levels[1].N != 3 {
		t.Fatalf("levels under kimai %+v", side.Levels)
	}

	if side = sideOf(views, hintFilter{Source: "borg"}); !side.MoreOpen {
		t.Fatalf("a chosen service below the top stays visible: %+v", side)
	}
}

// A rule without its own group is still reachable as #rule-<id>: its
// first row carries the anchor, once.
func TestRuleAnchorsOnRows(t *testing.T) {
	groups := arrange([]hints.View{{ID: 1, Rule: "a"}, {ID: 2, Rule: "a"}, {ID: 3, Rule: "b"}}, groupRule, hints.LayoutSingle)
	got := ruleAnchors(groups)
	if len(got) != 2 || got[1] != "a" || got[3] != "b" {
		t.Fatalf("anchors %v", got)
	}
	grouped := arrange([]hints.View{{ID: 1, Rule: "a"}, {ID: 2, Rule: "a"}, {ID: 3, Rule: "b"}}, groupRule, hints.LayoutGrouped)
	if got = ruleAnchors(grouped); len(got) != 1 || got[3] != "b" {
		t.Fatalf("grouped anchors %v", got)
	}
}
