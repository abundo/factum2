package drivers

// Shared NETCONF/OpenConfig plumbing for drivers that manage interface
// state/config via NETCONF (RFC 6241) against OpenConfig-modeled YANG paths
// - currently Nokia SR OS (driver_nokia_sros.go) and Arista EOS
// (driver_arista_eos.go), both of which expose the same openconfig-
// interfaces model over their respective OpenConfig/NETCONF agents. Session
// handling is nemith.io/netconf, built on golang.org/x/crypto/ssh (the same
// SSH library the CLI fallback below uses) rather than a second, gRPC-based
// client stack the way gNMI would need.

import (
	"bytes"
	"context"
	"encoding/xml"
	"io"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/abundo/netboxtool"

	"golang.org/x/crypto/ssh"

	"nemith.io/netconf"
	"nemith.io/netconf/rpc"
	ncssh "nemith.io/netconf/transport/ssh"
)

// netconfDefaultPort is NETCONF-over-SSH's IANA-assigned default port
// (RFC 6242); sshDefaultPort is for the classic-CLI fallback transport.
const (
	netconfDefaultPort = "830"
	sshDefaultPort     = "22"
)

// sshPasswordAuthMethods offers both "password" and "keyboard-interactive"
// SSH auth, answering every keyboard-interactive question with password -
// some platforms' SSH/NETCONF servers (observed on Arista EOS, both the
// classic-CLI port 22 and the NETCONF port 830) only advertise
// keyboard-interactive, not password, and ssh.Password alone fails the
// handshake outright ("no supported methods remain") without ever trying
// the credential.
func sshPasswordAuthMethods(password string) []ssh.AuthMethod {
	return []ssh.AuthMethod{
		ssh.Password(password),
		ssh.KeyboardInteractive(func(name, instruction string, questions []string, echos []bool) ([]string, error) {
			answers := make([]string, len(questions))
			for i := range answers {
				answers[i] = password
			}
			return answers, nil
		}),
	}
}

// sshClientConfig builds the ssh.ClientConfig shared by netconfDial and
// sshRunCLI. KeyExchanges appends two legacy algorithms (implemented by
// x/crypto/ssh but excluded from its default list for being weak - SHA-1-
// based, or, for group1, a 1024-bit MODP group) after the library's own
// defaults, so a modern algorithm is always negotiated when the peer offers
// one, and these only kick in for old network gear (observed: a Nokia SR OS
// device offering only diffie-hellman-group-exchange-sha1/group1-sha1) that
// speaks nothing newer.
func sshClientConfig(username, password string) *ssh.ClientConfig {
	config := &ssh.ClientConfig{
		User:            username,
		Auth:            sshPasswordAuthMethods(password),
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         10 * time.Second,
	}
	config.KeyExchanges = append(ssh.SupportedAlgorithms().KeyExchanges,
		ssh.InsecureKeyExchangeDHGEXSHA1, ssh.InsecureKeyExchangeDH1SHA1)
	return config
}

// netconfDial opens a fresh SSH connection and NETCONF session against
// host:port (port defaults to netconfDefaultPort if empty), authenticating
// with username/password as SSH auth - there's no separate NETCONF-layer
// auth step the way gNMI needed username/password gRPC metadata.
func netconfDial(ctx context.Context, username, password, host, port string) (*netconf.Session, error) {
	if port == "" {
		port = netconfDefaultPort
	}
	config := sshClientConfig(username, password)
	transport, err := ncssh.Dial(ctx, "tcp", host+":"+port, config)
	if err != nil {
		return nil, err
	}
	session, err := netconf.NewSession(transport)
	if err != nil {
		transport.Close()
		return nil, err
	}
	return session, nil
}

// netconfGet issues a NETCONF <get> with filter (marshaled to XML as a
// subtree filter) and returns the raw XML of the matched <data>.
func netconfGet(username, password, host, port string, filter any) ([]byte, error) {
	filterXML, err := xml.Marshal(filter)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	session, err := netconfDial(ctx, username, password, host, port)
	if err != nil {
		return nil, err
	}
	defer session.Close(context.Background())
	return (&rpc.Get{Filter: rpc.SubtreeFilter(filterXML)}).Exec(ctx, session)
}

