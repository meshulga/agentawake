# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What this is

`agentawake` is a macOS CLI that keeps the Mac awake (including lid-closed, via the
undocumented `pmset -a disablesleep 1`) **only while a Claude Code or Codex agent
turn is actively running** — detected through the tools' lifecycle hooks, not CPU
heuristics. Single Go binary, stdlib only, no external dependencies.

## Commands

- Build: `go build -o agentawake .`
- Test everything: `go test ./...`
- Single package: `go test ./internal/reconcile/ -v`
- Single test: `go test ./internal/reconcile/ -run TestDecide -v`
- Vet: `go vet ./...`
- Module path is `github.com/hok/agentawake`; Go 1.26.

## Architecture

The **single source of truth is a token directory** (`~/.local/state/agentawake/sessions/`),
one JSON file per active turn. System sleep state is *derived* from that directory,
never stored separately.

```
UserPromptSubmit hook → acquire ┐
Stop / StopFailure    → release ┼→ write/remove token → reconcile → sudo agentawake-pmset 0|1
launchd (~60s)        → reconcile ┘
```

`reconcile` runs from every hook (immediate) and a launchd agent (safety net), and
always converges. Key invariant: **`reconcile` is the only thing that touches `pmset`**,
and it runs entirely under one `flock` including the `sudo` exec.

### Packages (`internal/`)

- **`reconcile`** — `Decide()` is a **pure function** (tokens + flag + pmset state +
  clock + PID-liveness oracle → prune set + `SleepAction`). No I/O. This is the
  correctness core; test it exhaustively. The four actions: `ActionEnable`,
  `ActionDisable`, `ActionWarnStuck` (sleep on but no flag — a lost flag, don't
  auto-clear), `ActionNone`.
- **`token`** — the on-disk JSON record (`agent`, `pid`, `session_id`, `started_at`).
  `started_at` is the *turn* start, not session start.
- **`state`** — owns the state dir: atomic token writes (`.tmp` + rename), the
  `we-enabled` flag, and the advisory `flock` that serializes reconciles. `tokenPath`
  validates session IDs against path traversal.
- **`hookjson`** — deliberately tolerant parser for hook stdin JSON; tries multiple
  field-name candidates because Claude Code and Codex name fields differently.
- **`pid`** — best-effort PID resolution (payload pid → ancestor matching name
  patterns → `$PPID` → 0) and `IsAlive` via `kill -0`. Best-effort by design — the
  max-age cap is the real backstop.
- **`pmset`** — reads `disablesleep` via `pmset -g`; writes only through the root
  wrapper at `/usr/local/sbin/agentawake-pmset` using `sudo -n` (fail fast, never prompt).
- **`cli`** — subcommand dispatch.

## Design principles to preserve

- **Pure core, thin I/O shells.** All side effects (sudo, launchd, pmset parsing,
  process table) sit behind interfaces; the decision logic stays pure and unit-tested.
- **Hooks must never break the agent.** `acquire`/`release` exit 0 even on internal
  error — log, don't surface.
- **Self-healing.** Missed `Stop` events are recovered by PID-liveness pruning and a
  max-age cap (~4h). The one non-self-healing case (lost `we-enabled` flag) is handled
  by a loud warning + `agentawake off`, never silently.
- **Two-stage write.** `acquire`/`release` always persist the token change *before*
  calling reconcile, so a killed reconcile still leaves correct directory state.

## Reference docs

The design spec (`docs/superpowers/specs/`) and implementation plan
(`docs/superpowers/plans/`) live under `docs/superpowers/` which is **gitignored**.
The spec covers the verified `disablesleep` mechanism, the full hook-event rationale
(why `SubagentStop`/`SessionStart` are deliberately *not* hooked), and error-handling
edge cases — read it before changing reconcile or hook behavior.
