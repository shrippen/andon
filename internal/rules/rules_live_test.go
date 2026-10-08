package rules_test

import (
	"fmt"
	"sort"
	"testing"
	"time"

	"andon/internal/enums"
	"andon/internal/rules"
	"andon/internal/sources"
	"andon/internal/testkit/live"
)

// findingsDump is the file in .local-test/datasets/ the findings land in.
const findingsDump = "findings"

// liveFinding is one finding as read by eye: which connection it came
// from ("cross" for cross rules) and what it says.
type liveFinding struct {
	From        string
	Rule        string
	Severity    string
	Fingerprint string
	Message     string
	Params      map[string]any
}

// The rules run over the real datasets of the local instance, with
// their defaults, as one space: are the findings plausible, are there
// false alarms? Only reads; nothing is stored as a hint.
//
//	connection datasets ──► service rules ─┐
//	        │                              ├─► findings.json + log
//	        └─ merged (Docker, Borg …) ─► cross rules
func TestRulesLive(t *testing.T) {
	type fetched struct {
		inst live.Instance
		data any
	}

	// Fetch every connection once; failures count as an outage.
	var runs []fetched
	env := rules.Env{Today: time.Now(), Settings: map[string]any{}, Datasets: map[string]any{}, Options: map[string]map[string]any{}}
	var failed []rules.Failed
	for _, inst := range live.Instances(t) {
		data, err := inst.Fetch(sources.DataKey(enums.ServiceType(inst.Service)))
		if err != nil || data == nil || inst.Err != nil {
			t.Logf("%s: no data (%v)", inst.Name, err)
			failed = append(failed, rules.Failed{Service: string(inst.Service), Name: inst.Name, Host: rules.HostOf(inst.Ctx.URL)})
			continue
		}
		runs = append(runs, fetched{inst, data})
		addDataset(env, string(inst.Service), data, inst.Ctx.Options)
	}
	env.Datasets[rules.FailedDataset] = failed

	// Service rules per connection, then the cross rules once.
	var found []liveFinding
	for _, r := range runs {
		found = append(found, runSpecs(t, rules.ForScope(string(r.inst.Service)), r.data, env, r.inst.Name)...)
	}
	found = append(found, runSpecs(t, rules.ForScope(rules.Cross), nil, env, rules.Cross)...)

	sort.Slice(found, func(i, j int) bool {
		if found[i].Rule != found[j].Rule {
			return found[i].Rule < found[j].Rule
		}
		return found[i].Fingerprint < found[j].Fingerprint
	})
	for _, f := range found {
		t.Logf("%-8s %-36s %s %v", f.Severity, f.Rule, f.Fingerprint, f.Params)
	}
	t.Logf("%d findings from %d datasets", len(found), len(runs))
	live.Dump(t, findingsDump, found)
}

// addDataset adds one connection's dataset to the space; a second one
// of the same service merges if it can (sources.Merger), else the
// first stays, as in a space without Verbund.
func addDataset(env rules.Env, service string, data any, options map[string]any) {
	prev, ok := env.Datasets[service]
	if !ok {
		env.Datasets[service] = data
		env.Options[service] = options
		return
	}
	if m, ok := prev.(sources.Merger); ok {
		env.Datasets[service] = m.Merge(data)
	}
}

// runSpecs runs specs with their defaults, skipping those whose inputs
// failed; a rule's panic fails the test.
func runSpecs(t *testing.T, specs []rules.Spec, data any, env rules.Env, from string) []liveFinding {
	t.Helper()
	var out []liveFinding
	for _, spec := range specs {
		if spec.Incomplete(env) {
			continue
		}
		cfg := rules.Config(spec, env.Settings)
		found, err := safeRun(spec, data, cfg, env)
		if err != nil {
			t.Errorf("%s on %s: %v", spec.ID, from, err)
			continue
		}
		for _, f := range rules.Filter(found, cfg) {
			out = append(out, liveFinding{From: from, Rule: f.Rule, Severity: f.Severity.Key(), Fingerprint: f.Fingerprint, Message: f.Message, Params: f.Params})
		}
	}
	return out
}

// safeRun returns a rule's panic as an error, so the other rules still
// run.
func safeRun(spec rules.Spec, data any, cfg map[string]any, env rules.Env) (out []rules.Finding, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("panic: %v", r)
		}
	}()
	return spec.Run(data, cfg, env), nil
}
