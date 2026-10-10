# arxi tui

The interface is a document. The running instance rewrites it live — you can
too, from inside it.

A terminal agent interface whose entire chrome (rows, panels, overlays,
animations, menus, the input bar itself) is a **scene document** addressed by
node id and edited through commands, patches, or the agent acting on your
word. The factory look is written with the same format you have; nothing the
interface can do is closed to you. The range is real: from two nodes
(type and answer, nothing else) to a full dashboard of panels, widgets and
plugins — and you can install a plugin, or a whole interface someone else
published, by handing arxi a link.

- `AGENTS.md` — how this repo is worked on (English-only, comment/test/commit
  policy, invariants).
- `docs/PLAN.md` — the plan of record: the spectrum, the decisions, the
  phases, the install rule.
- `docs/SCENES.md` — the golden scenes and the 23 settled format decisions.
- `docs/LESSONS.md` — the bugs and hard rules already paid for by arxi-sim
  and the arxi core, with sources. Do not re-derive them.
- `docs/BINDS.md` — the bind vocabulary: every field the host exposes to scenes.

## Status

## Scrolling a long conversation

The mouse wheel scrolls the chat. While you are scrolled away from the bottom, the top of
the chat repeats the question you are reading the answer to (at most two rows, with `…`
where it is cut), so a long answer never loses its question. It is drawn only for rows that
have really left the window, and not at all while you follow the newest text.

### How your messages look: `/style`

`/style` chooses how your own messages are drawn in the conversation: `bar` (a `┃`
marker down the left edge, the default), `band` (the same marker over a shaded block
that runs to the right edge) or `plain` (no marker, just brighter text). The choice is
remembered between sessions and also applies to the question pinned at the top while you
scroll. It only changes how things look; the model never sees it.

## Status bar and agent modes

The bottom bar reads `mode · model · thinking level · directory`, for example
`ask · deepseek/deepseek-chat · high · ~/projects/app`. The thinking level appears only
once you choose one.

After the first answer the bar also shows how big the conversation is and what it has
used: `ctx 1.2k · ↑3.4k ↓800`. `ctx` is the size of the latest request (what the model has
to read again on every turn, so it grows as the chat does); `↑` and `↓` are the running
totals sent and received. These are token counts: the provider does not report prices, so
no money figure is shown.

The mode says how much the agent may do without asking. Pick it with `/mode` or cycle it
with Shift+Tab; `/clear` keeps it.

| mode | reads files | edits files | runs commands | reads web pages |
|---|---|---|---|---|
| `ask` (default) | on its own | asks | asks | asks |
| `auto` | on its own | on its own | asks | asks |
| `plan` | on its own | never | never | asks |
| `full access` | on its own | on its own | on its own | on its own |

A command runs in your project folder with no keyboard, is stopped after two minutes
(ten at most), and does not see environment variables that look like secrets (`*KEY*`,
`*TOKEN*`, `*SECRET*`, `*PASSWORD*`, `ARXI_*`). It is **not** confined to the folder: it can
touch anything you can, so the question `Allow this command?` is the only guard. Think
before you press `full access`.

### Reading web pages (`web_fetch`)

The model can read a page you or it names, as plain text (headings, lists and links kept;
scripts and styles dropped). Every page is asked about first (`Allow reading this page?`)
in every mode except `full access`, because reading one sends the address to a third party
and brings back text nobody vouches for. What the tool does and does not do:

- It reaches only public `http`/`https` addresses. Your own machine and network are never
  reached, whatever the model asks or a page redirects to: `localhost`, private ranges,
  link-local addresses (where cloud metadata lives) and the like are refused at connection
  time, on every redirect hop, even in `full access`.
- It gives up after 20 seconds, reads at most 2 MB, hands the model at most 32 KB of text
  (saying how much was left out), follows at most 5 redirects, and refuses anything that is
  not text (PDFs, images, archives).
- It never runs the page: no scripts, so pages that only draw themselves with JavaScript
  come back nearly empty.
- The text is handed to the model wrapped as untrusted content, and the model is told never
  to follow instructions found in a page. That reduces the risk of a page hijacking the
  conversation; it does not remove it, which is why the question is asked.

### Searching the web (`web_search`)

Search needs a search service, and you choose which one. Until you do, the tool is simply
not offered to the model. The easy way is the `/search` command: pick a service, paste its
key (or, for SearxNG, its address) and it is saved on this computer and used from your next
question, with no restart. The key is kept in `search.json` in `~/.arxi`
(`ARXI_CONFIG_DIR` moves it), readable only by you, and choosing "Turn web search off" deletes it.

The same thing can be set with environment variables before starting `arxi-tui`, and they
win over what `/search` saved (the screen says so when that is the case):

| service | `ARXI_SEARCH_BACKEND` | also set | notes |
|---|---|---|---|
| Brave Search | `brave` | `ARXI_SEARCH_KEY` = your API key | independent index; has a free monthly allowance |
| Exa | `exa` | `ARXI_SEARCH_KEY` = your API key | built for AI agents; paid after a free allowance |
| SearxNG | `searxng` | `ARXI_SEARCH_URL` = your instance, e.g. `http://localhost:8080` | free and private if you run it yourself; the instance must allow `format=json` (`search.formats` in its `settings.yml`) |

A misspelled or incomplete setting leaves search off rather than half-working. The query is
sent to that service, so each search is asked about first, like a page (`Allow this
search?`), in every mode but `full access`. Results come back as titles, addresses and
short snippets wrapped as untrusted content; the model reads a page with `web_fetch` when
a snippet is not enough. A SearxNG instance you named yourself may be on your own machine
or network: that is the one place the web tools will connect to a private address, because
you asked for it. The key is never shown, never logged and never put in an error.

Long command output and long diffs are cut to a few rows and say `… +N lines (ctrl+o to
expand)`. Ctrl+O opens them all, and closes them again.

The words are the core's own tool policies (`allow` / `ask` / `deny`). The chat has no
tools yet, so for now the mode only changes what the bar says; the table is what the tool
loop will enforce. The directory is where the TUI was started (home shown as `~`). While
the `/` menu is open the bar shows only the navigation hint.

### Sessions and `/clear`

A *session* is the conversation on screen plus the history sent to the model with
each prompt. `/clear` (typed, or picked in the `/` menu) starts a new one:

| dropped | kept |
|---|---|
| the transcript and its scroll position | providers and their keys |
| the chat history sent with the next prompt | the selected model |
| a reply still in flight (its answer is discarded, never shown in the new session) | the thinking level (`/effort`) and the mode (`/mode`) |
| the run being followed, if any, and its actor label | the scene, theme and hidden/maximized panes |
| the notice line | |

