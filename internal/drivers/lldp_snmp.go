package drivers

// LLDP neighbors over SNMPv2c. IEEE 802.1AB LLDP-MIB (lldpRemTable,
// 1.0.8802.1.1.2) is the table VRP, IOS-XR, SR OS, and Cisco SMB share.
// LLDP-V2-MIB is used only when the original MIB is absent. EOS stays on
// eAPI; Open ROADM stays on its YANG external-link list.

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/gosnmp/gosnmp"
)

const (
	oidLLDPLocSysName   = "1.0.8802.1.1.2.1.3.3.0"
	oidLLDPLocPortEntry = "1.0.8802.1.1.2.1.3.7.1"
	oidLLDPRemEntry     = "1.0.8802.1.1.2.1.4.1.1"

	oidLLDPV2LocSysName   = "1.3.111.2.802.1.1.13.1.3.3.0"
	oidLLDPV2LocPortEntry = "1.3.111.2.802.1.1.13.1.3.7.1"
	oidLLDPV2RemEntry     = "1.3.111.2.802.1.1.13.1.4.1.1"

	lldpPortInterfaceAlias = 1
	lldpPortInterfaceName  = 5
	lldpPortLocal          = 7
	lldpChassisMAC         = 4

	lldpColPortIDSubtype = 2
	lldpColPortID        = 3

	lldpRemChassisSubtype = 4
	lldpRemChassisID      = 5
	lldpRemPortSubtype    = 6
	lldpRemPortID         = 7
	lldpRemSysName        = 9

	lldpV2RemChassisSubtype = 5
	lldpV2RemChassisID      = 6
	lldpV2RemPortSubtype    = 7
	lldpV2RemPortID         = 8
	lldpV2RemSysName        = 10
)

var errLLDPMIBMissing = errors.New("device does not expose LLDP-MIB or LLDP-V2-MIB")

// lldpBinding is one SNMP varbind, reduced to what the LLDP tables use.
type lldpBinding struct {
	OID   string
	Raw   []byte
	Num   int64
	IsNum bool
}

type lldpQuerier interface {
	Walk(base string) ([]lldpBinding, error)
	Get(oid string) (lldpBinding, bool, error)
	Close() error
}

// lldpNeighbors reads LLDP neighbors from host:161. communities are tried
// in order. An empty list is an error: there is no CLI fallback.
func lldpNeighbors(host string, communities []string) ([]*Neighbor, error) {
	host = snmpHost(host)
	if host == "" {
		return nil, errors.New("lldp: device name is empty")
	}
	var tried bool
	var last error
	for _, community := range communities {
		community = strings.TrimSpace(community)
		if community == "" {
			continue
		}
		tried = true
		q, err := openLLDP(host, community)
		if err != nil {
			last = err
			continue
		}
		neighbors, err := neighborsFromQuery(q)
		_ = q.Close()
		if err == nil || errors.Is(err, errLLDPMIBMissing) {
			return neighbors, err
		}
		last = err
	}
	if !tried {
		return nil, errors.New("lldp: no SNMPv2c community configured")
	}
	return nil, last
}

func snmpHost(name string) string {
	name = strings.TrimSpace(name)
	return strings.Trim(name, "[]")
}

func openLLDP(host, community string) (lldpQuerier, error) {
	g := &gosnmp.GoSNMP{
		Target:         host,
		Port:           161,
		Community:      community,
		Version:        gosnmp.Version2c,
		Timeout:        3 * time.Second,
		Retries:        1,
		MaxRepetitions: 25,
		Context:        context.Background(),
	}
	if err := g.Connect(); err != nil {
		return nil, err
	}
	return &gosnmpLLDP{g: g}, nil
}

type gosnmpLLDP struct {
	g *gosnmp.GoSNMP
}

func (c *gosnmpLLDP) Close() error {
	if c.g != nil && c.g.Conn != nil {
		return c.g.Conn.Close()
	}
	return nil
}

func (c *gosnmpLLDP) Walk(base string) ([]lldpBinding, error) {
	var out []lldpBinding
	err := c.g.BulkWalk(base, func(pdu gosnmp.SnmpPDU) error {
		b, ok := bindingFromPDU(pdu)
		if ok {
			out = append(out, b)
		}
		return nil
	})
	return out, err
}

