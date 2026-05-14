package cli

import (
	"strings"
	"testing"
	"time"

	"github.com/hok/agentawake/internal/state"
	"github.com/hok/agentawake/internal/token"
)

func TestStatus_ReportsActiveSessions(t *testing.T) {
	st := state.New(t.TempDir())
	tok := token.Token{
		Agent: token.AgentCodex, PID: 0, SessionID: "sess-x",
		StartedAt: time.Now().Add(-90 * time.Second).UTC(),
	}
	if err := st.WriteToken(tok); err != nil {
		t.Fatal(err)
	}

	var out strings.Builder
	// wrapperInstalled=true, sleepDisabled=true -> consistent (has a session)
	code := renderStatus(st, &out, statusFacts{wrapperInstalled: true, sleepDisabled: true})
	if code != 0 {
		t.Errorf("exit code = %d, want 0", code)
	}
	s := out.String()
	if !strings.Contains(s, "sess-x") || !strings.Contains(s, "codex") {
		t.Errorf("status output missing session details:\n%s", s)
	}
}

func TestStatus_NotInstalled(t *testing.T) {
	st := state.New(t.TempDir())
	var out strings.Builder
	code := renderStatus(st, &out, statusFacts{wrapperInstalled: false})
	if code != 1 {
		t.Errorf("exit code = %d, want 1 (not installed)", code)
	}
}

func TestStatus_StuckOnInconsistency(t *testing.T) {
	st := state.New(t.TempDir())
	// No tokens, no flag, but sleep is disabled -> stuck-on.
	var out strings.Builder
	code := renderStatus(st, &out, statusFacts{wrapperInstalled: true, sleepDisabled: true})
	if code != 2 {
		t.Errorf("exit code = %d, want 2 (stuck-on)", code)
	}
	if !strings.Contains(out.String(), "agentawake off") {
		t.Errorf("stuck-on output should advise `agentawake off`:\n%s", out.String())
	}
}