### Saved conversations and `/resume`

Every conversation is saved as you go, so closing the program loses nothing. `/resume`
opens a list of them, newest first, each with its first question and how long ago it was
used; type to filter the list, Enter to pick one. The conversation comes back on screen
exactly as it was, and the model carries on from where it stopped (the next question is sent
with that history). `/clear` starts a fresh conversation without deleting the old one.

What is saved is what you see: your questions, the answers, the files and pages the model
looked at, and errors. Not saved: the model's streamed thinking, an approval that was still
waiting for your answer, and your settings (model, effort, mode), which are yours and not
the conversation's. Files are kept in `~/.arxi/sessions`, readable only by you. The latest 100 are kept and older ones are deleted. A conversation can contain
anything you typed, so set `ARXI_SESSIONS=off` to save nothing at all.

### Asking the agent to change the interface

Start the request with `/ui` (or put `@ui` anywhere in it) and ask in your own words:
`/ui add a blank line between the input bar and the status bar`, `/ui make the blue words
in your answers purple`, `@ui move the status bar above the chat`. Without `/ui` or `@ui` the
model is not given the interface at all, so when you are building a TUI of your own, "the TUI"
means yours. (A `/ui` line that is one of the typed commands below, such as `/ui undo` or
`/ui add node ...`, is that command, not a request.) The model reads how this
interface is built, proposes the change, and you see the diff before anything moves:
`Allow this change to the interface?  y yes · n no`. Allowed, it is drawn at once and kept
for your next session.

- **It does not look in your project for it.** The interface lives inside the program (a
  release has no source tree), so, on a `/ui` request, the model is given two tools of the program's own:
  `ui_guide` (how the interface is built: the live document, every node type and bind, and
  every style token with its current colour and what it paints) and `ui_edit` (propose a
  change). It is also told that it runs inside arxi-tui and is handed the guide in the same turn, so
  "the TUI" or "your colours" never send it searching your files.
- **Colours and layout.** The layout is the scene document; the colours are style tokens.
  The coloured words in an answer are `markdown.code` (inline code) and `markdown.link`,
  cyan by default. You can do the same by hand: `/ui color markdown.code fg=magenta`
  (`fg=`/`bg=` with a name such as `magenta` or `bright-magenta`, `0-255` or `#rrggbb`,
  plus `bold`, `italic`, `underline`…); `/ui color markdown.code` alone puts the default
  back. Your colours are kept in `theme.json` in the settings folder.
- **The words of the menus and screens.** The sentences the program writes itself are not
  fixed in the code: what each command in the `/` menu is described as, the meaning beside
  each level of `/effort`, mode of `/mode` and style of `/style`, the key legends, and the
  whole of `/team` (its title, its rows, its explanations). Ask for them in any language:
  "put /team in Spanish". By hand, `/ui text team.title Agentes y equipos`; a key alone
  (`/ui text team.title`) puts the original back. The keys, each with what it says now and
  where it appears, are in the guide the model reads. Your wording is kept in `texts.json`
  in the settings folder and is undone and reset together with the layout and the colours.
  What is not yet a key: the labels and messages inside forms and error messages. The model
  is told this, so it says so instead of promising it. A text may not contain control
  characters (a terminal would act on them instead of showing them), and a title or a hint
  must be one line.
- **Behaviour, not only looks.** What the interface *does* is a fourth layer of data,
  kept in `behaviour.json` in the settings folder and undone and reset together with the
  layout, the colours and the words. It holds: `animations` (colours that move, usable
  anywhere a style token goes), `menu_keys` (the keys that steer the `/` menu and the
  `/effort`, `/mode`, `/style`, `/resume` and `/model` menus), `keys` (shortcuts: an F-key
  or a ctrl/alt chord that runs actions), `commands` (your own entries in the `/` menu) and
  `hooks` (a reaction when you change effort, mode or style). Ask the model, or type it:
  - A rainbow when the effort is `max`:
    `/ui animate rainbow red,yellow,green,cyan,blue,magenta spread=1 bold`, then
    `/ui set status_effort style_by {"max":"rainbow"}`. `style_by` works on any node that
    has a `bind`: it picks the token from the value shown.
  - `/effort` as a horizontal menu: `/ui set models layout horizontal`, then
    `/ui menukeys prev left` and `/ui menukeys next right`.
  - A shortcut: `/ui key f5 cmd:/effort max`. Several actions: `/ui key ctrl+g cmd:/mode plan;cmd:/effort low`.
  - The agent does the same with `ui_edit`'s `behaviour` argument and reads the format in `ui_guide`.
  An action is the scene's own closed grammar: `cmd:/<line>`, `focus:<node id>`, or
  `ext:<plugin>:<action>` to call a plugin you installed with `/ui plugin add` (that is the
  way to add behaviour that needs real code: the plugin proposes, you consent, it never
  writes the interface). Limits that keep it safe: Ctrl-C can never be taken by any key,
  plain letters cannot steer a menu (it filters by typing), a hook never fires from another
  hook or from its own action (so nothing loops), and changes to keys, commands and hooks
  are always put to you, even in full access, because they decide what your keyboard does.
- **The knowledge costs nothing until it is needed.** Only the two short tool descriptions
  travel with each question. The guide (about 1.5k tokens) is sent only on the turn where
  the model asks for it, so a plain question pays nothing for this feature.
- **Every change is checked before you are asked.** A proposal is held to the same rules as
  a scene loaded at start: the binds must exist, the style tokens must be in the theme, no
  property may be invented, and the input bar must stay. A refusal goes back to the model
  with its `file:line`, so it fixes it and tries again; you are only asked about a change
  that would really work.
- **Modes.** `plan` never changes the interface; `ask` and `auto` always ask (this is the tool
  you are using, so even `auto` asks); `full access` changes it unasked.
- **Undo.** `/ui undo` puts back the interface (layout, colours and words) as it was before the last change (a second
  undo redoes it). `/ui reset` returns to the built-in interface, and `/ui undo` brings yours
  back. Your interface is kept in `scene.json` in the `arxi` settings folder; if it ever stops
  loading, the built-in one is shown with a note, and `-raw` always boots the raw scene.
  Changes you type with `/ui` are kept the same way. A scene started with `-scene file.json`
  is shown but never saved over.

This needs an arxi core that knows `client_tools`; an older one says so and the chat works
without it.

`/clear` takes no arguments, so `/clear now` is not a clear. Managed processes (the
arxi core and plugins) are not restarted: the core keeps no chat state (`chat.send`
is stateless), so forgetting the history on the TUI side is the whole reset.

**Phase 0 — Scene engine:** Complete and running.

