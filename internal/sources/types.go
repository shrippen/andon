package sources

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
	URL           string
	Timesheets    []KimaiSheet
	Active        []KimaiSheet
	Projects      []KimaiProject
	Customers     []KimaiCustomer
	Absences      []KimaiAbsence
	Holidays      []KimaiHoliday
	HolidayBundle bool
	Contract      *WorkContract // working time from Kimai, nil if none
	Places        []KimaiPlace  // mileage plugin, nil without it
	Mileage       bool          // the mileage plugin answered
	MileageTrips  []KimaiMileageTrip
	PlacesWrite   bool // the plugin creates and changes places (feature placesWrite)
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
	Items              []string // line items: product key and notes, lower case
}

type NinjaPayment struct {
	ID       int64
	Date     string
	Amount   float64
	ClientID int64
}

type NinjaClient struct {
	ID        int64
	Key       string // id as sent (hashed string in v5), for write calls
	Name      string
	VATNumber string
	CountryID string
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
}

type NinjaVendor struct {
	Key, Name string
}

type NinjaQuote struct {
	ID       int64
	Number   string
	ClientID int64
	Status   string
	Date     string
	Amount   float64
}

type NinjaRecurring struct {
	ID              int64
	Number          string
	ClientID        int64
	Active          bool
	NextSendDate    string
	RemainingCycles int
	Amount          float64
}

type NinjaDataset struct {
	URL           string
	Currency      string
	Invoices      []NinjaInvoice
	Payments      []NinjaPayment
	Clients       []NinjaClient
	Expenses      []NinjaExpense
	Vendors       []NinjaVendor
	Quotes        []NinjaQuote
	Recurring     []NinjaRecurring
	HomeCountryID string
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
	Stats       map[string]any
	LastPoint   string
}
