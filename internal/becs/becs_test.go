package becs

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func loadFixture(t *testing.T) []Object {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", "tree.json"))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	var wrap struct {
		Result struct {
			Objects []Object `json:"objects"`
		} `json:"result"`
	}
	if err := json.Unmarshal(data, &wrap); err != nil {
		t.Fatalf("decode fixture: %v", err)
	}
	return wrap.Result.Objects
}

func TestIndexAndDevices(t *testing.T) {
	c := NewClient("http://unused", "u", "p")
	c.Index(loadFixture(t))

	devs, err := c.Devices("example.com")
	if err != nil {
		t.Fatalf("Devices: %v", err)
	}
	if len(devs) != 1 {
		t.Fatalf("got %d devices, want 1", len(devs))
	}
	d := devs[0]
	if d.OID != 3641645 {
		t.Errorf("oid=%d, want 3641645", d.OID)
	}
	if d.ShortName != "test-lab2" {
		t.Errorf("short=%q, want test-lab2", d.ShortName)
	}
	if d.Name != "test-lab2.example.com" {
		t.Errorf("name=%q, want test-lab2.example.com", d.Name)
	}
	if d.Model != "ASR7348" {
		t.Errorf("model=%q, want ASR7348", d.Model)
	}
	if d.ConnectionMethod != "ssh" {
		t.Errorf("conn=%q, want ssh", d.ConnectionMethod)
	}
	if !d.Enabled {
		t.Error("expected enabled")
	}
	if got := joinParents(d.Parents); got != "core1.example.com" {
		t.Errorf("parents=%q, want core1.example.com", got)
	}
	if d.AlarmDestination != "noc" {
		t.Errorf("alarm_destination=%q, want noc", d.AlarmDestination)
	}
	if d.AlarmTimeperiod != "24x7" {
		t.Errorf("alarm_timeperiod=%q, want 24x7", d.AlarmTimeperiod)
	}

	loop := d.Interfaces["loopback0"]
	if loop == nil {
		t.Fatal("missing loopback0")
	}
	if len(loop.Prefix4) != 1 || loop.Prefix4[0].Address != "10.15.15.153/32" {
		t.Errorf("loopback0 prefix=%v, want 10.15.15.153/32", loop.Prefix4)
	}

	vlan := d.Interfaces["vlan1"]
	if vlan == nil {
		t.Fatal("missing vlan1")
	}
	if len(vlan.Prefix4) != 1 || vlan.Prefix4[0].Address != "10.0.0.9/24" {
		t.Errorf("vlan1 prefix=%v, want 10.0.0.9/24 (useparentmask)", vlan.Prefix4)
	}

	eth0 := d.Interfaces["ethernet0"]
	if eth0 == nil {
		t.Fatal("missing ethernet0")
	}
	if eth0.Enabled {
		t.Error("ethernet0 should be disabled")
	}
}

func TestSearchParentOpaque(t *testing.T) {
	c := NewClient("http://unused", "u", "p")
	c.Index(loadFixture(t))

	got, err := c.SearchParent(3641645)
	if err != nil {
		t.Fatal(err)
	}
	if got != "core1" {
		t.Errorf("SearchParent=%q, want core1", got)
	}
	dest, err := c.SearchOpaque(3641645, "alarm_destination")
	if err != nil {
		t.Fatal(err)
	}
	if dest != "noc" {
		t.Errorf("SearchOpaque=%q, want noc", dest)
	}
}

func TestGetElement(t *testing.T) {
	c := NewClient("http://unused", "u", "p")
	c.Index(loadFixture(t))

	d, err := c.GetElement("TEST-LAB2", 1)
	if err != nil {
		t.Fatal(err)
	}
	if d.ShortName != "test-lab2" {
		t.Errorf("got %q", d.ShortName)
	}
	if _, err := c.GetElement("missing", 1); err == nil {
		t.Error("expected error for missing element")
	}
}

func TestLoginAndLoadTree(t *testing.T) {
	fixture, err := os.ReadFile(filepath.Join("testdata", "tree.json"))
	if err != nil {
		t.Fatal(err)
	}
	var calls []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var req struct {
			Method string `json:"method"`
		}
		if err := json.Unmarshal(body, &req); err != nil {
			t.Errorf("decode request: %v", err)
		}
		calls = append(calls, req.Method)
		w.Header().Set("Content-Type", "application/json")
		switch req.Method {
		case "sessionLogin":
			_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":{"sessionid":"sess-1"}}`))
		case "objectTreeFind":
			_, _ = w.Write(fixture)
		default:
			t.Errorf("unexpected method %s", req.Method)
			_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"error":{"code":1,"message":"unexpected"}}`))
		}
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "user", "pass")
	if err := c.LoadTree(1); err != nil {
		t.Fatalf("LoadTree: %v", err)
	}
	if c.SessionID != "sess-1" {
		t.Errorf("sessionid=%q", c.SessionID)
	}
	if len(calls) != 2 || calls[0] != "sessionLogin" || calls[1] != "objectTreeFind" {
		t.Errorf("calls=%v, want [sessionLogin objectTreeFind]", calls)
	}
	if len(c.objects) == 0 {
		t.Error("expected indexed objects")
	}
}

func TestCallJSONRPCError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"error":{"code":-32000,"message":"denied"}}`))
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "u", "p")
	c.SessionID = "already"
	_, err := c.Call("objectFind", map[string]any{})
	if err == nil || err.Error() == "" {
		t.Fatalf("expected error, got %v", err)
	}
}

func TestLoadTreeNoopWhenIndexed(t *testing.T) {
	c := NewClient("http://unused", "u", "p")
	c.Index(loadFixture(t))
	if err := c.LoadTree(1); err != nil {
		t.Fatal(err)
	}
	if _, ok := c.objects[3641645]; !ok {
		t.Fatal("indexed objects should survive LoadTree")
	}
}

func TestASR5ConnectionMethod(t *testing.T) {
	c := NewClient("http://unused", "u", "p")
	c.Index([]Object{{
		OID:         1,
		Class:       elementClass,
		ElementType: elementType,
		Name:        "core-asr",
		Parameters:  []Parameter{{Name: "model", Values: []ParameterValue{{Value: "ASR5024"}}}},
	}})
	devs, err := c.Devices("")
	if err != nil {
		t.Fatal(err)
	}
	if len(devs) != 1 || devs[0].ConnectionMethod != "telnet" {
		t.Fatalf("got %+v, want telnet", devs)
	}
}
