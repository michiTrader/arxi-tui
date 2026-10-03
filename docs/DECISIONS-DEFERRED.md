# Decisions deferred

Decisions the project has weighed, chosen the cheap side of, and deliberately
left open for later. Each entry says what was chosen now, what was postponed,
and what would make it worth revisiting, so a later reader does not mistake the
interim choice for the final design.

## D1 — `{row.<field>}` in drawn text

**Found by:** the live eval run of `subagents-turns-and-busy-dot` (Block A4). The
model wrote `"text": "{row.turns}•"`. Only `on_press` substitutes
`{row.<field>}`; a text node paints its `text` verbatim, so the engine accepted
the document and drew the braces literally. Validation was clean, so the repair
loop had nothing to repair.

**Chosen now — option (a):** refuse `{row.` in a node's `text`, `title` or
`placeholder`, with the node's file:line and the working spelling
(`"bind": "row.<field>"` in its own text node). Implemented in
`validateNoRowInterpolationInText` (`internal/scene/validate.go`), held by
`internal/scene/row_text_interpolation_test.go`. It closes the silent failure at
the lowest cost and gives a model something concrete to repair.

**Postponed — option (b):** sign interpolation in drawn text, so
`"text": "{row.turns}•"` works as written. It is the more comfortable authoring
form, which is why it is worth doing eventually, but it is a format change, not a
fix. Doing it means:

- a new signed rule in `docs/SCENES.md` and `docs/BINDS.md` §4.7/§4.8, stating
  that `text` interpolates inside a `row_template` and nowhere else;
- an escape for a literal `{row.` (today there is none, and none is needed
  because the spelling is refused);
- the same row-schema check `validateOnPress` already runs, applied to text, so
  `{row.nope}` is still refused with an address;
- a renderer change (`internal/engine`) that expands the tokens per row, with
  goldens for it in their own mutation family;
- re-running the eval corpus, because the prompt currently teaches the `bind`
  spelling.

**Revisit when:** authors or models keep reaching for the interpolated spelling
after the refusal exists (the eval cases that fail with this refusal in their
repair log are the measurement), or when a decorated row (a value plus a glyph in
one string) is needed often enough that two nodes per cell is the cost.

**Does not solve:** the other two causes seen in the same samples — omitting the
counter altogether and returning malformed JSON. Those are model-side and stay
open under Block A4.

## D2 — Distribution: a separate "product" repository

**Context.** This repository is the work repo: scene engine, eval harness, design
docs, goldens. A person who just wants to use arxi should not have to clone it, install
Go, or know there are two programs (the `arxi` core and the `arxi-tui` front end, joined
by `ARXI_BIN`).

**Chosen now:** nothing is split yet. `install.sh` + `scripts/build-release.sh` already
produce and install the static `arxi-tui` binary for linux/darwin/windows/termux with a
checksum. That is half of the product.

**Postponed — a product repo with one install command that brings everything.**
Proposed shape, to be confirmed with the owner:

- A small repo (name TBD) that holds no source of its own: its release pipeline pulls a
  pinned `arxi` core release and a pinned `arxi-tui` release, and publishes one bundle per
  platform plus `SHA256SUMS`, so the two versions that were tested together are the only
  pair a user can install.
- One command per platform: `curl -fsSL <url>/install.sh | sh` (POSIX sh, Termux
  included) and `irm <url>/install.ps1 | iex` for Windows PowerShell. Each installs both
  binaries into one directory and puts it on PATH.
- The launcher sets `ARXI_BIN` to the bundled core by itself, so `arxi` is the only thing
  the user types and the providers screen works on first run.
- A first-run check that tells the user, in plain words, what is missing (no provider
  registered -> "type /provider").
- This work repo keeps being the place where things are built and tested; the product repo
  only packages tagged releases of it.

**Needs first:** a decision on the core's release process (`arxi` lives in its own repo),
a Windows installer script (none exists), and a compatibility handshake test that refuses
a mismatched core/TUI pair with a sentence naming both versions.

**Revisit when:** the owner has a real `arxi` core build to bundle.
