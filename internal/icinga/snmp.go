package icinga

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/gosnmp/gosnmp"
)

const (
	oidIfDescr       = "1.3.6.1.2.1.2.2.1.2"
	oidIfAdminStatus = "1.3.6.1.2.1.2.2.1.7"
	oidIfOperStatus  = "1.3.6.1.2.1.2.2.1.8"
	oidIfInOctets    = "1.3.6.1.2.1.2.2.1.10"
	oidIfInDiscards  = "1.3.6.1.2.1.2.2.1.13"
	oidIfInErrors    = "1.3.6.1.2.1.2.2.1.14"
	oidIfOutOctets   = "1.3.6.1.2.1.2.2.1.16"
	oidIfOutDiscards = "1.3.6.1.2.1.2.2.1.19"
	oidIfOutErrors   = "1.3.6.1.2.1.2.2.1.20"
	oidIfName        = "1.3.6.1.2.1.31.1.1.1.1"
	oidIfHCInOctets  = "1.3.6.1.2.1.31.1.1.1.6"
	oidIfHCOutOctets = "1.3.6.1.2.1.31.1.1.1.10"
	oidIfHighSpeed   = "1.3.6.1.2.1.31.1.1.1.15"

	// 9 OIDs per interface, 5 interfaces per GET. Stays under gosnmp's
	// default MaxOids of 60 so one UDP response fits common agents.
	snmpGetBatch = 45
)

type snmpValue struct {
	text   string
	u      uint64
	ok     bool
	isText bool
}

type snmpQuerier interface {
	Walk(base string) (map[int]snmpValue, error)
	Get(oids []string) (map[string]snmpValue, error)
	Close() error
}

func normalizeOID(oid string) string {
	return strings.TrimPrefix(oid, ".")
}

func indexOf(oid, base string) (int, bool) {
	oid = normalizeOID(oid)
	base = normalizeOID(base)
	prefix := base + "."
	if !strings.HasPrefix(oid, prefix) {
		return 0, false
	}
	rest := oid[len(prefix):]
	if rest == "" || strings.Contains(rest, ".") {
		return 0, false
	}
	n, err := strconv.Atoi(rest)
	if err != nil || n < 0 {
		return 0, false
	}
	return n, true
}

func valueFromPDU(pdu gosnmp.SnmpPDU) snmpValue {
	switch pdu.Type {
	case gosnmp.OctetString:
		switch v := pdu.Value.(type) {
		case []byte:
			return snmpValue{text: strings.Trim(string(v), " \x00"), isText: true, ok: true}
		case string:
			return snmpValue{text: strings.Trim(v, " \x00"), isText: true, ok: true}
		default:
			return snmpValue{text: fmt.Sprint(pdu.Value), isText: true, ok: true}
		}
	default:
		if u, ok := pduUint(pdu.Value); ok {
			return snmpValue{u: u, ok: true}
		}
		return snmpValue{}
	}
}

func pduUint(v any) (uint64, bool) {
	switch n := v.(type) {
	case uint64:
		return n, true
	case uint32:
		return uint64(n), true
	case uint:
		return uint64(n), true
	case int:
		if n < 0 {
			return 0, false
		}
		return uint64(n), true
	case int64:
		if n < 0 {
			return 0, false
		}
		return uint64(n), true
	case int32:
		if n < 0 {
			return 0, false
		}
		return uint64(n), true
	default:
		return 0, false
	}
}

type gosnmpClient struct {
	g *gosnmp.GoSNMP
}

func openGoSNMP(ctx context.Context, addr, community string) (snmpQuerier, error) {
	g := &gosnmp.GoSNMP{
		Target:         addr,
		Port:           161,
		Community:      community,
		Version:        gosnmp.Version2c,
		Timeout:        2 * time.Second,
		Retries:        1,
		MaxRepetitions: 50,
		MaxOids:        snmpGetBatch,
		Context:        ctx,
	}
	if err := g.Connect(); err != nil {
		return nil, err
	}
	return &gosnmpClient{g: g}, nil
}

