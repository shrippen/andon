package metrics

import (
	"strconv"
	"strings"

	"andon/internal/bayes"
	"andon/internal/sources"
)

// Learned project and activity for a week gap, when the customer's
// latest entry leaves them open: a naive Bayes model over the words of
// Kimai's entries and of the gaps the user booked.
//
//	Kimai entry   w:<word> description, project,  ─┐
//	              customer, activity; c:<customer> │
//	booked gap    w:<word> title (appointment or  ─┼─► bayes.Model
//	              place);  c:<customer>            │   label "<project>:<activity>"
//	                                               ▼
//	gap ──► features ──► Best ≥ MinP, a known word, the gap's customer's project
//
// The customer is a feature, not evidence: every gap has one, and a
// customer alone would propose its most frequent pair. The model is
// stored per Kimai (services/learn).

// WeekFrom says where a gap's proposal comes from.
type WeekFrom string

const (
	WeekFromNothing WeekFrom = ""
	WeekFromLatest  WeekFrom = "latest"  // the customer's latest entry
	WeekFromLearned WeekFrom = "learned" // the model
)

// customerFeature prefixes a Kimai customer's feature.
const customerFeature = "c:"

// Example ids: a Kimai entry's, a booked gap's.
const (
	sheetExample = "sheet:"
	gapExample   = "gap:"
)

// labelSep parts a label's project id from its activity name.
const labelSep = ":"

// WeekGuesser proposes project and activity for one Kimai's gaps.
type WeekGuesser struct {
	learn bayes.Use
	owner map[int64]int64 // project → customer
}

// WeekGuesserOf proposes from l; l.Model learns k's entries first
// (WeekLearn), in memory.
func WeekGuesserOf(k *sources.KimaiDataset, l bayes.Use) WeekGuesser {
	g := WeekGuesser{learn: l, owner: map[int64]int64{}}
	if k == nil {
		return g
	}
	for _, p := range k.Projects {
		g.owner[p.ID] = p.CustomerID
	}
	if l.Model != nil {
		WeekLearn(l.Model, k)
	}
	return g
}

// WeekLearn teaches m k's entries with project and activity: new ones
// and changed ones; an entry gone from k stays learned. It says whether
// m changed.
func WeekLearn(m *bayes.Model, k *sources.KimaiDataset) bool {
	if k == nil {
		return false
	}
	projects := map[int64]string{}
	for _, p := range k.Projects {
		projects[p.ID] = p.Name
	}
	customers := KimaiCustomerNames(k)

	changed := false
	for _, s := range k.Timesheets {
		label := weekLabel(WeekProposal{ProjectID: s.ProjectID, Activity: s.Activity})
		if label == "" {
			continue
		}
		texts := []string{s.Notes, projects[s.ProjectID], customers[s.CustomerID], s.Activity}
		id := sheetExample + strconv.FormatInt(s.ID, 10)
		changed = m.Learn(id, label, weekFeatures(texts, s.CustomerID)) || changed
	}
	return changed
}

// WeekLearnGap teaches m that gap g was booked as p. It says whether m
// changed.
func WeekLearnGap(m *bayes.Model, g WeekGap, p WeekProposal) bool {
	label := weekLabel(p)
	if label == "" {
		return false
	}
	return m.Learn(gapExample+g.Key, label, g.features())
}

// Guess is the learned project and activity for g; only a project of
// g's customer counts: another's would leave the gap open.
func (w WeekGuesser) Guess(g WeekGap) (WeekProposal, bool) {
	best, ok := w.learn.Best(g.features(), IsWordFeature)
	if !ok {
		return WeekProposal{}, false
	}
	p, ok := weekParse(best.Label)
	if !ok || w.owner[p.ProjectID] != g.CustomerID {
		return WeekProposal{}, false
	}
	return p, true
}

// features are the gap's title words and its customer.
func (g WeekGap) features() []string {
	return weekFeatures([]string{g.Title}, g.CustomerID)
}

func weekFeatures(texts []string, customer int64) []string {
	var out []string
	for _, w := range words(texts) {
		out = append(out, wordFeature+w)
	}
	if customer > 0 {
		out = append(out, customerFeature+strconv.FormatInt(customer, 10))
	}
	return out
}

// weekLabel is p as a label: "12:Dreh"; "" without project or activity.
func weekLabel(p WeekProposal) string {
	if p.ProjectID <= 0 || p.Activity == "" {
		return ""
	}
	return strconv.FormatInt(p.ProjectID, 10) + labelSep + p.Activity
}

func weekParse(label string) (WeekProposal, bool) {
	id, activity, ok := strings.Cut(label, labelSep)
	project, err := strconv.ParseInt(id, 10, 64)
	if !ok || err != nil || project <= 0 || activity == "" {
		return WeekProposal{}, false
	}
	return WeekProposal{ProjectID: project, Activity: activity}, true
}
