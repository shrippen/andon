package widgetlib

import (
	"andon/internal/metrics"
	"andon/internal/repos/users"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"slices"
	"sort"
	"strings"
	"time"

	"andon/internal/db"
	"andon/internal/enums"
	"andon/internal/model"
	"andon/internal/repos/content"
	data "andon/internal/repos/data"
	"andon/internal/rules"
	"andon/internal/services/access"
	"andon/internal/services/connections"
	"andon/internal/services/hints"
	"andon/internal/services/history"
	"andon/internal/services/linkstatus"
	"andon/internal/services/svcdata"
	"andon/internal/services/util"
	"andon/internal/services/weekly"
	"andon/internal/sources"
	"andon/internal/widgets"
)

// ── Display ──

// Slot is one query's result, ready for a widget template.
type Slot struct {
	Data              any
	Error             string
	OkAt              time.Time
	MissingCredential string // connection name, "" if credentials are fine
	Pending           bool   // not fetched by a background run yet
}

// Fragment is a widget's live view: its queries' results shaped by its
// type's View function, plus its connection's hint badge.
type Fragment struct {
	WidgetID  int64
	Type      string
	Title     string
	Config    any
	Slots     map[string]Slot
	HintCount int
	HintLevel enums.Severity
	HintConn  int64 // connection the hint count belongs to, 0 = none
	View      map[string]any
	Frame     widgets.Frame
}

// Calm tells whether the tile has nothing to do and asks to be hidden then.
func (f *Fragment) Calm() bool {
	return f.Frame.OnlyIssues && f.View != nil && f.View[widgets.CalmSlot] == true
}

func sourceFor(q widgets.Query, target *model.Connection) string {
	if target == nil || !genericSources[q.Source] {
		return q.Source
	}
	if q.Source == dataSource {
		return sources.DataKey(enums.ServiceType(target.Service))
	}
	return target.Service + "." + q.Source
}

// infoKeyOf returns the connection key a link tile's info line names, or "".
func infoKeyOf(cfg any) string {
	link, ok := cfg.(widgets.LinkConfig)
	if !ok {
		return ""
	}
	return link.InfoConn
}

// infoConnection finds a connection by key: first in the widget's space,
// then in any space the viewer reaches.
func infoConnection(q db.Queryer, who *access.Principal, widget *model.Widget, key string) (*model.Connection, error) {
	found, err := content.ConnectionByKey(q, widget.SpaceID, key)
	if err != nil || found != nil {
		return found, err
	}
	for spaceID := range who.Spaces {
		found, err := content.ConnectionByKey(q, spaceID, key)
		if err != nil || found != nil {
			return found, err
		}
	}
	return nil, nil
}

// linkHost returns the lower-case host of a link tile's URL, or "".
func linkHost(cfg any) string {
	link, ok := cfg.(widgets.LinkConfig)
	if !ok {
		return ""
	}
	u, err := url.Parse(link.URL)
	if err != nil {
		return ""
	}
	return strings.ToLower(u.Hostname())
}

// hostConnection finds the connection serving host (pl.example.org →
// the Paperless connection), in the widget's space first. A link tile
// without an info connection counts that connection's hints.
func hostConnection(q db.Queryer, who *access.Principal, widget *model.Widget, host string) (*model.Connection, error) {
	spaceIDs := []int64{widget.SpaceID}
	for spaceID := range who.Spaces {
		if spaceID != widget.SpaceID {
			spaceIDs = append(spaceIDs, spaceID)
		}
	}
	slices.Sort(spaceIDs[1:])

	for _, spaceID := range spaceIDs {
		list, err := content.Connections(q, []int64{spaceID})
		if err != nil {
			return nil, err
		}
		for _, c := range list {
			u, err := url.Parse(c.URL)
			if err == nil && strings.EqualFold(u.Hostname(), host) {
				return c, nil
			}
		}
	}
	return nil, nil
}

// peerConnection finds a connection of a service for ConnPeer queries:
// first in the widget's space, then in any space the viewer reaches.
func peerConnection(q db.Queryer, who *access.Principal, widget *model.Widget, service enums.ServiceType) (*model.Connection, error) {
	spaceIDs := []int64{widget.SpaceID}
	for spaceID := range who.Spaces {
		if spaceID != widget.SpaceID {
			spaceIDs = append(spaceIDs, spaceID)
		}
	}
	slices.Sort(spaceIDs[1:])

	for _, spaceID := range spaceIDs {
		list, err := content.Connections(q, []int64{spaceID})
		if err != nil {
			return nil, err
		}
		for _, c := range list {
			if c.Service == string(service) {
				return c, nil
			}
		}
	}
	return nil, nil
}

