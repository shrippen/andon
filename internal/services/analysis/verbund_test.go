package analysis_test

import (
	"context"
	"testing"
	"time"

	"andon/internal/enums"
	"andon/internal/model"
	"andon/internal/repos/content"
	linkrepo "andon/internal/repos/links"
	"andon/internal/services/analysis"
)

// Two Kimai and a Dawarich: the space is asked to say which belongs with
// which; a Verbund for one of them leaves the other free and unambiguous.
func TestAmbiguousUntilVerbund(t *testing.T) {
	d := openTestDB(t)
	sid := addSpace(t, d)
	add := func(key string, s enums.ServiceType) *model.Connection {
		c := &model.Connection{SpaceID: sid, Key: key, Name: key, Service: string(s), URL: "demo://" + string(s),
			CredentialMode: enums.CredentialShared, VerifyTLS: true, CreatedAt: time.Now().UTC()}
		if err := content.AddConnection(d, c); err != nil {
			t.Fatal(err)
		}
		return c
	}
	daw, k1 := add("daw", enums.ServiceDawarich), add("k1", enums.ServiceKimai)
	add("k2", enums.ServiceKimai)
	today := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	open := func() int {
		var n int
		if err := d.QueryRow("SELECT COUNT(*) FROM hints WHERE rule = 'system.partner_ambiguous' AND resolved_at IS NULL").Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}

	if _, err := analysis.RunAll(context.Background(), d, today); err != nil {
		t.Fatal(err)
	}
	if open() != 1 {
		t.Fatalf("ambiguous hints: %d", open())
	}

	id, err := linkrepo.Add(d, "Firma", nil, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range []*model.Connection{daw, k1} {
		if err := linkrepo.AddMember(d, id, c.ID, c.Service); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := analysis.RunAll(context.Background(), d, today); err != nil {
		t.Fatal(err)
	}
	if open() != 0 {
		t.Fatalf("still ambiguous: %d", open())
	}
}
