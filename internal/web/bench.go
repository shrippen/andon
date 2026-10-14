package web

// Forms of the workbenches (services/workbench). Each ticked row sends
// its case and fields, suffixed with the case:
//
//	pick=Kx9  act_Kx9=create  vendor_Kx9=Nordhost  tax_Kx9=19
//
// The review page sends the same back (template bench_step), so Apply
// writes exactly what the user saw.

import (
	"net/url"
	"strconv"

	"andon/internal/services/workbench"
)

// benchPick names the ticked rows; benchAct prefixes a row's action;
// benchScope carries the Verbund a bench works in (workbench.Scope).
const (
	benchPick  = "pick"
	benchAct   = "act_"
	benchScope = "scope"
)

// benchSteps reads the ticked rows with the given fields; a row without
// an action is left out.
func benchSteps(form url.Values, fields []string) []workbench.Step {
	var out []workbench.Step
	for _, k := range form[benchPick] {
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
