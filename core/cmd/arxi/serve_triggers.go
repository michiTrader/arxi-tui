package main

import (
	"errors"
	"fmt"
	"time"

	"github.com/michiTrader/arxi/internal/trigger"
	"github.com/michiTrader/arxi/internal/trigstore"
)

// This file is what a person does to triggers over the protocol, so a screen can
// list, create and pause them with no command line. Each handler is the CLI's
// sequence minus os.Exit: a refusal is an error answered on the connection, which
// stays up, and it comes from the same validation the CLI uses (Record.Validate),
// so a trigger written here is one `trigger list` shows.
//
// Creating and pausing are Protocol without anything new being offered to an
// agent: both were already AgentTool commands, and the registry is unchanged.

// readTriggers opens the trigger store the way `trigger list` does (opening it
// makes the empty folder, which is all a listing on a fresh project leaves behind).
func readTriggers() (*trigstore.Store, error) {
	return trigstore.Open(triggerDir)
}

// handleTriggerList answers `trigger.list`.
func handleTriggerList(map[string]any) (any, error) {
	st, err := readTriggers()
	if err != nil {
		return nil, err
	}
	rs, err := st.List()
	if err != nil {
		return nil, err
	}
	return listPayload(rs, nowFunc()), nil
}

// handleTriggerCreate answers `trigger.create`.
func handleTriggerCreate(params map[string]any) (any, error) {
	budget := numParam(params, "budget")
	if budget <= 0 {
		return nil, errors.New("a trigger needs a spend ceiling above zero per period: without one it is an open subscription to the provider's bill")
	}
	now := nowFunc()
	r := trigger.Record{
		Name:         stringParam(params, "name"),
		On:           stringParam(params, "on"),
		Then:         stringParam(params, "then"),
		Budget:       budget,
		BudgetPeriod: trigger.Period(stringParam(params, "budget_period")),
		OnMissed:     trigger.OnMissed(orDefault(stringParam(params, "on_missed"), "skip")),
		Overlap:      trigger.Overlap(orDefault(stringParam(params, "overlap"), "skip")),
		Status:       trigger.StatusActive,
		CreatedAt:    now.Format(time.RFC3339),
	}
	if err := r.Validate(); err != nil {
		return nil, err
	}
	r.ID = r.Identity()
	st, err := trigstore.Open(triggerDir)
	if err != nil {
		return nil, err
	}
	if err := st.Create(r); err != nil {
		return nil, err
	}
	return showPayload(r, now), nil
}

// handleTriggerPause answers `trigger.pause`.
func handleTriggerPause(params map[string]any) (any, error) {
	st, err := readTriggers()
	if err != nil {
		return nil, err
	}
	r, err := st.Load(stringParam(params, "name"))
	if err != nil {
		return nil, err
	}
	if r.Status != trigger.StatusPaused {
		r.Status = trigger.StatusPaused
		if err := st.Save(r); err != nil {
			return nil, fmt.Errorf("pausing %s: %w", r.Name, err)
		}
	}
	return showPayload(r, nowFunc()), nil
}

func orDefault(v, def string) string {
	if v == "" {
		return def
	}
	return v
}
