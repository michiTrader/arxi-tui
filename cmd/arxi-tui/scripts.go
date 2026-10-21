package main

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/michiTrader/arxi_tui/internal/script"
)

// This file is where tier 2 (internal/script) meets the interface.
//
// A user script is a program in the strict JavaScript subset that the user's own keys,
// commands and hooks call with `do:script.run <name> [text]`. It has ONE door out: what
// `run(text, facts)` returns.
//
//	a text           shown as a note in the conversation
//	a list of texts  actions, each checked by validateActions exactly as the ones of a
//	                 shortcut or a command are, and queued the way those are
//	null             nothing
//
// So a script can compute which actions to take (a counter, a state machine, a choice by
// the effort level) and can do nothing a shortcut could not: it cannot start a typed verb,
// answer for the user, or reach a file, the network or the model. It cannot call another
// script either (`do:script.run` is refused in its result), so no definition can loop.
// The state a script keeps in its top-level `let`s lasts while the program runs and is
// lost when the script's source changes; it is never written to disk.

const (
	maxUserScripts = 16
	maxScriptBytes = 16 << 10
)

var scriptNameRe = regexp.MustCompile(`^[a-z][a-z0-9-]*$`)

// checkScript is the load-time check of one script: the name is plain, the source is not
// large, it parses, every name in it exists, its top level runs, and it has `run`.
func checkScript(name, src string) error {
	if len(name) > maxNameLen || !scriptNameRe.MatchString(name) {
		return fmt.Errorf("script name %q: use lowercase letters, digits and '-', starting with a letter (e.g. \"counter\")", name)
	}
	if strings.TrimSpace(src) == "" {
		return fmt.Errorf("script %q is empty; write `function run(text, facts) { ... }`, or send null to remove it", name)
	}
	if len(src) > maxScriptBytes {
		return fmt.Errorf("script %q is %d bytes, the most is %d; split it in two scripts", name, len(src), maxScriptBytes)
	}
	s, err := script.Load(src, script.Config{Name: name + ".js"})
	if err != nil {
		return fmt.Errorf("script %q: %w", name, err)
	}
	if !s.Has("run") {
		return fmt.Errorf("script %q has no `run`; write `function run(text, facts) { return 'a note' }` (it may return a list of actions such as ['cmd:/effort max'])", name)
	}
	return nil
}

// scriptHost keeps the loaded scripts and the state they hold between calls.
type scriptHost struct {
	loaded map[string]loadedScript
}

type loadedScript struct {
	sum [32]byte
	s   *script.Script
}

func newScriptHost() *scriptHost { return &scriptHost{loaded: map[string]loadedScript{}} }

// scriptResult is what one call gave back.
type scriptResult struct {
	note    string
	actions actionList
}

// call runs `run(text, facts)` of the named script of b. A script whose source changed
// since the last call starts again from its top level.
func (h *scriptHost) call(b behaviour, name, text string, facts map[string]any, fromHook bool) (scriptResult, error) {
	src, ok := b.Scripts[name]
	if !ok {
		have := sortedKeys(b.Scripts)
		if len(have) == 0 {
			return scriptResult{}, fmt.Errorf("script.run: there is no script %q; none is defined (behaviour.json, \"scripts\": {\"name\": \"function run(text, facts) {...}\"})", name)
		}
		return scriptResult{}, fmt.Errorf("script.run: there is no script %q; the scripts are %s", name, strings.Join(have, ", "))
	}
	sum := sha256.Sum256([]byte(src))
	ls, ok := h.loaded[name]
	if !ok || ls.sum != sum {
		s, err := script.Load(src, script.Config{Name: name + ".js"})
		if err != nil {
			return scriptResult{}, fmt.Errorf("script.run %s: %w", name, err)
		}
		ls = loadedScript{sum: sum, s: s}
		h.loaded[name] = ls
	}
	out, err := ls.s.Call("run", text, facts)
	if err != nil {
		var se *script.Error
		if errors.As(err, &se) {
			return scriptResult{}, fmt.Errorf("script.run %s: %w", name, err)
		}
		return scriptResult{}, err
	}
	switch v := out.(type) {
	case nil:
		return scriptResult{}, nil
	case string:
		return scriptResult{note: v}, nil
	case []any:
		list := make(actionList, 0, len(v))
		for i, e := range v {
			a, ok := e.(string)
			if !ok {
				return scriptResult{}, fmt.Errorf("script.run %s: item %d of the returned list is not a text; return actions such as 'cmd:/effort max', each a text", name, i+1)
			}
			list = append(list, a)
		}
		if err := validateScriptActions(name, list, fromHook); err != nil {
			return scriptResult{}, err
		}
		return scriptResult{actions: list}, nil
	}
	return scriptResult{}, fmt.Errorf("script.run %s: `run` returned a value of a kind that is neither a text nor a list; return a text (a note), a list of actions, or null", name)
}

// validateScriptActions is the check of what a script asked to do. It is the one a
// shortcut's actions get (a hook's, when the call came from a hook), plus the refusal of
// another script.
func validateScriptActions(name string, list actionList, fromHook bool) error {
	for _, a := range list {
		if strings.HasPrefix(strings.TrimSpace(a), "do:script.run") {
			return fmt.Errorf("script %q returned %q; a script cannot start a script (it could never end). Remedy: do the work in one script, with functions", name, a)
		}
	}
	if err := validateActions("script "+name, list, !fromHook, ""); err != nil {
		return fmt.Errorf("%w (the list a script returns is checked like a shortcut's)", err)
	}
	return nil
}
