package drivers

// ApplyELINE/RemoveELINE for Cisco IOS-XR - see README-DRIVERS.md's
// "IOS-XR ELINE" section for how this works and how it compares to the
// other platforms. Cleanup and desired-state CLI both live in
// templates/iosxr_eline.tmpl (the "cleanup" define plus the root apply body).

import (
	_ "embed"
	"fmt"
	"regexp"
	"strings"
)

//go:embed templates/iosxr_eline.tmpl
var iosxrELINETemplate string

// iosxrCLIErrorMarkers matches XR's "% ..." error line prefix (e.g.
// "% Invalid input detected ...", "% Failed to commit one or more
// configuration items ..."). Like SR OS's sshRunCLIPipeline transport, XR's
// classic CLI gives no structured per-command error signal - the session
// keeps accepting lines after a rejected one - so an error has to be found
// by scanning the captured output text rather than a status code.
var iosxrCLIErrorMarkers = regexp.MustCompile(`(?m)^%\s`)

// iosxrFindCLIError returns the first error line found in output (the
// combined capture for a whole sshRunCLIPipeline batch, since XR CLI
// doesn't mark which line within it failed), or "" if none of it looks
// like an XR CLI error.
func iosxrFindCLIError(output string) string {
	loc := iosxrCLIErrorMarkers.FindStringIndex(output)
	if loc == nil {
		return ""
	}
	line := output[loc[0]:]
	if i := strings.IndexByte(line, '\n'); i >= 0 {
		line = line[:i]
	}
	return strings.TrimSpace(line)
}

// iosxrELINECommands builds the full command list for intent - cleanup plus
// the apply body, both from templates/iosxr_eline.tmpl - without touching
// the network, so it can be unit-tested directly
// (driver_iosxr_eline_test.go) the way srosELINECommands is for SR OS.
func iosxrELINECommands(intent *ELINEIntent) ([]string, error) {
	return renderELINETemplate(iosxrELINETemplate, intent)
}

// iosxrELINESession pastes cmds inside a fresh "configure" candidate
// session, commits, and always follows up with an unconditional "abort" -
// see this file's package comment for why.
func (driver *IOSXRDriver) iosxrELINESession(cmds []string) error {
	full := append([]string{"configure"}, cmds...)
	full = append(full, "commit", "abort")

	output, err := sshRunCLIPipeline(driver.p.Username, driver.p.Password, driver.p.Name, "", full, nil)
	if err != nil {
		return err
	}
	if msg := iosxrFindCLIError(output); msg != "" {
		return fmt.Errorf("iosxr eline apply failed: %s", msg)
	}
	return nil
}

// ApplyELINE implements ELINEApplier for Cisco IOS-XR.
func (driver *IOSXRDriver) ApplyELINE(intent *ELINEIntent) error {
	cmds, err := iosxrELINECommands(intent)
	if err != nil {
		return err
	}
	return driver.iosxrELINESession(cmds)
}

// RemoveELINE implements ELINERemover for Cisco IOS-XR: tears down
// removal.Name's xconnect entry plus any subinterfaces still attributed to
// it, without configuring anything new - used when an ELINE endpoint moves
// off this device entirely (see web/handler_service_eline.go's
// ApiServiceElinePush). Renders only the shared "cleanup" define from
// templates/iosxr_eline.tmpl.
func (driver *IOSXRDriver) RemoveELINE(removal *ELINERemoval) error {
	cmds, err := renderELINETemplateDefine(iosxrELINETemplate, "cleanup", removal)
	if err != nil {
		return err
	}
	return driver.iosxrELINESession(cmds)
}
