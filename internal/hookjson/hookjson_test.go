package hookjson

import (
	"strings"
	"testing"
)

func TestParse_ClaudeStyle(t *testing.T) {
	p, err := Parse(strings.NewReader(`{"session_id":"abc","cwd":"/x"}`))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if p.SessionID != "abc" {
		t.Errorf("SessionID = %q, want abc", p.SessionID)
	}
}

func TestParse_AlternateFieldNames(t *testing.T) {
	p, err := Parse(strings.NewReader(`{"conversationId":"xyz","pid":4321}`))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if p.SessionID != "xyz" {
		t.Errorf("SessionID = %q, want xyz", p.SessionID)
	}
	if p.PID != 4321 {
		t.Errorf("PID = %d, want 4321", p.PID)
	}
}

func TestParse_MissingSessionID(t *testing.T) {
	p, err := Parse(strings.NewReader(`{"cwd":"/x"}`))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if p.SessionID != "" {
		t.Errorf("SessionID = %q, want empty", p.SessionID)
	}
}

func TestParse_Garbage(t *testing.T) {
	if _, err := Parse(strings.NewReader("not json")); err == nil {
		t.Error("expected error for malformed JSON")
	}
}
