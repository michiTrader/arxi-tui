package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"unicode"
	"unicode/utf8"

	"github.com/michiTrader/arxi_tui/internal/fold"
	"github.com/michiTrader/arxi_tui/internal/scene"
	"github.com/michiTrader/arxi_tui/internal/term"
	"github.com/michiTrader/arxi_tui/internal/theme"
)

// This file is the behaviour half of the user's interface. The scene says what is drawn,
// the theme says what each token looks like, the texts say what the host writes; the
// behaviour says what the interface DOES: which tokens move, which keys steer a menu,
// which keys and commands the user added, and what happens when a setting changes.
//
// # Why it is data and not code
//
// A user who asks for "an animation when I pick effort max" or "arrows left and right in
// the effort menu" must be able to get it from their agent without a rebuild, and must be
// able to take it back with /ui undo. So behaviour is one small JSON document, kept in the
// settings folder beside scene.json, theme.json and texts.json, saved and undone with
// them. It is closed on purpose: every field has a validator, every action is one the
// scene's own button grammar already knows (scene.ParseAction), and nothing here runs
// code. A user who wants code writes a plugin; a plugin proposes and never writes.
//
// # The five parts
//
//	animations  name -> {colors, fps, spread, attrs}: animated palette tokens (theme.Cycle).
//	menu_keys   action -> keys: how the / effort, mode, style, resume and model menus move.
//	keys        key -> actions: shortcuts the user added.
//	commands    entries added to the / menu, each running a list of actions.
//	hooks       "when effort becomes max, do these actions".
//
// # What this layer can never do
//
// It cannot capture Ctrl-C (the escape hatch, invariant 6). It cannot take a key the
// editor needs to type (a bare letter, an arrow, Enter): a shortcut is an F-key or a
// Ctrl/Alt chord. It cannot approve anything for the user (answer: is refused), and a hook
// cannot route to a plugin (ext: is refused there) because a hook fires without a keypress
// of its own. A hook or a command never chains into another: what an action types is not
// itself matched against commands and hooks, so no definition can loop.

// ---- the document ---------------------------------------------------------------

// actionList is one or several actions. A single string is accepted where a list is
// expected, because that is how a person writes the common case.
type actionList []string

func (a *actionList) UnmarshalJSON(b []byte) error {
	var one string
	if err := json.Unmarshal(b, &one); err == nil {
		*a = actionList{one}
		return nil
	}
	var many []string
	if err := json.Unmarshal(b, &many); err != nil {
		return fmt.Errorf("an action is a string like \"cmd:/effort max\", or a list of them")
	}
	*a = many
	return nil
}

// userCommand is one entry the user added to the / menu.
type userCommand struct {
	Name        string     `json:"name"`
	Category    string     `json:"category,omitempty"`
	Description string     `json:"description,omitempty"`
	Run         actionList `json:"run"`
}

// userHook is a reaction: when setting `On` becomes `Is` (any value when Is is empty), Do.
type userHook struct {
	On string     `json:"on"`
	Is string     `json:"is,omitempty"`
	Do actionList `json:"do"`
}

// behaviour is the whole layer. The zero value is the factory behaviour.
type behaviour struct {
	Animations map[string]theme.CycleDef `json:"animations,omitempty"`
	MenuKeys   map[string][]string       `json:"menu_keys,omitempty"`
	Commands   []userCommand             `json:"commands,omitempty"`
	Keys       map[string]actionList     `json:"keys,omitempty"`
	Hooks      []userHook                `json:"hooks,omitempty"`
	// EnterWhileBusy is what Enter does with a line sent while the agent is working:
	// "queue" (the factory behaviour, kept as "" so a document that never touched it
	// stays empty) or "steer".
	EnterWhileBusy string `json:"enter_while_busy,omitempty"`
}

func (b behaviour) empty() bool {
	return len(b.Animations) == 0 && len(b.MenuKeys) == 0 && len(b.Commands) == 0 &&
		len(b.Keys) == 0 && len(b.Hooks) == 0 && b.EnterWhileBusy == ""
}

