package install

import (
	"fmt"
	"regexp"

	"github.com/hok/agentawake/internal/pmset"
)

// SudoersPath is where the passwordless rule is installed.
const SudoersPath = "/etc/sudoers.d/agentawake"

// validUsername matches a conservative POSIX username: letters, digits,
// underscore, hyphen; must start with a letter or underscore.
var validUsername = regexp.MustCompile(`^[a-z_][a-z0-9_-]*$`)

// renderSudoers builds the sudoers line. Callers must pass a validated username.
func renderSudoers(user string) string {
	return fmt.Sprintf("%s ALL=(root) NOPASSWD: %s\n", user, pmset.WrapperPath)
}

// RenderSudoersChecked validates the username before rendering, to keep
// untrusted input out of the sudoers file.
func RenderSudoersChecked(user string) (string, error) {
	if !validUsername.MatchString(user) {
		return "", fmt.Errorf("refusing to write sudoers rule for unsafe username %q", user)
	}
	return renderSudoers(user), nil
}
