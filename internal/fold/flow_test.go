package fold

import (
	"reflect"
	"testing"
)

// ev builds an event with the actor at top level, the way the core writes it.
func ev(seq int64, typ, actor string, p map[string]any) Event {
	if p == nil {
		p = map[string]any{}
	}
	return Event{Seq: seq, Type: typ, Actor: actor, Payload: p}
}

// featureTeamStart is the beginning of a run of core/examples/feature-team.yaml:
// two implementers in the build stage, an advisory reviewer.
func featureTeamStart() []Event {
	return []Event{
		ev(1, "run.started", "", map[string]any{"budget_usd": float64(20)}),
		ev(2, "stage.entered", "", map[string]any{"stage": "build", "index": float64(0)}),
		ev(3, "agent.activated", "backend", map[string]any{"agent": "backend", "role": "implementer"}),
		ev(4, "agent.activated", "frontend", map[string]any{"agent": "frontend", "role": "implementer"}),
	}
}

func TestAttentionIsEmptyWhenNothingStopsTheRun(t *testing.T) {
	s := Fold(featureTeamStart())
	if !reflect.DeepEqual(s.Attention, Attention{}) {
		t.Fatalf("a running team has no attention item, got %+v", s.Attention)
	}
	if len(s.WaitingOn) != 0 {
		t.Fatalf("nobody waits for anybody, got %v", s.WaitingOn)
	}
}

func TestAttentionNamesTheApprovalAndKeepsTheRef(t *testing.T) {
	log := append(featureTeamStart(),
		ev(5, "agent.blocked", "backend", map[string]any{
			"blocked_on":  "approval",
			"blocked_ref": map[string]any{"inbox_id": "i1", "tool": "bash", "policy": "ask"},
		}))
	a := Fold(log).Attention
	if a.Kind != "approval" || a.Actor != "backend" {
		t.Fatalf("kind/actor = %q/%q", a.Kind, a.Actor)
	}
	if a.Text != "backend waits for approval: bash" {
		t.Fatalf("text = %q", a.Text)
	}
	if a.Ref["inbox_id"] != "i1" {
		t.Fatalf("the blocked_ref must survive whole, got %v", a.Ref)
	}
}

func TestAttentionClearsWhenTheMemberIsUnblocked(t *testing.T) {
	log := append(featureTeamStart(),
		ev(5, "agent.blocked", "backend", map[string]any{
			"blocked_on": "approval", "blocked_ref": map[string]any{"tool": "bash"}}),
		ev(6, "agent.unblocked", "backend", map[string]any{"blocked_on": "approval"}))
	if a := Fold(log).Attention; a.Kind != "" {
		t.Fatalf("approval was answered, attention must clear, got %+v", a)
	}
}

func TestAttentionFallsBackToTheEarlierBlockWhenTheNewerOneClears(t *testing.T) {
	log := append(featureTeamStart(),
		ev(5, "agent.blocked", "backend", map[string]any{
			"blocked_on": "lock", "blocked_ref": map[string]any{"key": "db", "holder": "frontend"}}),
		ev(6, "agent.blocked", "frontend", map[string]any{
			"blocked_on": "approval", "blocked_ref": map[string]any{"tool": "write"}}))
	if a := Fold(log).Attention; a.Kind != "approval" {
		t.Fatalf("the newest block is shown, got %+v", a)
	}
	log = append(log, ev(7, "agent.unblocked", "frontend", map[string]any{"blocked_on": "approval"}))
	a := Fold(log).Attention
	if a.Kind != "lock" || a.Text != "backend waits for lock db held by frontend" {
		t.Fatalf("the older lock block is still open, got %+v", a)
	}
}

func TestQuiescentDiagnosisIsShownVerbatim(t *testing.T) {
	const diag = "stage review advances with quorum:3 and only two members can submit"
	log := append(featureTeamStart(), ev(5, "run.quiescent", "", map[string]any{"diagnosis": diag}))
	a := Fold(log).Attention
	if a.Kind != "quiescent" || a.Text != diag {
		t.Fatalf("got %+v", a)
	}
}

func TestQuiescentEndsWhenTheRunMovesAgain(t *testing.T) {
	base := append(featureTeamStart(), ev(5, "run.quiescent", "", map[string]any{"diagnosis": "stuck"}))
	cases := map[string]Event{
		"an agent is activated": ev(6, "agent.activated", "security", map[string]any{"agent": "security"}),
		"the stage advances":    ev(6, "stage.advanced", "", map[string]any{"from": "build", "to": "review", "to_index": float64(1)}),
		"a stage is entered":    ev(6, "stage.entered", "", map[string]any{"stage": "review", "index": float64(1)}),
	}
	for name, e := range cases {
		if a := Fold(append(append([]Event{}, base...), e)).Attention; a.Kind != "" {
			t.Errorf("%s: quiescent must end, got %+v", name, a)
		}
	}
}

