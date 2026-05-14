package cli

import (
	"fmt"
	"os"

	"github.com/hok/agentawake/internal/pmset"
	"github.com/hok/agentawake/internal/state"
)

// cmdOff is the entrypoint for `agentawake off` — the emergency reset.
// off takes no flags; args is unused and kept only for dispatch-signature
// uniformity with the other cmd* entrypoints.
func cmdOff(args []string) int {
	st, _, err := stores()
	if err != nil {
		fmt.Fprintln(os.Stderr, "agentawake: cannot resolve state dir:", err)
		return 1
	}
	code := runOff(st, pmset.Set)
	if code == 0 {
		fmt.Println("agentawake: state cleared, normal sleep restored")
	}
	return code
}

// runOff clears all tokens and the flag, then forces sleep back on. setSleep is
// injected so tests do not touch the real pmset wrapper.
func runOff(st *state.Store, setSleep func(on bool) error) int {
	unlock, err := st.Lock()
	if err != nil {
		fmt.Fprintln(os.Stderr, "agentawake: lock:", err)
		return 1
	}
	defer unlock()

	tokens, _ := st.ListTokens()
	for _, tk := range tokens {
		_ = st.RemoveToken(tk.SessionID)
	}
	_ = st.ClearFlag()

	if err := setSleep(false); err != nil {
		fmt.Fprintln(os.Stderr, "agentawake: could not restore sleep:", err)
		return 1
	}
	return 0
}
