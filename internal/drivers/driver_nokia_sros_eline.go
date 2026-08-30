package drivers

// ApplyELINE/RemoveELINE for Nokia SR OS - see README-DRIVERS.md's "SR OS
// ELINE" section for how this works, how it differs structurally from EOS,
// and what is/isn't confirmed against real hardware.

import (
	_ "embed"
	"fmt"
	"net/netip"
	"regexp"
	"strings"
)

//go:embed templates/sros_eline.tmpl
var srosELINETemplate string

// srosELINETemplateData adds the fields sros_eline.tmpl needs beyond the
// vendor-neutral ELINEIntent - SDPID is derived fresh by ApplyELINE
// (srosSDPID) rather than carried on ELINEIntent itself, since it's purely
// an SR OS implementation detail no other driver or the web handler that
// builds ELINEIntent has any reason to know about.
type srosELINETemplateData struct {
	*ELINEIntent
	SDPID int
}

// srosCLIErrorMarkers matches known Nokia MD-CLI error line prefixes.
// Unlike eAPI's structured per-command JSON-RPC error codes, the SSH
// transport only gives back raw text - the device keeps accepting
// subsequent lines after a rejected one rather than aborting the session,
// so an error has to be found by scanning output text rather than a status
// code. NOT verified against a real device - see README-DRIVERS.md's
// "SR OS ELINE" section.
var srosCLIErrorMarkers = regexp.MustCompile(`(?im)^\s*(MINOR|MAJOR|ERROR)\s*:`)

// srosFindCLIError returns the first error line found in output (the
// combined capture for a whole sshRunCLIPipeline batch, since MD-CLI
// doesn't mark which line within it failed), or "" if none of it looks
// like an MD-CLI error.
func srosFindCLIError(output string) string {
	loc := srosCLIErrorMarkers.FindStringIndex(output)
	if loc == nil {
		return ""
	}
	line := output[loc[0]:]
	if i := strings.IndexByte(line, '\n'); i >= 0 {
		line = line[:i]
	}
	return strings.TrimSpace(line)
}

// srosSDPID derives the shared MPLS SDP ID this device uses toward
// neighborIP - see README-DRIVERS.md's "SR OS ELINE" section for why it's
// shared rather than per-service, and why the ID is this specific value.
func srosSDPID(neighborIP string) (int, error) {
	addr, err := netip.ParseAddr(neighborIP)
	if err != nil || !addr.Is4() {
		return 0, fmt.Errorf("sros eline: neighbor address %q is not a valid IPv4 address", neighborIP)
	}
	octets := addr.As4()
	last := int(octets[3])
	if last == 0 || last == 255 {
		return 0, fmt.Errorf("sros eline: neighbor address %q has no usable SDP ID (last octet %d)", neighborIP, last)
	}
	return last, nil
}

// srosSDPFarEnd returns the far-end IP address of sdpID within configure
// (the "nokia-conf:configure" subtree, as returned by srosConfig()), or ""
// if no sdp with that ID exists yet. Split out from srosCheckSDPConflict so
// the actual matching logic is unit-testable without a live device
// (driver_nokia_sros_eline_test.go), the way srosTestConfigure's fixture
// already exercises the read-side sdp parsing in srosParseEline.
func srosSDPFarEnd(configure map[string]any, sdpID int) string {
	services := srosMap(configure, "service")
	for _, s := range srosSlice(services, "sdp") {
		sdp, ok := s.(map[string]any)
		if !ok {
			continue
		}
		if srosInt(sdp, "sdp-id") == sdpID {
			return srosStr(srosMap(sdp, "far-end"), "ip-address")
		}
	}
	return ""
}

// srosCheckSDPConflict refuses to let ApplyELINE proceed if sdpID already
// exists on the device with a far-end other than neighborIP. srosSDPID's
// derivation (the neighbor's last IPv4 octet) can legitimately collide
// between two unrelated neighbors - without this check, ApplyELINE would
// silently repoint an existing, possibly-shared SDP's far-end to the wrong
// place, disrupting every other service riding it. Not hypothetical: an
// early manual test against a real device did exactly this before this
// check existed (sdp 127 already carried a real service toward
// 172.27.249.28; the derivation collided with a test neighbor and would
// have repointed it to 172.27.250.127 on commit).
func (driver *NokiaDriver) srosCheckSDPConflict(sdpID int, neighborIP string) error {
	configure, err := driver.srosConfig()
	if err != nil {
		return fmt.Errorf("sros eline: checking sdp %d for conflicts: %w", sdpID, err)
	}
	if existing := srosSDPFarEnd(configure, sdpID); existing != "" && existing != neighborIP {
		return fmt.Errorf("sros eline: sdp %d already exists with far-end %s, refusing to repoint it to %s - derived SDP IDs (srosSDPID) can collide between unrelated neighbors", sdpID, existing, neighborIP)
	}
	return nil
}

