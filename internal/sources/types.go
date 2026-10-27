package sources

import (
	"slices"
	"strconv"

	"andon/internal/caps"
	"andon/internal/enums"
)

// Normalized datasets: what widgets, metrics and rules actually consume,
// after each service's raw API shape is flattened here. Typed structs, so
// the metrics/rules layer gets compile-time field checks.

// ── Kimai ──

type KimaiSheet struct {
	ID         int64
	Begin      string
	End        string
	Minutes    int
	Rate       float64 // amount of this sheet
	HourlyRate float64 // 0 if Kimai did not send it
	Billable   bool
	Exported   bool
	ProjectID  int64
	CustomerID int64
	Activity   string
	UserID     int64
}

type KimaiProject struct {
	ID            int64
	Name          string
	CustomerID    int64
	Budget        float64
	TimeBudgetMin int
	BudgetType    string
	End           string
	UsedMoney     float64
	UsedMinutes   int
	Color         string // "#rrggbb", "" = none
}

type KimaiCustomer struct {
	ID    int64
	Name  string
	Color string // "#rrggbb", "" = none
}

type KimaiAbsence struct {
	Start   string
	End     string
	Type    string
	Status  string
	HalfDay bool
}

type KimaiHoliday struct {
	Date    string
	Name    string
	HalfDay bool
}

// KimaiPlace is a place of the Kimai mileage plugin: home, work, a
// customer site or another place, maybe imported from a Dawarich area
// (AreaID) or place (PlaceID).
type KimaiPlace struct {
	ID         int64
	Name       string
	Type       string // "home", "work", "customer", "other"
	CustomerID int64
	Lat, Lon   float64
	Radius     float64 // metres
	AreaID     int64
	PlaceID    int64
}

// KimaiMileageTrip is a trip of the Kimai mileage plugin. Departure and
// Arrival are RFC 3339 times, "" if not entered.
type KimaiMileageTrip struct {
	ID                 int64
	Date               string
	Departure, Arrival string
	Purpose            string // "commute", "business", "private"
	KM                 float64
	Project            int64
	Timesheet          int64
}

type KimaiDataset struct {
	URL          string
	Timesheets   []KimaiSheet
	Active       []KimaiSheet
	Projects     []KimaiProject
	Customers    []KimaiCustomer
	Absences     []KimaiAbsence
	Holidays     []KimaiHoliday
	Contract     *WorkContract // working time from Kimai, nil if none
	Places       []KimaiPlace  // mileage plugin, nil without it
	MileageTrips []KimaiMileageTrip
	Caps         caps.Set // what the plugins, rights and settings allow
}

// ── Invoice Ninja ──

type NinjaInvoice struct {
	ID       int64
	Key      string // id as sent (hashed string in v5), for write calls
	Number   string
	ClientID int64
	Status   string
	Date     string
	DueDate  string
	Amount   float64
	Balance  float64
	Taxes    float64
	Net      float64
	// Reminded is when the last reminder went out, NextSend when the
	// next one goes (or the invoice is sent again); "" = none.
	Reminded, NextSend string
	Stage              int      // reminders sent so far (0–3)
	Items              []string // line items: product key and notes, lower case
}

type NinjaPayment struct {
	ID          int64
	Date        string
	Amount      float64
	ClientID    int64
	Key, Number string // id as sent (hashed in v5) and the payment's number
}

type NinjaClient struct {
	ID        int64
	Key       string // id as sent (hashed string in v5), for write calls
	Name      string
	VATNumber string
	CountryID string
}

// Ref is the client's stable reference for links: its key, else its id.
func (c NinjaClient) Ref() string {
	if c.Key != "" {
		return c.Key
	}
	return strconv.FormatInt(c.ID, 10)
}

type NinjaExpense struct {
	ID        int64
	Number    string
	Date      string
	Amount    float64
	Tax       float64
	Notes     string
	VendorID  int64
	VendorKey string // id as sent (hashed string in v5)

	// For the VAT check: the write key, category, first tax and the bank
	// transaction it is linked to ("" = none).
	Key, CategoryKey, TaxName, TxnKey string
	TaxRate                           float64

	Custom [NinjaSlots]string // custom_value1 … custom_value4, e.g. the EÜR line
}

