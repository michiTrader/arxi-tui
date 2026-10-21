package main

import (
	"fmt"
	"strings"

	"github.com/michiTrader/arxi_tui/internal/fold"
	"github.com/michiTrader/arxi_tui/internal/term"
)

// This file is /keys: one place that shows every key of the app and what it does, and
// changes the ones that can change.
//
// The keys of the app live in several handlers (the editor, the scroll keys, the history,
// the find bar...), and a person who asks "what does ctrl+o do?" has no list to read. The
// registry below is that list. It is not a second source of truth for what a key does:
// each entry carries a probe that asks the real handler whether it takes the key, and a
// test runs every probe, so an entry that drifts from the code fails there.
//
// What can be changed, and where it is kept:
//
//	menu keys   prev, next, first, last, pick, close: the keys that steer every menu.
//	            Kept in behaviour.json as menu_keys, through the same /ui menukeys code.
//	shortcuts   F-keys and ctrl/alt chords the user binds to an action. Kept as keys.
//	            The typed line `/keys <key> <action>` is `/ui key <key> <action>`.
//	factory     everything else. Listed, explained, not movable in this version.
//
// So the menu adds no store and no rule: every change goes through applyBehaviourPatch,
// which validates the whole document (Ctrl-C refused, a key that does two things refused).
//
// Two stages, told apart by the line alone (never by hidden state), like /settings:
//
//	"/keys "          every key and what it does       (stage "")
//	"/keys <action> " the keys that menu action can have (stage <action>)

// factoryKey is one thing the keyboard does when nobody has changed anything.
type factoryKey struct {
	Keys  []string // spellings, as term.Key.String writes them
	What  string
	Note  string              // what can be done about it ("" when it is simply fixed)
	Spent func(term.Key) bool // asks the real handler whether it takes the key
}

// factoryKeys is the list /keys shows, in the order a person reads it: the keys that leave
// or stop things first, then writing, then reading the chat.
var factoryKeys = []factoryKey{
	{Keys: []string{"ctrl+c"}, What: "stop the answer being written; when idle it clears the line, and twice in a row leaves the program",
		Note: "cannot be moved or taken by any setting, scene or plugin", Spent: isCtrlC},
	{Keys: []string{"esc"}, What: "stop the answer being written, or close the menu that is open"},
	{Keys: []string{"enter"}, What: "send the message (or run the command on the line); while an answer is being written the message waits and is sent when it ends",
		Spent: keymapTakes},
	{Keys: []string{"alt+enter"}, What: "while an answer is being written, the other way from enter: stop it and send the line at once (steer), or make it wait if enter steers",
		Note: "enter's way is /ui busy queue|steer", Spent: keymapTakes},
	{Keys: []string{"shift+enter", "ctrl+enter", "ctrl+j"}, What: "a new line in the message, without sending it",
		Spent: isNewlineGesture},
	{Keys: []string{"up", "down"}, What: "walk the lines you sent before (inside a long message they move between its rows first)",
		Spent: func(k term.Key) bool { return historyStep(k) != 0 }},
	{Keys: []string{"ctrl+p", "ctrl+n"}, What: "the same as up and down",
		Spent: func(k term.Key) bool { return historyStep(k) != 0 }},
	{Keys: []string{"left", "right", "home", "end"}, What: "move the caret in the message",
		Spent: editorKey},
	{Keys: []string{"ctrl+left", "alt+left", "ctrl+right", "alt+right", "alt+b", "alt+f"}, What: "move the caret a word at a time",
		Spent: editorKey},
	{Keys: []string{"backspace", "delete", "alt+backspace", "ctrl+backspace"}, What: "delete a character, or a word with alt/ctrl",
		Spent: editorKey},
	{Keys: []string{"ctrl+z", "ctrl+y"}, What: "take back the last edit of the message, and put it again",
		Note:  "a shortcut of yours on the same key wins: /keys ctrl+z <action>",
		Spent: func(k term.Key) bool { return isUndoKey(k) || isRedoKey(k) }},
	{Keys: []string{"ctrl+f"}, What: "find a word in the chat (the find bar on the input row)",
		Note: "a shortcut of yours on the same key wins: /keys ctrl+f <action>", Spent: keymapTakes},
	{Keys: []string{"ctrl+o"}, What: "show the cut part of a result or a change, or cut it again", Spent: keymapTakes},
	{Keys: []string{"shift+tab"}, What: "next mode (ask, plan, edit...) without opening a menu", Spent: isModeKey},
	{Keys: []string{"tab"}, What: "in the / menu: the next category; in /copy: mark a row"},
	{Keys: []string{"pgup", "pgdown", "ctrl+end"}, What: "scroll the chat a page up, a page down, or to the latest line",
		Spent: func(k term.Key) bool { _, spent := scrollKey(k, "", 0, 0, 10); return spent }},
	{Keys: []string{"ctrl+up", "ctrl+down", "alt+up", "alt+down", "ctrl+home", "shift+up", "shift+down"},
		What:  "scroll the chat a notch, a row, to the oldest line, or to the previous/next message you wrote",
		Spent: func(k term.Key) bool { _, spent := scrollArrowKey(k, 0, 3, []int{5, 9}); return spent }},
	{Keys: []string{"wheelup", "wheeldown"}, What: "scroll the chat with the mouse wheel"},
	{Keys: []string{"y", "n"}, What: "answer a question the app is waiting on (allow a change, approve a package); esc declines",
		Spent: func(k term.Key) bool { _, decided := approvalKey(k); return decided }},
}

