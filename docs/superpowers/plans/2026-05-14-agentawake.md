# agentawake Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** A single Go CLI for macOS that keeps the Mac awake (including lid-closed) only while a Claude Code or Codex agent turn is actively running.

**Architecture:** A token directory under `~/.local/state/agentawake/` is the single source of truth — one file per active turn. Lifecycle hooks (`UserPromptSubmit` → `acquire`, `Stop` → `release`) write/remove tokens; an idempotent `reconcile` (run from every hook and from a ~60s launchd agent) prunes dead/over-age tokens and toggles `pmset disablesleep` via a passwordless-sudo root wrapper. The reconcile decision is a pure function; all I/O sits behind thin adapters.

**Tech Stack:** Go 1.23 (stdlib only — no external deps), `syscall.Flock` for locking, `go:embed` for the wrapper script, goreleaser + a Homebrew tap for distribution.

---

## Conventions

- **Module path:** `github.com/hok/agentawake`. If your GitHub username is not `hok`, after Task 1 run:
  `grep -rl 'github.com/hok/agentawake' . | xargs sed -i '' 's#github.com/hok/agentawake#github.com/YOURNAME/agentawake#g'` and update `.goreleaser.yaml` owner fields.
- **Test command:** `go test ./...` runs everything. Per-package: `go test ./internal/<pkg>/ -v`.
- **Every task ends with a commit.** Commit messages use Conventional Commits.
- The design spec is at `docs/superpowers/specs/2026-05-14-agentawake-design.md` (gitignored — local only).

## File structure

```
agentawake/
├── go.mod
├── main.go                          # entrypoint → internal/cli.Main
├── .goreleaser.yaml                 # release + Homebrew tap config
├── README.md
├── assets/
│   └── agentawake-pmset.sh          # embedded privileged wrapper script
├── internal/
│   ├── token/token.go               # Token struct + JSON round-trip
│   ├── state/state.go               # token dir, flock, token & flag I/O
│   ├── reconcile/decide.go          # pure decision function
│   ├── reconcile/run.go             # orchestration under lock
│   ├── pid/pid.go                   # PID liveness + ancestor detection
│   ├── pmset/pmset.go               # pmset -g parsing + wrapper invocation
│   ├── hookjson/hookjson.go         # parse hook stdin JSON (per-agent tolerant)
│   ├── logging/logging.go           # size-capped rotating log
│   ├── install/pathcheck.go         # parent-dir ownership safety check
│   ├── install/sudoers.go           # sudoers render + atomic write
│   ├── install/hooks.go             # merge/remove hook entries in config files
│   ├── install/launchd.go           # launchd plist write + load/unload
│   └── cli/
│       ├── cli.go                   # arg dispatch, usage, Version, shared helpers
│       ├── acquire.go               # `acquire` command
│       ├── release.go               # `release` command
│       ├── reconcile.go             # `reconcile` command
│       ├── status.go                # `status` command
│       ├── off.go                   # `off` command
│       └── install.go               # `install`, `uninstall`, `_root-setup`, `_root-teardown`
└── docs/superpowers/...             # spec + this plan (gitignored)
```

---

## Task 1: Project skeleton

**Files:**
- Create: `go.mod`, `main.go`, `internal/cli/cli.go`

- [ ] **Step 1: Initialize the Go module**

Run:
```bash
cd ~/work/projects/agentawake
go mod init github.com/hok/agentawake
```
Expected: creates `go.mod` with `module github.com/hok/agentawake` and a `go 1.23` line (or your installed version).

- [ ] **Step 2: Create `internal/cli/cli.go`**

```go
// Package cli dispatches command-line arguments to subcommands.
package cli

import (
	"fmt"
	"os"
)

// Version is overridden at build time via -ldflags.
var Version = "dev"

const usage = `agentawake — keep your Mac awake while an AI agent turn is running

Usage:
  agentawake install            Wire up hooks + privileged sleep toggle (asks for password once)
  agentawake uninstall          Remove everything agentawake installed
  agentawake status             Show active sessions and current sleep state
  agentawake off                Emergency reset: clear state and restore normal sleep
  agentawake reconcile          Re-sync sleep state with active sessions (used by launchd)
  agentawake version            Print version

Hook commands (invoked automatically once installed):
  agentawake acquire --agent <claude-code|codex>
  agentawake release
`

// Main runs the CLI and returns a process exit code.
func Main(args []string) int {
	if len(args) == 0 {
		fmt.Fprint(os.Stderr, usage)
		return 1
	}
	switch args[0] {
	case "version", "--version", "-v":
		fmt.Println(Version)
		return 0
	case "help", "--help", "-h":
		fmt.Print(usage)
		return 0
	default:
		fmt.Fprintf(os.Stderr, "agentawake: unknown command %q\n\n%s", args[0], usage)
		return 1
	}
}
```

- [ ] **Step 3: Create `main.go`**

```go
package main

import (
	"os"

	"github.com/hok/agentawake/internal/cli"
)

func main() {
	os.Exit(cli.Main(os.Args[1:]))
}
```

- [ ] **Step 4: Verify it builds and runs**

Run:
```bash
go build ./... && go run . version
```
Expected: prints `dev`.

- [ ] **Step 5: Commit**

```bash
git add go.mod main.go internal/cli/cli.go
git commit -m "feat: project skeleton with CLI dispatch"
```

---

## Task 2: `token` package

**Files:**
- Create: `internal/token/token.go`
- Test: `internal/token/token_test.go`

- [ ] **Step 1: Write the failing test**

