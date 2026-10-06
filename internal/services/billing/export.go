package billing

// Year package for the tax advisor: one ZIP with CSV files (semicolon,
// decimal comma, UTF-8 with BOM – opens directly in German Excel). Column
// headers and file names are German on purpose, whatever the user's
// language: the package is for a German tax advisor, not for the screen.
//
//	rechnungen.csv  zahlungen.csv  ausgaben.csv  stunden.csv  ust.csv  fahrten.csv  it-kosten.csv

import (
	"archive/zip"
	"bytes"
	"context"
	"database/sql"
	"encoding/csv"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"andon/internal/db"
	"andon/internal/enums"
	"andon/internal/metrics"
	"andon/internal/model"
	"andon/internal/repos/content"
	linkrepo "andon/internal/repos/links"
	"andon/internal/services/access"
	auditsvc "andon/internal/services/audit"
	"andon/internal/services/svcdata"
	"andon/internal/services/verbund"
	"andon/internal/sources"
)

const (
	hoursPerDay    = 24
	csvComma       = ';'
	utf8BOM        = "\ufeff"
	monthsInYear   = 12
	minutesPerHour = 60
)

// ErrNoData means the space has neither Kimai nor Invoice Ninja data.
var ErrNoData = errors.New("billing.no_data")

// money: 1234.5 → "1234,50".
func money(v float64) string {
	return strings.Replace(strconv.FormatFloat(v, 'f', 2, 64), ".", ",", 1)
}

func inYear(isoDay string, year int) bool {
	return strings.HasPrefix(isoDay, strconv.Itoa(year)+"-")
}

// ErrPickVerbund means the space holds several Verbünde: the package is
// built for one of them.
var ErrPickVerbund = errors.New("billing.pick_verbund")

// groupOf is the connections of the space's Verbund linkID; 0 is the
// implicit one, or the only Verbund there is.
func groupOf(conns []*model.Connection, stored []linkrepo.Link, linkID int64) ([]*model.Connection, error) {
	groups, _ := verbund.Groups(conns, stored)
	if linkID == 0 && len(groups) == 1 {
		return groupConns(groups[0]), nil
	}
	for _, g := range groups {
		if g.LinkID == linkID {
			return groupConns(g), nil
		}
	}
	return nil, ErrPickVerbund
}

func groupConns(g verbund.Group) []*model.Connection {
	out := make([]*model.Connection, 0, len(g.Conns))
	for _, c := range g.Conns {
		out = append(out, c)
	}
	slices.SortFunc(out, func(a, b *model.Connection) int { return int(a.ID - b.ID) })
	return out
}