// clone copies every part, so an edit never reaches the layer it was drafted from.
func (b behaviour) clone() behaviour {
	out := behaviour{EnterWhileBusy: b.EnterWhileBusy}
	if len(b.Animations) > 0 {
		out.Animations = make(map[string]theme.CycleDef, len(b.Animations))
		for k, v := range b.Animations {
			out.Animations[k] = v
		}
	}
	if len(b.MenuKeys) > 0 {
		out.MenuKeys = make(map[string][]string, len(b.MenuKeys))
		for k, v := range b.MenuKeys {
			out.MenuKeys[k] = append([]string(nil), v...)
		}
	}
	if len(b.Keys) > 0 {
		out.Keys = make(map[string]actionList, len(b.Keys))
		for k, v := range b.Keys {
			out.Keys[k] = append(actionList(nil), v...)
		}
	}
	out.Commands = append([]userCommand(nil), b.Commands...)
	out.Hooks = append([]userHook(nil), b.Hooks...)
	return out
}

// Limits. They bound what one document may ask the host to hold or run; each is far above
// anything a person writes by hand and well below what would make the menu or the loop
// unusable.
const (
	maxAnimations  = 32
	maxUserKeys    = 64
	maxUserCmds    = 64
	maxUserHooks   = 32
	maxActionsEach = 16
	maxKeysPerNav  = 8
	maxNameLen     = 48
	maxDescLen     = 200
)

var (
	animNameRe = regexp.MustCompile(`^[a-z][a-z0-9._-]*$`)
	cmdNameRe  = regexp.MustCompile(`^[a-z][a-z0-9-]*$`)
)

// menuNavActions are the verbs a choice menu understands; the factory keys for the first four
// are the arrows, Enter and Esc.
var menuNavActions = []string{"prev", "next", "first", "last", "pick", "close"}

var navDefaults = map[string][]string{
	"prev":  {"up"},
	"next":  {"down"},
	"pick":  {"enter"},
	"close": {"esc"},
}

// hookEvents are the settings a hook can watch, with the values each takes.
func hookValues(event string) ([]string, bool) {
	switch event {
	case "effort":
		return append(append([]string(nil), effortLevelOrder...), "none"), true
	case "mode":
		var out []string
		for _, m := range agentModes {
			out = append(out, m.name)
		}
		return out, true
	case "style":
		var out []string
		for _, o := range promptStyles {
			out = append(out, o.name)
		}
		return out, true
	}
	return nil, false
}

var hookEventNames = []string{"effort", "mode", "style"}

// validate refuses a document the host could not honour. Every message names what was
// wrong and what to write instead, because the reader is a model repairing its own draft.
func (b behaviour) validate() error {
	if len(b.Animations) > maxAnimations {
		return fmt.Errorf("animations: %d defined, the most is %d; remove some (an animation set to null is removed)", len(b.Animations), maxAnimations)
	}
	for _, name := range sortedKeys(b.Animations) {
		if len(name) > maxNameLen || !animNameRe.MatchString(name) {
			return fmt.Errorf("animation name %q: use lowercase letters, digits, '.', '-' or '_', starting with a letter (e.g. \"rainbow\" or \"effort.max\")", name)
		}
		if _, err := theme.ParseCycle(name, b.Animations[name]); err != nil {
			return fmt.Errorf("animations: %w", err)
		}
	}
	if err := b.validateMenuKeys(); err != nil {
		return err
	}
	if b.EnterWhileBusy != "" && b.EnterWhileBusy != busySteer {
		return fmt.Errorf("enter_while_busy: %q is not a way to treat a line sent while the agent works; use \"queue\" (it waits for the answer, the factory way) or \"steer\" (it interrupts the turn and goes at once)", b.EnterWhileBusy)
	}
	if len(b.Keys) > maxUserKeys {
		return fmt.Errorf("keys: %d shortcuts, the most is %d", len(b.Keys), maxUserKeys)
	}
	seen := map[string]string{}
	for _, raw := range sortedKeys(b.Keys) {
		k, err := shortcutKey(raw)
		if err != nil {
			return err
		}
		if prev, dup := seen[k]; dup {
			return fmt.Errorf("keys: %q and %q are the same key (%s); keep one", prev, raw, k)
		}
		seen[k] = raw
		for _, act := range menuNavActions {
			for _, mk := range effectiveNav(b.MenuKeys, act) {
				if nk, err := menuKey(mk); err == nil && nk == k {
					return fmt.Errorf("keys: %s is also a menu key (%s); a key does one thing, so move one of them", k, act)
				}
			}
		}
		if err := validateActions("key "+raw, b.Keys[raw], true, ""); err != nil {
			return err
		}
	}
	if len(b.Commands) > maxUserCmds {
		return fmt.Errorf("commands: %d defined, the most is %d", len(b.Commands), maxUserCmds)
	}
	factory := map[string]bool{}
	for _, c := range fold.Commands {
		factory[c.Name] = true
	}
	names := map[string]bool{}
	for i, c := range b.Commands {
		if len(c.Name) > maxNameLen || !cmdNameRe.MatchString(c.Name) {
			return fmt.Errorf("commands[%d]: name %q: use lowercase letters, digits and '-', starting with a letter (it is typed as /%s)", i, c.Name, c.Name)
		}
		if factory[c.Name] {
			return fmt.Errorf("commands[%d]: /%s is already a command of the app; a command you add cannot take its place, so pick another name", i, c.Name)
		}
		if names[c.Name] {
			return fmt.Errorf("commands: /%s is defined twice; keep one", c.Name)
		}
		names[c.Name] = true
		if err := validLabel("commands["+c.Name+"] description", c.Description); err != nil {
			return err
		}
		if err := validLabel("commands["+c.Name+"] category", c.Category); err != nil {
			return err
		}
		if err := validateActions("command /"+c.Name, c.Run, true, c.Name); err != nil {
			return err
		}
	}
	if len(b.Hooks) > maxUserHooks {
		return fmt.Errorf("hooks: %d defined, the most is %d", len(b.Hooks), maxUserHooks)
	}
	for i, h := range b.Hooks {
		vals, ok := hookValues(h.On)
		if !ok {
			return fmt.Errorf("hooks[%d]: cannot watch %q; the settings a hook can watch are %s", i, h.On, strings.Join(hookEventNames, ", "))
		}
		if h.Is != "" && !contains(vals, h.Is) {
			return fmt.Errorf("hooks[%d]: %s is never %q; its values are %s (leave \"is\" out to react to any)", i, h.On, h.Is, strings.Join(vals, ", "))
		}
		if err := validateActions(fmt.Sprintf("hook %s=%s", h.On, h.Is), h.Do, false, ""); err != nil {
			return err
		}
	}
	return nil
}

