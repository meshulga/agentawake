package cli

import (
	"testing"
	"time"

	"github.com/hok/agentawake/internal/state"
	"github.com/hok/agentawake/internal/token"
)

func TestOff_RemovesAllTokensAndFlag(t *testing.T) {
	st := state.New(t.TempDir())
	for _, id := range []string{"a", "b"} {
		tok := token.Token{
			Agent: token.AgentClaudeCode, PID: 0, SessionID: id,
			StartedAt: time.Now().UTC(),
		}
		if err := st.WriteToken(tok); err != nil {
			t.Fatal(err)
		}
	}
	if err := st.SetFlag(); err != nil {
		t.Fatal(err)
	}

	var setCalls []bool
	code := runOff(st, func(on bool) error { setCalls = append(setCalls, on); return nil })
	if code != 0 {
		t.Fatalf("runOff exit = %d, want 0", code)
	}
	toks, _ := st.ListTokens()
	if len(toks) != 0 {
		t.Errorf("tokens not cleared: %+v", toks)
	}
	if st.HasFlag() {
		t.Error("flag not cleared")
	}
	if len(setCalls) != 1 || setCalls[0] {
		t.Errorf("setCalls = %v, want [false]", setCalls)
	}
}
