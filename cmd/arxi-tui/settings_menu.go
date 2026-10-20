package main

import (
	"fmt"
	"strings"

	"github.com/michiTrader/arxi_tui/internal/engine"
	"github.com/michiTrader/arxi_tui/internal/fold"
	"github.com/michiTrader/arxi_tui/internal/scene"
)

// This file is the navigable half of /settings: `/settings` opens a menu of every
// setting with its value, picking one opens the values it can take, and picking a value
// changes it. It is the same overlay, keys and filter as /model, /effort and /mode.
//
// Why a menu on top of the typed surface (settings_surface.go): the typed form needs the
// exact name, and a name is what a user who asks "what can I change?" does not have. The
// menu is the answer to that question. It adds no store and no rules: a pick is turned
// into the same `/settings <name> <value>` line a user could type, and that line goes
// through runSettingsSurface, so the menu, the typed line, /ui text and the agent's
// ui_edit can never disagree about what is valid or where it is kept.
//
// Two stages, told apart by the line alone (never by hidden state, so the renderer, the
// key handler and a test all read the same answer):
//
//	"/settings "            the list of settings              (stage "")
//	"/settings <name> "     the values that setting can take  (stage <name>)
//
// A setting that is a free number (scroll.wheel_step, tool.output_rows...) has no list of
// values to offer, so it has no second stage: picking it leaves `/settings <name> ` in the
// input for the user to finish, which is the typed surface's.

// settingHiddenStage is the one stage of the menu that is not a text key: the parts of
// the screen the user can hide. It is a stage of its own because a part is not a value of
// a setting: there is one row per part, and a pick toggles it.
const settingHiddenStage = "hidden"

// hidablePart is one node the user may hide with the menu.
type hidablePart struct{ id, kind string }

// hidableParts lists the nodes of the live scene that the menu offers to hide, in the
// order the document declares them.
//
// `/ui hide` itself accepts any id, including the input and the menus; the menu does not
// offer those, because a menu is one Enter away from a mistake that a typed command is
// not. Hiding the input leaves a screen the user cannot type `/ui show` into, and hiding a
// menu overlay hides the very menu being used to undo it. Two rules decide, and neither
// reads a name (a scene of the user's own names its nodes as it likes):
//
//   - a node that is, or contains, an input is never offered: hiding it hides the input;
//   - an overlay, and everything inside it, is never offered: those are the menus.
//
// A row template is not walked: hiding it would blank every row of a list that draws from
// it, which is not "a part of the screen" the user would point at.
func hidableParts(doc *scene.Document) []hidablePart {
	if doc == nil {
		return nil
	}
	var out []hidablePart
	collectHidable(doc.Root, &out)
	return out
}

// collectHidable appends the offerable nodes under n and reports whether n is, or
// contains, an input or an overlay (so n itself is not offered).
func collectHidable(n *scene.Node, out *[]hidablePart) (protected bool) {
	if n == nil {
		return false
	}
	if n.Type == "overlay" {
		return true // its descendants are menus: not even visited
	}
	protected = n.Type == "input"
	var sub []hidablePart
	kids := append([]*scene.Node{}, n.Children...)
	kids = append(kids, n.PrefixNode(), n.Suffix)
	for _, c := range kids {
		if collectHidable(c, &sub) {
			protected = true
		}
	}
	if !protected && n.ID != "" {
		*out = append(*out, hidablePart{id: n.ID, kind: n.Type})
	}
	*out = append(*out, sub...)
	return protected
}

// hiddenRows is the second stage of "hidden": one row per part, saying what a pick does.
func hiddenRows(doc *scene.Document, hidden map[string]bool) []fold.ModelMatch {
	var rows []fold.ModelMatch
	for _, p := range hidableParts(doc) {
		state := "shown, enter hides it"
		if hidden[p.id] {
			state = "HIDDEN, enter shows it"
		}
		rows = append(rows, fold.ModelMatch{Ref: p.id, Name: p.id, Provider: p.kind + " · " + state})
	}
	return rows
}

