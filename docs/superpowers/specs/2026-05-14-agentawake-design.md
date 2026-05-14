# agentawake — Design

**Date:** 2026-05-14
**Status:** Revised after spec review — ready for implementation planning

## Problem

A MacBook sleeps on lid close. When Claude Code or Codex is running a long agent
turn (thinking, tool calls, subagents), closing the lid — or just walking away —
suspends the machine and kills the work in progress.

Existing tools do not solve this:

- `caffeinate` and **KeepingYouAwake** only work with the lid open.
- **Amphetamine** handles closed-lid, but its triggers are "while app is running"
  (process exists) or "CPU threshold". Neither matches the requirement: Claude
  Code may sit open and idle for hours — that must **not** block sleep. Only an
  **actively computing turn** should.

The distinguishing requirement: keep the Mac awake **only while an agent turn is
actively running**, including with the lid closed, for both Claude Code and Codex.

## Mechanism — verified

The whole project depends on one OS mechanism: `sudo pmset -a disablesleep 1`
keeping the machine awake with the lid **closed**, and `disablesleep 0` restoring
normal lid-close sleep.

`disablesleep` is undocumented (absent from `man pmset`), so it was verified
empirically before this design was accepted:

- **Verified on:** Apple M4 Pro, macOS 26.5 (build 25F71), Mac16,8. With
  `disablesleep 1` the machine stayed awake (audio kept playing) through a lid
  close; with `disablesleep 0` it slept normally on lid close.
- **Not assumed beyond that.** Older OS versions / other chip families are not
  guaranteed. `install` performs a runtime self-check (set the value, read it
  back via `pmset -g`, confirm it took) and refuses / warns if the mechanism does
  not behave as expected on the host. The README states the verified
  configuration.

## Goal

A single Go CLI, `agentawake`, that:

- Detects active agent turns precisely via the CLI tools' lifecycle hooks.
- Disables system sleep (including clamshell/lid-closed) while any turn is active.
- Restores normal sleep when no turn is active.
- **Self-heals:** an abandoned, crashed, or interrupted turn is cleaned up
  automatically by PID-liveness pruning and a max-age cap, so sleep is not left
  disabled indefinitely. (One residual edge case — loss of the `we-enabled`
  state file — cannot self-heal and is handled by a loud warning + manual reset;
  see Error handling.)
- Handles multiple concurrent sessions correctly (Claude + Codex, several windows).
- Records which agent each session belongs to, so per-agent stats are easy to add later.

Non-goals (explicitly out of scope for this implementation):

- Statistics collection / reporting. The state format is designed to make it
  trivial later (see "Future extensions"), but no `stats` command is built now.
- A GUI or menu-bar app.
- Battery-aware policy (e.g. only-on-power). The tool uses `pmset -a` (all power
  sources); switching to `-c` is a one-line change if wanted later.
- Multi-user / shared Macs. The design assumes one user. A shared Mac would need
  a per-user install (each with its own sudoers rule) sharing one root wrapper;
  not addressed here.

## Why hooks

Precisely detecting "a turn is computing" can only be done event-driven, from the
tool itself. Heuristics fail in both directions: at the `Stop` state (waiting for
user input) CPU is ~0, but during a long `Bash` tool call the `claude`/`codex`
process is also near-idle while a child process does the work. CPU/network
proxies therefore lie.

Both tools expose lifecycle hooks:

- **Claude Code** — `~/.claude/settings.json`: `UserPromptSubmit`, `Stop`, etc.
- **Codex** — `~/.codex/hooks.json` (Codex CLI ≥ 0.130): same event model,
  including `UserPromptSubmit` and `Stop`. `install` preflights the Codex version
  and skips Codex wiring (with a warning) if hooks are unavailable.

### Hooked events — explicit

| Event | Action | Notes |
|---|---|---|
| `UserPromptSubmit` | **acquire** | A turn started. |
| `Stop` | **release** | The turn completed normally. |
| `StopFailure` / turn-error terminal event, if the tool exposes one | **release** | A turn that ended on an API/internal error does **not** fire `Stop`; without this the token would leak until PID-death or max-age. `install` wires whatever turn-terminating events each tool version actually provides. |

**Deliberately NOT hooked:**

- `SubagentStop` — a subagent finishing is *inside* the parent turn; releasing on
  it would drop the parent's token mid-turn. The parent's `Stop` covers it.
- `SessionStart` / `SessionEnd` — session lifetime ≠ turn lifetime; a session
  open and idle for hours must not hold the Mac awake.
- `PreToolUse` / `PostToolUse` — inside a turn, redundant.
- `Notification` / `PermissionRequest` — see policy below.

### Permission-wait policy (decision)

