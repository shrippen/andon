// Package inventory brings the network's devices into Snipe-IT as a
// workbench (services/workbench): each device the router or the
// FRITZ!Box knows and no asset is named like (metrics.Uninventoried)
// becomes an asset after review, or is ignored from then on
// (cross.device_uninventoried's ignore list).
//
//	router, FRITZ!Box ─ devices ─┐
//	Snipe-IT ─ assets ───────────┴─► uninventoried ─► add as asset │ ignore
package inventory

import (
	"context"
	"database/sql"
	"errors"
	"slices"
	"strconv"
	"strings"

	"andon/internal/enums"
	"andon/internal/metrics"
	"andon/internal/model"
	"andon/internal/outbound"
	"andon/internal/rules"
	"andon/internal/services/access"
	"andon/internal/services/connections"
	"andon/internal/services/spaces"
	"andon/internal/services/svcdata"
	"andon/internal/services/workbench"
	"andon/internal/sources"
)

var (
	// ErrNoSnipe means the user reaches no Snipe-IT.
	ErrNoSnipe = errors.New("inventory.err_no_snipe")
	// ErrDemo means a demo connection, which takes no writes.
	ErrDemo = errors.New("inventory.err_demo")
)

var need = workbench.Need{Required: []enums.ServiceType{enums.ServiceSnipeIT},
	Optional: []enums.ServiceType{enums.ServiceGateway, enums.ServiceFritzBox}}

// The actions and fields: act_<device>=add name_… model_<model name>
// status_<id>, or act_<device>=ignore.
const (
	ActAdd      = "add"
	ActIgnore   = "ignore"
	FieldName   = "name"
	FieldModel  = "model"
	FieldStatus = "status"
)

// Fields lists the fields, for the form.
var Fields = []string{FieldName, FieldModel, FieldStatus}

const (
	ProblemGone   workbench.Problem = "inventory.problem_gone"   // in Snipe-IT or ignored meanwhile
	ProblemChoice workbench.Problem = "inventory.problem_choice" // no model or status chosen
)

// ruleID is the hint whose ignore list "ignore" extends.
const ruleID = "cross.device_uninventoried"

const prefKey = "inventory"

type state struct {
	Scope string `json:"scope,omitempty"`
}

// Page is the bench for one Verbund.
type Page struct {
	Scopes   []workbench.Scope
	Scope    string
	Choose   bool
	Cases    []metrics.NetClient
	Sources  []enums.ServiceType // what named the devices
	Models   []sources.SnipeChoice
	Statuses []sources.SnipeChoice
	Status   int64 // proposed: the first deployable status
}

type book struct {
	scope    workbench.Scope
	choose   bool
	conn     *model.Connection
	snipe    *sources.SnipeDataset
	settings map[string]any
	cases    []metrics.NetClient
	from     []enums.ServiceType
}

// Open lists the devices without asset.
func Open(ctx context.Context, d *sql.DB, who *access.Principal, scope string) (Page, error) {
	b, scopes, err := load(ctx, d, who, scope, svcdata.Stored)
	p := Page{Scopes: scopes, Scope: b.scope.Key, Choose: b.choose}
	if err != nil {
		return p, err
	}
	p.Cases, p.Sources, p.Models, p.Statuses = b.cases, b.from, b.snipe.Models, b.snipe.Statuses
	for _, s := range b.snipe.Statuses {
		if s.Kind == "deployable" {
			p.Status = s.ID
			break
		}
	}
	return p, nil
}

