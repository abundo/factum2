package dns

import (
	"bufio"
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/netip"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"
)

const (
	DefaultKea4CtrlSocket = "/run/kea/kea4-ctrl-socket"
	DefaultKea6CtrlSocket = "/run/kea/kea6-ctrl-socket"
)

// DHCPLease is one Kea IPv4 or IPv6 lease with a hardware address.
// Leases without a MAC (typical DUID-only IPv6) are omitted: the zone
// editor picker assigns a MAC on A/AAAA records.
type DHCPLease struct {
	Family   string `json:"family"`
	IP       string `json:"ip"`
	MAC      string `json:"mac"`
	Hostname string `json:"hostname"`
	State    string `json:"state,omitempty"`
	Type     string `json:"type,omitempty"`
}

type KeaSocket struct {
	Family  string // ipv4 or ipv6
	Path    string
	Command string
}

func DefaultKeaSockets() []KeaSocket {
	return []KeaSocket{
		{Family: "ipv4", Path: DefaultKea4CtrlSocket, Command: "lease4-get-all"},
		{Family: "ipv6", Path: DefaultKea6CtrlSocket, Command: "lease6-get-all"},
	}
}

func defaultKeaCSVPaths(family string) []string {
	if family == "ipv6" {
		return []string{"/etc/kea/kea-leases6.csv", "/var/lib/kea/kea-leases6.csv"}
	}
	return []string{"/etc/kea/kea-leases4.csv", "/var/lib/kea/kea-leases4.csv"}
}

// ListKeaLeases queries each Kea control socket. Missing sockets, or a
// daemon without the lease_cmds hook, fall back to the memfile CSV.
// sockets nil uses DefaultKeaSockets.
func ListKeaLeases(ctx context.Context, sockets []KeaSocket) ([]DHCPLease, error) {
	if sockets == nil {
		sockets = DefaultKeaSockets()
	}
	var (
		out   []DHCPLease
		errs  []error
		tried int
	)
	for _, sock := range sockets {
		family := sock.Family
		if family == "" {
			family = "ipv4"
		}
		got, used, err := leasesFromSocket(ctx, sock)
		if used {
			tried++
			if err != nil {
				csvGot, csvErr := leasesFromCSV(family, defaultKeaCSVPaths(family))
				if csvErr == nil {
					out = append(out, csvGot...)
					continue
				}
				errs = append(errs, fmt.Errorf("%s: %w", sock.Path, err))
				continue
			}
			out = append(out, got...)
			continue
		}
		csvGot, csvErr := leasesFromCSV(family, defaultKeaCSVPaths(family))
		if csvErr != nil {
			continue
		}
		tried++
		out = append(out, csvGot...)
	}
	if tried == 0 {
		names := make([]string, 0, len(sockets))
		for _, sock := range sockets {
			if sock.Path != "" {
				names = append(names, sock.Path)
			}
		}
		return nil, fmt.Errorf("Kea control sockets not found (tried %s)", strings.Join(names, ", "))
	}
	if len(out) == 0 && len(errs) > 0 && len(errs) == tried {
		return nil, errs[0]
	}
	sortLeases(out)
	return out, nil
}

func leasesFromSocket(ctx context.Context, sock KeaSocket) ([]DHCPLease, bool, error) {
	if sock.Path == "" {
		return nil, false, nil
	}
	if _, err := os.Stat(sock.Path); err != nil {
		return nil, false, nil
	}
	got, err := queryKeaLeases(ctx, sock)
	return got, true, err
}

type keaCommand struct {
	Command string `json:"command"`
}

type keaResponse struct {
	Result    int             `json:"result"`
	Text      string          `json:"text"`
	Arguments json.RawMessage `json:"arguments"`
}

type keaLeaseArgs struct {
	Leases []keaLease `json:"leases"`
}

type keaLease struct {
	IP       string `json:"ip-address"`
	HW       string `json:"hw-address"`
	Hostname string `json:"hostname"`
	State    int    `json:"state"`
	Type     string `json:"type"`
}

func queryKeaLeases(ctx context.Context, sock KeaSocket) ([]DHCPLease, error) {
	cmd := sock.Command
	if cmd == "" {
		if sock.Family == "ipv6" {
			cmd = "lease6-get-all"
		} else {
			cmd = "lease4-get-all"
		}
	}
	raw, err := keaUnixCommand(ctx, sock.Path, keaCommand{Command: cmd})
	if err != nil {
		return nil, err
	}
	var resp keaResponse
	if err := json.Unmarshal(raw, &resp); err != nil {
		return nil, fmt.Errorf("decode Kea response: %w", err)
	}
	// 0 = success, 3 = empty (no leases). Other codes include
	// "command not supported" when lease_cmds is not loaded.
	if resp.Result != 0 && resp.Result != 3 {
		msg := strings.TrimSpace(resp.Text)
		if msg == "" {
			msg = fmt.Sprintf("Kea result %d", resp.Result)
		}
		return nil, fmt.Errorf("%s", msg)
	}
	if resp.Result == 3 || len(resp.Arguments) == 0 {
		return nil, nil
	}
	var args keaLeaseArgs
	if err := json.Unmarshal(resp.Arguments, &args); err != nil {
		return nil, fmt.Errorf("decode Kea leases: %w", err)
	}
	family := sock.Family
	if family == "" {
		family = "ipv4"
	}
	out := make([]DHCPLease, 0, len(args.Leases))
	for _, l := range args.Leases {
		if strings.EqualFold(l.Type, "IA_PD") {
			continue
		}
		mac, err := NormalizeMAC(l.HW)
		if err != nil {
			continue
		}
		out = append(out, DHCPLease{
			Family:   family,
			IP:       strings.TrimSpace(l.IP),
			MAC:      mac,
			Hostname: strings.TrimSpace(l.Hostname),
			State:    keaLeaseState(l.State),
			Type:     l.Type,
		})
	}
	return out, nil
}

