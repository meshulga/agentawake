package cli

import (
	"io"
	"os"

	"github.com/hok/agentawake/internal/hookjson"
	"github.com/hok/agentawake/internal/logging"
	"github.com/hok/agentawake/internal/reconcile"
	"github.com/hok/agentawake/internal/state"
)

// cmdRelease is the entrypoint for `agentawake release`, invoked by the Stop
// (and any turn-terminal) hook. It must always exit 0.
func cmdRelease(args []string) int {
	st, _, err := stores()
	if err != nil {
		return 0
	}
	return runRelease(os.Stdin, st)
}

// runRelease is the testable core: a stdin reader and a Store.
func runRelease(stdin io.Reader, st *state.Store) int {
	log := logging.New(st.LogPath())

	p, err := hookjson.Parse(stdin)
	if err != nil {
		log.Printf("release: parse stdin: %v", err)
		return 0
	}

	unlock, err := st.Lock()
	if err != nil {
		log.Printf("release: lock: %v", err)
		return 0
	}
	defer unlock()

	if p.SessionID == "" {
		log.Printf("release: no session id in hook payload; relying on prune")
	} else if err := st.RemoveToken(p.SessionID); err != nil {
		log.Printf("release: remove token %s: %v", p.SessionID, err)
	}

	if err := reconcile.RunLocked(st, log); err != nil {
		log.Printf("release: reconcile: %v", err)
	}
	return 0
}
