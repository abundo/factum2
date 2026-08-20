package drivers

// Tests for the Arista EOS driver, run against fakeEOS - an httptest TLS
// server that speaks eAPI's JSON-RPC dialect - rather than a real switch.
// No production code needed changing to make this possible: eapiRunCmds
// builds its URL as "https://" + host + eapiPath, so pointing DriverParam.Name
// at the test server's host:port is enough, and eapiClient already skips
// certificate verification (real switches serve eAPI with a self-signed cert),
// which is exactly what an httptest TLS server needs too.
//
// The two NETCONF-backed methods are covered on their fallback path only:
// see fakeEOS.driver for how the NETCONF dial is made to fail immediately.
// The NETCONF path itself needs a device, or the containerlab cEOS node in
// testdata/clab - it's covered by driver_arista_eos_integration_test.go.

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/abundo/netboxtool"
)

// ----------------------------------------------------------------------
// Fake device
// ----------------------------------------------------------------------

// fakeEOS is a stand-in for a device's eAPI endpoint. respond is called for
// every runCmds request and returns one raw JSON result per command (or an
// error object, which EOS reports with HTTP 200 rather than a status code).
// Results are raw rather than `any` so a test can control the exact key order
// of a JSON object - the interface-order behaviour in
// TestAristaGetInterfacesStatus depends on it and re-encoding a Go map would
// destroy it.
type fakeEOS struct {
	*httptest.Server

	respond func(req eapiRequest) ([]json.RawMessage, *eapiError)

	mu       sync.Mutex
	requests []eapiRequest
	auth     []string // "user:pass" as sent, one per request
}

func newFakeEOS(t *testing.T, respond func(req eapiRequest) ([]json.RawMessage, *eapiError)) *fakeEOS {
	t.Helper()
	fake := &fakeEOS{respond: respond}
	fake.Server = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != eapiPath {
			t.Errorf("unexpected request %s %s, want POST %s", r.Method, r.URL.Path, eapiPath)
			w.WriteHeader(http.StatusNotFound)
			return
		}
		var req eapiRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("decode request: %v", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		user, pass, _ := r.BasicAuth()

		fake.mu.Lock()
		fake.requests = append(fake.requests, req)
		fake.auth = append(fake.auth, user+":"+pass)
		fake.mu.Unlock()

		results, apiErr := fake.respond(req)
		w.Header().Set("Content-Type", "application/json")
		if apiErr != nil {
			json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": req.ID, "error": apiErr})
			return
		}
		json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": req.ID, "result": results})
	}))
	t.Cleanup(fake.Close)
	return fake
}

// driver builds an AristaDriver pointed at the fake device.
//
// Name is the server's host:port, which the eAPI transport uses verbatim. It
// also makes the NETCONF dial in netconfDial fail immediately (it builds
// host+":"+port, so a host that already carries a port yields an unparseable
// address) - which is what the fallback tests want: the fallback is reached
// deterministically, with no 10-second SSH dial timeout to wait out.
func (f *fakeEOS) driver(t *testing.T) *AristaDriver {
	t.Helper()
	driver, err := NewAristaDriver(DriverParam{
		Name:     strings.TrimPrefix(f.URL, "https://"),
		Username: "admin",
		Password: "secret",
		Platform: "eos",
	})
	if err != nil {
		t.Fatalf("NewAristaDriver: %v", err)
	}
	return driver
}

// only returns the single request the device received, failing the test if it
// got any other number.
func (f *fakeEOS) only(t *testing.T) eapiRequest {
	t.Helper()
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.requests) != 1 {
		t.Fatalf("got %d requests, want 1", len(f.requests))
	}
	return f.requests[0]
}

// raw is a shorthand for a canned JSON result.
func raw(s string) json.RawMessage { return json.RawMessage(s) }

// respondText answers any request with output as a single text-format result.
func respondText(output string) func(eapiRequest) ([]json.RawMessage, *eapiError) {
	return func(req eapiRequest) ([]json.RawMessage, *eapiError) {
		results := make([]json.RawMessage, len(req.Params.Cmds))
		for i := range results {
			results[i] = raw(`{"output": ""}`)
		}
		body, _ := json.Marshal(eapiTextResult{Output: output})
		results[len(results)-1] = body
		return results, nil
	}
}

