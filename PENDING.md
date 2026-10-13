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

## arxi-tui work not done yet

- Mouse-draggable scrollbar (needs `?1002h` drag events and a drawn scrollbar).
- Background shells ("N shell", output inside the TUI, `/shell` list/show/stop).
- Extensibility phases 4-6 host wiring (`/service`, `/package list|show`, `host.panels`), docs.
- Rate-limit pacing: the free model allows 5 requests/min; a turn of ~13 tool rounds waits several minutes.
