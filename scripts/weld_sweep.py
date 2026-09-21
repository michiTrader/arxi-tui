#!/usr/bin/env python3
"""Mutation sweep: break the fold on purpose, confirm the tests notice.

A passing test suite is evidence of nothing until the tests have been shown to
fail on a broken implementation. This repo has twice reported a defect
"covered" on the strength of a green run that would have stayed green with the
defect present, so the sweep is the instrument that keeps a claim of coverage
honest.

Three outcomes, and the third is the one that matters:

  CAUGHT   the weld compiled AND at least one test failed.
  ESCAPED  the weld compiled, every test passed. A real hole.
  INVALID  the weld did not compile. NOT a catch.

INVALID exists because a previous sweep counted a non-compiling weld as
caught: `[build failed]` was matched against a grep for `--- FAIL` and scored
as a pass. A weld that does not compile measures the Go parser, not the test
suite, and reporting it as coverage is the exact "result reported without
measuring" failure this harness is meant to detect.

Usage:  python3 scripts/weld_sweep.py
Exit:   0 when every weld is CAUGHT; 1 if any ESCAPED or INVALID.
"""

import argparse
import hashlib
import pathlib
import re
import shutil
import subprocess
import sys
import tempfile

ROOT = pathlib.Path(__file__).resolve().parent.parent
FOLD = ROOT / "internal" / "fold" / "fold.go"
NDJSON = ROOT / "internal" / "driver" / "ndjson.go"
ENGINE = ROOT / "internal" / "engine" / "render.go"
SCENE = ROOT / "internal" / "scene" / "validate.go"

# The packages whose tests are the instrument. A weld is CAUGHT only if one of
# these fails, so a weld in a file no test here exercises reports ESCAPED --
# which is the correct answer, not a harness bug.
#
# engine and scene joined the list after the sweep had run four times without
# them. They are the two biggest test packages in the repo -- 62 and 59 tests
# -- and neither had ever had a weld aimed at it, so their green was the one
# kind this harness exists to distrust. Two consecutive turns have now found
# real defects underneath a green suite, which makes "never measured" a
# statement about the measurement and not about the code.
TEST_PKGS = [
    "./internal/fold/",
    "./internal/driver/",
    "./internal/engine/",
    "./internal/scene/",
]


def weld_path(weld):
    """The file a weld targets: its 5th element, or fold.go by default.

    Defaulting keeps the 4-element welds written before the harness was
    multi-file working unchanged, rather than rewriting 18 known-good entries
    to add a constant.
    """
    return weld[4] if len(weld) > 4 else FOLD