// toggleHidden hides a part that is shown and shows one that is hidden, and says which.
// An id the menu would not have offered is refused, so a stale row cannot hide the input.
func toggleHidden(doc *scene.Document, hidden map[string]bool, id string) string {
	ok := false
	for _, p := range hidableParts(doc) {
		ok = ok || p.id == id
	}
	if !ok {
		return "/settings: " + strconvQuote(id) + " is not a part the menu hides (/ui hide <id> hides any node by hand)"
	}
	if hidden[id] {
		delete(hidden, id)
		return "/settings: " + id + " is shown again"
	}
	hidden[id] = true
	return "/settings: " + id + " is hidden for this session (/settings hidden shows it again; not kept between sessions)"
}

// settingChoices are the values a setting can take when they are a short closed set; nil
// when it is a free number or sentence, which a list of rows cannot offer. The accepted
// values stay wider than these rows (yes/no also accepts on/off/true/false): the rows are
// the choices worth pointing at, validSettingValue is the rule.
func settingChoices(key string) []string {
	switch key {
	case "tool.inline", "chat.light", "chat.scrollbar":
		return []string{"yes", "no"}
	case "resume.scope":
		return []string{resumeFolder, resumeAll}
	case "status.narrow":
		return []string{engine.NarrowWrap, engine.NarrowHide}
	case "effort.default":
		out := []string{effortNone}
		for _, l := range effortLevelOrder {
			if l != "off" { // "off" is not a default; validChatLook refuses it too
				out = append(out, l)
			}
		}
		return out
	}
	return nil
}

