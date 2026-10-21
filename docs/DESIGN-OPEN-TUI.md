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

- The core declares `--on-busy steer` but honours only `queue`; steering does not interrupt a turn
  (`core/cmd/arxi/inject.go`, `steer.go`) **(verified)**. Real mid-turn steering is core work.
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
- Not done yet: `down` (focus the shells chip) and `Enter` while the agent works (`queue.push`); they
  need names such as `input.at_end`, `shells.count`, `agent.working`, and the message queue. A keymap
  file the user can edit (`/ui keymap`) comes after those rows move.
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
