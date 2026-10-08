package sources_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"andon/internal/sources"
)

// fakeFritz answers like a FRITZ!Box 7590 on a DSL line: TR-064 actions
// by control and action, the lists by path.
func fakeFritz(t *testing.T) *httptest.Server {
	t.Helper()
	answers := map[string]string{
		"wanipconnection1 GetInfo": `<NewConnectionStatus>Connected</NewConnectionStatus><NewUptime>600</NewUptime>`,
		"wandslifconfig1 GetInfo": `<NewDownstreamCurrRate>250000</NewDownstreamCurrRate><NewUpstreamCurrRate>40000</NewUpstreamCurrRate>` +
			`<NewDownstreamNoiseMargin>55</NewDownstreamNoiseMargin><NewUpstreamNoiseMargin>90</NewUpstreamNoiseMargin>`,
		"deviceinfo GetInfo": `<NewModelName>FRITZ!Box 7590</NewModelName><NewSoftwareVersion>154.08.03</NewSoftwareVersion>`,
		"userif GetInfo":     `<NewUpgradeAvailable>1</NewUpgradeAvailable><NewX_AVM-DE_Version>154.08.21</NewX_AVM-DE_Version><NewX_AVM-DE_UpdateState>UpdateAvailable</NewX_AVM-DE_UpdateState>`,
		// Newest first, bytes/s: 125000 B/s = 1000 kbit/s.
		"wancommonifconfig1 X_AVM-DE_GetOnlineMonitor": `<Newds_current_bps>125000,250</Newds_current_bps><Newus_current_bps>500,0</Newus_current_bps>`,
		"wlanconfig1 GetInfo":                          `<NewStatus>Up</NewStatus><NewChannel>6</NewChannel><NewX_AVM-DE_FrequencyBand>2400</NewX_AVM-DE_FrequencyBand>`,
		"wlanconfig1 GetTotalAssociations":             `<NewTotalAssociations>3</NewTotalAssociations>`,
		"wlanconfig2 GetInfo":                          `<NewStatus>Disabled</NewStatus><NewX_AVM-DE_FrequencyBand>unknown</NewX_AVM-DE_FrequencyBand>`,
		"hosts X_AVM-DE_GetHostListPath":               `<NewX_AVM-DE_HostListPath>/devicehostlist.lua?sid=s</NewX_AVM-DE_HostListPath>`,
		"hosts X_AVM-DE_GetMeshListPath":               `<NewX_AVM-DE_MeshListPath>/meshlist.lua?sid=s</NewX_AVM-DE_MeshListPath>`,
		"x_contact GetCallList":                        `<NewCallListURL>http://192.168.178.1:49000/calllist.lua?sid=s</NewCallListURL>`,
	}
	lists := map[string]string{
		"/devicehostlist.lua": `<List>
<Item><IPAddress>192.168.178.21</IPAddress><MACAddress>aa:00:00:00:00:01</MACAddress><Active>1</Active><HostName>laptop</HostName><InterfaceType>802.11</InterfaceType><X_AVM-DE_Speed>200</X_AVM-DE_Speed><X_AVM-DE_Model/><X_AVM-DE_FriendlyName>Laptop</X_AVM-DE_FriendlyName></Item>
<Item><IPAddress/><MACAddress>aa:00:00:00:00:02</MACAddress><Active>0</Active><HostName>old-phone</HostName><InterfaceType/><X_AVM-DE_Speed>0</X_AVM-DE_Speed></Item>
<Item><IPAddress>192.168.178.5</IPAddress><MACAddress>AA:00:00:00:00:03</MACAddress><Active>1</Active><HostName>repeater</HostName><InterfaceType>802.11</InterfaceType><X_AVM-DE_Speed>0</X_AVM-DE_Speed><X_AVM-DE_UpdateAvailable>1</X_AVM-DE_UpdateAvailable><X_AVM-DE_Model>FRITZ!Repeater 1200 AX</X_AVM-DE_Model><X_AVM-DE_Guest>0</X_AVM-DE_Guest></Item>
</List>`,
		// box (n-1) ─WLAN─ repeater (n-2) ─WLAN─ laptop (n-3)
		"/meshlist.lua": `{"nodes":[
{"uid":"n-1","device_name":"fritz.box","device_model":"FRITZ!Box 7590","device_manufacturer":"AVM","device_firmware_version":"154.08.03","device_mac_address":"AA:00:00:00:00:00","is_meshed":true,"mesh_role":"master",
 "node_interfaces":[{"node_links":[{"uid":"nl-1","type":"WLAN","state":"CONNECTED","node_1_uid":"n-1","node_2_uid":"n-2","cur_data_rate_rx":130000,"rx_rcpi":-79,"tx_rcpi":255}]}]},
{"uid":"n-2","device_name":"repeater","device_model":"FRITZ!Repeater 1200 AX","device_manufacturer":"AVM","device_firmware_version":"180.08.02","device_mac_address":"AA:00:00:00:00:03","is_meshed":true,"mesh_role":"slave",
 "node_interfaces":[{"node_links":[{"uid":"nl-1","type":"WLAN","state":"CONNECTED","node_1_uid":"n-1","node_2_uid":"n-2","cur_data_rate_rx":130000,"rx_rcpi":-79,"tx_rcpi":255},
  {"uid":"nl-2","type":"WLAN","state":"CONNECTED","node_1_uid":"n-2","node_2_uid":"n-3","cur_data_rate_rx":433000,"rx_rcpi":-55,"tx_rcpi":255}]}]},
{"uid":"n-3","device_name":"laptop","device_model":"","device_manufacturer":"","device_mac_address":"AA:00:00:00:00:01","is_meshed":false,"mesh_role":"unknown",
 "node_interfaces":[{"node_links":[{"uid":"nl-2","type":"WLAN","state":"CONNECTED","node_1_uid":"n-2","node_2_uid":"n-3","cur_data_rate_rx":433000,"rx_rcpi":-55,"tx_rcpi":255}]}]}]}`,
		"/calllist.lua": `<root>
<Call><Type>2</Type><Caller>040555</Caller><Name>Northlight</Name><Date>08.10.26 10:42</Date></Call>
<Call><Type>2</Type><Caller/><Name/><Date>08.10.26 03:13</Date></Call>
<Call><Type>1</Type><Caller>040555</Caller><Date>07.10.26 09:00</Date></Call>
<Call><Type>3</Type><Called>040555</Called><Date>07.10.26 09:30</Date></Call>
</root>`,
	}
	smart := []string{
		// A group's placeholder without functions, then a plug and a thermostat.
		`<NewFunctionBitMask>0</NewFunctionBitMask><NewDeviceName>group</NewDeviceName>`,
		`<NewFunctionBitMask>35712</NewFunctionBitMask><NewDeviceName>Kaffee</NewDeviceName><NewProductName>FRITZ!DECT 200</NewProductName><NewPresent>CONNECTED</NewPresent>` +
			`<NewSwitchIsEnabled>ENABLED</NewSwitchIsEnabled><NewSwitchIsValid>VALID</NewSwitchIsValid><NewSwitchState>ON</NewSwitchState>` +
			`<NewMultimeterIsEnabled>ENABLED</NewMultimeterIsEnabled><NewMultimeterIsValid>VALID</NewMultimeterIsValid><NewMultimeterPower>210</NewMultimeterPower><NewMultimeterEnergy>41700</NewMultimeterEnergy>` +
			`<NewTemperatureIsEnabled>ENABLED</NewTemperatureIsEnabled><NewTemperatureIsValid>VALID</NewTemperatureIsValid><NewTemperatureCelsius>225</NewTemperatureCelsius>`,
		`<NewFunctionBitMask>320</NewFunctionBitMask><NewDeviceName>Heizung</NewDeviceName><NewPresent>DISCONNECTED</NewPresent>` +
			`<NewHkrIsEnabled>ENABLED</NewHkrIsEnabled><NewHkrIsValid>VALID</NewHkrIsValid><NewHkrIsTemperature>195</NewHkrIsTemperature><NewHkrSetTemperature>210</NewHkrSetTemperature>`,
	}

	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if list, ok := lists[r.URL.Path]; ok {
			w.Write([]byte(list))
			return
		}
		control := strings.TrimPrefix(r.URL.Path, "/upnp/control/")
		_, action, _ := strings.Cut(strings.Trim(r.Header.Get("SOAPACTION"), `"`), "#")
		body, _ := io.ReadAll(r.Body)
		if control == "x_homeauto" {
			_, rest, _ := strings.Cut(string(body), "<NewIndex>")
			idx, _, _ := strings.Cut(rest, "<")
			for i, s := range smart {
				if idx == string(rune('0'+i)) {
					w.Write([]byte("<r>" + s + "</r>"))
					return
				}
			}
		}
		ans, ok := answers[control+" "+action]
		if !ok {
			w.WriteHeader(http.StatusInternalServerError)
			w.Write([]byte(`<UPnPError><errorCode>713</errorCode></UPnPError>`))
			return
		}
		w.Write([]byte("<r>" + ans + "</r>"))
	}))
}