// Export builds the year package of one space (or one of its Verbünde).
func Export(ctx context.Context, d *sql.DB, who *access.Principal, spaceID, linkID int64, year int, ip string) (string, []byte, error) {
	ref, ok := who.Spaces[spaceID]
	if !ok || access.SpaceRight(who, &ref) < enums.RightEdit {
		return "", nil, access.ErrDenied
	}
	var conns []*model.Connection
	var settings map[string]any
	err := db.WithRead(d, func(tx *sql.Tx) error {
		var err error
		if conns, err = content.Connections(tx, []int64{spaceID}); err != nil {
			return err
		}
		stored, err := linkrepo.All(tx)
		if err != nil {
			return err
		}
		if conns, err = groupOf(conns, stored, linkID); err != nil {
			return err
		}
		sp, err := content.Space(tx, spaceID)
		if sp != nil {
			settings = sp.Settings
		}
		return err
	})
	if err != nil {
		return "", nil, err
	}

	var kimai *sources.KimaiDataset
	var ninja *sources.NinjaDataset
	var geo *sources.DawarichDataset
	var geoOptions map[string]any
	costData := map[string]any{}
	uid := who.UserID
	for _, c := range conns {
		var params map[string]any
		switch enums.ServiceType(c.Service) {
		case enums.ServiceHomeAssistant, enums.ServiceTibber, enums.ServiceSnipeIT, enums.ServiceSure, enums.ServiceDomains,
			enums.ServiceKomodo, enums.ServiceGitea, enums.ServiceGitHub:
			// The last background run is enough for the cost overview.
			if res, err := svcdata.Get(ctx, d, sources.DataKey(enums.ServiceType(c.Service)), nil, c, model.UserHolder(uid), svcdata.Stored); err == nil && res.Data != nil {
				costData[c.Service] = res.Data
			}
			continue
		case enums.ServiceKimai, enums.ServiceInvoiceNinja:
		case enums.ServiceDawarich:
			// Visits and tracks back to the start of the tax year.
			params = map[string]any{"days": float64(time.Since(time.Date(year, 1, 1, 0, 0, 0, 0, time.UTC)).Hours()/hoursPerDay + 1)}
			geoOptions = c.Options
		default:
			continue
		}
		res, err := svcdata.Get(ctx, d, sources.DataKey(enums.ServiceType(c.Service)), params, c, model.UserHolder(uid), svcdata.Force)
		if err != nil {
			continue
		}
		switch data := res.Data.(type) {
		case *sources.KimaiDataset:
			kimai = data
		case *sources.NinjaDataset:
			ninja = data
		case *sources.DawarichDataset:
			geo = data
		}
	}
	if kimai == nil && ninja == nil {
		return "", nil, ErrNoData
	}

	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	files := map[string][][]string{}
	if ninja != nil {
		files["rechnungen.csv"], files["zahlungen.csv"], files["ausgaben.csv"] = invoiceRows(ninja, year), paymentRows(ninja, year), expenseRows(ninja, year)
		files["ust.csv"] = vatRows(ninja, year, metrics.TaxVATMethod(settings))
	}
	if kimai != nil {
		files["stunden.csv"] = hourRows(kimai, year)
	}
	if geo != nil && kimai != nil {
		set := metrics.TravelSettingsOf(settings)
		files["fahrten.csv"] = tripRows(metrics.TravelOf(geo, kimai, geoOptions, set, time.Now()), kimai, year, set.KMRate)
	}
	if rows := itCostRows(costData, kimai, settings); len(rows) > 1 {
		files["it-kosten.csv"] = rows
	}
	for _, name := range []string{"rechnungen.csv", "zahlungen.csv", "ausgaben.csv", "ust.csv", "stunden.csv", "fahrten.csv", "it-kosten.csv"} {
		rows, ok := files[name]
		if !ok {
			continue
		}
		if err := writeCSV(zw, name, rows); err != nil {
			return "", nil, err
		}
	}
	if err := zw.Close(); err != nil {
		return "", nil, err
	}
	filename := fmt.Sprintf("steuer-%d.zip", year)
	return filename, buf.Bytes(), auditsvc.Log(d, &who.UserID, "billing.export", filename, ip, nil)
}

func writeCSV(zw *zip.Writer, name string, rows [][]string) error {
	f, err := zw.Create(name)
	if err != nil {
		return err
	}
	if _, err := f.Write([]byte(utf8BOM)); err != nil {
		return err
	}
	w := csv.NewWriter(f)
	w.Comma = csvComma
	if err := w.WriteAll(rows); err != nil {
		return err
	}
	return w.Error()
}

func clientNames(ninja *sources.NinjaDataset) map[int64]string {
	out := map[int64]string{}
	for _, c := range ninja.Clients {
		out[c.ID] = c.Name
	}
	return out
}

func invoiceRows(ninja *sources.NinjaDataset, year int) [][]string {
	names := clientNames(ninja)
	rows := [][]string{{"Nummer", "Datum", "Kunde", "Netto", "USt", "Brutto", "Offen", "Status"}}
	for _, i := range metrics.NinjaCounted(ninja) {
		if inYear(i.Date, year) {
			rows = append(rows, []string{i.Number, i.Date, names[i.ClientID], money(i.Net), money(i.Taxes), money(i.Amount), money(i.Balance), i.Status})
		}
	}
	return rows
}

func paymentRows(ninja *sources.NinjaDataset, year int) [][]string {
	names := clientNames(ninja)
	rows := [][]string{{"Datum", "Kunde", "Betrag"}}
	for _, p := range ninja.Payments {
		if inYear(p.Date, year) {
			rows = append(rows, []string{p.Date, names[p.ClientID], money(p.Amount)})
		}
	}
	return rows
}

func expenseRows(ninja *sources.NinjaDataset, year int) [][]string {
	vendors := map[string]string{}
	for _, v := range ninja.Vendors {
		vendors[v.Key] = v.Name
	}
	rows := [][]string{{"Datum", "Lieferant", "Beschreibung", "Brutto", "Vorsteuer"}}
	for _, e := range ninja.Expenses {
		if inYear(e.Date, year) {
			rows = append(rows, []string{e.Date, vendors[e.VendorKey], e.Notes, money(e.Amount), money(e.Tax)})
		}
	}
	return rows
}

func vatRows(ninja *sources.NinjaDataset, year int, method string) [][]string {
	rows := [][]string{{"Monat", "Umsatzsteuer", "Vorsteuer", "Zahllast"}}
	for m := 1; m <= monthsInYear; m++ {
		start := time.Date(year, time.Month(m), 1, 0, 0, 0, 0, time.UTC)
		end := start.AddDate(0, 1, -1)
		out, in := metrics.NinjaOutputVAT(ninja, start, end, method), metrics.NinjaInputVAT(ninja, start, end)
		rows = append(rows, []string{start.Format("2006-01"), money(out), money(in), money(out - in)})
	}
	return rows
}

