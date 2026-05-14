package reconcile

import (
	"testing"
	"time"

	"github.com/hok/agentawake/internal/logging"
	"github.com/hok/agentawake/internal/state"
	"github.com/hok/agentawake/internal/token"
)

// fakeSink records what Run tried to do, instead of touching pmset.
type fakeSink struct {
	disabled bool
	setCalls []bool
}

func (f *fakeSink) IsDisabled() (bool, error) { return f.disabled, nil }
func (f *fakeSink) Set(on bool) error {
	f.setCalls = append(f.setCalls, on)
	f.disabled = on
	return nil
}

func liveToken(id string) token.Token {
	return token.Token{
		Agent:     token.AgentClaudeCode,
		PID:       0, // pid 0 -> governed by age only, always "live" here
		SessionID: id,
		StartedAt: time.Now().UTC(),
	}
}

func TestRun_EnablesAndSetsFlag(t *testing.T) {
	st := state.New(t.TempDir())
	log := logging.New(st.LogPath())
	sink := &fakeSink{disabled: false}
	if err := st.WriteToken(liveToken("s1")); err != nil {
		t.Fatal(err)
	}

	if err := RunWith(st, log, sink); err != nil {
		t.Fatalf("RunWith: %v", err)
	}
	if len(sink.setCalls) != 1 || sink.setCalls[0] != true {
		t.Errorf("setCalls = %v, want [true]", sink.setCalls)
	}
	if !st.HasFlag() {
		t.Error("flag should be set after enabling")
	}
}

func TestRun_DisablesAndClearsFlagWhenNoSessions(t *testing.T) {
	st := state.New(t.TempDir())
	log := logging.New(st.LogPath())
	sink := &fakeSink{disabled: true}
	if err := st.SetFlag(); err != nil {
		t.Fatal(err)
	}

	if err := RunWith(st, log, sink); err != nil {
		t.Fatalf("RunWith: %v", err)
	}
	if len(sink.setCalls) != 1 || sink.setCalls[0] != false {
		t.Errorf("setCalls = %v, want [false]", sink.setCalls)
	}
	if st.HasFlag() {
		t.Error("flag should be cleared after disabling")
	}
}

func TestRun_PrunesStaleTokens(t *testing.T) {
	st := state.New(t.TempDir())
	log := logging.New(st.LogPath())
	sink := &fakeSink{disabled: true}
	if err := st.SetFlag(); err != nil {
		t.Fatal(err)
	}
	stale := token.Token{
		Agent:     token.AgentCodex,
		PID:       0,
		SessionID: "old",
		StartedAt: time.Now().Add(-10 * time.Hour).UTC(),
	}
	if err := st.WriteToken(stale); err != nil {
		t.Fatal(err)
	}

	if err := RunWith(st, log, sink); err != nil {
		t.Fatalf("RunWith: %v", err)
	}
	toks, _ := st.ListTokens()
	if len(toks) != 0 {
		t.Errorf("stale token not pruned: %+v", toks)
	}
	if len(sink.setCalls) != 1 || sink.setCalls[0] != false {
		t.Errorf("setCalls = %v, want [false]", sink.setCalls)
	}
}