// respondJSON answers any request with one canned result per command.
func respondJSON(results ...json.RawMessage) func(eapiRequest) ([]json.RawMessage, *eapiError) {
	return func(eapiRequest) ([]json.RawMessage, *eapiError) { return results, nil }
}

// ----------------------------------------------------------------------
// Construction
// ----------------------------------------------------------------------

func TestNewAristaDriver(t *testing.T) {
	tests := []struct {
		name    string
		param   DriverParam
		wantErr bool
	}{
		{"ok", DriverParam{Name: "sw1", Username: "admin", Password: "secret"}, false},
		{"missing username", DriverParam{Name: "sw1", Password: "secret"}, true},
		{"missing password", DriverParam{Name: "sw1", Username: "admin"}, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := NewAristaDriver(tc.param)
			if (err != nil) != tc.wantErr {
				t.Fatalf("NewAristaDriver: err = %v, wantErr = %v", err, tc.wantErr)
			}
		})
	}
}

// ----------------------------------------------------------------------
// CLI-shaped methods - eAPI only
// ----------------------------------------------------------------------

func TestAristaExec(t *testing.T) {
	fake := newFakeEOS(t, respondText("Ethernet1 is up\n"))

	got, err := fake.driver(t).Exec("show interfaces Ethernet1")
	if err != nil {
		t.Fatalf("Exec: %v", err)
	}
	if got.Result != "Ethernet1 is up\n" {
		t.Errorf("Result = %q, want %q", got.Result, "Ethernet1 is up\n")
	}

	req := fake.only(t)
	if req.Method != "runCmds" || req.Jsonrpc != "2.0" {
		t.Errorf("request = %s/%s, want 2.0/runCmds", req.Jsonrpc, req.Method)
	}
	if !reflect.DeepEqual(req.Params.Cmds, []string{"show interfaces Ethernet1"}) {
		t.Errorf("cmds = %v", req.Params.Cmds)
	}
	// Arbitrary commands go out as text: plenty of EOS commands have no JSON
	// rendering and are answered with error 1003 if asked for one.
	if req.Params.Format != eapiFormatText {
		t.Errorf("format = %q, want %q", req.Params.Format, eapiFormatText)
	}
	if fake.auth[0] != "admin:secret" {
		t.Errorf("basic auth = %q, want admin:secret", fake.auth[0])
	}
}

func TestAristaVersion(t *testing.T) {
	fake := newFakeEOS(t, respondJSON(raw(`{
		"modelName": "DCS-7280SR-48C6-F",
		"internalVersion": "4.28.3M-27237887.4283M",
		"systemMacAddress": "44:4c:a8:11:22:33",
		"serialNumber": "JPE12345678",
		"memTotal": 8091252,
		"memFree": 4373596,
		"bootupTimestamp": 1690000000.5,
		"version": "4.28.3M",
		"architecture": "x86_64",
		"internalBuildId": "0b3e1e0a-1234",
		"hardwareRevision": "11.03"
	}`)))

	got, err := fake.driver(t).Version()
	if err != nil {
		t.Fatalf("Version: %v", err)
	}
	want := &VersionModel{
		ModelName:        "DCS-7280SR-48C6-F",
		InternalVersion:  "4.28.3M-27237887.4283M",
		SystemMacAddress: "44:4c:a8:11:22:33",
		SerialNumber:     "JPE12345678",
		MemTotal:         8091252,
		MemFree:          4373596,
		BootupTimestap:   1690000000.5,
		Version:          "4.28.3M",
		Architecture:     "x86_64",
		InternalBuildId:  "0b3e1e0a-1234",
		HardwareRevision: "11.03",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Version() = %+v\nwant %+v", got, want)
	}

	req := fake.only(t)
	if !reflect.DeepEqual(req.Params.Cmds, []string{"show version"}) {
		t.Errorf("cmds = %v", req.Params.Cmds)
	}
	if req.Params.Format != eapiFormatJSON {
		t.Errorf("format = %q, want %q", req.Params.Format, eapiFormatJSON)
	}
}

