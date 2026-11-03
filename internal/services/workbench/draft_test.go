package workbench_test

import (
	"errors"
	"strings"
	"testing"

	"andon/internal/enums"
	"andon/internal/services/workbench"
	"andon/internal/testkit"
)

// A kept case comes back while open, a newer keep replaces it, a closed
// case drops out for good, and each user and place has its own draft.
func TestDraft(t *testing.T) {
	d := testkit.DB(t)
	who, _ := testkit.User(t, d, "owner@x.de", enums.RoleUser)
	other, _ := testkit.User(t, d, "other@x.de", enums.RoleUser)
	at := workbench.Place{Bench: "vat", Key: "v4/2026-Q3"}

	keep := func(cases workbench.Draft) {
		t.Helper()
		if err := workbench.KeepDraft(d, who, at, cases, nil); err != nil {
			t.Fatal(err)
		}
	}
	load := func(open ...string) workbench.Draft {
		t.Helper()
		got, err := workbench.LoadDraft(d, who, at, open)
		if err != nil {
			t.Fatal(err)
		}
		return got
	}

	keep(workbench.Draft{"a": {"vendor_a": {"Nordhost"}}, "b": {"pick": {"b"}}})
	keep(workbench.Draft{"a": {"vendor_a": {"Hetzner"}}})
	if got := load("a", "b"); got["a"]["vendor_a"][0] != "Hetzner" || got["b"]["pick"][0] != "b" {
		t.Fatalf("kept: %v", got)
	}

	// b written meanwhile: gone, also when it shows again.
	if got := load("a"); len(got) != 1 {
		t.Fatalf("closed case kept: %v", got)
	}
	if got := load("a", "b"); got["b"] != nil {
		t.Fatalf("closed case back: %v", got)
	}

	// A case without fields is removed.
	keep(workbench.Draft{"a": {}})
	if got := load("a"); len(got) != 0 {
		t.Fatalf("emptied case kept: %v", got)
	}

	keep(workbench.Draft{"a": {"vendor_a": {"Nordhost"}}})
	if got, err := workbench.LoadDraft(d, other, at, []string{"a"}); err != nil || len(got) != 0 {
		t.Fatalf("other user sees the draft: %v %v", got, err)
	}
	q2 := workbench.Place{Bench: "vat", Key: "v4/2026-Q2"}
	if got, err := workbench.LoadDraft(d, who, q2, []string{"a"}); err != nil || len(got) != 0 {
		t.Fatalf("other period sees the draft: %v %v", got, err)
	}

	if err := workbench.DropDraft(d, who, at); err != nil {
		t.Fatal(err)
	}
	if got := load("a"); len(got) != 0 {
		t.Fatalf("dropped draft back: %v", got)
	}
}

// A draft past its size is refused; the stored one stays.
func TestDraftSize(t *testing.T) {
	d := testkit.DB(t)
	who, _ := testkit.User(t, d, "owner@x.de", enums.RoleUser)
	at := workbench.Place{Bench: "vat", Key: "x"}
	if err := workbench.KeepDraft(d, who, at, workbench.Draft{"a": {"note_a": {"ok"}}}, nil); err != nil {
		t.Fatal(err)
	}

	big := workbench.Draft{"b": {"note_b": {strings.Repeat("x", 2<<20)}}}
	if err := workbench.KeepDraft(d, who, at, big, nil); !errors.Is(err, workbench.ErrDraftSize) {
		t.Fatalf("err = %v, want ErrDraftSize", err)
	}
	got, _ := workbench.LoadDraft(d, who, at, []string{"a", "b"})
	if len(got) != 1 || got["a"] == nil {
		t.Fatalf("stored draft lost: %v", got)
	}
}