When a turn is parked waiting for the user to approve a tool call
(`PermissionRequest`), `Stop` has **not** fired — the turn is genuinely
in-progress and will resume the instant the user approves. **Decision: a
permission-waiting turn counts as active; the Mac stays awake.** This is correct
behaviour, not a bug: the user may have stepped away briefly and wants the turn
to continue the moment they return. The failure mode — the user abandons the
machine entirely with a prompt up — is the same as any abandoned turn and is
caught by the **max-age cap** (see Data flow / reconcile), not by a special hook.

### Reliability of turn bracketing

`UserPromptSubmit … Stop` brackets a turn on the normal path. It is **not**
assumed reliable around interrupts: a user pressing Esc may not fire `Stop` (tool
behaviour here is not contractually guaranteed). When `Stop` is missed, the token
persists while the `claude`/`codex` process is still alive and idle — PID-liveness
pruning will *not* catch it. This is the single most likely "sleep stuck on" path,
and it is the reason the **max-age cap is a first-class, always-on guard**, not a
fallback.

## Architecture

**Approach A: hooks + passwordless sudo wrapper + launchd reconcile safety net.**

Single source of truth: a **token directory**. One file = one active turn.
System sleep state is *derived* from the directory by an idempotent `reconcile`
operation; no separate "current status" is stored.

```
UserPromptSubmit hook ─→ agentawake acquire ─┐
Stop / StopFailure    ─→ agentawake release ─┼─→ writes token dir ─→ reconcile ─→ sudo agentawake-pmset 0|1
launchd (~60s)        ─→ agentawake reconcile ┘                         (the only thing that touches pmset)
```

`reconcile` runs both from every hook (immediate reaction) and from a periodic
launchd agent (safety net). State always converges to reality.

## Components

### 1. `agentawake` — single Go binary, subcommands

- **`install`** — privileged one-time setup. **The only command requiring a
  password (`sudo`), once.** Steps:
  1. Self-check the `disablesleep` mechanism (see "Mechanism — verified").
  2. Verify every path component of `/usr/local/sbin` up to `/` is root-owned and
     not group/world-writable; create `/usr/local/sbin` as `root:wheel 0755` if
     missing. (A user-writable parent dir is a root-escalation vector and is a
     hard failure.)
  3. Install the pmset wrapper at `/usr/local/sbin/agentawake-pmset`,
     `chown root:wheel`, `chmod 755`.
  4. Write `/etc/sudoers.d/agentawake` atomically: render to a temp file,
     `visudo -c` the temp file, `chown root:wheel` + `chmod 0440`, then `mv` into
     place.
  5. Merge hook entries into `~/.claude/settings.json` and `~/.codex/hooks.json`
     (preflight Codex version first).
  6. Install and load the launchd agent.
- **`uninstall`** — reverses the above **in safe order**: first run a
  reconcile-to-off (while the wrapper still exists, so `disablesleep` is cleared),
  then remove hook entries, unload/remove the launchd agent, remove the sudoers
  file, remove the wrapper.
- **`acquire --agent <claude-code|codex>`** — invoked by the `UserPromptSubmit`
  hook. Reads hook JSON from stdin, writes a token, runs reconcile.
- **`release`** — invoked by `Stop` / turn-terminal hooks. Reads hook JSON from
  stdin, removes the matching token, runs reconcile.
- **`reconcile`** — invoked by launchd (~60s) and internally by acquire/release.
  Prunes dead-PID and over-age tokens, syncs `pmset`. Idempotent.
- **`status`** — prints active sessions (agent, pid, turn duration) and current
  `disablesleep` state. Surfaces the "stuck on" warning if detected (see Error
  handling). Exit codes: `0` installed and consistent; `1` not installed;
  `2` installed but inconsistent state detected (warning shown).
- **`off`** — emergency manual reset: removes **all** tokens, clears the
  `we-enabled` flag, and forces `sudo agentawake-pmset 0`. Note: if real sessions
  are still running, their next hook-triggered reconcile will re-enable — `off`
  is an immediate reset, not a persistent disable.

### 2. `agentawake-pmset` — tiny root-owned wrapper

A minimal script/binary at `/usr/local/sbin/agentawake-pmset`, owner
`root:wheel`, mode `755` (not user-writable — critical, or anyone could rewrite
it for root). Accepts exactly `0` or `1`; runs `pmset -a disablesleep <0|1>`.
Does nothing else — no pass-through args, no `eval`. Must be fast (a single
`pmset` exec) because `reconcile` holds the state lock across the call. This
narrow surface is what the sudoers rule grants.

Decision deferred to the implementation plan: whether this is a separate small
file or a hidden subcommand of the main binary invoked as
`/usr/local/sbin/agentawake-pmset`. Either way the sudoers rule names exactly one
absolute path.

