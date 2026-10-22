# DESIGN - an interface the owner can rebuild by asking

Status: **proposal, not built (2026-10-20)**. Only `docs/GUARDRAILS.md` and its tests exist so far.
Everything below is a plan; each claim about the current code was read in the repository on this
date and is marked **(verified)**. Anything marked **(estimate)** or **(untested)** is not.

## 1. The goal, and the test that decides it

The owner says to arxi, in any words, "change this part of the TUI", and it happens: the layout,
a button, a window, how a key behaves, how messages are drawn, an animation, a sound bar, a chart,
a picture. No pull request from a developer for each idea. The whole TUI, the factory one
included, is made of things the owner and the agent can edit.

The test is not a feature list. It is a battery of requests in plain language (section 9) that the
agent must carry out from a clean install without anyone touching Go, measured with the eval
system the repository already has (`docs/EVAL.md`). When the battery passes, this design is done.

Non-goals: replacing the core, or letting an edit reach files outside the settings folder.

## 2. Why it is not possible today

Every new idea runs into the same three walls. Each one is written in Go.

1. **Data.** A scene can read only a closed list of values (binds, `docs/BINDS.md`), and many arrive
   as finished text. The background shells reach the scene as the string `shell 1 · shell 2`
   (`host.shell.list`) or `1 shell` (`host.shells`) **(verified)**. A scene cannot draw one row per shell,
   nor show what a shell printed.
2. **Behaviour.** What a key does, and what happens when you press Enter while the agent works, is
   decided in the main loop of `cmd/arxi-tui/main.go` **(verified)**: the arrow keys walk history and the
   caret, `ctrl+o` is `isCtrlO`, and a message sent during a turn is refused with `errChatBusy`
   ("still waiting for the previous answer") **(verified)**. `/keys` lists these keys and can move six
   menu keys and bind shortcuts, but it cannot change what a factory key does.
3. **Drawing.** The chat, the tool calls, the menus and the `/provider` screen are drawn by Go code.
   Tool calls have settings for rows (`tool.arg_rows`, `tool.output_rows`, `tool.diff_rows`) and the
   chat for gaps between turns (`chat.turn_gap`, `chat.usage_gap`) **(verified)**; I did not find a setting for
   the blank row between two tool calls.

Adding freedom at one place has meant a pull request each time. That does not scale; the fix is
to move the line, not to add more exceptions.

Two limits outside this repository matter for the plan:

