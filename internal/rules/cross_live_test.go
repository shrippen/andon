package rules_test

import (
	"context"
	"sort"
	"testing"
	"time"

	"andon/internal/enums"
	"andon/internal/i18n"
	"andon/internal/rules"
	"andon/internal/sources"
	"andon/internal/testkit/live"
)

// crossWait bounds one dataset fetch against a real instance.
const crossWait = time.Minute

// TestCrossRulesLive runs every rule across services against the local
// instance's real data and logs its hints, to judge hits and false
// alarms by eye (ROADMAP „Querregeln mit echten Daten“). Only reads.
// A rule must not panic on real data; what it finds is not asserted.
//
//	connections ─fetch─► datasets by service (Merge where several) ─► Cross rules ─► t.Log
func TestCrossRulesLive(t *testing.T) {
	datasets := liveDatasets(t)
	env := rules.Env{Today: time.Now().UTC().Truncate(24 * time.Hour), Settings: map[string]any{}, Datasets: datasets}

	for _, spec := range rules.AllRules() {
		if spec.Scope != rules.Cross {
			continue
		}
		found := spec.Run(nil, spec.Defaults, env)
		if len(found) == 0 {
			t.Logf("%-28s –", spec.ID)
			continue
		}
		for _, f := range found {
			params := i18n.Typed(f.Params, enums.LocaleDE, enums.NotationDE)
			t.Logf("%-28s %v: %s", spec.ID, f.Severity, i18n.T("hint."+f.Message+".title", enums.LocaleDE, params))
			t.Logf("%-28s    %s", "", i18n.T("hint."+f.Message+".why", enums.LocaleDE, params))
		}
	}
}

// liveDatasets fetches each connection's dataset and keys it by service;
// a second connection of a service merges into the first where its
// dataset can (sources.Merger), else the first stays.
func liveDatasets(t *testing.T) map[string]any {
	t.Helper()
	out := map[string]any{}
	var names []string
	for _, inst := range live.Instances(t) {
		if inst.Err != nil {
			t.Logf("%s (%s): login: %v", inst.Name, inst.Service, inst.Err)
			continue
		}
		src, err := sources.Get(sources.DataKey(enums.ServiceType(inst.Service)))
		if err != nil {
			continue
		}
		ctx, cancel := context.WithTimeout(context.Background(), crossWait)
		data, err := src.Fetch(ctx, inst.Ctx)
		cancel()
		if err != nil || data == nil {
			t.Logf("%s (%s): %v", inst.Name, inst.Service, err)
			continue
		}

		key := string(inst.Service)
		names = append(names, key)
		have, ok := out[key]
		if !ok {
			out[key] = data
			continue
		}
		if m, ok := have.(sources.Merger); ok {
			out[key] = m.Merge(data)
		}
	}
	sort.Strings(names)
	t.Logf("datasets: %v", names)
	return out
}