// srosELINECommands builds the full command list for intent - cleanup plus
// the apply body, both from templates/sros_eline.tmpl - without touching
// the network, so it can be unit-tested directly
// (driver_nokia_sros_eline_test.go) the way iosxrELINECommands is for XR.
func srosELINECommands(intent *ELINEIntent) ([]string, error) {
	data := srosELINETemplateData{ELINEIntent: intent}
	if intent.Remote != nil {
		sdpID, err := srosSDPID(intent.Remote.NeighborIP)
		if err != nil {
			return nil, err
		}
		data.SDPID = sdpID
	}
	return renderELINETemplate(srosELINETemplate, data)
}

// srosELINESession pastes cmds inside a fresh MD-CLI candidate session,
// commits, and always follows up with "discard"/"exit all"/"quit-config"
// in the same session regardless of outcome - "discard" is a no-op after a
// successful commit (nothing left pending to discard) but what cleanly
// releases the candidate lock after a failed one without risking the
// interactive confirmation prompt a bare "quit-config" with uncommitted
// changes would otherwise trigger (which a non-interactive scripted
// session can't answer). "exit all" is required before "quit-config"
// specifically because cmds navigates into nested contexts (e.g.
// "/configure service epipe ... {") that "discard" clears the *content*
// of but doesn't itself back out of - confirmed against a real device:
// "quit-config" issued from inside a nested context like
// "[/configure service]" fails outright ("'quit-config' not allowed in
// 'service'") rather than implicitly returning to root first.
//
// The whole thing - preamble through quit-config - is one sshRunCLIPipeline
// write with a single idle wait, not one wait per line: see
// README-DRIVERS.md's "SR OS ELINE" section for why that matters for
// ApplyELINE's latency.
func (driver *NokiaDriver) srosELINESession(cmds []string) error {
	full := append([]string{"//environment no more", "edit-config exclusive"}, cmds...)
	full = append(full, "commit", "discard", "exit all", "quit-config")

	output, err := sshRunCLIPipeline(driver.p.Username, driver.p.Password, driver.p.Name, "", full, nil)
	if err != nil {
		return err
	}
	if msg := srosFindCLIError(output); msg != "" {
		return fmt.Errorf("sros eline apply failed: %s", msg)
	}
	return nil
}

// ApplyCLISession implements CLISessionApplier for Nokia SR OS.
func (driver *NokiaDriver) ApplyCLISession(_ string, cmds []string) error {
	return driver.srosELINESession(cmds)
}

// PrepareELINEApply implements ELINEPrepareChecker: same SDP far-end guard
// ApplyELINE runs, for the pack/CLISession path that does not call ApplyELINE.
func (driver *NokiaDriver) PrepareELINEApply(intent *ELINEIntent) error {
	if intent == nil || intent.Remote == nil {
		return nil
	}
	sdpID, err := srosSDPID(intent.Remote.NeighborIP)
	if err != nil {
		return err
	}
	return driver.srosCheckSDPConflict(sdpID, intent.Remote.NeighborIP)
}

// ELINETemplateData returns the value ELINE templates execute against.
// SR OS gets SDPID derived from the remote neighbor; other platforms get intent.
func ELINETemplateData(intent *ELINEIntent, platform string) (any, error) {
	if strings.EqualFold(platform, "sros") || strings.EqualFold(platform, "sros-md") {
		data := srosELINETemplateData{ELINEIntent: intent}
		if intent.Remote != nil {
			sdpID, err := srosSDPID(intent.Remote.NeighborIP)
			if err != nil {
				return nil, err
			}
			data.SDPID = sdpID
		}
		return data, nil
	}
	return intent, nil
}

// ApplyELINE implements ELINEApplier for Nokia SR OS.
func (driver *NokiaDriver) ApplyELINE(intent *ELINEIntent) error {
	if intent.Remote != nil {
		sdpID, err := srosSDPID(intent.Remote.NeighborIP)
		if err != nil {
			return err
		}
		if err := driver.srosCheckSDPConflict(sdpID, intent.Remote.NeighborIP); err != nil {
			return err
		}
	}
	cmds, err := srosELINECommands(intent)
	if err != nil {
		return err
	}
	return driver.srosELINESession(cmds)
}

// RemoveELINE implements ELINERemover for Nokia SR OS: deletes
// removal.Name's epipe (and everything nested under it) without
// configuring anything new. Renders only the shared "cleanup" define from
// templates/sros_eline.tmpl.
func (driver *NokiaDriver) RemoveELINE(removal *ELINERemoval) error {
	cmds, err := renderELINETemplateDefine(srosELINETemplate, "cleanup", removal)
	if err != nil {
		return err
	}
	return driver.srosELINESession(cmds)
}
