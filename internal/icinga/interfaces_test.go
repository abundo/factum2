package icinga

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/abundo/factum2/internal/jobevent"
	"github.com/abundo/factum2/internal/util"
	"github.com/abundo/factum2/models"
)

func TestSelectInterfaceTargets(t *testing.T) {
	devices := []*models.Device{
		{Name: "r1", Enabled: true, PrimaryIPv4: "10.0.0.1/24", CfMonitorIcinga: true, CfAlarmInterfaces: true, Interfaces: []models.Interface{
			{Name: "Ethernet1", Description: "uplink"},
			{Name: "Ethernet1"},
			{Name: "  "},
			{Name: `bad!name`},
		}},
		{Name: "quiet", Enabled: true, PrimaryIPv4: "10.0.0.2/24", CfAlarmInterfaces: false, Interfaces: []models.Interface{{Name: "Ethernet1"}}},
		{Name: "down", Enabled: false, PrimaryIPv4: "10.0.0.3/24", CfAlarmInterfaces: true, Interfaces: []models.Interface{{Name: "Ethernet1"}}},
		{Name: "skip", Enabled: true, PrimaryIPv4: "10.0.0.4/24", CfAlarmInterfaces: true, Interfaces: []models.Interface{{Name: "Ethernet9"}}},
	}
	got := selectInterfaceTargets(devices, "lab.example", "skip\n")
	if len(got) != 1 || got[0].Host != "r1.lab.example" || got[0].Addr != "10.0.0.1" {
		t.Fatalf("targets: %#v", got)
	}
	if len(got[0].Ifaces) != 3 {
		t.Fatalf("ifaces: %#v", got[0].Ifaces)
	}
	if got[0].Ifaces[0].Service != "iface Ethernet1" || got[0].Ifaces[1].Service != "iface Ethernet1 #2" {
		t.Fatalf("service names: %#v", got[0].Ifaces)
	}
	if got[0].Ifaces[2].Service != "iface bad_name" {
		t.Fatalf("sanitized: %#v", got[0].Ifaces[2])
	}
}

func TestRenderInterfaceConfOff(t *testing.T) {
	var b bytes.Buffer
	if err := renderInterfaceConf(&b, false, []deviceIfaces{{Host: "r1", Ifaces: []ifaceTarget{{Name: "Ethernet1", Service: "iface Ethernet1"}}}}); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(b.String(), "object Service") {
		t.Fatalf("off render still has services:\n%s", b.String())
	}
}

func TestWriteInterfaces(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "interfaces.conf")
	fic := &FactumIcingaClient{IcingaConfig: &util.ConfigIcinga{
		CommonConfig:      util.CommonConfig{DefaultDomain: "lab.example"},
		InterfacesFile:    path,
		MonitorInterfaces: true,
	}}
	devices := []*models.Device{{
		Name: "r1", Enabled: true, PrimaryIPv4: "10.1.0.1/24", CfAlarmInterfaces: true,
		Interfaces: []models.Interface{{Name: "Ethernet1/1", Description: "uplink"}},
	}}
	changed, err := fic.writeInterfaces(devices, jobevent.NewConsoleReporter(io.Discard))
	if err != nil || !changed {
		t.Fatalf("write: changed=%v err=%v", changed, err)
	}
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	s := string(body)
	for _, want := range []string{
		`object CheckCommand "factum-interface-stale"`,
		`enable_passive_checks = true`,
		`check_command = "factum-interface-stale"`,
		`apply Dependency "factum-interface-host"`,
		`object Service "iface Ethernet1/1"`,
		`host_name = "r1.lab.example"`,
		`vars.iface_name = "Ethernet1/1"`,
		`vars.iface_description = "uplink"`,
	} {
		if !strings.Contains(s, want) {
			t.Errorf("missing %q", want)
		}
	}
	if strings.Contains(s, "check_snmp") {
		t.Fatal("interface conf must not use per-interface SNMP checks")
	}
	fic.IcingaConfig.MonitorInterfaces = false
	if _, err := fic.writeInterfaces(devices, jobevent.NewConsoleReporter(io.Discard)); err != nil {
		t.Fatal(err)
	}
	body, err = os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(body), "object Service") {
		t.Fatalf("disabling monitoring left services:\n%s", body)
	}
}

func TestRenderInterfaceConf40000(t *testing.T) {
	const perDevice = 100
	const devicesN = 400
	devices := make([]deviceIfaces, devicesN)
	for d := 0; d < devicesN; d++ {
		ifaces := make([]ifaceTarget, perDevice)
		for i := 0; i < perDevice; i++ {
			name := "Ethernet" + itoa(i)
			ifaces[i] = ifaceTarget{Name: name, Service: "iface " + name, Description: "uplink"}
		}
		devices[d] = deviceIfaces{Host: "sw" + itoa(d) + ".lab.example", Addr: "10.0.0.1", Ifaces: ifaces}
	}
	start := time.Now()
	var b bytes.Buffer
	if err := renderInterfaceConf(&b, true, devices); err != nil {
		t.Fatal(err)
	}
	if elapsed := time.Since(start); elapsed > 10*time.Second {
		t.Fatalf("rendering 40000 services took %s", elapsed)
	}
	if got := strings.Count(b.String(), "object Service "); got != devicesN*perDevice {
		t.Fatalf("services: got %d", got)
	}
}