// editorKey asks the line editor whether it acts on the key.
func editorKey(k term.Key) bool {
	_, _, ok := applyEdit("ab cd", 2, k)
	return ok
}

// isModeKey is Shift+Tab: the key that walks the agent modes.
func isModeKey(k term.Key) bool {
	return k.Type == term.KeyTab && k.Mod&term.ModShift != 0
}

// ---- the menu --------------------------------------------------------------------

const keysPrefix = "/keys "

// keysFactoryRef is the row at the end of a menu action's keys that puts the factory keys back.
const keysFactoryRef = "factory"

// menuActionWhat is what each menu action does, for the list.
var menuActionWhat = map[string]string{
	"prev":  "move up one row in every menu",
	"next":  "move down one row in every menu",
	"first": "jump to the first row",
	"last":  "jump to the last row",
	"pick":  "choose the highlighted row",
	"close": "close the menu",
}

// menuKeySuggestions are the keys worth offering for each action; any other key can be
// typed (`/keys prev alt+k`). A key that another action already owns is left out.
var menuKeySuggestions = map[string][]string{
	"prev":  {"up", "left", "pgup", "alt+k"},
	"next":  {"down", "right", "pgdown", "alt+j"},
	"first": {"home", "pgup", "alt+g"},
	"last":  {"end", "pgdown", "alt+l"},
	"pick":  {"enter", "right", "alt+l"},
	"close": {"esc", "left", "alt+h"},
}

// keysStage reads the line: act is the menu action whose keys are listed ("" for the list
// of everything), prefix is what the line starts with before the filter, open is false when
// the menu has no claim on the line.
//
// The second stage steps aside as soon as the fragment names a key whole or cannot be a
// pick, so `/keys prev ctrl+k` reaches the typed line and a key that is not on offer can
// still be set. `/keys list` and `/keys <key> <action>` are the typed line's too.
func keysStage(input string) (act, prefix, filter string, open bool) {
	rest, in := menuOpen(keysPrefix, input)
	if !in || rest == "list" {
		return "", "", "", false
	}
	first, tail, spaced := strings.Cut(rest, " ")
	if !spaced {
		return "", keysPrefix, rest, true
	}
	if contains(menuNavActions, first) {
		tail = strings.TrimLeft(tail, " ")
		if !keyChoiceIs(first, tail) && keyChoiceMatches(first, tail) {
			return first, keysPrefix + first + " ", tail, true
		}
	}
	return "", "", "", false
}