func (b behaviour) validateMenuKeys() error {
	for _, act := range sortedKeys(b.MenuKeys) {
		if !contains(menuNavActions, act) {
			return fmt.Errorf("menu_keys: %q is not a menu action; the actions are %s", act, strings.Join(menuNavActions, ", "))
		}
		keys := b.MenuKeys[act]
		if len(keys) == 0 || len(keys) > maxKeysPerNav {
			return fmt.Errorf("menu_keys.%s: give between 1 and %d keys (remove the entry to put the factory key back)", act, maxKeysPerNav)
		}
		for _, raw := range keys {
			if _, err := menuKey(raw); err != nil {
				return fmt.Errorf("menu_keys.%s: %w", act, err)
			}
		}
	}
	// A key must mean one thing: the effective map (the user's keys where given, the
	// factory ones elsewhere) may not name a key twice.
	owner := map[string]string{}
	for _, act := range menuNavActions {
		for _, raw := range effectiveNav(b.MenuKeys, act) {
			k, _ := menuKey(raw)
			if prev, dup := owner[k]; dup && prev != act {
				return fmt.Errorf("menu_keys: %q would do both %q and %q; a key can steer a menu one way only (move one of them, or rebind the other action too)", k, prev, act)
			}
			owner[k] = act
		}
	}
	return nil
}

func effectiveNav(user map[string][]string, act string) []string {
	if keys, ok := user[act]; ok {
		return keys
	}
	return navDefaults[act]
}

// menuKey normalises a key a menu action may use. Unlike a shortcut it may be an arrow or
// Enter (that is what menu navigation is made of), but never a bare letter (it would stop
// the filter from taking that letter) and never Ctrl-C.
func menuKey(raw string) (string, error) {
	k, ok := term.ParseKey(strings.ToLower(strings.TrimSpace(raw)))
	if !ok {
		return "", fmt.Errorf("%q is not a key name; write names like \"left\", \"right\", \"enter\", \"esc\", \"tab\", \"pgup\", \"alt+j\", \"ctrl+f\"", raw)
	}
	if err := refuseEscapeHatch(k, raw); err != nil {
		return "", err
	}
	if k.Type == term.KeyRunes && k.Mod&(term.ModCtrl|term.ModAlt) == 0 {
		return "", fmt.Errorf("%q is a plain character, and a menu needs it to filter by typing; use a named key (left, right, pgup...) or a chord (alt+%s, ctrl+%s)", raw, raw, raw)
	}
	return k.String(), nil
}

