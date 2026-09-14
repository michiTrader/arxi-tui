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

Status: planning closed, Phase 0.5 (the bind inventory) next. Nothing here is
implemented yet.
