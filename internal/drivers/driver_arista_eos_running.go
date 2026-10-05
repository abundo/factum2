package drivers

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
)

// CommitRunningContext applies commands inside a configure session, after
// entering enter from configure mode. A rejected command aborts the
// session so nothing is committed. EOS has no "commit comment" on a
// configure session; comment is the session description.
func (driver *AristaDriver) CommitRunningContext(enter []string, commands []string, comment string) error {
	if len(commands) == 0 {
		return nil
	}
	session, err := newConfigSessionName()
	if err != nil {
		return err
	}
	// A completed session keeps its name. Dropping a leftover of this
	// name is best-effort and separate: the name is new, and a missing
	// session makes "no configure session" fail, which must not abort
	// the apply that follows.
	_, _ = eapiRunCmds(driver.p.Username, driver.p.Password, driver.p.Name,
		[]string{"no configure session " + session}, eapiFormatText)

	full := make([]string, 0, 2+len(enter)+len(commands))
	full = append(full, configSessionOpen(session, comment))
	full = append(full, enter...)
	full = append(full, commands...)
	full = append(full, "commit")

	_, err = eapiRunCmds(driver.p.Username, driver.p.Password, driver.p.Name, full, eapiFormatText)
	if err != nil {
		_, _ = eapiRunCmds(driver.p.Username, driver.p.Password, driver.p.Name,
			[]string{"configure session " + session, "abort"}, eapiFormatText)
		_, _ = eapiRunCmds(driver.p.Username, driver.p.Password, driver.p.Name,
			[]string{"no configure session " + session}, eapiFormatText)
		return err
	}
	_, _ = eapiRunCmds(driver.p.Username, driver.p.Password, driver.p.Name,
		[]string{"no configure session " + session}, eapiFormatText)
	return nil
}

func configSessionOpen(session, comment string) string {
	open := "configure session " + session
	if desc := sanitizeCommitComment(comment); desc != "" {
		open += ` description "` + desc + `"`
	}
	return open
}

func newConfigSessionName() (string, error) {
	var buf [4]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return "", fmt.Errorf("configure session name: %w", err)
	}
	return "factum-cfg-" + hex.EncodeToString(buf[:]), nil
}
