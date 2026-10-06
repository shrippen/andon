package caps

import "andon/internal/enums"

// Requirements of Kimai's mileage plugin ("Anfahrten").
var (
	mileagePlugin = Need{NeedPlugin, "mileage"}
	mileageView   = Need{NeedRight, "view"}
	mileageEdit   = Need{NeedRight, "editOwn"}
	placesWrite   = Need{NeedFeature, "placesWrite"}
	dawarichTrack = Need{NeedAPI, "tracks"}
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
	},
	HolderOf(enums.ServiceInvoiceNinja): {
		{Domain: Customers, Op: Read},
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

// Declared is what a holder can do at best.
func Declared(h Holder) []Cap { return declared[h] }
