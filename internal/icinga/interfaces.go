package icinga

// Interface services are passive. One active check_snmp per interface
// forks a process and opens an SNMP session per check. At 40000
// interfaces that is more than a hundred forks a second, and counter
// rates need the previous sample. factum2-icinga poll instead opens one
// SNMPv2c session per device, reads ifOperStatus and the HC counters for
// the interfaces on that device, and submits passive results.
//
// Sync only writes the Icinga objects. The poller is a separate process
// (factum2-icinga poll --interval 1m) so a config reload does not wait
// on SNMP.

import (
	"bufio"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"

	"github.com/abundo/factum2/internal/util"
	"github.com/abundo/factum2/models"
)

const (
	statusOK      = 0
	statusWarn    = 1
	statusCrit    = 2
	statusUnknown = 3

	interfaceConfNote = "Passive interface services. factum2-icinga poll submits status and counter results. Do not replace these with per-interface SNMP checks."
)

// interfaceServicePrelude is one CheckCommand, one Service template, and
// one apply Dependency. The apply rule covers every interface service, so
// the file does not grow a Dependency object per interface.
// Icinga 2 has no check_freshness or freshness_threshold attributes.
// A result that is older than check_interval is stale, and Icinga then
// runs the check command. That command exits UNKNOWN when poll has stopped.
const interfaceServicePrelude = `object CheckCommand "factum-interface-stale" {
  command = [ "/bin/sh", "-c", "echo 'No SNMP result from factum2-icinga poll'; exit 3" ]
}

template Service "factum-interface" {
  check_command = "factum-interface-stale"
  enable_active_checks = true
  enable_passive_checks = true
  max_check_attempts = 1
  check_interval = 5m
  retry_interval = 1m
  enable_flapping = false
  vars.factum_interface = true
}

apply Dependency "factum-interface-host" to Service {
  parent_host_name = host.name
  disable_notifications = true
  states = [ Up ]
  assign where service.vars.factum_interface
}

`

type ifaceTarget struct {
	Name        string
	Service     string
	Description string
}

type deviceIfaces struct {
	Host   string
	Addr   string
	Ifaces []ifaceTarget
}

func deviceEligible(device *models.Device, ignore string) bool {
	return device != nil && !isIgnoredDevice(device.Name, ignore) && device.Enabled && device.PrimaryIPv4 != ""
}

func deviceAddr(device *models.Device) string {
	return strings.Split(device.PrimaryIPv4, "/")[0]
}

