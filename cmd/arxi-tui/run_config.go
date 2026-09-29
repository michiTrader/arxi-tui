package main

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/michiTrader/arxi_tui/internal/driver"
)

// defaultActor is the blueprint a TUI session drives when ARXI_ACTOR is unset.
//
// The product decision is "plug and play": the TUI must start a real run with
// no configuration, so a default blueprint name is baked in rather than the
// host refusing an empty actor. It is a named constant, and it is the single
// line to change, because run.start resolves the actor through the core's
// agent store and a name the target kernel does not ship is rejected there --
// a fact only M2's live round-trip can confirm. The resolved value is surfaced
// in the status bar (resolveRunStartParams returns it as the label) precisely
// so a plug-and-play default is never invisible: the user always sees which
// agent is connected, and overrides it with ARXI_ACTOR when the default is
// wrong for their install.
const defaultActor = "default"

// defaultBudget is the per-turn USD budget requested when ARXI_BUDGET is unset.
//
// The core has no budget default and refuses a non-positive value, so the host
// must supply one. A conservative fixed default keeps a first live run bounded
// and cheap; the user raises it through ARXI_BUDGET when they trust the
// endpoint for more spend.
const defaultBudget = 1.0

// resolveRunStartParams builds the run.start parameters for a TUI session from
// the environment, applying the product decisions M1b flagged as the user's
// call: a plug-and-play default actor (overridable by ARXI_ACTOR and shown in
// the status bar), a conservative env-overridable budget, and a live model by
// default (sim is opt-in, not the default).
//
// getenv is injected rather than calling os.Getenv directly so the resolution
// -- which default applies, which override wins, which malformed value is
// refused -- is unit-testable without mutating process state. The returned
// actorLabel is the resolved actor name for the status bar; it equals
// p.Actor and is returned separately only to make the caller's intent (this
// string is for display) legible at the call site.
func resolveRunStartParams(getenv func(string) string) (p driver.RunStartParams, actorLabel string, err error) {
	actor := strings.TrimSpace(getenv("ARXI_ACTOR"))
	if actor == "" {
		actor = defaultActor
	}
	p.Actor = actor

	p.Budget = defaultBudget
	if raw := strings.TrimSpace(getenv("ARXI_BUDGET")); raw != "" {
		b, convErr := strconv.ParseFloat(raw, 64)
		if convErr != nil {
			// A budget the host cannot parse must not silently fall back to the
			// default: the user set ARXI_BUDGET on purpose, and starting a real
			// run (sim is off by default) at a budget they did not choose spends
			// their money against a number they never wrote. Refuse with the
			// bad value named so the fix is obvious.
			return driver.RunStartParams{}, "", fmt.Errorf(
				"cmd/arxi-tui/run_config.go: ARXI_BUDGET=%q is not a number; "+
					"remedy: set it to a positive USD amount (e.g. ARXI_BUDGET=1.0) or unset it to use the %.2f default",
				raw, defaultBudget)
		}
		if b <= 0 {
			// The core refuses a non-positive budget with bad_params; catching it
			// here names ARXI_BUDGET as the source, which the core's refusal
			// cannot, so the user edits the right knob instead of the blueprint.
			return driver.RunStartParams{}, "", fmt.Errorf(
				"cmd/arxi-tui/run_config.go: ARXI_BUDGET=%q is not positive; "+
					"remedy: a run.start budget must be > 0 (the core refuses 0 or less), so set ARXI_BUDGET to a positive USD amount",
				raw)
		}
		p.Budget = b
	}

	// Sim defaults to false: the product decision is a live model by default, so
	// omission of ARXI_SIM means a real run. ARXI_SIM turns the simulator on
	// (and back off), and an unrecognised value is refused rather than guessed,
	// because "did I ask for the simulator or a live model" is the difference
	// between a free dry run and real spend.
	if raw := strings.TrimSpace(getenv("ARXI_SIM")); raw != "" {
		s, convErr := parseSim(raw)
		if convErr != nil {
			return driver.RunStartParams{}, "", convErr
		}
		p.Sim = s
	}

	// Model is an optional override of the actor's configured model; empty means
	// "use the blueprint's model", which is the plug-and-play default.
	p.Model = strings.TrimSpace(getenv("ARXI_MODEL"))

	return p, p.Actor, nil
}

// parseSim reads ARXI_SIM as a boolean, accepting the spellings a user actually
// types (case-insensitive true/false/1/0/yes/no/on/off) and refusing anything
// else by name. strconv.ParseBool alone rejects yes/no/on/off, which are the
// natural words for a mode flag, so they are handled explicitly.
func parseSim(raw string) (bool, error) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "1", "true", "yes", "on":
		return true, nil
	case "0", "false", "no", "off":
		return false, nil
	default:
		return false, fmt.Errorf(
			"cmd/arxi-tui/run_config.go: ARXI_SIM=%q is not a boolean; "+
				"remedy: set it to true/false (or 1/0, yes/no, on/off), or unset it to run against a live model",
			raw)
	}
}
