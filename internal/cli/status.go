package cli

import (
	"fmt"
	"io"
	"os"
	"time"

	"github.com/hok/agentawake/internal/pmset"
	"github.com/hok/agentawake/internal/state"
)

// statusFacts are the system observations status needs, gathered separately
// so renderStatus stays testable without a real pmset/wrapper.
type statusFacts struct {
	wrapperInstalled bool
	sleepDisabled    bool
}

// cmdStatus is the entrypoint for `agentawake status`.
func cmdStatus(args []string) int {
	st, _, err := stores()
	if err != nil {
		fmt.Fprintln(os.Stderr, "agentawake: cannot resolve state dir:", err)
		return 1
	}
	disabled, _ := pmset.IsDisabled()
	facts := statusFacts{
		wrapperInstalled: pmset.WrapperInstalled(),
		sleepDisabled:    disabled,
	}
	return renderStatus(st, os.Stdout, facts)
}

// renderStatus writes the human-readable status report and returns the exit code.
func renderStatus(st *state.Store, w io.Writer, facts statusFacts) int {
	if !facts.wrapperInstalled {
		fmt.Fprintln(w, "agentawake: not installed — run `agentawake install`")
		return 1
	}

	tokens, err := st.ListTokens()
	if err != nil {
		fmt.Fprintln(w, "agentawake: cannot read sessions:", err)
		return 1
	}

	fmt.Fprintf(w, "active: %d session(s)\n", len(tokens))
	now := time.Now()
	for _, tk := range tokens {
		dur := now.Sub(tk.StartedAt).Round(time.Second)
		fmt.Fprintf(w, "  %-12s pid %-7d running %s\n", tk.Agent, tk.PID, dur)
	}

	sleepState := "OFF"
	if facts.sleepDisabled {
		sleepState = "ON"
	}
	owned := ""
	if st.HasFlag() {
		owned = " (set by agentawake)"
	}
	fmt.Fprintf(w, "disablesleep: %s%s\n", sleepState, owned)

	// Stuck-on: sleep disabled, but no sessions and no flag recording it.
	if facts.sleepDisabled && len(tokens) == 0 && !st.HasFlag() {
		fmt.Fprintln(w, "")
		fmt.Fprintln(w, "WARNING: disablesleep is ON but agentawake has no active sessions")
		fmt.Fprintln(w, "and no record of enabling it. If this is unexpected, run: agentawake off")
		return 2
	}
	return 0
}