// refuseEscapeHatch is the one rule no layer may bend: Ctrl-C leaves the program
// (invariant 6), whatever any scene, plugin or setting says.
func refuseEscapeHatch(k term.Key, raw string) error {
	if k.Type == term.KeyRunes && k.Mod&term.ModCtrl != 0 && len(k.Runes) == 1 && (k.Runes[0] == 'c' || k.Runes[0] == 'C') {
		return fmt.Errorf("%q is Ctrl-C, the way out of the program; no setting can take it, so choose another key", raw)
	}
	return nil
}

// reservedChords are chords the editor or the loop already uses; a shortcut on one would
// shadow it silently.
var reservedChords = map[string]string{
	"ctrl+o": "show the cut part of a result",
	"ctrl+j": "a new line in the message",
	"ctrl+p": "previous message",
	"ctrl+n": "next message",
	"ctrl+m": "Enter",
	"ctrl+i": "Tab",
	"ctrl+h": "Backspace",
	"ctrl+[": "Esc",
}

// shortcutKey normalises a key a user shortcut may take: an F-key or a Ctrl/Alt chord.
func shortcutKey(raw string) (string, error) {
	k, ok := term.ParseKey(strings.ToLower(strings.TrimSpace(raw)))
	if !ok {
		return "", fmt.Errorf("keys: %q is not a key name; write names like \"f5\", \"ctrl+g\", \"alt+e\"", raw)
	}
	if err := refuseEscapeHatch(k, raw); err != nil {
		return "", fmt.Errorf("keys: %w", err)
	}
	isF := k.Type >= term.KeyF1 && k.Type <= term.KeyF12
	chord := k.Type == term.KeyRunes && k.Mod&(term.ModCtrl|term.ModAlt) != 0
	if !isF && !chord {
		return "", fmt.Errorf("keys: %q would take a key you type with; a shortcut is an F-key (\"f5\") or a chord (\"ctrl+g\", \"alt+e\")", raw)
	}
	name := k.String()
	if why, bad := reservedChords[name]; bad {
		return "", fmt.Errorf("keys: %s already means %q in the app; choose another chord", name, why)
	}
	return name, nil
}

// validateActions checks every action against the scene's own closed grammar and the two
// extra rules this layer adds. self is the command's own name, so it cannot call itself.
func validateActions(who string, list actionList, allowExt bool, self string) error {
	if len(list) == 0 {
		return fmt.Errorf("%s: no action; write one like \"cmd:/effort max\"", who)
	}
	if len(list) > maxActionsEach {
		return fmt.Errorf("%s: %d actions, the most is %d", who, len(list), maxActionsEach)
	}
	for _, a := range list {
		act, err := scene.ParseAction(a)
		if err != nil {
			return fmt.Errorf("%s: %w", who, err)
		}
		switch act.Kind {
		case scene.ActionAnswer:
			return fmt.Errorf("%s: %q answers the agent's question on your behalf; only a button you press can do that, so a shortcut, command or hook may not", who, a)
		case scene.ActionExt:
			if !allowExt {
				return fmt.Errorf("%s: %q routes to a plugin without a keypress of its own; bind it to a key or a command instead", who, a)
			}
		case scene.ActionCmd:
			if !strings.HasPrefix(act.Arg, "/") {
				return fmt.Errorf("%s: %q runs a line as if you typed it, so it starts with '/': write \"cmd:/%s\"", who, a, act.Arg)
			}
			if strings.ContainsAny(act.Arg, "\r\n") {
				return fmt.Errorf("%s: %q holds a line break; one action is one line (use a list for several)", who, a)
			}
			if utf8.RuneCountInString(act.Arg) > 400 {
				return fmt.Errorf("%s: %q is longer than 400 characters", who, a)
			}
			if self != "" {
				first := strings.Fields(strings.TrimPrefix(act.Arg, "/"))
				if len(first) > 0 && first[0] == self {
					return fmt.Errorf("%s: calls /%s, itself; it would never end", who, self)
				}
			}
		}
	}
	return nil
}

func validLabel(what, s string) error {
	if !utf8.ValidString(s) {
		return fmt.Errorf("%s is not valid UTF-8", what)
	}
	if utf8.RuneCountInString(s) > maxDescLen {
		return fmt.Errorf("%s is longer than %d characters", what, maxDescLen)
	}
	for _, r := range s {
		if unicode.IsControl(r) {
			return fmt.Errorf("%s contains the control character U+%04X; the terminal would act on it instead of showing it", what, r)
		}
	}
	return nil
}

