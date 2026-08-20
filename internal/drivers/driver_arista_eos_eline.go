package drivers

// ApplyELINE provisions the write side of what eosParseEline
// (driver_arista_eos.go) reads: an mpls-ldp pseudowire (when the ELINE's
// other endpoint is on a different device) plus a patch-panel entry cross-
// connecting this device's subinterface to it - or, for a same-device
// ELINE, directly to the other local subinterface.
//
// Uses EOS's configure session feature so the whole change (removal of any
// stale pseudowire/patch this service previously owned, plus the new
// config) commits atomically - either all of it lands, or the session is
// aborted and nothing changes. Cleanup and desired-state CLI both live in
// templates/eos_eline.tmpl (the "cleanup" define plus the root apply body).

import (
	_ "embed"
)

//go:embed templates/eos_eline.tmpl
var eosELINETemplate string

// eosELINESession runs cmds (which must not include the surrounding
// "configure session"/"commit" lines) inside a fresh, uniquely-named
// configure session for name, shared by ApplyELINE and RemoveELINE.
func (driver *AristaDriver) eosELINESession(name string, cmds []string) error {
	session := "factum-eline-" + name

	// Best-effort pre-cleanup, deliberately its own request rather than the
	// first line of cmds below: EOS keeps a session's name reserved even
	// after it's committed, and re-entering an already-completed session by
	// name is rejected ("Cannot enter session ... (already completed)") -
	// confirmed against a real device. Clearing it first makes every apply
	// of this same name reusable, not just the first one. But on a
	// service's actual first-ever apply, EOS has never heard of the name at
	// all, and "no configure session <name>" then fails too ("Cannot delete
	// non-existent session ...") - also confirmed against a real device.
	// Since eAPI aborts a whole batch on its first failing command, that
	// expected/benign failure must not be allowed to poison the real apply
	// below, hence a separate call whose error is ignored.
	_, _ = eapiRunCmds(driver.p.Username, driver.p.Password, driver.p.Name,
		[]string{"no configure session " + session}, eapiFormatText)

	full := append([]string{"configure session " + session}, cmds...)
	full = append(full, "commit")

	_, err := eapiRunCmds(driver.p.Username, driver.p.Password, driver.p.Name, full, eapiFormatText)
	if err != nil {
		// Best-effort cleanup so a failed apply doesn't leave an open,
		// uncommitted (or unreleased) session behind on the device, which
		// would otherwise make every retry fail the same way.
		_, _ = eapiRunCmds(driver.p.Username, driver.p.Password, driver.p.Name,
			[]string{"configure session " + session, "abort"}, eapiFormatText)
		_, _ = eapiRunCmds(driver.p.Username, driver.p.Password, driver.p.Name,
			[]string{"no configure session " + session}, eapiFormatText)
	}
	return err
}

// ApplyELINE implements ELINEApplier for Arista EOS.
func (driver *AristaDriver) ApplyELINE(intent *ELINEIntent) error {
	// Root template includes the "cleanup" define then the new body - see
	// templates/eos_eline.tmpl.
	cmds, err := renderELINETemplate(eosELINETemplate, intent)
	if err != nil {
		return err
	}
	return driver.eosELINESession(intent.Name, cmds)
}

// RemoveELINE implements ELINERemover for Arista EOS: tears down
// removal.Name's pseudowire/patch plus any subinterfaces still
// attributed to it, without configuring anything new - used when an
// ELINE endpoint moves off this device entirely (see
// web/handler_service_eline.go's ApiServiceElinePush), so the device is
// no longer touched by an ApplyELINE call that could otherwise carry the
// cleanup. Renders only the shared "cleanup" define from
// templates/eos_eline.tmpl.
func (driver *AristaDriver) RemoveELINE(removal *ELINERemoval) error {
	cmds, err := renderELINETemplateDefine(eosELINETemplate, "cleanup", removal)
	if err != nil {
		return err
	}
	return driver.eosELINESession(removal.Name, cmds)
}