func TestAristaRunningConfigGetText(t *testing.T) {
	config := "! device: sw1\n!\nhostname sw1\n"
	fake := newFakeEOS(t, respondText(config))

	got, err := fake.driver(t).RunningConfigGet(false)
	if err != nil {
		t.Fatalf("RunningConfigGet: %v", err)
	}
	if got.ConfigStr != config {
		t.Errorf("ConfigStr = %q, want %q", got.ConfigStr, config)
	}
	req := fake.only(t)
	if !reflect.DeepEqual(req.Params.Cmds, []string{"show running-config"}) {
		t.Errorf("cmds = %v", req.Params.Cmds)
	}
	if req.Params.Format != eapiFormatText {
		t.Errorf("format = %q, want %q", req.Params.Format, eapiFormatText)
	}
}

func TestAristaRunningConfigGetJSON(t *testing.T) {
	// EOS's structured rendering is passed through verbatim, not re-decoded.
	structured := `{"cmds":{"hostname sw1":{"cmds":{}}},"comments":[]}`
	fake := newFakeEOS(t, respondJSON(raw(structured)))

	got, err := fake.driver(t).RunningConfigGet(true)
	if err != nil {
		t.Fatalf("RunningConfigGet: %v", err)
	}
	if got.ConfigStr != structured {
		t.Errorf("ConfigStr = %q, want %q", got.ConfigStr, structured)
	}
	if format := fake.only(t).Params.Format; format != eapiFormatJSON {
		t.Errorf("format = %q, want %q", format, eapiFormatJSON)
	}
}

func TestAristaRunningConfigSave(t *testing.T) {
	fake := newFakeEOS(t, respondText(""))

	if err := fake.driver(t).RunningConfigSave(); err != nil {
		t.Fatalf("RunningConfigSave: %v", err)
	}
	if cmds := fake.only(t).Params.Cmds; !reflect.DeepEqual(cmds, []string{"copy running-config startup-config"}) {
		t.Errorf("cmds = %v", cmds)
	}
}

// A command the device rejects comes back as a JSON-RPC error object with
// HTTP 200; the driver has to surface it as an error rather than as a
// successful call with no results, and keep the CLI's own message ("Invalid
// input ...") which says much more than the generic JSON-RPC one.
func TestAristaExecDeviceError(t *testing.T) {
	fake := newFakeEOS(t, func(eapiRequest) ([]json.RawMessage, *eapiError) {
		return nil, &eapiError{
			Code:    1002,
			Message: "CLI command 1 of 1 'show bogus' failed: invalid command",
			Data: []struct {
				Errors []string `json:"errors"`
			}{{Errors: []string{"Invalid input (at token 0: 'bogus')"}}},
		}
	})

	_, err := fake.driver(t).Exec("show bogus")
	if err == nil {
		t.Fatal("Exec: want error, got nil")
	}
	var apiErr *eapiError
	if !errors.As(err, &apiErr) {
		t.Fatalf("err = %T (%v), want *eapiError", err, err)
	}
	if apiErr.Code != 1002 {
		t.Errorf("code = %d, want 1002", apiErr.Code)
	}
	if !strings.Contains(err.Error(), "Invalid input (at token 0: 'bogus')") {
		t.Errorf("error = %q, want it to carry the CLI's own message", err)
	}
}

// ----------------------------------------------------------------------
// NETCONF-backed methods, on their eAPI fallback path
// ----------------------------------------------------------------------

