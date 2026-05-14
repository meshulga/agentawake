package cli

import "github.com/hok/agentawake/internal/reconcile"

// cmdReconcile is the entrypoint for `agentawake reconcile`, invoked every
// ~60s by the launchd agent and internally is not needed (acquire/release
// reconcile under their own lock). Exits 0 even on failure — it is a hook-like
// background command.
func cmdReconcile(args []string) int {
	st, log, err := stores()
	if err != nil {
		return 0
	}
	if err := reconcile.Run(st, log); err != nil {
		log.Printf("reconcile command: %v", err)
	}
	return 0
}
