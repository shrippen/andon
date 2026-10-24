package sources

// Kept (home inventory, github.com/keptinventory/kept): the things of the
// locations a personal token reaches, with purchase, warranty, expiry,
// the useful life from a custom field, receipts (attachments and Paperless
// links) and Kept's agenda of due dates. A member of the inventory
// category (caps.Inventory).
//
//	locations ─► things/sheet (purchase, warranty end, expiry)
//	          └► per bought thing: things/{id} (custom life field),
//	             attachments (if any), paperless-links (if Paperless is set up)
//	agenda ─► due dates with Kept's own lead times

import (
	"context"
	"net/url"
	"slices"
	"strings"
	"sync"
	"time"

	"andon/internal/caps"
	"andon/internal/drivers/services"
	"andon/internal/enums"
	"andon/internal/sources/demoworld"
)

const (
	keptLocations   = "api/v1/locations"
	keptSheet       = "api/v1/things/sheet"
	keptThing       = "api/v1/things/"
	keptAttachments = "/attachments"
	keptPaperless   = "api/v1/paperless-links"
	keptAgenda      = "api/v1/agenda"
	keptThingPage   = "t/"

	// KeptLifeOption names the custom field with the useful life in
	// months; KeptLifeField is its default key.
	KeptLifeOption = "life_field"
	KeptLifeField  = "nutzungsdauer"

	keptParallel  = 4
	keptAgendaAge = 30  // days back: overdue items stay in view
	keptAgendaFor = 400 // days ahead: a warranty ending next year counts
	keptMCPModule = "mcp"
	keptAddTool   = "add_warranty"
)

// KeptReceipt is one receipt of a thing: an attachment (file or link) or
// a Paperless document Kept links.
type KeptReceipt struct {
	Role      string // receipt, invoice, warranty_doc, manual …
	URL       string // the link, or the document in Paperless
	DocID     int64  // the Paperless document, 0 for an attachment
	Paperless string // the Paperless the link points into, "" for an attachment
}

// KeptThing is one thing.
type KeptThing struct {
	ID, Code, Name   string // UUID, short code (H6ZQ46), name
	Model, Serial    string
	Type, Location   string
	Bought           string // purchase date, "" none
	Price            float64
	Currency         string
	WarrantyEnds     string
	Expires          string // expiry date (fire extinguisher, first-aid kit)
	Life             int    // useful life in months from the life field, 0 none
	Receipts         []KeptReceipt
	PaperlessMissing bool // the location's Paperless is not connected (links unchecked)
	ReceiptsUnread   bool // a read of the receipts failed: receipt unknown, not missing
}

// keptProofs are the receipt roles that prove a purchase.
var keptProofs = []string{"receipt", "invoice"}

// receipt is whether a bought thing has a receipt or invoice, attached or
// linked in Paperless; things not bought need none. Unread links (a
// failed read, Paperless not connected) leave it unknown unless an
// attached receipt is there.
func (t KeptThing) receipt() ReceiptState {
	if t.Bought == "" || t.ReceiptsUnread {
		return ReceiptUnknown
	}
	for _, r := range t.Receipts {
		if slices.Contains(keptProofs, r.Role) {
			return ReceiptFound
		}
	}
	if t.PaperlessMissing {
		return ReceiptUnknown
	}
	return ReceiptNone
}

// KeptDue is one dated item of Kept's agenda.
type KeptDue struct {
	Kind    string // thing_expiry, warranty, document, schedule, loan
	State   string // upcoming, expiring, due, overdue (Kept's lead times)
	Due     string // "2026-11-20"
	Title   string
	Subject string // the thing or place
	Code    string // its short code, for the link
}

// KeptLocation is one location the token reaches.
type KeptLocation struct {
	ID, Name string
	MCP      bool // the MCP module is on: warranties can be written
}

// KeptDataset is what the token sees.
type KeptDataset struct {
	URL       string
	Locations []KeptLocation
	Things    []KeptThing
	Agenda    []KeptDue
	Writable  bool // the token may write (MCP offers its write tools)
}

