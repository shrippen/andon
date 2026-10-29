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
	// ReceiptDocs are the Paperless documents that prove the purchase
	// (Kept's links), nil none or unknown.
	ReceiptDocs []int64
}

// ReceiptState says whether an asset's purchase has a receipt.
type ReceiptState string

const (
	ReceiptUnknown ReceiptState = ""     // the service keeps no receipts (Snipe-IT)
	ReceiptNone    ReceiptState = "none" // it could, and has none
	ReceiptFound   ReceiptState = "found"
)

// InventoryData is an inventory connection's assets.
type InventoryData struct {
	URL     string
	Service enums.ServiceType // where they come from, e.g. for "open in …"
	Assets  []Asset
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
	}
	return out
}