// ExpenseField is an expense custom field as Invoice Ninja's company
// settings define it: "EÜR|Arbeitsmittel,Werbekosten" is a dropdown,
// "Rechnungsnummer|single_line_text" a text field; "" Label = unused.
type ExpenseField struct {
	Label   string
	Options []string // a dropdown's choices, nil for free text
}

type NinjaVendor struct {
	Key, Name string
	CountryID string // numeric ISO 3166 ("276" Germany), "" unknown
	Archived  bool   // kept for the names of old expenses
}

// AccountRole is what a bank account is used for, as the user named it
// (connection option OptAccountRoles); "" = not said.
type AccountRole string

const (
	RoleUnknown  AccountRole = ""
	RoleBusiness AccountRole = "business"
	RolePrivate  AccountRole = "private"
	// RoleReserve is money set aside for taxes: neither spent nor
	// counted as free (metrics.SafeToSpend).
	RoleReserve AccountRole = "reserve"
	// RoleIgnored drops the account and its bookings from every
	// evaluation: a depot or a car in Sure. The service's own net worth
	// (BankData.NetWorth) keeps it: the depot is still owned.
	RoleIgnored AccountRole = "ignored"
	// RoleMixed holds business and private bookings on one account.
	RoleMixed AccountRole = "mixed"
	// RoleTransfer is a card or PayPal: its bookings are business
	// spending, its settlement from the giro moves money between own
	// accounts.
	RoleTransfer AccountRole = "transfer"
)

// knownRoles are the roles a user can name.
var knownRoles = []AccountRole{RoleBusiness, RolePrivate, RoleReserve, RoleIgnored, RoleMixed, RoleTransfer}

// BusinessRoles are the accounts that hold business bookings (for
// BankData.Only); PrivateRoles those that hold private ones.
var (
	BusinessRoles = []AccountRole{RoleBusiness, RoleMixed, RoleTransfer}
	PrivateRoles  = []AccountRole{RolePrivate, RoleMixed}
)

// Known says whether r is a role a user can name, "" (not said) included.
func (r AccountRole) Known() bool { return r == RoleUnknown || slices.Contains(knownRoles, r) }

// kindRoles are the roles Sure's account kinds imply: what holds no
// money to spend is ignored, a credit card is a transfer account.
var kindRoles = map[string]AccountRole{
	"investment": RoleIgnored, "crypto": RoleIgnored, "property": RoleIgnored, "vehicle": RoleIgnored, "other_asset": RoleIgnored,
	"credit_card": RoleTransfer,
}

// OptAccountRoles is the connection option holding each account's role:
// {"<account key>": "business"}.
const OptAccountRoles = "account_roles"

// Options of an Invoice Ninja connection naming its categories:
//
//	category_map {"<Sure connection>": {"Software": "<Ninja category key>"}}
//	euer_slot    3, the expense custom field holding the EÜR line (1–4)
const (
	OptCategoryMap = "category_map"
	OptEuerSlot    = "euer_slot"
)

// Categories is what an Invoice Ninja connection's options say about
// categories.
type Categories struct {
	Sure     map[int64]map[string]string // per Sure (or Firefly) connection: its category → Ninja key
	EuerSlot int                         // 0 = not chosen
}

// CategoriesOf reads them from a connection's options.
func CategoriesOf(options map[string]any) Categories {
	out := Categories{Sure: map[int64]map[string]string{}}
	raw, _ := options[OptCategoryMap].(map[string]any)
	for conn, m := range raw {
		id, err := strconv.ParseInt(conn, 10, 64)
		if err != nil {
			continue
		}
		out.Sure[id] = strMap(m)
	}
	if n := int(asFloat(options[OptEuerSlot])); n >= 1 && n <= NinjaSlots {
		out.EuerSlot = n
	}
	return out
}

// Options writes them back into options (a copy is the caller's).
func (c Categories) Options(options map[string]any) {
	sure := map[string]any{}
	for conn, m := range c.Sure {
		sure[strconv.FormatInt(conn, 10)] = anyMap(m)
	}
	options[OptCategoryMap], options[OptEuerSlot] = sure, c.EuerSlot
}

