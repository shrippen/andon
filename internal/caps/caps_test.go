package caps

import (
	"slices"
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

// Partial: gaps beside what the connection has in the same domain are
// notes; a domain it lacks entirely is not.
func TestPartial(t *testing.T) {
	// Holiday bundle missing, no working time: only the target is partial.
	s := Detect(kimai, metOnly(mileagePlugin, mileageView, mileageEdit, placesWrite))
	gaps := s.Partial()
	if len(gaps) != 1 || gaps[0].Need != workContract {
		t.Fatalf("partial %+v", gaps)
	}
	if !s.Can(WorkTime, Read, "timesheets") || s.Can(WorkTime, Read, "target") || s.Can(Absences, Read, "") {
		t.Fatalf("set %+v", s.Have)
	}

	// Read-only plugin: places and rides lack update, nothing else.
	s = Detect(kimai, metOnly(mileagePlugin, mileageView, placesWrite, holidayPlugin, workContract))
	for _, g := range s.Partial() {
		if g.Need != mileageEdit {
			t.Fatalf("partial %+v", g)
		}
	}
	if len(s.Partial()) != 4 {
		t.Fatalf("partial %+v", s.Partial())
	}
}

// Gap tells capabilities of one domain and op apart by their kinds.
func TestGapByKinds(t *testing.T) {
	s := Detect(kimai, metOnly())
	target := Cap{Domain: WorkTime, Op: Read, Kinds: []string{"target"}}
	sheets := Cap{Domain: WorkTime, Op: Read, Kinds: []string{"timesheets"}}
	if g, ok := s.Gap(target); !ok || g.Need != workContract {
		t.Fatalf("target %+v %v", g, ok)
	}
	if _, ok := s.Gap(sheets); ok {
		t.Fatal("timesheets reported missing")
	}
}

// Every declaration is well formed: known domain and op, needs named.
func TestDeclarations(t *testing.T) {
	domains := []Domain{Places, Rides, Customers, Absences, WorkTime, Invoices, Payments, Receipts, Appointments, Subscriptions}
	for _, h := range Holders() {
		list := Declared(h)
		for i, c := range list {
			if !slices.Contains(domains, c.Domain) || (c.Op != Read && c.Op != Create && c.Op != Update) {
				t.Errorf("%s: %+v", h, c)
			}
			for _, n := range c.Needs {
				if n.Kind == "" || n.Name == "" {
					t.Errorf("%s: need %+v", h, n)
				}
			}
			for _, o := range list[i+1:] {
				if o.Same(c) {
					t.Errorf("%s: %+v declared twice", h, c)
				}
			}
		}
	}
}

// Paired: business services pair, homelab services are read space-wide.
func TestPaired(t *testing.T) {
	for _, s := range []enums.ServiceType{enums.ServiceKimai, enums.ServiceInvoiceNinja, enums.ServiceSure, enums.ServicePaperless,
		enums.ServiceDawarich, enums.ServiceMail, enums.ServiceCalendar, enums.ServiceWallos} {
		if !Paired(s) {
			t.Errorf("%s not paired", s)
		}
	}
	for _, s := range []enums.ServiceType{enums.ServiceDocker, enums.ServiceProxmox, enums.ServiceUptimeKuma, enums.ServiceBorgBackup} {
		if Paired(s) {
			t.Errorf("%s paired", s)
		}
	}
}
