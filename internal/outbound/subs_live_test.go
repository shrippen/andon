package outbound_test

import (
	"context"
	"testing"
	"time"

	"andon/internal/outbound"
	"andon/internal/sources"
	"andon/internal/testkit/live"
)

// kindSubscription is a Wallos test entry in the write log.
const kindSubscription = "subscription"

// A subscription added, repriced, ended and deleted: the Wallos writes
// of the subscription bench (services/subs).
func TestWallosLive(t *testing.T) {
	wallos := live.Target(t, live.Wallos)
	ctx := context.Background()
	name := live.Name(t)
	next := time.Now().AddDate(0, 1, 0).Format(time.DateOnly)

	before := wallosRead(t, wallos)
	cycle, frequency := sources.WallosCycle(1)
	id, err := outbound.WallosAdd(ctx, wallos, outbound.WallosNew{Name: name, Price: 1.23, CurrencyID: before.Main, Cycle: cycle, Frequency: frequency, Next: next})
	if err != nil {
		t.Fatalf("add: %v", err)
	}
	if id == 0 {
		id = wallosFind(t, wallos, name).ID
	}
	live.Created(t, live.Wallos, kindSubscription, id)
	t.Cleanup(func() {
		live.Change(t, live.Wallos, live.Delete, kindSubscription, id)
		if err := outbound.WallosDelete(ctx, wallos, id); err != nil {
			t.Errorf("delete: %v", err)
		}
		for _, s := range wallosRead(t, wallos).Subs {
			if s.ID == id {
				t.Errorf("subscription %d still there after delete", id)
			}
		}
	})

	got := wallosFind(t, wallos, name)
	if got.Price != 1.23 || got.Months() != 1 || got.Next != next || got.CurrencyID != before.Main || got.Inactive {
		t.Errorf("added: %+v", got)
	}

	live.Change(t, live.Wallos, live.Update, kindSubscription, id)
	if err := outbound.WallosPrice(ctx, wallos, id, 2.34); err != nil {
		t.Fatalf("price: %v", err)
	}
	if got := wallosFind(t, wallos, name); got.Price != 2.34 || got.Next != next {
		t.Errorf("after price: %+v", got)
	}

	live.Change(t, live.Wallos, live.Update, kindSubscription, id)
	if err := outbound.WallosEnd(ctx, wallos, id, time.Now().Format(time.DateOnly)); err != nil {
		t.Fatalf("end: %v", err)
	}
	if got := wallosFind(t, wallos, name); !got.Inactive || got.Price != 2.34 {
		t.Errorf("after end: %+v", got)
	}
}

// wallosRead fetches the Wallos dataset fresh.
func wallosRead(t *testing.T, to outbound.Target) *sources.WallosDataset {
	t.Helper()
	src, err := sources.Get("wallos.data")
	if err != nil {
		t.Fatal(err)
	}
	out, err := src.Fetch(context.Background(), sources.Ctx{URL: to.URL, Secret: to.Token, VerifyTLS: to.VerifyTLS})
	if err != nil {
		t.Fatalf("read wallos: %v", err)
	}
	return out.(*sources.WallosDataset)
}

// wallosFind is the subscription named name.
func wallosFind(t *testing.T, to outbound.Target, name string) sources.WallosSub {
	t.Helper()
	for _, s := range wallosRead(t, to).Subs {
		if s.Name == name {
			return s
		}
	}
	t.Fatalf("subscription %q not found", name)
	return sources.WallosSub{}
}
