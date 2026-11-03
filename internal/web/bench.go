package web

// Forms of the workbenches (services/workbench). Each ticked row sends
// its case and fields, suffixed with the case:
//
//	pick=Kx9  act_Kx9=create  vendor_Kx9=Nordhost  tax_Kx9=19
//
// only=Kx9 takes that row alone (a desk's "review this one").
//
// The review page sends the same back (template bench_step), so Apply
// writes exactly what the user saw.
//
// A form with data-draft keeps what the user changed (workbench.Draft):
// each change posts the case's fields, the page puts them back on load.
//
//	POST /bench/{id}/draft       place=v4/2026-Q3  cases={"Kx9": {"pick": ["Kx9"], …}}
//	POST /bench/{id}/draft/drop  place=…  back=referer

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"

	"andon/internal/services/workbench"
)

// benchPick names the ticked rows; benchAct prefixes a row's action;
// benchScope carries the Verbund a bench works in (workbench.Scope);
// benchOnly names one row to review and write alone.
const (
	benchPick  = "pick"
	benchAct   = "act_"
	benchScope = "scope"
	benchOnly  = "only"
)

// Form fields of a draft's posts.
const (
	draftPlace = "place"
	draftCases = "cases"
)

// sealedSuffix marks a sealed JSON key (repos/misc).
const sealedSuffix = "_enc"

// draftPlaceMax caps a place's key: a Verbund and a period.
const draftPlaceMax = 128

var errDraftShape = errors.New("workbench.err_draft_shape")

// benchSteps reads the ticked rows with the given fields; a row without
// an action is left out. only=<case> reads that row alone, ticked or
// not: one case written while the rest waits.
func benchSteps(form url.Values, fields []string) []workbench.Step {
	picks := form[benchPick]
	if k := form.Get(benchOnly); k != "" {
		picks = []string{k}
	}

	var out []workbench.Step
	for _, k := range picks {
		s := workbench.Step{Case: k, Action: form.Get(benchAct + k), Fields: map[string]string{}}
		if s.Action == "" {
			continue
		}
		for _, f := range fields {
			if v := form.Get(f + "_" + k); v != "" {
				s.Fields[f] = v
			}
		}
		out = append(out, s)
	}
	return out
}

// benchNote is the page's note after Apply: how many were done, skipped
// and failed, with the first failure's case and reason.
func benchNote(res workbench.Result) []string {
	pairs := []string{"done", strconv.Itoa(res.Done), "skipped", strconv.Itoa(res.Skipped)}
	if len(res.Failed) == 0 {
		return pairs
	}
	first := res.Failed[0]
	return append(pairs, "failed", strconv.Itoa(len(res.Failed)), "reason", first.Name+": "+first.Reason)
}

// handleDraft keeps the changed cases of a bench's form; 204, as the
// page posts in the background.
func (d Deps) handleDraft(w http.ResponseWriter, r *http.Request, ctx Ctx) {
	at, ok := draftAt(r)
	if !ok {
		http.NotFound(w, r)
		return
	}
	var cases workbench.Draft
	if err := json.Unmarshal([]byte(r.FormValue(draftCases)), &cases); err != nil || !draftShaped(cases) {
		d.fail(w, errDraftShape, http.StatusBadRequest)
		return
	}
	if err := workbench.KeepDraft(d.DB, ctx.Who, at, cases); err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, workbench.ErrDraftSize) {
			status = http.StatusRequestEntityTooLarge
		}
		d.fail(w, err, status)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleDraftDrop discards a bench's draft, then back to the page.
func (d Deps) handleDraftDrop(w http.ResponseWriter, r *http.Request, ctx Ctx) {
	at, ok := draftAt(r)
	if !ok {
		http.NotFound(w, r)
		return
	}
	if err := workbench.DropDraft(d.DB, ctx.Who, at); err != nil {
		d.fail(w, err, http.StatusInternalServerError)
		return
	}
	to, _ := url.Parse(backTo(r, benchURL(at.Bench)))
	q := to.Query()
	q.Set("note", "workbench.draft_dropped")
	to.RawQuery = q.Encode()
	http.Redirect(w, r, to.String(), http.StatusSeeOther)
}

// draftAt is the draft's place: a bench (benchURL) and a short key.
func draftAt(r *http.Request) (workbench.Place, bool) {
	at := workbench.Place{Bench: r.PathValue("id"), Key: r.FormValue(draftPlace)}
	if benchURL(at.Bench) == "" || len(at.Key) > draftPlaceMax {
		return at, false
	}
	return at, true
}

// draftShaped says whether each case holds only its own fields, as
// benchSteps reads them: pick with the case, <field>_<case>. No key may
// end in sealedSuffix: rotate-key takes such JSON keys for ciphertext
// (maintenance.sealedPurpose) and refuses a column it does not know.
func draftShaped(cases workbench.Draft) bool {
	for k, fields := range cases {
		if k == "" || strings.HasSuffix(k, sealedSuffix) {
			return false
		}
		for name, v := range fields {
			own := strings.HasSuffix(name, "_"+k) || (name == benchPick && !slices.ContainsFunc(v, func(s string) bool { return s != k }))
			if !own || strings.HasSuffix(name, sealedSuffix) {
				return false
			}
		}
	}
	return true
}