func (c *gosnmpLLDP) Get(oid string) (lldpBinding, bool, error) {
	pkt, err := c.g.Get([]string{oid})
	if err != nil {
		return lldpBinding{}, false, err
	}
	if pkt.Error != gosnmp.NoError {
		return lldpBinding{}, false, fmt.Errorf("snmp get %s: %s", oid, pkt.Error)
	}
	if len(pkt.Variables) == 0 {
		return lldpBinding{}, false, nil
	}
	b, ok := bindingFromPDU(pkt.Variables[0])
	return b, ok, nil
}

func bindingFromPDU(pdu gosnmp.SnmpPDU) (lldpBinding, bool) {
	switch pdu.Type {
	case gosnmp.NoSuchObject, gosnmp.NoSuchInstance, gosnmp.EndOfMibView, gosnmp.Null:
		return lldpBinding{}, false
	}
	b := lldpBinding{OID: strings.TrimPrefix(pdu.Name, ".")}
	switch pdu.Type {
	case gosnmp.OctetString:
		switch v := pdu.Value.(type) {
		case []byte:
			b.Raw = append([]byte(nil), v...)
		case string:
			b.Raw = []byte(v)
		default:
			b.Raw = []byte(fmt.Sprint(pdu.Value))
		}
		return b, true
	default:
		if n, ok := pduInt(pdu.Value); ok {
			b.IsNum = true
			b.Num = n
			return b, true
		}
		b.Raw = []byte(fmt.Sprint(pdu.Value))
		return b, true
	}
}

func pduInt(v any) (int64, bool) {
	switch n := v.(type) {
	case int:
		return int64(n), true
	case int64:
		return n, true
	case int32:
		return int64(n), true
	case uint:
		return int64(n), true
	case uint32:
		return int64(n), true
	case uint64:
		if n > uint64(^uint64(0)>>1) {
			return 0, false
		}
		return int64(n), true
	default:
		return 0, false
	}
}

func neighborsFromQuery(q lldpQuerier) ([]*Neighbor, error) {
	present, err := mibPresent(q, oidLLDPLocSysName)
	if err != nil {
		return nil, err
	}
	if present {
		rem, err := q.Walk(oidLLDPRemEntry)
		if err != nil {
			return nil, err
		}
		loc, err := q.Walk(oidLLDPLocPortEntry)
		if err != nil {
			return nil, err
		}
		return assembleLLDP(rem, loc, false), nil
	}
	present, err = mibPresent(q, oidLLDPV2LocSysName)
	if err != nil {
		return nil, err
	}
	if !present {
		return nil, errLLDPMIBMissing
	}
	rem, err := q.Walk(oidLLDPV2RemEntry)
	if err != nil {
		return nil, err
	}
	loc, err := q.Walk(oidLLDPV2LocPortEntry)
	if err != nil {
		return nil, err
	}
	return assembleLLDP(rem, loc, true), nil
}

func mibPresent(q lldpQuerier, oid string) (bool, error) {
	_, ok, err := q.Get(oid)
	if err != nil {
		return false, err
	}
	return ok, nil
}

type lldpRemRow struct {
	local             int
	sysName           string
	portRaw           []byte
	portSubtype       int
	portSubtypeSet    bool
	chassisRaw        []byte
	chassisSubtype    int
	chassisSubtypeSet bool
}

type lldpLocPort struct {
	raw        []byte
	subtype    int
	subtypeSet bool
}