`internal/token/token_test.go`:
```go
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
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/token/ -v`
Expected: FAIL — `undefined: Token` (package doesn't compile).

- [ ] **Step 3: Write `internal/token/token.go`**

```go
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
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/token/ -v`
Expected: PASS — both tests.

- [ ] **Step 5: Commit**

```bash
git add internal/token/
git commit -m "feat(token): add Token type with JSON round-trip"
```

---

## Task 3: `state` package

**Files:**
- Create: `internal/state/state.go`
- Test: `internal/state/state_test.go`

- [ ] **Step 1: Write the failing test**

`internal/state/state_test.go`:
```go
package state

import (
	"testing"
	"time"

	"github.com/hok/agentawake/internal/token"
)

func newTok(id string) token.Token {
	return token.Token{
		Agent:     token.AgentClaudeCode,
		PID:       1,
		SessionID: id,
		StartedAt: time.Now().UTC(),
	}
}

func TestWriteListRemoveToken(t *testing.T) {
	s := New(t.TempDir())

	if err := s.WriteToken(newTok("s1")); err != nil {
		t.Fatalf("WriteToken: %v", err)
	}
	if err := s.WriteToken(newTok("s2")); err != nil {
		t.Fatalf("WriteToken: %v", err)
	}
	toks, err := s.ListTokens()
	if err != nil {
		t.Fatalf("ListTokens: %v", err)
	}
	if len(toks) != 2 {
		t.Fatalf("got %d tokens, want 2", len(toks))
	}

	if err := s.RemoveToken("s1"); err != nil {
		t.Fatalf("RemoveToken: %v", err)
	}
	if err := s.RemoveToken("missing"); err != nil {
		t.Errorf("RemoveToken on missing id should be nil, got %v", err)
	}
	toks, _ = s.ListTokens()
	if len(toks) != 1 || toks[0].SessionID != "s2" {
		t.Errorf("after remove, got %+v, want only s2", toks)
	}
}

func TestListTokensSkipsMalformed(t *testing.T) {
	s := New(t.TempDir())
	if err := s.WriteToken(newTok("good")); err != nil {
		t.Fatal(err)
	}
	// Drop a malformed file directly into sessions/.
	if err := s.writeRaw("bad", []byte("{garbage")); err != nil {
		t.Fatal(err)
	}
	toks, err := s.ListTokens()
	if err != nil {
		t.Fatalf("ListTokens: %v", err)
	}
	if len(toks) != 1 || toks[0].SessionID != "good" {
		t.Errorf("malformed file not skipped: %+v", toks)
	}
}

func TestFlagLifecycle(t *testing.T) {
	s := New(t.TempDir())
	if s.HasFlag() {
		t.Fatal("flag should be absent initially")
	}
	if err := s.SetFlag(); err != nil {
		t.Fatalf("SetFlag: %v", err)
	}
	if !s.HasFlag() {
		t.Fatal("flag should be present after SetFlag")
	}
	if err := s.ClearFlag(); err != nil {
		t.Fatalf("ClearFlag: %v", err)
	}
	if s.HasFlag() {
		t.Fatal("flag should be absent after ClearFlag")
	}
	if err := s.ClearFlag(); err != nil {
		t.Errorf("ClearFlag when absent should be nil, got %v", err)
	}
}

func TestLockSerializes(t *testing.T) {
	s := New(t.TempDir())
	unlock, err := s.Lock()
	if err != nil {
		t.Fatalf("Lock: %v", err)
	}

	got := make(chan struct{})
	go func() {
		unlock2, err := s.Lock()
		if err == nil {
			unlock2()
		}
		close(got)
	}()

	select {
	case <-got:
		t.Fatal("second Lock acquired while first was held")
	case <-time.After(100 * time.Millisecond):
		// expected: still blocked
	}
	unlock()
	select {
	case <-got:
		// expected: acquired after release
	case <-time.After(time.Second):
		t.Fatal("second Lock never acquired after release")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/state/ -v`
Expected: FAIL — `undefined: New`.

- [ ] **Step 3: Write `internal/state/state.go`**

```go
// Package state manages the on-disk state directory: the token directory,
// the we-enabled flag, and the advisory file lock that serializes reconciles.
package state

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"syscall"

	"github.com/hok/agentawake/internal/token"
)

// Store is a handle to a state directory (real or a test temp dir).
type Store struct {
	base string
}

// New returns a Store rooted at base.
func New(base string) *Store { return &Store{base: base} }

// DefaultBase returns ~/.local/state/agentawake.
func DefaultBase() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".local", "state", "agentawake"), nil
}

func (s *Store) sessionsDir() string { return filepath.Join(s.base, "sessions") }
func (s *Store) flagPath() string    { return filepath.Join(s.base, "we-enabled") }
func (s *Store) lockPath() string    { return filepath.Join(s.base, "lock") }

// LogPath returns the path to the diagnostic log file.
func (s *Store) LogPath() string { return filepath.Join(s.base, "agentawake.log") }

func (s *Store) ensureDirs() error {
	return os.MkdirAll(s.sessionsDir(), 0o755)
}

// Lock acquires an exclusive advisory lock over the state directory and
// returns a function that releases it. Callers must invoke the returned
// function (typically via defer).
func (s *Store) Lock() (func(), error) {
	if err := s.ensureDirs(); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(s.lockPath(), os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		f.Close()
		return nil, err
	}
	return func() {
		syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
		f.Close()
	}, nil
}

// writeRaw writes arbitrary bytes as a session file; used by tests.
func (s *Store) writeRaw(sessionID string, data []byte) error {
	if err := s.ensureDirs(); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(s.sessionsDir(), sessionID), data, 0o644)
}

// WriteToken writes a token atomically (temp file + rename).
func (s *Store) WriteToken(t token.Token) error {
	if err := s.ensureDirs(); err != nil {
		return err
	}
	data, err := t.Marshal()
	if err != nil {
		return err
	}
	path := filepath.Join(s.sessionsDir(), t.SessionID)
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// RemoveToken removes a session's token file. A missing file is not an error.
func (s *Store) RemoveToken(sessionID string) error {
	err := os.Remove(filepath.Join(s.sessionsDir(), sessionID))
	if os.IsNotExist(err) {
		return nil
	}
	return err
}

// ListTokens returns all valid tokens. Malformed or unreadable files are skipped.
func (s *Store) ListTokens() ([]token.Token, error) {
	entries, err := os.ReadDir(s.sessionsDir())
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var tokens []token.Token
	for _, e := range entries {
		if e.IsDir() || strings.HasSuffix(e.Name(), ".tmp") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(s.sessionsDir(), e.Name()))
		if err != nil {
			continue
		}
		t, err := token.Unmarshal(data)
		if err != nil {
			continue
		}
		tokens = append(tokens, t)
	}
	sort.Slice(tokens, func(i, j int) bool {
		return tokens[i].SessionID < tokens[j].SessionID
	})
	return tokens, nil
}

// SetFlag records that agentawake itself enabled disablesleep.
func (s *Store) SetFlag() error {
	if err := s.ensureDirs(); err != nil {
		return err
	}
	tmp := s.flagPath() + ".tmp"
	if err := os.WriteFile(tmp, []byte("1\n"), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, s.flagPath())
}

// ClearFlag removes the we-enabled flag. A missing flag is not an error.
func (s *Store) ClearFlag() error {
	err := os.Remove(s.flagPath())
	if os.IsNotExist(err) {
		return nil
	}
	return err
}

// HasFlag reports whether the we-enabled flag is present.
func (s *Store) HasFlag() bool {
	_, err := os.Stat(s.flagPath())
	return err == nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/state/ -v`
Expected: PASS — all four tests.

- [ ] **Step 5: Commit**

```bash
git add internal/state/
git commit -m "feat(state): token directory, flag, and advisory lock"
```

---

## Task 4: `reconcile` pure decision function

**Files:**
- Create: `internal/reconcile/decide.go`
- Test: `internal/reconcile/decide_test.go`

- [ ] **Step 1: Write the failing test**

`internal/reconcile/decide_test.go`:
```go
package reconcile

import (
	"testing"
	"time"

	"github.com/hok/agentawake/internal/token"
)

var now = time.Date(2026, 5, 14, 12, 0, 0, 0, time.UTC)

func tok(id string, pid int, age time.Duration) token.Token {
	return token.Token{
		Agent:     token.AgentClaudeCode,
		PID:       pid,
		SessionID: id,
		StartedAt: now.Add(-age),
	}
}

// allAlive treats every positive pid as alive.
func allAlive(pid int) bool { return pid > 0 }

// noneAlive treats every pid as dead.
func noneAlive(pid int) bool { return false }

func baseInputs() Inputs {
	return Inputs{
		Now:     now,
		MaxAge:  4 * time.Hour,
		IsAlive: allAlive,
	}
}

func TestDecide_LiveSessionNoFlag_Enables(t *testing.T) {
	in := baseInputs()
	in.Tokens = []token.Token{tok("s1", 100, time.Minute)}
	in.FlagPresent = false
	in.SleepDisabled = false

	got := Decide(in)
	if got.Action != ActionEnable {
		t.Errorf("Action = %v, want ActionEnable", got.Action)
	}
	if len(got.Prune) != 0 {
		t.Errorf("Prune = %v, want empty", got.Prune)
	}
}

func TestDecide_NoSessionsWithFlag_Disables(t *testing.T) {
	in := baseInputs()
	in.Tokens = nil
	in.FlagPresent = true
	in.SleepDisabled = true

	if got := Decide(in); got.Action != ActionDisable {
		t.Errorf("Action = %v, want ActionDisable", got.Action)
	}
}

func TestDecide_DeadPidPrunedThenDisables(t *testing.T) {
	in := baseInputs()
	in.IsAlive = noneAlive
	in.Tokens = []token.Token{tok("dead", 100, time.Minute)}
	in.FlagPresent = true
	in.SleepDisabled = true

	got := Decide(in)
	if len(got.Prune) != 1 || got.Prune[0] != "dead" {
		t.Errorf("Prune = %v, want [dead]", got.Prune)
	}
	if got.Action != ActionDisable {
		t.Errorf("Action = %v, want ActionDisable", got.Action)
	}
}

func TestDecide_OverAgeTokenPruned(t *testing.T) {
	in := baseInputs()
	in.Tokens = []token.Token{tok("stale", 100, 5*time.Hour)} // alive pid but too old
	in.FlagPresent = true
	in.SleepDisabled = true

	got := Decide(in)
	if len(got.Prune) != 1 || got.Prune[0] != "stale" {
		t.Errorf("Prune = %v, want [stale]", got.Prune)
	}
	if got.Action != ActionDisable {
		t.Errorf("Action = %v, want ActionDisable", got.Action)
	}
}

func TestDecide_PidZeroGovernedByAgeOnly(t *testing.T) {
	in := baseInputs()
	in.IsAlive = noneAlive // would prune any real pid
	in.Tokens = []token.Token{tok("young", 0, time.Minute)}
	in.FlagPresent = true
	in.SleepDisabled = true

	got := Decide(in)
	if len(got.Prune) != 0 {
		t.Errorf("Prune = %v, want empty (pid 0, within max age)", got.Prune)
	}
	if got.Action != ActionNone {
		t.Errorf("Action = %v, want ActionNone", got.Action)
	}
}

func TestDecide_StuckOn_NoSessionsNoFlagButDisabled(t *testing.T) {
	in := baseInputs()
	in.Tokens = nil
	in.FlagPresent = false
	in.SleepDisabled = true

	if got := Decide(in); got.Action != ActionWarnStuck {
		t.Errorf("Action = %v, want ActionWarnStuck", got.Action)
	}
}

func TestDecide_NothingToDo(t *testing.T) {
	in := baseInputs()
	in.Tokens = nil
	in.FlagPresent = false
	in.SleepDisabled = false

	if got := Decide(in); got.Action != ActionNone {
		t.Errorf("Action = %v, want ActionNone", got.Action)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/reconcile/ -v`
Expected: FAIL — `undefined: Inputs`.

- [ ] **Step 3: Write `internal/reconcile/decide.go`**

```go
// Package reconcile decides and applies the system sleep state from the
// set of active session tokens.
package reconcile

import (
	"time"

	"github.com/hok/agentawake/internal/token"
)

// SleepAction is the decision reconcile reached about pmset disablesleep.
type SleepAction int

const (
	// ActionNone: leave pmset alone.
	ActionNone SleepAction = iota
	// ActionEnable: ensure disablesleep is on and the we-enabled flag is set.
	ActionEnable
	// ActionDisable: turn disablesleep off and clear the we-enabled flag.
	ActionDisable
	// ActionWarnStuck: disablesleep is on, no sessions, no flag — likely a
	// lost flag. Do not auto-clear; warn the user instead.
	ActionWarnStuck
)

func (a SleepAction) String() string {
	switch a {
	case ActionEnable:
		return "enable"
	case ActionDisable:
		return "disable"
	case ActionWarnStuck:
		return "warn-stuck"
	default:
		return "none"
	}
}

// Inputs is everything the pure decision needs. No I/O happens inside Decide.
type Inputs struct {
	Tokens        []token.Token
	FlagPresent   bool             // is the we-enabled flag set
	SleepDisabled bool             // current pmset disablesleep state
	Now           time.Time
	MaxAge        time.Duration    // tokens older than this are pruned
	IsAlive       func(pid int) bool
}

// Decision is the full output of Decide.
type Decision struct {
	Prune  []string    // session IDs whose token files should be removed
	Action SleepAction
}

// Decide is a pure function: given the current state it returns which tokens
// to prune and what to do with pmset. It performs no I/O.
func Decide(in Inputs) Decision {
	var live int
	var pruneIDs []string
	for _, t := range in.Tokens {
		dead := t.PID > 0 && !in.IsAlive(t.PID)
		overAge := in.Now.Sub(t.StartedAt) > in.MaxAge
		if dead || overAge {
			pruneIDs = append(pruneIDs, t.SessionID)
			continue
		}
		live++
	}

	var action SleepAction
	switch {
	case live > 0 && (!in.SleepDisabled || !in.FlagPresent):
		// Sessions are active but either sleep is not yet disabled or we have
		// no flag recording it. Enable is idempotent (pmset 1 + SetFlag).
		action = ActionEnable
	case live == 0 && in.FlagPresent:
		action = ActionDisable
	case live == 0 && !in.FlagPresent && in.SleepDisabled:
		action = ActionWarnStuck
	default:
		action = ActionNone
	}
	return Decision{Prune: pruneIDs, Action: action}
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/reconcile/ -v`
Expected: PASS — all seven tests.

- [ ] **Step 5: Commit**

```bash
git add internal/reconcile/
git commit -m "feat(reconcile): pure decision function"
```

---

## Task 5: `pid` package

**Files:**
- Create: `internal/pid/pid.go`
- Test: `internal/pid/pid_test.go`

- [ ] **Step 1: Write the failing test**

`internal/pid/pid_test.go`:
```go
package pid

import (
	"os"
	"testing"
)

// fakeTable is an injectable process table for tests.
type fakeTable map[int]ProcInfo

func (f fakeTable) Lookup(pid int) (ProcInfo, bool) {
	info, ok := f[pid]
	return info, ok
}

func TestIsAlive_SelfIsAlive(t *testing.T) {
	if !IsAlive(os.Getpid()) {
		t.Error("current process should be alive")
	}
}

func TestIsAlive_NonsensePidIsDead(t *testing.T) {
	if IsAlive(0) || IsAlive(-1) {
		t.Error("pid <= 0 must report dead")
	}
}

func TestDetect_PayloadPidPreferred(t *testing.T) {
	self := os.Getpid()
	got := Detect(self, 999, []string{"claude"}, fakeTable{})
	if got != self {
		t.Errorf("Detect = %d, want payload pid %d", got, self)
	}
}

func TestDetect_WalksToNamedAncestor(t *testing.T) {
	// tree: hook(50) -> sh(40) -> node/claude(30) -> launchd(1)
	tbl := fakeTable{
		50: {PPID: 40, Comm: "sh"},
		40: {PPID: 30, Comm: "node"},
		30: {PPID: 1, Comm: "launchd"},
	}
	got := Detect(0, 50, []string{"claude", "node"}, tbl)
	if got != 40 {
		t.Errorf("Detect = %d, want 40 (first ancestor matching 'node')", got)
	}
}

func TestDetect_FallsBackToStartPPID(t *testing.T) {
	tbl := fakeTable{
		50: {PPID: 40, Comm: "sh"},
		40: {PPID: 1, Comm: "bash"},
	}
	got := Detect(0, 50, []string{"claude"}, tbl)
	if got != 50 {
		t.Errorf("Detect = %d, want 50 (no match -> startPPID)", got)
	}
}

func TestDetect_ReturnsZeroWhenNothingUsable(t *testing.T) {
	got := Detect(0, 1, []string{"claude"}, fakeTable{})
	if got != 0 {
		t.Errorf("Detect = %d, want 0", got)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/pid/ -v`
Expected: FAIL — `undefined: ProcInfo`.

- [ ] **Step 3: Write `internal/pid/pid.go`**

```go
// Package pid resolves and checks the liveness of the agent process behind
// a hook invocation. PID detection is best-effort: the reconcile max-age cap
// is the correctness backstop, so imperfect detection degrades gracefully.
package pid

import (
	"os/exec"
	"strconv"
	"strings"
	"syscall"
)

// ProcInfo is the parent pid and command name of a process.
type ProcInfo struct {
	PPID int
	Comm string
}

// ProcTable looks up process info by pid.
type ProcTable interface {
	Lookup(pid int) (ProcInfo, bool)
}

// IsAlive reports whether a process with the given pid currently exists.
func IsAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	err := syscall.Kill(pid, 0)
	// nil: exists and we can signal it. EPERM: exists, owned by someone else.
	return err == nil || err == syscall.EPERM
}

// Detect resolves the agent process pid.
//
// payloadPID: a pid taken from the hook's stdin JSON, or 0 if none.
// startPPID:  the hook process's own parent pid (os.Getppid()).
// patterns:   command-name substrings identifying an agent process.
//
// Resolution order: payload pid -> first ancestor whose command matches a
// pattern -> startPPID -> 0.
func Detect(payloadPID, startPPID int, patterns []string, tbl ProcTable) int {
	if payloadPID > 0 && IsAlive(payloadPID) {
		return payloadPID
	}
	cur := startPPID
	for i := 0; i < 32 && cur > 1; i++ {
		info, ok := tbl.Lookup(cur)
		if !ok {
			break
		}
		for _, p := range patterns {
			if strings.Contains(info.Comm, p) {
				return cur
			}
		}
		cur = info.PPID
	}
	if startPPID > 1 {
		return startPPID
	}
	return 0
}

// psTable is the real ProcTable, backed by /bin/ps.
type psTable struct{}

func (psTable) Lookup(pid int) (ProcInfo, bool) {
	out, err := exec.Command("ps", "-o", "ppid=,comm=", "-p", strconv.Itoa(pid)).Output()
	if err != nil {
		return ProcInfo{}, false
	}
	fields := strings.Fields(strings.TrimSpace(string(out)))
	if len(fields) < 2 {
		return ProcInfo{}, false
	}
	ppid, err := strconv.Atoi(fields[0])
	if err != nil {
		return ProcInfo{}, false
	}
	return ProcInfo{PPID: ppid, Comm: strings.Join(fields[1:], " ")}, true
}

// SystemTable returns a ProcTable backed by the live process table.
func SystemTable() ProcTable { return psTable{} }
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/pid/ -v`
Expected: PASS — all six tests.

- [ ] **Step 5: Commit**

```bash
git add internal/pid/
git commit -m "feat(pid): liveness check and ancestor PID detection"
```

---

## Task 6: `pmset` package

**Files:**
- Create: `internal/pmset/pmset.go`
- Test: `internal/pmset/pmset_test.go`

- [ ] **Step 1: Write the failing test**

`internal/pmset/pmset_test.go`:
```go
package pmset

import "testing"

func TestParseDisabled(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want bool
	}{
		{"on", " SleepDisabled\t\t1\n Sleep On Power\t10\n", true},
		{"off", " SleepDisabled\t\t0\n", false},
		{"absent", " hibernatemode\t3\n", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := parseDisabled([]byte(c.in)); got != c.want {
				t.Errorf("parseDisabled(%q) = %v, want %v", c.in, got, c.want)
			}
		})
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/pmset/ -v`
Expected: FAIL — `undefined: parseDisabled`.

- [ ] **Step 3: Write `internal/pmset/pmset.go`**

```go
// Package pmset reads and changes macOS power-management sleep state.
// All writes go through a single privileged wrapper at WrapperPath.
package pmset

import (
	"os"
	"os/exec"
	"regexp"
)

// WrapperPath is the privileged sleep-toggle wrapper installed by `agentawake install`.
const WrapperPath = "/usr/local/sbin/agentawake-pmset"

var sleepDisabledRe = regexp.MustCompile(`(?m)^\s*SleepDisabled\s+(\d)`)

// parseDisabled extracts the SleepDisabled value from `pmset -g` output.
// A missing field is treated as off.
func parseDisabled(out []byte) bool {
	m := sleepDisabledRe.FindSubmatch(out)
	if m == nil {
		return false
	}
	return string(m[1]) == "1"
}

// IsDisabled reports whether pmset disablesleep is currently on.
func IsDisabled() (bool, error) {
	out, err := exec.Command("pmset", "-g").Output()
	if err != nil {
		return false, err
	}
	return parseDisabled(out), nil
}

// Set toggles disablesleep via the privileged wrapper. `sudo -n` is used so a
// missing sudoers rule fails fast instead of prompting.
func Set(on bool) error {
	arg := "0"
	if on {
		arg = "1"
	}
	return exec.Command("sudo", "-n", WrapperPath, arg).Run()
}

// WrapperInstalled reports whether the privileged wrapper exists as a file.
func WrapperInstalled() bool {
	info, err := os.Stat(WrapperPath)
	return err == nil && !info.IsDir()
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/pmset/ -v`
Expected: PASS — all three sub-tests.

- [ ] **Step 5: Commit**

```bash
git add internal/pmset/
git commit -m "feat(pmset): read sleep state and toggle via privileged wrapper"
```

---

## Task 7: `hookjson` package

**Files:**
- Create: `internal/hookjson/hookjson.go`
- Test: `internal/hookjson/hookjson_test.go`

- [ ] **Step 1: Write the failing test**

`internal/hookjson/hookjson_test.go`:
```go
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
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/hookjson/ -v`
Expected: FAIL — `undefined: Parse`.

- [ ] **Step 3: Write `internal/hookjson/hookjson.go`**

```go
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
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/hookjson/ -v`
Expected: PASS — all four tests.

- [ ] **Step 5: Commit**

```bash
git add internal/hookjson/
git commit -m "feat(hookjson): tolerant parser for hook stdin payloads"
```

---

## Task 8: `logging` package

**Files:**
- Create: `internal/logging/logging.go`
- Test: `internal/logging/logging_test.go`

- [ ] **Step 1: Write the failing test**

`internal/logging/logging_test.go`:
```go
package logging

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPrintfWritesLine(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.log")
	l := New(path)
	l.Printf("hello %s", "world")

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if !strings.Contains(string(data), "hello world") {
		t.Errorf("log missing message, got: %q", data)
	}
}

func TestRotatesWhenOversized(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.log")
	// Seed an oversized log file.
	big := strings.Repeat("x", maxSize+1)
	if err := os.WriteFile(path, []byte(big), 0o644); err != nil {
		t.Fatal(err)
	}
	l := New(path)
	l.Printf("after rotation")

	if _, err := os.Stat(path + ".1"); err != nil {
		t.Errorf("expected rotated file %s.1: %v", path, err)
	}
	data, _ := os.ReadFile(path)
	if !strings.Contains(string(data), "after rotation") || len(data) > 1000 {
		t.Errorf("current log should be small and fresh, got %d bytes", len(data))
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/logging/ -v`
Expected: FAIL — `undefined: New`.

- [ ] **Step 3: Write `internal/logging/logging.go`**

```go
// Package logging provides a tiny size-capped, rotating diagnostic log.
// agentawake never logs to stdout/stderr from hook commands, so this file is
// the only place failures surface.
package logging

import (
	"fmt"
	"os"
	"sync"
	"time"
)

// maxSize is the byte threshold at which the log rotates to <path>.1.
const maxSize = 1 << 20 // 1 MiB

// Logger is a concurrency-safe append logger with single-file rotation.
type Logger struct {
	path string
	mu   sync.Mutex
}

// New returns a Logger writing to path.
func New(path string) *Logger { return &Logger{path: path} }

// Printf appends a timestamped line. All errors are swallowed — logging must
// never be the reason a hook command fails.
func (l *Logger) Printf(format string, args ...any) {
	l.mu.Lock()
	defer l.mu.Unlock()

	if info, err := os.Stat(l.path); err == nil && info.Size() > maxSize {
		_ = os.Rename(l.path, l.path+".1")
	}
	f, err := os.OpenFile(l.path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return
	}
	defer f.Close()

	ts := time.Now().Format(time.RFC3339)
	fmt.Fprintf(f, "%s "+format+"\n", append([]any{ts}, args...)...)
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/logging/ -v`
Expected: PASS — both tests.

- [ ] **Step 5: Commit**

```bash
git add internal/logging/
git commit -m "feat(logging): size-capped rotating diagnostic log"
```

---

## Task 9: `reconcile.Run` orchestration

**Files:**
- Create: `internal/reconcile/run.go`
- Test: `internal/reconcile/run_test.go`

This task wires the pure `Decide` to real I/O. To keep `Run` testable without root or a real `pmset`, the side effects go behind a small `Sink` interface.

- [ ] **Step 1: Write the failing test**

`internal/reconcile/run_test.go`:
```go
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
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/reconcile/ -v`
Expected: FAIL — `undefined: RunWith`.

- [ ] **Step 3: Write `internal/reconcile/run.go`**

```go
package reconcile

import (
	"time"

	"github.com/hok/agentawake/internal/logging"
	"github.com/hok/agentawake/internal/pid"
	"github.com/hok/agentawake/internal/pmset"
	"github.com/hok/agentawake/internal/state"
)

// DefaultMaxAge bounds how long a single token can live before reconcile
// prunes it regardless of process liveness — the backstop for a missed Stop.
const DefaultMaxAge = 4 * time.Hour

// Sink is the side-effecting surface reconcile needs. The real implementation
// is pmsetSink; tests inject a fake.
type Sink interface {
	IsDisabled() (bool, error)
	Set(on bool) error
}

type pmsetSink struct{}

func (pmsetSink) IsDisabled() (bool, error) { return pmset.IsDisabled() }
func (pmsetSink) Set(on bool) error         { return pmset.Set(on) }

// Run performs a full reconcile cycle, taking the state lock for its entire
// duration (including the pmset call), then releasing it.
func Run(st *state.Store, log *logging.Logger) error {
	unlock, err := st.Lock()
	if err != nil {
		return err
	}
	defer unlock()
	return RunLocked(st, log)
}

// RunLocked is Run without taking the lock — the caller must already hold it.
// acquire/release use this so the token write and the reconcile happen under
// a single lock acquisition.
func RunLocked(st *state.Store, log *logging.Logger) error {
	return runLockedWith(st, log, pmsetSink{})
}

// RunWith is RunLocked-equivalent with an injectable Sink, for tests. It does
// not take the lock (tests are single-threaded).
func RunWith(st *state.Store, log *logging.Logger, sink Sink) error {
	return runLockedWith(st, log, sink)
}

func runLockedWith(st *state.Store, log *logging.Logger, sink Sink) error {
	tokens, err := st.ListTokens()
	if err != nil {
		return err
	}
	sleepDisabled, err := sink.IsDisabled()
	if err != nil {
		log.Printf("reconcile: read sleep state failed: %v", err)
		// Continue with sleepDisabled=false; reconcile is idempotent and the
		// next run corrects any mistake.
	}

	dec := Decide(Inputs{
		Tokens:        tokens,
		FlagPresent:   st.HasFlag(),
		SleepDisabled: sleepDisabled,
		Now:           time.Now(),
		MaxAge:        DefaultMaxAge,
		IsAlive:       pid.IsAlive,
	})

	for _, sid := range dec.Prune {
		if err := st.RemoveToken(sid); err != nil {
			log.Printf("reconcile: prune %s failed: %v", sid, err)
		}
	}

	switch dec.Action {
	case ActionEnable:
		if err := sink.Set(true); err != nil {
			log.Printf("reconcile: enable sleep-disable failed: %v", err)
			return err
		}
		if err := st.SetFlag(); err != nil {
			log.Printf("reconcile: set flag failed: %v", err)
		}
	case ActionDisable:
		if err := sink.Set(false); err != nil {
			log.Printf("reconcile: disable sleep-disable failed: %v", err)
			return err
		}
		if err := st.ClearFlag(); err != nil {
			log.Printf("reconcile: clear flag failed: %v", err)
		}
	case ActionWarnStuck:
		log.Printf("reconcile: WARNING disablesleep is ON but agentawake has " +
			"no active sessions and no record of enabling it — run `agentawake off` if unexpected")
	}
	return nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/reconcile/ -v`
Expected: PASS — all decision tests plus the three Run tests.

- [ ] **Step 5: Commit**

```bash
git add internal/reconcile/run.go internal/reconcile/run_test.go
git commit -m "feat(reconcile): lock-guarded orchestration with injectable sink"
```

---

## Task 10: `acquire` command

**Files:**
- Create: `internal/cli/acquire.go`
- Modify: `internal/cli/cli.go` (add `acquire` to dispatch + a shared `stores()` helper)
- Test: `internal/cli/cli_test.go`

- [ ] **Step 1: Write the failing test**

`internal/cli/cli_test.go`:
```go
package cli

import (
	"strings"
	"testing"

	"github.com/hok/agentawake/internal/state"
)

func TestAcquire_WritesTokenFromStdin(t *testing.T) {
	dir := t.TempDir()
	st := state.New(dir)

	code := runAcquire(
		[]string{"--agent", "claude-code"},
		strings.NewReader(`{"session_id":"sess-1"}`),
		st,
	)
	if code != 0 {
		t.Fatalf("runAcquire exit = %d, want 0", code)
	}
	toks, err := st.ListTokens()
	if err != nil {
		t.Fatalf("ListTokens: %v", err)
	}
	if len(toks) != 1 || toks[0].SessionID != "sess-1" {
		t.Fatalf("tokens = %+v, want one token sess-1", toks)
	}
	if string(toks[0].Agent) != "claude-code" {
		t.Errorf("agent = %q, want claude-code", toks[0].Agent)
	}
}

func TestAcquire_NeverFailsTheAgent(t *testing.T) {
	st := state.New(t.TempDir())
	// Garbage stdin must still exit 0 — a hook must never break the agent.
	code := runAcquire([]string{"--agent", "codex"}, strings.NewReader("garbage"), st)
	if code != 0 {
		t.Errorf("runAcquire exit = %d, want 0 even on bad input", code)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/cli/ -v`
Expected: FAIL — `undefined: runAcquire`.

- [ ] **Step 3: Add the shared helper to `internal/cli/cli.go`**

Add these imports to `internal/cli/cli.go` (merge with the existing `import` block):
```go
import (
	"fmt"
	"os"

	"github.com/hok/agentawake/internal/logging"
	"github.com/hok/agentawake/internal/state"
)
```

Add at the end of `internal/cli/cli.go`:
```go
// stores builds the default Store and Logger. Used by every command.
func stores() (*state.Store, *logging.Logger, error) {
	base, err := state.DefaultBase()
	if err != nil {
		return nil, nil, err
	}
	st := state.New(base)
	return st, logging.New(st.LogPath()), nil
}
```

In the `switch` in `Main`, add a case before `default`:
```go
	case "acquire":
		return cmdAcquire(args[1:])
```

- [ ] **Step 4: Write `internal/cli/acquire.go`**

```go
package cli

import (
	"flag"
	"io"
	"os"
	"time"

	"github.com/hok/agentawake/internal/hookjson"
	"github.com/hok/agentawake/internal/logging"
	"github.com/hok/agentawake/internal/pid"
	"github.com/hok/agentawake/internal/reconcile"
	"github.com/hok/agentawake/internal/state"
	"github.com/hok/agentawake/internal/token"
)

// agentPatterns are the process-name substrings used to find the agent process
// while walking up from the hook's parent pid.
var agentPatterns = []string{"claude", "codex", "node"}

// cmdAcquire is the entrypoint for `agentawake acquire`, invoked by the
// UserPromptSubmit hook. It must always exit 0 — a hook must never break the agent.
func cmdAcquire(args []string) int {
	st, _, err := stores()
	if err != nil {
		return 0
	}
	return runAcquire(args, os.Stdin, st)
}

// runAcquire is the testable core: args, a stdin reader, and a Store.
func runAcquire(args []string, stdin io.Reader, st *state.Store) int {
	fs := flag.NewFlagSet("acquire", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	agent := fs.String("agent", "", "claude-code or codex")
	if err := fs.Parse(args); err != nil {
		return 0
	}

	log := logging.New(st.LogPath())

	p, err := hookjson.Parse(stdin)
	if err != nil {
		log.Printf("acquire: parse stdin: %v", err)
		return 0
	}
	if p.SessionID == "" {
		log.Printf("acquire: no session id in hook payload")
		return 0
	}

	detected := pid.Detect(p.PID, os.Getppid(), agentPatterns, pid.SystemTable())
	tok := token.Token{
		Agent:     token.Agent(*agent),
		PID:       detected,
		SessionID: p.SessionID,
		StartedAt: time.Now().UTC(),
	}

	unlock, err := st.Lock()
	if err != nil {
		log.Printf("acquire: lock: %v", err)
		return 0
	}
	defer unlock()

	if err := st.WriteToken(tok); err != nil {
		log.Printf("acquire: write token: %v", err)
		return 0
	}
	if err := reconcile.RunLocked(st, log); err != nil {
		log.Printf("acquire: reconcile: %v", err)
	}
	return 0
}
```

- [ ] **Step 5: Run test to verify it passes**

Run: `go test ./internal/cli/ -v && go build ./...`
Expected: PASS — both tests; build succeeds.

- [ ] **Step 6: Commit**

```bash
git add internal/cli/
git commit -m "feat(cli): acquire command writes a token and reconciles"
```

---

## Task 11: `release` command

**Files:**
- Create: `internal/cli/release.go`
- Modify: `internal/cli/cli.go` (add `release` to dispatch)
- Test: `internal/cli/release_test.go`

- [ ] **Step 1: Write the failing test**

`internal/cli/release_test.go`:
```go
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
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/cli/ -run TestRelease -v`
Expected: FAIL — `undefined: runRelease`.

- [ ] **Step 3: Write `internal/cli/release.go`**

```go
package cli

import (
	"io"
	"os"

	"github.com/hok/agentawake/internal/hookjson"
	"github.com/hok/agentawake/internal/logging"
	"github.com/hok/agentawake/internal/reconcile"
	"github.com/hok/agentawake/internal/state"
)

// cmdRelease is the entrypoint for `agentawake release`, invoked by the Stop
// (and any turn-terminal) hook. It must always exit 0.
func cmdRelease(args []string) int {
	st, _, err := stores()
	if err != nil {
		return 0
	}
	return runRelease(os.Stdin, st)
}

// runRelease is the testable core: a stdin reader and a Store.
func runRelease(stdin io.Reader, st *state.Store) int {
	log := logging.New(st.LogPath())

	p, err := hookjson.Parse(stdin)
	if err != nil {
		log.Printf("release: parse stdin: %v", err)
		return 0
	}

	unlock, err := st.Lock()
	if err != nil {
		log.Printf("release: lock: %v", err)
		return 0
	}
	defer unlock()

	if p.SessionID == "" {
		log.Printf("release: no session id in hook payload; relying on prune")
	} else if err := st.RemoveToken(p.SessionID); err != nil {
		log.Printf("release: remove token %s: %v", p.SessionID, err)
	}

	if err := reconcile.RunLocked(st, log); err != nil {
		log.Printf("release: reconcile: %v", err)
	}
	return 0
}
```

- [ ] **Step 4: Add `release` to dispatch in `internal/cli/cli.go`**

In the `switch` in `Main`, add before `default`:
```go
	case "release":
		return cmdRelease(args[1:])
```

- [ ] **Step 5: Run test to verify it passes**

Run: `go test ./internal/cli/ -v && go build ./...`
Expected: PASS — all CLI tests; build succeeds.

- [ ] **Step 6: Commit**

```bash
git add internal/cli/
git commit -m "feat(cli): release command removes a token and reconciles"
```

---

## Task 12: `reconcile` command

**Files:**
- Create: `internal/cli/reconcile.go`
- Modify: `internal/cli/cli.go` (add `reconcile` to dispatch)

This command is what the launchd agent invokes. It is thin — the logic is already tested in `internal/reconcile`.

- [ ] **Step 1: Write `internal/cli/reconcile.go`**

```go
package cli

import "github.com/hok/agentawake/internal/reconcile"

// cmdReconcile is the entrypoint for `agentawake reconcile`, invoked every
// ~60s by the launchd agent and internally is not needed (acquire/release
// reconcile under their own lock). Exits 0 even on failure — it is a hook-like
// background command.
func cmdReconcile(args []string) int {
	st, log, err := stores()
	if err != nil {
		return 0
	}
	if err := reconcile.Run(st, log); err != nil {
		log.Printf("reconcile command: %v", err)
	}
	return 0
}
```

- [ ] **Step 2: Add `reconcile` to dispatch in `internal/cli/cli.go`**

In the `switch` in `Main`, add before `default`:
```go
	case "reconcile":
		return cmdReconcile(args[1:])
```

- [ ] **Step 3: Verify it builds and runs**

Run:
```bash
go build ./... && go run . reconcile && echo "exit: $?"
```
Expected: builds; `reconcile` runs and prints `exit: 0` (it will log a pmset error since nothing is installed yet — that is fine and expected).

- [ ] **Step 4: Commit**

```bash
git add internal/cli/reconcile.go internal/cli/cli.go
git commit -m "feat(cli): reconcile command for the launchd safety net"
```

---

## Task 13: `status` command

**Files:**
- Create: `internal/cli/status.go`
- Modify: `internal/cli/cli.go` (add `status` to dispatch)
- Test: `internal/cli/status_test.go`

Exit codes: `0` installed and consistent; `1` not installed; `2` installed but a "stuck on" inconsistency was detected.

- [ ] **Step 1: Write the failing test**

`internal/cli/status_test.go`:
```go
package cli

import (
	"strings"
	"testing"
	"time"

	"github.com/hok/agentawake/internal/state"
	"github.com/hok/agentawake/internal/token"
)

func TestStatus_ReportsActiveSessions(t *testing.T) {
	st := state.New(t.TempDir())
	tok := token.Token{
		Agent: token.AgentCodex, PID: 0, SessionID: "sess-x",
		StartedAt: time.Now().Add(-90 * time.Second).UTC(),
	}
	if err := st.WriteToken(tok); err != nil {
		t.Fatal(err)
	}

	var out strings.Builder
	// wrapperInstalled=true, sleepDisabled=true -> consistent (has a session)
	code := renderStatus(st, &out, statusFacts{wrapperInstalled: true, sleepDisabled: true})
	if code != 0 {
		t.Errorf("exit code = %d, want 0", code)
	}
	s := out.String()
	if !strings.Contains(s, "sess-x") || !strings.Contains(s, "codex") {
		t.Errorf("status output missing session details:\n%s", s)
	}
}

func TestStatus_NotInstalled(t *testing.T) {
	st := state.New(t.TempDir())
	var out strings.Builder
	code := renderStatus(st, &out, statusFacts{wrapperInstalled: false})
	if code != 1 {
		t.Errorf("exit code = %d, want 1 (not installed)", code)
	}
}

func TestStatus_StuckOnInconsistency(t *testing.T) {
	st := state.New(t.TempDir())
	// No tokens, no flag, but sleep is disabled -> stuck-on.
	var out strings.Builder
	code := renderStatus(st, &out, statusFacts{wrapperInstalled: true, sleepDisabled: true})
	if code != 2 {
		t.Errorf("exit code = %d, want 2 (stuck-on)", code)
	}
	if !strings.Contains(out.String(), "agentawake off") {
		t.Errorf("stuck-on output should advise `agentawake off`:\n%s", out.String())
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/cli/ -run TestStatus -v`
Expected: FAIL — `undefined: renderStatus`.

- [ ] **Step 3: Write `internal/cli/status.go`**

```go
package cli

import (
	"fmt"
	"io"
	"os"
	"time"

	"github.com/hok/agentawake/internal/pmset"
	"github.com/hok/agentawake/internal/state"
)

// statusFacts are the system observations status needs, gathered separately
// so renderStatus stays testable without a real pmset/wrapper.
type statusFacts struct {
	wrapperInstalled bool
	sleepDisabled    bool
}

// cmdStatus is the entrypoint for `agentawake status`.
func cmdStatus(args []string) int {
	st, _, err := stores()
	if err != nil {
		fmt.Fprintln(os.Stderr, "agentawake: cannot resolve state dir:", err)
		return 1
	}
	disabled, _ := pmset.IsDisabled()
	facts := statusFacts{
		wrapperInstalled: pmset.WrapperInstalled(),
		sleepDisabled:    disabled,
	}
	return renderStatus(st, os.Stdout, facts)
}

// renderStatus writes the human-readable status report and returns the exit code.
func renderStatus(st *state.Store, w io.Writer, facts statusFacts) int {
	if !facts.wrapperInstalled {
		fmt.Fprintln(w, "agentawake: not installed — run `agentawake install`")
		return 1
	}

	tokens, err := st.ListTokens()
	if err != nil {
		fmt.Fprintln(w, "agentawake: cannot read sessions:", err)
		return 1
	}

	fmt.Fprintf(w, "active: %d session(s)\n", len(tokens))
	now := time.Now()
	for _, tk := range tokens {
		dur := now.Sub(tk.StartedAt).Round(time.Second)
		fmt.Fprintf(w, "  %-12s pid %-7d running %s\n", tk.Agent, tk.PID, dur)
	}

	sleepState := "OFF"
	if facts.sleepDisabled {
		sleepState = "ON"
	}
	owned := ""
	if st.HasFlag() {
		owned = " (set by agentawake)"
	}
	fmt.Fprintf(w, "disablesleep: %s%s\n", sleepState, owned)

	// Stuck-on: sleep disabled, but no sessions and no flag recording it.
	if facts.sleepDisabled && len(tokens) == 0 && !st.HasFlag() {
		fmt.Fprintln(w, "")
		fmt.Fprintln(w, "WARNING: disablesleep is ON but agentawake has no active sessions")
		fmt.Fprintln(w, "and no record of enabling it. If this is unexpected, run: agentawake off")
		return 2
	}
	return 0
}
```

- [ ] **Step 4: Add `status` to dispatch in `internal/cli/cli.go`**

In the `switch` in `Main`, add before `default`:
```go
	case "status":
		return cmdStatus(args[1:])
```

- [ ] **Step 5: Run test to verify it passes**

Run: `go test ./internal/cli/ -v && go build ./...`
Expected: PASS — all status tests; build succeeds.

- [ ] **Step 6: Commit**

```bash
git add internal/cli/
git commit -m "feat(cli): status command with installed/stuck-on exit codes"
```

---

## Task 14: `off` command

**Files:**
- Create: `internal/cli/off.go`
- Modify: `internal/cli/cli.go` (add `off` to dispatch)
- Test: `internal/cli/off_test.go`

- [ ] **Step 1: Write the failing test**

`internal/cli/off_test.go`:
```go
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
	if len(setCalls) != 1 || setCalls[0] != false {
		t.Errorf("setCalls = %v, want [false]", setCalls)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/cli/ -run TestOff -v`
Expected: FAIL — `undefined: runOff`.

- [ ] **Step 3: Write `internal/cli/off.go`**

```go
package cli

import (
	"fmt"
	"os"

	"github.com/hok/agentawake/internal/pmset"
	"github.com/hok/agentawake/internal/state"
)

// cmdOff is the entrypoint for `agentawake off` — the emergency reset.
func cmdOff(args []string) int {
	st, _, err := stores()
	if err != nil {
		fmt.Fprintln(os.Stderr, "agentawake: cannot resolve state dir:", err)
		return 1
	}
	code := runOff(st, pmset.Set)
	if code == 0 {
		fmt.Println("agentawake: state cleared, normal sleep restored")
	}
	return code
}

// runOff clears all tokens and the flag, then forces sleep back on. setSleep is
// injected so tests do not touch the real pmset wrapper.
func runOff(st *state.Store, setSleep func(on bool) error) int {
	unlock, err := st.Lock()
	if err != nil {
		fmt.Fprintln(os.Stderr, "agentawake: lock:", err)
		return 1
	}
	defer unlock()

	tokens, _ := st.ListTokens()
	for _, tk := range tokens {
		_ = st.RemoveToken(tk.SessionID)
	}
	_ = st.ClearFlag()

	if err := setSleep(false); err != nil {
		fmt.Fprintln(os.Stderr, "agentawake: could not restore sleep:", err)
		return 1
	}
	return 0
}
```

- [ ] **Step 4: Add `off` to dispatch in `internal/cli/cli.go`**

In the `switch` in `Main`, add before `default`:
```go
	case "off":
		return cmdOff(args[1:])
```

- [ ] **Step 5: Run test to verify it passes**

Run: `go test ./internal/cli/ -v && go build ./...`
Expected: PASS — all CLI tests; build succeeds.

- [ ] **Step 6: Commit**

```bash
git add internal/cli/
git commit -m "feat(cli): off command for emergency state reset"
```

---

## Task 15: Embedded `agentawake-pmset` wrapper script

**Files:**
- Create: `internal/install/assets/agentawake-pmset.sh`
- Create: `internal/install/assets.go`

The asset lives *inside* the `internal/install` package directory because
`go:embed` can only embed files in or below the embedding package's directory.

- [ ] **Step 1: Create `internal/install/assets/agentawake-pmset.sh`**

```sh
#!/bin/sh
# agentawake-pmset — privileged sleep toggle.
# Installed root:wheel mode 0755 at /usr/local/sbin/agentawake-pmset.
# Granted passwordless via /etc/sudoers.d/agentawake. Accepts ONLY 0 or 1.
case "$1" in
  0|1) exec /usr/sbin/pmset -a disablesleep "$1" ;;
  *)   echo "usage: agentawake-pmset 0|1" >&2; exit 2 ;;
esac
```

- [ ] **Step 2: Create `internal/install/assets.go`**

```go
// Package install performs the privileged and user-level setup that wires
// agentawake into the system: the pmset wrapper, the sudoers rule, hook
// entries in the agent config files, and the launchd safety-net agent.
package install

import _ "embed"

// WrapperScript is the privileged sleep-toggle script, embedded at build time
// and written to disk by the root-setup step. The embed path is relative to
// this package's directory.
//
//go:embed assets/agentawake-pmset.sh
var WrapperScript []byte
```

- [ ] **Step 3: Verify it builds**

Run: `go build ./...`
Expected: builds — `install.WrapperScript` is populated from the embedded file.

- [ ] **Step 4: Commit**

```bash
git add internal/install/
git commit -m "feat(install): embed the privileged pmset wrapper script"
```

---

## Task 16: Parent-directory ownership safety check

**Files:**
- Create: `internal/install/pathcheck.go`
- Test: `internal/install/pathcheck_test.go`

A user-writable ancestor of `/usr/local/sbin` would let an attacker replace the privileged wrapper. `install` must refuse to proceed if any ancestor is unsafe.

- [ ] **Step 1: Write the failing test**

`internal/install/pathcheck_test.go`:
```go
package install

import (
	"os"
	"path/filepath"
	"testing"
)

func TestVerifySafeOwnership_RejectsWorldWritable(t *testing.T) {
	root := t.TempDir()
	bad := filepath.Join(root, "sbin")
	if err := os.Mkdir(bad, 0o755); err != nil {
		t.Fatal(err)
	}
	// Chmod explicitly: os.Mkdir's mode is subject to umask, so 0o777 at
	// creation would not reliably produce a world-writable dir.
	if err := os.Chmod(bad, 0o777); err != nil {
		t.Fatal(err)
	}
	if err := verifyDirSafe(bad); err == nil {
		t.Error("expected world-writable dir to be rejected")
	}
}

func TestVerifySafeOwnership_AcceptsTightDir(t *testing.T) {
	root := t.TempDir()
	good := filepath.Join(root, "sbin")
	if err := os.Mkdir(good, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := verifyDirSafe(good); err != nil {
		t.Errorf("tight dir (0755, owned by us) rejected: %v", err)
	}
}
```

Note: `verifyDirSafe` checks a single directory. The full ancestor walk (`VerifyInstallPath`) requires root-owned system paths and is exercised by the integration smoke test in Task 24, not by unit tests (CI cannot create root-owned dirs).

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/install/ -v`
Expected: FAIL — `undefined: verifyDirSafe`.

- [ ] **Step 3: Write `internal/install/pathcheck.go`**

```go
package install

import (
	"fmt"
	"os"
	"path/filepath"
	"syscall"
)

// verifyDirSafe reports an error if dir is group- or world-writable, or is not
// owned by root (uid 0) or the current user. This is the building block for
// VerifyInstallPath.
func verifyDirSafe(dir string) error {
	info, err := os.Stat(dir)
	if err != nil {
		return err
	}
	mode := info.Mode()
	if mode&0o022 != 0 {
		return fmt.Errorf("%s is group/world-writable (mode %o) — unsafe for a privileged binary", dir, mode.Perm())
	}
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return fmt.Errorf("%s: cannot read ownership", dir)
	}
	if st.Uid != 0 && int(st.Uid) != os.Getuid() {
		return fmt.Errorf("%s is owned by uid %d (not root or you) — unsafe", dir, st.Uid)
	}
	return nil
}

