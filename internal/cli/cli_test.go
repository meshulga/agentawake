package cli

import (
	"strings"
	"testing"

	"github.com/hok/agentawake/internal/state"
)

func TestAcquire_WritesTokenFromStdin(t *testing.T) {
	dir := t.TempDir()
	st := state.New(dir)

	code := runAcquire(
		[]string{"--agent", "claude-code"},
		strings.NewReader(`{"session_id":"sess-1"}`),
		st,
	)
	if code != 0 {
		t.Fatalf("runAcquire exit = %d, want 0", code)
	}
	toks, err := st.ListTokens()
	if err != nil {
		t.Fatalf("ListTokens: %v", err)
	}
	if len(toks) != 1 || toks[0].SessionID != "sess-1" {
		t.Fatalf("tokens = %+v, want one token sess-1", toks)
	}
	if string(toks[0].Agent) != "claude-code" {
		t.Errorf("agent = %q, want claude-code", toks[0].Agent)
	}
}

func TestAcquire_NeverFailsTheAgent(t *testing.T) {
	st := state.New(t.TempDir())
	// Garbage stdin must still exit 0 — a hook must never break the agent.
	code := runAcquire([]string{"--agent", "codex"}, strings.NewReader("garbage"), st)
	if code != 0 {
		t.Errorf("runAcquire exit = %d, want 0 even on bad input", code)
	}
}