### 3. `/etc/sudoers.d/agentawake`

```
<user> ALL=(root) NOPASSWD: /usr/local/sbin/agentawake-pmset
```

Grants passwordless execution of **only that one wrapper** — not `pmset`, not
`sudo` in general. Written atomically by `install`, mode `0440` `root:wheel`,
validated with `visudo -c` before being moved into place.

### 4. State directory — `~/.local/state/agentawake/`

- `sessions/` — one token file per active turn, named by `session_id`.
  Token content (JSON):
  ```json
  {
    "agent": "claude-code",
    "pid": 12345,
    "session_id": "abc123",
    "started_at": "2026-05-14T10:30:00Z"
  }
  ```
  `started_at` is the **turn** start (the token only exists during a turn).
  `status` reports `now - started_at` as turn duration.
- `we-enabled` — flag file. Present iff `agentawake` itself set `disablesleep 1`.
  Guards against clobbering a user's *manual* `pmset` setting. Written atomically.
- `agentawake.log` — append-only diagnostic log, size-capped with rotation.
- All mutations of `sessions/` and the flag happen under a single file lock
  (`flock`) so concurrent hooks cannot race.

### 5. Hook entries

Added **alongside** existing hooks (the user already has pixel-agents and rtk
hooks in both config files) — never replacing them. `install` must merge, not
overwrite, and `uninstall` must remove only the entries it added.

### 6. launchd agent — `~/Library/LaunchAgents/`

Runs `agentawake reconcile` every ~60s. A safety net, not a correctness
dependency. Note its real limit: a `LaunchAgent` does not run while the machine
is asleep — so the guarantee is "reconcile within ~60s **of wake**", and
reconcile-on-wake is the actual recovery mechanism for stale state accumulated
across a sleep.

## Data flow

### PID detection (resolved — not deferred)

Each token records a PID so `reconcile` can prune tokens whose process is gone.
PID detection is **best-effort**: the **max-age cap is the correctness backstop**,
so imperfect detection degrades gracefully (a turn whose PID could not be found
is still cleaned up by age) rather than breaking correctness.

Resolution order:

1. If the hook's stdin JSON includes a process id for the agent, use it.
2. Otherwise walk up the process tree from the hook's `$PPID`, matching against a
   configurable set of name patterns (`claude`, `codex`, `node` — Claude Code
   runs as a Node process and may appear as `node`). Stop at the first match.
3. If no ancestor matches (wrappers like `rtk` / pixel-agents may sit in the
   tree), record the hook's `$PPID` itself.
4. If even `$PPID` is unavailable, write the token with `pid: 0`; such a token is
   governed solely by the max-age cap.

### Turn start (`UserPromptSubmit`)

1. Hook runs `agentawake acquire --agent <claude-code|codex>`; hook JSON on stdin.
2. Binary parses stdin JSON for `session_id` (parsing is per-agent — see Open
   items) and resolves the PID as above.
3. Under `flock`: write `sessions/<session_id>` with the JSON token. If a token
   for that `session_id` already exists (e.g. a previous `Stop` was missed),
   overwrite it — `started_at` becomes the new turn's start.
4. Run `reconcile` (still under the lock — see below).

### Turn end (`Stop` / turn-terminal events)

1. Hook runs `agentawake release`; hook JSON on stdin.
2. Under `flock`: remove `sessions/<session_id>`. If no token matches that
   `session_id`, log and no-op (the token will be cleaned by prune anyway).
3. Run `reconcile`.

### `reconcile` (the only place `pmset` changes)

Runs entirely under the single `flock`, **including the `sudo` exec**, so the
decision and the action cannot interleave with another reconcile. The wrapper is
fast, so the serialization window is small; hook timeouts are set generously
enough never to fire during a normal reconcile. `acquire`/`release` always write
their token change *before* calling reconcile, so even if reconcile is killed by
a timeout the directory state is correct and the next reconcile (hook or launchd)
converges `pmset`.

Steps, under the lock:

1. **Prune** each token if either: its `pid` is non-zero and `kill -0 <pid>`
   fails (process gone — crash protection), **or** `now - started_at` exceeds the
   max-age cap (default ~4h, configurable — catches missed `Stop`, interrupts,
   `pid: 0` tokens).
2. Count remaining (live) tokens.
3. live > 0 **and** no `we-enabled` flag → `sudo agentawake-pmset 1`, create flag.
4. live == 0 **and** `we-enabled` flag present → `sudo agentawake-pmset 0`,
   remove flag.
5. live == 0 **and** no flag **and** `pmset -g` shows `disablesleep` is on →
   **the "stuck on" case**: do not auto-clear (could be the user's manual
   setting), but log loudly. `status` detects this same condition independently
   and shows the warning + exit code 2; no extra state is stored.