# Each weld: (name, what breaking it should be caught by, old, new).
#
# Every `old` must appear EXACTLY ONCE in fold.go. The harness verifies that
# before applying; an ambiguous or missing anchor is a harness bug and is
# reported as such rather than silently skipped, because a weld that is never
# applied looks identical to a weld that was caught.
WELDS = [
    (
        "active-count: subtract for every finish, not just started work",
        "drives the count to -7 on the real log (control work never starts)",
        """		if id != "" && s.started[id] {
			delete(s.started, id)
			if s.ExecActive > 0 {
				s.ExecActive--
			}
		}""",
        """		if id != "" {
			delete(s.started, id)
			if s.ExecActive > 0 {
				s.ExecActive--
			}
		}""",
    ),
    (
        "active-count: never decrement",
        "leaves 16 works in flight at the end of a finished run",
        """			if s.ExecActive > 0 {
				s.ExecActive--
			}""",
        """			if s.ExecActive > 0 {
				_ = s.ExecActive
			}""",
    ),
    (
        "prepared work counted as in flight",
        "claims work the executor never dispatched",
        """	case "exec.work_prepared":
		// A unit of work was bound""",
        """	case "exec.work_prepared":
		s.ExecActive++
		// A unit of work was bound""",
    ),
    (
        "unknown outcome folded into failed",
        "asserts a failure the core explicitly refused to assert",
        """		case "unknown":
			s.ExecUnknown++""",
        """		case "unknown":
			s.ExecFailed++""",
    ),
    (
        "completed outcome silently dropped",
        "reports zero completed work for a clean run",
        """		case "completed":
			s.ExecCompleted++""",
        """		case "completed":
			_ = s.ExecCompleted""",
    ),
    (
        "duplicate terminal record counted twice",
        "overstates work done on a log the core considers legal",
        """		if id != "" && s.finishedWork[id] {
			break // already counted; Recover() tolerates a repeated record
		}""",
        """		if false {
			break
		}""",
    ),
    (
        "cursor read as int64 instead of float64 (the silent-zero shape)",
        "leaves the durable cursor at 0 with no error",
        """		if v, ok := e.Payload["source_seq"].(float64); ok {
			if c := int64(v); c > s.ExecCursor {
				s.ExecCursor = c
			}
		}""",
        """		if v, ok := e.Payload["source_seq"].(int64); ok {
			if v > s.ExecCursor {
				s.ExecCursor = v
			}
		}""",
    ),
    (
        "cursor assigned rather than max'd (can move backwards)",
        "names an already-executed event as the next to run",
        """			if c := int64(v); c > s.ExecCursor {
				s.ExecCursor = c
			}""",
        """			s.ExecCursor = int64(v)""",
    ),
    (
        "run.result read as a generic verdict rather than success",
        "invents a failure the core never reports",
        """		s.RunOutcome = "succeeded\"""",
        """		s.RunOutcome = "finished\"""",
    ),
    (
        "run.result summary dropped",
        "paraphrases the run instead of quoting it",
        """		if v, ok := e.Payload["summary"].(string); ok {
			s.RunSummary = v
		}""",
        """		if v, ok := e.Payload["summary"].(string); ok {
			_ = v
		}""",
    ),
    (
        "run.result result_from dropped",
        "loses which blueprint rule produced the verdict",
        """		if v, ok := e.Payload["result_from"].(string); ok {
			s.RunResultFrom = v
		}""",
        """		if v, ok := e.Payload["result_from"].(string); ok {
			_ = v
		}""",
    ),
    (
        "outcome defaults to succeeded with no run.result",
        "collapses 'no verdict yet' into 'it worked'",
        """		ExecPhase:    "idle", // nothing in flight before the first work starts""",
        """		ExecPhase:    "idle", // nothing in flight before the first work starts
		RunOutcome:   "succeeded",""",
    ),
    (
        "phase pinned to working",
        "spins forever on a run that finished",
        """	if s.ExecActive > 0 {
		s.ExecPhase = "working"
		return
	}
	s.ExecPhase = "idle\"""",
        """	s.ExecPhase = "working\"""",
    ),
    (
        "phase pinned to idle",
        "shows nothing while the run works",
        """	if s.ExecActive > 0 {
		s.ExecPhase = "working"
		return
	}
	s.ExecPhase = "idle\"""",
        """	s.ExecPhase = "idle\"""",
    ),
    (
        "work with no id counted as active",
        "pins the indicator to working for the rest of the run",
        """		id, ok := e.Payload["work_id"].(string)
		if !ok || id == "" {""",
        """		id, ok := e.Payload["work_id"].(string)
		if false && (!ok || id == "") {""",
    ),
    (
        "repeated start counted twice",
        "double-counts one unit of work in flight",
        """		if s.started[id] {
			// Replay saw the same start twice; it is still one unit of work.
			break
		}""",
        """		if false {
			break
		}""",
    ),
    (
        "exec.* dropped from the handled set",
        "silently returns the host to a blank screen during execution",
        """	"exec.work_prepared":  true,""",
        """	"exec.work_prepared":  false,""",
    ),
    (
        "run.result dropped from the handled set",
        "hides the verdict from the coverage measurement",
        """	"run.result": true,""",
        """	"run.result": false,""",
    ),
    # --- tool.* family ---
    #
    # The first weld here is the one that matters most. Reading payload.agent
    # instead of the top-level actor passes EVERY test built on the recorded
    # --sim log, because fake.go stamps both spellings. Only the synthetic
    # production-shaped event catches it. If this one ESCAPES, the family's
    # coverage claim is worthless in exactly the deployment that costs money.
    (
        "actor read from payload.agent instead of the top-level field",
        "attributes tool calls in --sim and leaves them anonymous in production",
        """	if e.Actor != "" {
		return e.Actor
	}
	return str(e.Payload, "agent")""",
        """	if a := str(e.Payload, "agent"); a != "" {
		return a
	}
	return e.Actor""",
    ),
    (
        "payload.agent fallback removed (actor-only reading)",
        "leaves the whole exec.* family unattributed: 91 events carry the name only in the payload",
        """	if e.Actor != "" {
		return e.Actor
	}
	return str(e.Payload, "agent")""",
        """	return e.Actor""",
    ),
    (
        "tool calls deduped by call_id",
        "merges four calls by two agents into one, and the count looks plausible",
        """		s.ToolCalls = append(s.ToolCalls, a)
		s.ToolCallsTotal++""",
        """		dup := false
		for _, c := range s.ToolCalls {
			if c.CallID == a.CallID {
				dup = true
			}
		}
		if !dup {
			s.ToolCalls = append(s.ToolCalls, a)
		}
		s.ToolCallsTotal++""",
    ),
    (
        "terminal record with no open call is dropped",
        "makes the effect-runner path (production) fold to an empty tool list",
        """	s.ToolCalls = append(s.ToolCalls, ToolActivity{
		Actor:   actor,
		Tool:    str(e.Payload, "tool"),
		CallID:  str(e.Payload, "call_id"),
		Outcome: outcome,
		Result:  result,
		Policy:  policy,
		Seq:     e.Seq,
	})""",
        """	_ = outcome""",
    ),
    (
        "completion paired with any open call, ignoring the actor",
        "credits one agent's tool result to another",
        """		if c.Outcome != "" || c.Actor != actor {
			continue
		}""",
        """		if c.Outcome != "" {
			continue
		}""",
    ),
    (
        "ask-denial collapsed into a plain denial",
        "reports a refusal at the moment the run is waiting to be permitted",
        """		if policy == "ask" {
			s.ToolsAwaitingApproval++
		}""",
        """		if policy == "deny" {
			s.ToolsAwaitingApproval++
		}""",
    ),
    (
        "a denial also appended as a todo",
        "double-counts every approval request in the badge",
        """		s.closeToolCall(e, "denied", "", policy)
		s.ToolsDenied++""",
        """		s.closeToolCall(e, "denied", "", policy)
		s.Todos = append(s.Todos, TodoItem{Task: "tool", BlockedOn: "approval"})
		s.ToolsDenied++""",
    ),
    (
        "tool result dropped rather than carried verbatim",
        "loses the command's own output, which the core calls an answer",
        """		s.closeToolCall(e, "completed", str(e.Payload, "result"), "")""",
        """		s.closeToolCall(e, "completed", "", "")""",
    ),
    (
        "member never enters the tool state",
        "renders a member mid-tool-call as merely thinking",
        """			m.State = "tool"
			m.Busy = true""",
        """			m.Busy = true""",
    ),
    (
        "member left in the tool state after the call ended",
        "shows a tool running forever after it returned",
        """	if m, ok := s.members[actor]; ok && m.State == "tool" {
		m.State = "thinking"
	}""",
        """	if m, ok := s.members[actor]; ok && m.State == "tool" {
		_ = m
	}""",
    ),
    (
        "member goes idle after a tool instead of thinking",
        "says the agent stopped while it is mid-turn",
        """	if m, ok := s.members[actor]; ok && m.State == "tool" {
		m.State = "thinking"
	}
}""",
        """	if m, ok := s.members[actor]; ok && m.State == "tool" {
		m.State = "idle"
	}
}""",
    ),
    (
        "pending derived as total minus terminal (the underflow shape)",
        "underflows on the path where completions arrive with no opening",
        """	s.ToolsPending = 0
	for _, c := range s.ToolCalls {
		if c.Outcome == "" {
			s.ToolsPending++
		}
	}""",
        """	s.ToolsPending = s.ToolCallsTotal
	for _, c := range s.ToolCalls {
		if c.Outcome != "" {
			s.ToolsPending--
		}
	}""",
    ),
    (
        "tool.last pinned to the first call rather than the newest",
        "shows a stale tool in the status row for the rest of the run",
        """		s.ToolLast = s.ToolCalls[len(s.ToolCalls)-1]""",
        """		s.ToolLast = s.ToolCalls[0]""",
    ),
    (
        "tool.* dropped from the handled set",
        "returns the host to showing position and plumbing but never actions",
        """	"tool.call":           true,""",
        """	"tool.call":           false,""",
    ),
    # --- log-follow confirmed reads (internal/driver/ndjson.go) ---
    #
    # These are the welds the harness could not express until it went
    # multi-file, which is exactly why the log-follow fix shipped with four
    # green tests and no evidence they measured anything. The first weld is
    # the defect that was actually found in production code: trusting
    # newline-termination and ignoring pending.commit.
    (
        "confirmed boundary ignores pending.commit (the shipped defect)",
        "delivers events the core will truncate away; the fold cannot take them back",
        """	rollback, ok, err := pendingRollback(logPath)
	if err != nil {
		return 0, err
	}
	if ok && rollback < end {""",
        """	rollback, ok, err := pendingRollback(logPath)
	if err != nil {
		return 0, err
	}
	if false && ok && rollback < end {""",
        NDJSON,
    ),
    (
        "confirmed boundary ignores the newline bound",
        "delivers a half-written record as if it were an event",
        """	end, err := lastNewlineBefore(f, info.Size())""",
        """	end := info.Size()
	_ = lastNewlineBefore""",
        NDJSON,
    ),
    (
        "marker bound applied without clamping to a record boundary",
        "cuts the confirmed prefix mid-record when a marker is not record-aligned",
        """		return lastNewlineBefore(f, rollback)""",
        """		return rollback, nil""",
        NDJSON,
    ),
    (
        "a retreating confirmed boundary re-emits already-delivered events",
        "duplicates events into an append-only fold, which is not a correction",
        """	if confirmed <= *delivered {""",
        """	if confirmed == *delivered {""",
        NDJSON,
    ),
    (
        "delivered offset never advances",
        "re-sends the whole confirmed prefix on every poll",
        """	*delivered = confirmed
	return nil""",
        """	return nil""",
        NDJSON,
    ),
    (
        "legacy bare-integer pending marker treated as absent",
        "silently confirms an in-flight batch, the seq/sequence failure again",
        """	if n, perr := strconv.ParseInt(text, 10, 64); perr == nil {""",
        """	if n, perr := strconv.ParseInt(text, 10, 64); perr != nil {""",
        NDJSON,
    ),
    (
        "an unparsable pending marker is treated as absent",
        "turns a file the host cannot read into permission to deliver uncommitted events",
        """		return 0, false, fmt.Errorf("ndjson: pending.commit is neither a bare "+
			"offset nor JSON with pre_append_size: %w", jerr)""",
        """		return 0, false, nil""",
        NDJSON,
    ),
    # --- the stage.* family, and the three defects reading it exposed ---
    (
        "stage.entered does not clear the previous stage's submissions",
        "lets a stale submit satisfy the next stage's advance rule, skipping a stage",
        """\t\ts.StageSubmissions = []string{}""",
        """\t\t_ = s.StageSubmissions""",
    ),
    (
        "stage index assigned unconditionally (the silent-zero shape)",
        "walks a run in stage 3 back to stage 0 on one event with no index key",
        """\t\tif idx, ok := e.Payload["index"].(float64); ok {
\t\t\ts.StageIndex = int(idx)
\t\t}""",
        """\t\tidx, _ := e.Payload["index"].(float64)
\t\ts.StageIndex = int(idx)""",
    ),
    (
        "stage index read as int64 rather than float64",
        "leaves the position at its previous value on every event, with no error",
        """\t\tif idx, ok := e.Payload["index"].(float64); ok {""",
        """\t\tif idx, ok := e.Payload["index"].(int64); ok {""",
    ),
    (
        "stage.advanced records only `from`, not the destination",
        "shows the stage the run has already left when the log ends between the pair",
        """\t\tif to := str(e.Payload, "to"); to != "" {
\t\t\ts.StageName = to
\t\t}""",
        """\t\tif to := str(e.Payload, "to"); to != "" {
\t\t\t_ = to
\t\t}""",
    ),
    (
        "stage.advanced does not move the index",
        "leaves stage.index one behind stage.name on a truncated log",
        """\t\tif idx, ok := e.Payload["to_index"].(float64); ok {
\t\t\ts.StageIndex = int(idx)
\t\t}""",
        """\t\tif idx, ok := e.Payload["to_index"].(float64); ok {
\t\t\t_ = idx
\t\t}""",
    ),
    (
        "stage.advances never counted",
        "reports zero transitions for a run that changed stage",
        """\t\ts.StageAdvances++""",
        """\t\t_ = s.StageAdvances""",
    ),
    (
        "stage.index defaults to 0 instead of the -1 sentinel",
        "makes a run that has entered no stage indistinguishable from one in its first",
        """\t\tStageIndex:   -1,""",
        """\t\tStageIndex:   0,""",
    ),
    (
        "the submitter is read from payload.agent before the top-level actor",
        "attributes a submit to a name the core did not consider the member for it",
        """\t\tactor := e.actorName()
\t\tif actor != "" {
\t\t\t// Recorded once per member per stage.""",
        """\t\tactor := str(e.Payload, "agent")
\t\tif actor == "" {
\t\t\tactor = e.Actor
\t\t}
\t\tif actor != "" {
\t\t\t// Recorded once per member per stage.""",
    ),
    (
        "a repeated submit from one member is counted twice",
        "reports a two-member quorum for a stage one member has answered",
        """\t\t\tif !seen {
\t\t\t\ts.StageSubmissions = append(s.StageSubmissions, actor)
\t\t\t}""",
        """\t\t\t_ = seen
\t\t\ts.StageSubmissions = append(s.StageSubmissions, actor)""",
    ),
    (
        "stage.submitted does not set the member state",
        "the `submitted` state BINDS.md signs is never produced by the fold",
        """\t\t\tm.State = "submitted\"""",
        """\t\t\t_ = m""",
    ),
    (
        "submitted count maintained rather than derived from the list",
        "the badge and the list disagree after any clear",
        """\ts.StageSubmittedCount = uint(len(s.StageSubmissions))""",
        """\ts.StageSubmittedCount = s.StageSubmittedCount""",
    ),
    (
        "stage.timeout folded as a run failure",
        "asserts a verdict the core withheld; escalate leaves the stage open",
        """\tcase "stage.timeout":""",
        """\tcase "stage.timeout":
\t\ts.RunOutcome = "failed"
\t\ts.StageName = ""
""",
    ),
    (
        "agent.turn_done overwrites the submitted state",
        "erases `submitted` one event after it is set; the core guards with !m.Submitted",
        """\t\t\t\tcase "waiting", "failed", "submitted":""",
        """\t\t\t\tcase "waiting", "failed":""",
    ),
    (
        "agent.turn_done idles a member waiting on a human",
        "reports an agent ready for work while its approval sits unanswered",
        """\t\t\t\tcase "waiting", "failed", "submitted":
\t\t\t\t\t// State preserved.""",
        """\t\t\t\tcase "failed", "submitted":
\t\t\t\t\t// State preserved.""",
    ),
    (
        "agent.turn_done never returns anyone to idle",
        "leaves every member stuck on `thinking` for the whole run",
        """\t\t\t\tdefault:
\t\t\t\t\tm.State = "idle"
\t\t\t\t}""",
        """\t\t\t\tdefault:
\t\t\t\t}""",
    ),
    (
        "agent.blocked resolved by payload.actor only",
        "attributes every todo to nobody; the core reads out.Member(e.Actor)",
        """\t\tactor := e.actorName()
\t\tif actor == "" {
\t\t\tactor = str(e.Payload, "actor")
\t\t}
\t\ts.Todos = append(s.Todos,""",
        """\t\tactor := str(e.Payload, "actor")
\t\ts.Todos = append(s.Todos,""",
    ),
    (
        "team.members rendered by ranging the map (nondeterministic)",
        "the same bytes fold to a different order; goldens and replay fail intermittently",
        """\tfor _, id := range s.memberOrder {
\t\tif m, ok := s.members[id]; ok {
\t\t\ts.TeamMembers = append(s.TeamMembers, *m)
\t\t}
\t}""",
        """\tfor _, m := range s.members {
\t\ts.TeamMembers = append(s.TeamMembers, *m)
\t}""",
    ),
    (
        "a newly seen member is not appended to the order list",
        "the member exists in the map and vanishes from the rendered panel",
        """\t\t\t\ts.memberOrder = append(s.memberOrder, agent)""",
        """\t\t\t\t_ = agent""",
    ),

    # --- internal/engine and internal/scene: 121 tests, never measured ---
    #
    # These two packages have the most tests in the repo and no weld had ever
    # been aimed at either. That combination is exactly what this harness
    # exists to distrust: this turn and the last both found real defects
    # sitting under a green suite, so "green without a sweep" has now been
    # wrong twice in a row on measured evidence.
    #
    # The welds below break DECISIONS the source argues for in its own
    # comments -- the two-spelling style token, the id-equality focus glow,
    # the placeholder-as-falsy rule -- rather than arbitrary lines. A weld on
    # a line nobody reasoned about measures typing, not coverage.
    (
        "when: the placeholder is treated as a truthy value",
        "a gated node draws because its bind is UNRESOLVED, which is the opposite of what it means",
        """\tcase "", "0", "false", placeholderValue:""",
        """\tcase "", "0", "false":""",
        ENGINE,
    ),
    (
        "when: \"false\" read as truthy",
        "every `when`-gated node renders permanently",
        """\tcase "", "0", "false", placeholderValue:""",
        """\tcase "", "0", placeholderValue:""",
        ENGINE,
    ),
    (
        "when: a node with no `when` is hidden rather than shown",
        "blanks the whole scene; absence of a gate is not a closed gate",
        """\tif n == nil || n.When == "" {
\t\treturn false
\t}""",
        """\tif n == nil || n.When == "" {
\t\treturn true
\t}""",
        ENGINE,
    ),
    (
        "focus glow: matches on empty id",
        "glows every id-less node the moment nothing has focus -- the inverse of the property",
        """\tif n.ID == "" || state.UIFocus != n.ID {""",
        """\tif state.UIFocus != n.ID {""",
        ENGINE,
    ),
    (
        "focus glow: written to the document instead of a copy",
        "the glow becomes permanent; state keyed to the wrong lifetime",
        """\tglowed := *n
\tglowed.Style = make(map[string]string, len(n.Style)+1)""",
        """\tglowed := *n
\tif n.Style == nil {
\t\tglowed.Style = make(map[string]string, 1)
\t\tn.Style = glowed.Style
\t}
\tglowed.Style = n.Style""",
        ENGINE,
    ),
    (
        "focus glow: written under the canonical key only",
        "a node spelling its token `token` keeps its old style; the two-spelling defect again",
        """\tfor _, key := range scene.StyleTokenKeys() {
\t\tglowed.Style[key] = n.FocusGlow.Style
\t}""",
        """\tglowed.Style["style"] = n.FocusGlow.Style""",
        ENGINE,
    ),
    (
        "style token: only the canonical spelling is read",
        "a scene using the other ACCEPTED spelling passes validation and renders unstyled",
        """\tfor _, key := range scene.StyleTokenKeys() {
\t\tif name := style[key]; name != "" {""",
        """\tfor _, key := range []string{"style"} {
\t\tif name := style[key]; name != "" {""",
        ENGINE,
    ),
    (
        "validator: the two accepted style spellings are cut to one",
        "the validator and the renderer stop agreeing about the vocabulary",
        """var styleTokenKeys = [...]string{"token", "style"}""",
        """var styleTokenKeys = [...]string{"style"}""",
        SCENE,
    ),
    (
        "validator: an unrendered-but-accepted field is no longer refused",
        "the field is silently dropped -- the checked-but-never-drawn class, with validation reporting success",
        """\t\tbecause, ok := unrenderedFields[field]
\t\tif !ok {
\t\t\tcontinue
\t\t}""",
        """\t\tbecause, ok := unrenderedFields[field]
\t\t_ = because
\t\tif true || !ok {
\t\t\tcontinue
\t\t}""",
        SCENE,
    ),
    (
        "validator: unrendered fields reported in map order",
        "the error address moves between runs, so it is not an address",
        """\tsort.Strings(declared)""",
        """\t_ = sort.Strings""",
        SCENE,
    ),
]


