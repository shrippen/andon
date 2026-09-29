package hosts_test

import (
	"context"
	"errors"
	"testing"

	"andon/internal/enums"
	"andon/internal/repos/content"
	"andon/internal/services/hosts"
	"andon/internal/services/svcdata"
	"andon/internal/sources"
	"andon/internal/testkit"
)

const (
	// healthyURL serves the demo dataset on host "studio".
	healthyURL = "demo://studio"
	// brokenURL points at a closed local port on host "127.0.0.1".
	brokenURL = "http://127.0.0.1:1"
)

// Connections group by host name; hosts with failed services come
// first, lookups ignore case, strangers see nothing.
func TestListGroupsConnectionsByHost(t *testing.T) {
	d := testkit.DB(t)
	who, space := testkit.User(t, d, "owner@x.de", enums.RoleUser)
	stranger, _ := testkit.User(t, d, "other@x.de", enums.RoleUser)
	ctx := context.Background()

	// Seed the stored results the background run would leave behind.
	seeds := []struct {
		service enums.ServiceType
		url     string
	}{
		{enums.ServiceKimai, healthyURL},
		{enums.ServiceInvoiceNinja, healthyURL},
		{enums.ServiceKimai, brokenURL},
	}
	for _, s := range seeds {
		id := testkit.Conn(t, d, who, space, s.service, s.url)
		conn, err := content.Connection(d, id)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := svcdata.Get(ctx, d, sources.DataKey(s.service), nil, conn, &who.UserID, svcdata.Force); err != nil {
			t.Fatalf("seed %s: %v", s.url, err)
		}
	}

	list, err := hosts.List(ctx, d, who)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("hosts: %+v", list)
	}
	broken, healthy := list[0], list[1]
	if broken.Name != "127.0.0.1" || broken.Problems != 1 || len(broken.Services) != 1 || broken.Services[0].OK {
		t.Fatalf("broken host first: %+v", broken)
	}
	if healthy.Name != "studio" || healthy.Problems != 0 || len(healthy.Services) != 2 {
		t.Fatalf("healthy host: %+v", healthy)
	}

	one, err := hosts.One(ctx, d, who, "STUDIO")
	if err != nil || one.Name != "studio" || len(one.Services) != 2 {
		t.Fatalf("one: %+v, %v", one, err)
	}
	if _, err := hosts.One(ctx, d, who, "nas.lan"); !errors.Is(err, hosts.ErrNotFound) {
		t.Fatalf("unknown host: %v", err)
	}

	if other, _ := hosts.List(ctx, d, stranger); len(other) != 0 {
		t.Fatalf("stranger sees hosts: %+v", other)
	}
}
