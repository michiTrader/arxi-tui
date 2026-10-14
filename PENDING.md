# Pending work

## Done since: hub panic, ctx after an error, ctx after /resume (branch fix/hub-window-panic)

- Panic after `Set context window` -> Enter: a saved form dropped the form but left the hub on the form level; the next repaint read a nil form. `apply` now leaves the form level, and `publish` / `routeHubKey` repair that state instead of crashing.
- ctx fell from 36k to 3.5k after a timeout + "continua": only the NAMES of the tools of a failed turn were kept, not what they returned. Now the newest ~48 KB of tool output stays in the history of a failed or cancelled turn (live and after `/resume`). Answered turns are unchanged by design (tool output inside an answered turn is not carried to the next one).
- `/resume` showed no ctx: `chat.usage` / `chat.compacted` were not saved. They are now (only the last `chat.usage` of a turn that got no answer, so the file does not grow per ask); with no figure at all the bar shows `ctx ~N` estimated from the history.
- NOT verified: nothing run in a real terminal or Windows; the live apinex call was made with curl, not through arxi (see the PR text); whether the extra history makes a failed turn's retry slower on a slow provider is not measured.

## Done since: context bar, usage per ask, D1, D2 pieces (branch feat/urgent-fixes, PR #261)

- Status bar shows `ctx N%` of the real window (core > `ARXI_CONTEXT_WINDOW` > known-family table > `ctx ?`); the core sends `chat.usage` after every ask; `/compact` explains held tokens vs history; deadline errors say "timeout, not a full window"; `ARXI_CHAT_TIMEOUT` sets the per-call limit.
- D1 `{row.<field>}` in drawn text; D2 `install.ps1`, core version stamped by `build-release.sh`, handshake mismatch message naming both programs, notice when no core is found.
- NOT verified: nothing run against a real model or a real terminal; `install.ps1` ran only under PowerShell 7 on Linux against a local file server (not Windows PowerShell 5.1, not real Windows, not the real GitHub release); `core/` is not in CI (job ready in `docs/ci-core-job.yml`, needs `workflows` permission to push); live eval corpus for D1 not re-run; the separate product repo is not created.
- `cmd/arxi-tui/arxi-tui` (a 12 MB binary) is no longer tracked and is git-ignored; it stays in history, so a clone is still heavy until history is rewritten (owner decision).

## Done since: second status line (cwd + branch), PgUp/PgDn/End, hint only when scrolled up (03d3b44)

## Done since: background shells (this branch)

- `shell_start` / `shell_read` / `shell_stop` client tools (offered only where `run` is ask/allow,
  counted in the budget of 8, hint removed when a core cannot carry them), `/shell` (list, show, stop, start),
  `host.shells` bind ("N shell") in the status line, a warning line when a shell ends by itself,
  everything stopped on `/clear` and on exit. They live in the TUI, not in the core (the core is
  started per turn).
- Verified only on Linux with a real `sh`. NOT verified: Windows runtime (`cmd.exe`, `taskkill`),
  a real model calling the tools, a real terminal showing the bar.

## Done since: background shells die with the terminal

- Before, SIGTERM and SIGHUP killed arxi-tui outright: a server started with `/shell` or by the model kept running with its port taken (found by driving the real binary in a pty). Now they end the program normally, so the shells stop. Checked with the real binary for SIGTERM/SIGHUP (stopped) and SIGKILL (cannot be caught; the server stays). NOT verified: Windows console close.

## Extensibility phases 4-6 (events, panels, services): host wired

- `cmd/arxi-tui/package_auto.go`: events (turn.done, turn.failed, effort, mode, style), panels
  (`host.panels` bind, refresh on load / turn.done / every_s), services (`/service list|start|stop`,
  only when the user types it). `/package show|list` and the `ui_edit` guide describe them.
- Tested on Linux with a real `sh` (unit tests, race detector, mutation checks; the stale-answer
  check found and fixed a real bug). NOT verified: Windows runtime, a real model writing such a
  package, a real terminal showing the panels row.

## arxi-tui work not done yet

- Factory commands as packages: decided NOT now (they stay Go code; see DESIGN-EXTENSIBILITY.md). Reopen only with a design for what may never be removed (`/package`, `/ui reset`).
- Rate-limit pacing: DONE for providers that send rate-limit headers (`X-RateLimit-Remaining/Reset`, `RateLimit-*`, OpenAI's `-requests` variants). The core reads them on every answer, and when the budget is at 0 it waits for the window before the next ask of a tool loop (the TUI shows "• Waiting 41s · request limit reached"). Not covered: providers that send no such headers are still handled only reactively (429 + `Retry-After`); a window longer than `chatMaxWait` (90s) is not waited for; the wait is checked only against fakes (never against the real apinex, whose key now answers 402 "free tokens used").
- CI (GitHub Actions, ubuntu + windows) runs on pushes to `master` and on pull requests. PR #260 is green
  on both OSes at `b3d5e97`, so the Windows shell code (`cmd.exe`, `CREATE_NO_WINDOW`, `taskkill /T`, exit
  code) has now run for real, through `shells_windows_test.go`. Still NOT run on Windows: the
  events/panels/services host (its tests need `sh`, `//go:build !windows`).
- The core module `core/` (own `go.mod`) is NOT tested by CI: `go test ./...` at the root skips nested modules.
  The ready job is in `docs/ci-core-job.yml`; adding it to `.github/workflows/ci.yml` needs a token with the
  `workflows` permission (this environment's push was refused for exactly that). Action for the owner:
  paste that job into `ci.yml`.
- Blocked on the `arxi` core / product decisions, not on this repo: NEXT.md K1 core wiring and K3 (need a live
  agent and a coordinated core surface bump), K4 wasm (deferred on purpose), M3; DECISIONS-DEFERRED D1
  (`{row.<field>}` in drawn text: a format change) and D2 (product repo, `install.ps1`, version handshake).