func TestBudgetExceededIsAnAttentionItemUntilTheRunIsUnpaused(t *testing.T) {
	log := append(featureTeamStart(), ev(5, "budget.exceeded", "", map[string]any{
		"tree_spent_usd": float64(20.5), "budget_usd": float64(20)}))
	a := Fold(log).Attention
	if a.Kind != "budget_exceeded" || a.Text != "budget exceeded: 20.50 of 20.00 USD" {
		t.Fatalf("got %+v", a)
	}
	log = append(log, ev(6, "run.unpaused", "", map[string]any{"budget_usd": float64(40)}))
	if a := Fold(log).Attention; a.Kind != "" {
		t.Fatalf("raising the ceiling ends the stop, got %+v", a)
	}
}

func TestBudgetBlocksOfEveryMemberEndOnUnpause(t *testing.T) {
	log := append(featureTeamStart(),
		ev(5, "agent.blocked", "backend", map[string]any{"blocked_on": "budget", "blocked_ref": map[string]any{}}),
		ev(6, "run.unpaused", "", map[string]any{"budget_usd": float64(40)}))
	if a := Fold(log).Attention; a.Kind != "" {
		t.Fatalf("got %+v", a)
	}
}

func TestWaitingOnMapsAMemberToItsPeer(t *testing.T) {
	log := append(featureTeamStart(),
		ev(5, "agent.blocked", "frontend", map[string]any{
			"blocked_on": "peer", "blocked_ref": map[string]any{"peer": "backend"}}))
	s := Fold(log)
	if s.WaitingOn["frontend"] != "backend" || len(s.WaitingOn) != 1 {
		t.Fatalf("got %v", s.WaitingOn)
	}
	if s.Attention.Text != "frontend waits for backend" {
		t.Fatalf("text = %q", s.Attention.Text)
	}
	log = append(log, ev(6, "agent.unblocked", "frontend", map[string]any{"blocked_on": "peer"}))
	if w := Fold(log).WaitingOn; len(w) != 0 {
		t.Fatalf("the wait ended, got %v", w)
	}
}

func TestBlockedWithoutARefStillProducesALine(t *testing.T) {
	log := append(featureTeamStart(), ev(5, "agent.blocked", "backend", map[string]any{"blocked_on": "tool"}))
	if a := Fold(log).Attention; a.Text != "backend waits for a tool" {
		t.Fatalf("got %+v", a)
	}
}

func TestStageRunFollowsTheBuildReviewPlan(t *testing.T) {
	log := append(featureTeamStart(),
		ev(5, "stage.submitted", "backend", nil),
		ev(6, "stage.submitted", "frontend", nil),
		ev(7, "stage.advanced", "", map[string]any{"from": "build", "to": "review", "to_index": float64(1)}),
		ev(8, "stage.entered", "", map[string]any{"stage": "review", "index": float64(1)}),
		ev(9, "stage.submitted", "security", nil))
	got := Fold(log).StageRun
	want := []StageProgress{
		{Name: "build", Index: 0, Submitted: []string{"backend", "frontend"}, Left: true},
		{Name: "review", Index: 1, Submitted: []string{"security"}},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got  %+v\nwant %+v", got, want)
	}
}

func TestStageRunCountsADoubleSubmitOnce(t *testing.T) {
	log := append(featureTeamStart(),
		ev(5, "stage.submitted", "backend", nil),
		ev(6, "stage.submitted", "backend", nil))
	if got := Fold(log).StageRun[0].Submitted; !reflect.DeepEqual(got, []string{"backend"}) {
		t.Fatalf("got %v", got)
	}
}

func TestStageRunIsEmptyForAPlainChat(t *testing.T) {
	s := Fold([]Event{
		ev(1, "run.started", "", nil),
		ev(2, "run.prompt", "", map[string]any{"text": "hi"}),
		ev(3, "llm.response", "", map[string]any{"text": "hello"}),
	})
	if len(s.StageRun) != 0 {
		t.Fatalf("a plain chat has no stages, got %+v", s.StageRun)
	}
}

func TestStageEnteredAgainAfterLeavingIsANewRecord(t *testing.T) {
	log := append(featureTeamStart(),
		ev(5, "stage.submitted", "backend", nil),
		ev(6, "stage.advanced", "", map[string]any{"from": "build", "to": "review", "to_index": float64(1)}),
		ev(7, "stage.entered", "", map[string]any{"stage": "review", "index": float64(1)}),
		ev(8, "stage.advanced", "", map[string]any{"from": "review", "to": "build", "to_index": float64(0)}),
		ev(9, "stage.entered", "", map[string]any{"stage": "build", "index": float64(0)}))
	got := Fold(log).StageRun
	if len(got) != 3 || got[2].Name != "build" || got[2].Left || len(got[2].Submitted) != 0 {
		t.Fatalf("got %+v", got)
	}
}

func TestFlowFoldIsDeterministic(t *testing.T) {
	log := append(featureTeamStart(),
		ev(5, "agent.blocked", "frontend", map[string]any{"blocked_on": "peer", "blocked_ref": map[string]any{"peer": "backend"}}),
		ev(6, "stage.submitted", "backend", nil))
	a, b := Fold(log), Fold(log)
	if !reflect.DeepEqual(a.StageRun, b.StageRun) || !reflect.DeepEqual(a.Attention, b.Attention) || !reflect.DeepEqual(a.WaitingOn, b.WaitingOn) {
		t.Fatal("two folds of the same log disagree")
	}
}
