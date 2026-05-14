package token

import (
	"testing"
	"time"
)

func TestMarshalUnmarshalRoundTrip(t *testing.T) {
	original := Token{
		Agent:     AgentClaudeCode,
		PID:       12345,
		SessionID: "abc123",
		StartedAt: time.Date(2026, 5, 14, 10, 30, 0, 0, time.UTC),
	}
	data, err := original.Marshal()
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	got, err := Unmarshal(data)
	if err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if got != original {
		t.Errorf("round trip mismatch:\n got  %+v\n want %+v", got, original)
	}
}

func TestUnmarshalRejectsGarbage(t *testing.T) {
	if _, err := Unmarshal([]byte("not json")); err == nil {
		t.Error("expected error for malformed input, got nil")
	}
}