func (c *gosnmpClient) Close() error {
	if c.g != nil && c.g.Conn != nil {
		return c.g.Conn.Close()
	}
	return nil
}

func (c *gosnmpClient) Walk(base string) (map[int]snmpValue, error) {
	out := map[int]snmpValue{}
	err := c.g.BulkWalk(base, func(pdu gosnmp.SnmpPDU) error {
		if pdu.Type == gosnmp.NoSuchObject || pdu.Type == gosnmp.NoSuchInstance || pdu.Type == gosnmp.EndOfMibView {
			return nil
		}
		idx, ok := indexOf(pdu.Name, base)
		if !ok {
			return nil
		}
		out[idx] = valueFromPDU(pdu)
		return nil
	})
	return out, err
}

func (c *gosnmpClient) Get(oids []string) (map[string]snmpValue, error) {
	pkt, err := c.g.Get(oids)
	if err != nil {
		return nil, err
	}
	if pkt.Error != gosnmp.NoError {
		return nil, fmt.Errorf("snmp get error %d", pkt.Error)
	}
	out := make(map[string]snmpValue, len(pkt.Variables))
	for _, pdu := range pkt.Variables {
		if pdu.Type == gosnmp.NoSuchObject || pdu.Type == gosnmp.NoSuchInstance || pdu.Type == gosnmp.EndOfMibView {
			continue
		}
		out[normalizeOID(pdu.Name)] = valueFromPDU(pdu)
	}
	return out, nil
}

func columnText(cols map[int]snmpValue) map[int]string {
	out := make(map[int]string, len(cols))
	for idx, v := range cols {
		if v.isText {
			out[idx] = v.text
		}
	}
	return out
}

func matchWanted(names map[int]string, wants []ifaceTarget, used map[int]bool) (map[int]ifaceTarget, []ifaceTarget) {
	exact := make(map[string]int, len(names))
	fold := make(map[string]int, len(names))
	for idx, name := range names {
		name = strings.TrimSpace(name)
		if name == "" || used[idx] {
			continue
		}
		if _, ok := exact[name]; !ok {
			exact[name] = idx
		}
		key := strings.ToLower(name)
		if _, ok := fold[key]; !ok {
			fold[key] = idx
		}
	}
	byIndex := make(map[int]ifaceTarget, len(wants))
	var missing []ifaceTarget
	for _, w := range wants {
		if idx, ok := exact[w.Name]; ok && !used[idx] {
			used[idx] = true
			byIndex[idx] = w
			continue
		}
		if idx, ok := fold[strings.ToLower(w.Name)]; ok && !used[idx] {
			used[idx] = true
			byIndex[idx] = w
			continue
		}
		missing = append(missing, w)
	}
	return byIndex, missing
}

var interfaceColumns = []string{
	oidIfAdminStatus,
	oidIfOperStatus,
	oidIfHCInOctets,
	oidIfHCOutOctets,
	oidIfInErrors,
	oidIfOutErrors,
	oidIfInDiscards,
	oidIfOutDiscards,
	oidIfHighSpeed,
}

func getIndexed(q snmpQuerier, bases []string, indexes []int) (map[string]snmpValue, error) {
	oids := make([]string, 0, len(bases)*len(indexes))
	for _, idx := range indexes {
		suffix := strconv.Itoa(idx)
		for _, base := range bases {
			oids = append(oids, base+"."+suffix)
		}
	}
	out := make(map[string]snmpValue, len(oids))
	for i := 0; i < len(oids); i += snmpGetBatch {
		j := i + snmpGetBatch
		if j > len(oids) {
			j = len(oids)
		}
		part, err := q.Get(oids[i:j])
		if err != nil {
			return nil, err
		}
		for k, v := range part {
			out[k] = v
		}
	}
	return out, nil
}