func TestAristaGetInterfacesStatus(t *testing.T) {
	// Written as a raw object so the key order is the device's own interface
	// order - Ethernet2 before Ethernet2.210 before Ethernet10, which is not
	// what sorting the names would produce. Preserving it is the whole reason
	// getInterfacesStatusEAPI reads the keys with jsonObjectKeys instead of
	// ranging over the decoded map.
	fake := newFakeEOS(t, respondJSON(raw(`{"interfaceDescriptions": {
		"Ethernet2":     {"description": "uplink",   "interfaceStatus": "up",        "lineProtocolStatus": "up"},
		"Ethernet2.210": {"description": "cust-a",   "interfaceStatus": "up",        "lineProtocolStatus": "up"},
		"Ethernet10":    {"description": "",         "interfaceStatus": "adminDown", "lineProtocolStatus": "lowerLayerDown"},
		"Management1":   {"description": "oob",      "interfaceStatus": "disabled",  "lineProtocolStatus": "notPresent"}
	}}`)))

	got, err := fake.driver(t).GetInterfacesStatus()
	if err != nil {
		t.Fatalf("GetInterfacesStatus: %v", err)
	}

	// Statuses are translated into the openconfig-interfaces vocabulary the
	// NETCONF path returns, so a caller can't tell the transports apart.
	want := []*netboxtool.NBInterface{
		{Name: "Ethernet2", Description: "uplink", InterfaceStatus: "UP", LineProtocolStatus: "UP"},
		{Name: "Ethernet2.210", Description: "cust-a", InterfaceStatus: "UP", LineProtocolStatus: "UP"},
		{Name: "Ethernet10", Description: "", InterfaceStatus: "DOWN", LineProtocolStatus: "LOWER_LAYER_DOWN"},
		{Name: "Management1", Description: "oob", InterfaceStatus: "DOWN", LineProtocolStatus: "NOT_PRESENT"},
	}
	if len(got) != len(want) {
		t.Fatalf("got %d interfaces, want %d", len(got), len(want))
	}
	for i := range want {
		if !reflect.DeepEqual(got[i], want[i]) {
			t.Errorf("interface %d = %+v\nwant %+v", i, got[i], want[i])
		}
	}

	if cmds := fake.only(t).Params.Cmds; !reflect.DeepEqual(cmds, []string{"show interfaces description"}) {
		t.Errorf("cmds = %v", cmds)
	}
}

func TestAristaSetInterfaceDescriptions(t *testing.T) {
	fake := newFakeEOS(t, respondText(""))

	err := fake.driver(t).SetInterfaceDescriptions(
		[]string{"Ethernet1", "Ethernet2", "Ethernet2.210"},
		[]*netboxtool.NBInterface{
			{Description: "uplink"},
			{Description: ""}, // empty means remove, not set-to-empty
			{Description: "cust-a"},
		})
	if err != nil {
		t.Fatalf("SetInterfaceDescriptions: %v", err)
	}

	req := fake.only(t)
	want := []string{
		"configure",
		"interface Ethernet1", "description uplink",
		"interface Ethernet2", "no description",
		"interface Ethernet2.210", "description cust-a",
		"end",
	}
	if !reflect.DeepEqual(req.Params.Cmds, want) {
		t.Errorf("cmds = %v\nwant %v", req.Params.Cmds, want)
	}
	if req.Params.Format != eapiFormatText {
		t.Errorf("format = %q, want %q", req.Params.Format, eapiFormatText)
	}
}

func TestAristaSetInterfaceDescription(t *testing.T) {
	fake := newFakeEOS(t, respondText(""))

	err := fake.driver(t).SetInterfaceDescription(&netboxtool.NBInterface{Name: "Ethernet1", Description: "uplink"})
	if err != nil {
		t.Fatalf("SetInterfaceDescription: %v", err)
	}
	want := []string{"configure", "interface Ethernet1", "description uplink", "end"}
	if cmds := fake.only(t).Params.Cmds; !reflect.DeepEqual(cmds, want) {
		t.Errorf("cmds = %v\nwant %v", cmds, want)
	}
}

// Mismatched arguments must be rejected before anything is dialed - the
// driver here points at a name nothing is listening on, so a request would
// fail the test by erroring for the wrong reason.
func TestAristaSetInterfaceDescriptionsMismatch(t *testing.T) {
	driver, err := NewAristaDriver(DriverParam{Name: "invalid.example", Username: "admin", Password: "secret"})
	if err != nil {
		t.Fatalf("NewAristaDriver: %v", err)
	}
	err = driver.SetInterfaceDescriptions(
		[]string{"Ethernet1", "Ethernet2"},
		[]*netboxtool.NBInterface{{Description: "uplink"}})
	if err == nil {
		t.Fatal("want error for mismatched name/interface counts, got nil")
	}
}

