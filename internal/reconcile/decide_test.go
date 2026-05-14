package reconcile

import (
	"testing"
	"time"

	"github.com/hok/agentawake/internal/token"
)

var now = time.Date(2026, 5, 14, 12, 0, 0, 0, time.UTC)

func tok(id string, pid int, age time.Duration) token.Token {
	return token.Token{
		Agent:     token.AgentClaudeCode,
		PID:       pid,
		SessionID: id,
		StartedAt: now.Add(-age),
	}
}

// allAlive treats every positive pid as alive.
func allAlive(pid int) bool { return pid > 0 }

// noneAlive treats every pid as dead.
func noneAlive(pid int) bool { return false }

func baseInputs() Inputs {
	return Inputs{
		Now:     now,
		MaxAge:  4 * time.Hour,
		IsAlive: allAlive,
	}
}

func TestDecide_LiveSessionNoFlag_Enables(t *testing.T) {
	in := baseInputs()
	in.Tokens = []token.Token{tok("s1", 100, time.Minute)}
	in.FlagPresent = false
	in.SleepDisabled = false

	got := Decide(in)
	if got.Action != ActionEnable {
		t.Errorf("Action = %v, want ActionEnable", got.Action)
	}
	if len(got.Prune) != 0 {
		t.Errorf("Prune = %v, want empty", got.Prune)
	}
}

func TestDecide_NoSessionsWithFlag_Disables(t *testing.T) {
	in := baseInputs()
	in.Tokens = nil
	in.FlagPresent = true
	in.SleepDisabled = true

	if got := Decide(in); got.Action != ActionDisable {
		t.Errorf("Action = %v, want ActionDisable", got.Action)
	}
}

func TestDecide_DeadPidPrunedThenDisables(t *testing.T) {
	in := baseInputs()
	in.IsAlive = noneAlive
	in.Tokens = []token.Token{tok("dead", 100, time.Minute)}
	in.FlagPresent = true
	in.SleepDisabled = true

	got := Decide(in)
	if len(got.Prune) != 1 || got.Prune[0] != "dead" {
		t.Errorf("Prune = %v, want [dead]", got.Prune)
	}
	if got.Action != ActionDisable {
		t.Errorf("Action = %v, want ActionDisable", got.Action)
	}
}

func TestDecide_OverAgeTokenPruned(t *testing.T) {
	in := baseInputs()
	in.Tokens = []token.Token{tok("stale", 100, 5*time.Hour)} // alive pid but too old
	in.FlagPresent = true
	in.SleepDisabled = true

	got := Decide(in)
	if len(got.Prune) != 1 || got.Prune[0] != "stale" {
		t.Errorf("Prune = %v, want [stale]", got.Prune)
	}
	if got.Action != ActionDisable {
		t.Errorf("Action = %v, want ActionDisable", got.Action)
	}
}

func TestDecide_PidZeroGovernedByAgeOnly(t *testing.T) {
	in := baseInputs()
	in.IsAlive = noneAlive // would prune any real pid
	in.Tokens = []token.Token{tok("young", 0, time.Minute)}
	in.FlagPresent = true
	in.SleepDisabled = true

	got := Decide(in)
	if len(got.Prune) != 0 {
		t.Errorf("Prune = %v, want empty (pid 0, within max age)", got.Prune)
	}
	if got.Action != ActionNone {
		t.Errorf("Action = %v, want ActionNone", got.Action)
	}
}

func TestDecide_StuckOn_NoSessionsNoFlagButDisabled(t *testing.T) {
	in := baseInputs()
	in.Tokens = nil
	in.FlagPresent = false
	in.SleepDisabled = true

	if got := Decide(in); got.Action != ActionWarnStuck {
		t.Errorf("Action = %v, want ActionWarnStuck", got.Action)
	}
}

func TestDecide_NothingToDo(t *testing.T) {
	in := baseInputs()
	in.Tokens = nil
	in.FlagPresent = false
	in.SleepDisabled = false

	if got := Decide(in); got.Action != ActionNone {
		t.Errorf("Action = %v, want ActionNone", got.Action)
	}
}