// keyChoiceMatches says whether the fragment still points at a key on offer (an empty one
// points at all of them).
func keyChoiceMatches(act, fragment string) bool {
	words := strings.Fields(strings.ToLower(fragment))
	for _, k := range keyCandidates(act, currentBehaviour()) {
		ok := true
		for _, w := range words {
			if !strings.Contains(k, w) {
				ok = false
				break
			}
		}
		if ok {
			return true
		}
	}
	return false
}

// keyChoiceIs says whether the fragment is, whole, one of the keys on offer: a line that
// already names its key is the typed line's to carry out, not a menu to pick in. The
// factory spellings `-` and `factory` need no case of their own: no key on offer contains
// a hyphen or the word, so they match nothing and the menu steps aside (a test holds that).
func keyChoiceIs(act, fragment string) bool {
	f := strings.ToLower(strings.TrimSpace(fragment))
	if f == "" {
		return false
	}
	return contains(keyCandidates(act, currentBehaviour()), f)
}

// keyCandidates are the keys the second stage lists for an action: the ones in use, then
// the suggestions nobody else owns.
func keyCandidates(act string, b behaviour) []string {
	out := append([]string(nil), effectiveNav(b.MenuKeys, act)...)
	owned := map[string]bool{}
	for _, other := range menuNavActions {
		if other == act {
			continue
		}
		for _, k := range effectiveNav(b.MenuKeys, other) {
			owned[k] = true
		}
	}
	for _, k := range b.shortcutNames() {
		owned[k] = true
	}
	for _, s := range menuKeySuggestions[act] {
		if contains(out, s) || owned[s] {
			continue
		}
		out = append(out, s)
	}
	return out
}

// shortcutNames is the spellings of the shortcuts the user bound, in a stable order.
func (b behaviour) shortcutNames() []string { return sortedKeys(b.Keys) }

// keysRows are the rows of one stage.
func keysRows(act string, b behaviour) []fold.ModelMatch {
	if act == "" {
		return keysFirstRows(b)
	}
	inUse := effectiveNav(b.MenuKeys, act)
	var rows []fold.ModelMatch
	for _, k := range keyCandidates(act, b) {
		hint := ""
		if contains(navDefaults[act], k) {
			hint = "factory"
		}
		rows = append(rows, fold.ModelMatch{Ref: k, Name: k, Provider: hint, Current: contains(inUse, k)})
	}
	rows = append(rows, fold.ModelMatch{Ref: keysFactoryRef, Name: keysFactoryRef,
		Provider: "put back " + keyList(navDefaults[act])})
	return rows
}

func keyList(keys []string) string {
	if len(keys) == 0 {
		return "no key"
	}
	return strings.Join(keys, ", ")
}

// keysFirstRows is the first stage: the menu keys (changeable), the shortcuts the user
// bound, and the factory keys. The ref of a row starts with its kind and carries its words,
// so typing "scroll" finds the scroll keys; the kind is what the pick reads.
func keysFirstRows(b behaviour) []fold.ModelMatch {
	var rows []fold.ModelMatch
	for _, act := range menuNavActions {
		now := effectiveNav(b.MenuKeys, act)
		val := keyList(now)
		if _, moved := b.MenuKeys[act]; moved {
			val += " (factory " + keyList(navDefaults[act]) + ")"
		}
		rows = append(rows, fold.ModelMatch{
			Ref:      "menu:" + act + " " + menuActionWhat[act],
			Name:     "menu " + act,
			Provider: val + " — " + menuActionWhat[act] + " · pick to change",
		})
	}
	for _, k := range b.shortcutNames() {
		rows = append(rows, fold.ModelMatch{
			Ref:      "key:" + k + " shortcut",
			Name:     k,
			Provider: "your shortcut — " + strings.Join(b.Keys[k], "; "),
		})
	}
	for i, f := range factoryKeys {
		note := ""
		if f.Note != "" {
			note = " (" + f.Note + ")"
		}
		rows = append(rows, fold.ModelMatch{
			Ref:      fmt.Sprintf("fixed:%d %s %s", i, strings.Join(f.Keys, " "), f.What),
			Name:     strings.Join(f.Keys, " "),
			Provider: f.What + note,
		})
	}
	return rows
}

