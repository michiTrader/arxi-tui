package main

import (
	"errors"
	"fmt"
	"strings"

	"github.com/michiTrader/arxi/internal/agentstore"
	"github.com/michiTrader/arxi/internal/kernel"
)

// This file is what a person does to agents and teams over the protocol, so a
// screen can list, create and compose them with no command line. Each handler is
// the CLI's sequence minus os.Exit: a refusal is an error answered on the
// connection, which stays up. The refusals themselves come from the same store
// the CLI writes through (agentstore.Record.Validate, Team.Validate), so a file
// written here is a file `arxi blueprint validate` accepts.
//
// Like provider.add, these are Protocol without being offered to the agent loop
// as write tools for teams: composing the team that will run an agent is the
// owner's decision.

// handleAgentList answers `agent.list`: every stored agent and team, a file that
// does not load carried with its reason instead of dropped.
func handleAgentList(map[string]any) (any, error) {
	entries, err := readAgents().List()
	if err != nil {
		return nil, err
	}
	out := make([]agentJSON, 0, len(entries))
	for _, e := range entries {
		out = append(out, agentPayload(e, false))
	}
	// Wrapped, like provider.list, so the document can grow a field without
	// breaking a client that reads a bare array.
	return struct {
		Agents []agentJSON `json:"agents"`
	}{Agents: out}, nil
}

// handleAgentCreate answers `agent.create`.
func handleAgentCreate(params map[string]any) (any, error) {
	r := agentstore.Record{
		Name:     stringParam(params, "name"),
		Model:    stringParam(params, "model"),
		Role:     stringParam(params, "role"),
		Tools:    splitCSV(stringParam(params, "tools")),
		Advisory: boolParam(params, "advisory"),
	}
	// The role fills what was left blank, BEFORE Validate, so the record that is
	// checked is the record that is written (the same order the CLI keeps).
	rd, err := applyRoleErr(&r, r.Role)
	if err != nil {
		return nil, fmt.Errorf("no agent was written: its role names a file that does not load: %w", err)
	}
	if err := r.Validate(); err != nil {
		return nil, err
	}
	path, err := openAgents().Create(r)
	if err != nil {
		if errors.Is(err, agentstore.ErrExists) {
			return nil, fmt.Errorf("%q already exists; nothing was written. choose another name: `agent create` never overwrites", r.Name)
		}
		return nil, err
	}
	return struct {
		Name     string   `json:"name"`
		Path     string   `json:"path"`
		Tools    []string `json:"tools,omitempty"`
		Advisory bool     `json:"advisory,omitempty"`
		RoleNote string   `json:"role_note,omitempty"`
	}{Name: r.Name, Path: path, Tools: r.Tools, Advisory: r.Advisory, RoleNote: roleNote(rd)}, nil
}

// roleNote says in one line what a role contributed, or that it is not defined.
// An undefined role is a note and not a refusal (see applyRole): `role:` is a
// free-form label, so the caller must be told it supplied nothing.
func roleNote(rd roleDefaults) string {
	switch {
	case rd.name == "":
		return ""
	case !rd.found:
		return fmt.Sprintf("role %q is not defined, so it supplied no defaults", rd.name)
	case rd.tools && rd.advisory:
		return fmt.Sprintf("role %q supplied the tools and the advisory trait", rd.name)
	case rd.tools:
		return fmt.Sprintf("role %q supplied the tools", rd.name)
	case rd.advisory:
		return fmt.Sprintf("role %q made this agent advisory", rd.name)
	}
	return ""
}

