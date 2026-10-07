package main

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/michiTrader/arxi_tui/internal/driver"
	"github.com/michiTrader/arxi_tui/internal/fold"
)

var threeStages = []driver.BlueprintStage{{Name: "plan"}, {Name: "build"}, {Name: "review"}}

func TestStageLineShowsWhatIsAheadFromThePlan(t *testing.T) {
	runs := []fold.StageProgress{{Name: "plan", Left: true}, {Name: "build", Submitted: []string{"dev"}}}
	got := stageLine(runs, threeStages)
	want := "plan ✓ → build ● (submitted: dev) → review ○"
	if got != want {
		t.Fatalf("stageLine = %q, want %q", got, want)
	}
}

func TestStageLineWithoutAPlanDrawsOnlyWhatHappened(t *testing.T) {
	runs := []fold.StageProgress{{Name: "plan"}}
	if got := stageLine(runs, nil); got != "plan ●" {
		t.Fatalf("stageLine = %q", got)
	}
}

func TestStageLineBeforeAnyStageShowsTheWholePlan(t *testing.T) {
	if got := stageLine(nil, threeStages); got != "plan ○ → build ○ → review ○" {
		t.Fatalf("stageLine = %q", got)
	}
}

func TestStageLineIgnoresAStageTheLogHasButThePlanDoesNot(t *testing.T) {
	// The file was edited after the run began: nothing may be invented ahead.
	runs := []fold.StageProgress{{Name: "ghost"}}
	if got := stageLine(runs, threeStages); got != "ghost ●" {
		t.Fatalf("stageLine = %q", got)
	}
}

type planCore struct {
	info *driver.BlueprintInfo
	err  error
	path string
}

func (p *planCore) SubmitBlueprintValidate(_ context.Context, path string) (*driver.BlueprintInfo, error) {
	p.path = path
	return p.info, p.err
}

func TestFlowPlanIsReadFromTheTeamFileAndReachesTheScreen(t *testing.T) {
	core := &planCore{info: &driver.BlueprintInfo{Stages: threeStages}}
	done := make(chan flowOutcome, 1)
	startFlowPlan(context.Background(), core, "proj", "crew", done)
	select {
	case o := <-done:
		f := &flowScreen{}
		f.apply(o)
		f.working = "x"
		if len(f.plan) != 3 || f.working != "x" {
			t.Fatalf("plan = %+v, working = %q (a plan must not touch the answer state)", f.plan, f.working)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("no plan arrived")
	}
	if !strings.HasSuffix(strings.ReplaceAll(core.path, `\`, "/"), "proj/agents/crew.yaml") {
		t.Errorf("path = %q", core.path)
	}
}

func TestFlowPlanFailuresStaySilent(t *testing.T) {
	for name, core := range map[string]*planCore{
		"refused": {err: errors.New("nope")},
		"nothing": {info: &driver.BlueprintInfo{}},
	} {
		done := make(chan flowOutcome, 1)
		startFlowPlan(context.Background(), core, ".", "crew", done)
		select {
		case o := <-done:
			t.Errorf("%s: unexpected outcome %+v", name, o)
		case <-time.After(300 * time.Millisecond):
		}
	}
	done := make(chan flowOutcome, 1)
	startFlowPlan(context.Background(), nil, ".", "crew", done)
	startFlowPlan(context.Background(), &planCore{info: &driver.BlueprintInfo{Stages: threeStages}}, ".", `../evil`, done)
	select {
	case o := <-done:
		t.Errorf("a team name with a path in it was read: %+v", o)
	case <-time.After(300 * time.Millisecond):
	}
}

func TestFlowDetailDrawsTheFutureStagesBeforeTheRunEntersOne(t *testing.T) {
	f := &flowScreen{plan: threeStages}
	st := fold.Fold(nil)
	if d := f.detail(&st); !strings.Contains(d, "plan ○ → build ○ → review ○") {
		t.Fatalf("detail = %q", d)
	}
}
