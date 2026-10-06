package analysis

import (
	"testing"
	"time"

	"andon/internal/model"
	linkrepo "andon/internal/repos/links"
	"andon/internal/rules"
	"andon/internal/services/svcdata"
)

// A connection's rules read the other services of its own Verbund: the
// Dawarich linked with Kimai 1 never sees Kimai 2's data; a Kimai in no
// group sees only its own.
func TestEnvsFollowVerbund(t *testing.T) {
	daw := &model.Connection{ID: 1, Service: "dawarich"}
	k1 := &model.Connection{ID: 2, Service: "kimai"}
	k2 := &model.Connection{ID: 3, Service: "kimai"}
	k3 := &model.Connection{ID: 4, Service: "kimai"}
	runOf := func(c *model.Connection) run {
		return run{conn: c, result: svcdata.Result{Data: c.ID}}
	}

	sc := newScope(1, nil, map[string]any{})
	sc.datasets[rules.FailedDataset] = []rules.Failed{}
	for _, c := range []*model.Connection{daw, k1, k2, k3} {
		sc.fetched = append(sc.fetched, runOf(c))
		sc.datasets[c.Service] = c.ID // last one wins, as runSpace does
	}
	sc.split([]linkrepo.Link{{ID: 9, Members: []linkrepo.Member{{ConnID: 1, Service: "dawarich"}, {ConnID: 2, Service: "kimai"}}}})

	envs := sc.envsOf(runOf(daw), nil, time.Now())
	if len(envs) != 1 || envs[0].Datasets["kimai"] != int64(2) {
		t.Fatalf("dawarich env: %+v", envs)
	}
	if _, ok := envs[0].Datasets[rules.FailedDataset]; !ok {
		t.Fatal("space-wide dataset lost")
	}

	// Kimai 2 and 3 are both free: ambiguous, each alone.
	if len(sc.vague) != 1 || sc.vague[0] != "kimai" {
		t.Fatalf("vague: %v", sc.vague)
	}
	envs = sc.envsOf(runOf(k2), nil, time.Now())
	if len(envs) != 1 || envs[0].Datasets["kimai"] != int64(3) || envs[0].Datasets["dawarich"] != nil {
		t.Fatalf("kimai 2 env: %+v", envs[0].Datasets)
	}
}