func TestEvalInterface(t *testing.T) {
	iface := ifaceTarget{Name: "Ethernet1", Service: "iface Ethernet1"}
	now := time.Unix(1_700_000_100, 0)
	prev := &ifaceCounters{At: now.Add(-10 * time.Second).UnixMilli(), InOctets: 1000, OutOctets: 1000}
	sample := snmpIface{Admin: 1, HasAdmin: true, Oper: 1, HasOper: true, InOctets: 1000 + 1_250_000, OutOctets: 1000, HasIn: true, HasOut: true, SpeedMbps: 1000, HasSpeed: true}
	status, output, perf, _, keep := evalInterface(iface, sample, prev, now)
	if status != statusOK || !keep || !strings.Contains(output, "up") || !strings.Contains(output, "1.00 Mbit/s in") {
		t.Fatalf("up: status=%d keep=%v output=%q perf=%q", status, keep, output, perf)
	}
	if !strings.Contains(perf, "in_bps=1000000") || !strings.Contains(perf, "speed_mbps=1000") {
		t.Fatalf("perf: %s", perf)
	}

	sample.Oper = 2
	status, output, _, _, _ = evalInterface(iface, sample, prev, now)
	if status != statusCrit || !strings.Contains(output, "down") {
		t.Fatalf("down: %d %s", status, output)
	}

	sample.Admin = 2
	sample.Oper = 2
	status, output, _, _, _ = evalInterface(iface, sample, prev, now)
	if status != statusOK || !strings.Contains(output, "admin down") {
		t.Fatalf("admin down: %d %s", status, output)
	}

	wrapped := sample
	wrapped.Admin = 1
	wrapped.Oper = 1
	wrapped.InOctets = 10
	status, output, perf, next, keep := evalInterface(iface, wrapped, prev, now)
	if status != statusOK || !keep || perf != "" || next.InOctets != 10 {
		t.Fatalf("wrap: status=%d keep=%v perf=%q next=%d output=%s", status, keep, perf, next.InOctets, output)
	}
}

func TestFormatExternalCommand(t *testing.T) {
	line := formatExternalCommand(10, checkResult{Host: "r1.lab.example", Service: "iface Ethernet1", Status: statusCrit, Output: "Ethernet1 down\nbad|pipe", Perf: "in_bps=1"})
	want := "[10] PROCESS_SERVICE_CHECK_RESULT;r1.lab.example;iface Ethernet1;2;Ethernet1 down bad/pipe|in_bps=1\n"
	if line != want {
		t.Fatalf("got %q", line)
	}
}

func TestCounterStoreRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "interfaces.state")
	s := &counterStore{cur: map[string]ifaceCounters{
		counterKey("r1.lab.example", "iface Ethernet1"): {At: 5, InOctets: 9, OutOctets: 8},
	}}
	if err := s.save(path); err != nil {
		t.Fatal(err)
	}
	loaded, err := loadCounterStore(path)
	if err != nil {
		t.Fatal(err)
	}
	got := loaded.get("r1.lab.example", "iface Ethernet1")
	if got == nil || got.InOctets != 9 || got.OutOctets != 8 {
		t.Fatalf("loaded %#v", got)
	}
}

func TestPinIcingaInterfacePath(t *testing.T) {
	cfg := &util.ConfigIcinga{MonitorInterfaces: true}
	if err := pinIcingaPaths(cfg, util.ConfigWorkerPaths{}); err == nil {
		t.Fatal("monitoring on with an empty interfaces pin must fail")
	}
	cfg.MonitorInterfaces = false
	if err := pinIcingaPaths(cfg, util.ConfigWorkerPaths{}); err != nil {
		t.Fatal(err)
	}
	cfg = &util.ConfigIcinga{MonitorInterfaces: true}
	if err := pinIcingaPaths(cfg, util.ConfigWorkerPaths{IcingaInterfacesFile: "/factum/interfaces.conf"}); err != nil {
		t.Fatal(err)
	}
	if cfg.InterfacesFile != "/factum/interfaces.conf" {
		t.Fatalf("path %q", cfg.InterfacesFile)
	}
}

type memSNMP struct {
	cols  map[string]map[int]snmpValue
	walks []string
	gets  int
}

func (m *memSNMP) Walk(base string) (map[int]snmpValue, error) {
	m.walks = append(m.walks, base)
	if col, ok := m.cols[base]; ok {
		cp := make(map[int]snmpValue, len(col))
		for k, v := range col {
			cp[k] = v
		}
		return cp, nil
	}
	return map[int]snmpValue{}, nil
}

func (m *memSNMP) Get(oids []string) (map[string]snmpValue, error) {
	m.gets++
	out := make(map[string]snmpValue, len(oids))
	for _, oid := range oids {
		dot := strings.LastIndex(oid, ".")
		if dot < 0 {
			continue
		}
		base, idxText := oid[:dot], oid[dot+1:]
		idx := 0
		for _, r := range idxText {
			idx = idx*10 + int(r-'0')
		}
		if col, ok := m.cols[base]; ok {
			if v, ok := col[idx]; ok {
				out[oid] = v
			}
		}
	}
	return out, nil
}