def run(cmd, cwd):
    return subprocess.run(
        cmd, cwd=cwd, capture_output=True, text=True, timeout=600
    )


def parse_args(argv):
    """--shard i/n runs one slice of the welds; --list prints the inventory.

    Sharding exists because the sweep outgrew the wall-clock budget of the
    environment it runs in. At 67 welds and a four-package suite it needs
    roughly half an hour end to end, and this sandbox has been destroyed
    mid-run: the harness restores sources in a `finally`, but a process that
    is killed outright never reaches it, and the next session then inherits a
    working tree with one weld silently pasted into it. A sweep whose failure
    mode is "the defect is now in the source and the log that would say so is
    gone" is worse than no sweep.

    Shards are contiguous slices rather than a stride, so a shard that reports
    an escape names a range small enough to re-run on its own. Running every
    shard covers every weld exactly once -- asserted by --list, which prints
    the assignment so the partition can be checked rather than trusted.
    """
    p = argparse.ArgumentParser(description=__doc__)
    p.add_argument("--shard", metavar="I/N",
                   help="run slice I of N (1-based), e.g. 2/4")
    p.add_argument("--list", action="store_true",
                   help="print the weld inventory and shard assignment, run nothing")
    a = p.parse_args(argv)
    if not a.shard:
        return 0, 1, a.list
    try:
        i, n = (int(x) for x in a.shard.split("/"))
    except ValueError:
        p.error(f"--shard wants I/N with integers, got {a.shard!r}")
    if not (1 <= i <= n):
        p.error(f"--shard {a.shard}: need 1 <= I <= N")
    return i - 1, n, a.list


