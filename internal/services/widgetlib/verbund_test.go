package widgetlib_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"andon/internal/db/dbtest"
	"andon/internal/enums"
	"andon/internal/model"
	"andon/internal/repos/content"
	"andon/internal/services/access"
	"andon/internal/services/svcdata"
	"andon/internal/services/verbund"
	"andon/internal/services/widgetlib"
	"andon/internal/sources"
)

// A tile whose peer service has two connections says so instead of
// taking the first; its connection's Verbund settles it.
func TestPeerAmbiguousUntilVerbund(t *testing.T) {
	d := openTestDB(t)
	u := addUser(t, d, "amb@b.c")
	who, _ := access.Load(d, u.ID)
	space, _ := content.PersonalSpace(d, u.ID)

	add := func(svc enums.ServiceType, suffix string) int64 {
		c := &model.Connection{SpaceID: space.ID, Key: string(svc) + suffix, Name: string(svc) + suffix, Service: string(svc),
			URL: "demo://" + string(svc) + "/amb" + suffix, CredentialMode: enums.CredentialShared, VerifyTLS: true, CreatedAt: time.Now().UTC()}
		if err := content.AddConnection(d, c); err != nil {
			t.Fatal(err)
		}
		return c.ID
	}
	add(enums.ServiceKimai, "1")
	kimai2 := add(enums.ServiceKimai, "2")
	ninja := add(enums.ServiceInvoiceNinja, "")

	id, err := widgetlib.Create(d, who, space.ID, "kpi", "Rate", map[string]any{"metric": "effective_rate"}, &ninja, nil)
	if err != nil {
		t.Fatal(err)
	}
	w, _, _ := widgetlib.Detail(d, who, id)
	frag, err := widgetlib.Load(context.Background(), d, who, w, svcdata.Cached)
	if err != nil {
		t.Fatal(err)
	}
	if frag.Slots["kimai"].Error != verbund.ErrAmbiguous.Error() {
		t.Fatalf("two kimai: %+v", frag.Slots["kimai"])
	}

	if _, err := verbund.Create(d, who, "Firma", []int64{ninja, kimai2}, ""); err != nil {
		t.Fatal(err)
	}
	// Wait for the background fill, so the database outlives it.
	for range 100 {
		frag, err = widgetlib.Load(context.Background(), d, who, w, svcdata.Cached)
		if err != nil {
			t.Fatal(err)
		}
		if !frag.Slots["data"].Pending && !frag.Slots["kimai"].Pending {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if frag.Slots["kimai"].Error != "" || frag.Slots["kimai"].Data == nil {
		t.Fatalf("with verbund: %+v", frag.Slots["kimai"])
	}
}

// The backups tile reads every Borg server of the space: Borg pairs with
// nothing, so two servers are no reason to ask, and their data merges.
func TestPeerSpaceWideMerges(t *testing.T) {
	d := openTestDB(t)
	u := addUser(t, d, "fan@b.c")
	who, _ := access.Load(d, u.ID)
	space, _ := content.PersonalSpace(d, u.ID)
	for _, suffix := range []string{"1", "2"} {
		c := &model.Connection{SpaceID: space.ID, Key: "borg" + suffix, Name: "Borg " + suffix, Service: string(enums.ServiceBorgBackup),
			URL: "demo://borgbackup/fan" + suffix, CredentialMode: enums.CredentialShared, VerifyTLS: true, CreatedAt: time.Now().UTC()}
		if err := content.AddConnection(d, c); err != nil {
			t.Fatal(err)
		}
	}
	id, err := widgetlib.Create(d, who, space.ID, "backups", "Backups", map[string]any{}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	w, _, _ := widgetlib.Detail(d, who, id)
	var frag *widgetlib.Fragment
	for range 200 {
		if frag, err = widgetlib.Load(context.Background(), d, who, w, svcdata.Cached); err != nil {
			t.Fatal(err)
		}
		if !frag.Slots["borgbackup"].Pending {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	slot := frag.Slots["borgbackup"]
	borg, _ := slot.Data.(*sources.BorgDataset)
	one := sources.DemoBorg(time.Now())
	if slot.Error != "" || borg == nil || len(borg.Clients) != 2*len(one.Clients) {
		t.Fatalf("borg slot: %+v", slot)
	}
}

// TestPeerOncePerBoard: tiles of a board that ask the same Verbund for
// the same service ask it once (a board of 40 tiles read all Verbünde
// and connections per tile: 28 % of /boards/1).
func TestPeerOncePerBoard(t *testing.T) {
	d := openTestDB(t)
	u := addUser(t, d, "memo@b.c")
	who, _ := access.Load(d, u.ID)
	space, _ := content.PersonalSpace(d, u.ID)
	c := &model.Connection{SpaceID: space.ID, Key: "borg", Name: "Borg", Service: string(enums.ServiceBorgBackup),
		URL: "demo://borgbackup/memo", CredentialMode: enums.CredentialShared, VerifyTLS: true, CreatedAt: time.Now().UTC()}
	if err := content.AddConnection(d, c); err != nil {
		t.Fatal(err)
	}
	var tiles []*model.Widget
	for range 2 {
		id, err := widgetlib.Create(d, who, space.ID, "backups", "Backups", map[string]any{}, nil, nil)
		if err != nil {
			t.Fatal(err)
		}
		w, _, _ := widgetlib.Detail(d, who, id)
		tiles = append(tiles, w)
	}

	ctx := widgetlib.WithMemo(context.Background())
	if _, err := widgetlib.LoadRows(ctx, d, who, tiles[0], 0, svcdata.Cached); err != nil {
		t.Fatal(err)
	}
	seen := dbtest.Statements(t, d)
	if _, err := widgetlib.LoadRows(ctx, d, who, tiles[1], 0, svcdata.Cached); err != nil {
		t.Fatal(err)
	}
	for _, q := range seen() {
		if strings.Contains(q, "FROM links") || strings.Contains(q, "FROM connections WHERE space_id IN") {
			t.Errorf("second tile asked again: %s", q)
		}
	}
}
