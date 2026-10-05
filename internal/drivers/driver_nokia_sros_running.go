package drivers

import (
	"context"
	"fmt"
)

// CommitRunningContext applies commands inside an MD-CLI exclusive
// candidate. enter is one absolute path ("/configure port 1/1/1"), and
// commands are the MD-CLI diff of that context (delete, or a new line).
// commit is quoted. discard and exit all run afterwards so quit-config
// is not asked to drop a nested context or a failed candidate.
func (driver *NokiaDriver) CommitRunningContext(enter []string, commands []string, comment string) error {
	if len(commands) == 0 {
		return nil
	}
	full := srosCommitCmds(enter, commands, comment)
	output, err := sshRunCLIPipeline(context.Background(), driver.p, full, nil)
	if err != nil {
		return err
	}
	if msg := srosFindCLIError(output); msg != "" {
		return fmt.Errorf("sros-md configuration failed: %s", msg)
	}
	return nil
}

func srosCommitCmds(enter, commands []string, comment string) []string {
	full := make([]string, 0, 6+len(enter)+len(commands))
	// The leading "//" disables paging in the classic engine. The next
	// line starts with "/" (edit-config does not, so enter must) — the
	// caller passes an absolute "/configure ..." path, which switches
	// the session back to MD-CLI before the edit.
	full = append(full, "//environment no more", "edit-config exclusive")
	full = append(full, enter...)
	full = append(full, commands...)
	full = append(full, CommitCLI(comment, true), "discard", "exit all", "quit-config")
	return full
}
