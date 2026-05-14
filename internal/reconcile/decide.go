// Package reconcile decides and applies the system sleep state from the set of active session tokens.
package reconcile

import (
	"time"

	"github.com/hok/agentawake/internal/token"
)

// SleepAction is the decision reconcile reached about pmset disablesleep.
type SleepAction int

const (
	// ActionNone: leave pmset alone.
	ActionNone SleepAction = iota
	// ActionEnable: ensure disablesleep is on and the we-enabled flag is set.
	ActionEnable
	// ActionDisable: turn disablesleep off and clear the we-enabled flag.
	ActionDisable
	// ActionWarnStuck: disablesleep is on, no sessions, no flag - likely a
	// lost flag. Do not auto-clear; warn the user instead.
	ActionWarnStuck
)

func (a SleepAction) String() string {
	switch a {
	case ActionEnable:
		return "enable"
	case ActionDisable:
		return "disable"
	case ActionWarnStuck:
		return "warn-stuck"
	default:
		return "none"
	}
}

// Inputs is everything the pure decision needs. No I/O happens inside Decide.
type Inputs struct {
	Tokens        []token.Token
	FlagPresent   bool // is the we-enabled flag set
	SleepDisabled bool // current pmset disablesleep state
	Now           time.Time
	MaxAge        time.Duration // tokens older than this are pruned
	IsAlive       func(pid int) bool
}

// Decision is the full output of Decide.
type Decision struct {
	Prune  []string // session IDs whose token files should be removed
	Action SleepAction
}

// Decide is a pure function: given the current state it returns which tokens
// to prune and what to do with pmset. It performs no I/O.
func Decide(in Inputs) Decision {
	var live int
	var pruneIDs []string
	for _, t := range in.Tokens {
		dead := t.PID > 0 && !in.IsAlive(t.PID)
		overAge := in.Now.Sub(t.StartedAt) > in.MaxAge
		if dead || overAge {
			pruneIDs = append(pruneIDs, t.SessionID)
			continue
		}
		live++
	}

	var action SleepAction
	switch {
	case live > 0 && (!in.SleepDisabled || !in.FlagPresent):
		// Sessions are active but either sleep is not yet disabled or we have
		// no flag recording it. Enable is idempotent (pmset 1 + SetFlag).
		action = ActionEnable
	case live == 0 && in.FlagPresent:
		action = ActionDisable
	case live == 0 && !in.FlagPresent && in.SleepDisabled:
		action = ActionWarnStuck
	default:
		action = ActionNone
	}
	return Decision{Prune: pruneIDs, Action: action}
}