func load(ctx context.Context, d *sql.DB, who *access.Principal, scope string, fresh svcdata.Freshness) (book, []workbench.Scope, error) {
	scopes, err := workbench.Scopes(d, who, need)
	if err != nil {
		return book{}, nil, err
	}
	if len(scopes) == 0 {
		return book{}, nil, ErrNoSnipe
	}
	st, err := workbench.LoadState[state](d, who, prefKey)
	if err != nil {
		return book{}, scopes, err
	}
	if scope == "" {
		scope = st.Scope
	}
	var b book
	b.scope, b.choose = workbench.Pick(scopes, scope)
	b.conn = b.scope.Conn(enums.ServiceSnipeIT)

	holder := model.UserHolder(who.UserID)
	get := func(service enums.ServiceType, f svcdata.Freshness) any {
		c := b.scope.Conn(service)
		if c == nil {
			return nil
		}
		res, err := svcdata.Get(ctx, d, sources.DataKey(service), nil, c, holder, f)
		if err == nil && res.Pending {
			res, err = svcdata.Get(ctx, d, sources.DataKey(service), nil, c, holder, svcdata.Cached)
		}
		if err != nil {
			return nil
		}
		return res.Data
	}
	if b.snipe, _ = get(enums.ServiceSnipeIT, fresh).(*sources.SnipeDataset); b.snipe == nil {
		return b, scopes, ErrNoSnipe
	}
	gw, _ := get(enums.ServiceGateway, svcdata.Stored).(*sources.GatewayDataset)
	fritz, _ := get(enums.ServiceFritzBox, svcdata.Stored).(*sources.FritzDataset)
	if b.settings, err = spaces.Settings(d, who, b.conn.SpaceID); err != nil {
		b.settings = nil
	}
	var clients []metrics.NetClient
	clients, b.from = metrics.NetClients(gw, fritz)
	b.cases = metrics.Uninventoried(clients, b.snipe, ignored(b.settings))
	return b, scopes, nil
}

// ignored is the hint's ignore list of the space, lower case.
func ignored(settings map[string]any) map[string]bool {
	out := map[string]bool{}
	for _, spec := range rules.AllRules() {
		if spec.ID != ruleID {
			continue
		}
		list, _ := rules.Config(spec, settings)["ignore"].([]any)
		for _, v := range list {
			if s, ok := v.(string); ok {
				out[strings.ToLower(strings.TrimSpace(s))] = true
			}
		}
	}
	return out
}

// Item is a device as the review shows it.
type Item struct {
	metrics.NetClient
	Name          string // the asset's
	Model, Status string // by name
	ModelID       int64
	StatusID      int64
}

// Plan is the review.
type Plan struct {
	workbench.Plan[Item]
	Scope string
}

// Check: the device is still uninventoried; an asset has a name, a model
// and a status of Snipe-IT.
func (b book) Check(s *workbench.Step) (Item, workbench.Problem) {
	var it Item
	i := slices.IndexFunc(b.cases, func(c metrics.NetClient) bool { return c.Key() == s.Case })
	if i < 0 || (s.Action != ActAdd && s.Action != ActIgnore) {
		return it, ProblemGone
	}
	it.NetClient = b.cases[i]
	if s.Action == ActIgnore {
		return it, workbench.ProblemNone
	}

	it.Name = strings.TrimSpace(s.Field(FieldName))
	if it.Name == "" {
		it.Name = it.NetClient.Name
	}
	it.ModelID, it.Model = byName(b.snipe.Models, s.Field(FieldModel))
	it.StatusID, _ = strconv.ParseInt(s.Field(FieldStatus), 10, 64)
	it.Status = choice(b.snipe.Statuses, it.StatusID)
	if it.Model == "" || it.Status == "" {
		return it, ProblemChoice
	}
	return it, workbench.ProblemNone
}

// byName finds a model by its name as typed (the form offers them in a
// list), case aside.
func byName(list []sources.SnipeChoice, name string) (int64, string) {
	name = strings.TrimSpace(name)
	if i := slices.IndexFunc(list, func(c sources.SnipeChoice) bool { return strings.EqualFold(c.Name, name) }); i >= 0 {
		return list[i].ID, list[i].Name
	}
	return 0, ""
}

func choice(list []sources.SnipeChoice, id int64) string {
	if i := slices.IndexFunc(list, func(c sources.SnipeChoice) bool { return c.ID == id }); i >= 0 {
		return list[i].Name
	}
	return ""
}