func strMap(raw any) map[string]string {
	out := map[string]string{}
	m, _ := raw.(map[string]any)
	for k, v := range m {
		if s := asStr(v); s != "" {
			out[k] = s
		}
	}
	return out
}

func anyMap(m map[string]string) map[string]any {
	out := make(map[string]any, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

// accountRoles reads the roles of a connection's accounts.
func accountRoles(options map[string]any) map[string]AccountRole {
	out := map[string]AccountRole{}
	raw, _ := options[OptAccountRoles].(map[string]any)
	for k, v := range raw {
		if r := AccountRole(asStr(v)); r != RoleUnknown && r.Known() {
			out[k] = r
		}
	}
	return out
}

// NinjaBankAccount is an account Invoice Ninja's bank feed reads.
type NinjaBankAccount struct {
	Key, Name string
	Role      AccountRole
}

// BankStatus is where Invoice Ninja stands with a bank transaction.
type BankStatus string

const (
	BankOpen    BankStatus = "open"      // nothing linked
	BankMatched BankStatus = "matched"   // a Ninja rule proposes, not booked
	BankDone    BankStatus = "converted" // expense or payment linked
)

// NinjaBankTxn is one bank transaction Invoice Ninja imported. Amount is
// positive; Debit tells money going out.
type NinjaBankTxn struct {
	Key, Date, Description, Payer string
	Account, AccountKey           string // the bank account's name and id in Invoice Ninja
	Role                          AccountRole
	Amount                        float64
	Debit                         bool
	Status                        BankStatus
	Archived                      bool // set aside in Invoice Ninja (private)
	ExpenseKeys, InvoiceKeys      []string
	PaymentKey                    string // the payment it is booked to
	VendorKey, CategoryKey        string
}

// NinjaCategory is an expense category.
type NinjaCategory struct {
	Key, Name string
}

// NinjaTaxRate is a tax rate set up in Invoice Ninja ("MwSt.", 19).
type NinjaTaxRate struct {
	Name string
	Rate float64
}

type NinjaQuote struct {
	ID         int64
	Key        string // id as sent (hashed string in v5), for links
	Number     string
	ClientID   int64
	Status     string
	Date       string
	ValidUntil string // the quote's due date in Invoice Ninja, "" none
	Amount     float64
}

// ReminderStages is how many reminders Invoice Ninja schedules.
const ReminderStages = 3

// ReminderSchedule says what a reminder's days count from, as Invoice
// Ninja's company settings name it.
type ReminderSchedule string

const (
	AfterInvoiceDate ReminderSchedule = "after_invoice_date"
	BeforeDueDate    ReminderSchedule = "before_due_date"
	AfterDueDate     ReminderSchedule = "after_due_date"
)

// NinjaReminder is one reminder of Invoice Ninja's schedule (Settings →
// Reminders): sent Days after or before its date, if enabled.
type NinjaReminder struct {
	Enabled  bool
	Days     int
	Schedule ReminderSchedule
}

type NinjaRecurring struct {
	ID              int64
	Key             string // id as sent (hashed string in v5), for links
	Number          string
	ClientID        int64
	Active          bool
	NextSendDate    string
	RemainingCycles int
	Amount          float64
}

// NinjaRecurringExpense is a recurring expense: Invoice Ninja books it
// on its own every Months months (0: weekly).
type NinjaRecurringExpense struct {
	Key, Number, VendorKey, CategoryKey string
	Amount                              float64
	Months                              int
	Next                                string // next booking, "2026-11-01"
	Active                              bool   // running, not paused or ended
	Notes                               string
}

type NinjaDataset struct {
	URL      string
	Currency string
	Since    string // first day read ("2025-01-01"); older invoices only while open

	Invoices      []NinjaInvoice
	Payments      []NinjaPayment
	Clients       []NinjaClient
	Expenses      []NinjaExpense
	Vendors       []NinjaVendor
	Quotes        []NinjaQuote
	Recurring     []NinjaRecurring
	HomeCountryID string

	// RecurringExpenses are read when the instance has them.
	RecurringExpenses []NinjaRecurringExpense

	// Bank transactions (banking module), expense categories and tax
	// rates; BankRead says whether the bank API answered.
	BankTxns     []NinjaBankTxn
	BankRead     bool
	BankAccounts []NinjaBankAccount // the accounts the feed reads
	Categories   []NinjaCategory
	// ExpenseFields are the expense custom fields, slot 1–4 at 0–3.
	ExpenseFields [NinjaSlots]ExpenseField
	// Reminders is the company's reminder schedule (stage 1–3 at 0–2).
	Reminders [ReminderStages]NinjaReminder
	TaxRates  []NinjaTaxRate
}

// ── Snipe-IT ──

type SnipeAsset struct {
	ID              int64
	Name            string
	Tag             string
	Model           string
	Category        string
	Status          string
	Deployable      bool
	Assigned        bool
	PurchaseDate    string
	PurchaseCost    float64
	WarrantyExpires string
	EOLDate         string
	NextAudit       string
	LastChange      string
	AssignedTo      string // who has it (person, location or asset name)
	ExpectedCheckin string // agreed return date, "" if none
	Serial          string
	// DepMonths is the depreciation rule's length in months, 0 none
	// (Snipe-IT → Abschreibungen, "1 Jahr / PCs" = 12).
	DepMonths int
}

type SnipeLicense struct {
	ID      int64
	Name    string
	Expires string
	Seats   int
	Free    int
}

type SnipeConsumable struct {
	ID        int64
	Name      string
	Remaining int
	Min       int
}

type SnipeDataset struct {
	URL          string
	Assets       []SnipeAsset
	Licenses     []SnipeLicense
	Consumables  []SnipeConsumable
	AuditOverdue []int64
	// Models and status labels, for a new asset (inventory bench).
	Models   []SnipeChoice
	Statuses []SnipeChoice
}

// SnipeChoice is a model or status label: its id and name; Kind is a
// status label's type ("deployable", "pending" …).
type SnipeChoice struct {
	ID         int64
	Name, Kind string
}

// ── Dawarich ──

type DawarichArea struct {
	ID     int64
	Name   string
	Lat    float64
	Lon    float64
	Radius float64
}

type DawarichVisit struct {
	ID      int64
	Start   string
	End     string
	Minutes int
	AreaID  int64
	Name    string
	Lat     *float64
	Lon     *float64
}

// DawarichPlace is a place Dawarich knows (its "places", not areas).
type DawarichPlace struct {
	ID       int64
	Name     string
	Lat, Lon float64
}

// DawarichSegment is one part of a track with one transportation mode
// ("driving", "walking", "stationary" …). Times are Unix seconds; only the
// ends of its line are kept.
type DawarichSegment struct {
	Start, End       int64
	Mode             string
	Meters           float64
	FromLat, FromLon float64
	ToLat, ToLon     float64
}

// DawarichTrack is one journey Dawarich computed between two recording
// gaps, split into segments.
type DawarichTrack struct {
	ID         int64
	Start, End int64
	Segments   []DawarichSegment
}

// TracksState says how complete DawarichDataset.Tracks is.
type TracksState string

const (
	TracksNone    TracksState = ""        // not read
	TracksOK      TracksState = "ok"      // every track of the window
	TracksPartial TracksState = "partial" // more are read on the next fetches
	TracksMissing TracksState = "missing" // this Dawarich has no tracks API
	TracksFailed  TracksState = "failed"  // reading them failed
)

type DawarichDataset struct {
	URL         string
	Areas       []DawarichArea
	Places      []DawarichPlace
	Visits      []DawarichVisit
	Tracks      []DawarichTrack // oldest first
	TracksFrom  string          // start of the window the tracks cover (RFC 3339)
	TracksState TracksState
	Caps        caps.Set // places, and rides when the tracks API answers
	TracksRead  int      // of TracksTotal in the window; less while TracksPartial
	TracksTotal int
	Stats       map[string]any
	LastPoint   string
}

// CapSet: Invoice Ninja's capabilities need nothing beyond the token.
func (d *NinjaDataset) CapSet() caps.Set { return caps.Full(caps.HolderOf(enums.ServiceInvoiceNinja)) }

// CapSet reports what the Kimai connection allows.
func (d *KimaiDataset) CapSet() caps.Set { return d.Caps }

// CapSet reports what the Dawarich connection allows.
func (d *DawarichDataset) CapSet() caps.Set { return d.Caps }