// keysPick is what picking a first-stage row does: the line it leaves in the input (a menu
// key opens its keys), or a note explaining the row (a shortcut or a fixed key).
func keysPick(ref string, b behaviour) (line, note string) {
	kind, rest, _ := strings.Cut(ref, ":")
	id, _, _ := strings.Cut(rest, " ")
	switch kind {
	case "menu":
		return keysPrefix + id + " ", ""
	case "key":
		return "", fmt.Sprintf("%s is your shortcut: it runs %s. /keys %s off frees the key; /keys %s <action> changes it",
			id, strings.Join(b.Keys[id], "; "), id, id)
	case "fixed":
		var n int
		if _, err := fmt.Sscanf(id, "%d", &n); err == nil && n >= 0 && n < len(factoryKeys) {
			f := factoryKeys[n]
			msg := strings.Join(f.Keys, ", ") + ": " + f.What + ". "
			if f.Note != "" {
				return "", msg + f.Note
			}
			return "", msg + "It is fixed in this version; the keys that can change are the menu keys and your own shortcuts"
		}
	}
	return "", ""
}

// keysCommand is `/keys` picked from the command menu or typed whole.
func keysCommand(input string, sel int, cat string) bool {
	return menuCommand("keys", input, sel, cat)
}

// ---- carrying out a line ----------------------------------------------------------

// keysOutcome is what one /keys line did. A line prints a note, refuses (err), or changes
// the behaviour layer (changed), with next the layer to keep and note what to say.
type keysOutcome struct {
	note    string
	err     string
	changed bool
	next    behaviour
}

// runKeysLine carries out the words after "/keys" against the user's behaviour layer
// (user) given the layer in force (eff). It is pure, so every rule is a function call:
//
//	list                       print every key
//	<action> <key>             add the key to a menu action, or take it away if it is there
//	<action> -                 the factory keys of that action
//	<key> <action...>          bind a shortcut (the same as /ui key)
//	<key> off                  free a shortcut
func runKeysLine(args string, user, eff behaviour) keysOutcome {
	f := strings.Fields(args)
	if len(f) == 0 {
		return keysOutcome{err: "/keys: say what to change; /keys opens the list, /keys list prints it"}
	}
	if f[0] == "list" && len(f) == 1 {
		return keysOutcome{note: keysListing(eff)}
	}
	var patch behaviourPatch
	var label string
	if contains(menuNavActions, f[0]) {
		if len(f) != 2 {
			return keysOutcome{err: fmt.Sprintf("/keys: %s takes one key at a time (/keys %s alt+k adds it, or takes it away if it is already there; /keys %s - puts the factory keys back)", f[0], f[0], f[0])}
		}
		p, l, err := toggleMenuKey(user, f[0], f[1])
		if err != nil {
			return keysOutcome{err: "/keys: " + err.Error()}
		}
		patch, label = p, l
	} else {
		p, l, ok, err := uiBehaviourCommand("/ui key " + args)
		switch {
		case err != nil:
			return keysOutcome{err: strings.Replace(err.Error(), "/ui key", "/keys", 1)}
		case !ok:
			return keysOutcome{err: fmt.Sprintf("/keys: %q is not a menu action (%s); to bind a shortcut write /keys <key> <action>, e.g. /keys f5 cmd:/effort max", f[0], strings.Join(menuNavActions, ", "))}
		}
		patch, label = p, strings.Replace(l, "/ui key", "/keys", 1)
	}
	next, err := applyBehaviourPatch(user, patch)
	if err != nil {
		return keysOutcome{err: "/keys: " + err.Error()}
	}
	return keysOutcome{changed: true, next: next, note: label}
}

