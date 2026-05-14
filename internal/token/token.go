// Package token defines the on-disk record for one active agent turn.
package token

import (
	"encoding/json"
	"time"
)

// Agent identifies which CLI tool a session belongs to.
type Agent string

const (
	AgentClaudeCode Agent = "claude-code"
	AgentCodex      Agent = "codex"
)

// Token is the JSON record written to ~/.local/state/agentawake/sessions/<session_id>
// while a turn is active.
type Token struct {
	Agent     Agent     `json:"agent"`
	PID       int       `json:"pid"`
	SessionID string    `json:"session_id"`
	StartedAt time.Time `json:"started_at"`
}

// Marshal renders the token as indented JSON.
func (t Token) Marshal() ([]byte, error) {
	return json.MarshalIndent(t, "", "  ")
}

// Unmarshal parses a token from JSON.
func Unmarshal(data []byte) (Token, error) {
	var t Token
	err := json.Unmarshal(data, &t)
	return t, err
}
