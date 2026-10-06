package caps

import (
	"slices"

	"andon/internal/enums"
)

// Requirements of Kimai's mileage plugin ("Anfahrten").
var (
	mileagePlugin = Need{NeedPlugin, "mileage"}
	mileageView   = Need{NeedRight, "view"}
	mileageEdit   = Need{NeedRight, "editOwn"}
	placesWrite   = Need{NeedFeature, "placesWrite"}
	dawarichTrack = Need{NeedAPI, "tracks"}
)

// Other requirements: Kimai's holiday bundle and working time, the
// custom fields of Paperless (receipts keep their amounts there).
var (
	holidayPlugin  = Need{NeedPlugin, "holiday"}
	workContract   = Need{NeedSetting, "contract"}
	paperlessField = Need{NeedAPI, "custom_fields"}
)

// Kinds the mileage plugin knows: place types and the ride modes it
// pays (Dawarich's "driving" and "motorcycle").
var (
	mileagePlaceKinds = []string{"home", "work", "customer", "other"}
	mileageRideKinds  = []string{"driving", "motorcycle"}
)

// declared lists every holder's capabilities.
var declared = map[Holder][]Cap{
	HolderOf(enums.ServiceKimai): {
		{Domain: Places, Op: Read, Needs: []Need{mileagePlugin, mileageView}},
		{Domain: Places, Op: Create, Kinds: mileagePlaceKinds, Needs: []Need{mileagePlugin, mileageView, mileageEdit, placesWrite}},
		{Domain: Places, Op: Update, Kinds: mileagePlaceKinds, Needs: []Need{mileagePlugin, mileageView, mileageEdit, placesWrite}},
		{Domain: Rides, Op: Read, Needs: []Need{mileagePlugin, mileageView}},
		{Domain: Rides, Op: Create, Kinds: mileageRideKinds, Needs: []Need{mileagePlugin, mileageView, mileageEdit}},
		{Domain: Rides, Op: Update, Kinds: mileageRideKinds, Needs: []Need{mileagePlugin, mileageView, mileageEdit}},
		{Domain: Customers, Op: Read},
		{Domain: Customers, Op: Update}, // the name, e.g. taken from Invoice Ninja
		{Domain: WorkTime, Op: Read, Kinds: []string{"timesheets"}},
		{Domain: WorkTime, Op: Read, Kinds: []string{"target"}, Needs: []Need{workContract}},
		{Domain: WorkTime, Op: Create, Kinds: []string{"timesheets"}}, // start, book, edit
		{Domain: Absences, Op: Read, Needs: []Need{holidayPlugin}},    // absences and public holidays
	},
	HolderOf(enums.ServiceInvoiceNinja): {
		{Domain: Customers, Op: Read},
		{Domain: Invoices, Op: Read},
		{Domain: Invoices, Op: Create}, // drafts from Kimai
		{Domain: Payments, Op: Read},
		{Domain: Payments, Op: Create}, // bookings from Sure
		{Domain: Receipts, Op: Read},   // expenses
		{Domain: Receipts, Op: Create},
		{Domain: Receipts, Op: Update},
	},
	HolderOf(enums.ServiceSure): {
		{Domain: Payments, Op: Read}, // transactions of the bank accounts
	},
	HolderOf(enums.ServicePaperless): {
		{Domain: Receipts, Op: Read, Needs: []Need{paperlessField}},
		{Domain: Receipts, Op: Create},                                // upload
		{Domain: Receipts, Op: Update, Needs: []Need{paperlessField}}, // custom fields
	},
	HolderOf(enums.ServiceMail): {
		{Domain: Receipts, Op: Read}, // attachments of the inbox
	},
	HolderOf(enums.ServiceDawarich): {
		{Domain: Places, Op: Read},
		{Domain: Places, Op: Create}, // areas: geometry only, no kinds
		{Domain: Rides, Op: Read, Needs: []Need{dawarichTrack}},
	},
	Andon: {
		{Domain: Places, Op: Update},
		{Domain: Rides, Op: Update},
	},
}

// NameSource is the service whose names count in a domain: Invoice
// Ninja's client names for customers (decided 06.10.2026).
var NameSource = map[Domain]Holder{Customers: HolderOf(enums.ServiceInvoiceNinja)}

// Holders lists every holder with declarations, in a stable order.
func Holders() []Holder {
	out := make([]Holder, 0, len(declared))
	for h := range declared {
		out = append(out, h)
	}
	slices.Sort(out)
	return out
}

// Declared is what a holder can do at best.
func Declared(h Holder) []Cap { return declared[h] }
