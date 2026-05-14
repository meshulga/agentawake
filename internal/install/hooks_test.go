package install

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const preExisting = `{
  "hooks": {
    "UserPromptSubmit": [
      {"hooks": [{"type": "command", "command": "existing-tool"}]}
    ]
  }
}`

func writeFile(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func countCommands(t *testing.T, path, substr string) int {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return strings.Count(string(data), substr)
}

func TestMergeHooks_AddsAlongsideExisting(t *testing.T) {
	path := writeFile(t, preExisting)
	if err := MergeHooks(path, "claude-code"); err != nil {
		t.Fatalf("MergeHooks: %v", err)
	}
	if got := countCommands(t, path, "existing-tool"); got != 1 {
		t.Errorf("existing hook count = %d, want 1 (untouched)", got)
	}
	if got := countCommands(t, path, "agentawake acquire --agent claude-code"); got != 1 {
		t.Errorf("acquire hook count = %d, want 1", got)
	}
	if got := countCommands(t, path, "agentawake release"); got != 1 {
		t.Errorf("release hook count = %d, want 1", got)
	}
	// Result must still be valid JSON.
	var v any
	data, _ := os.ReadFile(path)
	if err := json.Unmarshal(data, &v); err != nil {
		t.Errorf("merged file is not valid JSON: %v", err)
	}
}

func TestMergeHooks_Idempotent(t *testing.T) {
	path := writeFile(t, preExisting)
	if err := MergeHooks(path, "claude-code"); err != nil {
		t.Fatal(err)
	}
	if err := MergeHooks(path, "claude-code"); err != nil {
		t.Fatal(err)
	}
	if got := countCommands(t, path, "agentawake acquire"); got != 1 {
		t.Errorf("acquire hook count = %d after double merge, want 1", got)
	}
}

func TestMergeHooks_CreatesMissingFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "new.json")
	if err := MergeHooks(path, "codex"); err != nil {
		t.Fatalf("MergeHooks on missing file: %v", err)
	}
	if got := countCommands(t, path, "agentawake acquire --agent codex"); got != 1 {
		t.Errorf("acquire hook count = %d, want 1", got)
	}
}

func TestRemoveHooks_LeavesExistingIntact(t *testing.T) {
	path := writeFile(t, preExisting)
	if err := MergeHooks(path, "claude-code"); err != nil {
		t.Fatal(err)
	}
	if err := RemoveHooks(path); err != nil {
		t.Fatalf("RemoveHooks: %v", err)
	}
	if got := countCommands(t, path, "agentawake"); got != 0 {
		t.Errorf("agentawake hooks remaining = %d, want 0", got)
	}
	if got := countCommands(t, path, "existing-tool"); got != 1 {
		t.Errorf("existing hook count = %d, want 1 (preserved)", got)
	}
}

func TestMergeHooks_RejectsMalformedJSON(t *testing.T) {
	const malformed = "{bad"
	path := writeFile(t, malformed)
	if err := MergeHooks(path, "claude-code"); err == nil {
		t.Fatal("MergeHooks on malformed JSON: want error, got nil")
	}
	// Parse-before-write safety: the original file must be untouched.
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != malformed {
		t.Errorf("file content changed after failed merge: got %q, want %q", data, malformed)
	}
}

func TestMergeHooks_PreservesFileMode(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := MergeHooks(path, "claude-code"); err != nil {
		t.Fatalf("MergeHooks: %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Errorf("file mode = %o after merge, want 600 (not widened)", got)
	}
}

func TestRemoveHooks_MissingFileIsNoOp(t *testing.T) {
	path := filepath.Join(t.TempDir(), "does-not-exist.json")
	if err := RemoveHooks(path); err != nil {
		t.Errorf("RemoveHooks on missing file: want nil, got %v", err)
	}
}

func TestRemoveHooks_AgentawakeOnlyFileLeavesNoCruft(t *testing.T) {
	path := writeFile(t, "{}")
	if err := MergeHooks(path, "claude-code"); err != nil {
		t.Fatal(err)
	}
	if err := RemoveHooks(path); err != nil {
		t.Fatalf("RemoveHooks: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var root map[string]any
	if err := json.Unmarshal(data, &root); err != nil {
		t.Fatalf("result is not valid JSON: %v", err)
	}
	if hooks, ok := root["hooks"]; ok {
		obj, ok := hooks.(map[string]any)
		if !ok || len(obj) != 0 {
			t.Errorf("hooks key should be absent or empty, got %#v", hooks)
		}
	}
}
