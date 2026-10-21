package icinga

import (
	"context"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/abundo/factum2/internal/jobevent"
	"github.com/abundo/factum2/internal/util"
	"github.com/abundo/factum2/models"
)

func TestElineVendor(t *testing.T) {
	cases := map[string]string{
		"eos":          elineVendorEOS,
		"Arista EOS":   elineVendorEOS,
		"ios-xr":       elineVendorIOSXR,
		"Cisco IOS-XR": elineVendorIOSXR,
		"iosxr":        elineVendorIOSXR,
		"sros":         elineVendorSROS,
		"sros-md":      elineVendorSROS,
		"Nokia SR OS":  elineVendorSROS,
		"7750 SR":      elineVendorSROS,
		"ios":          "",
		"vrp":          "",
	}
	for in, want := range cases {
		if got := elineVendor(in); got != want {
			t.Errorf("elineVendor(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestEvalPWEnums(t *testing.T) {
	up := elineTarget{ServiceID: "CN00570", PWID: 1000570, Service: "eline CN00570"}
	std := evalPW(up, pwRow{admin: 1, hasAdmin: true, oper: 6, hasOper: true}, false)
	if std.Status != statusCrit || !strings.Contains(std.Output, "lower layer down") {
		t.Fatalf("std lowerLayerDown: %#v", std)
	}
	cisco := evalPW(up, pwRow{admin: 1, hasAdmin: true, oper: 4, hasOper: true}, true)
	if cisco.Status != statusUnknown || !strings.Contains(cisco.Output, "unknown") {
		t.Fatalf("cisco unknown(4): %#v", cisco)
	}
	// Cisco notPresent is 6. The standard uses 6 for lowerLayerDown.
	missing := evalPW(up, pwRow{admin: 1, hasAdmin: true, oper: 6, hasOper: true}, true)
	if missing.Status != statusUnknown || !strings.Contains(missing.Output, "not present") {
		t.Fatalf("cisco notPresent: %#v", missing)
	}
	shut := evalPW(up, pwRow{admin: 2, hasAdmin: true, oper: 2, hasOper: true}, true)
	if shut.Status != statusOK || !strings.Contains(shut.Output, "admin down") {
		t.Fatalf("admin down: %#v", shut)
	}
}

func TestCollectPlatformElines(t *testing.T) {
	want := []elineTarget{{ServiceID: "CN00570", PWID: 1000570, Service: "eline CN00570"}}
	xr := &memSNMP{
		scalars: map[string]snmpValue{oidCpwIndexNext + ".0": numCol(1)},
		cols: map[string]map[int]snmpValue{
			oidCpwID:    {3: numCol(1000570)},
			oidCpwName:  {3: textCol(3, "CN00570")},
			oidCpwAdmin: {3: numCol(1)},
			oidCpwOper:  {3: numCol(2)},
		},
	}
	got, err := collectElines(xr, "ios-xr", want)
	if err != nil {
		t.Fatal(err)
	}
	if got["eline CN00570"].Status != statusCrit || !strings.Contains(got["eline CN00570"].Output, "down") {
		t.Fatalf("xr: %#v", got)
	}
	if len(xr.walks) == 0 || xr.walks[0] != oidCpwID {
		t.Fatalf("xr walks %v", xr.walks)
	}

	eos := &memSNMP{}
	got, err = collectElines(eos, "eos", want)
	if err != nil {
		t.Fatal(err)
	}
	if got["eline CN00570"].Status != statusUnknown || !strings.Contains(got["eline CN00570"].Output, "eAPI") {
		t.Fatalf("eos: %#v", got)
	}
	for _, w := range eos.walks {
		if strings.Contains(w, "1.3.6.1.2.1.10.246.1.2") {
			t.Fatalf("eos walked the pw table: %v", eos.walks)
		}
	}

	sros := &memSNMP{
		scalars: map[string]snmpValue{oidSvcNumEntries + ".0": numCol(1)},
		cols: map[string]map[int]snmpValue{
			oidSvcType:  {1000570: numCol(nokiaEpipe)},
			oidSvcAdmin: {1000570: numCol(1)},
			oidSvcOper:  {1000570: numCol(1)},
		},
	}
	got, err = collectElines(sros, "sros-md", want)
	if err != nil {
		t.Fatal(err)
	}
	if got["eline CN00570"].Status != statusOK || !strings.HasSuffix(got["eline CN00570"].Output, " up") {
		t.Fatalf("sros: %#v", got)
	}

	vpls := &memSNMP{
		scalars: map[string]snmpValue{oidSvcNumEntries + ".0": numCol(1)},
		cols: map[string]map[int]snmpValue{
			oidSvcType:  {1000570: numCol(3)},
			oidSvcAdmin: {1000570: numCol(1)},
			oidSvcOper:  {1000570: numCol(1)},
		},
	}
	got, err = collectElines(vpls, "Nokia SR OS", want)
	if err != nil {
		t.Fatal(err)
	}
	if got["eline CN00570"].Status != statusUnknown || !strings.Contains(got["eline CN00570"].Output, "not epipe") {
		t.Fatalf("vpls: %#v", got)
	}

	// A value on the object OID, not the .0 instance, is not an answer.
	bare := &memSNMP{scalars: map[string]snmpValue{oidSvcNumEntries: numCol(1)}}
	got, err = collectElines(bare, "sros", want)
	if err != nil {
		t.Fatal(err)
	}
	if got["eline CN00570"].Status != statusUnknown || !strings.Contains(got["eline CN00570"].Output, "TIMETRA-SERV-MIB") {
		t.Fatalf("scalar without .0: %#v", got)
	}
}

func TestSelectAndRenderElines(t *testing.T) {
	rows := []models.IcingaEline{
		{Device: "r1", Address: "10.0.0.1/24", Platform: "ios-xr", Monitor: true, ServiceID: "CN00570", PWID: 1000570},
		{Device: "r1", Address: "10.0.0.1/24", Platform: "ios-xr", Monitor: true, ServiceID: "CN00570", PWID: 1000570},
		{Device: "quiet", Address: "10.0.0.2/24", Platform: "eos", Monitor: false, ServiceID: "CN00571", PWID: 1000571},
		{Device: "skip", Address: "10.0.0.3/24", Platform: "sros", Monitor: true, ServiceID: "CN00572", PWID: 1000572},
		{Device: "r2", Address: "10.0.0.4/24", Platform: "eos", Monitor: true, ServiceID: "CN00580", PWID: 1000580, Local: true},
		{Device: "z9", Address: "10.0.0.9/24", Platform: "ios-xr", Monitor: true, ServiceID: "CN00570", PWID: 1000570},
	}
	got := selectElineTargets(rows, "lab.example", "skip\n")
	if len(got) != 3 {
		t.Fatalf("devices %#v", got)
	}
	if got[0].Host != "r1.lab.example" || len(got[0].Elines) != 1 || got[0].Elines[0].Service != "eline CN00570" || got[0].Elines[0].PeerHost != "" {
		t.Fatalf("r1 %#v", got[0])
	}
	if got[1].Host != "r2.lab.example" || !got[1].Elines[0].Local || got[1].Elines[0].PeerHost != "" {
		t.Fatalf("r2 %#v", got[1])
	}
	if got[2].Host != "z9.lab.example" || got[2].Elines[0].PeerHost != "r1.lab.example" || got[2].Elines[0].PeerService != "eline CN00570" {
		t.Fatalf("z9 %#v", got[2])
	}
	var b strings.Builder
	if err := renderInterfaceConf(&b, true, nil, got); err != nil {
		t.Fatal(err)
	}
	body := b.String()
	for _, want := range []string{
		`template Service "factum-eline"`,
		`assign where service.vars.factum_eline`,
		`object Service "eline CN00570"`,
		`import "factum-eline"`,
		`host_name = "r1.lab.example"`,
		`vars.eline_pw = "1000570"`,
		`object Service "eline CN00580"`,
		`apply Dependency "factum-eline-peer"`,
		`states = [ OK, Unknown ]`,
		`vars.eline_peer_host = "r1.lab.example"`,
		`vars.eline_peer_service = "eline CN00570"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("missing %q", want)
		}
	}
}

func TestPollElinesShareSession(t *testing.T) {
	devices := []*models.Device{{
		Name: "r1", Enabled: true, PrimaryIPv4: "10.1.0.1/24",
		CfMonitorIcinga: true, CfAlarmInterfaces: true,
		Interfaces: []models.Interface{{Name: "GigabitEthernet0/0/0/1"}},
	}}
	opens := 0
	open := func(ctx context.Context, addr, community string) (snmpQuerier, error) {
		opens++
		return &memSNMP{
			scalars: map[string]snmpValue{oidCpwIndexNext + ".0": numCol(1)},
			cols: map[string]map[int]snmpValue{
				oidIfName:        {1: textCol(1, "GigabitEthernet0/0/0/1")},
				oidIfAdminStatus: {1: numCol(1)},
				oidIfOperStatus:  {1: numCol(1)},
				oidIfHCInOctets:  {1: numCol(10)},
				oidIfHCOutOctets: {1: numCol(10)},
				oidCpwID:         {4: numCol(1000570)},
				oidCpwName:       {4: textCol(4, "CN00570")},
				oidCpwAdmin:      {4: numCol(1)},
				oidCpwOper:       {4: numCol(1)},
			},
		}, nil
	}
	fic := &FactumIcingaClient{IcingaConfig: &util.ConfigIcinga{
		CommonConfig:      util.CommonConfig{DefaultDomain: "lab.example"},
		MonitorInterfaces: true,
		SNMPCommunities:   "public",
	}}
	sub := &memSubmitter{}
	stats, err := fic.pollOnce(context.Background(), PollOptions{
		Open: open, Submit: sub,
		Elines: []models.IcingaEline{{
			Device: "r1", Address: "10.1.0.1/24", Platform: "ios-xr", Monitor: true,
			ServiceID: "CN00570", PWID: 1000570,
		}},
	}, devices, &counterStore{cur: map[string]ifaceCounters{}}, sub, time.Unix(1_700_000_000, 0))
	if err != nil {
		t.Fatal(err)
	}
	if opens != 1 {
		t.Fatalf("opens %d", opens)
	}
	if stats.Interfaces != 1 || stats.Elines != 1 || stats.Submitted != 2 || stats.SNMPErrors != 0 {
		t.Fatalf("stats %#v", stats)
	}
	var ifaceOK, elineOK bool
	for _, r := range sub.results {
		if r.Service == "iface GigabitEthernet0/0/0/1" && r.Status == statusOK {
			ifaceOK = true
		}
		if r.Service == "eline CN00570" && r.Status == statusOK && strings.Contains(r.Output, "pw 1000570 up") {
			elineOK = true
		}
	}
	if !ifaceOK || !elineOK {
		t.Fatalf("results %#v", sub.results)
	}
}

func TestWriteInterfacesIncludesElines(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/interfaces.conf"
	fic := &FactumIcingaClient{IcingaConfig: &util.ConfigIcinga{
		CommonConfig:      util.CommonConfig{DefaultDomain: "lab.example"},
		InterfacesFile:    path,
		MonitorInterfaces: true,
	}}
	devices := []*models.Device{{
		Name: "r1", Enabled: true, PrimaryIPv4: "10.1.0.1/24", CfAlarmInterfaces: true,
		Interfaces: []models.Interface{{Name: "Ethernet1"}},
	}}
	elines := []models.IcingaEline{{
		Device: "r1", Address: "10.1.0.1/24", Platform: "eos", Monitor: true,
		ServiceID: "CN00001", PWID: 1000001,
	}}
	if _, err := fic.writeInterfaces(devices, elines, jobevent.NewConsoleReporter(io.Discard)); err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), `object Service "eline CN00001"`) || !strings.Contains(string(body), `object Service "iface Ethernet1"`) {
		t.Fatalf("conf:\n%s", body)
	}
}
