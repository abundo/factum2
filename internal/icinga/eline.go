package icinga

// ELINE checks are passive services on the same poll as interfaces.
// There is no single MIB these platforms share:
//
//   - Arista EOS has no pseudowire MIB. Poll reads "show patch panel"
//     over eAPI when an EOS eAPI user is set. Without that account the
//     check stays UNKNOWN, unless the agent answers PW-STD-MIB.
//   - PW-STD-MIB (RFC 5601) pwOperStatus is the standard for everyone else.
//   - IOS-XR xconnects are in CISCO-IETF-PW-MIB (cpwVcOperStatus). Its
//     oper enum is not the RFC 5601 enum: unknown is 4, notPresent is 6,
//     lowerLayerDown is 7.
//   - Nokia epipe oper state is TIMETRA-SERV-MIB svcOperStatus, indexed
//     by the numeric service-id. epipe is svcType 1. up(1) and down(2).
//
// Admin-down stays OK, same as an admin-down interface.

import (
	"fmt"
	"sort"
	"strings"

	"github.com/abundo/factum2/internal/util"
	"github.com/abundo/factum2/models"
)

const (
	oidPwIndexNext = "1.3.6.1.2.1.10.246.1.1"
	oidPwID        = "1.3.6.1.2.1.10.246.1.2.1.12"
	oidPwName      = "1.3.6.1.2.1.10.246.1.2.1.33"
	oidPwAdmin     = "1.3.6.1.2.1.10.246.1.2.1.37"
	oidPwOper      = "1.3.6.1.2.1.10.246.1.2.1.38"

	oidCpwIndexNext = "1.3.6.1.4.1.9.10.106.1.1"
	oidCpwID        = "1.3.6.1.4.1.9.10.106.1.2.1.10"
	oidCpwName      = "1.3.6.1.4.1.9.10.106.1.2.1.21"
	oidCpwAdmin     = "1.3.6.1.4.1.9.10.106.1.2.1.25"
	oidCpwOper      = "1.3.6.1.4.1.9.10.106.1.2.1.26"

	oidSvcNumEntries = "1.3.6.1.4.1.6527.3.1.2.4.2.1"
	oidSvcType       = "1.3.6.1.4.1.6527.3.1.2.4.2.2.1.3"
	oidSvcAdmin      = "1.3.6.1.4.1.6527.3.1.2.4.2.2.1.8"
	oidSvcOper       = "1.3.6.1.4.1.6527.3.1.2.4.2.2.1.9"

	nokiaEpipe = 1
)

const (
	elineVendorEOS   = "eos"
	elineVendorIOSXR = "ios-xr"
	elineVendorSROS  = "sros"
)

type elineTarget struct {
	ServiceID string
	PWID      int
	Service   string
	Local     bool
	// PeerHost is the other end that notifies. Set only on the later
	// host, so a down pseudowire does not notify twice. Empty when this
	// end is the one that notifies, or when there is no other end.
	PeerHost    string
	PeerService string
}

type elineDevice struct {
	Host     string
	Addr     string
	Platform string
	Elines   []elineTarget
}

type elineReading struct {
	Status int
	Output string
}

type pwColumns struct {
	id    string
	name  string
	admin string
	oper  string
	where string
	cisco bool
}

type pwRow struct {
	index    int
	id       uint64
	hasID    bool
	name     string
	admin    int
	hasAdmin bool
	oper     int
	hasOper  bool
}

func elineVendor(platform string) string {
	p := strings.ToLower(strings.TrimSpace(platform))
	p = strings.NewReplacer("_", "-", " ", "-").Replace(p)
	switch {
	case p == "eos" || strings.HasPrefix(p, "eos-") || strings.Contains(p, "arista"):
		return elineVendorEOS
	case strings.Contains(p, "ios-xr") || strings.Contains(p, "iosxr") || strings.Contains(p, "asr9"):
		return elineVendorIOSXR
	case strings.Contains(p, "sros") || strings.Contains(p, "nokia") || strings.Contains(p, "7750") || strings.Contains(p, "7450") || strings.Contains(p, "timos"):
		return elineVendorSROS
	default:
		return ""
	}
}

