package sources

// Receipt matching reads Invoice Ninja expenses and Paperless documents
// with their custom fields, where the link between both is kept:
//
//	ninja.expenses    every expense: number, vendor, amount, custom_value1–4
//	paperless.docs    documents created in one year (param "year"), with
//	                  correspondent, OCR text (truncated) and custom fields
//
// One-off reads for the page (one expense, some documents, a search, a
// thumbnail) live here too; they skip the cache.

import (
	"context"
	"encoding/json"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"andon/internal/drivers/services"
	"andon/internal/enums"
)

// NinjaSlots is how many custom values an Invoice Ninja expense has.
const NinjaSlots = 4

// ReceiptExpense is one Invoice Ninja expense as the matcher sees it.
type ReceiptExpense struct {
	Key     string // id as sent (hashed in v5), for write calls
	Number  string
	Vendor  string
	Notes   string
	Day     string // "2026-08-01", "" if unknown
	Amount  float64
	Custom  [NinjaSlots]string // custom_value1 … custom_value4
	Updated time.Time
}

// ExpenseSet is every expense of one Invoice Ninja plus the names of the
// expense custom fields ("" = unnamed).
type ExpenseSet struct {
	URL      string
	Expenses []ReceiptExpense
	Slots    [NinjaSlots]string
}

// ReceiptDoc is one Paperless document as the matcher sees it.
type ReceiptDoc struct {
	ID            int64
	Title         string
	Correspondent string
	Day           string // created, "2026-08-02"
	Added         string
	Content       string // OCR text, truncated by Paperless
	Custom        map[int64]any
	Tags          []int64
}

// DocField is one Paperless custom field definition.
type DocField struct {
	ID         int64
	Name, Type string // Type: string, url, monetary, …
}

// DocSet is one year of Paperless documents plus the custom fields and
// tags that exist.
type DocSet struct {
	URL      string
	Docs     []ReceiptDoc
	Fields   []DocField
	Tags     map[string]int64 // lower-case name → id
	TagNames []string         // as written, sorted
}

const (
	receiptTTL      = 3 * time.Minute
	paperlessPage   = "100"
	maxDocPages     = 40
	searchPages     = 2
	searchPageSize  = "25"
	idChunk         = 50
	ninjaLabelSplit = "|" // "Rechnungsnummer|single_line_text"
)

var NinjaExpenses = source{key: "ninja.expenses", ttl: receiptTTL, service: enums.ServiceInvoiceNinja, fetch: fetchNinjaExpenses}

func fetchNinjaExpenses(ctx context.Context, sctx Ctx) (any, error) {
	if isDemo(sctx) {
		return DemoExpenses(time.Now()), nil
	}
	api, err := ninjaAPI(sctx)
	if err != nil {
		return nil, err
	}
	raw, err := api.Pages(ctx, "expenses", url.Values{"include": {"vendor"}, "is_deleted": {"false"}})
	if err != nil {
		return nil, fetchError(err)
	}
	set := &ExpenseSet{URL: sctx.URL, Slots: ninjaSlotLabels(ctx, api)}
	for _, e := range raw {
		set.Expenses = append(set.Expenses, receiptExpense(e))
	}
	return set, nil
}

func receiptExpense(raw any) ReceiptExpense {
	m := asMap(raw)
	e := ReceiptExpense{
		Key: idKey(m["id"]), Number: asStr(m["number"]), Vendor: asStr(asMap(m["vendor"])["name"]),
		Notes: asStr(m["public_notes"]), Day: day(m["date"]), Amount: asFloat(m["amount"]),
	}
	if e.Vendor == "" {
		e.Vendor = asStr(m["vendor_name"])
	}
	for i := range e.Custom {
		e.Custom[i] = strings.TrimSpace(asStr(m["custom_value"+strconv.Itoa(i+1)]))
	}
	if at := asFloat(m["updated_at"]); at > 0 {
		e.Updated = time.Unix(int64(at), 0).UTC()
	}
	return e
}

// ninjaSlotLabels reads the expense custom field names from the company
// settings ("expense1": "Rechnungsnummer|single_line_text"). Failures
// leave them unnamed.
func ninjaSlotLabels(ctx context.Context, api services.NinjaApi) [NinjaSlots]string {
	var out [NinjaSlots]string
	body, err := api.Get(ctx, "companies", nil)
	if err != nil {
		return out
	}
	company := asMap(body)
	if list := asList(company["data"]); len(list) > 0 {
		company = asMap(list[0])
	}
	fields := asMap(company["custom_fields"])
	for i := range out {
		label, _, _ := strings.Cut(asStr(fields["expense"+strconv.Itoa(i+1)]), ninjaLabelSplit)
		out[i] = strings.TrimSpace(label)
	}
	return out
}

