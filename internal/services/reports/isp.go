// Package reports builds printable evidence from recorded history.
//
//	ISP  one month of speed measurements, WAN outages and the router's
//	     reconnects per space, as a table and as CSV for a complaint to
//	     the provider (§ 57 TKG)
package reports

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/csv"
	"fmt"
	"maps"
	"slices"
	"strconv"
	"strings"
	"time"

	"andon/internal/enums"
	"andon/internal/metrics"
	"andon/internal/model"
	"andon/internal/services/access"
	"andon/internal/services/connections"
	"andon/internal/services/history"
	"andon/internal/services/svcdata"
	"andon/internal/sources"
)

const (
	// ISPDays is the report window.
	ISPDays = 30
	// ISPShare: a day counts as too slow below this share of the booked speed.
	ISPShare = 0.9
	csvComma = ';'
	utf8BOM  = "\ufeff"
)

// ISP is one space's report.
type ISP struct {
	Space                string
	ExpectDown, ExpectUp float64
	Speed                bool // from a Speedtest Tracker; else only the router's reconnects
	metrics.SpeedReport
}

// ISPReports lists a report per usable Speedtest Tracker connection, and
// one per space that has only a FRITZ!Box (its reconnects).
func ISPReports(ctx context.Context, d *sql.DB, who *access.Principal) ([]ISP, error) {
	views, err := connections.Listing(d, who, enums.RightUse)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	var out []ISP
	covered := map[int64]bool{} // spaces with a report
	routers := map[int64]bool{} // spaces with a FRITZ!Box
	for _, v := range views {
		if v.Service == enums.ServiceFritzBox {
			if conn, err := connections.ByID(d, v.ID); err == nil && conn != nil {
				routers[conn.SpaceID] = true
			}
		}
		if v.Service != enums.ServiceSpeedtest {
			continue
		}
		conn, err := connections.ByID(d, v.ID)
		if err != nil || conn == nil {
			continue
		}
		uid := who.UserID
		res, err := svcdata.Get(ctx, d, sources.DataKey(enums.ServiceSpeedtest), nil, conn, model.UserHolder(uid), svcdata.Stored)
		if err != nil {
			continue
		}
		st, ok := res.Data.(*sources.SpeedtestDataset)
		if !ok {
			continue
		}
		h, err := history.Load(d, conn.SpaceID, 0, now)
		if err != nil {
			return nil, err
		}
		covered[conn.SpaceID] = true
		out = append(out, ISP{Space: spaceName(who, conn.SpaceID), ExpectDown: st.ExpectDown, ExpectUp: st.ExpectUp, Speed: true,
			SpeedReport: metrics.SpeedDays(h, st.ExpectDown, ISPShare, now, ISPDays)})
	}

	for _, spaceID := range slices.Sorted(maps.Keys(routers)) {
		if covered[spaceID] {
			continue
		}
		h, err := history.Load(d, spaceID, 0, now)
		if err != nil {
			return nil, err
		}
		out = append(out, ISP{Space: spaceName(who, spaceID), SpeedReport: metrics.SpeedDays(h, 0, ISPShare, now, ISPDays)})
	}
	return out, nil
}

// spaceName is a space's name as the caller sees it.
func spaceName(who *access.Principal, spaceID int64) string {
	if ref, ok := who.Spaces[spaceID]; ok {
		return ref.Name
	}
	return ""
}

// downText is a reconnect's downtime for the CSV.
func downText(r metrics.Reconnect) string {
	if !r.Seen {
		return "kurz"
	}
	return fmt.Sprintf("Ausfall mind. %.0f min", r.Down.Minutes())
}

// num: 243.5 → "243,5".
func num(v float64) string {
	return strings.Replace(strconv.FormatFloat(v, 'f', 1, 64), ".", ",", 1)
}

// ISPCSV renders the reports as one CSV (semicolon, decimal comma, BOM).
func ISPCSV(reports []ISP) ([]byte, error) {
	var buf bytes.Buffer
	buf.WriteString(utf8BOM)
	w := csv.NewWriter(&buf)
	w.Comma = csvComma
	rows := [][]string{{"Bereich", "Datum", "Download Mbit/s", "Upload Mbit/s", "Gebucht Download", "Unter " + fmt.Sprintf("%.0f %%", ISPShare*100)}}
	for _, r := range reports {
		for _, day := range r.Days {
			below := ""
			if day.Below {
				below = "ja"
			}
			rows = append(rows, []string{r.Space, day.Day.Format(time.DateOnly), num(day.Down), num(day.Up), num(r.ExpectDown), below})
		}
		for _, o := range r.Outages {
			end := ""
			if !o.End.IsZero() {
				end = o.End.Format(time.DateTime)
			}
			rows = append(rows, []string{r.Space, o.Start.Format(time.DateTime), "WAN-Ausfall " + o.Gateway, "bis " + end, "", ""})
		}
		for _, c := range r.Reconnects {
			rows = append(rows, []string{r.Space, c.At.Format(time.DateTime), "Neuverbindung", downText(c), "", ""})
		}
	}
	if err := w.WriteAll(rows); err != nil {
		return nil, err
	}
	return buf.Bytes(), w.Error()
}