// toggleMenuKey is the patch for "this key, on this action", built over what the user has
// saved (never the folder's layer, which a pick must not write into their file): added when
// it is not one of the action's keys, taken away when it is. The result goes back to the
// factory entry (no override at all) when it equals the factory keys, so a change undone
// leaves nothing behind.
func toggleMenuKey(user behaviour, act, raw string) (behaviourPatch, string, error) {
	if raw == "-" || raw == keysFactoryRef {
		return behaviourPatch{MenuKeys: map[string]*[]string{act: nil}},
			"/keys: " + act + " is back to " + keyList(navDefaults[act]), nil
	}
	key, err := menuKey(raw)
	if err != nil {
		return behaviourPatch{}, "", err
	}
	now := append([]string(nil), effectiveNav(user.MenuKeys, act)...)
	var after []string
	var label string
	if contains(now, key) {
		for _, k := range now {
			if k != key {
				after = append(after, k)
			}
		}
		if len(after) == 0 {
			return behaviourPatch{}, "", fmt.Errorf("%s is the only key of %s, and an action needs one; add another key first, or /keys %s - puts the factory key back", key, act, act)
		}
		label = fmt.Sprintf("/keys: %s no longer does %s (it answers to %s)", key, act, keyList(after))
	} else {
		after = append(now, key)
		label = fmt.Sprintf("/keys: %s now answers to %s", act, keyList(after))
	}
	if sameKeySet(after, navDefaults[act]) {
		return behaviourPatch{MenuKeys: map[string]*[]string{act: nil}}, label, nil
	}
	return behaviourPatch{MenuKeys: map[string]*[]string{act: &after}}, label, nil
}

func sameKeySet(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for _, k := range a {
		if !contains(b, k) {
			return false
		}
	}
	return true
}

// keysTypedLine reads `/keys <something>` that is not the menu's: what follows "/keys".
func keysTypedLine(input string) (args string, ok bool) {
	rest, has := strings.CutPrefix(strings.TrimLeft(input, " \t"), "/keys")
	if !has || rest == "" || (rest[0] != ' ' && rest[0] != '\t') {
		return "", false
	}
	args = strings.TrimSpace(rest)
	if args == "" {
		return "", false
	}
	if _, _, _, open := keysStage(input); open {
		return "", false
	}
	return args, true
}

// keysValueMenuOpen is whether the line is in the second stage, where Enter picks a row.
func keysValueMenuOpen(input string) bool {
	act, _, _, open := keysStage(input)
	return open && act != ""
}

// keysListing is every key, as a note: what you can change first, then what is fixed.
func keysListing(b behaviour) string {
	var s strings.Builder
	s.WriteString("Keys you can change (/keys opens them; /keys <action> <key> adds a key or takes it away):\n")
	for _, act := range menuNavActions {
		fmt.Fprintf(&s, "  menu %-6s %-16s %s\n", act, keyList(effectiveNav(b.MenuKeys, act)), menuActionWhat[act])
	}
	if len(b.Keys) > 0 {
		s.WriteString("Your shortcuts:\n")
		for _, k := range b.shortcutNames() {
			fmt.Fprintf(&s, "  %-16s %s\n", k, strings.Join(b.Keys[k], "; "))
		}
	}
	s.WriteString("Fixed keys:\n")
	for _, f := range factoryKeys {
		fmt.Fprintf(&s, "  %-28s %s\n", strings.Join(f.Keys, " "), f.What)
	}
	return strings.TrimRight(s.String(), "\n")
}