// VerifyInstallPath walks every ancestor of target up to "/" and ensures none
// is group/world-writable or owned by an untrusted user. Run before installing
// the privileged wrapper into target's directory.
func VerifyInstallPath(target string) error {
	dir := filepath.Dir(target)
	for {
		if err := verifyDirSafe(dir); err != nil {
			return err
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return nil // reached "/"
		}
		dir = parent
	}
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/install/ -v`
Expected: PASS — both tests.

- [ ] **Step 5: Commit**

```bash
git add internal/install/pathcheck.go internal/install/pathcheck_test.go
git commit -m "feat(install): parent-directory ownership safety check"
```

---

## Task 17: sudoers rendering

**Files:**
- Create: `internal/install/sudoers.go`
- Test: `internal/install/sudoers_test.go`

This task covers rendering only; the privileged atomic write happens in the root-setup step (Task 20), which is exercised by the integration smoke test.

- [ ] **Step 1: Write the failing test**

`internal/install/sudoers_test.go`:
```go
package install

import (
	"strings"
	"testing"
)

func TestRenderSudoers(t *testing.T) {
	got := renderSudoers("alice")
	want := "alice ALL=(root) NOPASSWD: /usr/local/sbin/agentawake-pmset\n"
	if got != want {
		t.Errorf("renderSudoers:\n got  %q\n want %q", got, want)
	}
}

func TestRenderSudoers_RejectsBadUsername(t *testing.T) {
	for _, bad := range []string{"", "al ice", "alice\nroot", "alice;rm"} {
		if _, err := RenderSudoersChecked(bad); err == nil {
			t.Errorf("RenderSudoersChecked(%q) should error", bad)
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/install/ -run TestRenderSudoers -v`
Expected: FAIL — `undefined: renderSudoers`.

- [ ] **Step 3: Write `internal/install/sudoers.go`**

```go
package install

import (
	"fmt"
	"regexp"

	"github.com/hok/agentawake/internal/pmset"
)

// SudoersPath is where the passwordless rule is installed.
const SudoersPath = "/etc/sudoers.d/agentawake"

// validUsername matches a conservative POSIX username: letters, digits,
// underscore, hyphen; must start with a letter or underscore.
var validUsername = regexp.MustCompile(`^[a-z_][a-z0-9_-]*$`)

// renderSudoers builds the sudoers line. Callers must pass a validated username.
func renderSudoers(user string) string {
	return fmt.Sprintf("%s ALL=(root) NOPASSWD: %s\n", user, pmset.WrapperPath)
}

// RenderSudoersChecked validates the username before rendering, to keep
// untrusted input out of the sudoers file.
func RenderSudoersChecked(user string) (string, error) {
	if !validUsername.MatchString(user) {
		return "", fmt.Errorf("refusing to write sudoers rule for unsafe username %q", user)
	}
	return renderSudoers(user), nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/install/ -v`
Expected: PASS — both sudoers tests plus the pathcheck tests.

- [ ] **Step 5: Commit**

```bash
git add internal/install/sudoers.go internal/install/sudoers_test.go
git commit -m "feat(install): sudoers rule rendering with username validation"
```

---

## Task 18: Hook config merge

**Files:**
- Create: `internal/install/hooks.go`
- Test: `internal/install/hooks_test.go`

This merges agentawake's hook entries into `~/.claude/settings.json` and `~/.codex/hooks.json` without disturbing existing hooks, and removes them on uninstall. Both files use the same shape: a top-level `hooks` object whose keys are event names mapping to arrays of hook groups.

- [ ] **Step 1: Write the failing test**

`internal/install/hooks_test.go`:
```go
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
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/install/ -run Hooks -v`
Expected: FAIL — `undefined: MergeHooks`.

- [ ] **Step 3: Write `internal/install/hooks.go`**

```go
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
		hooksRaw[event] = filterOutMarked(arr)
	}
	root["hooks"] = hooksRaw
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

func writeJSONObject(path string, root map[string]any) error {
	data, err := json.MarshalIndent(root, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func childObject(parent map[string]any, key string) map[string]any {
	if existing, ok := parent[key].(map[string]any); ok {
		return existing
	}
	return map[string]any{}
}

// addEntry appends a hook entry for command under event, unless an entry with
// that exact command is already present (idempotency).
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
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/install/ -v`
Expected: PASS — all hooks tests plus earlier install tests.

- [ ] **Step 5: Commit**

```bash
git add internal/install/hooks.go internal/install/hooks_test.go
git commit -m "feat(install): idempotent hook config merge and removal"
```

---

## Task 19: launchd agent

**Files:**
- Create: `internal/install/launchd.go`
- Test: `internal/install/launchd_test.go`

- [ ] **Step 1: Write the failing test**

`internal/install/launchd_test.go`:
```go
package install

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRenderPlist(t *testing.T) {
	plist := renderPlist("/opt/bin/agentawake")
	for _, want := range []string{
		"com.agentawake.reconcile",
		"<string>/opt/bin/agentawake</string>",
		"<string>reconcile</string>",
		"<integer>60</integer>",
	} {
		if !strings.Contains(plist, want) {
			t.Errorf("plist missing %q:\n%s", want, plist)
		}
	}
}

func TestWritePlist(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "com.agentawake.reconcile.plist")
	if err := writePlistTo(path, "/opt/bin/agentawake"); err != nil {
		t.Fatalf("writePlistTo: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if !strings.Contains(string(data), "com.agentawake.reconcile") {
		t.Errorf("written plist missing label:\n%s", data)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/install/ -run Plist -v`
Expected: FAIL — `undefined: renderPlist`.

- [ ] **Step 3: Write `internal/install/launchd.go`**

```go
package install

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

// LaunchdLabel is the launchd job label for the reconcile safety-net agent.
const LaunchdLabel = "com.agentawake.reconcile"

// plistTemplate is the LaunchAgent definition. %s placeholders are, in order:
// label, binary path, reconcile interval (seconds).
const plistTemplate = `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>Label</key>
  <string>%s</string>
  <key>ProgramArguments</key>
  <array>
    <string>%s</string>
    <string>reconcile</string>
  </array>
  <key>StartInterval</key>
  <integer>%d</integer>
  <key>RunAtLoad</key>
  <true/>
</dict>
</plist>
`

// renderPlist builds the LaunchAgent plist XML for the given binary path.
func renderPlist(binaryPath string) string {
	return fmt.Sprintf(plistTemplate, LaunchdLabel, binaryPath, 60)
}

// PlistPath returns ~/Library/LaunchAgents/com.agentawake.reconcile.plist.
func PlistPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, "Library", "LaunchAgents", LaunchdLabel+".plist"), nil
}

func writePlistTo(path, binaryPath string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(renderPlist(binaryPath)), 0o644)
}

// InstallLaunchd writes the plist and loads it with launchctl.
func InstallLaunchd(binaryPath string) error {
	path, err := PlistPath()
	if err != nil {
		return err
	}
	if err := writePlistTo(path, binaryPath); err != nil {
		return err
	}
	// `load -w` is the widely-compatible form; ignore "already loaded" noise.
	_ = exec.Command("launchctl", "unload", path).Run()
	return exec.Command("launchctl", "load", "-w", path).Run()
}

// UninstallLaunchd unloads and removes the launchd agent. A missing plist is
// not an error.
func UninstallLaunchd() error {
	path, err := PlistPath()
	if err != nil {
		return err
	}
	_ = exec.Command("launchctl", "unload", path).Run()
	err = os.Remove(path)
	if os.IsNotExist(err) {
		return nil
	}
	return err
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/install/ -v`
Expected: PASS — both plist tests plus all earlier install tests.

- [ ] **Step 5: Commit**

```bash
git add internal/install/launchd.go internal/install/launchd_test.go
git commit -m "feat(install): launchd reconcile agent install/uninstall"
```

---

## Task 20: `_root-setup` and `_root-teardown` hidden subcommands

**Files:**
- Create: `internal/cli/install.go`
- Modify: `internal/cli/cli.go` (add hidden `_root-setup` / `_root-teardown` to dispatch)

These run as root (re-invoked via `sudo` by the user-facing `install`/`uninstall`). They are the only code that touches `/usr/local/sbin` and `/etc/sudoers.d`. They are deliberately omitted from `usage`.

- [ ] **Step 1: Write `internal/cli/install.go`**

```go
package cli

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/hok/agentawake/internal/install"
	"github.com/hok/agentawake/internal/pmset"
)

// cmdRootSetup performs the privileged half of `install`. It MUST run as root
// (the user-facing `install` re-invokes it via sudo). Args: --user <name>.
func cmdRootSetup(args []string) int {
	if os.Geteuid() != 0 {
		fmt.Fprintln(os.Stderr, "_root-setup must run as root")
		return 1
	}
	user := flagValue(args, "--user")
	if user == "" {
		fmt.Fprintln(os.Stderr, "_root-setup: --user is required")
		return 1
	}

	// 1. Refuse to install into an unsafe location.
	if err := install.VerifyInstallPath(pmset.WrapperPath); err != nil {
		// The parent dir may simply not exist yet; create it safely first.
		dir := filepath.Dir(pmset.WrapperPath)
		if mkErr := os.MkdirAll(dir, 0o755); mkErr != nil {
			fmt.Fprintln(os.Stderr, "_root-setup: cannot create", dir, ":", mkErr)
			return 1
		}
		_ = os.Chown(dir, 0, 0)
		if err := install.VerifyInstallPath(pmset.WrapperPath); err != nil {
			fmt.Fprintln(os.Stderr, "_root-setup: unsafe install path:", err)
			return 1
		}
	}

	// 2. Install the wrapper: write, chown root:wheel, chmod 0755.
	if err := os.WriteFile(pmset.WrapperPath, install.WrapperScript, 0o755); err != nil {
		fmt.Fprintln(os.Stderr, "_root-setup: write wrapper:", err)
		return 1
	}
	if err := os.Chown(pmset.WrapperPath, 0, 0); err != nil {
		fmt.Fprintln(os.Stderr, "_root-setup: chown wrapper:", err)
		return 1
	}
	if err := os.Chmod(pmset.WrapperPath, 0o755); err != nil {
		fmt.Fprintln(os.Stderr, "_root-setup: chmod wrapper:", err)
		return 1
	}

	// 3. Self-check: toggle disablesleep on then off and confirm it reads back.
	if err := exec.Command(pmset.WrapperPath, "1").Run(); err != nil {
		fmt.Fprintln(os.Stderr, "_root-setup: self-check enable failed:", err)
		return 1
	}
	on, err := pmset.IsDisabled()
	if err != nil || !on {
		fmt.Fprintln(os.Stderr, "_root-setup: self-check could not confirm disablesleep took effect")
		_ = exec.Command(pmset.WrapperPath, "0").Run()
		return 1
	}
	if err := exec.Command(pmset.WrapperPath, "0").Run(); err != nil {
		fmt.Fprintln(os.Stderr, "_root-setup: self-check restore failed:", err)
		return 1
	}

	// 4. Write the sudoers rule atomically: temp file -> visudo -c -> 0440 -> mv.
	if err := install.WriteSudoers(user); err != nil {
		fmt.Fprintln(os.Stderr, "_root-setup: sudoers:", err)
		return 1
	}

	fmt.Println("_root-setup: ok")
	return 0
}

// cmdRootTeardown removes the privileged bits. MUST run as root.
func cmdRootTeardown(args []string) int {
	if os.Geteuid() != 0 {
		fmt.Fprintln(os.Stderr, "_root-teardown must run as root")
		return 1
	}
	var failed bool
	if err := os.Remove(install.SudoersPath); err != nil && !os.IsNotExist(err) {
		fmt.Fprintln(os.Stderr, "_root-teardown: remove sudoers:", err)
		failed = true
	}
	if err := os.Remove(pmset.WrapperPath); err != nil && !os.IsNotExist(err) {
		fmt.Fprintln(os.Stderr, "_root-teardown: remove wrapper:", err)
		failed = true
	}
	if failed {
		return 1
	}
	fmt.Println("_root-teardown: ok")
	return 0
}

// flagValue is a tiny positional flag reader: returns the arg after `name`.
func flagValue(args []string, name string) string {
	for i, a := range args {
		if a == name && i+1 < len(args) {
			return args[i+1]
		}
	}
	return ""
}
```

- [ ] **Step 2: Write `internal/install/WriteSudoers`**

Append to `internal/install/sudoers.go`:
```go
// WriteSudoers atomically installs the passwordless rule: render -> temp file
// -> `visudo -c` validation -> chmod 0440, chown root:wheel -> rename into place.
// Must be called as root.
func WriteSudoers(user string) error {
	content, err := RenderSudoersChecked(user)
	if err != nil {
		return err
	}
	tmp := SudoersPath + ".tmp"
	if err := os.WriteFile(tmp, []byte(content), 0o440); err != nil {
		return err
	}
	defer os.Remove(tmp) // no-op if the rename below succeeded

	if out, err := exec.Command("visudo", "-c", "-f", tmp).CombinedOutput(); err != nil {
		return fmt.Errorf("visudo validation failed: %v: %s", err, out)
	}
	if err := os.Chown(tmp, 0, 0); err != nil {
		return fmt.Errorf("chown sudoers temp: %w", err)
	}
	if err := os.Chmod(tmp, 0o440); err != nil {
		return fmt.Errorf("chmod sudoers temp: %w", err)
	}
	return os.Rename(tmp, SudoersPath)
}
```

Add the imports `os` and `os/exec` to `internal/install/sudoers.go`'s import block:
```go
import (
	"fmt"
	"os"
	"os/exec"
	"regexp"

	"github.com/hok/agentawake/internal/pmset"
)
```

- [ ] **Step 3: Add hidden subcommands to dispatch in `internal/cli/cli.go`**

In the `switch` in `Main`, add before `default`:
```go
	case "_root-setup":
		return cmdRootSetup(args[1:])
	case "_root-teardown":
		return cmdRootTeardown(args[1:])
```
(These are intentionally not listed in `usage` — they are internal.)

- [ ] **Step 4: Verify it builds**

Run: `go build ./... && go test ./internal/install/ -v`
Expected: builds; existing install tests still PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/cli/install.go internal/cli/cli.go internal/install/sudoers.go
git commit -m "feat(cli): root-setup/root-teardown privileged subcommands"
```

---

## Task 21: `install` command

**Files:**
- Modify: `internal/cli/install.go` (add `cmdInstall`)
- Modify: `internal/cli/cli.go` (add `install` to dispatch)

`install` runs as the user and orchestrates: one `sudo` re-invocation for the privileged half, then user-level hook merge and launchd setup.

- [ ] **Step 1: Add `cmdInstall` to `internal/cli/install.go`**

Append to `internal/cli/install.go`:
```go
// claudeConfigPath / codexConfigPath return the per-tool hook config files.
func claudeConfigPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".claude", "settings.json"), nil
}
func codexConfigPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".codex", "hooks.json"), nil
}

// cmdInstall wires agentawake into the system. It asks for a password exactly
// once, for the privileged `_root-setup` re-invocation.
func cmdInstall(args []string) int {
	self, err := os.Executable()
	if err != nil {
		fmt.Fprintln(os.Stderr, "install: cannot find own path:", err)
		return 1
	}
	user := os.Getenv("USER")
	if user == "" {
		fmt.Fprintln(os.Stderr, "install: $USER is empty")
		return 1
	}

	// 1. Privileged half — one sudo prompt.
	fmt.Println("agentawake: requesting administrator access (once) to install the sleep toggle...")
	root := exec.Command("sudo", self, "_root-setup", "--user", user)
	root.Stdin, root.Stdout, root.Stderr = os.Stdin, os.Stdout, os.Stderr
	if err := root.Run(); err != nil {
		fmt.Fprintln(os.Stderr, "install: privileged setup failed:", err)
		return 1
	}

	// 2. Hook config merge (user-level). Codex is best-effort: if the config
	//    directory does not exist, skip it with a notice.
	claudePath, _ := claudeConfigPath()
	if err := install.MergeHooks(claudePath, "claude-code"); err != nil {
		fmt.Fprintln(os.Stderr, "install: claude hooks:", err)
		return 1
	}
	fmt.Println("agentawake: wired Claude Code hooks ->", claudePath)

	codexPath, _ := codexConfigPath()
	if _, statErr := os.Stat(filepath.Dir(codexPath)); statErr == nil {
		if err := install.MergeHooks(codexPath, "codex"); err != nil {
			fmt.Fprintln(os.Stderr, "install: codex hooks:", err)
			return 1
		}
		fmt.Println("agentawake: wired Codex hooks ->", codexPath)
	} else {
		fmt.Println("agentawake: ~/.codex not found — skipping Codex (re-run install after installing Codex)")
	}

	// 3. launchd safety-net agent (user-level).
	if err := install.InstallLaunchd(self); err != nil {
		fmt.Fprintln(os.Stderr, "install: launchd agent:", err)
		return 1
	}
	fmt.Println("agentawake: installed launchd reconcile agent")

	fmt.Println("agentawake: install complete. Restart any running Claude Code / Codex sessions to pick up the hooks.")
	return 0
}
```

- [ ] **Step 2: Add `install` to dispatch in `internal/cli/cli.go`**

In the `switch` in `Main`, add before `default`:
```go
	case "install":
		return cmdInstall(args[1:])
```

- [ ] **Step 3: Verify it builds**

Run: `go build ./...`
Expected: builds cleanly.

- [ ] **Step 4: Commit**

```bash
git add internal/cli/install.go internal/cli/cli.go
git commit -m "feat(cli): install command orchestrates privileged + user setup"
```

---

## Task 22: `uninstall` command

**Files:**
- Modify: `internal/cli/install.go` (add `cmdUninstall`)
- Modify: `internal/cli/cli.go` (add `uninstall` to dispatch)

Order matters: restore sleep *first* (while the wrapper still exists), then remove user-level wiring, then remove the privileged bits.

- [ ] **Step 1: Add `cmdUninstall` to `internal/cli/install.go`**

Append to `internal/cli/install.go`:
```go
// cmdUninstall reverses install in safe order: restore sleep, remove hooks,
// remove launchd agent, then remove the privileged bits (one sudo prompt).
func cmdUninstall(args []string) int {
	self, err := os.Executable()
	if err != nil {
		fmt.Fprintln(os.Stderr, "uninstall: cannot find own path:", err)
		return 1
	}

	// 1. Restore normal sleep while the wrapper is still installed.
	st, _, err := stores()
	if err == nil {
		runOff(st, pmset.Set) // best-effort; clears tokens + flag + disablesleep 0
	}

	// 2. Remove hook entries (user-level).
	if claudePath, e := claudeConfigPath(); e == nil {
		if err := install.RemoveHooks(claudePath); err != nil {
			fmt.Fprintln(os.Stderr, "uninstall: claude hooks:", err)
		}
	}
	if codexPath, e := codexConfigPath(); e == nil {
		if err := install.RemoveHooks(codexPath); err != nil {
			fmt.Fprintln(os.Stderr, "uninstall: codex hooks:", err)
		}
	}

	// 3. Remove the launchd agent (user-level).
	if err := install.UninstallLaunchd(); err != nil {
		fmt.Fprintln(os.Stderr, "uninstall: launchd agent:", err)
	}

	// 4. Remove the privileged bits — one sudo prompt.
	fmt.Println("agentawake: requesting administrator access (once) to remove the sleep toggle...")
	root := exec.Command("sudo", self, "_root-teardown")
	root.Stdin, root.Stdout, root.Stderr = os.Stdin, os.Stdout, os.Stderr
	if err := root.Run(); err != nil {
		fmt.Fprintln(os.Stderr, "uninstall: privileged teardown failed:", err)
		return 1
	}

	fmt.Println("agentawake: uninstall complete.")
	return 0
}
```

- [ ] **Step 2: Add `uninstall` to dispatch in `internal/cli/cli.go`**

In the `switch` in `Main`, add before `default`:
```go
	case "uninstall":
		return cmdUninstall(args[1:])
```

- [ ] **Step 3: Verify it builds and the full suite passes**

Run: `go build ./... && go test ./...`
Expected: builds; all tests across all packages PASS.

- [ ] **Step 4: Commit**

```bash
git add internal/cli/install.go internal/cli/cli.go
git commit -m "feat(cli): uninstall command with safe teardown ordering"
```

---

## Task 23: Distribution — goreleaser + Homebrew tap

**Files:**
- Create: `.goreleaser.yaml`
- Create: `LICENSE`

- [ ] **Step 1: Create `LICENSE`**

Run:
```bash
cd ~/work/projects/agentawake
curl -s https://raw.githubusercontent.com/spdx/license-list-data/main/text/MIT.txt -o LICENSE
```
Then open `LICENSE` and replace the `<year>` / `<copyright holders>` placeholders with `2026 hok` (or your name).

- [ ] **Step 2: Create `.goreleaser.yaml`**

```yaml
version: 2
project_name: agentawake

before:
  hooks:
    - go mod tidy

builds:
  - main: .
    binary: agentawake
    env:
      - CGO_ENABLED=0
    goos:
      - darwin
    goarch:
      - amd64
      - arm64
    ldflags:
      - -s -w -X github.com/hok/agentawake/internal/cli.Version={{.Version}}

archives:
  - formats: [tar.gz]
    name_template: "{{ .ProjectName }}_{{ .Version }}_{{ .Os }}_{{ .Arch }}"

checksum:
  name_template: "checksums.txt"

changelog:
  use: github

brews:
  - repository:
      owner: hok
      name: homebrew-tap
    homepage: "https://github.com/hok/agentawake"
    description: "Keep your Mac awake only while a Claude Code or Codex agent turn is running"
    license: "MIT"
    install: |
      bin.install "agentawake"
    caveats: |
      Run `agentawake install` once to wire up the hooks and the privileged
      sleep toggle. It asks for your password a single time.

release:
  github:
    owner: hok
    name: agentawake
```

- [ ] **Step 3: Validate the goreleaser config**

Run:
```bash
go install github.com/goreleaser/goreleaser/v2@latest
goreleaser check
```
Expected: `1 configuration file(s) validated` with no errors. (If `goreleaser` is already installed, skip the `go install`.)

- [ ] **Step 4: Verify a local snapshot build works**

Run:
```bash
goreleaser build --snapshot --clean --single-target
```
Expected: produces a binary under `dist/`. Confirm: `./dist/agentawake_darwin_*/agentawake version` prints a snapshot version string.

- [ ] **Step 5: Commit**

```bash
git add .goreleaser.yaml LICENSE
git commit -m "build: goreleaser config and Homebrew tap release pipeline"
```

Note: the `homebrew-tap` repository (`github.com/hok/homebrew-tap`) must exist before the first real release; goreleaser pushes the formula into it. Creating that repo is a one-time manual step outside this plan.

---

## Task 24: README and final verification

**Files:**
- Create: `README.md`

- [ ] **Step 1: Create `README.md`**

```markdown
# agentawake

Keep your Mac awake — **including with the lid closed** — but *only* while a
Claude Code or Codex agent turn is actively running. When the agent is idle,
your Mac sleeps normally.

Existing keep-awake tools either don't work with the lid closed
(`caffeinate`, KeepingYouAwake) or trigger on "app is running" — which keeps you
awake all day just because the editor is open. `agentawake` hooks into the
agent's turn lifecycle, so wakefulness tracks *actual work*.

## How it works

- `UserPromptSubmit` hook → a turn starts → a token is written.
- `Stop` hook → the turn ends → the token is removed.
- An idempotent `reconcile` (run from every hook and from a ~60s launchd agent)
  prunes dead/abandoned tokens and toggles `pmset disablesleep` through a tiny
  passwordless-sudo root wrapper.

A dead, crashed, or interrupted turn is cleaned up automatically (process-liveness
check + a max-age cap), so sleep is never left disabled indefinitely.

## Install

```sh
brew install hok/tap/agentawake
agentawake install   # wires hooks + the privileged toggle; asks for your password once
```

Restart any running Claude Code / Codex sessions afterwards so they pick up the hooks.

## Commands

| Command | Purpose |
|---|---|
| `agentawake install` / `uninstall` | Set up / remove everything (one password prompt) |
| `agentawake status` | Show active sessions and current sleep state |
| `agentawake off` | Emergency reset: clear state, restore normal sleep |
| `agentawake reconcile` | Re-sync (used by the launchd agent) |

## Verified configuration

The `pmset disablesleep` mechanism is undocumented by Apple. It was verified on
**Apple M4 Pro, macOS 26.5**. `agentawake install` runs a self-check and refuses
to proceed if the mechanism does not behave as expected on your machine.

## License

MIT
```

- [ ] **Step 2: Run the full test suite and build**

Run:
```bash
go test ./... && go vet ./... && go build ./...
```
Expected: all tests PASS, `go vet` clean, build succeeds.

- [ ] **Step 3: Manual integration smoke test**

This exercises the privileged paths that unit tests cannot. Run on the target Mac:
```bash
go build -o /tmp/agentawake .
/tmp/agentawake status                       # expect: "not installed", exit 1
sudo cp /tmp/agentawake /usr/local/bin/agentawake   # so `os.Executable()` is stable
agentawake install                            # expect: one password prompt, "install complete"
agentawake status                             # expect: "0 session(s)", "disablesleep: OFF", exit 0
echo '{"session_id":"smoke-1"}' | agentawake acquire --agent claude-code
agentawake status                             # expect: 1 session, "disablesleep: ON (set by agentawake)"
echo '{"session_id":"smoke-1"}' | agentawake release
agentawake status                             # expect: "0 session(s)", "disablesleep: OFF"
agentawake uninstall                           # expect: one password prompt, "uninstall complete"
```
Confirm each "expect" matches. If `acquire` did not flip `disablesleep` to ON,
check `~/.local/state/agentawake/agentawake.log`.

- [ ] **Step 4: Commit**

```bash
git add README.md go.mod
git commit -m "docs: add README; finalize agentawake v1"
```

(The project is stdlib-only, so there is no `go.sum` to commit.)

---

## Self-review notes

- **Spec coverage:** Mechanism self-check → Task 20 step 3. Hooked-events model
  (acquire/release) → Tasks 10–11; `install` wires `UserPromptSubmit`/`Stop`
  (Task 18). Permission-wait policy is "token stays" — implemented by *not*
  hooking `PermissionRequest` (Task 18 only adds the two events). Max-age cap →
  Task 4 + Task 9. `we-enabled` flag + stuck-on detection → Tasks 4, 9, 13.
  PID detection order → Task 5. flock-across-sudo → Task 9 (`Run` holds the lock
  around the sink call) + Tasks 10–11 (`RunLocked` under the caller's lock).
  Atomic sudoers `0440` → Task 20. Path-component ownership check → Task 16.
  Uninstall ordering → Task 22. `status` exit codes → Task 13. Log rotation →
  Task 8. Distribution → Task 23.
- **Open items from the spec** (exact hook JSON field names; turn-terminal
  events beyond `Stop`; whether the payload carries a PID) are handled by the
  *tolerant* `hookjson` parser (Task 7) and verified during the Task 24 smoke
  test; if `Stop` proves unreliable, `install` (Task 18) is where additional
  turn-terminal events get wired — no code-structure change needed.
- **Deferred decision resolved:** the pmset wrapper is a standalone embedded
  shell script (Task 15), not a hidden subcommand — smallest possible audit
  surface.
```
