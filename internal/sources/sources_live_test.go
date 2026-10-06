package sources_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"andon/internal/enums"
	"andon/internal/sources"
	"andon/internal/testkit/live"
)

// fetchWait bounds one fetch against a real instance.
const fetchWait = time.Minute

// Every connection of the local instance answers its connection check
// and its dataset: the parsers read what the real services send. Only
// reads; sources never write.
func TestSourcesLive(t *testing.T) {
	for _, inst := range live.Instances(t) {
		t.Run(inst.Name, func(t *testing.T) {
			if inst.Err != nil {
				t.Fatalf("%s login: %v", inst.Service, inst.Err)
			}
			svc := enums.ServiceType(inst.Service)
			for _, key := range []string{string(svc) + ".test", sources.DataKey(svc)} {
				fetchLive(t, key, inst.Ctx)
			}
		})
	}
}

// fetchLive runs one source, if the service has it, and logs the size
// of its answer.
func fetchLive(t *testing.T, key string, sctx sources.Ctx) {
	t.Helper()
	src, err := sources.Get(key)
	if err != nil {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), fetchWait)
	defer cancel()
	out, err := src.Fetch(ctx, sctx)
	if err != nil {
		t.Errorf("%s: %v", key, err)
		return
	}
	if out == nil {
		t.Errorf("%s: no data", key)
		return
	}
	raw, _ := json.Marshal(out)
	t.Logf("%s: %d bytes", key, len(raw))
}
