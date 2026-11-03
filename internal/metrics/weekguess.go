package metrics

import (
	"slices"
	"strconv"
	"strings"

	"andon/internal/bayes"
	"andon/internal/sources"
)

// Learned project and activity suggested for a week gap ("Vorschlag:"
// under its selects; nothing is filled in): a naive Bayes model over the
// words of Kimai's entries and of the gaps the user booked.
//
//	Kimai entry   w:<word> description, project,  ─┐
//	              customer, activity; c:<customer> │
//	booked gap    w:<word> title (appointment or  ─┼─► bayes.Model
//	              place);  c:<customer>            │   label "<project>:<activity>"
//	                                               ▼
//	gap ──► features without its names ──► Suggest (best ≥ MinP, a known word), its customer's projects
//
// A gap's words from its customer's name and its projects' names are
// left out when guessing (c:<customer> stands for them): every gap
// names one of them (that is how it found its customer), so they would
// propose the customer's most frequent pair ("Telefonat Fotohaus Elbe"
// → Dreh). Evidence is another known word ("Schnitt", a place). So a
// Dawarich place named only like the customer ("Fotohaus Elbe") gets no
// learned suggestion, also after booking it. The model is stored per
// Kimai (services/learn).

// customerFeature prefixes a Kimai customer's feature.
const customerFeature = "c:"

// Example ids: a Kimai entry's, a booked gap's.
const (
	sheetExample = "sheet:"
	gapExample   = "gap:"
)

// labelSep parts a label's project id from its activity name.
const labelSep = ":"

// WeekGuesser suggests project and activity for one Kimai's gaps.
type WeekGuesser struct {
	learn bayes.Use
	owner map[int64]int64           // project → customer
	named map[int64]map[string]bool // customer → its and its projects' name words
}

// WeekGuesserOf suggests from l; l.Model learns k's entries first
// (WeekLearn), in memory.
func WeekGuesserOf(k *sources.KimaiDataset, l bayes.Use) WeekGuesser {
	g := WeekGuesser{learn: l, owner: map[int64]int64{}, named: map[int64]map[string]bool{}}
	if k == nil {
		return g
	}
	for _, c := range k.Customers {
		g.name(c.ID, c.Name)
	}
	for _, p := range k.Projects {
		g.owner[p.ID] = p.CustomerID
		g.name(p.CustomerID, p.Name)
	}
	if l.Model != nil {
		WeekLearn(l.Model, k)
	}
	return g
}

// name notes a name's words as customer's, left out of its gaps' guess.
func (w WeekGuesser) name(customer int64, name string) {
	if w.named[customer] == nil {
		w.named[customer] = map[string]bool{}
	}
	for _, f := range weekFeatures([]string{name}, 0) {
		w.named[customer][f] = true
	}
}

// WeekLearn teaches m k's entries with project and activity: new ones
// and changed ones. An entry gone from k (deleted, or older than k's
// window) is forgotten, so the model does not grow without bound;
// booked gaps stay. k without entries forgets nothing. It says whether
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
	current := map[string]bool{}
	for _, s := range k.Timesheets {
		label := weekLabel(WeekProposal{ProjectID: s.ProjectID, Activity: s.Activity})
		if label == "" {
			continue
		}
		texts := []string{s.Notes, projects[s.ProjectID], customers[s.CustomerID], s.Activity}
		id := sheetExample + strconv.FormatInt(s.ID, 10)
		current[id] = true
		changed = m.Learn(id, label, weekFeatures(texts, s.CustomerID)) || changed
	}
	if len(k.Timesheets) == 0 {
		return changed
	}

	gone := func(id string) bool { return strings.HasPrefix(id, sheetExample) && !current[id] }
	return m.Prune(gone) || changed
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

// Suggest is the learned pairs for g, likeliest first, at most max;
// none unless the likeliest is a project of g's customer (another's
// would leave the gap open), and only its customer's after it.
func (w WeekGuesser) Suggest(g WeekGap, max int) []WeekProposal {
	// The customer's and its projects' names leave: c:<customer> says it.
	features := slices.DeleteFunc(g.features(), func(f string) bool { return w.named[g.CustomerID][f] })
	var out []WeekProposal
	for i, guess := range w.learn.Suggest(features, IsWordFeature, max) {
		p, ok := weekParse(guess.Label)
		if ok && w.owner[p.ProjectID] == g.CustomerID {
			out = append(out, p)
			continue
		}
		if i == 0 {
			return nil
		}
	}
	return out
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
