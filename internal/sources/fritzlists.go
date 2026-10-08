package sources

// The FRITZ!Box's lists: devices and mesh, calls, Smart Home.
//
//	hosts  X_AVM-DE_GetHostListPath → /devicehostlist.lua?sid=…  XML <Item> per device
//	hosts  X_AVM-DE_GetMeshListPath → /meshlist.lua?sid=…        JSON nodes, interfaces, links
//
// The mesh list links the devices: the box (master) ─LAN─ powerline ─PLC─
// repeater ─WLAN─ laptop. Walking it from the box gives each AVM device
// its uplink, each other device its link rate and WLAN signal.

import (
	"context"
	"encoding/json"
	"encoding/xml"
	"sort"
	"strconv"
	"strings"
	"time"

	"andon/internal/drivers/services"
)

// Words of the lists.
const (
	meshMaster    = "master"
	meshConnected = "CONNECTED"
	avmMaker      = "AVM"
	// rcpiUnknown is the mesh list's signal when it has none.
	rcpiUnknown = 255
	// kbitPerMbitInt turns the mesh list's kbit/s into Mbit/s.
	kbitPerMbitInt = 1000
)

// fritzLinks maps the lists' link and interface types.
var fritzLinks = map[string]FritzLink{"LAN": FritzLAN, "Ethernet": FritzLAN, "WLAN": FritzWLAN, "802.11": FritzWLAN, "PLC": FritzPLC, "HomePlug": FritzPLC}

type hostList struct {
	Items []struct {
		IP       string `xml:"IPAddress"`
		MAC      string `xml:"MACAddress"`
		Active   string `xml:"Active"`
		Host     string `xml:"HostName"`
		Iface    string `xml:"InterfaceType"`
		Speed    string `xml:"X_AVM-DE_Speed"`
		Update   string `xml:"X_AVM-DE_UpdateAvailable"`
		Model    string `xml:"X_AVM-DE_Model"`
		Guest    string `xml:"X_AVM-DE_Guest"`
		Friendly string `xml:"X_AVM-DE_FriendlyName"`
	} `xml:"Item"`
}

type meshList struct {
	Nodes []meshNode `json:"nodes"`
}

type meshNode struct {
	UID        string `json:"uid"`
	Name       string `json:"device_name"`
	Model      string `json:"device_model"`
	Maker      string `json:"device_manufacturer"`
	Firmware   string `json:"device_firmware_version"`
	MAC        string `json:"device_mac_address"`
	Meshed     bool   `json:"is_meshed"`
	Role       string `json:"mesh_role"`
	Interfaces []struct {
		Links []meshLink `json:"node_links"`
	} `json:"node_interfaces"`
}

type meshLink struct {
	UID    string `json:"uid"`
	Type   string `json:"type"`
	State  string `json:"state"`
	A      string `json:"node_1_uid"`
	B      string `json:"node_2_uid"`
	Rx     int    `json:"cur_data_rate_rx"` // kbit/s
	RxRCPI int    `json:"rx_rcpi"`
	TxRCPI int    `json:"tx_rcpi"`
}

// signal is the link's WLAN signal in dBm, 0 = unknown.
func (l meshLink) signal() int {
	for _, v := range []int{l.RxRCPI, l.TxRCPI} {
		if v < 0 && v != rcpiUnknown {
			return v
		}
	}
	return 0
}

// infra reports whether a node is an AVM device of the network.
func (n meshNode) infra() bool { return n.Meshed || n.Maker == avmMaker }

// fritzNetwork reads both lists; without the mesh list the devices still
// come, without rates and signal.
func fritzNetwork(ctx context.Context, box services.TR064) ([]FritzHost, []FritzNode) {
	var hosts hostList
	if raw := fritzList(ctx, box, "X_AVM-DE_GetHostListPath", "NewX_AVM-DE_HostListPath"); raw != nil {
		_ = xml.Unmarshal(raw, &hosts)
	}
	var mesh meshList
	if raw := fritzList(ctx, box, "X_AVM-DE_GetMeshListPath", "NewX_AVM-DE_MeshListPath"); raw != nil {
		_ = json.Unmarshal(raw, &mesh)
	}
	return parseNetwork(hosts, mesh)
}