- ✅ Scene document parser and validator (`internal/scene`)
- ✅ Fold over log events (`internal/fold`)
- ✅ Render engine with all base nodes: stack, row, box, text, markdown, input,
  overlay, list, spinner, marquee, rule (`internal/engine`)
- ✅ Terminal backend with input decoder, alternate buffer, cursor control (`internal/term`)
- ✅ Mock driver for Phase 0 dev, serve driver for Phase 0.5 (`internal/driver`)
- ✅ Event loop with double Ctrl-C escape gesture (`cmd/arxi-tui/main.go`)
- ✅ Golden scenes: RAW, SOBRIA, MAXIMUM running and tested
- ✅ Bootstrap binds: `chat.history`, `user.input`, `agent.working`, `thinking.text`,
  `usage.delta`, `slash.active`, `slash.matches`, `agent.mode`, `model.name`
- ✅ Full test suite passing (9 packages)

**Phase 1 — Tokens and themes:** Complete.

- ✅ Token format and JSON schema (`internal/theme/theme.go`)
- ✅ Token resolver with open definition (no enum)
- ✅ Factory SOBRIA theme: light/dark adaptation via relative dim/bright
  attributes the terminal resolves (no OSC 11 query; see `theme.SOBRIA`)
- ✅ Token wire-up in render pipeline (`engine.Cell.Style`)
- ✅ Styled golden fixtures: `RAW.styled`, `SOBRIA.styled`, `MAXIMUM.styled`
- ✅ Theme validation (scenes reference existing tokens) — repaired during
  Phase 2: `ValidateTokens` read `style["token"]`, while the shipped scenes,
  `SCENES.md`, `TOKENS.md` and the render path all write `style["style"]`, so
  the check was blind to the only spelling that occurs. With the key fixed,
  the defect it hid surfaced addressed: `SOBRIA.json:2:3` and `:25:5`
  reference the token `header`, which `TOKENS.md` signs and the theme had
  dropped. Every token test had used the validator's key rather than the
  scenes', so code and tests shared one wrong assumption and agreed.
- ✅ The shipped scenes are held to the rule the validator applies to
  downloaded ones (`TestTheShippedScenesReferenceOnlyDefinedTokens`)
- ✅ Every style reference the validator accepts now also reaches the screen.
  Fixing the key above left the renderer behind: `ValidateTokens` accepted
  both `style["token"]` and `style["style"]`, while `styleName()` still read
  `style["style"]` alone, so a scene using the other accepted spelling
  validated clean and drew **unstyled** — the one outcome that reports success
  and shows the wrong screen, since clearing validation is exactly the signal
  that says the document is fine. Found on the corpus' own gold answer: the
  converged document of `sobria-dim-the-footer`, whose order is *"grey it
  out"*, styled `model.name` as `{"token": "dim"}` and rendered it with no
  style. The render path now reads `scene.StyleTokenKeys()`, so the two
  cannot disagree again, and the guard is a property over that list rather
  than two hardcoded spellings — a test that enumerated the keys itself would
  reproduce the drift it exists to catch.
- ✅ A border's declared style token reaches the frame. `SCENES.md` Scene 3
  signs the object form (`{"shape": "single", "style": "warn"}`) so a frame
  can carry a token, `BorderStyleName` exists to read it and `ValidateTokens`
  refuses an undefined one — but both drawing paths stamped the literal
  `"border"` onto all eight frame spans and never asked. The same class as
  above, and more deceptive: naming a bad token *does* get a refusal, so the
  field looks wired; a correct value simply did nothing. The bare string form
  keeps `"border"`, held by its own test because MAXIMUM's styled golden pins
  six spans under that name and the unconditional fix moves it (verified: the
  naive version fails both that guard and `TestMaximumSceneStyledGolden`).
- ✅ A construction the validator accepts is one the engine draws — and where
  that is not true yet, the refusal is explicit. `row_template` was the third
  instance of the class above and the first to reach the *measuring
  instrument* rather than a scene: the validator walked it for binds and for
  tokens, `loc.go` addressed it, the binds audit collected through it and
  `eval`'s `CollectBinds` walked it by name citing Q10 — five places saying
  the field was live — while `internal/engine` read it in zero. Because the
  grader counts a bind found inside a template, an answer satisfying
  `must_bind` only there scored **converged** while the list drew `[…]`.
  Measured, not reasoned: reachable from a case the corpus already ships
  (`raw-add-tasks-panel`, *"put a tasks panel on the right"*), which returned
  `converged=true, missing=[]` on a panel with no tasks in it — the corpus
  lying in the model's favour, the one direction nobody audits.
  The fix is a refusal rather than an implementation: the field's semantics
  are relative binds (`row.kind`) and `row.*` is signed nowhere in BINDS.md —
  it is Scene 5, i.e. Phase 3 — so drawing it now would invent format ahead of
  the phase meant to design it. The message says *"not yet rendered"* rather
  than *"invalid"*, because the author spelled the field correctly and a wrong
  diagnosis costs the repair loop a turn it charges to the model.
- ✅ The **fourth** instance of the class fails the suite by itself
  (`internal/scene/unrendered_audit_test.go`). Three were found by hand, one
  per session; the audit enumerates `Node`'s fields from the source and holds
  each to one of three states — rendered, refused via `unrenderedFields`, or
  justified in writing in `acceptedUnreadFields`. Types are resolved with
  `go/types` rather than matched as text, because the text draft reported
  `Node.ID` as read (`eval.Case` also has an `ID`) and a guard that cries wolf
  is a guard that gets deleted. The scene package is excluded from the read
  set on purpose: *being validated* is the shared signature of all three
  instances, so counting this package's own reads would make the audit agree
  with the bug. Two fields stay unread and say why: `filter_by` is decorative
  (the host filters regardless — verified byte-identical frames with the
  field, with a nonsense value, and with no field at all), and `categories` is
  a real silent drop that cannot be refused because SOBRIA ships eleven and
  invariant 1 outranks this audit. Both entries fail the day the engine reads
  them, which is the signal to delete them.