// composeMembers copies each named agent's single member out of the store, the
// error-returning twin of resolveMembers. Every refusal is the CLI's: a missing
// agent, a file that does not load, an agent with no member, a team given where
// an agent belongs, and two members with one name.
func composeMembers(names []string) ([]kernel.MemberConfig, error) {
	st := readAgents()
	var out []kernel.MemberConfig
	seen := map[string]string{}
	for _, name := range names {
		bp, err := st.Load(name)
		if err != nil {
			if errors.Is(err, agentstore.ErrNotExist) {
				return nil, fmt.Errorf("member %q: there is no such agent. a team is composed from agents that exist", name)
			}
			return nil, fmt.Errorf("member %q did not load, so it cannot be copied: %w", name, err)
		}
		ms := bp.Config.Members
		switch {
		case len(ms) == 0:
			return nil, fmt.Errorf("member %q has no members of its own, so there is nothing to copy out of it", name)
		case len(ms) > 1:
			who := make([]string, 0, len(ms))
			for _, m := range ms {
				who = append(who, m.Name)
			}
			return nil, fmt.Errorf("member %q is itself a team of %d (%s); name its members instead",
				name, len(ms), strings.Join(who, ", "))
		}
		m := ms[0]
		if prev, dup := seen[m.Name]; dup {
			return nil, fmt.Errorf("two members would be named %q (%s and %s); a run could not address them apart", m.Name, prev, name)
		}
		seen[m.Name] = name
		out = append(out, m)
	}
	return out, nil
}

// handleBlueprintCreate answers `blueprint.create`: a team composed from stored
// agents. Stage names only; timeouts and advance rules are decisions about the
// team's process that no member can supply, so the file gets the default (all).
func handleBlueprintCreate(params map[string]any) (any, error) {
	names := splitCSV(stringParam(params, "members"))
	if len(names) == 0 {
		return nil, errors.New("a team needs at least one member")
	}
	members, err := composeMembers(names)
	if err != nil {
		return nil, err
	}
	t := agentstore.Team{
		Name:    stringParam(params, "name"),
		Members: members,
		Stages:  splitCSV(stringParam(params, "stages")),
	}
	if err := t.Validate(); err != nil {
		return nil, err
	}
	path, err := openAgents().CreateTeam(t)
	if err != nil {
		if errors.Is(err, agentstore.ErrExists) {
			return nil, fmt.Errorf("%q already exists; nothing was written. an agent and a team share one file name", t.Name)
		}
		return nil, err
	}
	stages := t.Stages
	if len(stages) == 0 {
		stages = []string{"work"}
	}
	return struct {
		Name    string   `json:"name"`
		Path    string   `json:"path"`
		Members []string `json:"members"`
		Stages  []string `json:"stages"`
	}{Name: t.Name, Path: path, Members: names, Stages: stages}, nil
}

// stageEditFrom reads the three optional rule fields. A timeout that is absent is
// "leave it" and a timeout of 0 is "remove it", so presence is what is checked.
func stageEditFrom(stage, advance, onTimeout string, timeout *int64) agentstore.StageEdit {
	return agentstore.StageEdit{Stage: stage, AdvanceWhen: advance, TimeoutMs: timeout, OnTimeout: onTimeout}
}

// handleBlueprintStage answers `blueprint.stage`: new rules for one stage of a
// stored blueprint. The file is validated before it is replaced, so a refusal
// (a quorum bigger than the team, a rule that does not exist) leaves it as it was.
func handleBlueprintStage(params map[string]any) (any, error) {
	name := stringParam(params, "name")
	var timeout *int64
	if raw, ok := params["timeout_ms"]; ok && raw != nil {
		f, isNum := raw.(float64)
		if !isNum || f != float64(int64(f)) {
			return nil, errors.New("timeout_ms must be a whole number of milliseconds")
		}
		n := int64(f)
		timeout = &n
	}
	e := stageEditFrom(stringParam(params, "stage"), stringParam(params, "advance_when"),
		stringParam(params, "on_timeout"), timeout)
	st := readAgents()
	if err := st.SetStage(name, e); err != nil {
		return nil, err
	}
	return blueprintStagesPayload(st, name)
}

// blueprintStagesPayload is the answer: the stages as they are on disk now, so a
// client shows what was saved rather than what it asked for.
func blueprintStagesPayload(st *agentstore.Store, name string) (any, error) {
	bp, err := st.Load(name)
	if err != nil {
		return nil, err
	}
	type stageOut struct {
		Name        string `json:"name"`
		AdvanceWhen string `json:"advance_when"`
		OnTimeout   string `json:"on_timeout"`
		TimeoutMs   int64  `json:"timeout_ms,omitempty"`
	}
	out := struct {
		Name   string     `json:"name"`
		Stages []stageOut `json:"stages"`
	}{Name: name, Stages: []stageOut{}}
	for _, s := range bp.Config.Stages {
		out.Stages = append(out.Stages, stageOut{Name: s.Name, AdvanceWhen: s.AdvanceWhen,
			OnTimeout: s.OnTimeout, TimeoutMs: s.TimeoutMs})
	}
	return out, nil
}

