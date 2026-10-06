package widgetlib_test

import (
	"context"
	"testing"
	"time"

	"andon/internal/enums"
	"andon/internal/model"
	"andon/internal/repos/content"
	"andon/internal/services/access"
	"andon/internal/services/svcdata"
	"andon/internal/services/verbund"
	"andon/internal/services/widgetlib"
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
