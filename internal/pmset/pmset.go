// Package pmset reads and changes macOS power-management sleep state.
// All writes go through a single privileged wrapper at WrapperPath.
package pmset

import (
	"os"
	"os/exec"
	"regexp"
)

// WrapperPath is the privileged sleep-toggle wrapper installed by `agentawake install`.
const WrapperPath = "/usr/local/sbin/agentawake-pmset"

var sleepDisabledRe = regexp.MustCompile(`(?m)^\s*SleepDisabled\s+(\d)`)

// parseDisabled extracts the SleepDisabled value from `pmset -g` output.
// A missing field is treated as off.
func parseDisabled(out []byte) bool {
	m := sleepDisabledRe.FindSubmatch(out)
	if m == nil {
		return false
	}
	return string(m[1]) == "1"
}

// IsDisabled reports whether pmset disablesleep is currently on.
func IsDisabled() (bool, error) {
	out, err := exec.Command("pmset", "-g").Output()
	if err != nil {
		return false, err
	}
	return parseDisabled(out), nil
}

// Set toggles disablesleep via the privileged wrapper. `sudo -n` is used so a
// missing sudoers rule fails fast instead of prompting.
func Set(on bool) error {
	arg := "0"
	if on {
		arg = "1"
	}
	return exec.Command("sudo", "-n", WrapperPath, arg).Run()
}

// WrapperInstalled reports whether the privileged wrapper exists as a file.
func WrapperInstalled() bool {
	info, err := os.Stat(WrapperPath)
	return err == nil && !info.IsDir()
}
