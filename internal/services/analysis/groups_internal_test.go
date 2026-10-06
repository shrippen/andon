package analysis

import (
	"testing"
	"time"

	"andon/internal/model"
	linkrepo "andon/internal/repos/links"
	"andon/internal/rules"
	"andon/internal/services/svcdata"
	"andon/internal/sources"
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
	sc.split([]linkrepo.Link{{ID: 9, Members: []linkrepo.Member{{ConnID: 1, Service: "dawarich"}, {ConnID: 2, Service: "kimai"}}}}, nil)

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

// Three Docker hosts and two Proxmox, no Verbund: the cross checks run
// once per host (round-robin), so every host is read with the one Kuma;
// no "ambiguous" hint. Each host's own rules see the shared datasets.
func TestEnvsReadEverySpaceWideConnection(t *testing.T) {
	var conns []*model.Connection
	for i, s := range []string{"docker", "docker", "docker", "proxmox", "proxmox", "uptimekuma"} {
		conns = append(conns, &model.Connection{ID: int64(i + 1), Service: s})
	}
	runOf := func(c *model.Connection) run { return run{conn: c, result: svcdata.Result{Data: c.ID}} }
	sc := newScope(1, nil, map[string]any{})
	for _, c := range conns {
		sc.fetched = append(sc.fetched, runOf(c))
		sc.datasets[c.Service] = c.ID
	}
	sc.split(nil, nil)

	if len(sc.vague) != 0 || len(sc.groups) != 3 {
		t.Fatalf("vague %v groups %d", sc.vague, len(sc.groups))
	}
	docker, proxmox := map[any]bool{}, map[any]bool{}
	for _, g := range sc.groups {
		docker[g.datasets["docker"]] = true
		proxmox[g.datasets["proxmox"]] = true
		if g.datasets["uptimekuma"] != int64(6) {
			t.Fatalf("kuma missing: %v", g.datasets)
		}
	}
	if len(docker) != 3 || len(proxmox) != 2 {
		t.Fatalf("docker %v proxmox %v", docker, proxmox)
	}

	envs := sc.envsOf(runOf(conns[1]), nil, time.Now())
	if len(envs) != 1 || envs[0].Datasets["docker"] != int64(2) || envs[0].Datasets["uptimekuma"] != int64(6) {
		t.Fatalf("docker 2 envs: %+v", envs)
	}
}

// Datasets that merge become the space's: one group, all three hosts'
// containers in it, every host counted as part of it.
func TestEnvsMergeSpaceWideDatasets(t *testing.T) {
	sc := newScope(1, nil, map[string]any{})
	for i, name := range []string{"web", "db", "cache"} {
		c := &model.Connection{ID: int64(i + 1), Service: "docker"}
		data := &sources.DockerDataset{Containers: []sources.Container{{Name: name}}}
		sc.fetched = append(sc.fetched, run{conn: c, result: svcdata.Result{Data: data}})
		sc.datasets["docker"] = data
	}
	sc.split(nil, nil)
	if len(sc.groups) != 1 || len(sc.vague) != 0 {
		t.Fatalf("groups %d vague %v", len(sc.groups), sc.vague)
	}
	merged, _ := sc.groups[0].datasets["docker"].(*sources.DockerDataset)
	if merged == nil || len(merged.Containers) != 3 || !sc.groups[0].conns[2] {
		t.Fatalf("merged %+v conns %v", merged, sc.groups[0].conns)
	}
}