// Inventory is Kept's part of the inventory shape.
func (d *KeptDataset) Inventory() *InventoryData {
	out := &InventoryData{URL: d.URL, Service: enums.ServiceKept, Assets: make([]Asset, 0, len(d.Things))}
	base := strings.TrimRight(d.URL, "/") + "/" + keptThingPage
	for _, t := range d.Things {
		out.Assets = append(out.Assets, Asset{
			ID: t.ID, Link: base + t.Code, Name: t.Name, Tag: t.Code, Model: t.Model, Category: t.Type,
			Serial: t.Serial, PurchaseDate: t.Bought, PurchaseCost: t.Price,
			WarrantyExpires: t.WarrantyEnds, DepMonths: t.Life, Receipt: t.receipt(),
		})
	}
	return out
}

// CapSet: reading needs the token only; writes a read+write token and
// the MCP module in a location.
func (d *KeptDataset) CapSet() caps.Set {
	mcp := false
	for _, l := range d.Locations {
		mcp = mcp || l.MCP
	}
	return caps.Detect(caps.HolderOf(enums.ServiceKept), func(n caps.Need) bool {
		switch n.Kind {
		case caps.NeedRight:
			return d.Writable
		case caps.NeedSetting:
			return mcp
		}
		return false
	})
}

var KeptData = source{key: "kept.data", ttl: dataTTL, service: enums.ServiceKept, fetch: fetchKept}

func fetchKept(ctx context.Context, sctx Ctx) (any, error) {
	if isDemo(sctx) {
		return DemoKept(time.Now().UTC()), nil
	}
	secret, err := needSecret(sctx)
	if err != nil {
		return nil, err
	}
	api := services.KeptApi{URL: sctx.URL, Token: secret, Verify: sctx.VerifyTLS}
	life := asStr(sctx.Options[KeptLifeOption])
	if life == "" {
		life = KeptLifeField
	}
	data, err := loadKept(ctx, api, life, time.Now().UTC())
	if err != nil {
		return nil, fetchError(err)
	}
	return data, nil
}

func loadKept(ctx context.Context, api services.KeptApi, life string, now time.Time) (*KeptDataset, error) {
	data := &KeptDataset{URL: api.URL}
	raw, err := api.Get(ctx, keptLocations, nil)
	if err != nil {
		return nil, err
	}
	for _, l := range asList(asMap(raw)["locations"]) {
		m := asMap(l)
		data.Locations = append(data.Locations, KeptLocation{ID: asStr(m["id"]), Name: asStr(m["name"]),
			MCP: slicesHas(asList(m["modules"]), keptMCPModule)})
	}

	for _, loc := range data.Locations {
		rows, err := api.Items(ctx, keptSheet, url.Values{"locationId": {loc.ID}})
		if err != nil {
			return nil, err
		}
		for _, r := range rows {
			data.Things = append(data.Things, keptThingOf(asMap(r), loc.Name))
		}
	}
	keptDetails(ctx, api, data.Things, life)

	agenda, err := api.Items(ctx, keptAgenda, url.Values{
		"from": {now.AddDate(0, 0, -keptAgendaAge).Format(time.DateOnly)},
		"to":   {now.AddDate(0, 0, keptAgendaFor).Format(time.DateOnly)}})
	if err != nil {
		return nil, err
	}
	for _, a := range agenda {
		data.Agenda = append(data.Agenda, keptDueOf(asMap(a)))
	}

	data.Writable = keptWritable(ctx, api, data.Locations)
	return data, nil
}

func keptThingOf(m map[string]any, location string) KeptThing {
	p, w := asMap(m["purchase"]), asMap(m["warranty"])
	t := KeptThing{ID: asStr(m["id"]), Code: asStr(m["shortCode"]), Name: asStr(m["name"]), Model: asStr(m["model"]),
		Serial: asStr(m["serial"]), Type: asStr(asMap(m["type"])["name"]), Location: location,
		Bought: day(p["purchasedOn"]), Price: asFloat(p["price"]), Currency: asStr(p["currency"]),
		WarrantyEnds: day(w["endsOn"]), Expires: day(m["expiresOn"])}
	if t.Type == "" {
		t.Type = asStr(asMap(m["type"])["builtinKey"])
	}
	return t
}

func keptDueOf(m map[string]any) KeptDue {
	s := asMap(m["subject"])
	return KeptDue{Kind: asStr(m["sourceType"]), State: asStr(m["state"]), Due: day(m["dueOn"]),
		Title: asStr(m["title"]), Subject: asStr(s["name"]), Code: asStr(s["shortCode"])}
}

