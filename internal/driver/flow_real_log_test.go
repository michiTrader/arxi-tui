package driver

import (
	"reflect"
	"testing"

	"github.com/michiTrader/arxi_tui/internal/fold"
)

// The /flow fold was written against hand-built events. These tests hold it to
// the log the real arxi core recorded (testdata/serve/real_run.ndjson), which
// walks build -> review, so the fold and the core cannot quietly disagree.

func TestTheRealRunsStagesAreFoldedForTheFlowScreen(t *testing.T) {
	state := fold.Fold(replayBytes(t, realRunLog(t)))

	if len(state.StageRun) != 2 {
		t.Fatalf("stages = %+v, want build then review", state.StageRun)
	}
	build, review := state.StageRun[0], state.StageRun[1]
	if build.Name != "build" || build.Index != 0 || !build.Left {
		t.Errorf("build = %+v: the run entered it first and advanced out of it at seq 62", build)
	}
	if review.Name != "review" || review.Index != 1 || review.Left {
		t.Errorf("review = %+v: the run ends in it", review)
	}
	if len(build.Submitted) == 0 {
		t.Errorf("nobody is recorded as having submitted to build, but the run advanced out of it")
	}
}

func TestTheRealRunLeavesNothingHoldingTheRunUp(t *testing.T) {
	state := fold.Fold(replayBytes(t, realRunLog(t)))
	if !reflect.DeepEqual(state.Attention, fold.Attention{}) {
		t.Errorf("a run that ended well must not show a blocker, got %+v", state.Attention)
	}
	if len(state.WaitingOn) != 0 {
		t.Errorf("nobody waits at the end of the run, got %v", state.WaitingOn)
	}
}
