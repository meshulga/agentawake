// Package install performs the privileged and user-level setup that wires
// agentawake into the system: the pmset wrapper, the sudoers rule, hook
// entries in the agent config files, and the launchd safety-net agent.
package install

import _ "embed"

// WrapperScript is the privileged sleep-toggle script, embedded at build time
// and written to disk by the root-setup step. The embed path is relative to
// this package's directory.
//
//go:embed assets/agentawake-pmset.sh
var WrapperScript []byte
