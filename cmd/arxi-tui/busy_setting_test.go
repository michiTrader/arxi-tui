package main

import (
	"strings"
	"testing"
)

// enter_while_busy is a user setting like the others: validated, written canonically, put to
// the user when the agent proposes it, taught in the guide, typed as /ui busy.
func TestEnterWhileBusyIsAUserSettingWithAValidator(t *testing.T) {
	steer, queue := busySteer, busyQueue
	b, err := applyBehaviourPatch(behaviour{}, behaviourPatch{EnterWhileBusy: &steer})
	if err != nil || b.EnterWhileBusy != busySteer {
		t.Fatalf("steer: %v %+v", err, b)
	}
	b, err = applyBehaviourPatch(b, behaviourPatch{EnterWhileBusy: &queue})
	if err != nil || !b.empty() {
		t.Errorf("queue is the factory way and must leave the document empty: %v %+v.\nConsequence: behaviour.json keeps a line that says nothing.\nRemedy: applyBehaviourPatch stores queue as \"\".", err, b)
	}
	bad := "interrupt"
	_, err = applyBehaviourPatch(behaviour{}, behaviourPatch{EnterWhileBusy: &bad})
	if err == nil || !strings.Contains(err.Error(), "queue") || !strings.Contains(err.Error(), "steer") {
		t.Errorf("a refusal must say what is allowed: %v", err)
	}
	if !(behaviourPatch{EnterWhileBusy: &steer}).touchesWhatRuns() {
		t.Error("changing what Enter does was not put to the user.\nConsequence: the agent can make Enter stop the user's work without asking.\nRemedy: touchesWhatRuns includes EnterWhileBusy.")
	}
	if !strings.Contains(behaviourGuide(behaviour{}), "enter_while_busy") {
		t.Error("the guide must teach enter_while_busy, or the agent cannot offer it")
	}
	p, _, ok, perr := uiBehaviourCommand("/ui busy steer")
	if !ok || perr != nil || p.EnterWhileBusy == nil || *p.EnterWhileBusy != busySteer {
		t.Errorf("/ui busy steer: %+v %v %v", p, ok, perr)
	}
	if _, _, ok, perr := uiBehaviourCommand("/ui busy nonsense"); !ok || perr == nil {
		t.Error("/ui busy with a wrong word must be refused with the usage")
	}
	raw := encodeBehaviour(behaviour{EnterWhileBusy: busySteer})
	if !strings.Contains(string(raw), `"enter_while_busy": "steer"`) {
		t.Errorf("not written to behaviour.json: %s", raw)
	}
}

// A project may say how Enter treats a busy agent; the user's own choice stands where the
// project says nothing, and a clone keeps it (an edit never reaches the layer it came from).
func TestEnterWhileBusyFollowsTheLayersAndSurvivesAClone(t *testing.T) {
	if got := mergeBehaviour(behaviour{EnterWhileBusy: busySteer}, behaviour{Keys: map[string]actionList{"f5": {"cmd:/mode plan"}}}).EnterWhileBusy; got != busySteer {
		t.Errorf("a project that says nothing changed the user's setting to %q", got)
	}
	if got := mergeBehaviour(behaviour{}, behaviour{EnterWhileBusy: busySteer}).EnterWhileBusy; got != busySteer {
		t.Errorf("the project's setting did not apply, got %q.\nConsequence: /project settings for Enter are ignored.\nRemedy: mergeBehaviour copies EnterWhileBusy.", got)
	}
	if got := (behaviour{EnterWhileBusy: busySteer}).clone().EnterWhileBusy; got != busySteer {
		t.Errorf("clone lost the setting: %q.\nConsequence: any later edit of behaviour silently resets Enter to queue.\nRemedy: clone copies EnterWhileBusy.", got)
	}
}