// memberEditFrom builds the edit from what a caller said. Presence is the signal:
// a parameter that is absent leaves the field alone and one that is there, even
// empty, replaces it.
func memberEditFrom(member string, model, role, tools *string, advisory *bool) agentstore.MemberEdit {
	e := agentstore.MemberEdit{Member: member, Model: model, Role: role, Advisory: advisory}
	if tools != nil {
		list := splitCSV(*tools)
		e.Tools = &list
	}
	return e
}

// optString reads a string parameter that may be absent.
func optString(params map[string]any, name string) *string {
	raw, ok := params[name]
	if !ok || raw == nil {
		return nil
	}
	s, _ := raw.(string)
	return &s
}

// handleBlueprintMember answers `blueprint.member`: new model, role, tools or
// advisory flag for one member of a stored blueprint, validated before it replaces
// the file.
func handleBlueprintMember(params map[string]any) (any, error) {
	var advisory *bool
	if raw, ok := params["advisory"]; ok && raw != nil {
		b, isBool := raw.(bool)
		if !isBool {
			return nil, errors.New("advisory must be true or false")
		}
		advisory = &b
	}
	for _, k := range []string{"model", "role", "tools"} {
		if raw, ok := params[k]; ok && raw != nil {
			if _, isStr := raw.(string); !isStr {
				return nil, fmt.Errorf("%s must be text", k)
			}
		}
	}
	name := stringParam(params, "name")
	e := memberEditFrom(stringParam(params, "member"),
		optString(params, "model"), optString(params, "role"), optString(params, "tools"), advisory)
	st := readAgents()
	if err := st.SetMember(name, e); err != nil {
		return nil, err
	}
	bp, err := st.Load(name)
	if err != nil {
		return nil, err
	}
	type memberOut struct {
		Name     string   `json:"name"`
		Role     string   `json:"role,omitempty"`
		Model    string   `json:"model,omitempty"`
		Tools    []string `json:"tools,omitempty"`
		Advisory bool     `json:"advisory,omitempty"`
		Stages   []string `json:"stages,omitempty"`
	}
	out := struct {
		Name    string      `json:"name"`
		Members []memberOut `json:"members"`
	}{Name: name, Members: []memberOut{}}
	for _, m := range bp.Config.Members {
		out.Members = append(out.Members, memberOut{Name: m.Name, Role: m.Role, Model: m.Model,
			Tools: m.Tools, Advisory: m.Advisory, Stages: m.Stages})
	}
	return out, nil
}

// handleBlueprintWatch answers `blueprint.watch`: add, replace or remove one watcher
// of a stored blueprint, validated before it replaces the file. The answer is the
// watchers as they are on disk now.
func handleBlueprintWatch(params map[string]any) (any, error) {
	remove := false
	if raw, ok := params["remove"]; ok && raw != nil {
		b, isBool := raw.(bool)
		if !isBool {
			return nil, errors.New("remove must be true or false")
		}
		remove = b
	}
	name := stringParam(params, "name")
	st := readAgents()
	if err := st.SetWatcher(name, agentstore.WatchEdit{Agent: stringParam(params, "agent"),
		Pattern: stringParam(params, "pattern"), Action: stringParam(params, "action"),
		Tool: stringParam(params, "tool"), Remove: remove}); err != nil {
		return nil, err
	}
	bp, err := st.Load(name)
	if err != nil {
		return nil, err
	}
	type watchOut struct {
		Agent   string `json:"agent"`
		Pattern string `json:"pattern"`
		Action  string `json:"action,omitempty"`
		Tool    string `json:"tool,omitempty"`
	}
	out := struct {
		Name     string     `json:"name"`
		Watchers []watchOut `json:"watchers"`
	}{Name: name, Watchers: []watchOut{}}
	for _, w := range bp.Config.Watchers {
		out.Watchers = append(out.Watchers, watchOut{Agent: w.Agent, Pattern: w.Pattern, Action: w.Action, Tool: w.Tool})
	}
	return out, nil
}
