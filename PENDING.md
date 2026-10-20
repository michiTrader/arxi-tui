# Pending work

## Done since: the settings menu hides and shows parts of the screen (branch feat/hidden-parts)

- Closes "settings that are only a `/ui` command": the parts you can hide are now a stage of the `/settings` menu (`hidden`), one row per part, Enter toggles it and the menu stays open on the same row. Session only, as `/ui hide` is; nothing is written.
- Decision, and why: the menu never offers the input, an overlay (the menus), or any node that contains an input. `/ui hide` accepts any id, and that is kept. A menu is one Enter from a mistake a typed command is not, and hiding the input leaves a screen from which `/ui show` cannot be typed. The rule reads structure (type input / overlay), not names, so a user's own scene is covered. `toggleHidden` re-checks, so a stale row cannot hide the input either.
- Tests: 10 new (what is offered, never the input / menus / a wrapper of the input, toggle both ways, stale row refused, rows say HIDDEN, stage stays open on a filter that matches nothing, first-stage row, real loop hide/show, nothing written to texts.json, highlight stays on the toggled part). Mutations: 11, one survived at first (the highlight reset after a pick had no test); a test now holds it and kills it.
- NOT done: parts that have no `id` in the scene cannot be addressed (same limit as `/ui hide`); hidden parts are still not kept between sessions.
- NOT verified: a real terminal.

## Done since: `/settings` is a menu (branch genspark_ai_developer_settings_menu)

- Closes the first half of "the owner can see and change every setting from inside the TUI": `/settings` now lists every setting with its value and what it does, narrowed by typing; Enter on a setting opens the values it can take, Enter on a value changes it. A pick becomes the same `/settings <name> <value>` call the typed line makes, so the menu, the typed line, `/ui text` and the agent's `ui_edit` share one rule and one file (`texts.json`). New file `cmd/arxi-tui/settings_menu.go`; `main.go` only routes to it.
- Two bugs found by the first test run, both fixed and covered: (1) the typed surface also recognises `/settings chat.scrollbar n` (a setting plus a value) and is checked earlier in the loop, so it took the Enter and refused the half-typed `n`; (2) `setRows` starts on the row in force, which was the autocompact one at the very bottom, so the menu opened on its last page and hid every setting.
- Tests: 17 new (stage reading, rows, value marks, spellings like `off` for `no`, every offered value accepted by the rule that checks writes, hyphen assumption behind `-`, and the real loop for list/narrow/pick/autocompact/refusal). Mutations: 13 on the new code, 12 killed at first; the survivor was a guard that could never matter (a dead `"-"` check), removed, and the assumption it hid is now a test with a counterfactual (a value with a hyphen fails it).
- Still NOT a free-form editor: a setting that is a number has no list, so after the pick you type the number. Settings that are only a `/ui` command (hidden parts) are listed by `/settings list` but are not rows of the menu.
- NOT verified: in a real terminal. The full suite has one failure that is not this change: `TestLoopABigPasteShowsAMarkerAndSendsTheRealText` (see "Frágil sin arreglar" below) fails on this branch's long name (`genspark_ai_developer_settings_menu`) and passes on a short one (checked: same code, a branch called `tmp-short`). It is the known fragility, not a regression of this change; a short branch name avoids it.

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
- The input window is now also tested with real keystrokes through the loop (`input_window_loop_test.go`: it shows the end where the caret is, follows the caret back up to the start, shows `↑`/`↓` only when rows are hidden on that side, stays put while the caret is inside it, never takes more than a third of the screen). 10 mutations on the window and its wiring, all killed (one survived at first: the loop remembering where the window was; the fourth test closes it).
- NOT verified: in a real PowerShell / Windows Terminal (modified arrows, bracketed paste delivery); ctrl+z after a marker was opened (not tested).

## Done since: row of active shells above the info bar (branch feat/shells-line)

