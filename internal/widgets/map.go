package widgets

// Maps in detail dialogs: a route and places, drawn in the browser by
// Kante's vector map (kante-map.js, a Protomaps basemap) from the tile
// source the admin set.
//
//	dialog ──MapData (route, marks)──► web: adds the source ──► .map-frame[data-map]

// GeoPoint is a position in degrees.
type GeoPoint struct{ Lat, Lon float64 }

// MapMark is a pin; State colours it (ok, warn, bad).
type MapMark struct {
	Lon   float64 `json:"lon"`
	Lat   float64 `json:"lat"`
	Title string  `json:"title,omitempty"`
	State string  `json:"state,omitempty"`
}

// MapData is a map block's content as Kante's data-map reads it; Source
// is set by the web layer ("" = no tile source set).
type MapData struct {
	Route  [][2]float64 `json:"route,omitempty"` // [lon, lat]
	Marks  []MapMark    `json:"marks,omitempty"`
	Source string       `json:"-"`
}

// NewMap is a map of a route and pins; ok is false without any point.
func NewMap(route []GeoPoint, marks []MapMark) (*MapData, bool) {
	if len(route) == 0 && len(marks) == 0 {
		return nil, false
	}
	m := &MapData{Marks: marks}
	for _, p := range route {
		m.Route = append(m.Route, [2]float64{p.Lon, p.Lat})
	}
	return m, true
}

// Pin is a mark at a point.
func Pin(at GeoPoint, title, state string) MapMark {
	return MapMark{Lon: at.Lon, Lat: at.Lat, Title: title, State: state}
}
