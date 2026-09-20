package dns

import (
	"context"
	"encoding/json"
	"net"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestListKeaLeasesV4AndV6(t *testing.T) {
	v4 := startFakeKea(t, func(cmd string) any {
		if cmd != "lease4-get-all" {
			t.Errorf("v4 command %q", cmd)
		}
		return map[string]any{
			"result": 0,
			"text":   "2 IPv4 lease(s) found.",
			"arguments": map[string]any{
				"leases": []map[string]any{
					{
						"hw-address": "02:00:00:00:00:02",
						"ip-address": "192.0.2.101",
						"hostname":   "two.lab.",
						"state":      0,
					},
					{
						"hw-address": "02-00-00-00-00-01",
						"ip-address": "192.0.2.100",
						"hostname":   "one.lab.",
						"state":      0,
					},
					{
						"hw-address": "",
						"ip-address": "192.0.2.200",
						"hostname":   "nomac",
						"state":      0,
					},
				},
			},
		}
	})
	v6 := startFakeKea(t, func(cmd string) any {
		if cmd != "lease6-get-all" {
			t.Errorf("v6 command %q", cmd)
		}
		return map[string]any{
			"result": 0,
			"arguments": map[string]any{
				"leases": []map[string]any{
					{
						"hw-address": "02:00:00:00:00:01",
						"ip-address": "2001:db8::10",
						"hostname":   "one.lab.",
						"state":      0,
						"type":       "IA_NA",
					},
					{
						"hw-address": "02:00:00:00:00:99",
						"ip-address": "2001:db8:1::/64",
						"hostname":   "pd",
						"state":      0,
						"type":       "IA_PD",
					},
				},
			},
		}
	})
	got, err := ListKeaLeases(context.Background(), []KeaSocket{
		{Family: "ipv4", Path: v4, Command: "lease4-get-all"},
		{Family: "ipv6", Path: v6, Command: "lease6-get-all"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 {
		t.Fatalf("len=%d got=%+v", len(got), got)
	}
	if got[0].Family != "ipv4" || got[0].IP != "192.0.2.100" || got[0].MAC != "02:00:00:00:00:01" {
		t.Fatalf("first %+v", got[0])
	}
	if got[0].Hostname != "one.lab." || got[0].State != "active" {
		t.Fatalf("first hostname/state %+v", got[0])
	}
	if got[1].IP != "192.0.2.101" {
		t.Fatalf("second %+v", got[1])
	}
	if got[2].Family != "ipv6" || got[2].IP != "2001:db8::10" || got[2].MAC != "02:00:00:00:00:01" {
		t.Fatalf("v6 %+v", got[2])
	}
}

func TestListKeaLeasesEmpty(t *testing.T) {
	sock := startFakeKea(t, func(string) any {
		return map[string]any{"result": 3, "text": "No lease found."}
	})
	got, err := ListKeaLeases(context.Background(), []KeaSocket{
		{Family: "ipv4", Path: sock, Command: "lease4-get-all"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("got %+v", got)
	}
}

func TestListKeaLeasesMissingSockets(t *testing.T) {
	_, err := ListKeaLeases(context.Background(), []KeaSocket{
		{Family: "ipv4", Path: filepath.Join(t.TempDir(), "missing"), Command: "lease4-get-all"},
	})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestListKeaLeasesSkipsMissingFamily(t *testing.T) {
	v4 := startFakeKea(t, func(string) any {
		return map[string]any{
			"result": 0,
			"arguments": map[string]any{
				"leases": []map[string]any{
					{"hw-address": "aa:bb:cc:dd:ee:ff", "ip-address": "192.0.2.1", "state": 0},
				},
			},
		}
	})
	got, err := ListKeaLeases(context.Background(), []KeaSocket{
		{Family: "ipv4", Path: v4, Command: "lease4-get-all"},
		{Family: "ipv6", Path: filepath.Join(t.TempDir(), "no-kea6"), Command: "lease6-get-all"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].MAC != "aa:bb:cc:dd:ee:ff" {
		t.Fatalf("got %+v", got)
	}
}

func TestParseKeaLeaseCSV(t *testing.T) {
	const raw = `address,hwaddr,client_id,valid_lifetime,expire,subnet_id,fqdn_fwd,fqdn_rev,hostname,state,user_context
192.0.2.100,02:00:00:00:00:01,,4000,9999999999,1,0,0,one.lab.,0,
192.0.2.101,02:00:00:00:00:02,,4000,9999999999,1,0,0,two.lab.,0,
192.0.2.100,02:00:00:00:00:01,,4000,9999999999,1,0,0,one.lab.,0,
192.0.2.9,02:00:00:00:00:09,,4000,1,1,0,0,expired,0,
192.0.2.8,,,4000,9999999999,1,0,0,nomac,0,
`
	got, err := parseKeaLeaseCSV("ipv4", strings.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("len=%d got=%+v", len(got), got)
	}
	byIP := map[string]DHCPLease{}
	for _, l := range got {
		byIP[l.IP] = l
	}
	if byIP["192.0.2.100"].MAC != "02:00:00:00:00:01" || byIP["192.0.2.100"].Hostname != "one.lab." {
		t.Fatalf("100 %+v", byIP["192.0.2.100"])
	}
	if byIP["192.0.2.101"].MAC != "02:00:00:00:00:02" {
		t.Fatalf("101 %+v", byIP["192.0.2.101"])
	}
}

func startFakeKea(t *testing.T, handler func(cmd string) any) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "kea.sock")
	ln, err := net.Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				_ = c.SetDeadline(time.Now().Add(2 * time.Second))
				var req keaCommand
				if err := json.NewDecoder(c).Decode(&req); err != nil {
					return
				}
				_ = json.NewEncoder(c).Encode(handler(req.Command))
			}(conn)
		}
	}()
	return path
}
