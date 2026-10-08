package sources_test

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"andon/internal/caps"
	"andon/internal/enums"
	"andon/internal/sources"
	"andon/internal/testkit/live"
)

// capSetter is a dataset that reports what its connection can do.
type capSetter interface{ CapSet() caps.Set }

// Every connection of the local instance answers its connection check
// and its dataset: the parsers read what the real services send. Only
// reads; sources never write. Each dataset lands in
// .local-test/datasets/ for reading by eye.
func TestSourcesLive(t *testing.T) {
	for _, inst := range live.Instances(t) {
		t.Run(inst.Name, func(t *testing.T) {
			if inst.Err != nil {
				t.Fatalf("%s login: %v", inst.Service, inst.Err)
			}
			svc := enums.ServiceType(inst.Service)
			fetchLive(t, inst, string(svc)+".test")

			out := fetchLive(t, inst, sources.DataKey(svc))
			if out == nil {
				return
			}
			live.Dump(t, inst.Name, out)
			logCaps(t, out)

			// Empty fields hint at a parser reading the wrong names.
			if empty := emptyFields(out); len(empty) > 0 {
				t.Logf("empty: %s", strings.Join(empty, ", "))
			}
		})
	}
}

// fetchLive runs one source, if the service has it, and logs the size
// of its answer.
func fetchLive(t *testing.T, inst live.Instance, key string) any {
	t.Helper()
	out, err := inst.Fetch(key)
	if err != nil {
		t.Errorf("%s: %v", key, err)
		return nil
	}
	if out == nil {
		if _, err := sources.Get(key); err == nil {
			t.Errorf("%s: no data", key)
		}
		return nil
	}
	raw, _ := json.Marshal(out)
	t.Logf("%s: %d bytes", key, len(raw))
	return out
}

// logCaps logs what caps.Detect found, e.g. "missing: places/update
// (plugin mileage)".
func logCaps(t *testing.T, out any) {
	t.Helper()
	cs, ok := out.(capSetter)
	if !ok {
		return
	}

	set := cs.CapSet()
	var have []string
	for _, c := range set.Have {
		have = append(have, string(c.Domain)+"/"+string(c.Op))
	}
	t.Logf("caps: %s", strings.Join(have, ", "))
	for _, g := range set.Missing {
		t.Logf("missing: %s/%s (%+v)", g.Cap.Domain, g.Cap.Op, g.Need)
	}
}

// emptyFields names the exported top-level fields of a dataset struct
// that hold their zero value.
func emptyFields(out any) []string {
	v := reflect.Indirect(reflect.ValueOf(out))
	if v.Kind() != reflect.Struct {
		return nil
	}

	var empty []string
	for i := range v.NumField() {
		f := v.Type().Field(i)
		if !f.IsExported() || !v.Field(i).IsZero() {
			continue
		}
		empty = append(empty, f.Name)
	}
	return empty
}