// interfaceServiceName is the Icinga service object name. "!" is illegal
// in an object name. ";" and "|" would break the external command pipe.
func interfaceServiceName(name string) string {
	var b strings.Builder
	b.Grow(len(name) + 6)
	b.WriteString("iface ")
	for _, r := range strings.TrimSpace(name) {
		switch r {
		case '!', ';', '\n', '\r', '|', '"':
			b.WriteByte('_')
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

func assignServiceNames(ifaces []models.Interface) []ifaceTarget {
	sorted := append([]models.Interface(nil), ifaces...)
	sort.Slice(sorted, func(i, j int) bool {
		if sorted[i].Name != sorted[j].Name {
			return sorted[i].Name < sorted[j].Name
		}
		return sorted[i].ID < sorted[j].ID
	})
	seen := make(map[string]int, len(sorted))
	out := make([]ifaceTarget, 0, len(sorted))
	for _, iface := range sorted {
		name := strings.TrimSpace(iface.Name)
		if name == "" {
			continue
		}
		base := interfaceServiceName(name)
		service := base
		if n := seen[base]; n > 0 {
			service = fmt.Sprintf("%s #%d", base, n+1)
		}
		seen[base]++
		desc := iface.Description
		if runes := []rune(desc); len(runes) > 120 {
			desc = string(runes[:120])
		}
		out = append(out, ifaceTarget{Name: name, Service: service, Description: desc})
	}
	return out
}

// selectInterfaceTargets returns devices that are written as Icinga hosts
// and have Interface alarms set. Host names use the same FQDN rule as the
// host template's fqdn() function.
func selectInterfaceTargets(devices []*models.Device, domain, ignore string) []deviceIfaces {
	out := make([]deviceIfaces, 0, len(devices))
	for _, device := range devices {
		if !device.CfAlarmInterfaces || !deviceEligible(device, ignore) || strings.TrimSpace(device.Name) == "" {
			continue
		}
		ifaces := assignServiceNames(device.Interfaces)
		if len(ifaces) == 0 {
			continue
		}
		out = append(out, deviceIfaces{
			Host:   util.FormatName(domain, device.Name),
			Addr:   deviceAddr(device),
			Ifaces: ifaces,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Host < out[j].Host })
	return out
}

func countIfaces(devices []deviceIfaces) int {
	n := 0
	for _, d := range devices {
		n += len(d.Ifaces)
	}
	return n
}

func renderInterfaceConf(w io.Writer, monitor bool, devices []deviceIfaces) error {
	bw := bufio.NewWriterSize(w, 1<<20)
	if !monitor {
		if _, err := io.WriteString(bw, "// Interface monitoring is off.\n"); err != nil {
			return err
		}
		return bw.Flush()
	}
	if _, err := io.WriteString(bw, interfaceServicePrelude); err != nil {
		return err
	}
	for _, device := range devices {
		for _, iface := range device.Ifaces {
			if _, err := fmt.Fprintf(bw, "object Service \"%s\" {\n  import \"factum-interface\"\n  host_name = \"%s\"\n  vars.iface_name = \"%s\"\n  vars.iface_description = \"%s\"\n}\n\n",
				quote(iface.Service), quote(device.Host), quote(iface.Name), quote(iface.Description)); err != nil {
				return err
			}
		}
	}
	return bw.Flush()
}

type ifaceCounters struct {
	At          int64
	InOctets    uint64
	OutOctets   uint64
	InErrors    uint64
	OutErrors   uint64
	InDiscards  uint64
	OutDiscards uint64
}

type snmpIface struct {
	Admin       int
	Oper        int
	HasAdmin    bool
	HasOper     bool
	InOctets    uint64
	OutOctets   uint64
	HasIn       bool
	HasOut      bool
	InErrors    uint64
	OutErrors   uint64
	InDiscards  uint64
	OutDiscards uint64
	SpeedMbps   uint64
	HasSpeed    bool
}

func counterDelta(prev, cur uint64) (uint64, bool) {
	if cur < prev {
		return 0, false
	}
	return cur - prev, true
}

func formatBitrate(bps float64) string {
	switch {
	case bps >= 1e9:
		return fmt.Sprintf("%.2f Gbit/s", bps/1e9)
	case bps >= 1e6:
		return fmt.Sprintf("%.2f Mbit/s", bps/1e6)
	case bps >= 1e3:
		return fmt.Sprintf("%.2f kbit/s", bps/1e3)
	default:
		return fmt.Sprintf("%.0f bit/s", bps)
	}
}

type ratePair struct {
	inBps   float64
	outBps  float64
	inErr   float64
	outErr  float64
	inDisc  float64
	outDisc float64
	ok      bool
}

func rates(prev ifaceCounters, cur snmpIface, now time.Time) ratePair {
	if prev.At == 0 || !cur.HasIn || !cur.HasOut {
		return ratePair{}
	}
	dt := now.Sub(time.UnixMilli(prev.At)).Seconds()
	if dt < 0.5 {
		return ratePair{}
	}
	inOct, okIn := counterDelta(prev.InOctets, cur.InOctets)
	outOct, okOut := counterDelta(prev.OutOctets, cur.OutOctets)
	if !okIn || !okOut {
		return ratePair{}
	}
	rp := ratePair{
		inBps:  float64(inOct) * 8 / dt,
		outBps: float64(outOct) * 8 / dt,
		ok:     true,
	}
	if d, ok := counterDelta(prev.InErrors, cur.InErrors); ok {
		rp.inErr = float64(d) / dt
	}
	if d, ok := counterDelta(prev.OutErrors, cur.OutErrors); ok {
		rp.outErr = float64(d) / dt
	}
	if d, ok := counterDelta(prev.InDiscards, cur.InDiscards); ok {
		rp.inDisc = float64(d) / dt
	}
	if d, ok := counterDelta(prev.OutDiscards, cur.OutDiscards); ok {
		rp.outDisc = float64(d) / dt
	}
	return rp
}

func operText(oper int) (int, string) {
	switch oper {
	case 1:
		return statusOK, "up"
	case 2:
		return statusCrit, "down"
	case 3:
		return statusOK, "testing"
	case 4:
		return statusUnknown, "unknown"
	case 5:
		return statusWarn, "dormant"
	case 6:
		return statusOK, "not present"
	case 7:
		return statusCrit, "lower layer down"
	default:
		return statusUnknown, "unknown oper status"
	}
}

// evalInterface turns one SNMP sample into an Icinga result. Admin-down
// interfaces stay OK so unused ports do not page. A counter wrap skips
// the rate for this interval and still stores the new sample.
func evalInterface(iface ifaceTarget, sample snmpIface, prev *ifaceCounters, now time.Time) (status int, output, perf string, next ifaceCounters, keep bool) {
	next = ifaceCounters{
		At:          now.UnixMilli(),
		InOctets:    sample.InOctets,
		OutOctets:   sample.OutOctets,
		InErrors:    sample.InErrors,
		OutErrors:   sample.OutErrors,
		InDiscards:  sample.InDiscards,
		OutDiscards: sample.OutDiscards,
	}
	keep = sample.HasIn && sample.HasOut
	if !sample.HasAdmin {
		return statusUnknown, iface.Name + " missing ifAdminStatus", "", next, keep
	}
	if sample.Admin != 1 {
		switch sample.Admin {
		case 2:
			return statusOK, iface.Name + " admin down", "", next, keep
		case 3:
			return statusOK, iface.Name + " admin testing", "", next, keep
		default:
			return statusUnknown, fmt.Sprintf("%s admin status %d", iface.Name, sample.Admin), "", next, keep
		}
	}
	if !sample.HasOper {
		return statusUnknown, iface.Name + " missing ifOperStatus", "", next, keep
	}
	status, label := operText(sample.Oper)
	var rp ratePair
	if prev != nil {
		rp = rates(*prev, sample, now)
	}
	b := strings.Builder{}
	b.WriteString(iface.Name)
	b.WriteByte(' ')
	b.WriteString(label)
	if rp.ok {
		b.WriteString(", ")
		b.WriteString(formatBitrate(rp.inBps))
		b.WriteString(" in, ")
		b.WriteString(formatBitrate(rp.outBps))
		b.WriteString(" out")
		if rp.inErr > 0 || rp.outErr > 0 {
			fmt.Fprintf(&b, ", errors %.2f/s in %.2f/s out", rp.inErr, rp.outErr)
		}
		perf = fmt.Sprintf("in_bps=%.0f out_bps=%.0f in_err_ps=%.3f out_err_ps=%.3f in_disc_ps=%.3f out_disc_ps=%.3f",
			rp.inBps, rp.outBps, rp.inErr, rp.outErr, rp.inDisc, rp.outDisc)
		if sample.HasSpeed && sample.SpeedMbps > 0 {
			perf += fmt.Sprintf(" speed_mbps=%d", sample.SpeedMbps)
		}
	}
	return status, b.String(), perf, next, keep
}
