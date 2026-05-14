package install

import (
	"encoding/json"
	"fmt"
	"os"
)

// hookMarker identifies entries this tool added, so RemoveHooks can find them
// and MergeHooks can stay idempotent.
const hookMarker = "agentawake "

// acquireCommand / releaseCommand are the hook command strings.
func acquireCommand(agent string) string {
	return fmt.Sprintf("agentawake acquire --agent %s", agent)
}
func releaseCommand() string { return "agentawake release" }

// hookEntry is one hook group: a matcher plus a list of commands.
func hookEntry(command string) map[string]any {
	return map[string]any{
		"matcher": "",
		"hooks": []any{
			map[string]any{"type": "command", "command": command, "timeout": 10},
		},
	}
}

// MergeHooks adds agentawake's UserPromptSubmit/Stop entries to a Claude Code
// or Codex config file, preserving all existing content. Idempotent. agent is
// "claude-code" or "codex". A missing file is created.
func MergeHooks(path, agent string) error {
	root, err := loadJSONObject(path)
	if err != nil {
		return err
	}
	hooks := childObject(root, "hooks")
	addEntry(hooks, "UserPromptSubmit", acquireCommand(agent))
	addEntry(hooks, "Stop", releaseCommand())
	root["hooks"] = hooks
	return writeJSONObject(path, root)
}

// RemoveHooks deletes only the entries MergeHooks added, leaving the rest.
func RemoveHooks(path string) error {
	root, err := loadJSONObject(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	hooksRaw, ok := root["hooks"].(map[string]any)
	if !ok {
		return nil
	}
	for event, v := range hooksRaw {
		arr, ok := v.([]any)
		if !ok {
			continue
		}
		filtered := filterOutMarked(arr)
		// Drop the event key entirely once it has no remaining entries, so
		// uninstall does not leave empty arrays as cruft.
		if len(filtered) == 0 {
			delete(hooksRaw, event)
		} else {
			hooksRaw[event] = filtered
		}
	}
	// If nothing is left under "hooks", drop the top-level key too.
	if len(hooksRaw) == 0 {
		delete(root, "hooks")
	} else {
		root["hooks"] = hooksRaw
	}
	return writeJSONObject(path, root)
}

// --- helpers ---

func loadJSONObject(path string) (map[string]any, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return map[string]any{}, nil
	}
	if err != nil {
		return nil, err
	}
	if len(data) == 0 {
		return map[string]any{}, nil
	}
	var root map[string]any
	if err := json.Unmarshal(data, &root); err != nil {
		return nil, fmt.Errorf("%s: not a JSON object: %w", path, err)
	}
	return root, nil
}

// writeJSONObject atomically writes root to path (temp file + rename).
//
// Known limitation: json.MarshalIndent over a map[string]any alphabetizes
// object keys, so the user's original top-level key order is not preserved
// across a merge. This is an accepted trade-off of the stdlib-only JSON
// round-trip.
func writeJSONObject(path string, root map[string]any) error {
	data, err := json.MarshalIndent(root, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	// Preserve the target file's existing permissions; never widen a
	// user credential file like ~/.claude/settings.json (typically 0600).
	// Default to 0600 for a not-yet-existing home-directory config file.
	mode := os.FileMode(0o600)
	if info, err := os.Stat(path); err == nil {
		mode = info.Mode().Perm()
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, mode); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

func childObject(parent map[string]any, key string) map[string]any {
	if existing, ok := parent[key].(map[string]any); ok {
		return existing
	}
	return map[string]any{}
}

// addEntry appends a hook entry for command under event, unless an entry with
// that exact command is already present (idempotency).
//
// Idempotency is keyed on the exact command string: a hand-edited entry (e.g. a
// changed timeout) is not recognized as ours and would produce a duplicate on
// re-merge.
func addEntry(hooks map[string]any, event, command string) {
	var arr []any
	if existing, ok := hooks[event].([]any); ok {
		arr = existing
	}
	if containsCommand(arr, command) {
		return
	}
	hooks[event] = append(arr, hookEntry(command))
}

func containsCommand(arr []any, command string) bool {
	for _, group := range arr {
		for _, cmd := range commandsOf(group) {
			if cmd == command {
				return true
			}
		}
	}
	return false
}

// filterOutMarked drops hook groups whose every command is an agentawake command.
func filterOutMarked(arr []any) []any {
	kept := []any{}
	for _, group := range arr {
		cmds := commandsOf(group)
		allOurs := len(cmds) > 0
		for _, c := range cmds {
			if !startsWith(c, hookMarker) {
				allOurs = false
			}
		}
		if !allOurs {
			kept = append(kept, group)
		}
	}
	return kept
}

// commandsOf extracts the command strings from a hook group object.
func commandsOf(group any) []string {
	obj, ok := group.(map[string]any)
	if !ok {
		return nil
	}
	inner, ok := obj["hooks"].([]any)
	if !ok {
		return nil
	}
	var out []string
	for _, h := range inner {
		hm, ok := h.(map[string]any)
		if !ok {
			continue
		}
		if c, ok := hm["command"].(string); ok {
			out = append(out, c)
		}
	}
	return out
}

func startsWith(s, prefix string) bool {
	return len(s) >= len(prefix) && s[:len(prefix)] == prefix
}