func TestAristaSetInterfaceVLANs(t *testing.T) {
	fake := newFakeEOS(t, respondText(""))

	err := fake.driver(t).SetInterfaceVLANs(
		[]string{"Ethernet1", "Ethernet2", "Ethernet3"},
		[]*VLANConfig{
			{SwitchportMode: "access", UntaggedVLAN: 10},
			{SwitchportMode: "trunk", UntaggedVLAN: 10, TaggedVLANs: []int{20, 30}},
			{SwitchportMode: "dot1q-tunnel", UntaggedVLAN: 40},
		})
	if err != nil {
		t.Fatalf("SetInterfaceVLANs: %v", err)
	}

	req := fake.only(t)
	want := []string{
		"configure",
		"vlan 10", "vlan 20", "vlan 30", "vlan 40",
		"interface Ethernet1", "switchport mode access", "switchport access vlan 10",
		"interface Ethernet2", "switchport mode trunk",
		"switchport trunk native vlan 10", "switchport trunk allowed vlan 20,30",
		"interface Ethernet3", "switchport mode dot1q-tunnel", "switchport access vlan 40",
		"end",
	}
	if !reflect.DeepEqual(req.Params.Cmds, want) {
		t.Errorf("cmds = %v\nwant %v", req.Params.Cmds, want)
	}
	if req.Params.Format != eapiFormatText {
		t.Errorf("format = %q, want %q", req.Params.Format, eapiFormatText)
	}
}

// Removing switchport config (empty SwitchportMode) must clear both the
// mode and any previously configured VLANs, not just leave them as-is.
func TestAristaSetInterfaceVLANsRemove(t *testing.T) {
	fake := newFakeEOS(t, respondText(""))

	err := fake.driver(t).SetInterfaceVLANs(
		[]string{"Ethernet1"},
		[]*VLANConfig{{}})
	if err != nil {
		t.Fatalf("SetInterfaceVLANs: %v", err)
	}

	want := []string{"configure", "interface Ethernet1", "no switchport", "end"}
	if cmds := fake.only(t).Params.Cmds; !reflect.DeepEqual(cmds, want) {
		t.Errorf("cmds = %v\nwant %v", cmds, want)
	}
}

func TestAristaSetInterfaceVLANsMismatch(t *testing.T) {
	driver, err := NewAristaDriver(DriverParam{Name: "invalid.example", Username: "admin", Password: "secret"})
	if err != nil {
		t.Fatalf("NewAristaDriver: %v", err)
	}
	err = driver.SetInterfaceVLANs(
		[]string{"Ethernet1", "Ethernet2"},
		[]*VLANConfig{{SwitchportMode: "access", UntaggedVLAN: 10}})
	if err == nil {
		t.Fatal("want error for mismatched name/config counts, got nil")
	}
}

// When eAPI fails too, the NETCONF error that made this a fallback in the
// first place must not be dropped - eapiFallback joins the two.
func TestAristaFallbackKeepsBothErrors(t *testing.T) {
	fake := newFakeEOS(t, func(eapiRequest) ([]json.RawMessage, *eapiError) {
		return nil, &eapiError{Code: 1000, Message: "eapi is unhappy"}
	})

	_, err := fake.driver(t).GetInterfacesStatus()
	if err == nil {
		t.Fatal("want error, got nil")
	}
	if !strings.Contains(err.Error(), "eapi is unhappy") {
		t.Errorf("error %q, want it to carry the eAPI failure", err)
	}
	// The NETCONF dial failed on the malformed address (see fakeEOS.driver),
	// so its error mentions the address it couldn't use.
	if !strings.Contains(err.Error(), "dial") && !strings.Contains(err.Error(), "address") {
		t.Errorf("error %q, want it to carry the NETCONF failure too", err)
	}
}

// ----------------------------------------------------------------------
// Status vocabulary mapping
// ----------------------------------------------------------------------

func TestEapiOperStatus(t *testing.T) {
	tests := map[string]string{
		"up":             "UP",
		"down":           "DOWN",
		"lowerLayerDown": "LOWER_LAYER_DOWN",
		"notPresent":     "NOT_PRESENT",
		"testing":        "TESTING",
	}
	for in, want := range tests {
		if got := eapiOperStatus(in); got != want {
			t.Errorf("eapiOperStatus(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestEapiAdminStatus(t *testing.T) {
	// eAPI folds admin state into the same field as operational state: only
	// adminDown/disabled mean shut down, everything else is admin-enabled.
	tests := map[string]string{
		"up":          "UP",
		"down":        "UP",
		"errdisabled": "UP",
		"adminDown":   "DOWN",
		"disabled":    "DOWN",
	}
	for in, want := range tests {
		if got := eapiAdminStatus(in); got != want {
			t.Errorf("eapiAdminStatus(%q) = %q, want %q", in, got, want)
		}
	}
}
