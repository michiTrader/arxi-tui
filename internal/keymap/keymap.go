// Package keymap is the keyboard as data (docs/DESIGN-OPEN-TUI.md section 5.3).
//
// A binding says which key, in which situation, runs which action:
//
//	{ "key": "ctrl+f", "when": "!menu.open && !key.user_bound", "do": "do:find.toggle" }
//
// The situation (`when`) is a tier-1 expression (internal/expr) over names the host publishes,
// and the action (`do`) is the same closed grammar a button carries (scene.ParseAction), so a
// key can do nothing a button could not. The first binding of a key whose `when` holds wins,
// in file order; a binding with no `when` always holds.
//
// The package knows nothing about the screen. The host gives the names (a schema) and says,
// when a key is pressed, what each name is right now. What the chain of Go code used to decide
// implicitly by where it sat ("not while a consent screen is open") is written here as a
// condition anybody can read, test and change.
//
// Ctrl-C is not in any keymap and cannot be put in one: it is the way out of the program
// (invariant 6) and stays in the core.
package keymap

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/michiTrader/arxi_tui/internal/expr"
	"github.com/michiTrader/arxi_tui/internal/term"
)

// Limits: far above any keymap a person writes, far below one that slows a key press.
const (
	MaxBindings = 256
	MaxDoRunes  = 400
)

// Binding is one line of a keymap.
type Binding struct {
	Key  string `json:"key"`
	When string `json:"when,omitempty"`
	Do   string `json:"do"`
}

type file struct {
	Version  int       `json:"version"`
	Bindings []Binding `json:"bindings"`
}

type compiled struct {
	Binding
	prog *expr.Program // nil when there is no condition
}

// Map is a checked keymap. It is immutable and safe to share.
type Map struct {
	all   []compiled
	byKey map[string][]int // canonical key -> indexes into all, in file order
}

// CheckDo is the host's own check of an action, run on every binding at load. The grammar
// is scene.ParseAction's; what a key may not carry (answering the agent for the user,
// starting what only a typed line may start) is the host's rule, so the host says it.
type CheckDo func(do string) error

// Parse reads a keymap and checks all of it: key spellings, conditions against schema,
// actions with check, and that no binding is hidden behind an earlier one. The first problem
// is returned, with the binding's place and what to write instead.
func Parse(data []byte, schema expr.Schema, check CheckDo) (*Map, error) {
	var f file
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&f); err != nil {
		return nil, fmt.Errorf("keymap: %v (the file is {\"version\":1,\"bindings\":[{\"key\":\"ctrl+o\",\"when\":\"...\",\"do\":\"do:view.expand\"}]}; `when` may be left out)", err)
	}
	if f.Version != 1 {
		return nil, fmt.Errorf("keymap: version is %d; write \"version\": 1, the only one there is", f.Version)
	}
	if len(f.Bindings) == 0 {
		return nil, fmt.Errorf("keymap: no bindings; a keymap with none does nothing, so write at least one or leave the file out")
	}
	if len(f.Bindings) > MaxBindings {
		return nil, fmt.Errorf("keymap: %d bindings, more than the %d allowed; split the keymap or drop the ones nobody uses", len(f.Bindings), MaxBindings)
	}
	m := &Map{byKey: map[string][]int{}}
	for i, b := range f.Bindings {
		c, err := compile(i+1, b, schema, check)
		if err != nil {
			return nil, err
		}
		for _, j := range m.byKey[c.Key] {
			prev := m.all[j]
			switch {
			case prev.When == "":
				return nil, fmt.Errorf("keymap: binding %d (%s) can never run: binding %d takes %s first and has no `when`, so it always wins; give binding %d a `when`, or put this one before it", i+1, b.Key, j+1, b.Key, j+1)
			case strings.TrimSpace(prev.When) == strings.TrimSpace(c.When):
				return nil, fmt.Errorf("keymap: binding %d (%s) can never run: binding %d has the same `when` (%s) and wins first; change the condition or remove one", i+1, b.Key, j+1, c.When)
			}
		}
		m.byKey[c.Key] = append(m.byKey[c.Key], len(m.all))
		m.all = append(m.all, c)
	}
	return m, nil
}