// ---- editing ------------------------------------------------------------------

// behaviourPatch is a change to the layer. The maps merge entry by entry and a null entry
// removes it; the lists, when present, replace the whole list (an empty list removes
// them). A part left out is untouched. It is both what the agent passes and what the typed
// /ui commands build.
type behaviourPatch struct {
	Animations map[string]*theme.CycleDef `json:"animations"`
	MenuKeys   map[string]*[]string       `json:"menu_keys"`
	Keys       map[string]*actionList     `json:"keys"`
	Commands   *[]userCommand             `json:"commands"`
	Hooks      *[]userHook                `json:"hooks"`
	// EnterWhileBusy is "queue" or "steer"; left out it is untouched.
	EnterWhileBusy *string `json:"enter_while_busy"`
}

func parseBehaviourPatch(raw json.RawMessage) (behaviourPatch, error) {
	var p behaviourPatch
	dec := json.NewDecoder(strings.NewReader(string(raw)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&p); err != nil {
		return p, fmt.Errorf("behaviour: %w (the parts are animations, menu_keys, keys, commands, hooks and enter_while_busy)", err)
	}
	return p, nil
}

// applyBehaviourPatch returns base with the patch applied and the result validated whole,
// so a refusal names the document that would have resulted, never half of it.
func applyBehaviourPatch(base behaviour, p behaviourPatch) (behaviour, error) {
	out := base.clone()
	for k, v := range p.Animations {
		if v == nil {
			delete(out.Animations, k)
			continue
		}
		if out.Animations == nil {
			out.Animations = map[string]theme.CycleDef{}
		}
		out.Animations[k] = *v
	}
	for k, v := range p.MenuKeys {
		if v == nil {
			delete(out.MenuKeys, k)
			continue
		}
		if out.MenuKeys == nil {
			out.MenuKeys = map[string][]string{}
		}
		out.MenuKeys[k] = *v
	}
	for k, v := range p.Keys {
		// Normalise the spelling so "Ctrl+G" and "ctrl+g" are the same entry.
		if nk, err := shortcutKey(k); err == nil {
			k = nk
		}
		if v == nil {
			delete(out.Keys, k)
			continue
		}
		if out.Keys == nil {
			out.Keys = map[string]actionList{}
		}
		out.Keys[k] = *v
	}
	if p.Commands != nil {
		out.Commands = append([]userCommand(nil), (*p.Commands)...)
	}
	if p.Hooks != nil {
		out.Hooks = append([]userHook(nil), (*p.Hooks)...)
	}
	if p.EnterWhileBusy != nil {
		// "queue" is the factory way and is kept as nothing, so the file stays canonical.
		if v := strings.TrimSpace(*p.EnterWhileBusy); v == busyQueue {
			out.EnterWhileBusy = ""
		} else {
			out.EnterWhileBusy = v
		}
	}
	out = out.normalised()
	if err := out.validate(); err != nil {
		return base, err
	}
	return out, nil
}

// normalised puts every key in its canonical spelling and fills the defaults, so the same
// meaning is always the same bytes on disk and in a diff.
func (b behaviour) normalised() behaviour {
	out := b.clone()
	if len(out.MenuKeys) > 0 {
		for act, keys := range out.MenuKeys {
			for i, raw := range keys {
				if k, err := menuKey(raw); err == nil {
					keys[i] = k
				}
			}
			out.MenuKeys[act] = keys
		}
	}
	for i := range out.Commands {
		if out.Commands[i].Category == "" {
			out.Commands[i].Category = "Custom"
		}
	}
	return out
}

// touchesWhatRuns reports whether the patch adds anything that runs when a key is pressed
// or a setting changes. Those are asked about even in full access: the user approved
// "full access" for the agent's work, not for what their own keyboard will do later.
func (p behaviourPatch) touchesWhatRuns() bool {
	// enter_while_busy is here too: "steer" makes Enter stop the model's work in flight,
	// which is the user's decision about their own keyboard and not the agent's.
	return len(p.Keys) > 0 || p.Commands != nil || p.Hooks != nil || p.EnterWhileBusy != nil
}

func sameBehaviour(a, b behaviour) bool {
	return string(encodeBehaviour(a)) == string(encodeBehaviour(b))
}

func sortedKeys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

// ---- keeping the user's behaviour ---------------------------------------------

func userBehaviourPath() string {
	d := configDir()
	if d == "" {
		return ""
	}
	return filepath.Join(d, "behaviour.json")
}

func loadUserBehaviour() (behaviour, error) {
	path := userBehaviourPath()
	if path == "" {
		return behaviour{}, nil
	}
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return behaviour{}, nil
	}
	if err != nil {
		return behaviour{}, err
	}
	return parseUserBehaviour(path, data)
}

