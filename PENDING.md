# Pending work

## Interrupted by the user: tradetropy plotting investigation

Outside this repository (`D:\projects\tradetropy\src`), so it could not be continued here.
It was being done from `D:\projects\pitlane-strategies` with `apinex/free/mimo-v2.6-pro`.

- Goal: find how Bokeh glyphs (Rects, `line_width`) are created in
  `tradetropy/plotting/render/_tools.py`.
- Found so far: `_build_data` at char 6401 and `_create_glyph` at 7344 of that file (19533 chars).
  `_create_line` and `_create_rect` do not exist there; `tradetropy/plotting/_core.py` was not found.
- Next: read `_build_data` (from offset 6401, ~150 lines) and `_create_glyph` in full, then
  look for where `line_width` is set for each primitive kind.
- With `/folder add D:\projects\tradetropy` (or "ask" mode) the model can now read that folder directly.

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

- Decide whether factory commands become packages (not decided).
- `host.shells` bar mutation (`barvis`) still survives: nothing asserts the shell count on the real bar through the loop.
- Rate-limit pacing: the free model allows 5 requests/min; a long turn of tool rounds waits minutes (the round limit itself is now 40, `ARXI_MAX_TOOL_ROUNDS`).
