package reconcile

import (
	"time"

	"github.com/hok/agentawake/internal/logging"
	"github.com/hok/agentawake/internal/pid"
	"github.com/hok/agentawake/internal/pmset"
	"github.com/hok/agentawake/internal/state"
)

// DefaultMaxAge bounds how long a single token can live before reconcile
// prunes it regardless of process liveness — the backstop for a missed Stop.
const DefaultMaxAge = 4 * time.Hour

// Sink is the side-effecting surface reconcile needs. The real implementation
// is pmsetSink; tests inject a fake.
type Sink interface {
	IsDisabled() (bool, error)
	Set(on bool) error
}

type pmsetSink struct{}

func (pmsetSink) IsDisabled() (bool, error) { return pmset.IsDisabled() }
func (pmsetSink) Set(on bool) error         { return pmset.Set(on) }

// Run performs a full reconcile cycle, taking the state lock for its entire
// duration (including the pmset call), then releasing it.
func Run(st *state.Store, log *logging.Logger) error {
	unlock, err := st.Lock()
	if err != nil {
		return err
	}
	defer unlock()
	return RunLocked(st, log)
}

// RunLocked is Run without taking the lock — the caller must already hold it.
// acquire/release use this so the token write and the reconcile happen under
// a single lock acquisition.
func RunLocked(st *state.Store, log *logging.Logger) error {
	return runLockedWith(st, log, pmsetSink{})
}

// RunWith is RunLocked-equivalent with an injectable Sink, for tests. It does
// not take the lock (tests are single-threaded).
func RunWith(st *state.Store, log *logging.Logger, sink Sink) error {
	return runLockedWith(st, log, sink)
}

func runLockedWith(st *state.Store, log *logging.Logger, sink Sink) error {
	tokens, err := st.ListTokens()
	if err != nil {
		return err
	}
	sleepDisabled, err := sink.IsDisabled()
	if err != nil {
		log.Printf("reconcile: read sleep state failed: %v", err)
		// Continue with sleepDisabled=false; reconcile is idempotent and the
		// next run corrects any mistake.
	}

	dec := Decide(Inputs{
		Tokens:        tokens,
		FlagPresent:   st.HasFlag(),
		SleepDisabled: sleepDisabled,
		Now:           time.Now(),
		MaxAge:        DefaultMaxAge,
		IsAlive:       pid.IsAlive,
	})

	for _, sid := range dec.Prune {
		if err := st.RemoveToken(sid); err != nil {
			log.Printf("reconcile: prune %s failed: %v", sid, err)
		}
	}

	switch dec.Action {
	case ActionEnable:
		if err := sink.Set(true); err != nil {
			log.Printf("reconcile: enable sleep-disable failed: %v", err)
			return err
		}
		if err := st.SetFlag(); err != nil {
			log.Printf("reconcile: set flag failed: %v", err)
		}
	case ActionDisable:
		if err := sink.Set(false); err != nil {
			log.Printf("reconcile: disable sleep-disable failed: %v", err)
			return err
		}
		if err := st.ClearFlag(); err != nil {
			log.Printf("reconcile: clear flag failed: %v", err)
		}
	case ActionWarnStuck:
		log.Printf("reconcile: WARNING disablesleep is ON but agentawake has " +
			"no active sessions and no record of enabling it — run `agentawake off` if unexpected")
	}
	return nil
}