func assembleLLDP(rem, loc []lldpBinding, v2 bool) []*Neighbor {
	entry := oidLLDPRemEntry
	locEntry := oidLLDPLocPortEntry
	sysCol, portCol, portSubCol := lldpRemSysName, lldpRemPortID, lldpRemPortSubtype
	chCol, chSubCol := lldpRemChassisID, lldpRemChassisSubtype
	if v2 {
		entry = oidLLDPV2RemEntry
		locEntry = oidLLDPV2LocPortEntry
		sysCol, portCol, portSubCol = lldpV2RemSysName, lldpV2RemPortID, lldpV2RemPortSubtype
		chCol, chSubCol = lldpV2RemChassisID, lldpV2RemChassisSubtype
	}

	ports := map[int]*lldpLocPort{}
	for _, b := range loc {
		tail := oidTail(b.OID, locEntry)
		if len(tail) != 2 {
			continue
		}
		p := ports[tail[1]]
		if p == nil {
			p = &lldpLocPort{}
			ports[tail[1]] = p
		}
		switch tail[0] {
		case lldpColPortID:
			p.raw = b.Raw
		case lldpColPortIDSubtype:
			if b.IsNum {
				p.subtype = int(b.Num)
				p.subtypeSet = true
			}
		}
	}

	rows := map[string]*lldpRemRow{}
	for _, b := range rem {
		tail := oidTail(b.OID, entry)
		if len(tail) < 2 {
			continue
		}
		col, index := tail[0], tail[1:]
		if len(index) < 2 {
			continue
		}
		local := index[1]
		key := joinIndex(index)
		row := rows[key]
		if row == nil {
			row = &lldpRemRow{local: local}
			rows[key] = row
		}
		switch col {
		case sysCol:
			row.sysName = textOf(b.Raw)
		case portCol:
			row.portRaw = b.Raw
		case portSubCol:
			if b.IsNum {
				row.portSubtype = int(b.Num)
				row.portSubtypeSet = true
			}
		case chCol:
			row.chassisRaw = b.Raw
		case chSubCol:
			if b.IsNum {
				row.chassisSubtype = int(b.Num)
				row.chassisSubtypeSet = true
			}
		}
	}

	seen := map[string]bool{}
	var neighbors []*Neighbor
	for _, row := range rows {
		localName, ok := portName(ports[row.local])
		if !ok {
			continue
		}
		remoteIf, ok := portIDName(row.portSubtype, row.portSubtypeSet, row.portRaw)
		if !ok {
			continue
		}
		n := &Neighbor{
			LocalInterface:  localName,
			RemoteInterface: remoteIf,
			RemoteName:      row.sysName,
			SystemID:        chassisID(row.chassisSubtype, row.chassisSubtypeSet, row.chassisRaw),
		}
		key := strings.ToLower(n.LocalInterface) + "\x00" + strings.ToLower(n.RemoteName) + "\x00" + strings.ToLower(n.RemoteInterface)
		if seen[key] {
			continue
		}
		seen[key] = true
		neighbors = append(neighbors, n)
	}
	slices.SortFunc(neighbors, func(a, b *Neighbor) int {
		if c := strings.Compare(a.LocalInterface, b.LocalInterface); c != 0 {
			return c
		}
		if c := strings.Compare(a.RemoteName, b.RemoteName); c != 0 {
			return c
		}
		return strings.Compare(a.RemoteInterface, b.RemoteInterface)
	})
	return neighbors
}

func portName(p *lldpLocPort) (string, bool) {
	if p == nil {
		return "", false
	}
	return portIDName(p.subtype, p.subtypeSet, p.raw)
}

func portIDName(subtype int, set bool, raw []byte) (string, bool) {
	if set {
		switch subtype {
		case lldpPortInterfaceAlias, lldpPortInterfaceName, lldpPortLocal:
		default:
			return "", false
		}
	}
	text := textOf(raw)
	if text == "" {
		return "", false
	}
	return text, true
}

func chassisID(subtype int, set bool, raw []byte) string {
	if set && subtype == lldpChassisMAC {
		if mac := formatMAC(raw); mac != "" {
			return mac
		}
	}
	if text := textOf(raw); text != "" {
		return text
	}
	if mac := formatMAC(raw); mac != "" {
		return mac
	}
	if len(raw) == 0 {
		return ""
	}
	return fmt.Sprintf("%x", raw)
}

func formatMAC(raw []byte) string {
	if len(raw) != 6 {
		return ""
	}
	return fmt.Sprintf("%02x:%02x:%02x:%02x:%02x:%02x", raw[0], raw[1], raw[2], raw[3], raw[4], raw[5])
}

func textOf(raw []byte) string {
	raw = trimNUL(raw)
	if len(raw) == 0 {
		return ""
	}
	for _, b := range raw {
		if b < 0x20 || b > 0x7e {
			return ""
		}
	}
	return strings.TrimSpace(string(raw))
}

func trimNUL(raw []byte) []byte {
	return []byte(strings.Trim(string(raw), "\x00 "))
}

func oidTail(oid, base string) []int {
	oid = strings.TrimPrefix(oid, ".")
	base = strings.TrimPrefix(base, ".")
	prefix := base + "."
	if !strings.HasPrefix(oid, prefix) {
		return nil
	}
	parts := strings.Split(oid[len(prefix):], ".")
	out := make([]int, 0, len(parts))
	for _, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 {
			return nil
		}
		out = append(out, n)
	}
	return out
}

func joinIndex(index []int) string {
	var b strings.Builder
	for i, n := range index {
		if i > 0 {
			b.WriteByte('.')
		}
		b.WriteString(strconv.Itoa(n))
	}
	return b.String()
}
