package main

import (
	"path/filepath"
	"strings"
	"testing"
)

// inTriggerDir points the trigger store at a disposable folder.
func inTriggerDir(t *testing.T) {
	t.Helper()
	old := triggerDir
	triggerDir = filepath.Join(t.TempDir(), "triggers")
	t.Cleanup(func() { triggerDir = old })
}

// The whole story a screen needs, over the protocol and with no command line:
// create a trigger, see it listed with its next firing, pause it.
func TestTriggersCanBeCreatedListedAndPausedOverTheProtocol(t *testing.T) {
	inTriggerDir(t)

	if l := wireOK(t, `{"id":"1","type":"trigger.list"}`); len(l["triggers"].([]any)) != 0 {
		t.Fatalf("a fresh folder lists %v", l)
	}

	created := wireOK(t, `{"id":"2","type":"trigger.create","params":{"name":"nightly","on":"every:30m","then":"run start duo 'audit' --sim","budget":2,"budget_period":"day"}}`)
	rec := created["record"].(map[string]any)
	if rec["name"] != "nightly" || rec["status"] != "active" || rec["on_missed"] != "skip" || rec["overlap"] != "skip" {
		t.Fatalf("created = %v", created)
	}
	if created["next"] == nil || created["next"] == "" {
		t.Errorf("an every: trigger must report its next firing: %v", created)
	}

	l := wireOK(t, `{"id":"3","type":"trigger.list"}`)["triggers"].([]any)
	if len(l) != 1 || l[0].(map[string]any)["record"].(map[string]any)["name"] != "nightly" {
		t.Fatalf("listed %v", l)
	}

	paused := wireOK(t, `{"id":"4","type":"trigger.pause","params":{"name":"nightly"}}`)
	if paused["record"].(map[string]any)["status"] != "paused" || paused["next_absent"] != "paused" {
		t.Fatalf("paused = %v", paused)
	}
	// Pausing twice is not an error: a screen may be showing a stale row.
	wireOK(t, `{"id":"5","type":"trigger.pause","params":{"name":"nightly"}}`)

	// Resuming brings it back with a next firing again; resuming twice is fine too.
	resumed := wireOK(t, `{"id":"6","type":"trigger.resume","params":{"name":"nightly"}}`)
	if resumed["record"].(map[string]any)["status"] != "active" || resumed["next"] == nil || resumed["next"] == "" {
		t.Fatalf("resumed = %v", resumed)
	}
	wireOK(t, `{"id":"7","type":"trigger.resume","params":{"name":"nightly"}}`)
	wireRefused(t, `{"id":"8","type":"trigger.resume","params":{"name":"nope"}}`)
}

func TestTriggerCreateRefusalsComeFromTheSameValidationAsTheCLI(t *testing.T) {
	inTriggerDir(t)
	ok := `"name":"t","on":"every:1h","then":"run start duo 'x' --sim","budget":1,"budget_period":"day"`
	wireOK(t, `{"id":"0","type":"trigger.create","params":{`+ok+`}}`)

	for name, tc := range map[string]struct{ params, want string }{
		"no budget":      {`"name":"a","on":"every:1h","then":"run start duo 'x'","budget_period":"day"`, "spend ceiling"},
		"zero budget":    {`"name":"a","on":"every:1h","then":"run start duo 'x'","budget":0,"budget_period":"day"`, "spend ceiling"},
		"bad cron":       {`"name":"a","on":"cron:0 3 30 2 *","then":"run start duo 'x'","budget":1,"budget_period":"day"`, ""},
		"too frequent":   {`"name":"a","on":"every:10s","then":"run start duo 'x'","budget":1,"budget_period":"day"`, ""},
		"not an action":  {`"name":"a","on":"every:1h","then":"inbox approve x","budget":1,"budget_period":"day"`, ""},
		"no period":      {`"name":"a","on":"every:1h","then":"run start duo 'x'","budget":1`, ""},
		"duplicate name": {ok, ""},
	} {
		msg := wireRefused(t, `{"id":"9","type":"trigger.create","params":{`+tc.params+`}}`)
		if msg == "" || (tc.want != "" && !strings.Contains(msg, tc.want)) {
			t.Errorf("%s: refusal = %q, want it to mention %q", name, msg, tc.want)
		}
	}
	if l := wireOK(t, `{"id":"10","type":"trigger.list"}`)["triggers"].([]any); len(l) != 1 {
		t.Errorf("a refused create must leave nothing behind, listed %d", len(l))
	}
}

func TestTriggerPauseOfAnUnknownNameIsRefused(t *testing.T) {
	inTriggerDir(t)
	if msg := wireRefused(t, `{"id":"1","type":"trigger.pause","params":{"name":"ghost"}}`); msg == "" {
		t.Fatal("expected a refusal with a reason")
	}
}