// Review checks the steps and spells them out.
func Review(ctx context.Context, d *sql.DB, who *access.Principal, scope string, steps []workbench.Step) (Plan, error) {
	b, _, err := load(ctx, d, who, scope, svcdata.Cached)
	if err != nil {
		return Plan{}, err
	}
	plan, err := workbench.PlanOf[Item](b, steps)
	return Plan{Plan: plan, Scope: b.scope.Key}, err
}

// Apply adds the assets against fresh data and extends the ignore list.
func Apply(ctx context.Context, d *sql.DB, who *access.Principal, scope string, steps []workbench.Step, ip string) (workbench.Result, error) {
	b, _, err := load(ctx, d, who, scope, svcdata.Force)
	if err != nil {
		return workbench.Result{}, err
	}
	plan, err := workbench.PlanOf[Item](b, steps)
	if err != nil {
		return workbench.Result{}, err
	}
	w := &writer{book: b, d: d, who: who, ip: ip}
	if slices.ContainsFunc(plan.Steps, func(p workbench.Planned[Item]) bool { return p.Action == ActAdd && p.Problem == workbench.ProblemNone }) {
		if err := connections.Writable(d, who, b.conn.ID); err != nil {
			return workbench.Result{}, err
		}
		if sources.IsDemo(b.conn.URL) {
			return workbench.Result{}, ErrDemo
		}
		token, err := svcdata.Secret(ctx, d, b.conn, model.UserHolder(who.UserID))
		if err != nil {
			return workbench.Result{}, err
		}
		w.to = outbound.Target{URL: b.conn.URL, Token: token, VerifyTLS: b.conn.VerifyTLS}
	}
	out := workbench.Apply[Item](ctx, d, who, w, plan, "inventory", ip)
	svcdata.Forget(b.conn.ID)
	err = workbench.SaveState(d, who, prefKey, func(s *state) { s.Scope = b.scope.Key })
	return out, err
}

type writer struct {
	book
	to  outbound.Target
	d   *sql.DB
	who *access.Principal
	ip  string
}

// Write adds the asset, or puts the device on the hint's ignore list of
// the Snipe-IT connection's space.
func (w *writer) Write(ctx context.Context, p workbench.Planned[Item]) (string, error) {
	it := p.Item
	if p.Action == ActIgnore {
		return it.NetClient.Name, w.ignore(it.NetClient.Name)
	}
	notes := strings.TrimSpace(strings.Join([]string{it.IP, it.MAC}, " "))
	id, err := outbound.SnipeAssetCreate(ctx, w.to, outbound.SnipeNew{Name: it.Name, Model: it.ModelID, Status: it.StatusID, Notes: notes})
	if err != nil {
		return "", err
	}
	return it.Name + " #" + strconv.FormatInt(id, 10), nil
}

// ignore appends name to the rule's ignore list in the space settings.
func (w *writer) ignore(name string) error {
	settings, err := spaces.Settings(w.d, w.who, w.conn.SpaceID)
	if err != nil {
		return err
	}
	all, _ := settings["rules"].(map[string]any)
	merged := map[string]any{}
	for k, v := range all {
		merged[k] = v
	}
	cfg := map[string]any{}
	if old, ok := merged[ruleID].(map[string]any); ok {
		for k, v := range old {
			cfg[k] = v
		}
	}
	list, _ := cfg["ignore"].([]any)
	cfg["ignore"] = append(slices.Clone(list), strings.ToLower(name))
	merged[ruleID] = cfg
	return spaces.Update(w.d, w.who, w.conn.SpaceID, map[string]any{"rules": merged}, w.ip)
}

func (w *writer) Name(p workbench.Planned[Item]) string { return p.Item.NetClient.Name }

func (w *writer) Reason(err error) string { return err.Error() }

func (w *writer) Audit(p workbench.Planned[Item]) map[string]any {
	return map[string]any{"device": p.Item.NetClient.Name, "model": p.Item.Model, "status": p.Item.Status}
}
