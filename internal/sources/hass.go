package sources

import (
	"context"
	"sort"
	"strings"
	"time"

	"andon/internal/drivers/services"
	"andon/internal/enums"
)

// Home Assistant states that mean "no value".
const (
	HassUnavailable = "unavailable"
	HassUnknown     = "unknown"
	HassOn          = "on"
)

// Entity is one Home Assistant state.
type Entity struct {
	ID, Name, Domain string
	State, Unit      string
	DeviceClass      string
	Device           string // Home Assistant device id; set for battery sensors
	Changed          time.Time
}

// batteryDevices asks Home Assistant which device each battery sensor
// belongs to, as [[entity_id, device_id], …]; the states API lacks it.
const batteryDevices = `[{% for s in states.sensor | selectattr('attributes.device_class', 'eq', 'battery') %}` +
	`{{ [s.entity_id, device_id(s.entity_id)] | tojson }}{% if not loop.last %},{% endif %}{% endfor %}]`

type HassDataset struct {
	URL      string
	Entities []Entity
}

// Find returns an entity by id.
func (d *HassDataset) Find(id string) (Entity, bool) {
	for _, e := range d.Entities {
		if e.ID == id {
			return e, true
		}
	}
	return Entity{}, false
}

var HassData = source{key: "homeassistant.data", ttl: time.Minute, service: enums.ServiceHomeAssistant, fetch: fetchHass}

func fetchHass(ctx context.Context, sctx Ctx) (any, error) {
	if isDemo(sctx) {
		return DemoHass(time.Now()), nil
	}
	secret, err := needSecret(sctx)
	if err != nil {
		return nil, err
	}
	api := services.HassApi{URL: sctx.URL, Token: secret, Verify: sctx.VerifyTLS}
	states, err := api.States(ctx)
	if err != nil {
		return nil, fetchError(err)
	}
	data := parseHass(sctx.URL, states)

	// Devices only merge battery hints: without them each sensor counts.
	if pairs, err := api.Template(ctx, batteryDevices); err == nil {
		data.setDevices(pairs)
	}
	return data, nil
}

// setDevices fills Entity.Device from [[entity_id, device_id], …].
func (d *HassDataset) setDevices(pairs any) {
	devices := map[string]string{}
	for _, raw := range asList(pairs) {
		pair := asList(raw)
		if len(pair) == 2 {
			devices[asStr(pair[0])] = asStr(pair[1])
		}
	}
	for i := range d.Entities {
		d.Entities[i].Device = devices[d.Entities[i].ID]
	}
}

func parseHass(base string, states any) *HassDataset {
	data := &HassDataset{URL: base}
	for _, raw := range asList(states) {
		m := asMap(raw)
		attrs := asMap(m["attributes"])
		id := asStr(m["entity_id"])
		domain, _, _ := strings.Cut(id, ".")
		name := asStr(attrs["friendly_name"])
		if name == "" {
			name = id
		}
		data.Entities = append(data.Entities, Entity{
			ID: id, Name: name, Domain: domain, State: asStr(m["state"]),
			Unit: asStr(attrs["unit_of_measurement"]), DeviceClass: asStr(attrs["device_class"]),
			Changed: parseTime(m["last_changed"]),
		})
	}
	sort.Slice(data.Entities, func(i, j int) bool { return data.Entities[i].ID < data.Entities[j].ID })
	return data
}

// HassPoint is one state of an entity from its start on.
type HassPoint struct {
	At    time.Time
	State string
}

// HassHistory is the last day of the tile's entities, read when the
// dialog opens (nothing stored).
type HassHistory struct{ ByID map[string][]HassPoint }

// HassHistoryHours is the span of the dialog's history.
const HassHistoryHours = 24

var HassHistorySource = source{key: "homeassistant.history", ttl: detailTTL, service: enums.ServiceHomeAssistant, fetch: fetchHassHistory}

// fetchHassHistory reads params "entities" (the tile's entity ids).
func fetchHassHistory(ctx context.Context, sctx Ctx) (any, error) {
	ids, _ := sctx.Params["entities"].([]string)
	if isDemo(sctx) {
		return DemoHassHistory(time.Now(), ids), nil
	}
	if len(ids) == 0 {
		return &HassHistory{ByID: map[string][]HassPoint{}}, nil
	}
	secret, err := needSecret(sctx)
	if err != nil {
		return nil, err
	}
	api := services.HassApi{URL: sctx.URL, Token: secret, Verify: sctx.VerifyTLS}
	body, err := api.History(ctx, time.Now().Add(-HassHistoryHours*time.Hour), ids)
	if err != nil {
		return nil, fetchError(err)
	}
	return parseHassHistory(body), nil
}

// parseHassHistory reads minimal responses: per entity a list whose first
// entry names the entity, the others only state and time.
//
//	[[{"entity_id": "sensor.t", "state": "21.0", "last_changed": "…"}, {"state": "21.5", "last_changed": "…"}]]
func parseHassHistory(body any) *HassHistory {
	out := &HassHistory{ByID: map[string][]HassPoint{}}
	for _, raw := range asList(body) {
		list := asList(raw)
		if len(list) == 0 {
			continue
		}
		id := asStr(asMap(list[0])["entity_id"])
		for _, p := range list {
			m := asMap(p)
			out.ByID[id] = append(out.ByID[id], HassPoint{At: parseTime(m["last_changed"]), State: asStr(m["state"])})
		}
	}
	return out
}

func init() {
	Register(HassHistorySource)
	Register(HassData)
	Register(testOf{HassData, func(d any) map[string]any { return map[string]any{"entities": len(d.(*HassDataset).Entities)} }})
}