// fritzList runs a hosts action that names a list and reads that list.
func fritzList(ctx context.Context, box services.TR064, action, field string) []byte {
	ans, err := box.Call(ctx, "hosts", "Hosts:1", action)
	if err != nil || ans[field] == "" {
		return nil
	}
	raw, err := box.Fetch(ctx, ans[field])
	if err != nil {
		return nil
	}
	return raw
}

// parseNetwork joins the device list with the mesh: uplinks of AVM
// devices, rate and signal of the others.
func parseNetwork(hosts hostList, mesh meshList) ([]FritzHost, []FritzNode) {
	nodes := map[string]meshNode{}
	byMAC := map[string]string{}
	master := ""
	for _, n := range mesh.Nodes {
		nodes[n.UID] = n
		byMAC[strings.ToUpper(n.MAC)] = n.UID
		if n.Role == meshMaster || master == "" && n.infra() {
			master = n.UID
		}
	}

	// Links as neighbours, each once (both ends list it).
	type hop struct {
		to   string
		link meshLink
	}
	near := map[string][]hop{}
	seen := map[string]bool{}
	for _, n := range mesh.Nodes {
		for _, iface := range n.Interfaces {
			for _, l := range iface.Links {
				if seen[l.UID] || l.State != meshConnected {
					continue
				}
				seen[l.UID] = true
				near[l.A] = append(near[l.A], hop{l.B, l})
				near[l.B] = append(near[l.B], hop{l.A, l})
			}
		}
	}

	// Walk from the box: the link a node is reached by is its uplink.
	up := map[string]meshLink{}
	queue, reached := []string{master}, map[string]bool{master: true}
	for len(queue) > 0 {
		at := queue[0]
		queue = queue[1:]
		for _, h := range near[at] {
			if reached[h.to] {
				continue
			}
			reached[h.to], up[h.to] = true, h.link
			queue = append(queue, h.to)
		}
	}

	var out []FritzNode
	for _, n := range mesh.Nodes {
		if !n.infra() {
			continue
		}
		node := FritzNode{Name: firstStr(n.Name, n.Model), Model: n.Model, Firmware: fritzOS(n.Firmware)}
		if n.Meshed {
			node.Role = n.Role
		}
		if l, ok := up[n.UID]; ok {
			node.Uplink, node.Rate, node.Signal = fritzLinks[l.Type], l.Rx/kbitPerMbitInt, l.signal()
		}
		for _, h := range near[n.UID] {
			if !nodes[h.to].infra() {
				node.Clients++
			}
		}
		out = append(out, node)
	}

	var list []FritzHost
	for _, it := range hosts.Items {
		h := FritzHost{Name: firstStr(it.Friendly, it.Host, it.IP, it.MAC), IP: it.IP, MAC: it.MAC, Active: it.Active == "1",
			Link: fritzLinks[it.Iface], Speed: atoiOr(it.Speed), Guest: it.Guest == "1", Model: it.Model, Update: it.Model != "" && it.Update == "1"}
		if l, ok := up[byMAC[strings.ToUpper(it.MAC)]]; ok && h.Active {
			if l.Rx > 0 {
				h.Speed = l.Rx / kbitPerMbitInt
			}
			h.Signal = l.signal()
		}
		list = append(list, h)
	}
	sort.SliceStable(list, func(a, b int) bool {
		if list[a].Active != list[b].Active {
			return list[a].Active
		}
		return strings.ToLower(list[a].Name) < strings.ToLower(list[b].Name)
	})
	return list, out
}

// ── Calls ──

// The call list: the last days, the newest missed calls.
const (
	fritzCallDays    = 7
	fritzMissedShown = 10
	// fritzCallTime is the list's date ("08.10.26 03:13", the box's zone).
	fritzCallTime = "02.01.06 15:04"
)

// Call types of the list.
const (
	callIn     = "1"
	callMissed = "2"
	callOut    = "3"
)