func elineServiceName(id string) string {
	var b strings.Builder
	b.Grow(len(id) + 6)
	b.WriteString("eline ")
	for _, r := range strings.TrimSpace(id) {
		switch r {
		case '!', ';', '\n', '\r', '|', '"':
			b.WriteByte('_')
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

func elinePrefix(t elineTarget) string {
	if t.PWID > 0 {
		return fmt.Sprintf("%s pw %d", t.ServiceID, t.PWID)
	}
	return t.ServiceID
}

// selectElineTargets keeps applied ELINE endpoints on monitored devices
// that are not in the ignore list. One Icinga service per service id on
// a host, named the same way sync writes the object.
func selectElineTargets(rows []models.IcingaEline, domain, ignore string) []elineDevice {
	type acc struct {
		host     string
		addr     string
		platform string
		byID     map[string]*elineTarget
	}
	order := make([]string, 0)
	byHost := map[string]*acc{}
	for _, row := range rows {
		if !row.Monitor || isIgnoredDevice(row.Device, ignore) {
			continue
		}
		id := strings.TrimSpace(row.ServiceID)
		addr := strings.Split(strings.TrimSpace(row.Address), "/")[0]
		if id == "" || addr == "" || strings.TrimSpace(row.Device) == "" {
			continue
		}
		host := util.FormatName(domain, row.Device)
		a := byHost[host]
		if a == nil {
			a = &acc{host: host, addr: addr, platform: strings.TrimSpace(row.Platform), byID: map[string]*elineTarget{}}
			byHost[host] = a
			order = append(order, host)
		}
		if a.platform == "" {
			a.platform = strings.TrimSpace(row.Platform)
		}
		cur := a.byID[id]
		if cur == nil {
			cur = &elineTarget{ServiceID: id, PWID: row.PWID, Local: row.Local}
			a.byID[id] = cur
			continue
		}
		if cur.PWID == 0 {
			cur.PWID = row.PWID
		}
		cur.Local = cur.Local || row.Local
	}
	out := make([]elineDevice, 0, len(order))
	for _, host := range order {
		a := byHost[host]
		ids := make([]string, 0, len(a.byID))
		for id := range a.byID {
			ids = append(ids, id)
		}
		sort.Strings(ids)
		seen := map[string]int{}
		elines := make([]elineTarget, 0, len(ids))
		for _, id := range ids {
			t := *a.byID[id]
			base := elineServiceName(t.ServiceID)
			service := base
			if n := seen[base]; n > 0 {
				service = fmt.Sprintf("%s #%d", base, n+1)
			}
			seen[base]++
			t.Service = service
			elines = append(elines, t)
		}
		out = append(out, elineDevice{Host: a.host, Addr: a.addr, Platform: a.platform, Elines: elines})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Host < out[j].Host })
	linkElinePeers(out)
	return out
}

// linkElinePeers points every later end of a service at the earliest
// host. That host is the one Icinga notifies. The others are still
// checked, and they notify only when this end is OK or UNKNOWN.
func linkElinePeers(devices []elineDevice) {
	type ref struct {
		dev int
		idx int
	}
	byID := map[string][]ref{}
	for di, d := range devices {
		for ei, e := range d.Elines {
			byID[e.ServiceID] = append(byID[e.ServiceID], ref{di, ei})
		}
	}
	for _, refs := range byID {
		if len(refs) < 2 {
			continue
		}
		sort.Slice(refs, func(i, j int) bool {
			hi, hj := devices[refs[i].dev].Host, devices[refs[j].dev].Host
			if hi != hj {
				return hi < hj
			}
			return devices[refs[i].dev].Elines[refs[i].idx].Service < devices[refs[j].dev].Elines[refs[j].idx].Service
		})
		parent := devices[refs[0].dev].Elines[refs[0].idx]
		parentHost := devices[refs[0].dev].Host
		for _, r := range refs[1:] {
			devices[r.dev].Elines[r.idx].PeerHost = parentHost
			devices[r.dev].Elines[r.idx].PeerService = parent.Service
		}
	}
}

// scalarInstance is the SNMPv2 instance of a scalar. GET of the object
// OID itself is NoSuchInstance even when the MIB is implemented.
func scalarInstance(oid string) string {
	oid = normalizeOID(oid)
	if strings.HasSuffix(oid, ".0") {
		return oid
	}
	return oid + ".0"
}

func scalarPresent(q snmpQuerier, oid string) (bool, error) {
	inst := scalarInstance(oid)
	vals, err := q.Get([]string{inst})
	if err != nil {
		return false, err
	}
	_, ok := vals[inst]
	return ok, nil
}

func walkPW(q snmpQuerier, cols pwColumns) ([]pwRow, error) {
	ids, err := q.Walk(cols.id)
	if err != nil {
		return nil, err
	}
	names, err := q.Walk(cols.name)
	if err != nil {
		return nil, err
	}
	admins, err := q.Walk(cols.admin)
	if err != nil {
		return nil, err
	}
	opers, err := q.Walk(cols.oper)
	if err != nil {
		return nil, err
	}
	idxSet := map[int]struct{}{}
	for _, col := range []map[int]snmpValue{ids, names, admins, opers} {
		for idx := range col {
			idxSet[idx] = struct{}{}
		}
	}
	indexes := make([]int, 0, len(idxSet))
	for idx := range idxSet {
		indexes = append(indexes, idx)
	}
	sort.Ints(indexes)
	rows := make([]pwRow, 0, len(indexes))
	for _, idx := range indexes {
		row := pwRow{index: idx}
		if v, ok := ids[idx]; ok && v.ok && !v.isText {
			row.id = v.u
			row.hasID = true
		}
		if v, ok := names[idx]; ok && v.ok && v.isText {
			row.name = strings.TrimSpace(v.text)
		}
		if v, ok := admins[idx]; ok && v.ok && !v.isText {
			row.admin = int(v.u)
			row.hasAdmin = true
		}
		if v, ok := opers[idx]; ok && v.ok && !v.isText {
			row.oper = int(v.u)
			row.hasOper = true
		}
		rows = append(rows, row)
	}
	return rows, nil
}

func findPW(rows []pwRow, id int, name string) (pwRow, bool) {
	if id > 0 {
		hits := make([]pwRow, 0, 1)
		for _, row := range rows {
			if row.hasID && row.id == uint64(id) {
				hits = append(hits, row)
			}
		}
		if len(hits) == 1 {
			return hits[0], true
		}
		if len(hits) > 1 {
			for _, row := range hits {
				if row.name == name || strings.EqualFold(row.name, name) {
					return row, true
				}
			}
			return hits[0], true
		}
	}
	for _, row := range rows {
		if name != "" && (row.name == name || strings.EqualFold(row.name, name)) {
			return row, true
		}
	}
	return pwRow{}, false
}

// pwOperText is PwOperStatusTC from RFC 5601. It is not ifOperStatus:
// notPresent is 5 and lowerLayerDown is 6.
func pwOperText(oper int) (int, string) {
	switch oper {
	case 1:
		return statusOK, "up"
	case 2:
		return statusCrit, "down"
	case 3:
		return statusOK, "testing"
	case 4:
		return statusWarn, "dormant"
	case 5:
		return statusUnknown, "not present"
	case 6:
		return statusCrit, "lower layer down"
	default:
		return statusUnknown, "unknown oper status"
	}
}

// ciscoOperText is CpwOperStatus. unknown is 4, notPresent is 6, and
// lowerLayerDown is 7.
func ciscoOperText(oper int) (int, string) {
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
		return statusUnknown, "not present"
	case 7:
		return statusCrit, "lower layer down"
	default:
		return statusUnknown, "unknown oper status"
	}
}

func evalAdminOper(admin, oper int, hasAdmin, hasOper bool, operTextFn func(int) (int, string)) (int, string) {
	if hasAdmin && admin != 1 {
		switch admin {
		case 2:
			return statusOK, "admin down"
		case 3:
			return statusOK, "admin testing"
		default:
			return statusUnknown, fmt.Sprintf("admin status %d", admin)
		}
	}
	if !hasOper {
		return statusUnknown, "missing oper status"
	}
	return operTextFn(oper)
}

func readingFrom(t elineTarget, status int, text string) elineReading {
	return elineReading{Status: status, Output: elinePrefix(t) + " " + text}
}

func evalPW(t elineTarget, row pwRow, cisco bool) elineReading {
	fn := pwOperText
	if cisco {
		fn = ciscoOperText
	}
	status, text := evalAdminOper(row.admin, row.oper, row.hasAdmin, row.hasOper, fn)
	return readingFrom(t, status, text)
}

func unsupportedText(vendor string, local bool) string {
	switch vendor {
	case elineVendorEOS:
		return "EOS eAPI user is not set"
	case elineVendorIOSXR:
		return "no SNMP status (IOS-XR did not answer CISCO-IETF-PW-MIB)"
	case elineVendorSROS:
		return "no SNMP status (SR OS did not answer TIMETRA-SERV-MIB)"
	default:
		return "no SNMP status (no PW-STD-MIB, CISCO-IETF-PW-MIB, or TIMETRA-SERV-MIB)"
	}
}

func collectPWTable(q snmpQuerier, cols pwColumns, wants []elineTarget) (map[string]elineReading, error) {
	rows, err := walkPW(q, cols)
	if err != nil {
		return nil, err
	}
	out := make(map[string]elineReading, len(wants))
	for _, want := range wants {
		row, ok := findPW(rows, want.PWID, want.ServiceID)
		if !ok {
			out[want.Service] = readingFrom(want, statusUnknown, "not in "+cols.where)
			continue
		}
		out[want.Service] = evalPW(want, row, cols.cisco)
	}
	return out, nil
}

func collectNokia(q snmpQuerier, wants []elineTarget) (map[string]elineReading, error) {
	out := make(map[string]elineReading, len(wants))
	var indexed []elineTarget
	indexes := make([]int, 0, len(wants))
	seen := map[int]bool{}
	for _, want := range wants {
		if want.PWID <= 0 {
			out[want.Service] = readingFrom(want, statusUnknown, "no service-id")
			continue
		}
		indexed = append(indexed, want)
		if !seen[want.PWID] {
			seen[want.PWID] = true
			indexes = append(indexes, want.PWID)
		}
	}
	if len(indexes) == 0 {
		return out, nil
	}
	values, err := getIndexed(q, []string{oidSvcType, oidSvcAdmin, oidSvcOper}, indexes)
	if err != nil {
		return nil, err
	}
	for _, want := range indexed {
		typ, hasType := uintAt(values, oidSvcType, want.PWID)
		admin, hasAdmin := uintAt(values, oidSvcAdmin, want.PWID)
		oper, hasOper := uintAt(values, oidSvcOper, want.PWID)
		if !hasType && !hasAdmin && !hasOper {
			out[want.Service] = readingFrom(want, statusUnknown, "not in TIMETRA-SERV-MIB")
			continue
		}
		if hasType && typ != nokiaEpipe {
			out[want.Service] = readingFrom(want, statusUnknown, fmt.Sprintf("service-id %d is type %d, not epipe", want.PWID, typ))
			continue
		}
		status, text := evalAdminOper(int(admin), int(oper), hasAdmin, hasOper, func(v int) (int, string) {
			switch v {
			case 1:
				return statusOK, "up"
			case 2:
				return statusCrit, "down"
			default:
				return statusUnknown, "unknown oper status"
			}
		})
		out[want.Service] = readingFrom(want, status, text)
	}
	return out, nil
}

func markUnsupported(wants []elineTarget, vendor string) map[string]elineReading {
	out := make(map[string]elineReading, len(wants))
	for _, want := range wants {
		out[want.Service] = readingFrom(want, statusUnknown, unsupportedText(vendor, want.Local))
	}
	return out
}

// collectElines reads one device's ELINE rows. A missing MIB is UNKNOWN
// on the service, not an SNMP session error. A walk or GET error is
// returned so the caller can count it.
func collectElines(q snmpQuerier, platform string, wants []elineTarget) (map[string]elineReading, error) {
	if len(wants) == 0 {
		return map[string]elineReading{}, nil
	}
	vendor := elineVendor(platform)
	switch vendor {
	case elineVendorEOS:
		ok, err := scalarPresent(q, oidPwIndexNext)
		if err != nil {
			return nil, err
		}
		if !ok {
			return markUnsupported(wants, vendor), nil
		}
		return collectPWTable(q, pwColumns{id: oidPwID, name: oidPwName, admin: oidPwAdmin, oper: oidPwOper, where: "PW-STD-MIB"}, wants)
	case elineVendorIOSXR:
		ok, err := scalarPresent(q, oidCpwIndexNext)
		if err != nil {
			return nil, err
		}
		if !ok {
			return markUnsupported(wants, vendor), nil
		}
		return collectPWTable(q, pwColumns{id: oidCpwID, name: oidCpwName, admin: oidCpwAdmin, oper: oidCpwOper, where: "CISCO-IETF-PW-MIB", cisco: true}, wants)
	case elineVendorSROS:
		ok, err := scalarPresent(q, oidSvcNumEntries)
		if err != nil {
			return nil, err
		}
		if !ok {
			return markUnsupported(wants, vendor), nil
		}
		return collectNokia(q, wants)
	default:
		std, err := scalarPresent(q, oidPwIndexNext)
		if err != nil {
			return nil, err
		}
		if std {
			return collectPWTable(q, pwColumns{id: oidPwID, name: oidPwName, admin: oidPwAdmin, oper: oidPwOper, where: "PW-STD-MIB"}, wants)
		}
		cisco, err := scalarPresent(q, oidCpwIndexNext)
		if err != nil {
			return nil, err
		}
		if cisco {
			return collectPWTable(q, pwColumns{id: oidCpwID, name: oidCpwName, admin: oidCpwAdmin, oper: oidCpwOper, where: "CISCO-IETF-PW-MIB", cisco: true}, wants)
		}
		nokia, err := scalarPresent(q, oidSvcNumEntries)
		if err != nil {
			return nil, err
		}
		if nokia {
			return collectNokia(q, wants)
		}
		return markUnsupported(wants, ""), nil
	}
}