func compile(n int, b Binding, schema expr.Schema, check CheckDo) (compiled, error) {
	at := fmt.Sprintf("keymap: binding %d", n)
	k, ok := term.ParseKey(strings.ToLower(strings.TrimSpace(b.Key)))
	if !ok {
		return compiled{}, fmt.Errorf("%s: %q is not a key name; write names like \"ctrl+o\", \"f5\", \"alt+left\", \"down\"", at, b.Key)
	}
	if k.Type == term.KeyRunes && k.Mod&term.ModCtrl != 0 && len(k.Runes) == 1 && (k.Runes[0] == 'c' || k.Runes[0] == 'C') {
		return compiled{}, fmt.Errorf("%s: ctrl+c is the way out of the program and no keymap can take it; choose another key", at)
	}
	if k.Type == term.KeyRunes && k.Mod&(term.ModCtrl|term.ModAlt) == 0 && k.String() == string(k.Runes) {
		return compiled{}, fmt.Errorf("%s: %q is a letter you type with; a binding is a named key (\"down\", \"f5\") or a chord (\"ctrl+g\", \"alt+e\"), or typing would stop working", at, b.Key)
	}
	b.Key = k.String()
	if strings.TrimSpace(b.Do) == "" {
		return compiled{}, fmt.Errorf("%s (%s): no `do`; write an action such as \"do:view.expand\" or \"cmd:/effort max\"", at, b.Key)
	}
	if utf8.RuneCountInString(b.Do) > MaxDoRunes {
		return compiled{}, fmt.Errorf("%s (%s): `do` is longer than %d characters", at, b.Key, MaxDoRunes)
	}
	if check != nil {
		if err := check(b.Do); err != nil {
			return compiled{}, fmt.Errorf("%s (%s): %w", at, b.Key, err)
		}
	}
	c := compiled{Binding: b}
	if strings.TrimSpace(b.When) != "" {
		p, err := expr.Compile(b.When, schema, expr.Bool)
		if err != nil {
			return compiled{}, fmt.Errorf("%s (%s): `when` %q: %w", at, b.Key, b.When, err)
		}
		c.prog = p
	} else {
		c.When = ""
	}
	return c, nil
}

// Resolve finds what a pressed key runs: the first binding of that key whose condition
// holds. ok is false when none does (the key is not the keymap's right now). An error means
// a condition could not be decided, and says which binding and why; the host shows it and
// treats the key as not taken, so a broken name never silently swallows a key.
func (m *Map) Resolve(k term.Key, get expr.Lookup) (b Binding, ok bool, err error) {
	if m == nil {
		return Binding{}, false, nil
	}
	for _, i := range m.byKey[k.String()] {
		c := m.all[i]
		if c.prog == nil {
			return c.Binding, true, nil
		}
		held, e := c.prog.EvalBool(get)
		if e != nil {
			return Binding{}, false, fmt.Errorf("keymap: binding %d (%s) could not be decided: %w", i+1, c.Key, e)
		}
		if held {
			return c.Binding, true, nil
		}
	}
	return Binding{}, false, nil
}

// Takes says whether any binding names the key, whatever its condition: the question a key
// list or a "may a shortcut use this key" check asks.
func (m *Map) Takes(k term.Key) bool {
	return m != nil && len(m.byKey[k.String()]) > 0
}

// Bindings returns the keymap in file order, as a copy.
func (m *Map) Bindings() []Binding {
	if m == nil {
		return nil
	}
	out := make([]Binding, len(m.all))
	for i, c := range m.all {
		out[i] = c.Binding
	}
	return out
}

// Keys returns each key the keymap names once, in order of first appearance.
func (m *Map) Keys() []string {
	if m == nil {
		return nil
	}
	var out []string
	seen := map[string]bool{}
	for _, c := range m.all {
		if !seen[c.Key] {
			seen[c.Key] = true
			out = append(out, c.Key)
		}
	}
	return out
}

// Names returns the names the conditions read, to be checked against what the host publishes.
func (m *Map) Names() []string {
	if m == nil {
		return nil
	}
	seen := map[string]bool{}
	var out []string
	for _, c := range m.all {
		if c.prog == nil {
			continue
		}
		for _, n := range c.prog.Names() {
			if !seen[n] {
				seen[n] = true
				out = append(out, n)
			}
		}
	}
	return out
}
