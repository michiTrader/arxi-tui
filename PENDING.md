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
- Not done: the model is not told to act instead of announcing ("let me check...") without a tool call.

## arxi-tui work not done yet

- Extensibility phases 4-6 host wiring (`settingChanged` events, `agent.turn_done`/`agent.failed`,
  `host.panels` bind, `/service list|start|stop`, `/package list|show`, `ui_edit` guide text) and docs
  (`DESIGN-EXTENSIBILITY.md`; `/folder`, `folders`/`outside` wire params, scrollbar, `/shell`).
- Re-run mutation checks on the scrollbar tests.
- Rate-limit pacing: the free model allows 5 requests/min; a turn of ~13 tool rounds waits several minutes.
