package web

import (
	"andon/internal/enums"
	"andon/internal/services/access"
	"andon/internal/services/boards"
	"andon/internal/services/hints"
	"andon/internal/services/release"
	"andon/internal/services/workbench"
)

// addNav fills what the app header shows on every page: the viewer's
// boards, the open hint count with its highest severity and the benches
// (benches.go). It only decorates the page, so a failed lookup leaves
// the entry out.
func (d Deps) addNav(data map[string]any, who *access.Principal) {
	if _, ok := data["CurBoard"]; !ok {
		data["CurBoard"] = int64(0)
	}
	if _, ok := data["NavBoards"]; !ok {
		if list, err := boards.Nav(d.DB, who); err == nil {
			data["NavBoards"] = list
		}
	}

	tally, err := hints.Count(d.DB, who)
	if err != nil {
		return
	}
	favs, err := workbench.LoadFavs(d.DB, who)
	if err == nil {
		data["NavBenchGroups"], data["NavBenches"] = benchMenu(tally, favs)
	}

	total, level := 0, enums.Severity(0)
	for sev, n := range tally.Levels {
		total += n
		if n > 0 && sev > level {
			level = sev
		}
	}
	data["HintOpen"], data["HintLevel"] = total, int(level)

	// Switched off, the hint goes too, not only the daily check.
	if d.Settings.Demo || !d.live().UpdateCheck {
		return
	}
	if r, ok := release.Pending(d.DB, who, d.Settings.DataDir); ok {
		data["Release"] = r
	}
}

// sectionRun is one section on the main grid, or a run of neighbouring
// flowing sections set in newspaper columns (see boards.SpanFlow).
type sectionRun struct {
	Flow     bool
	Sections []boards.SectionView
}

// mainRuns groups the main-area sections for the board template:
//
//	[full] [flow flow flow] [third] [flow]  →  4 runs, the flows grouped
func mainRuns(sections []boards.SectionView) []sectionRun {
	var out []sectionRun
	for _, s := range sections {
		if s.Area == areaSide {
			continue
		}

		flow := s.Span == boards.SpanFlow
		if flow && len(out) > 0 && out[len(out)-1].Flow {
			last := &out[len(out)-1]
			last.Sections = append(last.Sections, s)
			continue
		}
		out = append(out, sectionRun{Flow: flow, Sections: []boards.SectionView{s}})
	}
	return out
}

const areaSide = "side"
