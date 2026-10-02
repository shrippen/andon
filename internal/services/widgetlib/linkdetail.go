package widgetlib

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"andon/internal/model"
	"andon/internal/services/linkstatus"
	"andon/internal/services/svcdata"
	"andon/internal/services/util"
	"andon/internal/sources"
	"andon/internal/widgets"
)

// linkInfoSource gathers a link's address facts for its detail dialog.
const linkInfoSource = "link_info"

// ErrNoStatus: the tile is no link with a status check, so it has no
// details to show.
var ErrNoStatus = errors.New("link tile without status check")

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
}

// LoadLinkDetail loads a link tile's details; the caller checked the
// viewer's right on the widget.
func LoadLinkDetail(ctx context.Context, d *sql.DB, widget *model.Widget, today time.Time) (*LinkDetail, error) {
	cfg, _ := widgets.Decode(widget.Type, util.OpenSecrets(widget.Config))
	link, ok := cfg.(widgets.LinkConfig)
	if !ok || link.Status != widgets.StatusHTTP {
		return nil, ErrNoStatus
	}
	kind, _ := widgets.Get(widget.Type)
	var params map[string]any
	for _, q := range kind.Queries(cfg) {
		if q.Source == statusSource {
			params = q.Params
		}
	}

	out := &LinkDetail{
		WidgetID: widget.ID, Title: widget.Title, URL: link.URL, Description: link.Description, Tags: link.Tags,
		Accept: link.Accept, Method: link.Method, Interval: linkstatus.Interval,
		Timeout: time.Duration(link.TimeoutS * float64(time.Second)),
	}
	if link.StatusURL != link.URL {
		out.StatusURL = link.StatusURL
	}

	if res, err := svcdata.Get(ctx, d, statusSource, params, nil, nil, svcdata.Stored); err == nil {
		out.Status, _ = res.Data.(*sources.HTTPStatusResult)
		out.CheckedAt = res.FetchedAt
	}
	out.History = linkstatus.HistoryOf(d, widget.ID, today)

	// Fetched on demand, kept an hour: redirects, certificate, clock.
	if res, err := svcdata.Get(ctx, d, linkInfoSource, params, nil, nil, svcdata.Cached); err == nil {
		out.Info, _ = res.Data.(*sources.LinkInfo)
	}
	return out, nil
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