// settingChoiceMatches says whether a typed fragment still points at one of the key's
// values, the way filterModels reads a filter: every word is somewhere in the value.
// An empty fragment points at all of them.
func settingChoiceMatches(key, fragment string) bool {
	words := strings.Fields(strings.ToLower(fragment))
	for _, c := range settingChoices(key) {
		ok := true
		for _, w := range words {
			if !strings.Contains(c, strings.TrimSuffix(w, "%")) {
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

// settingChoiceIs says whether the fragment is, whole, one of the key's values: a line
// that already says its value is the typed surface's to carry out, not a menu to pick in.
func settingChoiceIs(key, fragment string) bool {
	v := strings.TrimSuffix(strings.ToLower(strings.TrimSpace(fragment)), "%")
	for _, c := range settingChoices(key) {
		if c == v {
			return true
		}
	}
	return false
}

// settingsMenuStage reads the line. open is false when the menu has no claim on it
// (not a /settings line, or one the typed surface carries out); key is the setting whose
// values are listed, "" for the list of settings; prefix is what the line starts with
// before the filter; filter is what narrows the rows.
//
// The value stage steps aside as soon as the fragment cannot be a pick: `/settings
// chat.light zzz` must reach the typed surface so Enter refuses it, and offering an empty
// menu there would make Enter do nothing at all. The factory spelling `-` needs no case of
// its own: no value a row offers contains a hyphen, so `-` matches nothing and steps aside
// (a test holds that, because the day a value gains a hyphen `-` would stop resetting).
func settingsMenuStage(input string) (key, prefix, filter string, open bool) {
	rest, in := menuOpen(settingsPrefix, input)
	if !in {
		return "", "", "", false
	}
	first, tail, spaced := strings.Cut(rest, " ")
	if spaced && first == settingHiddenStage {
		// Always open, even when the filter matches nothing: there is no typed form of
		// this stage for the line to fall to, so stepping aside would send the words to
		// the model as a message.
		return first, settingsPrefix + first + " ", strings.TrimLeft(tail, " "), true
	}
	if spaced && isSettingKey(first) && settingChoices(first) != nil {
		tail = strings.TrimLeft(tail, " ")
		if !settingChoiceIs(first, tail) && settingChoiceMatches(first, tail) {
			return first, settingsPrefix + first + " ", tail, true
		}
		return "", "", "", false
	}
	if _, surface := settingsSurfaceLine(input); surface {
		return "", "", "", false
	}
	return "", settingsPrefix, rest, true
}

// settingsRows are the rows of one stage.
//
// The first stage ends with the autocompact rows (off, 50%...90%) that `/settings` has
// always opened on, with the refs they always had ("autocompact:70"). Typing `/settings
// 80` and Enter was how autocompact was set before this menu listed anything else; it
// still is, and the pick, and the warning it gives when the model's window is unknown,
// stay one path. A second stage for autocompact would only repeat these six rows.
func settingsRows(stage string, ac autoCompactSetting, window int, doc *scene.Document, hidden map[string]bool) []fold.ModelMatch {
	if stage == "" {
		return append(settingsKeyRows(hidden), settingsMenuData(ac, window)...)
	}
	if stage == settingHiddenStage {
		return hiddenRows(doc, hidden)
	}
	cur := normalizedChoice(stage, uiText(stage))
	var rows []fold.ModelMatch
	for _, c := range settingChoices(stage) {
		hint := ""
		if c == textIndex[stage].def {
			hint = "factory"
		}
		if stage == "effort.default" {
			if h := effortHints[c]; h != "" {
				hint = strings.TrimSpace(h + "  " + hint)
			}
		}
		rows = append(rows, fold.ModelMatch{Ref: c, Name: c, Provider: hint, Current: c == cur})
	}
	return rows
}

// normalizedChoice maps the spellings validSettingValue accepts onto the row that stands
// for them, so a setting the user wrote as "off" still has its row marked.
func normalizedChoice(key, v string) string {
	v = strings.ToLower(strings.TrimSpace(v))
	switch key {
	case "tool.inline", "chat.light", "chat.scrollbar":
		switch v {
		case "true", "on":
			return "yes"
		case "false", "off":
			return "no"
		}
	}
	return v
}

// settingsKeyRows is the first stage: every setting, its value now, and what it does.
// A value that is not the factory one says what the factory one is, so the menu answers
// "what did I change?" without a second command.
func settingsKeyRows(hidden map[string]bool) []fold.ModelMatch {
	rows := make([]fold.ModelMatch, 0, len(settingsKeys))
	for _, k := range settingsKeys {
		cur := uiText(k)
		val := strconvQuote(cur)
		if cur != textIndex[k].def {
			val += " (factory " + strconvQuote(textIndex[k].def) + ")"
		}
		rows = append(rows, fold.ModelMatch{Ref: k, Name: k, Provider: val + " — " + firstClause(textIndex[k].role)})
	}
	rows = append(rows, fold.ModelMatch{Ref: settingHiddenStage, Name: settingHiddenStage,
		Provider: hiddenList(hidden) + " — parts of the screen you hid; pick to hide or show one (kept for this session only)"})
	return rows
}

// settingPickLine is the line a pick of a setting leaves in the input: the setting's name
// and a space, which is the second stage for a setting with choices and the typed
// surface's for one without.
func settingPickLine(key string) string { return settingsPrefix + key + " " }

// settingPickNotice tells the user what to type after picking a setting that has no list
// of values, because the input alone (`/settings scroll.wheel_step `) does not say what
// is accepted. Empty for a setting that has a second stage: its rows say it.
func settingPickNotice(key string) string {
	if settingChoices(key) != nil {
		return ""
	}
	d, ok := textIndex[key]
	if !ok {
		return ""
	}
	return fmt.Sprintf("%s is %s — %s. Type the new value and press enter (- puts the factory value back)",
		key, strconvQuote(uiText(key)), firstClause(d.role))
}

// settingsValueMenuOpen is whether the line is in the second stage, where Enter picks a
// row. The typed surface also recognises such a line (`/settings chat.scrollbar n` is a
// setting and a value), and it is checked earlier in the loop, so without this it takes
// the Enter first and reads the half-typed fragment "n" as the value: refused, and the
// row that was highlighted is never picked.
func settingsValueMenuOpen(input string) bool {
	key, _, _, open := settingsMenuStage(input)
	return open && key != ""
}
