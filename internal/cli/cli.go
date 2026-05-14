// Package cli dispatches command-line arguments to subcommands.
package cli

import (
	"fmt"
	"os"

	"github.com/hok/agentawake/internal/logging"
	"github.com/hok/agentawake/internal/state"
)

// Version is overridden at build time via -ldflags.
var Version = "dev"

const usage = `agentawake — keep your Mac awake while an AI agent turn is running

Usage:
  agentawake install            Wire up hooks + privileged sleep toggle (asks for password once)
  agentawake uninstall          Remove everything agentawake installed
  agentawake status             Show active sessions and current sleep state
  agentawake off                Emergency reset: clear state and restore normal sleep
  agentawake reconcile          Re-sync sleep state with active sessions (used by launchd)
  agentawake version            Print version

Hook commands (invoked automatically once installed):
  agentawake acquire --agent <claude-code|codex>
  agentawake release
`

// Main runs the CLI and returns a process exit code.
func Main(args []string) int {
	if len(args) == 0 {
		fmt.Fprint(os.Stderr, usage)
		return 1
	}
	switch args[0] {
	case "version", "--version", "-v":
		fmt.Println(Version)
		return 0
	case "help", "--help", "-h":
		fmt.Print(usage)
		return 0
	case "acquire":
		return cmdAcquire(args[1:])
	case "release":
		return cmdRelease(args[1:])
	case "reconcile":
		return cmdReconcile(args[1:])
	case "status":
		return cmdStatus(args[1:])
	default:
		fmt.Fprintf(os.Stderr, "agentawake: unknown command %q\n\n%s", args[0], usage)
		return 1
	}
}

// stores builds the default Store and Logger. Used by every command.
func stores() (*state.Store, *logging.Logger, error) {
	base, err := state.DefaultBase()
	if err != nil {
		return nil, nil, err
	}
	st := state.New(base)
	return st, logging.New(st.LogPath()), nil
}