// keptDetails reads, for every bought thing, the life field and the
// receipts, keptParallel at a time. A failed read leaves the thing as the
// sheet had it, its receipt unknown.
func keptDetails(ctx context.Context, api services.KeptApi, things []KeptThing, life string) {
	sem := make(chan struct{}, keptParallel)
	var wg sync.WaitGroup
	var mu sync.Mutex
	noPaperless := map[string]bool{} // locations whose Paperless is not connected
	for i := range things {
		if things[i].Bought == "" {
			continue
		}
		wg.Add(1)
		sem <- struct{}{}
		go func(t *KeptThing) {
			defer wg.Done()
			defer func() { <-sem }()
			detail, err := api.Get(ctx, keptThing+t.ID, nil)
			if err != nil {
				t.ReceiptsUnread = true
				return
			}
			d := asMap(detail)
			t.Life = int(asFloat(asMap(d["custom"])[life]))
			if asFloat(d["attachmentsCount"]) > 0 {
				found, err := keptAttachmentsOf(ctx, api, t.ID)
				t.Receipts = append(t.Receipts, found...)
				t.ReceiptsUnread = err != nil
			}
			mu.Lock()
			skip := noPaperless[t.Location]
			mu.Unlock()
			if skip {
				t.PaperlessMissing = true
				return
			}
			links, connected, err := keptPaperlessOf(ctx, api, t.ID)
			if err != nil {
				t.ReceiptsUnread = true
				return
			}
			if !connected {
				mu.Lock()
				noPaperless[t.Location] = true
				mu.Unlock()
				t.PaperlessMissing = true
			}
			t.Receipts = append(t.Receipts, links...)
		}(&things[i])
	}
	wg.Wait()
}

func keptAttachmentsOf(ctx context.Context, api services.KeptApi, id string) ([]KeptReceipt, error) {
	raw, err := api.Get(ctx, keptThing+id+keptAttachments, nil)
	if err != nil {
		return nil, err
	}
	var out []KeptReceipt
	for _, a := range asList(asMap(raw)["items"]) {
		m := asMap(a)
		out = append(out, KeptReceipt{Role: asStr(m["role"]), URL: asStr(m["url"])})
	}
	return out, nil
}

// keptPaperlessOf reads a thing's Paperless links; connected is false
// when the location has no working Paperless connection.
func keptPaperlessOf(ctx context.Context, api services.KeptApi, id string) ([]KeptReceipt, bool, error) {
	raw, err := api.Get(ctx, keptPaperless, url.Values{"thingId": {id}})
	if err != nil {
		return nil, false, err
	}
	m := asMap(raw)
	connected, _ := m["connected"].(bool)
	base := asStr(m["url"])
	var out []KeptReceipt
	for _, l := range asList(m["items"]) {
		lm := asMap(l)
		out = append(out, KeptReceipt{Role: asStr(lm["kind"]), URL: asStr(lm["openUrl"]), DocID: asInt64(lm["documentId"]), Paperless: base})
	}
	return out, connected, nil
}

// keptWritable asks Kept's MCP door which tools the token gets: a write
// token in a location with the MCP module is offered add_warranty. Without
// the module nothing tells read from write; it counts as read-only.
func keptWritable(ctx context.Context, api services.KeptApi, locations []KeptLocation) bool {
	for _, l := range locations {
		if !l.MCP {
			continue
		}
		names, err := api.Tools(ctx)
		return err == nil && slicesHas(names, keptAddTool)
	}
	return false
}

// slicesHas reports whether a JSON list holds the string s.
func slicesHas(list []any, s string) bool {
	for _, v := range list {
		if asStr(v) == s {
			return true
		}
	}
	return false
}

// DemoKept is Studio Weber's things in Kept.
func DemoKept(now time.Time) *KeptDataset {
	data := &KeptDataset{}
	demoworld.MustDecode("kept_state", now, data)
	return data
}

func init() {
	Register(KeptData)
	Register(testOf{KeptData, func(d any) map[string]any {
		data := d.(*KeptDataset)
		names := make([]string, 0, len(data.Locations))
		for _, l := range data.Locations {
			names = append(names, l.Name)
		}
		return map[string]any{"locations": strings.Join(names, ", "), "things": len(data.Things), TestCaps: data.CapSet()}
	}})
}
