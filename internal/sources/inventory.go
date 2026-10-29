package sources

import (
	"strconv"
	"strings"

	"andon/internal/enums"
)

// The inventory category's shape (caps.Inventory): every inventory
// service fills it, and rules, metrics and tiles read only it.
//
//	SnipeDataset.Inventory() ─┐
//	                          ├─► InventoryData{Assets} ─► depreciation, inventory.* …
//	(Kept, …)                 ┘

// Asset is one thing of an inventory: what Andon needs for depreciation,
// warranty, costs and purchases.
type Asset struct {
	ID              string // the service's id, as text (Snipe-IT counts, others use UUIDs)
	Name            string
	Tag             string // inventory number or short code
	Link            string // the asset's page in its service
	Model           string
	Category        string
	Serial          string
	PurchaseDate    string // "2026-03-15", "" unknown
	PurchaseCost    float64
	WarrantyExpires string
	// DepMonths is the useful life in months for depreciation, 0 none.
	DepMonths int
	Receipt   ReceiptState
}

// ReceiptState says whether an asset's purchase has a receipt.
type ReceiptState string

const (
	ReceiptUnknown ReceiptState = ""     // the service keeps no receipts (Snipe-IT)
	ReceiptNone    ReceiptState = "none" // it could, and has none
	ReceiptFound   ReceiptState = "found"
)

// AssetDate is a dated item of an inventory: a warranty end, an end of
// life, a licence, or what the service keeps on its agenda (Kept).
type AssetDate struct {
	Kind string // warranty, eol, license; Kept: thing_expiry, document, schedule, loan
	Name string
	Date string // "2026-11-20"
	Link string // its page in the service, "" none
	Late bool   // the service calls it due or overdue
}

// InventoryData is an inventory connection's assets and dates.
type InventoryData struct {
	URL     string
	Service enums.ServiceType // where they come from, e.g. for "open in …"
	Assets  []Asset
	Dates   []AssetDate
}

// InventorySource is a dataset of the inventory category.
type InventorySource interface{ Inventory() *InventoryData }

// Inventory is Snipe-IT's part of the inventory shape.
func (d *SnipeDataset) Inventory() *InventoryData {
	out := &InventoryData{URL: d.URL, Service: enums.ServiceSnipeIT, Assets: make([]Asset, 0, len(d.Assets))}
	base := strings.TrimRight(d.URL, "/") + "/hardware/"
	for _, a := range d.Assets {
		id := strconv.FormatInt(a.ID, 10)
		out.Assets = append(out.Assets, Asset{
			ID: id, Link: base + id, Name: a.Name, Tag: a.Tag, Model: a.Model, Category: a.Category,
			Serial: a.Serial, PurchaseDate: a.PurchaseDate, PurchaseCost: a.PurchaseCost,
			WarrantyExpires: a.WarrantyExpires, DepMonths: a.DepMonths,
		})
		for _, d := range []AssetDate{{Kind: "warranty", Date: a.WarrantyExpires}, {Kind: "eol", Date: a.EOLDate}} {
			if d.Date != "" {
				d.Name, d.Link = a.Name, base+id
				out.Dates = append(out.Dates, d)
			}
		}
	}

	licenses := strings.TrimRight(d.URL, "/") + "/licenses/"
	for _, l := range d.Licenses {
		if l.Expires != "" {
			out.Dates = append(out.Dates, AssetDate{Kind: "license", Name: l.Name, Date: l.Expires,
				Link: licenses + strconv.FormatInt(l.ID, 10)})
		}
	}
	return out
}