6. Otherwise → no-op (idempotent).

## Error handling

- **Hook must never break the agent.** `acquire`/`release` exit 0 even on
  internal error; failures are logged, not surfaced to the CLI tool. A generous
  timeout is set on the hook entries.
- **`Stop` missed — crash / `kill -9`** → token stays but its PID is dead; the
  next `reconcile` prunes it via `kill -0`.
- **`Stop` missed — process alive (Esc interrupt, unhooked turn-error)** → PID is
  still alive, so PID-pruning will not catch it. The **max-age cap** prunes it;
  in the meantime the Mac stays awake (bounded by max-age, not indefinite). This
  is the explicit reason max-age exists.
- **All sessions die at once** → no hook fires, but the launchd `reconcile`
  (~60s of wake time) prunes everything and restores sleep.
- **`we-enabled` flag lost** (e.g. `~/.local/state` cleared by a cleanup tool,
  Migration Assistant) while `disablesleep` is on → `reconcile` cannot safely
  auto-clear (it can no longer tell our setting from a manual one). It does **not**
  silently leave the user stuck: it logs loudly and `status` shows a prominent
  warning telling the user to run `agentawake off`. This is the one edge case the
  tool cannot fully self-heal; it is documented rather than hidden.
- **`sudo` wrapper missing / not installed** → `reconcile` logs and exits 0;
  `status` reports "not installed" (exit 1). Never crashes the hook.
- **`pmset` itself fails** → logged; the flag reflects what actually succeeded so
  the next reconcile retries.
- **PID reuse** — a pruned-then-reused PID is a theoretical concern; the max-age
  cap and the short hook-to-reconcile cycle bound the exposure, and a stale token
  pointing at a reused PID at worst keeps the Mac awake until max-age. Acceptable.

## Testing

- **Pure logic, no root, no side effects:** the reconcile decision function
  (tokens + flag state + PID-liveness oracle + clock → desired pmset action +
  prune set) is a pure function, unit-tested exhaustively: empty, one live,
  several live, dead PIDs, over-age tokens, `pid: 0` tokens, mixed,
  flag-present-but-no-sessions, the "stuck on" detection case.
- **State directory operations:** temp-dir tests — token write/read, concurrent
  `flock` behaviour, JSON round-trip, malformed/partial token files, atomic
  flag write.
- **PID detection:** tested with synthetic process trees / injected ancestry,
  including the no-match and `pid: 0` fallbacks.
- **`install` config merge:** fixture `settings.json` / `hooks.json` containing
  pre-existing hooks — assert our entries are added and existing ones untouched;
  assert `uninstall` removes only our entries.
- **`install` safety checks:** path-component ownership verification rejects a
  user-writable `/usr/local/sbin`; sudoers file is written `0440 root:wheel`.
- **Side-effecting parts** (`sudo` call, launchd load, sudoers write,
  `pmset -g` parsing) are thin shells around the tested core, isolated behind
  interfaces and exercised by an integration smoke test.

## Open items — to verify during implementation

- **Hook stdin JSON schema.** The exact field carrying the session identifier
  must be confirmed for *both* tools (Claude Code and Codex may differ — e.g.
  `session_id` vs another name/nesting). `acquire`/`release` parse per-agent.
  Until confirmed, this is the highest-risk unknown for Codex support.
- **Turn-terminal events.** Confirm which events each tool version fires when a
  turn ends abnormally (API error, interrupt) and wire all of them to `release`.
- **Whether the hook payload exposes a PID** — if so, it is the preferred PID
  source (step 1 of PID detection) and the tree-walk becomes a pure fallback.

## Distribution

- **GitHub repo** + **goreleaser**: tagged releases produce prebuilt binaries
  (arm64 + amd64), changelog, GitHub Releases.
- **Own Homebrew tap** (`homebrew-tap` repo); goreleaser auto-updates the formula
  on each release:
  ```
  brew install <user>/tap/agentawake   # installs the binary only
  agentawake install                   # privileged setup, password once
  ```
- `go install github.com/<user>/agentawake@latest` works for Go users.
- **Homebrew core** is a later goal, gated by the project's notability — not a
  launch target.

## Future extensions (designed-for, not built)

- **Stats:** on `release`, append a completed-turn record (agent, duration,
  timestamps) to `~/.local/state/agentawake/events.jsonl`; add an
  `agentawake stats` command over it. The structured token format already
  carries `agent` and `started_at`, so this is additive.
- Battery-aware policy (`-c` vs `-a`).
- Per-agent or per-project enable/disable.
- Multi-user / shared-Mac support.
