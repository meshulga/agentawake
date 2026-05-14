package cli

import (
	"flag"
	"io"
	"os"
	"time"

	"github.com/hok/agentawake/internal/hookjson"
	"github.com/hok/agentawake/internal/logging"
	"github.com/hok/agentawake/internal/pid"
	"github.com/hok/agentawake/internal/reconcile"
	"github.com/hok/agentawake/internal/state"
	"github.com/hok/agentawake/internal/token"
)

// agentPatterns are the process-name substrings used to find the agent process
// while walking up from the hook's parent pid.
var agentPatterns = []string{"claude", "codex", "node"}

// cmdAcquire is the entrypoint for `agentawake acquire`, invoked by the
// UserPromptSubmit hook. It must always exit 0 — a hook must never break the agent.
func cmdAcquire(args []string) int {
	st, _, err := stores()
	if err != nil {
		return 0
	}
	return runAcquire(args, os.Stdin, st)
}

// runAcquire is the testable core: args, a stdin reader, and a Store.
func runAcquire(args []string, stdin io.Reader, st *state.Store) int {
	fs := flag.NewFlagSet("acquire", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	agent := fs.String("agent", "", "claude-code or codex")
	if err := fs.Parse(args); err != nil {
		return 0
	}

	log := logging.New(st.LogPath())

	p, err := hookjson.Parse(stdin)
	if err != nil {
		log.Printf("acquire: parse stdin: %v", err)
		return 0
	}
	if p.SessionID == "" {
		log.Printf("acquire: no session id in hook payload")
		return 0
	}

	detected := pid.Detect(p.PID, os.Getppid(), agentPatterns, pid.SystemTable())
	tok := token.Token{
		Agent:     token.Agent(*agent),
		PID:       detected,
		SessionID: p.SessionID,
		StartedAt: time.Now().UTC(),
	}

	unlock, err := st.Lock()
	if err != nil {
		log.Printf("acquire: lock: %v", err)
		return 0
	}
	defer unlock()

	if err := st.WriteToken(tok); err != nil {
		log.Printf("acquire: write token: %v", err)
		return 0
	}
	if err := reconcile.RunLocked(st, log); err != nil {
		log.Printf("acquire: reconcile: %v", err)
	}
	return 0
}
