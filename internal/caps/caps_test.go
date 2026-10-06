package caps

import (
	"testing"

	"andon/internal/enums"
)

var kimai = HolderOf(enums.ServiceKimai)

// metOnly meets exactly the given needs.
func metOnly(have ...Need) func(Need) bool {
	return func(n Need) bool {
		for _, h := range have {
			if h == n {
				return true
			}
		}
		return false
	}
}

func TestDetectReadOnlyPlugin(t *testing.T) {
	s := Detect(kimai, metOnly(mileagePlugin, mileageView, placesWrite))
	if !s.Can(Places, Read, "") || s.Can(Places, Update, "home") || s.Can(Rides, Update, "driving") {
		t.Fatalf("set %+v", s.Have)
	}
	if need, ok := s.Lacks(Places, Update); !ok || need != mileageEdit {
		t.Fatalf("lacks %v %v", need, ok)
	}
}

func TestDetectOldPlugin(t *testing.T) {
	s := Detect(kimai, metOnly(mileagePlugin, mileageView, mileageEdit))
	if s.Can(Places, Update, "") || !s.Can(Rides, Update, "driving") {
		t.Fatalf("set %+v", s.Have)
	}
	if need, _ := s.Lacks(Places, Create); need != placesWrite {
		t.Fatalf("lacks %v", need)
	}
}

func TestDetectWithoutPlugin(t *testing.T) {
	s := Detect(kimai, metOnly())
	if s.Can(Places, Read, "") || s.Can(Rides, Read, "") || !s.Can(Customers, Update, "") {
		t.Fatalf("set %+v", s)
	}
	if need, _ := s.Lacks(Places, Read); need != mileagePlugin {
		t.Fatalf("first missing %v", need)
	}
}

// Store: the plugin keeps business kinds and car rides, Andon private
// places, bicycle rides and everything without the plugin.
func TestStore(t *testing.T) {
	full := Full(kimai)
	none := Detect(kimai, metOnly())
	cases := []struct {
		d     Domain
		op    Op
		kind  string
		order []Set
		want  Holder
	}{
		{Places, Update, "customer", []Set{full}, kimai},
		{Places, Update, "private", []Set{full}, Andon},
		{Places, Update, "customer", []Set{none}, Andon},
		{Places, Update, "customer", nil, Andon},
		{Rides, Update, "driving", []Set{full}, kimai},
		{Rides, Update, "motorcycle", []Set{full}, kimai},
		{Rides, Update, "cycling", []Set{full}, Andon},
		{Places, Update, "", []Set{full}, kimai},
	}
	for _, c := range cases {
		if got := Store(c.d, c.op, c.kind, c.order...); got != c.want {
			t.Errorf("Store(%s, %s, %q) = %s, want %s", c.d, c.op, c.kind, got, c.want)
		}
	}
}

func TestDawarichTracks(t *testing.T) {
	daw := HolderOf(enums.ServiceDawarich)
	if s := Detect(daw, metOnly()); s.Can(Rides, Read, "") || !s.Can(Places, Create, "") {
		t.Fatalf("old dawarich %+v", s)
	}
	if s := Full(daw); !s.Can(Rides, Read, "") {
		t.Fatal("tracks not read")
	}
}
