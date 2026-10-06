package sources_test

import (
	"testing"
	"time"

	"andon/internal/sources"
)

// Two Docker hosts and two Borg servers merge into the space's; the
// cached datasets stay as they were.
func TestMergeSpaceWideDatasets(t *testing.T) {
	a := &sources.DockerDataset{URL: "a", Containers: []sources.Container{{Name: "web"}}}
	b := &sources.DockerDataset{URL: "b", Containers: []sources.Container{{Name: "db"}}}
	m := a.Merge(b).(*sources.DockerDataset)
	if len(m.Containers) != 2 || len(a.Containers) != 1 || len(b.Containers) != 1 || m.URL != "a" {
		t.Fatalf("docker %+v a %+v", m, a)
	}

	early, late := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC), time.Date(2026, 10, 5, 0, 0, 0, 0, time.UTC)
	b1 := &sources.BorgDataset{Clients: []sources.BorgClient{{}}, Failed24h: 1, LastBackup: early}
	b2 := &sources.BorgDataset{Clients: []sources.BorgClient{{}, {}}, Failed24h: 2, LastBackup: late, ServerUpdate: true}
	mb := b1.Merge(b2).(*sources.BorgDataset)
	if len(mb.Clients) != 3 || mb.Failed24h != 3 || !mb.LastBackup.Equal(late) || !mb.ServerUpdate || b1.Failed24h != 1 {
		t.Fatalf("borg %+v", mb)
	}

	p1 := &sources.ProxmoxDataset{Backups: map[int64]time.Time{1: early}}
	p2 := &sources.ProxmoxDataset{Backups: map[int64]time.Time{2: late}}
	if mp := p1.Merge(p2).(*sources.ProxmoxDataset); len(mp.Backups) != 2 || len(p1.Backups) != 1 {
		t.Fatalf("proxmox %+v", mp)
	}

	// Another type leaves the receiver as it is.
	if got := a.Merge(b1); got != any(a) {
		t.Fatalf("foreign merge %+v", got)
	}
}