- The core declared `--on-busy steer` but honoured only `queue`; steering did not reach a turn
  (`core/cmd/arxi/inject.go`, `steer.go`) **(verified)**. This was core work and is done: ADR-0055
  (core #328) and the "Built (C7)" section below.
- The core is a separate Go module with no third-party dependencies **(verified)** and stays so.

## 3. Constraints (the guardrails)

`docs/GUARDRAILS.md` makes these tests; the design may not break them without a decision in that file.

- One static binary, `CGO_ENABLED=0`, built for `android/arm64` (Termux) on every test run.
  Measured today: **9503073 bytes** for Termux; the budget is 11000000.
- A closed list of dependencies; none for the core.
- The factory scene and its goldens do not move by accident (pinned by hash).
- No npm, no Node, no second repository. A feature that needs something installed is a **package
  that asks the user to install it**, never a requirement of the binary.
- Ctrl-C, twice, is immovable (invariant 6). The input can never be removed. `-raw` always boots.

## 4. What already exists and is reused

Read in the code **(verified)** unless noted.

| Piece | Where | What it gives |
|---|---|---|
| Scene document | `internal/scene` | node types `stack row box overlay text markdown input spinner marquee list button switch slider sparkline rule`; `when`, `bind`, `style_by`, `row_template` (one row per array element) |
| Press grammar | `scene/action.go` | `on_press` is a closed set: `cmd: focus: answer: ext:<plugin>:<action> say: open: set:` |
| Focus ring | `cmd/arxi-tui/press.go` | Tab / Shift+Tab over pressable nodes; Enter runs the focused node's action |
| Four documents | settings folder | `scene.json`, `theme.json`, `texts.json`, `behaviour.json`, undone and reset together by `/ui undo`, `/ui reset` |
| Agent tools | `ui_bridge.go` | `ui_guide` (the live document, about 1.5k tokens, paid only on the turn that needs it) and `ui_edit` (scene, colours, texts, behaviour, packages; shows a diff, asks, validates, retries on refusal) |
| Packages (ext/v2) | `internal/ext/v2` | commands, input rules, model-callable tools, events, panels, services; operations `builtin`, `http`, `exec` (argv, never a shell); permissions the user reads before approving |
| Plugin values | `internal/ext/store.go` | a plugin can publish values the scene binds to, of kind `text` or `series` (numbers, drawn by `sparkline`) |
| Settings, keys | `/settings`, `/keys` | every setting and key listed, changed from a menu |

The design adds to this list. It does not replace it.

## 5. Architecture: a small core and a profile

```
 PROFILE   everything the owner edits: screens, windows, components, keys, logic,
           effects, widgets, texts, themes, tests
 ---------------------------------------------------------------------------
 CONTRACT  DATA (a reactive store)  VERBS (named actions)  EVENTS
           KEYS (contexts)  DRAWING primitives  STREAMS
 ---------------------------------------------------------------------------
 CORE (Go) terminal, the arxi core connection, permissions, Ctrl-C x2, safe mode,
           the renderer, the small interpreter
```

The rule: Go never decides how a particular thing looks or what a particular key does. It offers
data, verbs and primitives, and validates what the profile asks. The factory interface is a profile
written with the same tools the owner has; that equality is the thesis of the project (AGENTS.md).

### 5.1 Data: a store with schemas

Everything the app knows is published as structured values, not finished strings:

```
shells      [{id, command, status, started, tail}]       queue   [{id, text, mode}]
chat.items  [{kind, name, arg, result, diff, ok}]        input   {text, caret, at_start, at_end, rows}
providers   [{id, name, status, models}]                 voice   {level_db, speaking, partial}   (from a package)
```

Namespaces: `core.*` and `host.*` read-only; `ui.*` view state the profile may write; `pkg.<id>.*`
published by a package. The list of signed binds stays audited (`binds_audit_test.go`), but it
becomes a registry with schemas that packages extend, instead of one closed document. The audit
rule is kept: a bind no scene uses is a warning; a scene using an unknown bind is refused.

### 5.2 Verbs: every Go capability has a name

`chat.send chat.cancel queue.push queue.pop queue.edit shell.start shell.stop shell.read input.set
input.insert input.move focus.to window.open window.close clipboard.copy notify open_url audio.play`
and so on, each with parameters and a protection level. Factory commands (`/shell`, `/copy`,
`/provider`) become thin wrappers over verbs. **Protected verbs** (saving a key, answering an
approval) can be bound to a key or a button but never run from a script without the user's own
press. This extends the existing closed `on_press` grammar; it does not replace it.

**Built (B2).** `internal/verb` holds the catalogue: each verb has a name, typed parameters (int with limits, or
text) and a protection (`free` or `typed`). Today: `shell.start` (typed), `shell.stop`, `shell.read`. `on_press`
gains an eighth prefix, `do:<verb> <arguments>`, read by the same `scene.ParseAction`. A typed verb cannot be
carried by a button, key, command or hook (refused at load and again at run); only a line the user types starts
it. `/shell` now calls the verbs, and `/shell start` from a button (`cmd:`) is refused too. The host keeps a map
of implementations and a test walks the catalogue so the two cannot drift; the VERBS chapter of `ui_guide` is
generated from the catalogue.

### 5.3 Keys with contexts, as data

Today a key goes through the big chain in `main.go`. The proposal is a keymap in data:

```json
{ "key": "down",  "when": "focus == 'prompt' && input.at_end && shells.count > 0", "do": "focus.to:shells_chip" }
{ "key": "ctrl+o", "do": "ui.cycle:tools.level" }
{ "key": "enter", "when": "agent.working", "do": "queue.push:{input.text}" }
```

The factory keymap is the default one and is the contract of `docs/KEYS.md`; that file's test then
checks the data and not a Go table. Ctrl-C x2 stays in the core and is not in the keymap.

### 5.4 Logic, in tiers (the cost of each is stated)

| Tier | What | Dependencies | Covers |
|---|---|---|---|
| 0 | validated documents (scene, components, keymap, theme, effects with parameters) | none | most of the work: layout, windows, buttons, menus, `/provider`, tool-call spacing, three-level `ctrl+o`, the shells chip |
| 1 | an expression evaluator for `when` and formats: `focus == 'prompt' && input.at_end` | written here | conditions, small calculations |
| 2 | a small interpreter for a subset of JavaScript (variables, `if`, `for`, functions, objects, arrays, template strings) | written here | logic with state: the message queue, state machines, new text effects, custom widgets |
| 3 | an external program talking NDJSON (exists as package `services`) | none new | voice, hardware, odd APIs, anything heavy |

Why tier 2 is written here and not taken from an interpreter library: a library such as goja would
add to the binary an amount nobody has measured here, and a large surface the project does not
control; a step and memory limit that we own is what keeps a script from freezing the screen. The
size of tier 1 and tier 2 is an **estimate** (hundreds of lines, and a few thousand) and is
re-measured against the Termux budget when each lands. The language is a subset of JavaScript only
because models write it well; where something is missing the interpreter says what to use
instead ("`class` is not available, use an object with functions"), which is the repair loop the
eval already measures. npm and Node are never involved. WASM is left as a possible later tier: a
user cannot produce it inside Termux.

### 5.5 Components, chat included

Each kind of chat item (`chat.item.user`, `answer`, `tool`, `diff`, `approval`) becomes a component
with properties (a `level`: one line, cut, full; a gap). "One line per tool call, a blank row
between, three levels on `ctrl+o`" is then editing a component and a keymap entry. Reusable
components (a card, a chip, a dialog, tabs) are documents too. The render cache that keeps the chat
fast today is kept, keyed by component and data.

### 5.6 Windows and layout

Docked panels (left, right, top, bottom) that do not cover the chat; splits, tabs, floating
windows with order, popups anchored to a node; mouse press, drag and hover. `/provider`, `/settings`,
`/keys` and every menu become windows of the profile. What stays in Go is the verb that stores a
credential, never the look of the screen that asks for it.

### 5.7 Effects, widgets, streams, pictures

- **Effects:** a text effect is a function `(text, time, width) -> cells`. Factory: the sliding
  Thinking line, the typewriter, and "words that appear and wrap when the line fills".
- **Widgets:** a widget is `(props, data, size, time) -> grid of cells`. A decibel bar, a spectrum, a
  trading chart, a minimap of the chat, a game: all the same primitive.
- **Streams:** a service publishes values at a rate, capped (for example `pkg.voice.level_db`).
- **Pictures:** a widget may draw an image as half-block cells (`▀` with colour), which works on any
  true-colour terminal and is the base. Sixel or the Kitty protocol are used only where a
  capability check says the terminal has them, and never required. Which terminals have them is
  **not verified**; `-probe-keys` showed the way to do such a check and the same is needed here.

### 5.8 Tools for the agent: see, test, edit

The agent edits blind today: `ui_edit` checks that a change is valid, not how it looks.

| Tool | Does |
|---|---|
| `ui_inspect` | searches the tree, data, verbs, keys and components, instead of sending one 20 KB guide |
| `ui_patch` | edits any document of the profile |
| `ui_preview` | draws the screen as text with simulated state (3 shells, a full queue, a voice level) |
| `ui_simulate` | sends keys ("down down enter") and shows the result |
| `ui_test` | saves a scenario as a profile test, rerun on every change |

The loop: ask, edit, preview, run the tests, fix, show the diff and the preview, the owner accepts or
discards. The preview and simulate tools reuse the renderer and the fake terminal that the loop
tests in `cmd/arxi-tui` already use **(verified: `runLoopDefault`, `typed`, `enterLine`)**.

### 5.9 Safety when everything is editable

- Double Ctrl-C and `--safe` always give back the basic interface; no profile touches them.
- The input never disappears (already validated). A change that leaves the app unusable is rolled
  back on its own to the last good version.
- Permissions per package (network, programs, microphone, send to the model), invalidated when the
  package changes (exists). Protected verbs never run unattended.
- A time limit per frame and per script, and a watchdog that switches off what hangs and says so.
- Layers: factory, community packages, user, project. A full history of the profile (`/ui history`,
  diff, restore, named versions), `/ui export` and `/ui import`, and **promote to factory**: the
  owner's TUI becomes the default with its tests and goldens.

## 6. Voice

Capturing a microphone without C is not portable, so the binary does not do it. Voice is a package
with a helper program, and a stream: Termux uses `termux-microphone-record` (from Termux:API), Linux
uses `arecord`, `sox` or `ffmpeg`, Windows and macOS use `ffmpeg` or a helper that arxi offers to
download once. The helper publishes `voice.level_db`, `voice.speaking`, `voice.partial`; the
decibel bar is a widget bound to the level; a key sends the partial text with `chat.send`. Speaking
the answer needs the text of the answer: events carry no text today (**verified**:
`packageAuto.turnDone` fires `turn.done` with an empty argument, `package_auto.go`; and `template.go`
refuses a placeholder in anything that runs by itself), so an event payload is part of phase B.

**Built (B3, part 1).** A `turn.done` event can read the model's answer as `${text}` in a url, a body or an
argument. Only that event gets it (any other place that writes `${text}` is refused when the package is saved,
and again when it runs); the answer is cleaned of escapes and cut to 4000 characters; an event that names
`${text}` does not run when the turn said nothing (a hole is never filled with nothing); the words of a failed
or cancelled turn are dropped; the consent screen says the package reads every answer. What an event returns is
still only shown, so it cannot send the answer anywhere the package was not already allowed to reach (`net`,
`exec`), and those are listed on the same screen.
None of this is tested on any device; Termux:API availability and latency are open questions.

## 7. Translation

The Google web endpoint is not reliable: from this sandbox it answered with a block page
(**verified 2026-10-20**), and it is not an official API. A package can use a keyed API instead, but
two gaps exist: an HTTP `extract` takes **one** value, while a translation of a paragraph comes in
segments, and a rule sees only the line you type (`input.pre`), never the model's answer. Phase B adds
an `extract` that joins an array and an `output.post` stage for rules that may reword what is shown
(display only, never what the model sees).

**Built (B3, part 2).** `extract` can put a `*` where a list is (`result.segments.*.text`) and then needs a
`join` of `none`, `space` or `newline`: every entry becomes one text. One `*` per path, at most 200 entries (a
longer list is refused, never cut, because a translation with its tail missing reads as complete); a `*` without
`join`, or a `join` without `*`, is refused when the package is saved; an entry that lacks the key is an error
naming its position. The result is cleaned like any other output. `output.post` is the last piece of B3.

## 8. Phases

| Phase | Builds | Done when |
|---|---|---|
| 0 | guardrails (done in this branch) | `internal/guard` passes and fails when it should |
| A | `ui_inspect`, `ui_preview`, `ui_simulate`, `ui_test`, history, `/ui export` | the agent checks its own change before asking you |
| B | data store with schemas, the verb catalogue, shells / queue / input / chat as structured data, event payloads, `extract` that joins, `output.post` | every value and verb of the app is in the registry; a test enumerates both. Split in B1 `host.shell.rows` (done), B2 verb catalogue, B3 event payloads, `extract`, `output.post` |
| C | keymap with contexts, tier 1, tier 2, the message queue, mid-turn steering (core too) | `ctrl+o`, `ctrl+f`, the down arrow and Enter-while-busy are no longer decided in Go |
| D | components, panels, windows, mouse; menus and `/provider` as windows | no Go file draws a particular screen (a test searches for it) |
| E | effects, widgets, streams, pictures | word-by-word animation and a decibel bar exist as profile content |
| F | voice package | speaking to arxi end to end on at least Termux and one desktop |
| G | the factory interface as a profile, and promote-to-factory | the default TUI is the profile, with the same bytes on screen |

Each phase ships on its own and the factory goldens do not change until G. Every phase carries its
acceptance scenarios from section 9 as tests.

## 9. Acceptance scenarios

Each is a request to the agent, resolved without Go, from a clean install. The right column is the
first phase after which it can pass.

| # | Request | Needs |
|---|---|---|
| 1 | "A button under the input with the number of running shells; Enter opens the shells" | A (`host.shells` plus a button with `cmd:/shell`; checked by a test) |
| 2 | "Let me reach that button with the down arrow from the last line of the input" | C |
| 3 | "A window listing the shells; choose one to see its output" | B1 for the list and the choice (`host.shell.rows`, `/shell show {row.id}`), D for the window |
| 4 | "Queue what I type while you work, show how many are waiting, let me edit or cancel them" | B, C |
| 5 | "One line per tool call, a blank row between them, and `ctrl+o` cycles one line / cut / full" | D, C |
| 6 | "Thinking: words appear, the line is cleared when full, then it continues" | E |
| 7 | "Redesign the `/provider` screen and the `/settings` menu: look, order, how I move in them" | D |
| 8 | "A decibel bar above the input driven by the microphone" | E, F |
| 9 | "Talk to arxi instead of typing, and have it speak its answers" | F, B |
| 10 | "A command that opens a window with a candlestick chart of a symbol" | E |
| 11 | "A command that shows several pictures in a window" | E |
| 12 | "Translate what I write to English before sending; translate your answers to Spanish" | B |
| 13 | "A side panel with the project's files" | D |
| 14 | "Make my TUI the default" | G |

## 10. Decisions of the owner

All four were answered by the owner and are now closed.

1. **Budget:** 11000000 bytes for the Termux binary. Approved.
2. **Tier 2:** a small JavaScript subset interpreter written in this repository, with no dependency.
   Approved.
3. **Order:** A, then B, then C. The rest follows. Approved.
4. **First visible result:** scenarios 1 to 3 (shells), after A and B. Approved.

## 11. Risks, stated

- This is a large change to the one place everything meets (`main.go`). The phases are ordered so
  that each can be reverted, and the factory goldens do not move until the end.
- The interpreter is the part most likely to cost more than estimated.
- A fully editable interface can be edited into a worse one; history, undo, safe mode and
  profile tests are the answer and must exist before the windows do (hence phase A first).
- Real steering needs the core to change; until then the queue is the honest version.

### Built (B3, part 3): `output.post`

A rule with `"stage":"output.post"` rewords what the user is SHOWN of each answer of the model.
It reads the answer as `${input}` and its `transform` text is drawn instead. The model's own
words are kept everywhere that remembers: the model's history, the saved session, and the
`${text}` of `turn.done` events. The host draws a copy of the `llm.response` event (as
`sentPaste.forChat` does for a pasted block) and logs the original.

Limits, all tested: a rule can only reword or say something (no `handled`/`block`, no `send`,
no empty text; `on_error: "block"` is refused at save); a failing rule shows the answer as the
model said it; at most 8 rules and 2 s per answer; an answer over 4000 characters is shown
as said, with a note; a piece of a streamed answer keeps the spaces at its ends. While the
rules run (on a worker) later events wait in order; `/clear` and `/resume` drop an answer in
flight. With no `output.post` rule nothing is held. The consent screen has its own line.

### Built (C1): tier 1, `internal/expr`

A small expression language for conditions and short calculations, for example
`focus == 'prompt' && input.at_end && shells.count > 0`. It is a package of its own and nothing in
the TUI uses it yet; the keymap (next in C) will be its first user, so no binary-size change is
claimed here (about 600 lines of Go, no dependencies).

- **Names are checked when the expression is compiled**, against a schema the host gives
  (`name -> yes/no | number | text`). A misspelt name is refused where it is written, with up to
  three near names ("did you mean shells.count?").
- **Strict types, no truthiness.** `shells.count` alone is refused with "compare it, for example
  `(shells.count) > 0`". `&&`, `||` and `!` take yes/no; `<` takes numbers; `==` takes two values
  of the same kind; `+` adds numbers or joins texts. Every refusal says what to write instead.
- **It always ends and stays small.** No loops, functions, assignment or recursion. At most 400
  characters, 120 parts and 24 levels deep; numbers are whole and at most 2^40, so a product that
  would wrap is an error and not a wrong answer; a joined text is at most 1000 characters; division
  by zero is an error with a remedy.
- **A missing value is an error, never `false`.** `&&` and `||` stop early, so a name on the right is
  not read when the left already decides. A host that gives another kind than its schema promised
  is reported as "the host and its schema disagree".

Each behaviour above was checked by breaking it on purpose: more than 60 mutants of the parser, checker and
evaluator, each caught in the end by the test that names it (some survived at first and led to new tests: a product
that wraps to exactly 0, `<`/`>` against `<=`/`>=`, an escaped backslash, a single `&`, a `)` inside a
text, the order of suggestions, and the order of `Names()`).

### Built (C2): the keymap as data, first slice (`ctrl+o`, `ctrl+f`)

`internal/keymap` reads `{version:1, bindings:[{key, when, do}]}`. `when` is a tier-1 yes/no
condition over names the host publishes (`menu.open`, `key.user_bound`, `screen.open`,
`approval.pending`, `find.open`); `do` is the same closed action grammar a button
carries, checked with the rules of a user shortcut. The first binding of a key whose `when` holds
wins, in file order. A binding hidden behind an earlier one with no `when` (or the same `when`) is
refused at load. `ctrl+c`, plain letters and unknown key names are refused, each with what to write.
A condition that cannot be decided shows a notice and does not take the key.

- `ctrl+o` is `do:view.expand`; `ctrl+f` is `do:find.toggle`, two bindings (the bar is open: always,
  unless a screen or a question stands in the way; the bar is closed: only when no menu, no user
  shortcut, no screen and no pending question stands in the way). Two new Free verbs, `view.expand`
  and `find.toggle`, run from a key or a button; queued without a screen to change they say so.
- What used to be said by where a branch sat in the Go chain of `main.go` is now written in
  `internal/keymap/factory.json`. The `isCtrlO` and `isCtrlF` helpers are gone; the find bar keeps only
  the keys it owns while open (typing, Enter, up/down, Esc).
- Not done yet: `down` (focus the shells chip). Checked while reading the code: **there is no shells
  chip and no focus target for one**, and `down` today only walks the history, a menu or the caret. So
  that row is not a move of existing behaviour but a new widget (a chip with focus); it needs the
  widget first (phase D) and is left for it.

- Size: the android/arm64 binary (`CGO_ENABLED=0`, `-s -w`, `-trimpath`) is 9,830,753 B against
  9,765,217 B on master, +65,536 B. That includes `internal/expr`, which is linked in for the first
  time here. The limit is 11,000,000 B.
- Checked by breaking it on purpose. `internal/keymap`: 15 mutants, each caught by the test that names
  it (one needed a new test first: `alt+ctrl+g` and `ctrl+alt+g` being one key). Host side
  (`factory.json` conditions, `keymapState.lookup`, the facts the loop gives, the verbs, the action
  check): the mutants of the three conditions in the file, of `lookup`, of the user-bound, menu,
  approval and screen facts, of the verbs, and of the action check are each caught by the test that
  names them. Four needed something first: the screen fact and the action check had no test through
  the loop (a loop test with the automations screen up, and a test of what a key may carry, were
  added); two mutants of the loop's facts did not build and were redone in a form that does.
  **Not caught:** making the loop's `find.open` fact always "no". Every test still passes, because
  with the bar open the second binding of ctrl+f holds anyway and `find.toggle` closes the bar from
  the loop's own state. It only matters when a menu is open or the user bound ctrl+f while the bar
  is open, which I could not reach from the keyboard. So it is either equivalent in practice or a
  gap I did not find; it is listed here instead of being passed over. `consent.open` was removed
  from the names: it could never answer yes, because a consent screen is handled before the keymap.

### Built (C3): `enter` while the agent works

- `enter` is now a binding (`agent.working && !menu.open && !screen.open && !approval.pending &&
  !find.open && !ui.focused && !key.user_bound`, `do:queue.push`). New names: `agent.working`,
  `ui.focused`. New Free verb: `queue.push`.
- This one is **a new behaviour, not a move**: until now Enter on a busy session handed the line to
  the chat, which refused it, and the text was lost. The queue is in the loop: up to 8 lines, one sent
  per finished answer (through the `input.pre` rules like a typed line), put back on the input when
  the answer is stopped or fails, forgotten by `/clear` and `/resume`. The line goes into the history.
  A line with a picture is refused with a notice, because the marker would outlive its picture.
- Real mid-turn steering was still core work at this point (done later: C7 below). C4 below adds the
  stop-and-send form of steering.
- Size: the android/arm64 binary (`CGO_ENABLED=0`, `-s -w`, `-trimpath`) is 9,896,289 B against
  9,830,753 B on master, +65,536 B. The limit is 11,000,000 B.
- Checked by breaking it on purpose (30 mutants in two rounds, run on a committed tree). The first
  round found that two things I had added were not needed and no test noticed them: the `input.filled`
  name and a guard against synthetic events. Both were removed rather than tested. It also found five
  real gaps, each closed with a test before this note was written: the focus fact, the condition
  table for the key, the history, the picture refusal, and the forgetting of the queue. Of the last
  round of 14 mutants, 12 were caught at once and the other 2 (the focus fact and `msgQ = nil` on
  `/clear`) are caught since by `TestEnterOnAFocusedButtonPressesItWhileTheAgentWorks` and by
  `TestClearForgetsTheQueue` / `TestLinesGivenBackAreNotSentAgainWhenALaterAnswerEnds`.
  **Not checked:** the mutation of `if busy {` in `drainQueue` did not build in the form I wrote, and I did
  not redo it, so the wait-and-retry path (the session still finishing a turn after `turn_done`) has no
  test of its own. The test driver is never busy after `turn_done`. A real session is, for a moment,
  which is why the retry exists. It is the least checked part of this change.

### Built (C4): steer, `enter_while_busy` and the queued lines on screen

This is the part of the older, never-merged work in `063e98e` (#246) that fits the keymap, ported by
hand on top of C3. Its model-memory part (tool-call record in the history) is **not** ported: master
already has its own (`chat_history.go`, `chat_memory.go`), and the two do the same job.

- `alt+enter` is a binding, and `enter` has a second one for the other mode. Four rows in
  `factory.json`: `enter`/`alt+enter` x `busy.steers`, to `do:queue.push` or `do:queue.steer`, all
  under the same guard as C3. New name `busy.steers` (the user's setting). New Free verb `queue.steer`.
- Setting `enter_while_busy` (`queue`, the factory way and stored as nothing, or `steer`) in
  `behaviour.json`, typed as `/ui busy queue|steer`, taught in the guide the model reads, and always put
  to the user when the agent proposes it (`touchesWhatRuns`): steering stops work in flight.
- Steering is `chatSession.stopForSteer`: like a cancel but with no "Cancelled" line and the waiting
  lines are not given back. The stopped turn stays in the history (request, tools, and the note
  `whyInterrupted`), so the new line reads as a correction. A stopped turn says nothing more: `post` now
  ignores a turn whose context is cancelled (this also covers a cancel). A compaction is left alone.
- The waiting lines are drawn dim under the conversation, `┃ text  (queued)` (token `chat.queued`),
  from `State.Queued` (events `chat.queued` and `chat.unqueued`, which the host emits). They replace the
  C3 note "queued (N waiting)".
- A line with a picture can steer (it is sent at once); it still cannot wait in the queue.
- Not done: Windows Terminal never delivers `alt+enter`, so on that terminal only the setting reaches
  steer (said in KEYS.md). The core cannot take a message mid-turn, so a steer is a stop plus a new
  request, and what the model was writing is lost. A session that cannot be stopped (no
  `StopForSteer`) keeps the line and says so.
- Mutation checks (committed tree, one mutant at a time). First batch of 18: 12 were caught by the
  intended test, 3 survived (the cancelled-context check in `post`, and the two `chat.unqueued` marks
  that take a line off the screen when it is sent or given back) and 3 did not compile. After adding
  tests the 3 survivors are caught, and a rewritten form of one of the non-compiling ones (the
  "driver cannot steer" refusal) is caught. A second batch of 6 had 4 caught, 1 that did not compile
  and 1 survivor: keeping the pictures on a steering line. That survivor was a real bug, not a weak
  test: the keymap decides the key before the block that attaches a line's pictures, so a steering
  line lost its picture. It is fixed, a test was added, and the mutant is caught. **Not checked:** the
  wait-and-retry of `drainQueue` (still no test of its own), and the removal of `queuedMark(text)`,
  which I could not write as a mutant that compiles.


### Built (C7): a line written to an answer in flight

Core half: ADR-0055 (core #328). TUI half (branch `feat/tui-steer`):

- When a `chat.send` with tools runs, the core writes `chat.steerable`. From then on the driver can
  write `{"type":"chat.steer","text":...}` on the turn's own connection (`ChatSendParams.OnSteerable`,
  serialised with the reader's other writes by `NDJSONDriver.wmu`, `ErrTurnOver` after the call ends).
  The core reads it between a tool round and the next ask, appends it as a user message, and answers
  `chat.steered` with the text it took (several texts since the last ask are joined by a blank line).
- `queue.steer` first tries that (`liveSteerer`: `CanSteer`, `Steer`). The model keeps everything it had
  read and done. It falls back to the C4 stop-and-resend when there is no live steer: an older core,
  a line with pictures, package input rules (a line written straight to the turn would skip them), a
  turn with no tools, a write that failed.
- A text written stays *owed* (`chatSession.steerOwed`) until `chat.steered` says the core took it.
  What the core never read (the turn ended first) comes back as `chat.steer_unsent`, which the loop puts
  on the queue; cancelling gives it back too; `stopForSteer` folds it into the history note instead,
  because the replacement line overtakes it.
- What the core took shows as a user line in place (`chat.steered` in `internal/fold`), is recorded in
  the saved session, and enters the history as an "App note" appended to the request
  (`steersNote`), live and when resumed alike, so "retry" after a stopped turn means the request as it
  had become.
- Wire only, no new surface capability: an older core never says `chat.steerable`, and the TUI never
  writes `chat.steer` to it (it would answer it as a response, which the turn's reader would take as the
  turn's own result).
- Size: android/arm64 10,223,969 B, the same as master (the binaries differ; the size is aligned).
  The limit is 11,000,000 B.
- Tests: driver (4, with `-race`), chatSession (10), fold (2), history live-vs-resumed (2), and the
  loop with the real key (4). Mutation checks (committed tree, one mutant at a time, 17 first-round
  mutants plus 4 reformulated): 13 caught by the intended test at once, 2 survived at first
  (`steered`/`steerable` ignoring which turn they belong to; the old-turn test had no running next
  turn, so `canSteer` hid it), and 2 did not compile (a `gen+1000` variant of the give-back and a
  note without `owed`). After rewriting the old-turn test and the two mutants, all four are caught by the
  right test. Driver half, 6 mutants on `callWatching`: 4 caught by the intended test, 1 did not
  compile (rewritten, then caught), and **1 survives without `-race`**: removing the write lock around
  a steer is caught only by `TestSteeringWhileDecisionsAreWrittenKeepsEveryLineWhole` under `-race`,
  and CI does not run `-race`. So that lock is not protected by CI today; run
  `go test -race ./internal/driver` when touching it. **Not checked:** the real core against the real
  TUI end to end (each half is tested against a fake of the other).
- Not done: the `down` row (needs the phase-D shells chip), a line with pictures cannot steer live.

**Built (C8): `down` reaches the shells chip.** The last row of the C keymap, now that there is something to reach.
- The shells row of the default scene is pressable: its text node has the id `shells_chip`, `on_press: cmd:/shell`
  and `focus_glow` (`header`). Unfocused it draws the same bytes as before; only the scene hash in
  `docs/GUARDRAILS.md` moved, because the JSON gained three fields.
- Two factory bindings, in `internal/keymap/factory.json`: `down` -> `focus:shells_chip` and `up` -> `do:focus.input`
  (new Free verb). `down` is taken only when nothing else wants it: no menu, screen, question or find bar, no node
  focused, the user did not bind the key, the arrows do not scroll the chat (phone), the line is not a recalled one,
  the caret is at the end and a shell runs. Every other case keeps the old Go path (history walk, caret move).
- New names for conditions: `input.at_end`, `history.browsing`, `arrows.scroll` (yes/no) and `shells.count`
  (number, the first number name in the schema).
- Fix found on the way: `focusRing` ignored a node's `when`, so Tab stopped on nodes that are not drawn (a chip with
  no shell). It now asks `engine.NodeHidden`, the predicate the renderer asks.
- Tests: ring (1), the `down` table, 11 situations (1), real loop (6: press, return, no shells, caret inside, recalled
  line, phone). Mutants: 10 on the keymap/facts/verb plus 1 on the ring, each caught by the test that names it.
  Not run: a mutant of the `focus_glow` look (no test reads the glow colour here).
- Size: android/arm64 10,289,505 B (limit 11,000,000).
- Not done: Esc as a way back from the chip (Tab and `up` do it), a window listing the shells (phase D).

### Built (D1): a window is a scene

First step of phase D. Acceptance scenario 3, a window listing the shells, now needs no Go.
- A window is a full `overlay` gated on `ui.max.is.<pane>` and opened with `/max <pane>` (or a button `cmd:/max <pane>`);
  `/max` closes it. The test scene (`cmd/arxi-tui/shells_window_test.go`) lists `host.shell.rows`, one pressable row per
  shell (`cmd:/shell show {row.id}`) and a `[close]` button.
- Two bugs found by building it: (1) a full overlay shorter than the screen was dropped without a word, it only
  replaced the screen when it was at least as tall; it now pads to the frame height and hides the caret under it
  (`internal/engine/render.go`); (2) the focus ring was built from a state without `ui.max`, so Tab found nothing in a
  window while the frame showed it (`pressState` now takes the cursor).
- Tests: engine (window absent closed, present open, input covered, frame height, caret hidden) and the real loop
  (open, Tab to the second row, Enter, the output of that shell and not the other, close with its own button).
  Mutants on a committed tree: no padding, old "only when taller" rule, caret shown, press state without `ui.max`;
  each caught by the test that names it. The caret mutant survived the first test and a caret check was added.
- Not done yet in D: the Go-drawn screens (hub, settings, keys, automations, team, flow, installer) are still Go; docked
  panels, tabs, popups and the mouse; the test that searches for Go files drawing a particular screen.

### Built (C5): tier 2, `internal/script`

An interpreter for a small, strict subset of JavaScript, written in this repo with no dependency
(about 3,900 lines of Go without tests). It is **not wired in yet**: nothing in `cmd/` imports it, so
the Termux binary did not grow (9,961,825 B, limit 11,000,000 B, measured on android/arm64). Wiring
it to a script-backed verb or keymap action is the next PR, and the size is measured again there.

- **API.** `script.Load(src, Config{Name, Globals, Limits})` parses, checks every name and runs the
  top level. `Script.Call(name, args...)` runs a top-level function; `Eval` runs `main()`. A script
  sees only `Config.Globals`: data is copied in and out, a `HostFunc` is the only door to Go. State
  kept in top-level `let`s lasts between calls; a failed call keeps what it did and the script stays
  usable. Panics become an "internal error" `*Error`.
- **Language.** `let`/`const`, functions, arrows, closures, `if`/`for`/`for of`/`while`, `break`/
  `continue`, destructuring, defaults, rest, spread, template texts, `?.` and `??`, lists and objects
  (keys in the order they were set), and `Math`, `Number`, `String`, `Object`, `JSON`, list and text
  methods.
- **Strict as tier 1.** No truthiness (conditions and `!` need a yes/no), `===`/`!==` only, no
  `NaN`/`Infinity` (a result that would be one is an error), `+` joins text with text only, a read
  before the declaration has run is an error, and a `const`, a host global or a function cannot be
  assigned (refused at load).
- **Refusals say what to use instead.** `class`, `new`, `this`, `async`, `try`, `==`, `&`, `var`,
  `require`, `eval`, `Date`, `RegExp`, `console`, `parseInt`, `Array(n)` ... each fails with the line,
  the column and a remedy ("Consequence: ... Remedy: ..."). That text is the repair loop the eval
  measures: the model reads it and fixes its own script.
- **Limits owned here** (`DefaultLimits`): 200,000 steps and 4 MiB allocated per call (counters start
  fresh on every call), calls nested 64 deep, 1 MiB of live state kept between calls, 1 MiB per text.
  A limit error is its own kind (`KindLimit`), apart from syntax and runtime errors.
- **Tests.** About 40 tests in `script_test.go`, including the message queue written as a script.
- **Mutation checks** (committed tree, one mutant at a time). Caught by the intended test: the step
  limit (by a **timeout**, since without it the test hangs, not by an assertion), the alloc limit, the
  depth limit, the Live-state limit, truthiness in `!`, `==` refused, `const` assignment, use before
  declaration, a fresh binding per loop round, state kept after a call, `+` on mixed types, the
  `NaN`/`Infinity` check, strict equality for numbers, texts and yes/no (after a test was added for
  yes/no), and the panic recovery. **One real gap found and closed:** giving the depth back after a
  call survived, because no test made many calls in a row; `TestManyCallsOneAfterTheOtherAreNotNesting`
  catches it. Mutants that did not compile or changed nothing were discarded and redone, not counted.
  **Not checked:** the refusal word lists one by one, the text and list library functions, and the
  per-call fresh counters (my mutant for that did not compile, and a second attempt was not run).
- **Not done:** `switch`, `try`/`catch`, regular expressions, `Date`, getters, classes (refused on
  purpose, each with its remedy); timing of a script (steps count work, not milliseconds).

### Built (D3): a click presses

Second step of phase D, the mouse. Until now the pointer only dragged the chat scrollbar; a button or a list row was
dead to it, so every window needed the keyboard.
- Every `ui.Span` carries `Hit`: the focus key of the pressable node that drew it (its id, or id and row for a node
  in a `row_template`, the same `\x00row\x00` key Tab uses; `engine.HitKey`). The innermost pressable node wins; a
  node with no id has no target, as in the ring. Emit ignores `Hit`, so no golden moves. It survives wrapping,
  cutting, overlay compositing, the find highlight and gradient cells.
- A left press on a span with a `Hit` runs `pressFocused`, the function Enter on a focused node now calls too, so a
  click and Enter cannot differ on what a press does. The draft being typed stays. A click is ignored while a screen
  (hub, team, flow, automations, installer) or a consent modal is up (`clickMayPress`).
- Tests: engine (stamp, innermost, row key, wrapped, composited overlay, idless row, find), ui (wrap), theme (cycle,
  gradient cells), loop (click a row, click `[close]`, click on text, draft kept).
- Mutants on a committed tree, each caught by the test that names it: outer node overwrites inner, rows keyed by id,
  idless nodes stamped, `HitAt` by byte, wrap drops `Hit`, cut drops `Hit`, cycle (both branches) drops `Hit`, find
  drops `Hit`, a click clears the draft. First runs of the wrap, cycle and find mutants SURVIVED; tests were added.
- NOT covered, said plainly: the call to `clickMayPress` inside the loop. Its table test passes, but removing the call
  in `main.go` survived: no loop test reached a state where a screen is up with a pressable node under the
  pointer (no Go-drawn screen has `on_press`). A consent modal is likewise untested for clicks.
- Not done: hover, drag, right/middle button, a click that moves the caret in the input, mouse on Termux (the mouse is
  left to the terminal there on purpose, see `wantMouse`).

### Built (D6): a profile may replace a screen

`/provider`, `/team`, `/flow` and `/auto` were already scene documents, but embedded: no profile could change them.
`loadScreen` (`cmd/arxi-tui/screen_override.go`) reads `<settings>/screens/{hub,team,flow,auto}.json` first and falls
back to the embedded document. The replacement is parsed and validated; a broken one is refused with a notice that
names the file, and the built-in screen is shown, so a typo can never remove `/provider` (where credentials are set).
Only the everyday interface reads the folder (`persistsScene`), so `-raw` and `-scene` stay what they were asked for.
- Tests: no file, valid file, three kinds of broken file (not JSON, unknown bind, empty), the folder ignored when not
  allowed, and through the real loop: `/provider` shows the profile's screen, shows the built-in one without a profile,
  and an explicit scene ignores the folder.
- Mutants on a committed tree, each caught by the test that names it: loop never allows the profile, loop always
  allows the folder, a broken file used anyway, no notice on a broken file, validation skipped (caught for "unknown
  bind" only). The two loop mutants were caught by the 30 s wait timing out, not by an immediate assertion.
- Follow-up: `/flow`, `/team` and `/auto` now have a real-loop test each (`screen_override_screens_test.go`).
- Installer (follow-up): `installer.json` replaces the community installer, proven through the real loop against a local
  TLS registry (`newRegistryFetchClient` became a variable so a test can inject the client); mutant (direct
  `ext.LiveInstallerScene()`) caught by the 30 s wait timing out. `/settings`, `/keys`, `/model` and the slash menu are
  not separate screens: `/settings` and `/keys` borrow the `model.active` overlay of the main scene, so they are reshaped
  by replacing the main scene, which already works; no test replaces just that overlay.
- NOT done, said plainly: a replacement is not re-read while the
  app runs (restart to see a change); there is no `/ui` verb to write one, so today it is a file edit. The sandbox
  was reset once while this was being written and the first attempt was lost before it was pushed; this is the redo.

### Built (D5): tabs and a popup are a scene, no engine change

Tabs and popups needed no new node type. `ui.max` is a free name the host stores (`cmd:/max <name>` writes it, `/max`
clears it) and `ui.max.is.<name>` / `ui.max.none` gate any node on it, so a tab strip is buttons that write the name and
each pane is gated on its own, and a popup is an `overlay` gated the same way, closed by `cmd:/max`
(`cmd/arxi-tui/tabs_popup_test.go`).
- Tests: a click on one tab shows its body and a click on another swaps it with no two bodies at once; a popup opens from
  a button and a click on its own `[close]` brings the scene back; nothing reaches the model.
- Mutants on a committed tree, each caught: clicks never press (both tests), `/max <name>` ignores the name (both
  tests), the `top` overlay composites no rows (the popup test only, which is the right one).
- NOT done, said plainly: one cursor serves one selector, so two independent tab sets (or a tab set plus a maximized
  pane) on one screen collide on `ui.max`; a scene-owned value of its own is the fix and is not built. Keyboard
  navigation between tabs (arrows) is not tested, only Tab/click. A popup does not trap focus: Tab may still reach
  nodes behind it. Hover and drag are still to do.

### Built (D4): a side panel is a scene, no engine change

Acceptance scenario 13 (a side panel) needed nothing new: a `row` already splits its width by `weight` and joins its
children side by side. D4 only proves it and pins it (`cmd/arxi-tui/side_panel_test.go`).
- A row of a `list` (weight 1) and a main area (weight 3) puts the main area at column 20 of 80, and a shell and the main
  area share a screen row.
- A click on a row of the side panel presses it (`/shell show {row.id}`) and shows the right shell, so `Hit` survives the
  side-by-side join.
- Mutants on a committed tree: weights ignored (caught by the width test; the first try did not compile, an unused
  variable, and was redone), `Hit` dropped in the join (caught by the click test).
- NOT done, said plainly: a panel the user can resize, hide with a key, or dock to the other side is not tested; the
  chat pane (`markdown`) inside a weighted row was not tested because the test driver does not echo a sent message, so
  a panel beside a chat with history is checked only with a fixed main area. Splits are the same `row` (or a `stack`),
  so they are covered by the same mechanism, but not by a test of their own. Tabs and popups are still to do.

### Built (D2): a test that no Go file draws a screen

The exit test of phase D, first half. `TestNoGoFileDrawsAScreen` (`cmd/arxi-tui/no_go_screens_test.go`) parses every
non-test Go file of the host and fails if any function other than `emitFrame` names `ui.Line`, `ui.Span`, `ui.Frame` or
`ui.Cursor`. It passes today: the hub, flow, automations, team and installer screens and every menu already reach the
terminal through a scene document (or the default scene) and binds; Go supplies data, the engine draws.
- Mutant: a new file with a function that returns a `ui.Line` fails the test and names the file and line.
- Limits, said plainly: it sees only those four selectors. A file that imports `ui` under another name, or that writes
  ANSI text straight to the terminal, would not be seen. It proves where lines are built, not that every screen has
  a scene a user can edit: that is the rest of D (menus and `/provider` as scene windows the profile can replace,
  panels, tabs, popups, mouse).

### Built (C6): tier 2 reaches the interface, `do:script.run`

The interpreter of C5 is now used. A user script is a text in `behaviour.json` (`scripts`, name ->
source), and a key, a command or a hook calls it with `do:script.run <name> [text]`.

- **One door out.** The script declares `function run(text, facts)`. `facts` is `{effort, mode,
  style, working}`. What `run` returns is all it does: a text is a note, a list of texts is a list of
  actions, `null` is nothing. Nothing else leaves the script (no file, network, model, clock).
- **Checked like a shortcut.** The returned list goes through `validateActions` (a hook's call is
  checked as a hook's: no `ext:`, no `open:`), so a script cannot start a typed verb, answer for the
  user or reach a plugin without a keypress. A script cannot start a script, so no definition loops.
  What a hook's script sets never fires another hook, as for any hook.
- **State.** Top-level `let`s last while the program runs and start over when the script's text
  changes (the source hash). They are not written to disk. A failed call keeps its partial change and
  the key works on the next press; the error is a note with the script name, line and remedy.
- **Saved only if it works.** `behaviour.validate` loads every script (parse, names, top level, has
  `run`), so a script that cannot run is refused when the agent saves it, not when a key is pressed.
  At most 16 scripts of 16 KiB. A patch adds, replaces or removes (`null`) a script, and is always
  put to the user (`touchesWhatRuns`). A trusted project layer ships scripts too (by name, the
  project wins).
- **The model is taught.** The guide that `ui_guide` returns has a `scripts` entry with the contract,
  the language and an example; a test runs that example, so the guide cannot teach a broken one.
- **Size.** android/arm64 went from 9,961,825 B to 10,223,969 B (+262,144 B) with the interpreter
  imported; 776,031 B remain under the 11,000,000 B limit.
- **Mutation checks** (committed tree, one at a time). 14 mutation sites. The first form of 3 of
  them did not compile; each was rewritten and then caught. 13 of the 14 were caught by the intended
  test (10 at the first try, 3 after that rewrite: the saved-script check, the count limit, the source hash, the refusal of a script starting a
  script, the hook/key check of the returned list, patch add/remove, the project-layer merge, the
  clone of the layer, the effort fact). 1 survived at first: passing `false` instead of `q.hook` to
  the script call, so a hook's script was checked as a key's. A loop test was added and that mutant is
  now caught. **Not checked:** the exact words of the refusals, and the facts `mode`, `style` and
  `working` (only `effort` was mutated).
- **Not done:** a script cannot read the input line, the conversation or the shells (only the four
  facts), cannot draw, and has no timers. Those arrive with the data store of phase D/E, as more
  `facts` and more verbs, not as new doors in the interpreter.
