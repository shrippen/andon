// Package demoworld holds Studio Weber, the demo world shared by all shrippen
// projects (world.json is copied from shrippen.github.io/demo by
// tools/sync-demo.py; do not edit it here). The demo:// datasets in package
// sources take their names, places and receipts from it.
package demoworld

import (
	_ "embed"
	"encoding/json"
)

//go:embed world.json
var raw []byte

// Text is a {de, en} value or a plain string.
type Text map[string]string

func (t *Text) UnmarshalJSON(b []byte) error {
	var s string
	if json.Unmarshal(b, &s) == nil {
		*t = Text{"de": s, "en": s}
		return nil
	}
	var m map[string]string
	if err := json.Unmarshal(b, &m); err != nil {
		return err
	}
	*t = m
	return nil
}

// DE is the German text (andon's demo instance is German).
func (t Text) DE() string { return t["de"] }

type Person struct {
	ID, Name, Alias, Email string
}

type Customer struct {
	ID, Name, Country string
	VATID             string `json:"vat_id"`
}

type Project struct {
	ID, Customer string
	Name         Text
	HourlyRate   float64 `json:"hourly_rate"`
}

type Activity struct {
	ID   string
	Name Text
}

type Place struct {
	ID       string
	Name     Text
	Lat, Lon float64
	Customer string
}

type Receipt struct {
	ID       int
	Vendor   string
	Day      int
	Amount   float64
	Number   string
	Category Text
	Note     Text
}

type World struct {
	Studio struct {
		Name     string
		Hostname string
	}
	People       []Person
	Customers    []Customer
	Projects     []Project
	Activities   []Activity
	Places       []Place
	Receipts     []Receipt
	DemoPassword string `json:"demo_password"`
}

var world World

func init() {
	if err := json.Unmarshal(raw, &world); err != nil {
		panic("demoworld: " + err.Error())
	}
}

// Get returns the demo world.
func Get() *World { return &world }

func (w *World) Person(id string) Person {
	for _, p := range w.People {
		if p.ID == id {
			return p
		}
	}
	panic("demoworld: no person " + id)
}

func (w *World) Customer(id string) Customer {
	for _, c := range w.Customers {
		if c.ID == id {
			return c
		}
	}
	panic("demoworld: no customer " + id)
}

func (w *World) Project(id string) Project {
	for _, p := range w.Projects {
		if p.ID == id {
			return p
		}
	}
	panic("demoworld: no project " + id)
}

func (w *World) Activity(id string) Activity {
	for _, a := range w.Activities {
		if a.ID == id {
			return a
		}
	}
	panic("demoworld: no activity " + id)
}

func (w *World) Place(id string) Place {
	for _, p := range w.Places {
		if p.ID == id {
			return p
		}
	}
	panic("demoworld: no place " + id)
}
