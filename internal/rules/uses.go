package rules

// What the rules of several services read, declared once (Uses): the
// services they need follow from it (NeedsOf). A rule whose service
// failed keeps its hints instead of resolving them for lack of data.

import (
	"slices"

	"andon/internal/caps"
	"andon/internal/enums"
)

func use(s enums.ServiceType, d caps.Domain) caps.Use {
	return caps.Use{Holder: caps.HolderOf(s), Domain: d}
}

// useAll is domain d of every member of a category: a rule reading the
// category's shape needs whichever member the Verbund has.
func useAll(c caps.Category, d caps.Domain) []caps.Use {
	var out []caps.Use
	for _, s := range caps.Members(c) {
		out = append(out, use(s, d))
	}
	return out
}

var (
	kimaiWork      = use(enums.ServiceKimai, caps.WorkTime)
	kimaiPlaces    = use(enums.ServiceKimai, caps.Places)
	kimaiRides     = use(enums.ServiceKimai, caps.Rides)
	ninjaInvoices  = use(enums.ServiceInvoiceNinja, caps.Invoices)
	ninjaClients   = use(enums.ServiceInvoiceNinja, caps.Customers)
	ninjaReceipts  = use(enums.ServiceInvoiceNinja, caps.Receipts)
	bankPayments   = useAll(caps.Bank, caps.Payments)
	bankSubs       = useAll(caps.Bank, caps.Subscriptions)
	paperlessDocs  = use(enums.ServicePaperless, caps.Receipts)
	paperlessSubs  = use(enums.ServicePaperless, caps.Subscriptions)
	mailReceipts   = use(enums.ServiceMail, caps.Receipts)
	wallosSubs     = use(enums.ServiceWallos, caps.Subscriptions)
	calendarDates  = use(enums.ServiceCalendar, caps.Appointments)
	dawarichPlaces = use(enums.ServiceDawarich, caps.Places)
	dawarichRides  = use(enums.ServiceDawarich, caps.Rides)
)

func init() {
	Uses("geo.visit_without_time", dawarichPlaces, kimaiWork)
	Uses("geo.time_without_visit", dawarichPlaces, dawarichRides, kimaiWork)
	Uses("inventory.expense_missing", ninjaReceipts)
	Uses("in.rate_below", kimaiWork, ninjaInvoices, dawarichRides, kimaiRides)
	Uses("bank.subscription_unused", slices.Concat(bankSubs, []caps.Use{paperlessSubs})...)
	Uses("bank.spendable_negative", slices.Concat(bankPayments, []caps.Use{ninjaInvoices})...)
	Uses("calendar.unbooked", calendarDates, kimaiWork)
	Uses("in.order_gap", kimaiWork, ninjaInvoices)
	Uses("kimai.margin_low", kimaiWork, ninjaInvoices)
	Uses("cross.invoice_paid", slices.Concat(bankPayments, []caps.Use{ninjaInvoices})...)
	Uses("cross.payment_unmatched", slices.Concat(bankPayments, []caps.Use{ninjaInvoices, ninjaClients})...)
	Uses("cross.expense_unrecorded", slices.Concat(bankPayments, []caps.Use{ninjaReceipts, paperlessDocs, mailReceipts})...)
	Uses("cross.wallos_missing", slices.Concat(bankSubs, []caps.Use{wallosSubs})...)
	Uses("geo.travel_costs", dawarichRides, kimaiRides)
	Uses("geo.per_diem", dawarichRides, kimaiRides)
	Uses("geo.travel_unbilled", dawarichRides, kimaiRides, kimaiWork, ninjaInvoices, ninjaClients)
	Uses("geo.unplaced", dawarichRides, kimaiRides, kimaiPlaces)
	Uses("geo.plugin_missing", kimaiPlaces)
	Uses("geo.car_private_share", dawarichRides, kimaiRides)
	Uses("cross.charge_business", dawarichRides, kimaiRides)
	Uses("mail.invoice_unrecorded", mailReceipts, ninjaReceipts)
	Uses("paperless.invoice_unrecorded", paperlessDocs, ninjaReceipts)
}