// NinjaExpenseByKey reads one expense fresh, for a write that depends on it.
func NinjaExpenseByKey(ctx context.Context, sctx Ctx, key string) (ReceiptExpense, error) {
	if isDemo(sctx) {
		for _, e := range DemoExpenses(time.Now()).Expenses {
			if e.Key == key {
				return e, nil
			}
		}
		return ReceiptExpense{}, newSourceError("expense %s not found", key)
	}
	api, err := ninjaAPI(sctx)
	if err != nil {
		return ReceiptExpense{}, err
	}
	body, err := api.Get(ctx, "expenses/"+url.PathEscape(key), url.Values{"include": {"vendor"}})
	if err != nil {
		return ReceiptExpense{}, fetchError(err)
	}
	return receiptExpense(asMap(body)["data"]), nil
}

var PaperlessDocs = source{key: "paperless.docs", ttl: receiptTTL, service: enums.ServicePaperless, fetch: fetchPaperlessDocs}

// Fetch reads the documents created in Params["year"] (this year if
// missing).
func fetchPaperlessDocs(ctx context.Context, sctx Ctx) (any, error) {
	year := int(asFloat(sctx.Params["year"]))
	if year == 0 {
		year = time.Now().Year()
	}
	if isDemo(sctx) {
		return DemoDocs(time.Now(), year), nil
	}
	api, err := paperlessAPI(sctx)
	if err != nil {
		return nil, err
	}
	set := &DocSet{URL: sctx.URL, Tags: map[string]int64{}}
	if set.Fields, err = paperlessFields(ctx, api); err != nil {
		return nil, fetchError(err)
	}
	if tags, err := api.Get(ctx, "tags/", url.Values{"page_size": {paperlessTagPage}}); err == nil {
		for _, raw := range asList(asMap(tags)["results"]) {
			tag := asMap(raw)
			set.Tags[strings.ToLower(asStr(tag["name"]))] = asInt64(tag["id"])
			set.TagNames = append(set.TagNames, asStr(tag["name"]))
		}
		slices.Sort(set.TagNames)
	}
	names := correspondentNames(ctx, api)
	query := url.Values{
		"created__date__gte": {strconv.Itoa(year) + "-01-01"}, "created__date__lte": {strconv.Itoa(year) + "-12-31"},
		"page_size": {paperlessPage}, "truncate_content": {"true"},
	}
	if set.Docs, err = documentPages(ctx, api, query, maxDocPages, names); err != nil {
		return nil, fetchError(err)
	}
	return set, nil
}

func paperlessFields(ctx context.Context, api services.PaperlessApi) ([]DocField, error) {
	body, err := api.Get(ctx, "custom_fields/", url.Values{"page_size": {paperlessPage}})
	if err != nil {
		return nil, err
	}
	var out []DocField
	for _, raw := range asList(asMap(body)["results"]) {
		f := asMap(raw)
		out = append(out, DocField{ID: asInt64(f["id"]), Name: asStr(f["name"]), Type: asStr(f["data_type"])})
	}
	return out, nil
}

// correspondentNames maps correspondent ids to names; failures leave
// documents without correspondent.
func correspondentNames(ctx context.Context, api services.PaperlessApi) map[int64]string {
	out := map[int64]string{}
	body, err := api.Get(ctx, "correspondents/", url.Values{"page_size": {paperlessTagPage}})
	if err != nil {
		return out
	}
	for _, raw := range asList(asMap(body)["results"]) {
		c := asMap(raw)
		out[asInt64(c["id"])] = asStr(c["name"])
	}
	return out
}

// documentPages follows Paperless' "next" links up to pages pages.
func documentPages(ctx context.Context, api services.PaperlessApi, query url.Values, pages int, names map[int64]string) ([]ReceiptDoc, error) {
	var out []ReceiptDoc
	for page := 1; page <= pages; page++ {
		query.Set("page", strconv.Itoa(page))
		body, err := api.Get(ctx, "documents/", query)
		if err != nil {
			return nil, err
		}
		m := asMap(body)
		rows := asList(m["results"])
		for _, raw := range rows {
			out = append(out, receiptDoc(raw, names))
		}
		if m["next"] == nil || len(rows) == 0 {
			break
		}
	}
	return out, nil
}

func receiptDoc(raw any, names map[int64]string) ReceiptDoc {
	m := asMap(raw)
	doc := ReceiptDoc{
		ID: asInt64(m["id"]), Title: asStr(m["title"]), Day: day(m["created_date"]), Added: day(m["added"]),
		Content: asStr(m["content"]), Custom: map[int64]any{},
	}
	if doc.Day == "" {
		doc.Day = day(m["created"])
	}
	if c, ok := m["correspondent"].(map[string]any); ok {
		doc.Correspondent = asStr(c["name"])
	} else {
		doc.Correspondent = names[asInt64(m["correspondent"])]
	}
	for _, raw := range asList(m["custom_fields"]) {
		f := asMap(raw)
		doc.Custom[asInt64(f["field"])] = f["value"]
	}
	for _, t := range asList(m["tags"]) {
		doc.Tags = append(doc.Tags, asInt64(t))
	}
	return doc
}

