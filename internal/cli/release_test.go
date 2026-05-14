package cli

import (
	"strings"
	"testing"
	"time"

	"github.com/hok/agentawake/internal/state"
	"github.com/hok/agentawake/internal/token"
)

func TestRelease_RemovesMatchingToken(t *testing.T) {
	st := state.New(t.TempDir())
	tok := token.Token{
		Agent: token.AgentClaudeCode, PID: 0, SessionID: "sess-1",
		StartedAt: time.Now().UTC(),
	}
	if err := st.WriteToken(tok); err != nil {
		t.Fatal(err)
	}

	code := runRelease(strings.NewReader(`{"session_id":"sess-1"}`), st)
	if code != 0 {
		t.Fatalf("runRelease exit = %d, want 0", code)
	}
	toks, _ := st.ListTokens()
	if len(toks) != 0 {
		t.Errorf("token not removed: %+v", toks)
	}
}

func TestRelease_UnknownSessionIsNoOp(t *testing.T) {
	st := state.New(t.TempDir())
	code := runRelease(strings.NewReader(`{"session_id":"never-existed"}`), st)
	if code != 0 {
		t.Errorf("runRelease exit = %d, want 0", code)
	}
}

func TestRelease_GarbageStdinNeverFails(t *testing.T) {
	st := state.New(t.TempDir())
	if code := runRelease(strings.NewReader("garbage"), st); code != 0 {
		t.Errorf("runRelease exit = %d, want 0", code)
	}
}
