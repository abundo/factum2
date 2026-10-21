package icinga

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/abundo/factum2/internal/util"
	"github.com/abundo/factum2/models"
)

func TestEvalEOSPatches(t *testing.T) {
	raw := []byte(`{"patches":{
		"CI00001":{"status":"down","connectors":{
			"1":{"status":"waitingForVlanId"},
			"2":{"status":"adminDown"}
		}},
		"CN00387-ka1-r3":{"status":"down","connectors":{
			"1":{"status":"adminDown"},
			"2":{"status":"noRemoteEnd","ldpPseudowireConnectorInfo":{"name":"CN00387","pseudowireId":100038700}}
		}},
		"shut":{"status":"down","connectors":{
			"1":{"status":"adminDown"},
			"2":{"status":"adminDown","ldpPseudowireConnectorInfo":{"name":"SHUT","pseudowireId":9}}
		}},
		"211-411":{"status":"up","connectors":{
			"1":{"status":"up"},
			"2":{"status":"up","ldpPseudowireConnectorInfo":{"name":"211-411","pseudowireId":211}}
		}}
	}}`)
	patches, err := parseEOSPatches(raw)
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		id, want string
		pw       int
		status   int
	}{
		{"CI00001", "waiting for vlan", 0, statusCrit},
		{"CN00387", "no remote end", 100038700, statusCrit},
		{"SHUT", "admin down", 9, statusOK},
		{"211-411", " up", 211, statusOK},
		{"missing", "not in patch panel", 1, statusUnknown},
	}
	for _, tc := range cases {
		got := readingsFromEOS([]elineTarget{{
			ServiceID: tc.id, PWID: tc.pw, Service: "eline " + tc.id,
		}}, patches)["eline "+tc.id]
		if got.Status != tc.status || !strings.Contains(got.Output, tc.want) {
			t.Errorf("%s: %#v", tc.id, got)
		}
	}
}

func TestPollEOSUsesEAPI(t *testing.T) {
	opens := 0
	open := func(ctx context.Context, addr, community string) (snmpQuerier, error) {
		opens++
		return &memSNMP{cols: map[string]map[int]snmpValue{
			oidIfName:        {1: textCol(1, "Ethernet1")},
			oidIfAdminStatus: {1: numCol(1)},
			oidIfOperStatus:  {1: numCol(1)},
			oidIfHCInOctets:  {1: numCol(1)},
			oidIfHCOutOctets: {1: numCol(1)},
		}}, nil
	}
	var asked string
	fic := &FactumIcingaClient{IcingaConfig: &util.ConfigIcinga{
		CommonConfig:      util.CommonConfig{DefaultDomain: "lab.example"},
		MonitorInterfaces: true,
		SNMPCommunities:   "public",
		EAPIUser:          "monitor",
		EAPIPassword:      "secret",
	}}
	sub := &memSubmitter{}
	_, err := fic.pollOnce(context.Background(), PollOptions{
		Open: open, Submit: sub,
		EOSShow: func(ctx context.Context, host string) (map[string]eosPatch, error) {
			asked = host
			return map[string]eosPatch{"CN00570": {status: "up"}}, nil
		},
		Elines: []models.IcingaEline{{
			Device: "r0", Address: "10.1.0.1/24", Platform: "eos", Monitor: true,
			ServiceID: "CN00570", PWID: 1000570,
		}},
	}, []*models.Device{{
		Name: "r0", Enabled: true, PrimaryIPv4: "10.1.0.1/24",
		CfMonitorIcinga: true, CfAlarmInterfaces: true,
		Interfaces: []models.Interface{{Name: "Ethernet1"}},
	}}, &counterStore{cur: map[string]ifaceCounters{}}, sub, time.Unix(1_700_000_000, 0))
	if err != nil {
		t.Fatal(err)
	}
	if opens != 1 || asked != "10.1.0.1" {
		t.Fatalf("opens %d host %q", opens, asked)
	}
	var eline string
	for _, r := range sub.results {
		if strings.HasPrefix(r.Service, "eline ") {
			eline = r.Output
		}
	}
	if !strings.Contains(eline, " up") {
		t.Fatalf("eline %q results %#v", eline, sub.results)
	}
}