func uintAt(values map[string]snmpValue, base string, idx int) (uint64, bool) {
	v, ok := values[base+"."+strconv.Itoa(idx)]
	if !ok || !v.ok || v.isText {
		return 0, false
	}
	return v.u, true
}

func samplesFrom(values map[string]snmpValue, byIndex map[int]ifaceTarget) map[string]snmpIface {
	out := make(map[string]snmpIface, len(byIndex))
	for idx, iface := range byIndex {
		s := snmpIface{}
		if u, ok := uintAt(values, oidIfAdminStatus, idx); ok {
			s.Admin = int(u)
			s.HasAdmin = true
		}
		if u, ok := uintAt(values, oidIfOperStatus, idx); ok {
			s.Oper = int(u)
			s.HasOper = true
		}
		if u, ok := uintAt(values, oidIfHCInOctets, idx); ok {
			s.InOctets = u
			s.HasIn = true
		} else if u, ok := uintAt(values, oidIfInOctets, idx); ok {
			s.InOctets = u
			s.HasIn = true
		}
		if u, ok := uintAt(values, oidIfHCOutOctets, idx); ok {
			s.OutOctets = u
			s.HasOut = true
		} else if u, ok := uintAt(values, oidIfOutOctets, idx); ok {
			s.OutOctets = u
			s.HasOut = true
		}
		if u, ok := uintAt(values, oidIfInErrors, idx); ok {
			s.InErrors = u
		}
		if u, ok := uintAt(values, oidIfOutErrors, idx); ok {
			s.OutErrors = u
		}
		if u, ok := uintAt(values, oidIfInDiscards, idx); ok {
			s.InDiscards = u
		}
		if u, ok := uintAt(values, oidIfOutDiscards, idx); ok {
			s.OutDiscards = u
		}
		if u, ok := uintAt(values, oidIfHighSpeed, idx); ok && u > 0 {
			s.SpeedMbps = u
			s.HasSpeed = true
		}
		out[iface.Service] = s
	}
	return out
}

// collectSamples maps factum interface names to SNMP rows. It walks
// ifName once (ifDescr only for names ifName did not match), then GETs
// the status and counter columns for the matched ifIndexes. One session
// covers every interface on the device. 32-bit octet counters are read
// only for rows that do not implement ifHCInOctets.
func collectSamples(q snmpQuerier, ifNames map[int]snmpValue, wants []ifaceTarget) (map[string]snmpIface, error) {
	used := map[int]bool{}
	byIndex, missing := matchWanted(columnText(ifNames), wants, used)
	if len(missing) > 0 {
		descr, err := q.Walk(oidIfDescr)
		if err != nil {
			return nil, err
		}
		extra, _ := matchWanted(columnText(descr), missing, used)
		for idx, iface := range extra {
			byIndex[idx] = iface
		}
	}
	if len(byIndex) == 0 {
		return map[string]snmpIface{}, nil
	}
	indexes := make([]int, 0, len(byIndex))
	for idx := range byIndex {
		indexes = append(indexes, idx)
	}
	values, err := getIndexed(q, interfaceColumns, indexes)
	if err != nil {
		return nil, err
	}
	var need32 []int
	for _, idx := range indexes {
		if _, ok := uintAt(values, oidIfHCInOctets, idx); !ok {
			need32 = append(need32, idx)
		}
		if _, ok := uintAt(values, oidIfHCOutOctets, idx); !ok {
			need32 = append(need32, idx)
		}
	}
	if len(need32) > 0 {
		uniq := make([]int, 0, len(need32))
		seen := map[int]bool{}
		for _, idx := range need32 {
			if !seen[idx] {
				seen[idx] = true
				uniq = append(uniq, idx)
			}
		}
		legacy, err := getIndexed(q, []string{oidIfInOctets, oidIfOutOctets}, uniq)
		if err != nil {
			return nil, err
		}
		for k, v := range legacy {
			values[k] = v
		}
	}
	return samplesFrom(values, byIndex), nil
}