// netconfEditConfig issues a NETCONF <edit-config> against the running
// datastore with config (marshaled to XML) as the config payload.
func netconfEditConfig(username, password, host, port string, config any) error {
	configXML, err := xml.Marshal(config)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	session, err := netconfDial(ctx, username, password, host, port)
	if err != nil {
		return err
	}
	defer session.Close(context.Background())
	return rpc.EditConfig{Target: rpc.Running, Config: configXML}.Exec(ctx, session)
}

// netconfEditConfigCandidate issues a NETCONF <edit-config> against the
// candidate datastore and commits it - unlike netconfEditConfig's direct
// running-datastore edit (what Nokia SR OS and Arista EOS both want), IOS-XR
// requires the candidate+lock+commit workflow (RFC 6241 §8.3/§8.5): edits
// land in candidate first and have no effect until committed. Best-effort
// unlock/discard on the way out isn't itself error-checked - the session is
// closed immediately after regardless, which releases the lock server-side
// even if an explicit unlock/discard RPC failed to round-trip.
func netconfEditConfigCandidate(username, password, host, port string, config any) error {
	configXML, err := xml.Marshal(config)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	session, err := netconfDial(ctx, username, password, host, port)
	if err != nil {
		return err
	}
	defer session.Close(context.Background())

	if err := (rpc.Lock{Target: rpc.Candidate}).Exec(ctx, session); err != nil {
		return err
	}
	defer (rpc.Unlock{Target: rpc.Candidate}).Exec(ctx, session)

	if err := (rpc.EditConfig{Target: rpc.Candidate, Config: configXML}).Exec(ctx, session); err != nil {
		_ = (rpc.DiscardChanges{}).Exec(ctx, session)
		return err
	}
	if err := (rpc.Commit{}).Exec(ctx, session); err != nil {
		_ = (rpc.DiscardChanges{}).Exec(ctx, session)
		return err
	}
	return nil
}

// ----------------------------------------------------------------------
// SSH / classic CLI fallback transport
//
// NETCONF has no RPC to run an arbitrary CLI command, nor one that returns
// config/version the way an operator would read it (as CLI text/JSON) - a
// <get> returns the OpenConfig YANG tree as XML instead. Drivers that need
// that (Exec, RunningConfigGet, Version where the platform's OpenConfig
// agent doesn't cover it) shell out over SSH and run the actual CLI command
// a human would type.
// ----------------------------------------------------------------------

// hasConfigEndMarker reports whether s contains a line, matching endMarker,
// that signals a full config dump has finished (right before the prompt
// reappears) - see sshRunCLI's endMarker parameter. Checked against the raw
// line (only the CRLF's "\r" stripped) rather than a trimmed one, so a
// marker regexp can itself require zero indentation, e.g. SR OS's "}" -
// otherwise it would match every nested object's closing brace throughout
// the JSON, long before the dump actually finishes.
func hasConfigEndMarker(s string, endMarker *regexp.Regexp) bool {
	if endMarker == nil {
		return false
	}
	for _, line := range strings.Split(s, "\n") {
		if endMarker.MatchString(strings.TrimSuffix(line, "\r")) {
			return true
		}
	}
	return false
}