func keaUnixCommand(ctx context.Context, path string, cmd keaCommand) ([]byte, error) {
	d := net.Dialer{Timeout: 3 * time.Second}
	conn, err := d.DialContext(ctx, "unix", path)
	if err != nil {
		return nil, err
	}
	defer conn.Close()
	deadline, ok := ctx.Deadline()
	if !ok {
		deadline = time.Now().Add(10 * time.Second)
	}
	if err := conn.SetDeadline(deadline); err != nil {
		return nil, err
	}
	if err := json.NewEncoder(conn).Encode(cmd); err != nil {
		return nil, fmt.Errorf("write Kea command: %w", err)
	}
	if uc, ok := conn.(*net.UnixConn); ok {
		_ = uc.CloseWrite()
	}
	dec := json.NewDecoder(conn)
	var raw json.RawMessage
	if err := dec.Decode(&raw); err != nil {
		return nil, fmt.Errorf("read Kea response: %w", err)
	}
	return raw, nil
}

func leasesFromCSV(family string, paths []string) ([]DHCPLease, error) {
	var lastErr error
	for _, path := range paths {
		if path == "" {
			continue
		}
		f, err := os.Open(path)
		if err != nil {
			lastErr = err
			continue
		}
		leases, err := parseKeaLeaseCSV(family, f)
		f.Close()
		if err != nil {
			lastErr = err
			continue
		}
		return leases, nil
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("no Kea lease CSV")
	}
	return nil, lastErr
}

func parseKeaLeaseCSV(family string, r io.Reader) ([]DHCPLease, error) {
	cr := csv.NewReader(bufio.NewReader(r))
	cr.TrimLeadingSpace = true
	header, err := cr.Read()
	if err != nil {
		return nil, err
	}
	col := map[string]int{}
	for i, name := range header {
		col[strings.TrimSpace(strings.ToLower(name))] = i
	}
	idx := func(names ...string) int {
		for _, n := range names {
			if i, ok := col[n]; ok {
				return i
			}
		}
		return -1
	}
	iAddr := idx("address", "ip-address")
	iHW := idx("hwaddr", "hw-address")
	iHost := idx("hostname")
	iState := idx("state")
	iExpire := idx("expire")
	iType := idx("lease_type", "type")
	if iAddr < 0 {
		return nil, fmt.Errorf("Kea lease CSV missing address column")
	}
	now := time.Now().Unix()
	latest := map[string]DHCPLease{}
	for {
		rec, err := cr.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		ip := csvField(rec, iAddr)
		if ip == "" {
			continue
		}
		if iExpire >= 0 {
			if exp, err := strconv.ParseInt(csvField(rec, iExpire), 10, 64); err == nil && exp > 0 && exp < now {
				delete(latest, ip)
				continue
			}
		}
		typ := csvField(rec, iType)
		if typ == "2" || strings.EqualFold(typ, "IA_PD") {
			continue
		}
		mac, err := NormalizeMAC(csvField(rec, iHW))
		if err != nil {
			continue
		}
		state := 0
		if iState >= 0 {
			state, _ = strconv.Atoi(csvField(rec, iState))
		}
		latest[ip] = DHCPLease{
			Family:   family,
			IP:       ip,
			MAC:      mac,
			Hostname: csvField(rec, iHost),
			State:    keaLeaseState(state),
			Type:     typ,
		}
	}
	out := make([]DHCPLease, 0, len(latest))
	for _, l := range latest {
		out = append(out, l)
	}
	return out, nil
}

func csvField(rec []string, i int) string {
	if i < 0 || i >= len(rec) {
		return ""
	}
	return strings.TrimSpace(rec[i])
}

func keaLeaseState(state int) string {
	switch state {
	case 0:
		return "active"
	case 1:
		return "declined"
	case 2:
		return "expired"
	case 3:
		return "released"
	default:
		return fmt.Sprintf("%d", state)
	}
}

func sortLeases(leases []DHCPLease) {
	sort.Slice(leases, func(i, j int) bool {
		a, b := leases[i], leases[j]
		if a.Family != b.Family {
			return a.Family < b.Family
		}
		ai, aErr := netip.ParseAddr(a.IP)
		bi, bErr := netip.ParseAddr(b.IP)
		if aErr == nil && bErr == nil && ai != bi {
			return ai.Compare(bi) < 0
		}
		if a.IP != b.IP {
			return a.IP < b.IP
		}
		return a.MAC < b.MAC
	})
}
