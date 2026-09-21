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

# The packages whose tests are the instrument. A weld is CAUGHT only if one of
# these fails, so a weld in a file no test here exercises reports ESCAPED --
# which is the correct answer, not a harness bug.
TEST_PKGS = ["./internal/fold/", "./internal/driver/"]


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
]


def run(cmd, cwd):
    return subprocess.run(
        cmd, cwd=cwd, capture_output=True, text=True, timeout=600
    )


def main():
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

    for i, w in enumerate(WELDS, 1):
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
    print(f"welds:   {len(WELDS)}")
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