func hourRows(kimai *sources.KimaiDataset, year int) [][]string {
	customers := metrics.KimaiCustomerNames(kimai)
	projects := map[int64]string{}
	for _, p := range kimai.Projects {
		projects[p.ID] = p.Name
	}
	rows := [][]string{{"Datum", "Kunde", "Projekt", "Tätigkeit", "Stunden", "Betrag", "Abrechenbar", "Exportiert"}}
	yesNo := map[bool]string{true: "ja", false: "nein"}
	for _, s := range kimai.Timesheets {
		day, ok := metrics.ParseDay(s.Begin)
		if !ok || day.Year() != year {
			continue
		}
		rows = append(rows, []string{day.Format("2006-01-02"), customers[s.CustomerID], projects[s.ProjectID], s.Activity,
			money(float64(s.Minutes) / minutesPerHour), money(s.Rate), yesNo[s.Billable], yesNo[s.Exported]})
	}
	return rows
}

// kmRate is the geo.travel_costs rule's rate of the space, else the default.
// classLabels name ride classes in the mileage log.
var classLabels = map[metrics.RideClass]string{metrics.ClassBusiness: "beruflich", metrics.ClassCommute: "Arbeitsweg"}

// reasonLabels say why a ride counts as business or commute.
var reasonLabels = map[metrics.RideReason]string{
	metrics.ReasonPlugin: "Kimai Anfahrten", metrics.ReasonKimai: "Zeit in Kimai", metrics.ReasonCustomer: "Kundenort",
	metrics.ReasonChain: "zwischen Kundenfahrten", metrics.ReasonCommute: "Wohnung – Arbeit",
}

// tripRows is the mileage log: the year's business rides and commutes
// as Dawarich tracked them; business km are priced at the km rate.
func tripRows(travel metrics.Travel, kimai *sources.KimaiDataset, year int, rate float64) [][]string {
	customers := metrics.KimaiCustomerNames(kimai)
	start := time.Date(year, 1, 1, 0, 0, 0, 0, time.UTC)
	rows := [][]string{{"Datum", "Abfahrt", "Ankunft", "Von", "Nach", "Art", "Kunde", "Verkehrsmittel", "Kilometer", "Betrag", "Grund", "Geschätzt"}}
	for _, r := range travel.Between(start, start.AddDate(1, 0, -1)) {
		label, ok := classLabels[r.Class]
		if !ok {
			continue
		}
		amount := 0.0
		if r.Class == metrics.ClassBusiness && metrics.Payable(r.Ride) {
			amount = r.KM * rate
		}
		estimated := ""
		if r.Estimated {
			estimated = "ja"
		}
		rows = append(rows, []string{r.Day().Format("2006-01-02"), r.Start.In(time.Local).Format("15:04"), r.End.In(time.Local).Format("15:04"),
			siteName(r.From), siteName(r.To), label, customers[r.CustomerID], r.Mode, money(r.KM), money(amount), reasonLabels[r.Reason], estimated})
	}
	return rows
}

func siteName(s *metrics.Site) string {
	if s == nil {
		return ""
	}
	return s.Name
}

// itCostRows lists the homelab's monthly costs with the business share
// (stacks and repos named after Kimai customers or projects).
func itCostRows(data map[string]any, kimai *sources.KimaiDataset, settings map[string]any) [][]string {
	in := metrics.CostInputs{}
	in.Hass, _ = data[string(enums.ServiceHomeAssistant)].(*sources.HassDataset)
	in.Tibber, _ = data[string(enums.ServiceTibber)].(*sources.TibberDataset)
	in.Snipe, _ = data[string(enums.ServiceSnipeIT)].(*sources.SnipeDataset)
	in.Sure, _ = data[string(enums.ServiceSure)].(*sources.SureDataset)
	in.Domains, _ = data[string(enums.ServiceDomains)].(*sources.DomainsDataset)
	bill := metrics.HomelabCost(in, metrics.HomelabSettingsOf(settings), time.Now().UTC())
	share, _, _ := metrics.BusinessShare(kimai, metrics.WorkNames(data))

	rows := [][]string{{"Posten", "Bezeichnung", "Je Monat", "Je Jahr", "Betrieblicher Anteil", "Betrieblich je Jahr"}}
	for _, item := range bill.Items {
		yearly := item.Monthly * monthsInYear
		rows = append(rows, []string{item.Key, item.Name, money(item.Monthly), money(yearly), money(share * 100), money(yearly * share)})
	}
	return rows
}