def shard_bounds(total, index, count):
    """The [lo, hi) slice for one shard, with the remainder spread evenly.

    Computed rather than rounded so that the shards partition the welds
    exactly: sum of all shard sizes == total, and no weld is in two shards.
    A sweep that silently skipped a weld would report a clean run it never
    performed, which is the failure this whole harness exists to refuse.
    """
    base, extra = divmod(total, count)
    lo = index * base + min(index, extra)
    hi = lo + base + (1 if index < extra else 0)
    return lo, hi


def main(argv=None):
    shard_i, shard_n, want_list = parse_args(argv if argv is not None else sys.argv[1:])
    lo, hi = shard_bounds(len(WELDS), shard_i, shard_n)

    if want_list:
        print(f"{len(WELDS)} welds, {shard_n} shard(s)")
        for s in range(shard_n):
            a, b = shard_bounds(len(WELDS), s, shard_n)
            print(f"  shard {s+1}/{shard_n}: welds {a+1}..{b} ({b-a})")
        covered = sum(shard_bounds(len(WELDS), s, shard_n)[1]
                      - shard_bounds(len(WELDS), s, shard_n)[0]
                      for s in range(shard_n))
        # The partition is asserted, not assumed. A shard arithmetic bug that
        # dropped a weld would make every shard pass while the weld it
        # skipped was never applied -- indistinguishable, in the summary,
        # from a weld that was caught.
        print(f"  total covered: {covered} "
              f"({'exact' if covered == len(WELDS) else 'MISMATCH'})")
        for i, w in enumerate(WELDS, 1):
            print(f"  [{i:2d}] {weld_path(w).stem}: {w[0]}")
        return 0 if covered == len(WELDS) else 2

    # Every file any weld targets, read once up front. The sweep used to hold
    # a single `original` string for fold.go, which is why the log-follow fix
    # in ndjson.go shipped unmeasured: the harness could not express a weld
    # outside one file, so the question was never asked. An instrument that
    # can only inspect the place a defect was last found is not an instrument.
    targets = sorted({weld_path(w) for w in WELDS})
    originals = {t: t.read_text() for t in targets}
    shas_before = {t: hashlib.sha256(s.encode()).hexdigest()
                   for t, s in originals.items()}

    # Anchor check first. A weld whose anchor is missing or ambiguous was
    # never really applied, and an unapplied weld is indistinguishable from a
    # caught one unless it is called out here.
    bad = []
    for w in WELDS:
        name, _, old, _ = w[0], w[1], w[2], w[3]
        path = weld_path(w)
        n = originals[path].count(old)
        if n != 1:
            bad.append(f"  {name}: anchor appears {n} times in "
                       f"{path.relative_to(ROOT)}, want exactly 1")
    if bad:
        print("HARNESS ERROR: welds cannot be applied unambiguously")
        print("\n".join(bad))
        return 2

    print("baseline: verifying the suite is green before breaking anything")
    base = run(["go", "test"] + TEST_PKGS, ROOT)
    if base.returncode != 0:
        print("HARNESS ERROR: the suite is already failing; a sweep against a")
        print("red baseline cannot distinguish a caught weld from a pre-existing")
        print("failure.")
        print(base.stdout[-3000:])
        return 2
    print(f"baseline green; {len(targets)} file(s) under weld: "
          f"{', '.join(str(t.relative_to(ROOT)) for t in targets)}\n")

    caught, escaped, invalid = [], [], []

    for i, w in enumerate(WELDS[lo:hi], lo + 1):
        name, why, old, new = w[0], w[1], w[2], w[3]
        path = weld_path(w)
        label = f"{path.stem}: {name}"
        path.write_text(originals[path].replace(old, new, 1))
        try:
            # Compile first, as its own question. Conflating "does not build"
            # with "test failed" is what produced a false catch before.
            build = run(["go", "build", "./..."], ROOT)
            if build.returncode != 0:
                invalid.append((label, build.stderr.strip().splitlines()[:3]))
                print(f"[{i:2d}/{len(WELDS)}] INVALID  {label}")
                continue

            res = run(["go", "test"] + TEST_PKGS, ROOT)
            out = res.stdout + res.stderr
            failed = re.search(r"^--- FAIL", out, re.M) or res.returncode != 0
            if failed:
                names = re.findall(r"^--- FAIL: (\S+)", out, re.M)
                caught.append((label, names))
                print(f"[{i:2d}/{len(WELDS)}] CAUGHT   {label}")
                for n in names[:3]:
                    print(f"              by {n}")
            else:
                escaped.append((label, why))
                print(f"[{i:2d}/{len(WELDS)}] ESCAPED  {label}")
                print(f"              would have: {why}")
        finally:
            path.write_text(originals[path])

    # Every welded file must come back byte-identical, or the sweep has
    # mutated the thing it was measuring.
    unrestored = []
    for t in targets:
        if hashlib.sha256(t.read_text().encode()).hexdigest() != shas_before[t]:
            unrestored.append(t)

    print()
    print("=" * 62)
    if shard_n > 1:
        print(f"shard:   {shard_i + 1}/{shard_n}  (welds {lo + 1}..{hi} of "
              f"{len(WELDS)})")
        print("         a clean shard is NOT a clean sweep; every shard must")
        print("         be run before the result means anything")
    print(f"welds:   {hi - lo}")
    print(f"CAUGHT:  {len(caught)}")
    print(f"ESCAPED: {len(escaped)}")
    print(f"INVALID: {len(invalid)}  (did not compile; NOT a catch)")
    print(f"sources restored byte-identical: {not unrestored}")
    for t in targets:
        print(f"  {t.relative_to(ROOT)}  sha256 {shas_before[t][:16]}...")

    if escaped:
        print("\nESCAPED welds are holes in the suite:")
        for n, why in escaped:
            print(f"  - {n}\n      {why}")
    if invalid:
        print("\nINVALID welds measured nothing and must be re-welded:")
        for n, err in invalid:
            print(f"  - {n}")
            for line in err:
                print(f"      {line}")

    if unrestored:
        print("\nFATAL: not restored: "
              f"{', '.join(str(t.relative_to(ROOT)) for t in unrestored)}. "
              "Check git diff.")
        return 2
    return 0 if (not escaped and not invalid) else 1


if __name__ == "__main__":
    sys.exit(main())