// TestFritzMore: an IP connection (PPP refused), line margins, FRITZ!OS
// and its offered update, throughput oldest first, the radios with a
// band, devices joined with the mesh, the calls, Smart Home.
func TestFritzMore(t *testing.T) {
	srv := fakeFritz(t)
	defer srv.Close()
	raw, err := sources.FritzData.Fetch(context.Background(), sources.Ctx{URL: srv.URL, Secret: "andon:pw"})
	if err != nil {
		t.Fatal(err)
	}
	d := raw.(*sources.FritzDataset)

	if !d.Connected() || d.DownMargin != 5.5 || d.UpMargin != 9 || d.Firmware != "8.03" || d.Update != "8.21" || d.UpdateError {
		t.Fatalf("line %+v", d)
	}
	if len(d.TrafficDown) != 2 || d.TrafficDown[0] != 2 || d.TrafficDown[1] != 1000 || d.TrafficUp[0] != 0 || d.TrafficUp[1] != 4 {
		t.Fatalf("traffic %v %v", d.TrafficDown, d.TrafficUp)
	}
	if len(d.Radios) != 1 || !d.Radios[0].On || d.Radios[0].Band != "2.4" || d.Radios[0].Clients != 3 {
		t.Fatalf("radios %+v", d.Radios)
	}

	// Active first; the laptop's rate and signal come from its mesh link.
	if len(d.Hosts) != 3 || d.Hosts[0].Name != "Laptop" || d.Hosts[0].Speed != 433 || d.Hosts[0].Signal != -55 || d.Hosts[2].Active {
		t.Fatalf("hosts %+v", d.Hosts)
	}
	if d.Active() != 2 || len(d.MeshUpdates()) != 1 || d.MeshUpdates()[0] != "repeater" {
		t.Fatalf("active %d, updates %v", d.Active(), d.MeshUpdates())
	}
	if len(d.Mesh) != 2 || d.Mesh[0].Role != "master" || d.Mesh[0].Uplink != sources.FritzNoLink || d.Mesh[0].Clients != 0 {
		t.Fatalf("mesh box %+v", d.Mesh)
	}
	if r := d.Mesh[1]; r.Uplink != sources.FritzWLAN || r.Rate != 130 || r.Signal != -79 || r.Clients != 1 || r.Firmware != "8.02" {
		t.Fatalf("mesh repeater %+v", r)
	}

	c := d.Calls
	if c == nil || c.In != 1 || c.Out != 1 || c.Missed != 2 || len(c.Recent) != 2 || c.Recent[0].Who != "Northlight" || c.Recent[1].Who != "" || c.Recent[1].At.Hour() != 3 {
		t.Fatalf("calls %+v", c)
	}

	if len(d.Smart) != 2 {
		t.Fatalf("smart %+v", d.Smart)
	}
	if p := d.Smart[0]; p.Switch != "on" || !p.Meter || p.Power != 2.1 || p.Energy != 41.7 || !p.Thermo || p.Temp != 22.5 || !p.Present {
		t.Fatalf("plug %+v", p)
	}
	if h := d.Smart[1]; h.Present || !h.Heater || h.Set != 21 || h.Temp != 19.5 || h.Switch != "" {
		t.Fatalf("thermostat %+v", h)
	}
}

// The demo box carries every part, so the tile and dialog show them.
func TestDemoFritzMore(t *testing.T) {
	d := sources.DemoFritz(time.Now())
	if d.Update == "" || len(d.TrafficDown) == 0 || len(d.Hosts) == 0 || len(d.Mesh) == 0 || len(d.Radios) == 0 || d.Calls == nil || len(d.Calls.Recent) == 0 || len(d.Smart) == 0 {
		t.Fatalf("demo %+v", d)
	}
	if len(d.MeshUpdates()) == 0 || d.Mesh[1].Uplink != sources.FritzWLAN {
		t.Fatalf("demo mesh %+v", d.Mesh)
	}
}