// PaperlessDocsByID reads documents fresh, for a write that depends on
// them. Unknown ids are left out.
func PaperlessDocsByID(ctx context.Context, sctx Ctx, ids []int64) ([]ReceiptDoc, error) {
	if isDemo(sctx) {
		var out []ReceiptDoc
		for _, doc := range DemoDocs(time.Now(), 0).Docs {
			for _, id := range ids {
				if doc.ID == id {
					out = append(out, doc)
				}
			}
		}
		return out, nil
	}
	api, err := paperlessAPI(sctx)
	if err != nil {
		return nil, err
	}
	names := correspondentNames(ctx, api)
	var out []ReceiptDoc
	for start := 0; start < len(ids); start += idChunk {
		chunk := ids[start:min(start+idChunk, len(ids))]
		list := make([]string, len(chunk))
		for i, id := range chunk {
			list[i] = strconv.FormatInt(id, 10)
		}
		found, err := documentPages(ctx, api, url.Values{"id__in": {strings.Join(list, ",")}, "page_size": {paperlessPage},
			"truncate_content": {"true"}}, 1, names)
		if err != nil {
			return nil, fetchError(err)
		}
		out = append(out, found...)
	}
	return out, nil
}

// DocSearch is a manual Paperless search: full text, title/content,
// correspondent, a creation window, and optionally only documents whose
// custom field EmptyField is empty (not linked yet).
type DocSearch struct {
	Query, TitleContent, Correspondent string
	From, To                           string // "2026-01-01"
	EmptyField                         string // custom field name, "" = any
}

// PaperlessSearch runs a manual search, at most two pages of hits.
func PaperlessSearch(ctx context.Context, sctx Ctx, s DocSearch) ([]ReceiptDoc, error) {
	if isDemo(sctx) {
		return demoSearch(time.Now(), s), nil
	}
	api, err := paperlessAPI(sctx)
	if err != nil {
		return nil, err
	}
	query := url.Values{"page_size": {searchPageSize}, "truncate_content": {"true"}}
	set := func(key, value string) {
		if value != "" {
			query.Set(key, value)
		}
	}
	set("query", s.Query)
	set("title_content", s.TitleContent)
	set("correspondent__name__icontains", s.Correspondent)
	set("created__date__gte", s.From)
	set("created__date__lte", s.To)
	if s.EmptyField != "" {
		// ["OR", [["Ausgabe", "isnull", true], ["Ausgabe", "exact", ""]]]
		empty, _ := json.Marshal([]any{"OR", []any{[]any{s.EmptyField, "isnull", true}, []any{s.EmptyField, "exact", ""}}})
		query.Set("custom_field_query", string(empty))
	}
	docs, err := documentPages(ctx, api, query, searchPages, correspondentNames(ctx, api))
	if err != nil {
		return nil, fetchError(err)
	}
	return docs, nil
}

// PaperlessThumb reads a document's thumbnail image.
func PaperlessThumb(ctx context.Context, sctx Ctx, id int64) ([]byte, string, error) {
	if isDemo(sctx) {
		return demoThumb(time.Now(), id)
	}
	api, err := paperlessAPI(sctx)
	if err != nil {
		return nil, "", err
	}
	body, kind, err := api.Bytes(ctx, "documents/"+strconv.FormatInt(id, 10)+"/thumb/")
	if err != nil {
		return nil, "", fetchError(err)
	}
	return body, kind, nil
}

// NinjaVendorKey finds a vendor by name (case-insensitive), "" if none.
func NinjaVendorKey(ctx context.Context, sctx Ctx, name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" || isDemo(sctx) {
		return "", nil
	}
	api, err := ninjaAPI(sctx)
	if err != nil {
		return "", err
	}
	list, err := api.Pages(ctx, "vendors", url.Values{"name": {name}})
	if err != nil {
		return "", fetchError(err)
	}
	for _, raw := range list {
		if v := asMap(raw); strings.EqualFold(strings.TrimSpace(asStr(v["name"])), name) {
			return idKey(v["id"]), nil
		}
	}
	return "", nil
}

// IsDemo tells whether a connection URL is a demo:// one, which answers
// from made-up data and takes no writes.
func IsDemo(rawURL string) bool { return isDemo(Ctx{URL: rawURL}) }

func init() {
	Register(NinjaExpenses)
	Register(PaperlessDocs)
}
