// Package hookjson parses the JSON payload that Claude Code and Codex pass on
// stdin to hook commands. It is deliberately tolerant: the two tools may name
// fields differently, and unknown fields are ignored.
package hookjson

import (
	"encoding/json"
	"io"
)

// Payload is the subset of hook stdin JSON that agentawake needs.
type Payload struct {
	SessionID string
	PID       int
}

// sessionKeys / pidKeys are the field names tried, in order. See the spec's
// "Open items" — the exact field names must be confirmed against both tools
// during implementation; extra candidates here cost nothing.
var sessionKeys = []string{"session_id", "sessionId", "conversation_id", "conversationId"}
var pidKeys = []string{"pid", "process_id", "agent_pid"}

// Parse reads hook JSON from r and extracts the session id and optional pid.
func Parse(r io.Reader) (Payload, error) {
	var raw map[string]any
	if err := json.NewDecoder(r).Decode(&raw); err != nil {
		return Payload{}, err
	}
	var p Payload
	for _, k := range sessionKeys {
		if v, ok := raw[k].(string); ok && v != "" {
			p.SessionID = v
			break
		}
	}
	for _, k := range pidKeys {
		if v, ok := raw[k].(float64); ok && v > 0 {
			p.PID = int(v)
			break
		}
	}
	return p, nil
}
