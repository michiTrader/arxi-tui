package main

import (
	"strings"
	"testing"
)

// envMap turns a map into the getenv function resolveRunStartParams takes, so a
// test states the environment as data and never touches os.Setenv (which would
// make these tests order-dependent and unsafe to parallelise).
func envMap(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

// TestResolveDefaultsAreThePlugAndPlaySession pins the zero-config path: with no
// ARXI_* set, the TUI must still start a real run, so the actor is the baked
// default, the budget is the conservative default, and sim is off (a live
// model). If any of these silently changed, a user who set nothing would get a
// different agent, a different spend, or a dry run they did not ask for.
func TestResolveDefaultsAreThePlugAndPlaySession(t *testing.T) {
	p, label, err := resolveRunStartParams(envMap(nil))
	if err != nil {
		t.Fatalf("the zero-config path refused to resolve: %v. "+
			"remedy: plug-and-play means an empty environment yields a valid run.start, never an error", err)
	}
	if p.Actor != defaultActor {
		t.Errorf("actor = %q, want the baked default %q. "+
			"remedy: an unset ARXI_ACTOR must fall back to defaultActor so the session starts with no config", p.Actor, defaultActor)
	}
	if label != defaultActor {
		t.Errorf("status-bar label = %q, want %q. "+
			"remedy: the label must equal the resolved actor so the connected agent is visible even when it is the default", label, defaultActor)
	}
	if p.Budget != defaultBudget {
		t.Errorf("budget = %v, want the default %v. "+
			"remedy: an unset ARXI_BUDGET must use defaultBudget; the core has no budget default and refuses a zero", p.Budget, defaultBudget)
	}
	if p.Sim {
		t.Error("sim = true with ARXI_SIM unset. " +
			"remedy: the product decision is a live model by default; the simulator is opt-in, so an unset ARXI_SIM must leave Sim false")
	}
	if p.Model != "" {
		t.Errorf("model = %q, want empty. "+
			"remedy: an unset ARXI_MODEL must leave Model empty so the blueprint's own model is used", p.Model)
	}
}

// TestArxiActorOverridesTheDefault is the whole point of the override: a user
// whose kernel ships a different blueprint must be able to name it without a
// code change, and the status bar must then show that name, not the default.
func TestArxiActorOverridesTheDefault(t *testing.T) {
	p, label, err := resolveRunStartParams(envMap(map[string]string{"ARXI_ACTOR": "  feature-team  "}))
	if err != nil {
		t.Fatalf("resolving a set ARXI_ACTOR failed: %v", err)
	}
	if p.Actor != "feature-team" {
		t.Errorf("actor = %q, want %q (trimmed). "+
			"remedy: ARXI_ACTOR must win over defaultActor and be trimmed of surrounding whitespace", p.Actor, "feature-team")
	}
	if label != "feature-team" {
		t.Errorf("label = %q, want %q. "+
			"remedy: the status-bar label tracks the resolved actor, so an override must change what the user sees", label, "feature-team")
	}
}

// TestArxiBudgetOverridesTheDefault pins that a user can raise the spend cap
// through the environment.
func TestArxiBudgetOverridesTheDefault(t *testing.T) {
	p, _, err := resolveRunStartParams(envMap(map[string]string{"ARXI_BUDGET": "5.5"}))
	if err != nil {
		t.Fatalf("resolving a set ARXI_BUDGET failed: %v", err)
	}
	if p.Budget != 5.5 {
		t.Errorf("budget = %v, want 5.5. remedy: a parseable ARXI_BUDGET must replace the default", p.Budget)
	}
}

// TestArxiBudgetMalformedIsRefusedNotDefaulted is the money guard. A budget the
// host cannot parse must fail loud: silently using the default would start a
// live run (sim is off by default) at a number the user did not choose.
func TestArxiBudgetMalformedIsRefusedNotDefaulted(t *testing.T) {
	_, _, err := resolveRunStartParams(envMap(map[string]string{"ARXI_BUDGET": "cheap"}))
	if err == nil {
		t.Fatal("a malformed ARXI_BUDGET resolved without error. " +
			"remedy: an unparseable budget must be refused by name, never silently replaced by the default, because a live run would then spend against a number the user never wrote")
	}
	if !strings.Contains(err.Error(), "ARXI_BUDGET") || !strings.Contains(err.Error(), "cheap") {
		t.Errorf("the refusal does not name the variable and its bad value: %q. "+
			"remedy: name ARXI_BUDGET and the offending value so the fix is obvious", err.Error())
	}
}

// TestArxiBudgetNonPositiveIsRefused catches the value the core itself would
// reject, but here, where ARXI_BUDGET can be named as the source -- the core's
// bad_params refusal cannot point at the environment variable.
func TestArxiBudgetNonPositiveIsRefused(t *testing.T) {
	for _, raw := range []string{"0", "-1", "-0.5"} {
		_, _, err := resolveRunStartParams(envMap(map[string]string{"ARXI_BUDGET": raw}))
		if err == nil {
			t.Errorf("ARXI_BUDGET=%q resolved without error. "+
				"remedy: the core refuses a non-positive budget, so catch it here and name ARXI_BUDGET as the knob to fix", raw)
		}
	}
}

// TestArxiSimTurnsTheSimulatorOn pins the opt-in: only an explicit truthy
// ARXI_SIM makes Sim true, and the natural spellings a user types are accepted.
func TestArxiSimTurnsTheSimulatorOn(t *testing.T) {
	for _, raw := range []string{"1", "true", "TRUE", "yes", "On"} {
		p, _, err := resolveRunStartParams(envMap(map[string]string{"ARXI_SIM": raw}))
		if err != nil {
			t.Fatalf("ARXI_SIM=%q was refused: %v", raw, err)
		}
		if !p.Sim {
			t.Errorf("ARXI_SIM=%q left Sim false. "+
				"remedy: accept the truthy spellings a user actually types (1/true/yes/on, case-insensitive)", raw)
		}
	}
	for _, raw := range []string{"0", "false", "no", "OFF"} {
		p, _, err := resolveRunStartParams(envMap(map[string]string{"ARXI_SIM": raw}))
		if err != nil {
			t.Fatalf("ARXI_SIM=%q was refused: %v", raw, err)
		}
		if p.Sim {
			t.Errorf("ARXI_SIM=%q set Sim true. "+
				"remedy: the falsey spellings must keep the live-model default", raw)
		}
	}
}

// TestArxiSimGarbageIsRefused: an unrecognised ARXI_SIM must not be guessed,
// because the wrong guess is the difference between a free dry run and real
// spend.
func TestArxiSimGarbageIsRefused(t *testing.T) {
	_, _, err := resolveRunStartParams(envMap(map[string]string{"ARXI_SIM": "maybe"}))
	if err == nil {
		t.Fatal("ARXI_SIM=maybe resolved without error. " +
			"remedy: refuse an unrecognised mode by name rather than guessing whether the user wanted the simulator or a live model")
	}
	if !strings.Contains(err.Error(), "ARXI_SIM") {
		t.Errorf("the refusal does not name ARXI_SIM: %q", err.Error())
	}
}

// TestArxiModelOverridesTheBlueprintModel pins the optional model override.
func TestArxiModelOverridesTheBlueprintModel(t *testing.T) {
	p, _, err := resolveRunStartParams(envMap(map[string]string{"ARXI_MODEL": " deepseek-v4.1-flash "}))
	if err != nil {
		t.Fatalf("resolving a set ARXI_MODEL failed: %v", err)
	}
	if p.Model != "deepseek-v4.1-flash" {
		t.Errorf("model = %q, want %q (trimmed). "+
			"remedy: a set ARXI_MODEL overrides the blueprint's model and is trimmed", p.Model, "deepseek-v4.1-flash")
	}
}
