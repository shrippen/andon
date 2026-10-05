package widgetlib

import (
	"context"
	"database/sql"
	"time"

	"andon/internal/metrics"
	"andon/internal/model"
	"andon/internal/services/access"
	"andon/internal/services/history"
	"andon/internal/services/linkstatus"
	"andon/internal/services/svcdata"
	"andon/internal/services/util"
	"andon/internal/sources"
	"andon/internal/widgets"
)

// linkInfoSource gathers a link's address facts for its detail dialog.
const linkInfoSource = "link_info"

// LinkDetail is a link tile's detail dialog: latest check, kept days and
// the facts of its address. Secrets of the tile (status headers) stay out.
type LinkDetail struct {
	WidgetID    int64
	Title       string
	URL         string
	StatusURL   string // checked address, "" when it is URL
	Description string
	Tags        []string
	Accept      []int // codes counting as up besides 2xx and 3xx
	Method      string
	Timeout     time.Duration
	Interval    time.Duration
	Status      *sources.HTTPStatusResult // latest check, nil before the first
	CheckedAt   time.Time
	History     linkstatus.History
	Info        *sources.LinkInfo // nil if it could not be read
	Checked     bool              // the tile checks its address (else only facts)
	Clicks      widgets.Graph     // openings from boards per day, the last weeks
	ClickTotal  int
}

// clickDays is the span of a link's openings in its dialog.
const clickDays = 30

// loadLinkDetail loads a link tile's dialog: a link with a status check
// shows its history, any other one the facts of its address.
func loadLinkDetail(ctx context.Context, d *sql.DB, _ *access.Principal, widget *model.Widget, today time.Time) (*DetailDialog, error) {
	cfg, _ := widgets.Decode(widget.Type, util.OpenSecrets(widget.Config))
	link, ok := cfg.(widgets.LinkConfig)
	if !ok {
		return nil, ErrNoDetail
	}
	kind, _ := widgets.Get(widget.Type)
	params := map[string]any{"url": link.URL}
	for _, q := range kind.Queries(cfg) {
		if q.Source == statusSource {
			params = q.Params
		}
	}

	out := &LinkDetail{
		WidgetID: widget.ID, Title: widget.Title, URL: link.URL, Description: link.Description, Tags: link.Tags,
		Accept: link.Accept, Method: link.Method, Interval: linkstatus.Interval,
		Timeout: time.Duration(link.TimeoutS * float64(time.Second)), Checked: link.Status == widgets.StatusHTTP,
	}
	if link.StatusURL != link.URL {
		out.StatusURL = link.StatusURL
	}

	if out.Checked {
		if res, err := svcdata.Get(ctx, d, statusSource, params, nil, model.NoHolder, svcdata.Stored); err == nil {
			out.Status, _ = res.Data.(*sources.HTTPStatusResult)
			out.CheckedAt = res.FetchedAt
		}
		out.History = linkstatus.HistoryOf(d, widget.ID, today)
	}

	if h, err := history.Load(d, widget.SpaceID, 0, today); err == nil {
		perDay := map[string]float64{}
		for _, p := range h.SeriesOf(metrics.LinkClicksKey(widget.ID)) {
			perDay[p.Day.Format(time.DateOnly)] += p.Value
		}
		values := make([]float64, clickDays)
		for i := range values {
			values[i] = perDay[today.AddDate(0, 0, i-clickDays+1).Format(time.DateOnly)]
			out.ClickTotal += int(values[i])
		}
		out.Clicks = widgets.ColGraph(values, "s1")
		out.Clicks.Ticks = []any{widgets.Day(today.AddDate(0, 0, 1-clickDays)), widgets.Day(today)}
	}

	// Fetched on demand, kept an hour: redirects, certificate, clock.
	if res, err := svcdata.Get(ctx, d, linkInfoSource, params, nil, model.NoHolder, svcdata.Cached); err == nil {
		out.Info, _ = res.Data.(*sources.LinkInfo)
	}
	return &DetailDialog{Type: widget.Type, Head: out.head(), Body: out}, nil
}

// head: up, down or never checked; check now and open the address.
func (l *LinkDetail) head() DetailHead {
	h := DetailHead{Title: l.Title, Sub: l.Description, State: "off", StateKey: "linkdetail.never",
		Actions: []DetailAction{{LabelKey: "linkdetail.check_now", Refresh: true}, {LabelKey: "linkdetail.open", Href: l.URL, Primary: true}}}
	if !l.Checked {
		h.State, h.StateKey = "", ""
		h.Actions = h.Actions[1:]
	}
	switch {
	case l.Status == nil:
	case l.Status.Up:
		h.State, h.StateKey = "ok", "status.up"
	default:
		h.State, h.StateKey = "bad", "status.down"
	}
	return h
}

// TLSDaysLeft counts the days until the certificate expires, -1 when
// none was read.
func (l *LinkDetail) TLSDaysLeft() int {
	if l.Info == nil || l.Info.TLSUntil.IsZero() {
		return -1
	}
	return int(time.Until(l.Info.TLSUntil).Hours() / hoursPerDay)
}

const hoursPerDay = 24
