package drivers

import (
	"context"
	"fmt"
)

// CommitRunningContext applies commands inside an IOS-XR candidate
// session. enter is the mode path from configure root, the same shape
// EOS uses ("router bgp 1234", then "address-family ipv4 unicast").
// A rejected command is reported and the candidate is aborted, so a
// failed commit does not stay pending. IOS-XR has no JSON form of this
// edit; the commands are classic CLI.
func (driver *IOSXRDriver) CommitRunningContext(enter []string, commands []string, comment string) error {
	if len(commands) == 0 {
		return nil
	}
	full := iosxrCommitCmds(enter, commands, comment)
	output, err := sshRunCLIPipeline(context.Background(), driver.p, full, nil)
	if err != nil {
		return err
	}
	if msg := iosxrFindCLIError(output); msg != "" {
		return fmt.Errorf("ios-xr configuration failed: %s", msg)
	}
	return nil
}

func iosxrCommitCmds(enter, commands []string, comment string) []string {
	full := make([]string, 0, 3+len(enter)+len(commands))
	full = append(full, "terminal length 0", "configure")
	full = append(full, enter...)
	full = append(full, commands...)
	full = append(full, CommitCLI(comment, false), "abort")
	return full
}