- A row of its own above the info bar names the shells that run: `shell 1 · shell 2` (the numbers `/shell show <n>` and the model's `shell_read` use; an ended one drops out, numbers are not reused). New bind `host.shell.list`; the default scene gets a node `shells`, so it is configurable like everything else: `/ui hide shells`, move, restyle, or a scene of your own.
- Changed on purpose: the info bar no longer carries the count (`2 shells`); the row replaces it. `host.shells` still exists for scenes that want a count.
- Tests: the list itself, the real loop (two shells named right above the info bar; one after stopping the other; stepping aside for the slash menu; `/ui hide shells` really hides it and does not stop the shell). 14 mutation checks (two survived at first: the menu case and the hide case were not covered; both now are). One old test expected the count in the bar and was adapted.
- Later: with more shells than fit the width the row now wraps onto the next row like the info bars (`status.narrow`, #285; test through the real loop, mutation killed) instead of being cut.
- NOT verified: in a real terminal; Windows runtime of the new tests (they need `sh`).

## Done since: browser opened twice, Windows tests that failed for speed (PRs #272, #273)

- `run` reported a command that opened a browser as "could not be started" (the browser keeps the output pipe; Go answers `WaitDelay expired`), so the model ran it again and the page opened twice. Now it is Exit 0 with a note not to repeat it (core; the core must be rebuilt). 2 tests, 5 mutations killed.
- Three Windows tests failed on the CI runner for speed, never for the code (a temp folder still locked after `taskkill`, a 20 s wait for powershell, too few animation ticks). They waited too little; now they wait more. The animation test, if it fails again, prints how often each colour was drawn, which tells "did not tick" from "hardly repainted". It failed once more with 2 s of waiting before the last run passed, so it is NOT proven stable.

## Still open from the owner's list (round of the lag report)

- CLOSED (owner): the tool record of a browser-opening call. arxi has no browser tool: the model opens pages with `run` + `start`. The owner's saved sessions (20261013-140244, 20261013-192904) show those calls recorded as "could not be started: WaitDelay expired" with the wording from before #272, which is what hid them; 20261011-194901 shows one as Exit 0. The record existed. Still to watch: the first real call with a rebuilt core, to see it reads well.
- Right-edge glitch ("arxi-tui" -> "arxi-ti" while narrowing): FIXED (#285) and CONFIRMED by the owner in a real PowerShell, both with the default `wrap` and with `hide`. Cause found (it had not reproduced before because the probe never made the terminal narrower than the info row): the folder/branch row and the mode/model/usage row were drawn as one long row, however narrow the terminal; the engine did not wrap or cut them, and the terminal (auto-wrap is off on purpose) cuts a row at the last column. Now a row too wide wraps its words onto the next row (default) or drops whole segments, chosen with `/ui text status.narrow wrap|hide`; also applied to lone one-line notices (the "press ctrl+c again" hint) and to rows inside boxes. Not covered: a row with weighted columns (the author's columns stay as written), a column that is itself several rows. If the owner still sees a cut letter, it is not this row: tell me which row.
- Per-model modalities: only "takes images" exists (a switch per model); other modalities are not started. (Chat copy without the scrollbar column: done, see above.)
- How images reach the model: DONE in the code (see "Imágenes por modelo" below). The old note that they did not is gone.
- Per key, at 100 turns of the benchmark session, the chat costs about 0.55 ms (cached layout) plus about 1.9 ms for the fold of the log: about 2.5 ms, against about 30 ms before the cache. See "Done since: chat render cache".

## Done since: core tests on Windows (branch fix/core-tests-windows)

- `TestStandingPromptStaysWithinItsBudget` (cmd/arxi): the hints measured 984 bytes on the owner's Windows against a budget of 950. The cause is real, not noise: on Windows `envHint` adds a whole sentence (the cmd.exe commands) and longer names. The sentence is now the constant `windowsShellHint`, and on Windows the test allows exactly its length plus 20 bytes; Linux keeps 950. The test also fixes HOME/USERPROFILE, because the hint carries the user's home path and a long one would fail it for no reason of ours. (The tool-definition budget is unchanged.)
- `TestSuggestFolderIsTheNearestProjectAndNeverADriveOrHome` (internal/chattools): the code was right; the test was wrong. It asked for `\` as the root, which on Windows has no drive and is read as a place under the project. It now asks for the root of the project's own volume (`D:\`). The note pasted from the owner said the code suggested the root; I read `SuggestFolder` and `tooWide` and found no such path, so I did not change the code. Not proven on Windows.
- Mutation checks (Linux): removing the root check in `tooWide` is killed by the folder test; growing a hint by 300 bytes is killed by the budget test (a 100-byte growth is NOT caught on Linux, which has 150 bytes of room: that is the budget as set, not a new gap).
- NOT verified: I have no Windows. I could vet-compile for Windows and run on Linux, and I computed the Windows hint length by hand (Linux 801 + 162 for the sentence, in the same range as the owner's 984, which also includes his longer paths). The first real proof is the owner's own Windows run, or the CI step once he enables it (it is not live, see above); if it is red, a test still assumes Linux.

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
- `core/` tests on Windows (reported by the owner, output pasted from his machine): 2 of the 3 were adapted and the CI step that runs them on Windows is ready in `docs/ci-core-job.yml` but NOT live (my GitHub token cannot push `.github/workflows`: the owner must paste the Test step into `.github/workflows/ci.yml`) (see "Done since: core tests on Windows"). The third (a timing one) failed only sometimes; the owner's note says it passed in his last run and does not say which test it is, so it is NOT fixed. If it shows up again, paste its name and output.

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
- The core module `core/` (own `go.mod`) IS tested by CI now: the `core` job in `.github/workflows/ci.yml` runs on ubuntu and windows (4 jobs in all with `test`). `docs/ci-core-job.yml` is the old copy and is no longer needed.
- Blocked on the `arxi` core / product decisions, not on this repo: NEXT.md K1 core wiring and K3 (need a live
  agent and a coordinated core surface bump), K4 wasm (deferred on purpose), M3; DECISIONS-DEFERRED D1
  (`{row.<field>}` in drawn text: a format change) and D2 (product repo, `install.ps1`, version handshake).

## Imágenes por modelo (hecho en el código; falta probar con un modelo real)

Hecho y probado con mutaciones: un modelo NO acepta imágenes hasta que alguien lo diga
(menú /model, fila "Accepts images", o `arxi model update <ref> --images`, o `--images` al
añadirlo). `chat.send` lleva `images`; el core se niega ANTES de cobrar si el modelo no las
acepta. En la TUI: `/image <ruta>` o pegar la ruta de un archivo (arrastrarlo al terminal)
pone un marcador `[Image #N nombre]`; las imágenes viajan solo con ese mensaje.

Límites (el tope de 1 MiB de la línea NDJSON NO se subió): 4 imágenes por mensaje y ~600 KB
ya codificadas; la TUI lo comprueba al adjuntar. El formato se lee de los bytes, no de la
extensión. Un core viejo recibe el texto sin las imágenes y un aviso.

Aviso temprano (hecho): al adjuntar una imagen (`/image` o ruta pegada) la TUI avisa si el
modelo elegido no está marcado para imágenes ("<modelo> is not set to take images ... /model
... Accepts images"). No avisa si aún no sabe cuál es el modelo (lectura en curso o sin core).
Sigue el modelo elegido en /model y lo que se aprende al abrir /providers. El core sigue
rechazando antes de cobrar.

Ctrl+Z y marcadores (hecho): con el bucle real, ctrl+z tras pegar una imagen quita el marcador
y la línea sale sin imagen; ctrl+y lo devuelve con su imagen; ctrl+z tras borrar el marcador
lo devuelve con su imagen. Estaba "no verificado"; ahora hay test y no hubo bug.

NO verificado (solo el dueño puede, necesita sus claves o su terminal):
- (YA PROBADO por el dueño con Gemini gemini-3.5-flash-lite: describió una foto a la primera. El fallo anterior con GLM era que ese modelo no admite visión, no un fallo de arxi-tui.) Falta solo probar otros proveedores si se quiere;
- pegar rutas arrastradas en PowerShell/Windows Terminal reales;
- las imágenes no aparecen en el historial del chat salvo por el texto del marcador (decisión
  de diseño, no un fallo).

## Tests flaky (lista de #273)

- `TestBehaviourAnimationReachesTheEmittedFrames`: endurecido en #283 (espera hasta ver un
  segundo color, límite 60 s).
- `TestBehaviourAnimationMovesWithTheClock`: HECHO. El color del rainbow sale de la hora
  (`theme.Cycled(frame, now)`), no de `animClock`; se añadió la costura `cycleNow` en main.go
  y el test la controla: mismo instante = mismas letras, un tick después = otro color, y vuelve
  al instante inicial. Sin esperas reales. Tres mutaciones muertas (reloj congelado, reloj de
  pared, paso desfasado).
- Core, los dos que fallaron en Windows en la corrida local del dueño: ARREGLADOS por
  razonamiento y probados en Linux; NO reproducidos en una máquina Windows (solo los verá CI):
  - `TestSpentBudgetMakesTheNextAskWaitForTheWindow`: la expectativa ignoraba el tiempo gastado
    tras la respuesta (el pacer cuenta desde que anotó la respuesta). Ahora los límites salen del
    código y el margen es grande para que un margen perdido se vea. Mutaciones muertas.
  - `TestRunAttachInitialJoinExcludesThenPrintsAProvisionalCompleteEventOnce`: era el bloqueo de
    archivo de Windows sobre `pending.commit`; el test lo borra ahora reintentando
    (`removeWhenReleased`, igual que `logstore.removePendingFile` en el producto), con su propio
    test. Mutaciones muertas. Si vuelve a fallar en CI, el mensaje dirá el plazo agotado.

Vistos fallar UNA vez en CI de Windows y pasar al relanzar, sin relación con el cambio del PR
(ambos dependen del tiempo del runner, que anda lento: la suite raíz tarda ~250 s allí):
- `TestAfterResumeWithNoFigureTheBarEstimatesAndSaysSo` (cmd/arxi-tui, #292).
- `TestBehaviourAnimationMovesWithTheClock` (cmd/arxi-tui, 60 s hasta agotar, #294).
No se arreglaron: falta reproducirlos en una máquina Windows real.

## Ctrl+F no hacía nada (rama fix/find-bar-saved-scene)

Causa encontrada y reproducida: quien ya había cambiado algo con `/ui` tiene su interfaz guardada en
`~/.arxi/scene.json`, y esa copia es de ANTES de la barra, así que el prompt no traía el nodo `suffix`
y `Ctrl+F` no tenía nada que abrir (y tampoco avisaba). Arreglo: al arrancar, una interfaz guardada cuyo
prompt no tiene `suffix` recibe la barra (invisible mientras está cerrada); si el usuario ya puso su propio
`suffix`, no se toca. Y si una escena no tiene barra, `Ctrl+F` ahora dice por qué en la línea de avisos.
Quedan 2 mutaciones vivas que son equivalentes (una guarda repetida, y una rama que la del buscador
atrapa antes). NO verificado: la otra causa posible, que la terminal se quede con `Ctrl+F` antes de que
llegue al programa; si con esto sigue sin abrirse, hay que probar otra tecla.

## Ronda actual: Ctrl+F, buscar una palabra en el chat (rama feat/find-in-chat)

Pedido del dueño: una mini barra de búsqueda en la zona de la barra de entrada, a la derecha, con otro
color de fondo, hecha respetando el dogfooding. Se hizo así:

- La barra NO es código de dibujo: es el nodo `suffix` del prompt en la escena, atado a `host.find`
  (bind firmado, fila en `docs/BINDS.md`), con el token `find.bar`. Los aciertos se pintan con los
  tokens `find.match` / `find.current`, y la palabra `find` es el texto `find.label`. El host solo
  guarda el estado de vista (la palabra, qué acierto es el actual), como ya hace con el scroll y el pegado.
- Teclas: `ctrl+f` abre/cierra; con la barra abierta, letras y borrar editan, `enter`/`↑` va al acierto
  anterior, `↓` al siguiente, `esc` cierra. PgUp/PgDn y la rueda siguen valiendo. `ctrl+c` nunca se toca.
  Un atajo propio del usuario en `ctrl+f` gana. Con el menú `/` abierto no actúa. Una escena cuyo prompt no
  tiene barra deja `ctrl+f` en paz.
- Las escenas por defecto no cambian al dibujar (barra cerrada = mismos bytes); el `SOBRIA.json` solo
  gana el `suffix`.
- Tests con mutaciones. Quedaron vivas 2 mutaciones que no son fallos: quitar el rótulo en el primer
  dibujo (el repintado posterior lo corrige solo) y quitar el borrado del rótulo con el menú `/`
  (no se puede llegar ahí: con la barra abierta, escribir `/` va a la barra).
- NO verificado: cómo se ve en una terminal real (colores `#3a3a46`, `#5c4a00`, `#e0b000`: si no se leen
  bien, se cambian en los tokens, sin tocar código), que Windows Terminal entregue `ctrl+f` al programa,
  y el comportamiento con `shift+enter`.
- Fuera de esta ronda por pedido del dueño: las mejoras de la caché de lecturas.

## Ronda actual: DeepSeek en uso real (rama fix/context-and-limits)

Las cinco preguntas del dueño, con lo que se encontró y lo que se hizo:

1. **`decode tool arguments: unexpected EOF`** (y "con continua funciona"). Es un flujo cortado a medias
   por el proveedor/pasarela: la llamada llega con los argumentos incompletos. Ya se trataba desde #295
   (se vuelve a preguntar solo); lo que faltaba era el caso de un JSON completo pero que no es un objeto
   (`[]`, `1`...): ahora también es un reintento y no mata el turno. Si el dueño sigue viendo el texto
   exacto `decode tool arguments`, su binario del core es anterior a #295: reconstruirlo.
   NO se pudo reproducir un corte real aquí (solo con un proveedor falso).
2. **"used up its whole output allowance of 4096 tokens"**. Era nuestro: el core pedía siempre
   `max_tokens=4096` y un modelo que razona gasta parte en pensar. Ahora 16384 por defecto, `ARXI_MAX_OUTPUT_TOKENS`
   lo cambia, y si un proveedor contesta 400 diciendo su máximo, se vuelve a pedir con ese número.
3. **Límite de 40 rondas**: desactivado por defecto. `ARXI_MAX_TOOL_ROUNDS=<1..1000>` pone uno; `off`/`0` = sin límite.
4. **El % de contexto baja de golpe** (8% → 2%). Es normal, no es la API ni una compactación: la barra muestra lo
   que el modelo tenía en la ÚLTIMA llamada del turno; entre preguntas el historial solo guarda tu texto y la respuesta
   final, así que la siguiente empieza más pequeña. No hacían falta logs. Lo que sí tenía arreglo (punto 5) es que
   por eso el modelo se olvidaba de lo leído.
5. **Muchos `Read` del mismo archivo**. Es normal en este diseño por dos causas, y las dos se atacaron:
   (a) dentro de un turno repetía el mismo `read`: ahora un resultado idéntico (>= 400 bytes) de `read`/`list`/`grep`
   se manda una sola vez y la repetición va como una línea; (b) entre preguntas se olvidaba lo leído: ahora la app
   guarda lo que devolvieron `read`/`list`/`grep` (32 KB, lo más nuevo primero, máx. 40) y lo pone en las instrucciones
   del siguiente turno como texto de la app. Se olvida al hacer `write`/`edit` de ese archivo o cualquier `run`, con `/clear` y
   al compactar; un resultado cortado (archivo largo en páginas) nunca se guarda; `/resume` lo reconstruye.
   Coste: hasta ~8k tokens más en cada pregunta siguiente, a cambio de no releer el archivo entero.

Mutaciones: todas las del código nuevo muertas, salvo una EQUIVALENTE (poner `run` dentro de `remembers`: `run` se
atiende antes en `foldCalls`, nunca llega ahí).

NO verificado: nada de esto de punta a punta con arxi-tui y DeepSeek real. Con la clave del dueño solo se hicieron
llamadas sueltas a la API para ver el rango de `max_tokens` (1..393216), el contexto (1.048.576) y que las herramientas
funcionan con y sin `reasoning_content`; la clave no se guardó en ningún archivo y conviene ROTARLA. Que el modelo
REALMENTE deje de releer con el texto de memoria es una petición en el prompt, no una garantía: contar `Read` repetidos en
las próximas sesiones. Efecto que no se midió: cuánto sube el coste por pregunta por llevar esa memoria.

## Ronda anterior: hecho y lo que falta (honesto)

Hecho y probado con el bucle real + mutaciones:
- El historial de entrada conserva el borrador al pulsar Flecha abajo (#287).
- `/resume` lista por defecto solo la carpeta actual (`resume.scope` = folder|all) (#287).
- El modelo de chat es POR INSTANCIA: cada turno envía `Model` explícito; el archivo
  `default-model` del core es global a la máquina y ahora solo es el modelo inicial (#288).
- Un pegado largo se dibuja en el chat con el mismo marcador que mostró la barra; pegar dos
  veces lo abre en ambos sitios. Solo es visual: se envía y se guarda el texto real (#289).
- `context deadline exceeded`: había un fallo real. El core daba UN presupuesto a todo el
  mensaje (límite de una llamada + 5 min de reintentos), así que un turno con muchas rondas de
  herramientas moría con ese error aunque cada llamada fuera sana. Ahora el límite
  (`ARXI_CHAT_TIMEOUT`, 3 min por defecto) vale POR LLAMADA al modelo.
- "(the model sent an empty reply)": el core ahora vuelve a preguntar hasta 2 veces (avisando
  en la línea Thinking como "empty reply") antes de rendirse. Si el modelo se quedó sin
  presupuesto de salida (finish=length, típico de modelos que razonan) NO reintenta y lo dice
  con otra frase.

- Los bloques de código de las respuestas ya NO llevan la barra `│` delante de cada línea:
  se marcan con un fondo (`markdown.codeblock`), así que al copiar con el ratón sale solo el
  código (#291). La `│` entre columnas de una tabla y la `▏` de una cita siguen siendo texto.
- Del análisis de dos sesiones reales (#292): una ruta que empieza por el nombre del proyecto
  (`pitlane-strategies/strategies`) ya funciona (antes "does not exist" y el modelo decía que el
  proyecto estaba vacío); y tres frases nuevas en las instrucciones fijas: no pedir permiso para
  mirar, listar `.` antes de decir que el proyecto está vacío, y no decir "lo estoy ejecutando"
  sin llamar a `run` en esa misma respuesta (unos 60 tokens por turno).

- De tres capturas del dueño ("¿es culpa nuestra o de la API?"), lo que SÍ era nuestro:
  1. **"[No reply was given…]" como respuesta.** Tras un turno cancelado o fallido, la app guardaba
     en el historial una respuesta FALSA del asistente con ese texto. El modelo la veía como suya y
     la copiaba: al decir "reintenta" contestaba con esa frase, 3 veces seguidas (y se leía como
     "cancelado sin que lo pidiera"). Ahora el aviso va dentro del mensaje del USUARIO, marcado como
     nota de la app ("not typed by the user… Never repeat this note"), y no hay respuesta inventada.
     El core une los mensajes de usuario seguidos en uno (algunos proveedores rechazan dos seguidos).
  2. **Un pegado largo con una pausa en medio.** Si entre dos trozos de un pegado pasaban más de
     400 ms, se tiraba lo ya recibido y el resto se leía como TECLAS: cada salto de línea enviaba un
     mensaje, y una ESC dentro (la salida pegada de una terminal las trae) cancelaba el turno. Ahora
     un pegado empezado espera su marcador de fin (hasta 15 s) y llega entero; si el fin nunca llega
     se entrega lo recibido como pegado, no se tira. Probado con la tubería real y su temporizador.
  3. **`decode tool arguments: unexpected EOF`.** Una llamada a herramienta con los argumentos
     cortados mataba el turno. Ahora: si el flujo se cortó, se vuelve a preguntar (reintento
     transitorio, como las llamadas sin nombre); si el modelo se quedó sin espacio de salida
     (finish=length) se le dice "hazlo en pasos más pequeños" (máx. 2 veces) y, si insiste, el
     usuario lee una frase clara. Nunca se ejecuta media llamada ni una buena que iba con una cortada.
  4. La instrucción fija dice "si la petición es clara, hazlo; no preguntes 'would you like me to
     proceed?'" (es una frase en el prompt: una petición, no una garantía).
  Lo que NO era nuestro y no se tocó: `Cancelled` tras "cambiar de modelo" ocurre solo si se
  pulsa Esc/Ctrl-C (única fuente de `chat.cancelled`); el límite de 40 rondas y el coste
  (`↑1928710` = suma de lo enviado en cada ronda, cada ronda reenvía toda la conversación) son
  por diseño [desde fix/context-and-limits sin límite por defecto]; que el proveedor corte un flujo es del proveedor.
  NO verificado: nada de esto contra un proveedor real ni una consola de Windows real; las pruebas
  usan un proveedor falso y una tubería. No se pudo reproducir que Esc llegue "solo" por otra vía.

## Veces que hubo que explicarle al modelo lo que ya debía saber (a evitar)

Contadas de las 4 conversaciones que pegó el dueño. Cada fila es un mensaje del usuario que no
debería haber hecho falta. Esto es el criterio: menos mensajes para llegar a lo mismo.

| Conversación | Mensajes de más | Qué pasó | Dónde se ataca |
|---|---|---|---|
| SMC (pitlane-strategies) | 5 | "no ves que estamos en un proyecto?", "estamos en la carpeta ... y tienes acceso", "estamos dentro de la ruta, busca las estrategias", "investiga a ver y ejecútala". El modelo escribió el nombre del proyecto como ruta, recibió "does not exist" y dijo que estaba vacío | #292: la ruta con el nombre del proyecto funciona; "lista `.` antes de decir que está vacío" |
| SMC, 2.ª sesión | 3 | La misma petición larga enviada 3 veces (una cancelada, una por saldo, una tras cambiar de modelo); luego "no me llegó ninguna petición y además estamos en full access" (el modelo pidió permiso para listar) | #292: "no pidas permiso para mirar"; el saldo agotado ya se dice claro (no es del modelo) |
| OpenCode en WSL | 4 | "no corriste nada, no usaste la herramienta run"; "es ubuntu, por favor instálalo, no quiero hacer nada"; "si hazlo tú, usa el comando wsl no?"; un "sí" suelto. El modelo decía "voy a ejecutarlo" sin llamar a `run`, y pedía confirmación para algo que ya estaba permitido | #292: "no digas que ejecutas sin llamar a `run`"; esta ronda: "no pidas permiso ni 'di sí y lo hago'" |
| Abrir en el navegador | 1 (y el turno perdido) | "abre la estrategia en el navegador": el modelo entendió "muéstrame el archivo" y contestó que ya estaba abierto | esta ronda: la instrucción dice qué significa y qué ejecutar (`start ""` / `open` / `xdg-open`), y la de las shells dice cómo mostrar una página servida |

Total: unos 13 mensajes de más en 4 conversaciones. Ninguno de los arreglos está comprobado con un
modelo real: son frases en las instrucciones fijas (una petición al modelo, no una garantía). La
forma honesta de saber si funcionó es volver a contar en las próximas conversaciones.

Hecho en esta ronda (PRs #296, #297 y el de ajustes):
1. Línea que empieza por una ruta (`/mnt/c/Users/...`, `/usr/bin/ls -l`): ya no se rechaza ni abre el
   menú; se envía al modelo como texto normal. Una palabra suelta mal escrita (`/nonsens`) sigue
   avisando.
2. Herramienta real `open_url`: el modelo abre una dirección http/https en TU navegador (pide permiso
   según la política, como `run`). NO verificado: el abridor real (`xdg-open` / `open` / `start` /
   `wslview`) nunca se ejecutó aquí; solo se probó con un abridor falso y la elección por sistema.
3. Superficie de ajustes: `/settings list`, `/settings <nombre>`, `/settings <nombre> <valor>`,
   `/settings <nombre> -` (valor de fábrica), `/settings autocompact <off|N>`. Mismas validaciones y
   mismo `texts.json` que `/ui text`. Nuevas claves: `scroll.wheel_step`, `scroll.page_overlap`,
   `input.max_rows`. Límite honesto: las partes ocultas (`uiHidden`) se MUESTRAN en la lista pero no se
   guardan entre sesiones (se cambian con `/ui hide` / `/ui show`).

NO verificado: que un modelo OBEDEZCA las tres frases nuevas (una frase en un prompt es una
petición, no una garantía; solo se probó que se envía). Y nada de lo anterior contra un proveedor real (vyceai/glm-5.3-flash); el reintento
por respuesta vacía y el límite por llamada se probaron con un proveedor falso. Si la respuesta
vacía de glm viene con finish=length, el mensaje nuevo lo dirá; entonces el remedio sería subir
`MaxTokens` (hoy fijo en 4096), que NO se tocó. [Hecho después: ahora 16384, ver arriba.]

Frágil sin arreglar: `TestLoopABigPasteShowsAMarkerAndSendsTheRealText` busca "paste again to expand"
en la barra de información, que lleva carpeta + RAMA + esa frase en una línea de 80 columnas. Con un
nombre de rama largo (más de ~35 caracteres) la frase queda recortada y el test falla aunque el código
esté bien (pasó en una rama de nombre largo; con un nombre corto pasa). No es un fallo del producto.

## Solo el dueño puede hacerlo (no bloquea nada de lo anterior)

Cerrado por el dueño: el borde derecho en PowerShell real (OK, #285); la herramienta de navegador
(no existe, el modelo usa `run` + `start`); las imágenes con un proveedor real (Gemini OK; el
fallo con GLM era el proveedor, que no admite visión).

Abierto, menor:
1. Pegar rutas arrastradas (imágenes) en PowerShell / Windows Terminal reales.
2. Si se quiere, probar las imágenes con otro proveedor.
3. Vigilar la primera llamada `run` + `start` con el core recompilado y confirmar que el registro
   se lee bien (la redacción nueva es de #272).
4. Confirmar en una máquina Windows real (o dejar que CI lo haga) que los dos tests de core ya
   no fallan de forma intermitente.
