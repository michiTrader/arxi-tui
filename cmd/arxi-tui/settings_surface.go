package main

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// This file is the settings surface: ONE place that lists every setting with its value
// and what it does, and changes one with the same checks `/ui text` makes.
//
//	/settings list                 every setting: value, and (factory) when you changed it
//	/settings <name>               one setting, in full: what it does, its value, its limits
//	/settings <name> <value>       change it; an empty value (/settings <name> -) puts the factory one back
//	/settings autocompact <off|N>  the autocompact choice, kept in settings.json
//
// Before this, a user who wanted to know "what can I change?" had to read /ui guide and
// know that a setting is called a "text". A setting here IS one of the registry's text
// keys (ui_texts.go): the same names, the same validation (validText), the same file
// (texts.json), the same undo (/ui undo). The surface adds no second store, so what it
// writes and what the agent's ui_edit writes can never disagree.
//
// The few settings that are not text keys are named as what they are: autocompact lives
// in settings.json; the parts of the screen that are hidden are session state, shown but
// not written (they are changed with /ui hide and /ui show).

// settingsKeys are the text keys that are settings (a number, a yes/no, a choice), not
// wording. The sentences of the interface (menu descriptions, hints, titles) are edited
// with /ui text and are not listed here: a list of 150 lines would hide the 20 that matter.
var settingsKeys = []string{
	"scroll.wheel_step", "scroll.page_overlap", "input.max_rows",
	"chat.scrollbar", "chat.light", "effort.default", "resume.scope", "status.narrow",
	"paste.fold_lines", "paste.fold_chars",
	"tool.inline", "tool.arg_rows", "tool.output_rows", "tool.diff_rows", "tool.approval_rows",
	"chat.user.max_lines", "chat.answer.indent", "chat.turn_gap", "chat.usage_gap",
}

func isSettingKey(k string) bool {
	for _, s := range settingsKeys {
		if s == k {
			return true
		}
	}
	return false
}

// settingsKeysDef declares the three settings that used to be constants in the code. The
// factory value of each is "auto" or the old number, so an interface with no override
// behaves exactly as before.
func settingsKeysDef(add func(key, def, role string, paragraph bool)) {
	add("scroll.wheel_step", "auto", "how many rows one notch of the mouse wheel (or ctrl+up/down) scrolls the chat: a number from 1 to 50, or \"auto\" (3, or 1 on a phone without a mouse)", false)
	add("scroll.page_overlap", "4", "how many rows PgUp/PgDn keep from the page you were on, so the eye keeps its place: a number from 0 to 20 (a page is the screen height less this)", false)
	add("input.max_rows", "auto", "the most rows the input box takes before it becomes a window that follows the caret: a number from 3 to 200, or \"auto\" (a third of the screen, at least 3)", false)
}

const (
	maxWheelStep   = 50
	maxPageOverlap = 20
	minInputRows   = 3
	maxInputRows   = 200
)

// validSettingValue is the check for the three settings of this file, called from
// validChatLook so `/ui text`, the agent's ui_edit and `/settings` refuse the same values.
func validSettingValue(key, val string) error {
	v := strings.ToLower(strings.TrimSpace(val))
	whole := func(lo, hi int, allowAuto bool) error {
		if allowAuto && v == "auto" {
			return nil
		}
		n, err := strconv.Atoi(v)
		if err != nil || n < lo || n > hi {
			if allowAuto {
				return fmt.Errorf("text %q is a whole number from %d to %d, or auto; %q is not", key, lo, hi, val)
			}
			return fmt.Errorf("text %q is a whole number from %d to %d; %q is not", key, lo, hi, val)
		}
		return nil
	}
	switch key {
	case "scroll.wheel_step":
		return whole(1, maxWheelStep, true)
	case "scroll.page_overlap":
		return whole(0, maxPageOverlap, false)
	case "input.max_rows":
		return whole(minInputRows, maxInputRows, true)
	}
	return nil
}

// settingNumber reads a setting that is a number or "auto": the number, or def for auto
// and for anything that is not a valid number (the setting is checked when it is written,
// so this only meets a bad value if the file was edited by hand).
func settingNumber(key string, def, lo, hi int) int {
	v := strings.ToLower(strings.TrimSpace(uiText(key)))
	if v == "" || v == "auto" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < lo || n > hi {
		return def
	}
	return n
}

// settingsSurfaceLine reads `/settings list`, `/settings <setting>` and
// `/settings <setting> <value>` (and `/settings autocompact <value>`), returning what
// follows "/settings". Everything else (`/settings`, `/settings auto`) is the menu's.
func settingsSurfaceLine(input string) (args string, ok bool) {
	rest, has := strings.CutPrefix(strings.TrimLeft(input, " \t"), "/settings")
	if !has || rest == "" || (rest[0] != ' ' && rest[0] != '\t') {
		return "", false
	}
	args = strings.TrimSpace(rest)
	first, tail, _ := strings.Cut(args, " ")
	switch {
	case first == "list":
		return args, true
	case isSettingKey(first):
		return args, true
	case first == "autocompact" && strings.TrimSpace(tail) != "":
		return args, true
	}
	return "", false
}

// settingsOutcome is what a /settings line did. Exactly one of the four things happens:
// a note to show, a words change to keep, an autocompact change to keep, or an error.
type settingsOutcome struct {
	note         string
	words        userTexts
	wordsChanged bool
	ac           autoCompactSetting
	acChanged    bool
	err          string
}