func (m *memSNMP) Close() error { return nil }

func textCol(idx int, text string) snmpValue {
	return snmpValue{text: text, isText: true, ok: true}
}

func numCol(n uint64) snmpValue { return snmpValue{u: n, ok: true} }

func TestCollectSamplesMatchesDescrAndLegacyCounters(t *testing.T) {
	m := &memSNMP{cols: map[string]map[int]snmpValue{
		oidIfName:        {1: textCol(1, "lo")},
		oidIfDescr:       {7: textCol(7, "Ethernet1")},
		oidIfAdminStatus: {7: numCol(1)},
		oidIfOperStatus:  {7: numCol(1)},
		oidIfInOctets:    {7: numCol(50)},
		oidIfOutOctets:   {7: numCol(60)},
	}}
	wants := []ifaceTarget{{Name: "ethernet1", Service: "iface ethernet1"}}
	got, err := collectSamples(m, map[int]snmpValue{1: textCol(1, "lo")}, wants)
	if err != nil {
		t.Fatal(err)
	}
	sample, ok := got["iface ethernet1"]
	if !ok || !sample.HasIn || sample.InOctets != 50 || sample.OutOctets != 60 || sample.Oper != 1 {
		t.Fatalf("sample %#v walks=%v", sample, m.walks)
	}
	if len(m.walks) != 1 || m.walks[0] != oidIfDescr {
		t.Fatalf("walks %v", m.walks)
	}
}

type memSubmitter struct {
	mu      sync.Mutex
	results []checkResult
}

func (m *memSubmitter) Submit(ctx context.Context, results []checkResult) error {
	m.mu.Lock()
	m.results = append(m.results, results...)
	m.mu.Unlock()
	return nil
}

func TestPollManyInterfaces(t *testing.T) {
	const perDevice = 100
	const devicesN = 400
	devices := make([]*models.Device, devicesN)
	for d := 0; d < devicesN; d++ {
		ifaces := make([]models.Interface, perDevice)
		for i := 0; i < perDevice; i++ {
			ifaces[i] = models.Interface{Name: "Ethernet" + itoa(i)}
		}
		devices[d] = &models.Device{
			Name: "sw" + itoa(d), Enabled: true, PrimaryIPv4: "10.0.0.1/24",
			CfMonitorIcinga: true, CfAlarmInterfaces: true, Interfaces: ifaces,
		}
	}
	round := 0
	open := func(ctx context.Context, addr, community string) (snmpQuerier, error) {
		if community != "public" {
			return nil, io.ErrUnexpectedEOF
		}
		names := make(map[int]snmpValue, perDevice)
		cols := map[string]map[int]snmpValue{oidIfName: names}
		for _, base := range interfaceColumns {
			cols[base] = make(map[int]snmpValue, perDevice)
		}
		for i := 1; i <= perDevice; i++ {
			names[i] = textCol(i, "Ethernet"+itoa(i-1))
			cols[oidIfAdminStatus][i] = numCol(1)
			cols[oidIfOperStatus][i] = numCol(1)
			cols[oidIfHCInOctets][i] = numCol(uint64(round) * 125000)
			cols[oidIfHCOutOctets][i] = numCol(uint64(round) * 125000)
			cols[oidIfHighSpeed][i] = numCol(1000)
		}
		return &memSNMP{cols: cols}, nil
	}
	fic := &FactumIcingaClient{IcingaConfig: &util.ConfigIcinga{
		CommonConfig:      util.CommonConfig{DefaultDomain: "lab.example"},
		MonitorInterfaces: true,
		SNMPCommunities:   "wrong\npublic",
	}}
	sub := &memSubmitter{}
	opt := PollOptions{Workers: 32, Open: open, Submit: sub}
	start := time.Now()
	now := time.Unix(1_700_000_000, 0)
	store := &counterStore{cur: map[string]ifaceCounters{}}
	stats, err := fic.pollOnce(context.Background(), opt, devices, store, sub, now)
	if err != nil {
		t.Fatal(err)
	}
	round = 1
	sub.mu.Lock()
	sub.results = nil
	sub.mu.Unlock()
	stats, err = fic.pollOnce(context.Background(), opt, devices, store, sub, now.Add(time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if elapsed := time.Since(start); elapsed > 15*time.Second {
		t.Fatalf("polling 40000 interfaces took %s", elapsed)
	}
	if stats.Interfaces != devicesN*perDevice || stats.Submitted != devicesN*perDevice || stats.SNMPErrors != 0 {
		t.Fatalf("stats %#v", stats)
	}
	if len(sub.results) != devicesN*perDevice {
		t.Fatalf("results %d", len(sub.results))
	}
	if sub.results[0].Status != statusOK || !strings.Contains(sub.results[0].Perf, "in_bps=1000000") {
		t.Fatalf("result %#v", sub.results[0])
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [16]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}