- ✅ A container does not restyle its children's content
  (`internal/engine/container_preserves_child_style_test.go`). The bind guards
  ask whether a value reaches the frame; this asks about the other half of what
  a node declares — the token it is drawn under. A value arriving under the
  wrong token is on screen and wrong, and every projection guard passes it.
  Swept as a matrix (every node type × bordered/borderless × the container
  declaring a token or not), one shape of sixteen discarded the child's token:
  a **bordered box**, whose content loop flattened each row with `l.Text()` and
  re-emitted it under the box's style. The rule was already written down in
  `padLine` — *"chrome must not restyle the content it fills around"* — and
  already honoured by `wrapWithBorder`, the bordered *overlay* path. Three of
  four drawing paths obeyed it and nothing compared them, which is why the
  guard enumerates rather than testing the box. Reachable from a shipped scene:
  MAXIMUM's Tasks panel is a bordered box around a list whose empty state is
  minted `dim`, and `MAXIMUM.styled` had been pinning it bare as correct output
  since the day it was generated. The fix moves two golden lines and they were
  measured apart: `no tasks` gains `«dim:…»` (the repair), and the banner's one
  span becomes two adjacent spans of the *same* token (a boundary, not a
  change) — with SGR codes stripped the emitted text is byte-identical, so no
  cell moved and invariant 1 holds. Verified by injection: welding the sibling
  `wrapWithBorder` path leaves every golden green and fails only this guard.

- ✅ A node draws its own content under its own token
  (`internal/engine/node_honours_own_style_test.go`). The guard above holds
  the *child* fixed at `text` and sweeps the containers, which can only see a
  parent discarding a declaration — never a leaf that never applied one. That
  is the **fifth** instance of the accepted-but-not-drawn class: `style` is a
  universal property in SCENES.md's vocabulary and `ValidateTokens` is
  type-agnostic, yet `input`, `list`, `markdown` and `rule` emitted the token
  they minted and ignored the one the scene declared. Reachable and silent:
  SOBRIA has one node of each, and styling all four is accepted by **both**
  validators while leaving the frame **byte-identical** — so nothing refuses,
  the repair loop gets no `file:line`, and `converged` scores the unchanged
  screen as a win. The fix makes a declaration replace the minted default and
  keeps the default when the scene declares nothing; that fallback is
  load-bearing, since all three goldens declare nothing here and invariant 1
  says the factory frames do not move (none did). Two tokens stay fixed on
  purpose: the list's `[…]` placeholder is the engine reporting it has no
  projection, not content a scene may dress up, and the **selected** slash row
  stays bright because BINDS.md §4.3 signs it as the only indication of what
  `Enter` will submit. That second boundary was argued and unmeasured —
  erasing the highlight passed the entire suite — so it now has its own guard,
  failing on that injection alone. Verified one weld at a time (`rule`,
  `markdown`, `input`, `list`): each caught only by these guards, with
  `render.go` byte-identical after every restore.

**Phase 1.5 — The SCENES ↔ BINDS audit:** Complete.

- ✅ `internal/scene/binds_audit_test.go` parses `docs/BINDS.md` and holds the
  validator's runtime inventory to the signed document, in both directions
- ✅ All three pinned scenes audited (MAXIMUM was previously unchecked)
- ✅ An unexercised signed bind is a logged warning, per `AGENTS.md`
- ✅ Inventory drift repaired: 4 unsigned binds removed, 18 signed-but-rejected
  binds restored, `agent.todos` signed in §4.1

**Phase 1.6 — The address on every refusal:** Complete.

- ✅ `internal/scene/loc.go` maps a byte offset to `file:line:col`, with the
  column counted in runes (the shipped scenes are full of `Δ`, `┃` and
  box-drawing glyphs, so a byte column points mid-character)
- ✅ Every refusal in `internal/scene` is a `*scene.Error` carrying its
  address — unsigned binds and `when` conditions point at the offending node
  (BINDS.md §4.5), and JSON syntax/type errors keep the offset the standard
  library had already computed