// sshCmd is one command in an sshRunCLI batch, plus that command's own
// end-of-output marker (see EndMarker below) - each command in a batch gets
// its own fast-path regexp rather than one shared across the whole batch,
// since only the specific command that dumps a full config actually ends
// with a recognizable marker line (VRP's "display current-config", IOS-XR's
// "show running-config", SR OS's "//admin display-config"/"show
// configuration json"); a paging-disable command, an arbitrary caller
// command (Exec), or a config-mode command list (SetInterfaceDescriptions)
// has no such guarantee, so those leave EndMarker nil and just pay
// idleWindow.
type sshCmd struct {
	Cmd string
	// EndMarker is an optional, caller-supplied fast path for detecting this
	// specific command's output has finished: a per-platform regexp matching
	// the line a full config dump ends with (VRP's "^return$", IOS-XR's
	// "^end$", SR OS's "^}$"), letting a slow command complete as soon as it
	// actually finishes instead of always waiting out idleWindow below. Nil
	// for commands with no such recognizable terminator.
	EndMarker *regexp.Regexp
}

// sshRunCLI opens an interactive SSH shell against host:port, runs cmds in
// order and returns the output produced by the last command - see
// sshRunCLIBatch for the underlying implementation and the per-command
// variant config-apply callers need.
func sshRunCLI(username, password, host, port string, cmds []sshCmd) (string, error) {
	outputs, err := sshRunCLIBatch(username, password, host, port, cmds)
	if err != nil {
		return "", err
	}
	if len(outputs) == 0 {
		return "", nil
	}
	return outputs[len(outputs)-1], nil
}

// idleWindow is the fallback safety margin for a wait with no recognizable
// end-of-output marker (e.g. VRP's config-mode commands while running
// SetInterfaceDescriptions, or Nokia's MD-CLI JSON dumps). It used to be 1
// second, which was too tight for a full config dump like VRP's "display
// current-config": on a large config the device can pause mid-render for
// just over a second before producing the interface/VLAN sections, which
// got misread as command completion and silently truncated the capture
// before any interface ever appeared in it (see configEndMarkerIdle below
// for the fast path that avoids paying this full window on output that has
// a recognizable end marker).
const idleWindow = 5 * time.Second

// configEndMarkerIdle lets a full config dump (or any other marked output)
// complete as soon as it actually finishes instead of always waiting out
// idleWindow - see hasConfigEndMarker.
const configEndMarkerIdle = 200 * time.Millisecond

const overallTimeout = 30 * time.Second

// markerTailBytes only needs to cover one short marker line (plus whatever
// partial line precedes it) - bounding the check to this tail avoids
// re-copying and re-scanning a large config dump's whole buffer on every
// 100ms poll in waitIdle.
const markerTailBytes = 256

// sshShellSession is an interactive SSH shell with idle-based output
// capture, shared by sshRunCLIBatch (wait once per command) and
// sshRunCLIPipeline (write every command in one go, wait once for the
// whole thing).
type sshShellSession struct {
	client  *ssh.Client
	session *ssh.Session
	stdin   io.WriteCloser

	mu           sync.Mutex
	buf          bytes.Buffer
	lastActivity time.Time
}

func dialSSHShell(username, password, host, port string) (*sshShellSession, error) {
	if port == "" {
		port = sshDefaultPort
	}
	config := sshClientConfig(username, password)
	client, err := ssh.Dial("tcp", host+":"+port, config)
	if err != nil {
		return nil, err
	}

	session, err := client.NewSession()
	if err != nil {
		client.Close()
		return nil, err
	}

	modes := ssh.TerminalModes{
		ssh.ECHO:          0,
		ssh.TTY_OP_ISPEED: 115200,
		ssh.TTY_OP_OSPEED: 115200,
	}
	if err := session.RequestPty("vt100", 50, 200, modes); err != nil {
		session.Close()
		client.Close()
		return nil, err
	}

	stdin, err := session.StdinPipe()
	if err != nil {
		session.Close()
		client.Close()
		return nil, err
	}
	stdout, err := session.StdoutPipe()
	if err != nil {
		session.Close()
		client.Close()
		return nil, err
	}
	if err := session.Shell(); err != nil {
		session.Close()
		client.Close()
		return nil, err
	}

	s := &sshShellSession{client: client, session: session, stdin: stdin, lastActivity: time.Now()}
	go func() {
		chunk := make([]byte, 4096)
		for {
			n, err := stdout.Read(chunk)
			if n > 0 {
				s.mu.Lock()
				s.buf.Write(chunk[:n])
				s.lastActivity = time.Now()
				s.mu.Unlock()
			}
			if err != nil {
				return
			}
		}
	}()
	return s, nil
}