// runSettingsSurface carries out one /settings line. It is pure: it reads the current
// words, autocompact choice and hidden parts, and returns what to do, so every rule of
// the surface is a plain function call in a test.
func runSettingsSurface(args string, words userTexts, ac autoCompactSetting, hidden map[string]bool) settingsOutcome {
	first, tail, _ := strings.Cut(args, " ")
	tail = strings.TrimSpace(tail)
	switch {
	case first == "list":
		return settingsOutcome{note: settingsListing(words, ac, hidden)}
	case first == "autocompact":
		return setAutoCompact(tail, ac)
	case isSettingKey(first):
		if tail == "" {
			return settingsOutcome{note: settingDetail(first)}
		}
		val := tail
		if val == "-" || val == textIndex[first].def {
			val = "" // the empty value puts the factory one back; so does saying the factory value
		}
		next, err := applyTexts(words, map[string]string{first: val})
		if err != nil {
			return settingsOutcome{err: "/settings: " + err.Error()}
		}
		if sameTexts(next, words) {
			return settingsOutcome{note: first + " is already " + describeValue(first)}
		}
		return settingsOutcome{words: next, wordsChanged: true,
			note: "/settings: " + first + " is now " + describeValue(first, next)}
	}
	return settingsOutcome{err: "/settings: unknown setting " + strconv.Quote(first) + "; /settings list names them all"}
}

func setAutoCompact(val string, cur autoCompactSetting) settingsOutcome {
	v := strings.ToLower(strings.TrimSpace(val))
	if v == "off" {
		next := settingsAfterPick(cur, settingOff)
		return settingsOutcome{ac: next, acChanged: true, note: "/settings: autocompact off"}
	}
	n, err := strconv.Atoi(strings.TrimSuffix(v, "%"))
	if err != nil || n < minAutoCompactPercent || n > maxAutoCompactPercent {
		return settingsOutcome{err: fmt.Sprintf("/settings: autocompact is off, or a percentage from %d to %d (of the model's context window); %q is not", minAutoCompactPercent, maxAutoCompactPercent, val)}
	}
	next := settingsAfterPick(cur, settingRef(n))
	return settingsOutcome{ac: next, acChanged: true, note: fmt.Sprintf("/settings: autocompact on at %d%% of the context window", n)}
}

// describeValue is a setting's value as the list shows it: the value, and "(factory)"
// when it is the factory one. With no words given it reads the live ones.
func describeValue(key string, words ...userTexts) string {
	cur := uiText(key)
	if len(words) > 0 {
		if v, ok := words[0][key]; ok {
			cur = v
		} else {
			cur = textIndex[key].def
		}
	}
	return strconvQuote(cur) + factoryMark(key, cur)
}

func factoryMark(key, cur string) string {
	if cur == textIndex[key].def {
		return " (factory)"
	}
	return " (factory: " + strconvQuote(textIndex[key].def) + ")"
}

// settingDetail is one setting in full.
func settingDetail(key string) string {
	d := textIndex[key]
	return fmt.Sprintf("%s = %s\n%s\nChange it: /settings %s <value>   ·   factory: /settings %s -", key, describeValue(key), d.role, key, key)
}

// settingsListing is every setting with its value. A short line each; /settings <name>
// gives the full explanation. The hidden parts and autocompact are named as what they are.
func settingsListing(words userTexts, ac autoCompactSetting, hidden map[string]bool) string {
	var b strings.Builder
	b.WriteString("Settings. /settings <name> shows one in full; /settings <name> <value> changes it; /settings <name> - puts the factory value back.\n")
	width := 0
	for _, k := range settingsKeys {
		if len(k) > width {
			width = len(k)
		}
	}
	for _, k := range settingsKeys {
		cur := uiText(k)
		mark := ""
		if cur != textIndex[k].def {
			mark = "  (factory " + strconvQuote(textIndex[k].def) + ")"
		}
		fmt.Fprintf(&b, "  %-*s = %s%s — %s\n", width, k, strconvQuote(cur), mark, firstClause(textIndex[k].role))
	}
	state := "off"
	if ac.On {
		state = fmt.Sprintf("on at %d%%", clampPercent(ac.Percent))
	}
	fmt.Fprintf(&b, "  %-*s = %s — compacts the conversation on its own at this share of the window (/settings autocompact <off|%d-%d>)\n",
		width, "autocompact", state, minAutoCompactPercent, maxAutoCompactPercent)
	fmt.Fprintf(&b, "  %-*s = %s — parts you hid with /ui hide <id>; /ui show <id> brings them back; not kept between sessions",
		width, "hidden parts", hiddenList(hidden))
	return b.String()
}

func hiddenList(hidden map[string]bool) string {
	var ids []string
	for id, h := range hidden {
		if h {
			ids = append(ids, id)
		}
	}
	if len(ids) == 0 {
		return "none"
	}
	sort.Strings(ids)
	return strings.Join(ids, ", ")
}

// firstClause cuts an explanation to its first sentence-ish part, for the one-line list.
func firstClause(s string) string {
	for _, sep := range []string{": ", "; ", ". "} {
		if i := strings.Index(s, sep); i > 20 {
			s = s[:i]
			break
		}
	}
	if r := []rune(s); len(r) > 90 {
		s = string(r[:90]) + "…"
	}
	return s
}