// Load runs a widget's queries against its connection (svcdata.Get, so
// caching and credential resolution apply) and shapes the results via its
// type's View function.
func Load(ctx context.Context, d *sql.DB, who *access.Principal, widget *model.Widget, fresh svcdata.Freshness) (*Fragment, error) {
	return load(ctx, d, who, widget, fresh, originStored)
}

// origin says where a fragment's connection data comes from.
type origin int

const (
	storyDays           = 7    // the week story's default span
	originStored origin = iota // the widget's real connections
	originDemo                 // generated demo datasets, nothing fetched or stored
)

// demoURL makes sources return their demo dataset (see sources/demo.go).
const demoURL = "demo://gallery"

// demoConn is an unsaved connection that yields a service's demo data.
func demoConn(service enums.ServiceType) *model.Connection {
	if service == "" {
		return nil
	}
	return &model.Connection{Service: string(service), URL: demoURL}
}

func load(ctx context.Context, d *sql.DB, who *access.Principal, widget *model.Widget, fresh svcdata.Freshness, from origin) (*Fragment, error) {
	kind, ok := widgets.Get(widget.Type)
	if !ok {
		return nil, ErrUnknownType
	}
	cfg, _ := widgets.Decode(widget.Type, util.OpenSecrets(widget.Config))
	frag := &Fragment{WidgetID: widget.ID, Type: widget.Type, Title: widget.Title, Config: cfg, Slots: map[string]Slot{},
		Frame: widgets.FrameOf(widget.Config)}

	var conn, infoConn, hostConn *model.Connection
	var settings map[string]any
	infoKey := infoKeyOf(cfg)
	host := linkHost(cfg)
	err := db.WithRead(d, func(tx *sql.Tx) error {
		if widget.ConnectionID != nil {
			c, err := content.Connection(tx, *widget.ConnectionID)
			if err != nil {
				return err
			}
			conn = c
		}
		if infoKey != "" {
			c, err := infoConnection(tx, who, widget, infoKey)
			if err != nil {
				return err
			}
			infoConn = c
		}
		if conn == nil && infoConn == nil && host != "" {
			c, err := hostConnection(tx, who, widget, host)
			if err != nil {
				return err
			}
			hostConn = c
		}
		space, err := content.Space(tx, widget.SpaceID)
		if err != nil {
			return err
		}
		if space != nil {
			settings = space.Settings
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	if settings == nil {
		settings = map[string]any{}
	}
	if from == originDemo {
		conn = demoConn(kind.Service)
	}

	own := svcdata.Stored
	if widgets.LiveData(kind, widget.Config) {
		own = svcdata.Cached
	}
	peerOptions := map[string]map[string]any{}
	for _, q := range kind.Queries(cfg) {
		var target *model.Connection
		switch q.Conn {
		case widgets.ConnWidget:
			target = conn
		case widgets.ConnInfo:
			target = infoConn
		case widgets.ConnPeer:
			if from == originDemo {
				target = demoConn(q.Service)
				break
			}
			if target, err = peerConnection(d, who, widget, q.Service); err != nil {
				return nil, err
			}
			if target != nil {
				peerOptions[q.Name] = target.Options
			}
		}
		if q.Conn != widgets.ConnNone && target == nil {
			frag.Slots[q.Name] = Slot{Error: "connection.missing"}
			continue
		}
		if from == originDemo && target != nil {
			frag.Slots[q.Name] = demoQuery(ctx, sourceFor(q, target), q.Params)
			continue
		}
		frag.Slots[q.Name] = runQuery(ctx, d, sourceFor(q, target), q.Params, target, who.UserID, integrationFreshness(q, target, own, fresh))
	}

	serviceConn := conn
	if infoConn != nil {
		serviceConn = infoConn
	}
	if serviceConn == nil {
		serviceConn = hostConn
	}
	if serviceConn != nil && from == originStored {
		count, level, err := hints.CountFor(d, who, serviceConn.ID)
		if err != nil {
			return nil, err
		}
		frag.HintCount, frag.HintLevel, frag.HintConn = count, level, serviceConn.ID
	}

	if kind.Extra == widgets.ExtraPoints && conn != nil && from == originStored {
		trend := cfg.(widgets.TrendConfig)
		points, err := loadPoints(d, conn, who.UserID, string(trend.Metric), trend.Days)
		if err != nil {
			return nil, err
		}
		frag.Slots["points"] = Slot{Data: points}
	}

	if kind.Extra == widgets.ExtraStory {
		now := time.Now().UTC()
		start := metrics.Today(now).AddDate(0, 0, -storyDays)
		if story, _ := cfg.(widgets.StoryConfig); story.Calendar {
			start = metrics.WeekStart(metrics.Today(now))
		}
		lines, err := weekly.StorySince(ctx, d, who, start, now)
		if err != nil {
			return nil, err
		}
		frag.Slots[widgets.StorySlot] = Slot{Data: lines}
	}
	if kind.Extra == widgets.ExtraGreeting {
		g, err := greetingData(d, who, cfg.(widgets.GreetingConfig))
		if err != nil {
			return nil, err
		}
		frag.Slots[widgets.GreetingSlot] = Slot{Data: g}
	}
	wantsStrips := false
	if w, ok := cfg.(widgets.ConnHealthWanter); ok {
		wantsStrips = w.WantsConnHealth()
	}
	if kind.Extra == widgets.ExtraConnHealth || wantsStrips {
		strips, err := connections.Strips(d, who, widgets.ConnHealthDays, time.Now().UTC())
		if err != nil {
			return nil, err
		}
		frag.Slots[widgets.ConnHealthSlot] = Slot{Data: connStrips(strips)}
	}
	if kind.Extra == widgets.ExtraHintBriefs {
		hcfg := cfg.(widgets.HintSource).Hints()
		found, err := hints.Filtered(d, who, hints.Filter{MinSeverity: enums.Severity(hcfg.MinSeverity), Sources: hcfg.Sources}, 0)
		if err != nil {
			return nil, err
		}
		briefs := make([]widgets.HintBrief, len(found))
		for i, v := range found {
			briefs[i] = widgets.HintBrief{ID: v.ID, Severity: v.Severity, Title: v.Title, Sources: v.Sources}
		}
		frag.Slots[widgets.HintsSlot] = Slot{Data: briefs}
	}
	if kind.Extra == widgets.ExtraNoise {
		n, err := hints.Noise(d, who, time.Now().UTC(), extraDays(cfg, widgets.NoiseDays))
		if err != nil {
			return nil, err
		}
		data := widgets.NoiseData{Daily: n.Daily, Open: n.Open}
		for _, f := range n.Flapping {
			data.Flaps = append(data.Flaps, widgets.Flap{Rule: f.Rule, Returns: f.Returns})
		}
		frag.Slots[widgets.NoiseSlot] = Slot{Data: data}
	}
	if kind.Extra == widgets.ExtraLinksDown {
		links, err := linksDown(ctx, d, who, widget.SpaceID)
		if err != nil {
			return nil, err
		}
		frag.Slots[widgets.LinksDownSlot] = Slot{Data: links}
	}
	if kind.Extra == widgets.ExtraTimeline {
		entries, err := history.Timeline(d, who, time.Now().UTC().AddDate(0, 0, -extraDays(cfg, widgets.TimelineDays)), timelineMax)
		if err != nil {
			return nil, err
		}
		items := make([]widgets.TimelineItem, len(entries))
		for i, e := range entries {
			items[i] = widgets.TimelineItem{At: e.At, Kind: e.Kind, Subject: e.Subject, Detail: e.Detail, HintID: e.HintID, Count: e.Count}
		}
		frag.Slots[widgets.TimelineSlot] = Slot{Data: items}
	}
	if kind.Extra == widgets.ExtraForwarded && conn != nil {
		reads, err := data.MailReads(d, conn.ID)
		if err != nil {
			return nil, err
		}
		sent := map[uint32]bool{}
		for uid, fields := range reads {
			_, sent[uid] = fields[mailForwardedKey]
		}
		frag.Slots[widgets.ForwardedSlot] = Slot{Data: sent}
	}
	if kind.Extra == widgets.ExtraCloseTicks {
		u, err := users.Get(d, who.UserID)
		if err != nil {
			return nil, err
		}
		if u != nil {
			frag.Slots[widgets.CloseTicksPref] = Slot{Data: u.Prefs[widgets.CloseTicksPref]}
		}
	}
	if kind.Extra == widgets.ExtraIPWatch {
		if w, ok := cfg.(interface{ WatchesIP() bool }); ok && w.WatchesIP() {
			if ip, ok := frag.Slots["ip"].Data.(*sources.PublicIPResult); ok && ip.IP != "" {
				seen, err := watchIP(d, who, ip.IP, time.Now().UTC())
				if err != nil {
					return nil, err
				}
				frag.Slots[widgets.IPSeenSlot] = Slot{Data: seen}
			}
		}
	}
	if kind.Extra == widgets.ExtraHistory {
		h, err := history.Load(d, widget.SpaceID, 0, time.Now().UTC())
		if err != nil {
			return nil, err
		}
		frag.Slots[widgets.HistorySlot] = Slot{Data: h}
	}

	if kind.View != nil {
		viewCtx := widgets.ViewCtx{Today: time.Now().UTC().Format("2006-01-02"), Settings: settings, PeerOptions: peerOptions}
		if serviceConn != nil {
			viewCtx.Service, viewCtx.Options = serviceConn.Service, serviceConn.Options
		}
		results := map[string]any{}
		for name, slot := range frag.Slots {
			if slot.Data != nil {
				results[name] = slot.Data
			}
		}
		frag.View = kind.View(cfg, results, viewCtx)
	}
	if link, ok := cfg.(widgets.LinkConfig); ok && link.Status == widgets.StatusHTTP {
		if up, ok := linkstatus.Bars(d, widget.ID, time.Now().UTC()); ok {
			if frag.View == nil {
				frag.View = map[string]any{}
			}
			frag.View["Uptime"] = up
		}
	}

	if kind.Extra == widgets.ExtraHints {
		hcfg := cfg.(widgets.HintSource).Hints()
		filter := hints.Filter{MinSeverity: enums.Severity(hcfg.MinSeverity), Sources: hcfg.Sources, Rules: rules.RulesOf(hcfg.Topic)}
		// All matching hints for the level bar, the first Limit for the list.
		all, err := hints.Filtered(d, who, filter, 0)
		if err != nil {
			return nil, err
		}
		sevs := make([]enums.Severity, len(all))
		for i, v := range all {
			sevs[i] = v.Severity
		}
		switch hcfg.Sort {
		case widgets.HintSortValue:
			hints.ByValue(all)
		case widgets.HintSortAge:
			sort.SliceStable(all, func(i, j int) bool { return all[i].FirstSeen.Before(all[j].FirstSeen) })
		}
		if hcfg.DueDays > 0 {
			all = dueWithin(all, hcfg.DueDays, time.Now())
		}
		views := all[:min(len(all), hcfg.Limit)]
		frag.View = map[string]any{"Hints": views, "Groups": hintGroups(views), "Total": len(all), "Buttons": hcfg.Buttons,
			"Left": daysLeft(views, time.Now())}
		if !hcfg.NoLevels {
			frag.View["Levels"] = widgets.LevelBar(sevs)
		}
	}

	if widgets.IsCalm(kind.Key, frag.View) {
		frag.View[widgets.CalmSlot] = true
	}
	return frag, nil
}

// ipSeenPref is the user pref that remembers the last public IP.
const ipSeenPref = "public_ip_seen"

// watchIP compares the address with the one the viewer last saw and
// remembers a change with its time.
func watchIP(d *sql.DB, who *access.Principal, ip string, now time.Time) (widgets.IPSeen, error) {
	var seen widgets.IPSeen
	err := db.WithTx(d, func(tx *sql.Tx) error {
		u, err := users.Get(tx, who.UserID)
		if err != nil || u == nil {
			return err
		}
		stored, _ := u.Prefs[ipSeenPref].(map[string]any)
		seen.IP, _ = stored["ip"].(string)
		seen.Prev, _ = stored["prev"].(string)
		if at, ok := stored["since"].(string); ok {
			seen.Since, _ = time.Parse(time.RFC3339, at)
		}
		if seen.IP == ip {
			return nil
		}
		if seen.IP != "" {
			seen.Prev, seen.Since = seen.IP, now
		}
		seen.IP = ip
		if u.Prefs == nil {
			u.Prefs = map[string]any{}
		}
		entry := map[string]any{"ip": seen.IP, "prev": seen.Prev}
		if !seen.Since.IsZero() {
			entry["since"] = seen.Since.Format(time.RFC3339)
		}
		u.Prefs[ipSeenPref] = entry
		return users.Update(tx, u)
	})
	return seen, err
}

// extraDays is how far back a config wants its extra, else def.
func extraDays(cfg any, def int) int {
	if w, ok := cfg.(widgets.DaysWanter); ok && w.ExtraDays() > 0 {
		return w.ExtraDays()
	}
	return def
}

// integrationFreshness: connection data comes from the background run
// (Stored) unless the widget is live, then the page view fetches it
// (Cached, reused for the source TTL). Peer data (another connection,
// e.g. Kimai hours next to Ninja revenue) always stays background, so
// one widget can mix both. Sources without a connection (status ping,
// feeds, weather) keep their own cache; Force always fetches.
// own is the freshness for the widget's own connection: Cached when the
// widget is live, else Stored.
func integrationFreshness(q widgets.Query, target *model.Connection, own, fresh svcdata.Freshness) svcdata.Freshness {
	if target == nil || fresh != svcdata.Cached {
		return fresh
	}
	if q.Conn != widgets.ConnPeer {
		return own
	}
	return svcdata.Stored
}

func runQuery(ctx context.Context, d *sql.DB, source string, params map[string]any, conn *model.Connection, userID int64, fresh svcdata.Freshness) Slot {
	res, err := svcdata.Get(ctx, d, source, params, conn, &userID, fresh)
	if err != nil {
		if errors.Is(err, svcdata.ErrMissingCredential) {
			name := ""
			if conn != nil {
				name = conn.Name
			}
			return Slot{MissingCredential: name}
		}
		return Slot{Error: "source.unknown"}
	}
	return Slot{Data: res.Data, Error: res.Error, OkAt: res.OkAt, Pending: res.Pending}
}

// demoQuery asks a source directly for its demo dataset: no cache, no
// fetch log, since the connection doesn't exist.
func demoQuery(ctx context.Context, source string, params map[string]any) Slot {
	src, err := sources.Get(source)
	if err != nil {
		return Slot{Error: "source.unknown"}
	}
	out, err := src.Fetch(ctx, sources.Ctx{URL: demoURL, Params: params})
	if err != nil {
		return Slot{Error: err.Error()}
	}
	return Slot{Data: out, OkAt: time.Now().UTC()}
}

// loadPoints returns a trend widget's daily snapshots (written by the
// analysis job), scoped to the connection and credential owner.
func loadPoints(d *sql.DB, conn *model.Connection, userID int64, metric string, days int) ([][2]any, error) {
	owner := svcdata.CredentialOwner(conn, &userID)
	ownerID := int64(0)
	if owner != nil {
		ownerID = *owner
	}
	scope := fmt.Sprintf("%d:%d", conn.ID, ownerID)
	since := time.Now().UTC().AddDate(0, 0, -days).Format("2006-01-02")

	var out [][2]any
	err := db.WithRead(d, func(tx *sql.Tx) error {
		points, err := data.Points(tx, scope, metric, since)
		if err != nil {
			return err
		}
		for _, p := range points {
			out = append(out, [2]any{p.Day, p.Value})
		}
		return nil
	})
	return out, err
}

// snapshot stores a revision of a widget's own fields as JSON (see the
// same note on boards.snapshot).
func snapshot(q db.Queryer, who *access.Principal, w *model.Widget) error {
	raw, err := json.Marshal(map[string]any{"title": w.Title, "type": w.Type, "config": w.Config})
	if err != nil {
		return err
	}
	var data map[string]any
	if err := json.Unmarshal(raw, &data); err != nil {
		return err
	}
	rev := &model.Revision{
		Kind: enums.RevisionWidget, EntityID: w.ID, SpaceID: w.SpaceID, UserID: &who.UserID,
		Version: w.Version, Data: data, CreatedAt: time.Now().UTC(),
	}
	if err := content.AddRevision(q, rev); err != nil {
		return err
	}
	return content.PruneRevisions(q, enums.RevisionWidget, w.ID)
}

// Preview renders an unsaved widget with the same checks as Create
// (EDIT on the space, USE on the connection), so the editor can show
// live data before saving.
func Preview(ctx context.Context, d *sql.DB, who *access.Principal, spaceID int64, typeKey, title string,
	config map[string]any, connID *int64) (*Fragment, error) {
	err := db.WithRead(d, func(tx *sql.Tx) error {
		space, err := access.SpaceOf(tx, who, spaceID)
		if err != nil {
			return err
		}
		if err := access.Need(access.SpaceRight(who, space), enums.RightEdit); err != nil {
			return err
		}
		return checkConnection(tx, who, connID, typeKey)
	})
	if err != nil {
		return nil, err
	}
	w := &model.Widget{SpaceID: spaceID, Type: typeKey, Title: title, Config: config, ConnectionID: connID}
	return Load(ctx, d, who, w, svcdata.Cached)
}

// Demo renders a type with its default config and demo data in place of
// connections, for the gallery and for a form without a connection.
// Needs EDIT on the space, like Preview.
func Demo(ctx context.Context, d *sql.DB, who *access.Principal, spaceID int64, typeKey, title string,
	config map[string]any) (*Fragment, error) {
	if _, ok := widgets.Get(typeKey); !ok {
		return nil, ErrUnknownType
	}
	err := db.WithRead(d, func(tx *sql.Tx) error {
		space, err := access.SpaceOf(tx, who, spaceID)
		if err != nil {
			return err
		}
		return access.Need(access.SpaceRight(who, space), enums.RightEdit)
	})
	if err != nil {
		return nil, err
	}
	w := &model.Widget{SpaceID: spaceID, Type: typeKey, Title: title, Config: config}
	return load(ctx, d, who, w, svcdata.Cached, originDemo)
}

// mailForwardedKey marks a mail sent to Paperless (see mailfwd).
const mailForwardedKey = "forwarded"

// timelineMax caps what the recent-timeline tile loads.
const timelineMax = 50

// HintGroup is one severity band of the hints widget, highest first.
type HintGroup struct {
	Severity enums.Severity
	Hints    []hints.View
}

// hintGroups splits hints into severity bands, keeping their order:
//
// dueWithin keeps the hints due within days from now, soonest first.
func dueWithin(all []hints.View, days int, now time.Time) []hints.View {
	horizon := now.AddDate(0, 0, days).Format(time.DateOnly)
	var out []hints.View
	for _, v := range all {
		if v.Due != "" && v.Due <= horizon {
			out = append(out, v)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Due < out[j].Due })
	return out
}

// daysLeft maps each hint with a due date to the days until it, by id.
func daysLeft(views []hints.View, now time.Time) map[int64]int {
	today, _ := time.Parse(time.DateOnly, now.Format(time.DateOnly))
	out := map[int64]int{}
	for _, v := range views {
		if due, err := time.Parse(time.DateOnly, v.Due); err == nil {
			out[v.ID] = int(due.Sub(today).Hours() / 24)
		}
	}
	return out
}

// [warn a, crit b, info c, warn d]  →  crit [b] · warn [a d] · info [c]
func hintGroups(views []hints.View) []HintGroup {
	levels := []enums.Severity{enums.SeverityCritical, enums.SeverityWarn, enums.SeverityInfo}
	var out []HintGroup
	for _, level := range levels {
		group := HintGroup{Severity: level}
		for _, v := range views {
			if v.Severity.Key() == level.Key() {
				group.Hints = append(group.Hints, v)
			}
		}
		if len(group.Hints) > 0 {
			out = append(out, group)
		}
	}
	return out
}

// greetingData collects what a greeting says about the viewer: open hints
// and the timeline since yesterday evening in the greeting's timezone.
func greetingData(d *sql.DB, who *access.Principal, cfg widgets.GreetingConfig) (*widgets.GreetingData, error) {
	loc, err := time.LoadLocation(cfg.Timezone)
	if err != nil {
		loc = time.Local
	}
	since := widgets.GreetingSince(time.Now().In(loc), cfg.SinceHour)

	counts, err := hints.Summary(d, who)
	if err != nil {
		return nil, err
	}
	g := &widgets.GreetingData{Name: who.Name, Since: since}
	for sev, n := range counts {
		g.OpenHints += n
		if n > 0 && int(sev) > g.HintLevel {
			g.HintLevel = int(sev)
		}
	}

	entries, err := history.Timeline(d, who, since.UTC(), greetingChanges)
	if err != nil {
		return nil, err
	}
	for _, e := range entries {
		g.Changes = append(g.Changes, widgets.GreetingChange{Kind: e.Kind, Subject: e.Subject, Detail: e.Detail, At: e.At, Count: e.Count})
	}
	return g, nil
}

// greetingChanges caps the timeline a greeting reads.
const greetingChanges = 200

// connStrips hands connection strips to the widget layer.
func connStrips(strips []connections.Strip) []widgets.ConnStrip {
	out := make([]widgets.ConnStrip, len(strips))
	for i, s := range strips {
		out[i] = widgets.ConnStrip{Name: s.Name, Service: s.Service, FailPct: s.FailPct}
		for _, d := range s.Days {
			out[i].Days = append(out[i].Days, widgets.ConnDayState{Day: d.Day, OK: d.OK, Fail: d.Fail})
		}
	}
	return out
}