type callList struct {
	Calls []struct {
		Type   string `xml:"Type"`
		Caller string `xml:"Caller"`
		Name   string `xml:"Name"`
		Date   string `xml:"Date"`
	} `xml:"Call"`
}

// fritzCalls reads the call list; nil when the box or user has none.
func fritzCalls(ctx context.Context, box services.TR064, now time.Time) *FritzCalls {
	ans, err := box.Call(ctx, "x_contact", "X_AVM-DE_OnTel:1", "GetCallList")
	if err != nil || ans["NewCallListURL"] == "" {
		return nil
	}
	raw, err := box.Fetch(ctx, ans["NewCallListURL"]+"&days="+strconv.Itoa(fritzCallDays))
	if err != nil {
		return nil
	}
	var list callList
	if xml.Unmarshal(raw, &list) != nil {
		return nil
	}
	return parseCalls(list, now)
}

// parseCalls counts the list; the box lists the newest first.
func parseCalls(list callList, now time.Time) *FritzCalls {
	out := &FritzCalls{Days: fritzCallDays}
	for _, c := range list.Calls {
		switch c.Type {
		case callIn:
			out.In++
		case callOut:
			out.Out++
		case callMissed:
			out.Missed++
			at, err := time.ParseInLocation(fritzCallTime, strings.TrimSpace(c.Date), now.Location())
			if err != nil || len(out.Recent) >= fritzMissedShown {
				continue
			}
			out.Recent = append(out.Recent, FritzCall{At: at, Who: firstStr(c.Name, c.Caller)})
		}
	}
	return out
}

// ── Smart Home ──

// Smart Home over TR-064, one device per index until the box answers
// 713 (index past the end).
const (
	smartMax   = 64
	smartOn    = "ENABLED"
	smartValid = "VALID"
	smartGone  = "DISCONNECTED"
	// Units of the answers: 1/100 W, Wh, 1/10 °C.
	smartPerWatt   = 100.0
	smartPerKWh    = 1000.0
	smartPerDegree = 10.0
)

func fritzSmart(ctx context.Context, box services.TR064) []FritzSmart {
	var out []FritzSmart
	for i := range smartMax {
		ans, err := box.Call(ctx, "x_homeauto", "X_AVM-DE_Homeauto:1", "GetGenericDeviceInfos", services.Arg{Name: "NewIndex", Value: strconv.Itoa(i)})
		if err != nil {
			break // 713: past the last device
		}
		if d, ok := parseSmart(ans); ok {
			out = append(out, d)
		}
	}
	return out
}

// parseSmart reads one device; one without functions (a group's
// placeholder) is left out.
func parseSmart(a map[string]string) (FritzSmart, bool) {
	if atoiOr(a["NewFunctionBitMask"]) == 0 {
		return FritzSmart{}, false
	}
	has := func(part string) bool {
		return a["New"+part+"IsEnabled"] == smartOn && a["New"+part+"IsValid"] == smartValid
	}
	d := FritzSmart{Name: a["NewDeviceName"], Product: a["NewProductName"], Present: a["NewPresent"] != smartGone}
	if state := strings.ToLower(a["NewSwitchState"]); has("Switch") && (state == "on" || state == "off") {
		d.Switch = state
	}
	if d.Meter = has("Multimeter"); d.Meter {
		d.Power = float64(atoiOr(a["NewMultimeterPower"])) / smartPerWatt
		d.Energy = float64(atoiOr(a["NewMultimeterEnergy"])) / smartPerKWh
	}
	if d.Thermo = has("Temperature"); d.Thermo {
		d.Temp = float64(atoiOr(a["NewTemperatureCelsius"])) / smartPerDegree
	}
	if d.Heater = has("Hkr"); d.Heater {
		d.Set = float64(atoiOr(a["NewHkrSetTemperature"])) / smartPerDegree
		if !d.Thermo {
			d.Thermo, d.Temp = true, float64(atoiOr(a["NewHkrIsTemperature"]))/smartPerDegree
		}
	}
	return d, true
}
