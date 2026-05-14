package state

import (
	"testing"
	"time"

	"github.com/hok/agentawake/internal/token"
)

func newTok(id string) token.Token {
	return token.Token{
		Agent:     token.AgentCodex,
		PID:       123,
		SessionID: id,
		StartedAt: time.Unix(100, 0).UTC(),
	}
}

func TestWriteListRemoveToken(t *testing.T) {
	s := New(t.TempDir())

	if err := s.WriteToken(newTok("s1")); err != nil {
		t.Fatalf("WriteToken s1: %v", err)
	}
	if err := s.WriteToken(newTok("s2")); err != nil {
		t.Fatalf("WriteToken s2: %v", err)
	}

	toks, err := s.ListTokens()
	if err != nil {
		t.Fatalf("ListTokens: %v", err)
	}
	if len(toks) != 2 {
		t.Fatalf("len(ListTokens) = %d, want 2", len(toks))
	}

	if err := s.RemoveToken("s1"); err != nil {
		t.Fatalf("RemoveToken s1: %v", err)
	}
	if err := s.RemoveToken("missing"); err != nil {
		t.Fatalf("RemoveToken missing: %v", err)
	}

	toks, err = s.ListTokens()
	if err != nil {
		t.Fatalf("ListTokens after remove: %v", err)
	}
	if len(toks) != 1 || toks[0].SessionID != "s2" {
		t.Fatalf("remaining tokens = %#v, want only s2", toks)
	}
}

func TestListTokensSkipsMalformed(t *testing.T) {
	s := New(t.TempDir())

	if err := s.WriteToken(newTok("good")); err != nil {
		t.Fatalf("WriteToken: %v", err)
	}
	if err := s.writeRaw("bad", []byte("{garbage")); err != nil {
		t.Fatalf("writeRaw: %v", err)
	}

	toks, err := s.ListTokens()
	if err != nil {
		t.Fatalf("ListTokens: %v", err)
	}
	if len(toks) != 1 || toks[0].SessionID != "good" {
		t.Fatalf("tokens = %#v, want only good", toks)
	}
}

func TestFlagLifecycle(t *testing.T) {
	s := New(t.TempDir())

	if s.HasFlag() {
		t.Fatalf("HasFlag before SetFlag = true, want false")
	}
	if err := s.SetFlag(); err != nil {
		t.Fatalf("SetFlag: %v", err)
	}
	if !s.HasFlag() {
		t.Fatalf("HasFlag after SetFlag = false, want true")
	}
	if err := s.ClearFlag(); err != nil {
		t.Fatalf("ClearFlag: %v", err)
	}
	if s.HasFlag() {
		t.Fatalf("HasFlag after ClearFlag = true, want false")
	}
	if err := s.ClearFlag(); err != nil {
		t.Fatalf("ClearFlag absent: %v", err)
	}
}

func TestLockSerializes(t *testing.T) {
	s := New(t.TempDir())

	unlock, err := s.Lock()
	if err != nil {
		t.Fatalf("Lock: %v", err)
	}
	defer unlock()

	acquired := make(chan func(), 1)
	errs := make(chan error, 1)
	go func() {
		unlock2, err := s.Lock()
		if err != nil {
			errs <- err
			return
		}
		acquired <- unlock2
	}()

	select {
	case err := <-errs:
		t.Fatalf("second Lock: %v", err)
	case unlock2 := <-acquired:
		unlock2()
		t.Fatalf("second Lock acquired before first unlock")
	case <-time.After(50 * time.Millisecond):
	}

	unlock()

	select {
	case err := <-errs:
		t.Fatalf("second Lock after unlock: %v", err)
	case unlock2 := <-acquired:
		unlock2()
	case <-time.After(time.Second):
		t.Fatalf("second Lock did not acquire after unlock")
	}
}