func (s *sshShellSession) Close() {
	s.session.Close()
	s.client.Close()
}

func (s *sshShellSession) write(line string) error {
	_, err := s.stdin.Write([]byte(line + "\n"))
	return err
}

// waitIdle blocks until output has gone quiet for idleWindow, or until a
// line matching endMarker has appeared (in which case it only waits out
// the much shorter configEndMarkerIdle) - see hasConfigEndMarker.
func (s *sshShellSession) waitIdle(endMarker *regexp.Regexp) {
	deadline := time.Now().Add(overallTimeout)
	for time.Now().Before(deadline) {
		s.mu.Lock()
		idle := time.Since(s.lastActivity)
		tail := s.buf.Bytes()
		if len(tail) > markerTailBytes {
			tail = tail[len(tail)-markerTailBytes:]
		}
		tailStr := string(tail)
		s.mu.Unlock()
		want := idleWindow
		if hasConfigEndMarker(tailStr, endMarker) {
			want = configEndMarkerIdle
		}
		if idle >= want {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
}

// resetCapture clears buf and, crucially, lastActivity - without the
// latter, waitIdle sees the idle time left over from whatever was
// captured before (which is by definition >= idleWindow, that's why it
// just returned) and reports "idle" immediately, before the device has
// sent a single byte of the new output.
func (s *sshShellSession) resetCapture() {
	s.mu.Lock()
	s.buf.Reset()
	s.lastActivity = time.Now()
	s.mu.Unlock()
}

func (s *sshShellSession) capture() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.buf.String()
}

// sshRunCLIBatch is sshRunCLI's underlying implementation, returning every
// command's own captured output instead of just the last one - used by
// config-apply callers that need to detect an error on an early command:
// unlike eAPI's structured per-command JSON-RPC error codes, a CLI session
// has no such signal and keeps accepting subsequent lines after a rejected
// one rather than aborting, so only looking at the final command's output
// (e.g. "commit") can miss an earlier line that silently failed. Every
// other caller only wants the last command's output (typically a "show
// ..." at the end of a paging-disable preamble), hence sshRunCLI staying
// the primary entry point.
//
// This writes one command, waits for it to settle, then writes the next -
// right for a sequence where a later line depends on the device's reaction
// to an earlier one (VRP's "save" followed by a "y" confirmation prompt,
// driver_vrp.go). A batch of independent config-mode lines that don't need
// that back-and-forth should use sshRunCLIPipeline instead, which pays the
// idle wait once for the whole batch rather than once per line.
func sshRunCLIBatch(username, password, host, port string, cmds []sshCmd) ([]string, error) {
	s, err := dialSSHShell(username, password, host, port)
	if err != nil {
		return nil, err
	}
	defer s.Close()

	// drain the login banner/MOTD before sending any command
	s.waitIdle(nil)

	results := make([]string, len(cmds))
	for i, cmd := range cmds {
		s.resetCapture()
		if err := s.write(cmd.Cmd); err != nil {
			return nil, err
		}
		s.waitIdle(cmd.EndMarker)
		results[i] = s.capture()
	}
	// The PTY session emits CRLF line endings, but every caller splits
	// output on "\n" alone - left in, the trailing "\r" ends up glued onto
	// the last regex-captured group of whatever line it's on (e.g. an
	// interface description), silently mismatching the same clean value
	// stored in Netbox on every single sync run.
	for i, r := range results {
		results[i] = strings.ReplaceAll(r, "\r\n", "\n")
	}
	return results, nil
}

// sshRunCLIPipeline writes every line in cmds to the shell in a single
// write - MD-CLI/block-paste style, the way a human pasting a whole config
// block would - and waits once for the combined output to settle, instead
// of sshRunCLIBatch's one-write-one-wait-per-line loop. Right for a batch
// of config-mode lines that don't depend on the device's response to any
// earlier line in the same batch (Nokia's ELINE candidate session,
// driver_nokia_sros_eline.go); wrong for anything that needs to react to
// an intermediate prompt (VRP's "save"/"y" confirmation), which must keep
// using sshRunCLIBatch. endMarker works like sshCmd.EndMarker: nil pays
// the full idleWindow once output goes quiet, a marker matching the last
// line of expected output lets that single wait finish as soon as the
// device is actually done instead of always waiting out idleWindow.
func sshRunCLIPipeline(username, password, host, port string, cmds []string, endMarker *regexp.Regexp) (string, error) {
	s, err := dialSSHShell(username, password, host, port)
	if err != nil {
		return "", err
	}
	defer s.Close()

	// drain the login banner/MOTD before sending anything
	s.waitIdle(nil)

	s.resetCapture()
	if err := s.write(strings.Join(cmds, "\n")); err != nil {
		return "", err
	}
	s.waitIdle(endMarker)
	return strings.ReplaceAll(s.capture(), "\r\n", "\n"), nil
}

// ----------------------------------------------------------------------
// OpenConfig XML shapes (NETCONF filters/payloads and their replies)
//
// Shared across vendors because openconfig-interfaces/-platform are
// standard YANG models, not vendor-specific - the same subtree filter and
// reply shape works against any OpenConfig-compliant NETCONF agent.
// ----------------------------------------------------------------------

// ocComponentsFilter selects state for every component under
// /components/component - the empty State field selects that whole subtree
// (an empty container/leaf in a NETCONF subtree filter means "everything
// below this node", same as leaving a gNMI path at "state" with no further
// elements did).
type ocComponentsFilter struct {
	XMLName   xml.Name `xml:"http://openconfig.net/yang/platform components"`
	Component struct {
		State struct{} `xml:"state"`
	} `xml:"component"`
}

type ocComponentsData struct {
	XMLName    xml.Name `xml:"http://openconfig.net/yang/platform components"`
	Components []struct {
		Name  string `xml:"name"`
		State struct {
			Type            string `xml:"type"`
			PartNo          string `xml:"part-no"`
			SerialNo        string `xml:"serial-no"`
			HardwareVersion string `xml:"hardware-version"`
			SoftwareVersion string `xml:"software-version"`
		} `xml:"state"`
	} `xml:"component"`
}

// ocInterfacesFilter selects state for every interface and every
// subinterface. A subinterface is not a top-level list entry in
// openconfig-interfaces - it lives at /interfaces/interface[name]/
// subinterfaces/subinterface[index] - and RFC 6241 subtree filtering
// excludes any sibling that isn't itself named once *some* sibling
// selection node is present, so leaving <subinterfaces> out of this filter
// is what makes a reply contain parent interfaces only.
type ocInterfacesFilter struct {
	XMLName   xml.Name `xml:"http://openconfig.net/yang/interfaces interfaces"`
	Interface struct {
		// Name must be present (even empty) as the list's key-selection
		// node - Nokia SR OS treats a keyless <interface> as "select all"
		// per RFC 6241's containment-node rules, but Arista EOS's NETCONF
		// agent rejects it outright ("application missing-element:
		// Expected element \"name\" is missing") unless the key leaf
		// itself is present in the filter.
		Name struct{} `xml:"name"`
		// State selects the interface's own state container. Note this is
		// as deep as a named <state> selection node can go here: EOS's
		// NETCONF agent rejects the whole filter ("application
		// unknown-element: An unexpected element \"state\" is present") if
		// one appears under <subinterface> too, even alongside the <index>
		// key leaf. Hence the empty container below rather than the
		// symmetric <subinterface><index/><state/></subinterface>.
		State struct{} `xml:"state"`
		// Subinterfaces is left empty on purpose - an empty container
		// selects everything below it, which is the only spelling all three
		// platforms accept. It pulls in each subinterface's config/ipv4/vlan
		// subtrees alongside the state this actually wants; ocInterfacesData
		// ignores them.
		Subinterfaces struct{} `xml:"subinterfaces"`
	} `xml:"interface"`
}

type ocInterfaceState struct {
	Name        string `xml:"name"`
	Description string `xml:"description"`
	AdminStatus string `xml:"admin-status"`
	OperStatus  string `xml:"oper-status"`
}

type ocSubinterfaceData struct {
	Index string           `xml:"index"`
	State ocInterfaceState `xml:"state"`
}

type ocInterfacesData struct {
	XMLName    xml.Name `xml:"http://openconfig.net/yang/interfaces interfaces"`
	Interfaces []struct {
		Name          string               `xml:"name"`
		State         ocInterfaceState     `xml:"state"`
		Subinterfaces []ocSubinterfaceData `xml:"subinterfaces>subinterface"`
	} `xml:"interface"`
}

// ocEditLeaf marshals either a plain leaf value, or (when Operation is set)
// a NETCONF nc:operation="delete" attribute with no value - used to delete
// a description by removing the leaf rather than setting it to "".
type ocEditLeaf struct {
	Operation string `xml:"urn:ietf:params:xml:ns:netconf:base:1.0 operation,attr,omitempty"`
	Value     string `xml:",chardata"`
}

type ocEditConfig struct {
	Description ocEditLeaf `xml:"description"`
}

type ocEditSubinterface struct {
	Index  string       `xml:"index"`
	Config ocEditConfig `xml:"config"`
}

type ocEditSubinterfaces struct {
	Subinterface []ocEditSubinterface `xml:"subinterface"`
}

// ocEditInterface carries both sub-elements as pointers so each is omitted
// when unused. Config: marshaling it as a zero value would send an empty
// description and wipe the parent interface's own description as a side
// effect of editing one of its subinterfaces. Subinterfaces: it has to be a
// pointer to a wrapper rather than a slice with an `xml:"subinterfaces>
// subinterface,omitempty"` path, because omitempty on such a path suppresses
// only the child elements - an empty <subinterfaces></subinterfaces>
// container is still emitted, which a strict agent has no reason to accept
// in an edit-config.
type ocEditInterface struct {
	Name          string               `xml:"name"`
	Config        *ocEditConfig        `xml:"config,omitempty"`
	Subinterfaces *ocEditSubinterfaces `xml:"subinterfaces,omitempty"`
}

type ocEditInterfaces struct {
	XMLName    xml.Name          `xml:"http://openconfig.net/yang/interfaces interfaces"`
	Interfaces []ocEditInterface `xml:"interface"`
}

// splitSubinterface splits a device-style subinterface name ("Ethernet2.210",
// "GigabitEthernet0/0/0/0.100") into its parent interface and subinterface
// index. Only an all-digit suffix after the last dot counts, since that's
// what openconfig-interfaces' subinterface key holds; anything else is a
// plain interface name that happens to contain a dot.
func splitSubinterface(name string) (parent, index string, ok bool) {
	dot := strings.LastIndex(name, ".")
	if dot <= 0 || dot == len(name)-1 {
		return name, "", false
	}
	index = name[dot+1:]
	if strings.TrimLeft(index, "0123456789") != "" {
		return name, "", false
	}
	return name[:dot], index, true
}

// newOcEditInterfaces builds an ocEditInterfaces edit-config payload setting
// (or, for an empty description, deleting) the description leaf for each
// name/description pair - shared by Arista's and IOS-XR's
// SetInterfaceDescriptions, since both edit the same openconfig-interfaces
// paths (Nokia doesn't: see driver_nokia_sros.go for why its writes go to
// the native nokia-conf model instead).
//
// A subinterface name is routed to the nested
// interface[name]/subinterfaces/subinterface[index]/config path rather than
// being sent as a top-level interface of its own, which no OpenConfig agent
// has an entry for. Edits are grouped by parent interface, so a batch
// touching several subinterfaces of one parent (or a parent and its own
// subinterfaces) produces one <interface> element with that key rather than
// several colliding ones.
func newOcEditInterfaces(names []string, descriptions []string) ocEditInterfaces {
	var edit ocEditInterfaces
	position := make(map[string]int, len(names))
	// entryFor returns a pointer into edit.Interfaces; callers must use it
	// before the next append can reallocate the slice.
	entryFor := func(name string) *ocEditInterface {
		if ix, ok := position[name]; ok {
			return &edit.Interfaces[ix]
		}
		position[name] = len(edit.Interfaces)
		edit.Interfaces = append(edit.Interfaces, ocEditInterface{Name: name})
		return &edit.Interfaces[len(edit.Interfaces)-1]
	}

	for ix, ifname := range names {
		leaf := ocEditLeaf{Value: descriptions[ix]}
		if leaf.Value == "" {
			leaf = ocEditLeaf{Operation: "delete"}
		}
		if parent, index, isSub := splitSubinterface(ifname); isSub {
			entry := entryFor(parent)
			if entry.Subinterfaces == nil {
				entry.Subinterfaces = &ocEditSubinterfaces{}
			}
			entry.Subinterfaces.Subinterface = append(entry.Subinterfaces.Subinterface, ocEditSubinterface{
				Index:  index,
				Config: ocEditConfig{Description: leaf},
			})
			continue
		}
		entryFor(ifname).Config = &ocEditConfig{Description: leaf}
	}
	return edit
}

// interfacesFromOcData converts a decoded ocInterfacesData reply into the
// netboxtool.NBInterface list shape GetInterfacesStatus returns - shared
// since Nokia, Arista and IOS-XR all decode the same openconfig-interfaces
// reply into the same result type. Each parent interface is followed by its
// own subinterfaces, matching the order a device's own CLI lists them in
// (and, on EOS, the order the eAPI fallback produces).
func interfacesFromOcData(reply ocInterfacesData) []*netboxtool.NBInterface {
	var interfaces []*netboxtool.NBInterface
	for _, ifc := range reply.Interfaces {
		name := ifc.State.Name
		if name == "" {
			name = ifc.Name
		}
		if name == "" {
			// EOS pads its reply with empty <interface></interface>
			// elements (18 of them on a 7280SR with 59 real interfaces).
			// Every caller matches interfaces by name, so a nameless entry
			// is nothing but noise to them.
			continue
		}
		interfaces = append(interfaces, &netboxtool.NBInterface{
			Name:               name,
			Description:        ifc.State.Description,
			InterfaceStatus:    ifc.State.AdminStatus,
			LineProtocolStatus: ifc.State.OperStatus,
		})
		interfaces = append(interfaces, subinterfacesFromOcData(name, ifc.Subinterfaces)...)
	}
	return interfaces
}

// subinterfacesFromOcData converts one interface's subinterface list, naming
// each entry the way the device itself does: state/name when the platform
// fills it in (EOS reports "Ethernet2.210" there), otherwise parent.index.
func subinterfacesFromOcData(parent string, subs []ocSubinterfaceData) []*netboxtool.NBInterface {
	var interfaces []*netboxtool.NBInterface
	for _, sub := range subs {
		name := sub.State.Name
		if name == "" {
			if sub.Index == "" || sub.Index == "0" {
				// Index 0 is the parent interface itself on platforms that
				// model the base L3 interface as a subinterface (SR OS).
				// With no state/name saying otherwise, synthesizing
				// "1/1/1.0" would invent a name no other source - NetBox,
				// the CLI, the eAPI fallback - uses for it.
				continue
			}
			name = parent + "." + sub.Index
		}
		if name == parent {
			continue
		}
		interfaces = append(interfaces, &netboxtool.NBInterface{
			Name:               name,
			Description:        sub.State.Description,
			InterfaceStatus:    sub.State.AdminStatus,
			LineProtocolStatus: sub.State.OperStatus,
		})
	}
	return interfaces
}