func parseUserBehaviour(name string, data []byte) (behaviour, error) {
	if len(strings.TrimSpace(string(data))) == 0 {
		return behaviour{}, nil
	}
	var b behaviour
	dec := json.NewDecoder(strings.NewReader(string(data)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&b); err != nil {
		return behaviour{}, fmt.Errorf("%s: %w", name, err)
	}
	b = b.normalised()
	if err := b.validate(); err != nil {
		return behaviour{}, fmt.Errorf("%s: %w", name, err)
	}
	return b, nil
}

// encodeBehaviour writes the layer as indented JSON, maps sorted by encoding/json, so the
// file a person opens reads the same every time and a diff shows only what changed.
func encodeBehaviour(b behaviour) []byte {
	out, err := json.MarshalIndent(b, "", "  ")
	if err != nil {
		return []byte("{}\n")
	}
	return append(out, '\n')
}

// ---- what the layer does to the rest of the program ----------------------------

// withBehaviour lays the animations over a theme.
func withBehaviour(t *theme.Theme, b behaviour) *theme.Theme {
	if len(b.Animations) == 0 {
		return t
	}
	cycles := make(map[string]theme.Cycle, len(b.Animations))
	for name, def := range b.Animations {
		c, err := theme.ParseCycle(name, def)
		if err != nil {
			continue // validated on the way in; a stale file cannot stop the interface drawing
		}
		cycles[name] = c
	}
	return t.WithCycles(cycles)
}

// activeBeh is the layer on screen, read by the places that have no access to the loop's
// variables (the menus' key handler). The loop sets it whenever the layer changes.
var (
	activeBehMu sync.RWMutex
	activeBeh   behaviour
)

// setActiveBehaviour makes b the behaviour in force: the menu keys, and the commands the /
// menu lists.
func setActiveBehaviour(b behaviour) {
	activeBehMu.Lock()
	activeBeh = b.clone()
	activeBehMu.Unlock()
	var cmds []fold.SlashMatch
	for _, c := range b.Commands {
		cat := c.Category
		if cat == "" {
			cat = "Custom"
		}
		desc := c.Description
		if desc == "" {
			desc = "your command"
		}
		cmds = append(cmds, fold.SlashMatch{Name: c.Name, Category: cat, Description: desc})
	}
	fold.SetUserCommands(cmds)
}

func currentBehaviour() behaviour {
	activeBehMu.RLock()
	defer activeBehMu.RUnlock()
	return activeBeh
}

// navAction says what a key does in a choice menu: "prev", "next", "first", "last",
// "pick", "close", or "" for a key the menu does not steer with. A factory key whose
// action the user moved elsewhere answers "inert", so an arrow the user gave up does not
// keep half-working.
func navAction(k term.Key) string {
	name := k.String()
	user := currentBehaviour().MenuKeys
	for _, act := range menuNavActions {
		if contains(effectiveNav(user, act), name) {
			return act
		}
	}
	for _, act := range menuNavActions {
		if _, moved := user[act]; moved && contains(navDefaults[act], name) {
			return "inert"
		}
	}
	return ""
}

// ---- the user's commands, keys and hooks, as the loop uses them --------------------

// boundActions is what a pressed key runs, if the user bound it.
func (b behaviour) boundActions(k term.Key) (actionList, bool) {
	if len(b.Keys) == 0 {
		return nil, false
	}
	if k.Type != term.KeyRunes && !(k.Type >= term.KeyF1 && k.Type <= term.KeyF12) {
		return nil, false
	}
	a, ok := b.Keys[k.String()]
	return a, ok
}

// commandFor resolves what Enter means on the / menu line: the user's command the line
// names, or the highlighted row when it is one of theirs. ok is false for anything else,
// so every factory command takes its usual path.
func (b behaviour) commandFor(input string, sel int, cat string) (userCommand, bool) {
	if len(b.Commands) == 0 {
		return userCommand{}, false
	}
	body := strings.TrimSpace(input)
	if !strings.HasPrefix(body, "/") || strings.ContainsAny(body, " \t") {
		return userCommand{}, false
	}
	typed := strings.TrimPrefix(body, "/")
	find := func(name string) (userCommand, bool) {
		for _, c := range b.Commands {
			if c.Name == name {
				return c, true
			}
		}
		return userCommand{}, false
	}
	if c, ok := find(typed); ok {
		return c, true
	}
	matches := fold.FilterSlashCategory(typed, fold.NormalizeSlashCategory(typed, cat))
	if len(matches) == 0 {
		return userCommand{}, false
	}
	for _, m := range matches {
		if m.Name == typed {
			return userCommand{}, false // an exact factory name belongs to that command
		}
	}
	if sel < 0 || sel >= len(matches) {
		sel = len(matches) - 1
	}
	return find(matches[sel].Name)
}

// hooksFor lists the actions to run when `event` becomes `value`, in document order.
func (b behaviour) hooksFor(event, value string) actionList {
	var out actionList
	for _, h := range b.Hooks {
		if h.On == event && (h.Is == "" || h.Is == value) {
			out = append(out, h.Do...)
		}
	}
	return out
}

// ---- what the user and the agent are shown -----------------------------------

// behaviourGuide is the BEHAVIOUR chapter of ui_guide plus the live document.
func behaviourGuide(b behaviour) string {
	var s strings.Builder
	s.WriteString(`BEHAVIOUR. Beyond looks, the interface has a behaviour layer, also data: animations, the keys that steer the choice menus, shortcuts, commands of your own in the / menu, and hooks (reactions to a setting changing). Pass behaviour: {...} to ui_edit. The maps animations, menu_keys and keys merge entry by entry (null removes an entry); commands and hooks, when present, replace the whole list. Read the current document below first, and keep what you are not changing.

  animations  {"<name>": {"colors":["red","yellow","green","cyan","blue","magenta"], "fps":10, "spread":1, "attrs":["bold"]}}
      A colour that moves. The name is a style token: use it anywhere a token goes (a node's "style", a colour map...). spread 0 paints a whole span one colour at a time, 1 runs a rainbow along the text. fps 1-30 (default 8), 2-64 colours.
      To make a word change with a setting use the node property style_by: {"<value>": "<token>"} on a node with a bind. Example, the effort word in the status bar turns rainbow when the level is max:
        behaviour: {"animations": {"rainbow": {"colors": ["red","yellow","green","cyan","blue","magenta"], "spread": 1, "attrs": ["bold"]}}}
        commands: ["/ui set status_effort style_by {\"max\":\"rainbow\"}"]   (give the node an id first if it has none)
      A token that is animated cannot also be recoloured with colors; remove the animation first.
  menu_keys   {"prev":["up","left"], "next":["down","right"], "first":["pgup"], "last":["pgdown"], "pick":["enter"], "close":["esc"]}
      How the /effort, /mode, /style, /resume and /model menus move. Giving an action keys replaces its factory key (up, down, enter, esc); null puts the factory key back. A key cannot do two things. Plain letters are refused (the menu filters by typing); arrows, F-keys, pgup... and ctrl/alt chords are fine. For a horizontal menu: /ui set models layout horizontal, then bind prev to left and next to right.
  keys        {"f5": "cmd:/effort max", "ctrl+g": ["cmd:/mode plan", "cmd:/ui hide status"]}
      Shortcuts: an F-key or a ctrl/alt chord, running one action or a list. Never ctrl+c.
  commands    [{"name":"deep","category":"Custom","description":"max effort, plan mode","run":["cmd:/effort max","cmd:/mode plan"]}]
      Appear in the / menu and run when picked. The name cannot be an existing command.
  hooks       [{"on":"effort","is":"max","do":["cmd:/ui style status header"]}]
      on is effort, mode or style; is is one of its values (effort also has "none" for cleared) or left out for any change. Hooks fire when the user changes the setting, never when an action did, so nothing loops.
  An action is one of: cmd:/<line typed as the user would type it>, focus:<node id>, ext:<plugin-id>:<action> (keys and commands only; plugins are installed with /ui plugin add <url> and ask the user's consent). answer: is not allowed here.
  A key, a command or a hook can be removed with null (keys) or by sending the list without it. Changes to keys, commands and hooks are always put to the user, even in full access, because they decide what their own keyboard does later.
  enter_while_busy  "queue" | "steer"
      What Enter does with a line sent while the agent is working. queue (the factory way): the line waits, shown dimmed under the conversation, and is sent when the answer ends. steer: the work in flight is interrupted (what it already did is kept in the conversation) and the line is sent at once. Alt+Enter does the other one for a single line. Always put to the user.
  The user types the common ones themselves: /ui busy <queue|steer>, /ui animate <name> <colour,colour,...> [fps=N] [spread=N] [bold] | off, /ui key <key> <action...> | off, /ui menukeys <action> <key...> | off.

`)
	s.WriteString("Current behaviour:\n")
	s.Write(encodeBehaviour(b))
	return s.String()
}

// behaviourDiff is a change of behaviour as the conversation shows a diff.
func behaviourDiff(before, after behaviour) (string, error) {
	return uiDiffText(encodeBehaviour(before), encodeBehaviour(after))
}

// ---- typed /ui commands ----------------------------------------------------------

// uiBehaviourCommand reads the three typed /ui lines that edit behaviour and returns the
// patch they mean. ok is false for any other line; err is a refusal to show the user.
func uiBehaviourCommand(input string) (p behaviourPatch, label string, ok bool, err error) {
	f := strings.Fields(input)
	if len(f) < 3 || f[0] != "/ui" {
		return p, "", false, nil
	}
	switch f[1] {
	case "busy":
		if len(f) != 3 || (f[2] != busyQueue && f[2] != busySteer) {
			return p, "", true, fmt.Errorf("/ui busy <queue|steer>  (what Enter does with a line sent while the agent works: queue waits for the answer, steer interrupts the turn)")
		}
		v := f[2]
		return behaviourPatch{EnterWhileBusy: &v}, "/ui busy: Enter now " + map[string]string{busyQueue: "queues a line sent while the agent works", busySteer: "steers the agent with a line sent while it works"}[v], true, nil
	case "animate":
		name := f[2]
		if len(f) == 4 && f[3] == "off" {
			return behaviourPatch{Animations: map[string]*theme.CycleDef{name: nil}}, "/ui animate: " + name + " is off", true, nil
		}
		if len(f) < 4 {
			return p, "", true, fmt.Errorf("/ui animate <name> <colour,colour,...> [fps=N] [spread=N] [bold ...]  (or: /ui animate <name> off)")
		}
		def := theme.CycleDef{}
		for _, c := range strings.Split(f[3], ",") {
			if c = strings.TrimSpace(c); c != "" {
				def.Colors = append(def.Colors, c)
			}
		}
		for _, w := range f[4:] {
			switch {
			case strings.HasPrefix(w, "fps="):
				fmt.Sscanf(strings.TrimPrefix(w, "fps="), "%d", &def.FPS)
			case strings.HasPrefix(w, "spread="):
				fmt.Sscanf(strings.TrimPrefix(w, "spread="), "%d", &def.Spread)
			default:
				def.Attrs = append(def.Attrs, w)
			}
		}
		return behaviourPatch{Animations: map[string]*theme.CycleDef{name: &def}}, "/ui animate: " + name + " now moves", true, nil
	case "key":
		key := f[2]
		if len(f) == 4 && f[3] == "off" {
			return behaviourPatch{Keys: map[string]*actionList{key: nil}}, "/ui key: " + key + " is free again", true, nil
		}
		if len(f) < 4 {
			return p, "", true, fmt.Errorf("/ui key <key> <action>  e.g. /ui key f5 cmd:/effort max  (or: /ui key <key> off)")
		}
		// Several actions are separated by ';' so a line stays one line.
		var acts actionList
		for _, a := range strings.Split(strings.Join(f[3:], " "), ";") {
			if a = strings.TrimSpace(a); a != "" {
				acts = append(acts, a)
			}
		}
		return behaviourPatch{Keys: map[string]*actionList{key: &acts}}, "/ui key: " + key + " is bound", true, nil
	case "menukeys":
		act := f[2]
		if len(f) == 4 && f[3] == "off" {
			return behaviourPatch{MenuKeys: map[string]*[]string{act: nil}}, "/ui menukeys: " + act + " is back to the factory key", true, nil
		}
		if len(f) < 4 {
			return p, "", true, fmt.Errorf("/ui menukeys <%s> <key...>  e.g. /ui menukeys next down right  (or: /ui menukeys <action> off)", strings.Join(menuNavActions, "|"))
		}
		keys := append([]string(nil), f[3:]...)
		return behaviourPatch{MenuKeys: map[string]*[]string{act: &keys}}, "/ui menukeys: " + act + " now answers to " + strings.Join(keys, ", "), true, nil
	}
	return p, "", false, nil
}
