package main

import (
	"fmt"
	"strings"

	"github.com/michiTrader/arxi_tui/internal/engine"
	"github.com/michiTrader/arxi_tui/internal/fold"
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
// autocompact 75` must reach the typed surface (75 is a valid percentage the rows do not
// list), and offering an empty menu there would make Enter do nothing at all.
func settingsMenuStage(input string) (key, prefix, filter string, open bool) {
	rest, in := menuOpen(settingsPrefix, input)
	if !in {
		return "", "", "", false
	}
	first, tail, spaced := strings.Cut(rest, " ")
	if spaced && isSettingKey(first) && settingChoices(first) != nil {
		tail = strings.TrimLeft(tail, " ")
		if tail != "-" && !settingChoiceIs(first, tail) && settingChoiceMatches(first, tail) {
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
func settingsRows(stage string, ac autoCompactSetting, window int) []fold.ModelMatch {
	if stage == "" {
		return append(settingsKeyRows(), settingsMenuData(ac, window)...)
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
func settingsKeyRows() []fold.ModelMatch {
	rows := make([]fold.ModelMatch, 0, len(settingsKeys))
	for _, k := range settingsKeys {
		cur := uiText(k)
		val := strconvQuote(cur)
		if cur != textIndex[k].def {
			val += " (factory " + strconvQuote(textIndex[k].def) + ")"
		}
		rows = append(rows, fold.ModelMatch{Ref: k, Name: k, Provider: val + " — " + firstClause(textIndex[k].role)})
	}
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
