# Pending work

## Done since: copying the chat without the scrollbar column (branch feat/copy-clean)

- The bar's column and the spaces that pad each row out to it are real characters on the screen, so a mouse selection took them. Changed: no bar (the chat fits) means no padding and no bar cell; `/ui text chat.scrollbar no` removes the bar altogether (validated yes/no). With the bar showing, the `│` and the padding are STILL copied: a program cannot make a terminal selection skip a column. `/copy` is the clean way.
- Tests: no trailing spaces and the same text as the narrower layout when the chat fits; a real bar still aligns every row; the text key turns the bar off through the real loop (found on the way: the loop reads the user's texts only for the built-in scene by name, so the helper now uses that name); the key refuses a value that is not yes/no. Mutations: 4 killed; one survivor is old code (`wantMouse` inside the loop, not covered by any test) and was not touched.
- NOT verified: in a real terminal (that a drag-select without the bar really copies no trailing spaces; in Windows Terminal/PowerShell the selection may also trim them by itself); the bar-on case still copies `│`.

## The owner's definition of "finished" (recorded on purpose)

**The product is not finished until the owner can edit the TUI from inside the TUI, however he
wants, and define the default style and behaviour once and for all.** Every look and behaviour
item below is a step toward that, not the end of it. Concretely still missing: a way to see and
change, from inside the TUI, each setting that today needs a `/ui` command or a scene file (the
paste limits, scroll steps, the input window height are text keys or constants now), and a
decision, taken by the owner, of what the factory style and keys are, after which they stop
moving. Not started as a single piece of work.

## Done since: keyboard scroll, big pastes folded, long input scrolls (branch feat/paste-input-scroll)

- Chat scroll from the keyboard like `arxi_cli_sim`: ctrl+↑/↓ (a notch), alt+↑/↓ (a row), ctrl+Home (oldest), shift+↑/↓ (previous/next message you wrote; the renderer reports where they sit). PgUp/PgDn/End/ctrl+End already existed.
- A paste over 5 lines / 1500 characters shows `[Pasted text #1 +68 lines]` in the input and `paste again to expand` in the info bar (new bind `host.paste`, in the default scene). Enter sends the real text; the same paste again opens the marker; Backspace/Delete take a marker whole; the numbers restart after a send. Limits and words are text keys (`paste.fold_lines`, `paste.fold_chars`, `paste.hint`).
- A tall input becomes a window that follows the caret (max a third of the screen, at least 3 rows), with `↑`/`↓` markers.
- Tests: 14+ for the paste store, 6 through the real loop, 6 for the input window in the engine, plus the keyboard scroll ones. Mutation checks: paste store 14/14 killed, loop wiring 8/8 killed, keyboard scroll 7/7; input window 15 mutations: the real gaps (box child renderer, edges) were found and closed; 3 survive and are equivalent by reasoning (`top > row` written as `>= row+1` or `>= row`, and `>` vs `>=` on the window limit give the same result for whole numbers / when the text exactly fits).
- NOT verified: in a real PowerShell / Windows Terminal (modified arrows, bracketed paste delivery); ctrl+z after a marker was opened (not tested); the input window with real keystrokes through the loop (tested in the engine only).

## Done since: row of active shells above the info bar (branch feat/shells-line)

- A row of its own above the info bar names the shells that run: `shell 1 · shell 2` (the numbers `/shell show <n>` and the model's `shell_read` use; an ended one drops out, numbers are not reused). New bind `host.shell.list`; the default scene gets a node `shells`, so it is configurable like everything else: `/ui hide shells`, move, restyle, or a scene of your own.
- Changed on purpose: the info bar no longer carries the count (`2 shells`); the row replaces it. `host.shells` still exists for scenes that want a count.
- Tests: the list itself, the real loop (two shells named right above the info bar; one after stopping the other; stepping aside for the slash menu; `/ui hide shells` really hides it and does not stop the shell). 14 mutation checks (two survived at first: the menu case and the hide case were not covered; both now are). One old test expected the count in the bar and was adapted.
- NOT verified: in a real terminal; with more shells than fit the width (the row is one line, it is cut at the edge like the rest); Windows runtime of the new tests (they need `sh`).

## Done since: browser opened twice, Windows tests that failed for speed (PRs #272, #273)

- `run` reported a command that opened a browser as "could not be started" (the browser keeps the output pipe; Go answers `WaitDelay expired`), so the model ran it again and the page opened twice. Now it is Exit 0 with a note not to repeat it (core; the core must be rebuilt). 2 tests, 5 mutations killed.
- Three Windows tests failed on the CI runner for speed, never for the code (a temp folder still locked after `taskkill`, a 20 s wait for powershell, too few animation ticks). They waited too little; now they wait more. The animation test, if it fails again, prints how often each colour was drawn, which tells "did not tick" from "hardly repainted". It failed once more with 2 s of waiting before the last run passed, so it is NOT proven stable.

## Still open from the owner's list (round of the lag report)

- NOT reproduced: the tool record of a browser-opening call not showing in the chat. The core sends one `chat.tool` per call and the screen draws every one, so I do not know why one is missing; I need which tool the model used (the line that did show, or `run`).
- Right-edge glitch ("arxi-tui" -> "arxi-ti" while narrowing): the frame drawn at widths 133-140, run through a terminal emulator, showed no overwritten character. The row with the folder path was not in my probe, so this did NOT reproduce it; it may be the Windows terminal's own handling of the last column. The owner uses PowerShell; still not reproduced. Useful test: run with `-mouse=false` (no scrollbar) and see if it still happens.
- Not started: per-model modalities; (chat copy without the scrollbar column: done, see above).
- How images reach the model TODAY: they do not. The core has a content-block type for images (`turn.BlockImage`, `MediaSource`) but nothing builds or sends one, the turn validator refuses any block that is not text, no provider code mentions images, the chat tools only read text files, and a model in the store has an id, an on/off flag, a price, a wire and a context window, with no notion of what it accepts. So there is nothing to select per model yet: it is a feature to build (model field, form fields in add/edit, a way to attach an image, the wire encoding for each provider, and refusing an image for a model that cannot take it).
- Per key, at 100 turns of the benchmark session, the chat costs about 0.55 ms (cached layout) plus about 1.9 ms for the fold of the log: about 2.5 ms, against about 30 ms before the cache. See "Done since: chat render cache".

## Done since: chat render cache (branch perf/chat-line-cache)

- Before: every frame laid out the whole chat again (markdown, wrapping, tool boxes). Measured with `go test ./internal/engine -bench Chat` (100 columns, 30-row pane, a question + 2 tool calls + a markdown answer per turn; 2-core sandbox): 10 turns 2.7 ms, 40 turns 11 ms, 100 turns 28 ms per frame, about 70 percent of it markdown/wrapping.
- Now: `engine.ChatRowCache` (owned by the loop, handed to each frame's Renderer) keeps the rows of each history item. Same benchmark with the cache once warm: 10 turns 0.27 ms, 40 turns 0.43 ms, 100 turns 0.55 ms. The fold is measured separately (`go test ./internal/fold -bench Fold`, log shaped like the loop's, with the thinking merged into one event per turn): 10 turns 0.18 ms, 40 turns 0.76 ms, 100 turns 1.9 ms. The same log with thinking kept in 30 fragments a turn would cost 25 ms at 100 turns, which is what `addEvent` already prevents.
- It cannot change what is drawn: tests compare cached against uncached frames over widths, prompt styles, expanded tools, looks, scrollbar on/off and a growing conversation, with every kind of item (tool stacks, approval, empty answer, usage line). Any change of width, prompt style, Ctrl+O, look or style token drops the rows; an item that changes (a streaming answer) is laid out again alone; whatever is not drawn in a frame is forgotten, so the cache never holds more than the current conversation.
- Mutation checks: 17 mutants of the cache and its wiring, all killed (one survived at first because the test used the function under test to build its expectation; fixed with an independent copy).
- NOT verified: speed in the user's real terminal (only the benchmark above, in the engine, not the whole loop with a real TTY); the whole loop with a real TTY (the engine and the fold were measured apart and added up by hand); memory on a real session (measured on the benchmark session only: about 13 KB per turn, 1.4 MB at 100 turns, 5.6 MB at 400; real answers with long code or tool output will weigh more).

## Done since: lag in a long conversation (branch fix/long-conversation-lag)

- Symptom: typing/deleting in the input bar got slow in a developed conversation, only in that window, and `/resume` cured it. Cause (found by profiling, not guessed): every streamed fragment of the model's thinking was kept as its own event in the in-memory log, and the whole log is folded again on every key. After 40 turns with ~200 fragments each, a key cost ~125 ms. `/resume` rebuilt the log from the saved file, which never kept thinking events, hence the cure.
- Fix: consecutive `chat.thinking` events are merged into one using the fold's own function, so the marquee text is exactly the same (random property test). Same conversation: ~34 ms a key (a quiet one: ~30 ms).
- Tests: log stays flat; merged text equals what the fragments would show; only consecutive thinking is merged; timing (thoughtful <= quiet*2+5 ms, skipped with -short); the real loop keeps one event per thought. 5 mutation checks, all killed (two tests had to be reinforced first).
- NOT fixed / NOT verified: the cost still grows with the length of the visible history even with no thinking (about 5 ms at 10 turns, 24 ms at 40) because the whole chat is re-rendered on each frame. That is a second, smaller cause; a render cache is the next step. Not measured in the owner's real terminal nor on Windows.

## Done since: seven more package events (15 in all)

- `tool.used`, `file.changed`, `approval.asked`, `provider.error`, `context.high`, `model.changed`, `session.resumed`. Same rules as the rest: they only show a note. `is` narrows `tool.used` / `approval.asked` to a tool and `model.changed` to a model (checked when the package is written; a package's own `pkg_` tools pass unchecked).
- Decisions worth knowing: `file.changed` is only the model's own successful `edit`/`write` (a `run` that writes files cannot be told apart, so it does not count; a file the user changes is not seen at all); `context.high` is 80% of the window and fires once until it drops below (compact, /clear, resume) and climbs back; `provider.error` is only the model call failing (a core that could not be reached is not it); `model.changed` fires on a pick through `/model` that went through, not on the startup read nor on a change made in the providers screen.
- Tests: unit (each fires only its own, `is` narrowing, validation) plus the real loop for each wire; the full root suite found one old test that expected the old wording of the refusal, adapted; 23 mutation checks, all killed in the end (the validator's own rule for `is` was first covered only through the host; it now has a test in its own package, after a mutation survived). One survived at first because it was dead code (an `if out.err == ""` that the branch already guaranteed; removed); another "survived" only because I pointed it at the wrong package and re-ran it correctly.
- NOT verified: a real model writing a package that uses them; Windows runtime (the tests need `sh`); the 80% is fixed, not a setting.
- Still no event for: a file changed by the user, `/clear`, a new project folder.
- TODO (reported by the owner): 3 tests of the `core/` module fail when run on Windows locally (two assume Linux paths/timings, one behaves differently depending on the disk). The CI `core` job runs tests on Linux only, so it does not see them. Adapt those 3 tests to Windows, then run `core` tests on Windows in CI. Not done: I have no Windows here and do not know which 3 they are.

## Done since: three more package events, and the pacing seen live

- Package events were 5 (`turn.done`, `turn.failed`, `effort`, `mode`, `style`). Now also `turn.started` (a message went to the model), `turn.cancelled` (the user pressed Esc/Ctrl-C on a turn) and `shell.ended` (a background shell finished by itself). Same rules as the others: they only show a note, never send anything, never start another event. Tests drive the real loop; mutation checks for each wire.
- Rate-limit pacing was seen with the real binary and a real model: apinex `free/mimo-v2.6-pro` (5 requests/min), six files read one at a time: the sixth waited the window ("Waiting 56s · request limit reached") and answered, no 429 in between. One run, one provider; VyceAI sends no such headers so it is not paced.

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

- `cmd/arxi-tui/package_auto.go`: events (turn.done, turn.failed, turn.started, turn.cancelled, shell.ended, effort, mode, style), panels
  (`host.panels` bind, refresh on load / turn.done / every_s), services (`/service list|start|stop`,
  only when the user types it). `/package show|list` and the `ui_edit` guide describe them.
- Tested on Linux with a real `sh` (unit tests, race detector, mutation checks; the stale-answer
  check found and fixed a real bug). NOT verified: Windows runtime, a real model writing such a
  package, a real terminal showing the panels row.

## arxi-tui work not done yet

- Factory commands as packages: decided NOT now (they stay Go code; see DESIGN-EXTENSIBILITY.md). Reopen only with a design for what may never be removed (`/package`, `/ui reset`).
- Rate-limit pacing: DONE for providers that send rate-limit headers (`X-RateLimit-Remaining/Reset`, `RateLimit-*`, OpenAI's `-requests` variants). The core reads them on every answer, and when the budget is at 0 it waits for the window before the next ask of a tool loop (the TUI shows "• Waiting 41s · request limit reached"). Not covered: providers that send no such headers are still handled only reactively (429 + `Retry-After`); a window longer than `chatMaxWait` (90s) is not waited for; the wait was also seen once live (apinex mimo, 5 requests/min: the sixth read waited 56 s, no 429), with one provider only.
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
