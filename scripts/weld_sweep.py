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
]


def run(cmd, cwd):
    return subprocess.run(
        cmd, cwd=cwd, capture_output=True, text=True, timeout=600
    )


def main():
    original = FOLD.read_text()
    src_sha_before = hashlib.sha256(original.encode()).hexdigest()

    # Anchor check first. A weld whose anchor is missing or ambiguous was
    # never really applied, and an unapplied weld is indistinguishable from a
    # caught one unless it is called out here.
    bad = []
    for name, _, old, _ in WELDS:
        n = original.count(old)
        if n != 1:
            bad.append(f"  {name}: anchor appears {n} times, want exactly 1")
    if bad:
        print("HARNESS ERROR: welds cannot be applied unambiguously")
        print("\n".join(bad))
        return 2

    print(f"baseline: verifying the suite is green before breaking anything")
    base = run(["go", "test", "./internal/fold/", "./internal/driver/"], ROOT)
    if base.returncode != 0:
        print("HARNESS ERROR: the suite is already failing; a sweep against a")
        print("red baseline cannot distinguish a caught weld from a pre-existing")
        print("failure.")
        print(base.stdout[-3000:])
        return 2
    print("baseline green\n")

    caught, escaped, invalid = [], [], []

    for i, (name, why, old, new) in enumerate(WELDS, 1):
        FOLD.write_text(original.replace(old, new, 1))
        try:
            # Compile first, as its own question. Conflating "does not build"
            # with "test failed" is what produced a false catch before.
            build = run(["go", "build", "./..."], ROOT)
            if build.returncode != 0:
                invalid.append((name, build.stderr.strip().splitlines()[:3]))
                print(f"[{i:2d}/{len(WELDS)}] INVALID  {name}")
                continue

            res = run(
                ["go", "test", "./internal/fold/", "./internal/driver/"], ROOT
            )
            out = res.stdout + res.stderr
            failed = re.search(r"^--- FAIL", out, re.M) or res.returncode != 0
            if failed:
                names = re.findall(r"^--- FAIL: (\S+)", out, re.M)
                caught.append((name, names))
                print(f"[{i:2d}/{len(WELDS)}] CAUGHT   {name}")
                for n in names[:3]:
                    print(f"              by {n}")
            else:
                escaped.append((name, why))
                print(f"[{i:2d}/{len(WELDS)}] ESCAPED  {name}")
                print(f"              would have: {why}")
        finally:
            FOLD.write_text(original)

    # The source must come back byte-identical, or the sweep has mutated the
    # thing it was measuring.
    sha_after = hashlib.sha256(FOLD.read_text().encode()).hexdigest()
    restored = sha_after == src_sha_before

    print()
    print("=" * 62)
    print(f"welds:   {len(WELDS)}")
    print(f"CAUGHT:  {len(caught)}")
    print(f"ESCAPED: {len(escaped)}")
    print(f"INVALID: {len(invalid)}  (did not compile; NOT a catch)")
    print(f"source restored byte-identical: {restored}")
    print(f"sha256 {src_sha_before[:16]}... -> {sha_after[:16]}...")

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

    if not restored:
        print("\nFATAL: fold.go was not restored. Check git diff.")
        return 2
    return 0 if (not escaped and not invalid) else 1


if __name__ == "__main__":
    sys.exit(main())
