# Pending work

## Done since: second status line (cwd + branch), PgUp/PgDn/End, hint only when scrolled up (03d3b44)

## Done since: background shells (this branch)

- `shell_start` / `shell_read` / `shell_stop` client tools (offered only where `run` is ask/allow,
  counted in the budget of 8, hint removed when a core cannot carry them), `/shell` (list, show, stop, start),
  `host.shells` bind ("N shell") in the status line, a warning line when a shell ends by itself,
  everything stopped on `/clear` and on exit. They live in the TUI, not in the core (the core is
  started per turn).
- Verified only on Linux with a real `sh`. NOT verified: Windows runtime (`cmd.exe`, `taskkill`),
  a real model calling the tools, a real terminal showing the bar.

## Extensibility phases 4-6 (events, panels, services): host wired

- `cmd/arxi-tui/package_auto.go`: events (turn.done, turn.failed, effort, mode, style), panels
  (`host.panels` bind, refresh on load / turn.done / every_s), services (`/service list|start|stop`,
  only when the user types it). `/package show|list` and the `ui_edit` guide describe them.
- Tested on Linux with a real `sh` (unit tests, race detector, mutation checks; the stale-answer
  check found and fixed a real bug). NOT verified: Windows runtime, a real model writing such a
  package, a real terminal showing the panels row.

## arxi-tui work not done yet

- Factory commands as packages: decided NOT now (they stay Go code; see DESIGN-EXTENSIBILITY.md). Reopen only with a design for what may never be removed (`/package`, `/ui reset`).
- Rate-limit pacing: the free model allows 5 requests/min; a long turn of tool rounds waits minutes (the round limit itself is now 40, `ARXI_MAX_TOOL_ROUNDS`).
- CI (GitHub Actions, ubuntu + windows) runs only on pushes to `master` and on pull requests, so
  commits pushed to this branch without a PR are not tested there. Last green run on both OSes:
  `8e29592` (includes the Windows `onVolume` fix and the Windows shell code). Everything after it
  (package events/panels/services, scrollbar tests, tool-round limit, act-don't-announce hint) has
  only run on Linux locally, plus `GOOS=windows go vet`. Opening a PR (even a draft) would run it.