- ✅ Undefined token references carry `file:line` too (LESSONS.md's rule, with
  arxi-sim's polarity inverted: the reference is checked, not the inventory)
- ✅ Invariant 3's boot notice reaches the screen: `loadScene` returns the
  addressed reason and the host publishes it on `host.scene.error`, in both
  the tty and the piped render path. It used to fall back silently.
- ✅ A structural guard (`TestEveryRefusalInThisPackageIsAddressed`) covers
  both parse entry points, so a later rule returning a bare `fmt.Errorf`
  fails the suite instead of quietly reopening the gap

**Phase 2 (in progress) — Mutation from inside.** The gate lands before the
feature, as `PLAN.md` requires:

- ✅ `docs/EVAL.md` — the corpus contract: case shape, scoring, and why the
  corpus is data rather than code
- ✅ `internal/eval` + `testdata/eval/` — the corpus loader and its first four
  cases, covering all three pinned scenes. Every expected refusal is replayed
  against the real validator on each `go test`, so a case cannot claim a
  refusal the engine does not produce — which already caught an invented one
  and three misaddressed ones (see EVAL.md).
- ✅ A repair path longer than one turn (`maximum-count-the-tasks`): an unsigned
  bind, then an invented token, refused through two different types on
  consecutive turns. Until it landed, every case held a single refusal, so the
  corpus measured the first shot while documenting that it measured the loop.
- ✅ The false-pass guard (`TestDoingNothingDoesNotPass`): a scripted model that
  ignored the order and echoed the base scene back scored **2/4 converged**,
  because both SOBRIA cases demanded only fields SOBRIA already binds. Every
  other test stayed green — they ask whether a refusal is real, and every
  refusal was. Each case must now demand a bind its base scene lacks; the
  do-nothing model scores 0/4.
- ✅ `scene.SignedBinds()` — the validator's own inventory, exported so the
  runner can tell the model which binds exist. The alternative was a fourth
  hand-written copy of a list that has already drifted once, and the drift
  would have been recorded as the model's failure rather than the prompt's.
- ✅ One judge for both callers (`internal/eval/grade.go`): the corpus test and
  the runner grade through the same code, so the recorded attempts are a
  prediction of the runner's behaviour rather than a parallel story about it
- ✅ The model runner (`internal/eval/run.go`) — the repair loop, converged /
  turns-to-convergence, and five outcomes rather than two: `converged`,
  `exhausted`, `looped`, `incomplete`, `model_error`. Proven by scripted
  models, so the loop is testable without a network; ten injected regressions,
  all caught.
- ✅ `cmd/arxi-eval` — runs the corpus against an OpenAI-compatible endpoint.
  A separate binary: the shipped interface carries no eval harness and no
  reason to read `OPENAI_API_KEY`.
- ✅ **The corpus has been run against a real model.** First live run on
  2026-09-22 against `deepseek-v4.1-flash` on an OpenAI-compatible endpoint
  (not the plan-blocked gateway below). Three runs, because a single run of a
  non-deterministic model is weak evidence: **11 of 12 case-runs converged**.
  - Run 1: 3/4 converged (turns 1,1,2); `raw-add-tasks-panel` scored
    `incomplete` (`unbound: agent.todos`).
  - Run 2: 4/4 converged (turns 1,1,2,1).
  - Run 3: 4/4 converged (all 1 turn).

  Two facts matter more than the ratio. **The repair loop demonstrably works
  against a real model**: in two of the three runs a turn-1 JSON syntax error
  (`invalid character ']' after object key:value pair`) was handed back as an
  addressed refusal and the model self-corrected on turn 2 — the exact
  ask→grade→hand-back mechanism Phase 2 exists to prove. And **the one
  non-convergence is the failure the corpus was built to detect, not a
  repair-loop fault**: `raw-add-tasks-panel` predicts the model will guess
  `tasks.list` for what BINDS.md signs as `agent.todos`, and because PLAN.md
  decided an unknown-but-parseable bind is a *warning*, not a load-time
  refusal, there is no validator message to repair from — so it scores
  `incomplete` rather than `exhausted`. It converged in the other two runs, so
  the miss is a probabilistic bind-naming slip against a deliberate product
  tradeoff, not evidence the model cannot read addresses.

  Phase 2's question — can a model patch scenes reliably — is answered *yes*
  for this model: it reads the format, converges mostly on the first turn, and
  uses addressed refusals to recover. The ship/no-ship call for
  agent-command-driven `/ui` self-extension is recorded in `docs/PLAN.md`.

  The plan-block detection below stays because it guards a real hazard: the
  gateway once available here refused on plan grounds with **HTTP 200 carrying
  a normal-looking assistant message** — `x_genspark.code = free_plan_block` —
  so without the detection a plan limit would record as a capability
  measurement. It was re-confirmed live each session it was checked (latest,
  against `gpt-5-mini`: `0/4 converged, model_error=4`, exit 1, block confirmed
  by a direct probe rather than inferred from the harness).
- ✅ The `/ui` control surface (`internal/patch`), PLAN.md's deterministic
  half. A patch is a **source-to-source transformation** — bytes in, bytes
  out, re-parsed through `scene.ParseNamed` — rather than a mutation of
  `*scene.Node`, and the choice was measured rather than argued. A
  `Document`'s address book is built by the parser and by nothing else, so a
  Document assembled in memory has no offsets; the same refusal, on the same
  document, both ways:

  ```
  parsed from bytes : probe.json:2:4: node type "text" declares "row_template" …
  rebuilt by hand   : <scene>: node type "text" declares "row_template" …
  ```

  The address is gone. That is the worst place in the project to lose it,
  because Phase 2's thesis is the repair loop and the validator error is the
  only input the retry gets: a tree-mutating `/ui` works for every command
  that succeeds and degrades the diagnostics of exactly the commands that
  fail. Round-trip fidelity was measured before it was built on (both shipped
  scenes re-marshal and re-parse tree-stable and warning-stable). The edit
  walks a generic map rather than the typed tree, because `scene.Node` drops
  undeclared keys and editing through it would silently delete every unknown
  property on the way past — including every field a later phase adds and
  every field a plugin fragment carries.
- ✅ Six verbs — `add`, `move`, `set`, `style`, `hide`, `show` — and the two
  that took longest to land are the ones whose *argument* had to be designed
  first. `add`/`move` answer *where*, which needed the write-path addressing
  vocabulary (`docs/ADDRESSING.md`, D2): `above`/`below <id>`, `into <id>
  [top]`, and the `below_input`/`above_input` semantic anchors. `move` adds the
  one refusal `add` gets to skip — the **cycle** refusal, a node cannot become
  its own descendant — checked over the whole moved subtree before it is
  detached. `hide`/`show` are different in kind from the other four: they do
  **not** edit the document, they write the `ui.hidden` view-state set (BINDS.md
  §4.3, D3) that the engine walk reads as a visibility filter, so a node draws
  iff its `when` is truthy **and** its id is not in the set. `ui.hidden` is a
  *set of node ids*, not a scalar, for a reason paid for once: every other
  `ui.*` row is a single id, so a scalar flag would make `/ui hide a` silently
  unhide `b`. `/ui show *` clears the set. All six are source-to-source (or, for
  hide/show, set mutations) that re-validate before anything reaches the screen,
  and every refusal carries `file:line`.
- ✅ Measured while wiring it: **about half of every shipped scene is
  unaddressable** — SOBRIA declares an id on 7 of 15 nodes, MAXIMUM 7 of 13,
  RAW 3 of 4. So "no node with that id" is the expected answer to much of what
  a user will try, and the reason is invisible from the screen (the status
  row's model name is a node they can see and point at, and `model.name` is
  its *bind*). The refusal lists the ids that exist and counts the anonymous
  ones; synthesising addresses was rejected as a second way to name a node,
  designed here rather than in SCENES.md.
- ✅ The dispatch runs **ahead of the slash menu**, which is a fix rather than
  an ordering preference. The menu filters on the whole typed string, so it
  matches nothing once an argument is present (`ui` → 1, `ui style` → 0,
  `ui style status dim` → 0) and its Enter branch returns the buffer
  untouched: a complete, correct command did nothing at all — no patch, no
  refusal, no prompt — with the menu showing an empty list. The guard for it
  had to be rewritten after an injection: the first version called the handler
  directly, and welding the dispatch back behind the menu left it **green**,
  because the defect is in the order of the branches and that order does not
  exist inside the function under test. It now types the command into `loop()`
  through the scripted TTY.
- ✅ The slash menu once advertised `add, move, style, plugin` while the
  surface implemented fewer verbs — the accepted-but-not-drawn class one layer
  out, and worse there than in a document, since the menu is read at the moment
  of use and so invites the user into a refusal. It now names exactly the
  implemented set (`add, move, set, style, hide, show`), held in both directions
  by a test in `patch_test` — every `patch.Verbs()` entry appears in the menu
  and every word the menu lists is a real verb — which is where it can import
  both packages without `fold` importing the mutation layer (ADR-0002).
- ⬜ Agent-driven patches, with the full change-diff view. The summary line is
  in place (`/ui: styled "status" as "dim"`); the side-by-side view belongs
  with the agent half, where a proposal arrives *before* it is applied.

## Repository layout

This is a monorepo with two Go modules:

| Folder | What it is | Module |
|---|---|---|
| `/` (root) | `arxi-tui`, the terminal front end | `github.com/michiTrader/arxi_tui` |
| `core/` | `arxi`, the backend/kernel the TUI drives (full history imported from `github.com/michiTrader/arxi`) | `github.com/michiTrader/arxi` |

They stay separate modules on purpose: the core is Go 1.22 with no third-party
dependencies and its tests enforce that; the TUI uses Charm libraries. Build both:

```bash
go build -o arxi-tui ./cmd/arxi-tui          # from the repo root
cd core && go build -o ../arxi ./cmd/arxi    # the core
ARXI_BIN=./arxi ./arxi-tui
```

Installing a release (Linux, macOS, Termux): `curl -fsSL https://raw.githubusercontent.com/michiTrader/arxi-tui/master/install.sh | sh`
puts `arxi-tui` and the `arxi` core in the same folder. The TUI looks for the core next to
itself, so no `ARXI_BIN` is needed; setting `ARXI_BIN` still overrides it. On Windows,
download both `.exe` files from the release page into one folder.

The core is found without `ARXI_BIN`: next to the program, in `core/` of the checkout it
sits in, or built on the spot from `core/` when a Go toolchain is on `PATH` (and rebuilt
when its source is newer, so a stale core never silently lacks a feature). The working
folder is never searched or built from. Providers whose base URL answers a web page
(a website instead of the API, or a bot-protection page) are reported with the cause and
the fix, e.g. add `/v1` to the base URL.

Direction: one program. The core now lives in this repository so the frontend and the
backend change together; the goal is a single binary installed with one command. Until
that lands, build both as above.

CI note: the root `go test ./...` does not descend into `core/` (nested module). Test the
core with `cd core && go test ./...`; the matching CI job is in `docs/ci-core-job.yml`,
ready to paste into `.github/workflows/ci.yml`.

## Build

Requires Go 1.25.

```bash
go build -o arxi-tui ./cmd/arxi-tui
go build -o arxi-eval ./cmd/arxi-eval    # the Phase 2 eval harness
go vet ./... && gofmt -l .
go test -count=1 ./...
UPDATE_GOLDEN=1 go test ./internal/...   # regenerate golden fixtures
```

## Run

```bash
# With mock driver (Phase 0 dev, no arxi binary needed)
./arxi-tui

# With arxi serve subprocess (Phase 0.5, requires arxi binary)
ARXI_BIN=/path/to/arxi ./arxi-tui
```

### Providers and keys (`/provider`)

Everything about providers lives in one screen. Type `/provider` (also `/providers`,
`/login`, or pick it in the `/` menu). The choices appear at the bottom,
like the command menu. Type to filter any list; type `other` to jump to "Other…".

| Key | Does |
|---|---|
| Up / Down, PgUp / PgDn | move the `→` marker |
| type | filter the list (or fill the focused form field) |
| Enter | choose / next field / save |
| Tab / Shift-Tab | next / previous form field |
| Esc | back one step (then close) |
| Ctrl-C | the escape hatch, as everywhere |

From there you can:

- **Add a provider**: pick one of 17 services (OpenAI, Anthropic, OpenRouter, Gemini,
  Groq, Ollama, ...) or "Other…" for any OpenAI-compatible service. Give the key once
  (and, for "Other…", the name and base URL). The model list is fetched for you; if the
  service has no list endpoint, type the model ids by hand instead. The form also has an
  optional **Model ids** field: list several (comma or space separated) and only those are
  added, with no list fetched. Leave it empty to fetch them all.
- **Edit a provider**: change its base URL, key or key variable name.
- **Manage models**: fetch the list again, add several models by hand (comma or space
  separated, with optional prices), enable, disable or remove them.
- **Choose the model to chat with**: use `/model` (next section). The status bar shows
  the chosen one, and every chat message goes to it.

### Choosing the model (`/model `)

Type `/model` followed by a space and a small menu opens right above the input, one
row per enabled model: `  name  provider`, with a `✓` on the model the chat uses.
Keep typing to filter live (every word must match: `/model deeps flash`). Up / Down
move (wrapping), Enter picks, Esc closes. Nothing else is printed; the status bar
shows the new model. With no provider yet, the notice points you to `/provider`.

### Copying answers (`/copy`)

Selecting text with the mouse also takes the frame's border, so `/copy` puts what the model
said on the clipboard without it. Type `/copy` (or pick it in the `/` menu) and a menu opens
with one row per answer: `last` (the newest), `all` (the whole conversation, each message
labelled `You:` / `Assistant:`) and then `2`, `3`, ... counting back from the newest, each
with the first line as a preview. Enter copies the highlighted row. **Tab marks** several
rows (a `✓` shows); Enter then copies every marked answer, oldest first, so the pasted text
reads in the order it was said. `/copy 3` goes straight to answer 3.

It writes to the terminal clipboard with OSC 52 (it works over ssh and inside tmux) and also
runs a local clipboard program when one is installed: `termux-clipboard-set`, `wl-copy`,
`xclip`, `xsel`, `pbcopy`, or `clip` on Windows. A terminal that ignores OSC 52 drops it
silently, so the notice says what was *sent*, not that it arrived; if nothing pastes, enable
OSC 52 in the terminal (or install one of the programs above). Over 100 kB only a clipboard
program is used.

### The conversation

Your lines carry a `┃ ` marker; every answer is drawn two columns in, so the two voices
read as a pair of columns. Under an answer a dim line says what it cost, for example
`2s (↑2 ↓57)`: how long it took, tokens sent (↑) and received (↓).

```
┃ hoola

  Hola. ¿En qué te ayudo?

  2s (↑2 ↓57)

┃ Gracias
```

A warning reads `! auth: API key setup is unavailable in this WASM session.` and a
failed request `✗ …`.

**Esc or Ctrl-C stops the answer being waited for** and leaves
`✗ request failed: Cancelled` under your question; you can send the next line at
once. Each message runs on its own `arxi serve` process, so cancelling really stops the
request instead of leaving its late answer to be mistaken for the next one. When
nothing is running, Ctrl-C keeps its usual meaning (clear the line; twice to leave) and
Esc closes a menu.

### Look

The Δr×i mark runs from deep orange to yellow. The caret is amber (set with OSC 12 and
handed back to the terminal on exit; a terminal without OSC 12 keeps its own colour). The
caret blinks only while you are idle: every key keeps it solid for about a second, so it
never disappears while you type or move with the arrows. With the `/` menu open its hint
line sits right under the bottom rule, and the rules are drawn in a dim grey.

### Undo in the input bar (Ctrl+Z / Ctrl+Y)

Ctrl+Z takes back the last thing you did to the line you are typing, and Ctrl+Y puts it
back. A step is a word typed, a run of Backspaces, a paste, or the line a Ctrl+C wiped;
sending a line forgets the steps, so Ctrl+Z never brings a sent message back. It does not
close the program: leaving is Ctrl+C twice, and nothing can take that key. On Windows the
console used to report Ctrl+Z as end of input, which ended the session; arxi-tui now reads
the console itself so the key arrives like any other. If you bind Ctrl+Z to something of
your own (`/ui key ctrl+z <action>`), your shortcut wins.

### Where arxi-tui keeps its settings

Everything is in **`~/.arxi`**, the same on every system and from every project (the
`ARXI_*` overrides still win: `ARXI_CONFIG_DIR`, `ARXI_SECRETS_DIR`, `ARXI_PROVIDERS_DIR`,
`ARXI_HISTORY_DIR`):

| In `~/.arxi` | What |
|---|---|
| `scene.json`, `theme.json`, `texts.json`, `behaviour.json` | the interface you shaped (layout, colours, words, keys/commands/animations); `/ui undo` and `/ui reset` work on the four together |
| `history`, `sessions/`, `search.json`, `style` | input history, saved conversations, the search service, the message style |
| `providers/`, `secrets/` | the providers and the API keys (written by the core) |
| `trusted-projects.json` | which project folders you let load their `.arxi/` (below) |

**Coming from an older version.** Settings used to live in `%AppData%\arxi` (Windows),
`~/.config/arxi` (Linux) or `~/Library/Application Support/arxi` (macOS). The first time
`~/.arxi` is used, what the old folder holds is **copied** in, never moved: the old folder is
not touched or deleted (delete it by hand when you are happy), nothing already in `~/.arxi`
is overwritten, and a setting you delete later does not come back from the old folder (a
`.legacy-imported` marker records that the copy happened). Setting an `ARXI_*` folder
yourself means nothing is imported into it.

### Per-project settings (`<project>/.arxi/`, `/project`)

A repository can carry its own look and shortcuts. Put any of `theme.json`, `texts.json` and
`behaviour.json` (same formats as in `~/.arxi`) in the project's `.arxi/` folder and they are
laid over yours: colours and words entry by entry, behaviour maps entry by entry,
commands by name, hooks added after yours.

- **Nothing loads until you say so.** A cloned folder is somebody else's text, and
  `behaviour.json` can bind a key or a hook to a command. Run **`/project trust`** to load
  this folder's settings (now and on later starts), `/project` (or `/project status`) to see
  where it stands and **`/project forget`** to stop.
- **Consent is tied to the content.** `~/.arxi/trusted-projects.json` stores the folder and a
  hash of the three files; if any of them changes afterwards it is not loaded and `/project`
  says so, until you trust it again.
- **Keys never go in a project.** API keys, providers and the search key are read only from
  `~/.arxi`; any other file in `.arxi/` is ignored.
- **It is never written back.** `/ui` changes are saved to `~/.arxi` only, so a project
  colour cannot leak into your own settings. `-raw` and `-scene` ignore the project layer,
  like your own saved interface.

### Input history

Up and Down (or Ctrl+P and Ctrl+N) walk the lines you have sent, newest first. The line you
were typing is kept and comes back when you step past the newest one. In a multi-line input
the arrows move between its rows first and only walk the history from the top or bottom
row; with the `/` menu open they steer the menu. The last 500 lines are kept between
sessions in `~/.arxi/history` (`ARXI_HISTORY_DIR` moves it), readable only by you.

### Project rules (`ARXI.md` / `AGENTS.md`)

If the folder you start in has an `ARXI.md` (preferred) or an `AGENTS.md`, its text is added
to the model's instructions on every turn, and the chat says once `using AGENTS.md as
project rules`. Only that folder is read (no parent folders, no links to files elsewhere).
Every turn pays for the rules, so only the first 6,000 bytes are sent and the notice says
when a file was cut. The model is told the rules come from the project, not from you, and
they never switch off the questions the agent mode asks. `ARXI_PROJECT_RULES=off` disables
the feature. The file is re-read every turn, so an edit counts from your next message.

### Thinking level (`/effort `)

Type `/effort` followed by a space and a menu lists the levels the current model takes
(same keys as the model menu; a check marks the one in use). OpenAI reasoning models
(gpt-5, o-series) take `minimal`, `low`, `medium` and `high`; other OpenAI-compatible
models (DeepSeek, gateways) take `low`, `medium` and `high`; Claude models take none, so
the menu says so. Choosing the level already in use clears it. Until you choose, nothing
is sent and the bar shows nothing. A chosen level is sent with every chat message as
`reasoning_effort`; switching to a model that does not take it clears it. `/clear` keeps it.

The key is pasted into a masked field, shown as `••••`, and never printed back, logged
or sent to the chat. If sending a message fails (no provider, no model, a refused key,
no network) the reason appears in the banner instead of nothing happening.

Where the providers go: `~/.arxi/providers`, the same from every working directory
(override with `ARXI_PROVIDERS_DIR`). The previous location (`~/.config/arxi/providers`,
`%AppData%\arxi\providers`) and a `./providers` folder left by an older version are copied
there once, never moved or overwritten, and the originals stay where they were.

Where the key goes: the core writes it to `<name>.key` in `~/.arxi/secrets`
(mode 0600, directory 0700; override with `ARXI_SECRETS_DIR`). Keys from the old secrets
folder are copied once and the old files are kept. **It is not encrypted**,
only protected by file permissions. An environment variable named by the provider
(for example `OPENROUTER_API_KEY`) wins over a stored key. The commands take no
arguments on purpose, so a key is never typed on a command line.

This needs a core that implements the provider, model and `chat.send` verbs. If yours
is older the screen says so with the exact rebuild command:
`cd core && go build -o ../arxi ./cmd/arxi`
(Windows PowerShell: `cd core; go build -o ..\arxi.exe .\cmd\arxi`), then point
`ARXI_BIN` at it. Without a core (`ARXI_BIN` unset) the screen still opens and says so.

**Windows PowerShell:**

```powershell
$env:ARXI_BIN = "C:\path\to\arxi.exe"
.\arxi-tui.exe
```

`ARXI_BIN` must be the **arxi core** (built from the separate `arxi` project), never
`arxi-tui.exe` itself. Pointing it at the TUI used to fail with the cryptic
`hello is not JSON: invalid character`; it is now refused up front with a sentence that
names the wrong path. Leave `ARXI_BIN` unset to run the offline demo (the providers
screen opens but cannot reach a core).

### The files in `testdata/` ("scenarios")

`testdata/*.json` are scene documents: the JSON that describes one whole screen. They
are the fixtures the tests render and compare against `*.frame` / `*.styled` goldens.
Open one with `./arxi-tui -scene testdata/HUB.json`. That shows the screen as a
**static picture with fake data**: nothing is connected behind it, so `SUBAGENTS.json`
shows a frozen chat and only reacts when a live run emits sub-agent events. Seeing
"nothing happens" there is expected. The real, working screens are the ones you reach
from the normal chat (`/provider`, `/ui plugin browse`, ...).

### What the NDJSON bridge actually does today

Measured on 2026-09-21 against a real `arxi serve` (arxi 0.0.1-spec, surface
v1) built from `michiTrader/arxi@main`, not against the mock. The transcripts
are committed as `testdata/serve/session.ndjson` (a live protocol session) and
`testdata/serve/real_run.ndjson` (121 events the core wrote for a simulated
run). Both are regenerable; the commands are in the test headers.

The bridge had never been run against the core before this, and it did not
work:

- **The handshake could never succeed.** It compared the hello's `version`
  against a `"0.1.0"` invented in this repo; the core sends its *binary*
  version, `"0.0.1-spec"`. Fixed to gate on `surface_version`, which is the
  vocabulary number and the thing the bind inventory is written against.
- **`run.prompt` is declared and not implemented.** It is the only request
  arxi-tui sends, and this build of the core answers it `not_implemented`. The
  host now reads the hello's `implemented` list, so a permanent gap is
  distinguishable from a transient failure instead of being rediscovered one
  refused request at a time.
- **`run.attach` *is* implemented**, which contradicted ADR-0002's premise that
  no subscription layer exists in arxi to extend. **Re-decided** (docs/PLAN.md,
  "ADR-0002 re-decided"): log-follow stays, but for the opposite reason to the
  one recorded. Not because subscribing is unavailable — it is implemented,
  `event.subscribe` is in the session capabilities, and `serve_stream.go`
  already has subscription IDs, pumps, cancellation and writer arbitration —
  but because log-follow is the path `Replay`, the goldens and the eval corpus
  all run through, and a second ingestion path with a different confirmation
  model is two chances at a wrong frame. The adoption trigger is now specific:
  a positive end-of-run signal (`run.result` is *not* the last event — seq 112
  of 122), or reading a log whose directory the host does not own.
- **Log-follow said "confirmed" and meant "newline-terminated".** The commit
  point is the *removal* of `pending.commit` (logstore step 3), so between the
  batch append and the commit the log holds complete, newline-terminated
  records that the core's own `Open()` truncates away. Measured: two committed
  events plus a two-event in-flight batch delivered **four**. The follower now
  stops at the smaller of the last newline and the marker's rollback point,
  and tracks its own delivered offset because the confirmed boundary can move
  *backwards*.
- **A refusal read as success.** `ok:false` came back with a nil error, and the
  host's only call site was `_ = drv.SubmitPrompt(...)`. Refusals are now a
  `driver.Refusal` carrying the core's code, sentence, `fix` and `operation`,
  with `Permanent()` for the retry branch.
- **The event decoder was right about the file and wrong about the socket.**
  `kernel.Event` on disk spells the sequence `seq`; `host/v1.Event` on the wire
  spells it `sequence`. Reading only `seq` left every subscription event at
  Seq 0 — silently. Unified into one `decodeEvent` that accepts either and
  refuses a record with neither.
- **Every simulated run was labelled live.** `agent.activated` overwrote
  `AgentMode`, conflating "who is working" with "whose money is at stake". The
  mock could not catch it; its `run.started` carries `simulated:false`.

Fold coverage against a real run is **122 of 122 events** (100%), re-measured
from a starting point of 13/122. The `exec.*` family (91 events) and
`run.result` came first; then the `tool.*` family (8 events), which is not the
largest remaining count — `stage.*` is 7 — but is the only family in the log
that says *what the agent did*; then `stage.*` (blueprint position, 120/122);
and finally the `timer.*` pair (2 events) that closed it. The last two are a
stage deadline that armed and cancelled without firing, handled as a
read-and-understood no-op: a scheduled deadline has no host-facing projection,
because the user-visible half of *a timer exists* is an agent blocked on one
(`blocked_on=timer`), which folds into a todo already.

The figure is pinned by a test that also asserts the accounting closes against
the log's length, so every event is either handled or named in the blind list
(now empty, with a fail-loud check for any future unaccounted type). Every
re-measurement was *forced* by that pin rather than reported alongside it.

The default scene is the sobria look, built into the binary (a copy of `testdata/SOBRIA.json`, kept identical by a test), so it boots from any folder. `-scene <file>` boots a file instead and `-raw` the factory raw scene. If a scene fails to
load, the interface falls back to the factory RAW scene (two nodes: transcript
and input, nothing else) and states why, addressed, on screen.

## Evaluate (Phase 2)

```bash
OPENAI_API_KEY=... ./arxi-eval -model <name> -v
```

Runs the corpus in `testdata/eval/` and reports convergence and turns per case.
The exit status says whether the harness ran, not whether the model scored
well — see `docs/EVAL.md` for why no threshold is set.

## Automatic retries and gradient borders

- **Retries.** When a provider answers 429 or a gateway error (502/503/504/529/408), arxi waits and asks again by itself, up to 6 tries. The wait is what the provider asks for (`Retry-After` header, or "Retry in 27s" in its message) and otherwise doubles each time (2s, 4s, 8s... capped at 30s). The Thinking line counts the wait down ("Retrying in 27s · rate limited (2/6)") and Esc cancels it. A billing wall ("available only with a subscription", `insufficient_quota`) or a 401/403/400 is never retried.
- **Gradient borders.** An animation may set `"direction"`: `along` (default), `horizontal`, `vertical`, `diagonal`, `antidiagonal` or `radial`. Typed form: `/ui animate fire #ff3b30,#ffcc00,#0a84ff diagonal fps=12`. Put the animation on the border's `style` only; the input text keeps its own colour.

## `/ui` as a conversation, Termux, and richer gradients

**`/ui` is typed once.** `/ui <request>` (or `/ui` / `/ui on`) switches the interface conversation on: the guide and the tools stay loaded for every following message, so you can talk about the interface fluently. `/ui off` (or `/ui exit`, or `/clear`) leaves it. The status bar shows `· /ui` while it is on. The command word is kept in the chat, drawn in its own colour and in italics (token `chat.command`).

**Termux.** On Termux the mouse is not claimed (a tracked tap never brings Android's keyboard back) and alternate scroll is switched off; plain up/down scroll the chat (one row per swipe), the input history moves to ctrl+p / ctrl+n. While rows are painted the caret is hidden and only rows that changed are sent, which removes the glitching caret and flicker during animations. `-mouse` / `-mouse=false` override the choice on any terminal.

**Gradients.** An animation's `direction` may be `horizontal`, `vertical`, `diagonal`, `antidiagonal`, `radial`, `conic` or any angle (`"60deg"`, CSS meaning); add `"reverse": true` or `"static": true`. A border may name a token per side, `{"shape":"round","style":"a","top":"b","bottom":"c"}`, so top and bottom can differ. Typed: `/ui animate <name> <colours> [fps=N] [spread=N] [diagonal|conic|<N>deg] [reverse] [static]`.
