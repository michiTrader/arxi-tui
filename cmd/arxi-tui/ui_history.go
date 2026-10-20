package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/michiTrader/arxi_tui/internal/defaultscene"
)

// This file is the history of the interface: /ui history, diff, restore and named versions.
//
// # Why it exists
//
// /ui undo goes back ONE step. The interface is now something the model rebuilds all day,
// and the change that was good is often three changes ago. Without a history the only way
// back to it is to ask the model to remember what it was.
//
// # What it keeps
//
// Every saved state of the interface (layout, colours, words and behaviour: the four
// documents /ui undo already moves together) is a numbered version, with when it was made
// and one line saying what happened ("the interface was changed", "/ui color ..."). A
// version can be given a name ("before the demo") and is then never dropped to make room.
//
// # What it does not keep
//
//   - Packages. They are not part of the four documents, and a package that was approved
//     is not something to bring back by a restore. /ui undo still undoes the last package
//     change; the history does not.
//   - The saved tests (ui_tests.json): they are what the interface must satisfy, and they
//     are not rewound with it.
//
// # Who may use it
//
// The user, by typing. The model can read the list (ui_guide) but not restore: going back
// is a decision about the user's own screen, and a restore is as strong as /ui reset.
//
// # How it is kept
//
// One file, ui_history.json, in the config folder: at most 50 versions and about 2 MB, the
// oldest unnamed one dropped first. A file that cannot be read is said, never overwritten:
// the history is the user's, and one bad byte must not erase it.

const (
	maxVersions     = 50
	maxNamed        = 20
	maxHistoryBytes = 2 << 20
	maxVersionName  = 40
	maxVersionLabel = 110
	listedVersions  = 25
	guideVersions   = 8
)

var versionNameRe = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9 ._-]*$`)

// historyNow is the clock; a test replaces it.
var historyNow = time.Now

type uiVersion struct {
	ID    int            `json:"id"`
	At    string         `json:"at"`
	Label string         `json:"label"`
	Name  string         `json:"name,omitempty"`
	State interfaceState `json:"state"`
}

type uiHistoryFile struct {
	Next     int         `json:"next"`
	Versions []uiVersion `json:"versions"`
}

func uiHistoryPath() string {
	d := configDir()
	if d == "" {
		return ""
	}
	return filepath.Join(d, "ui_history.json")
}

// loadUIHistory reads the history; no file is an empty history.
func loadUIHistory() (uiHistoryFile, error) {
	path := uiHistoryPath()
	if path == "" {
		return uiHistoryFile{}, errNoConfigDir
	}
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return uiHistoryFile{Next: 1}, nil
	}
	if err != nil {
		return uiHistoryFile{}, err
	}
	var h uiHistoryFile
	if err := json.Unmarshal(data, &h); err != nil {
		return uiHistoryFile{}, fmt.Errorf("%s cannot be read (%v); it was left as it is, so fix or delete it", path, err)
	}
	if h.Next < 1 {
		h.Next = 1
	}
	for _, v := range h.Versions {
		if v.ID >= h.Next {
			h.Next = v.ID + 1
		}
	}
	return h, nil
}

func saveUIHistory(h uiHistoryFile) error {
	path := uiHistoryPath()
	if path == "" {
		return errNoConfigDir
	}
	h.trim()
	out, err := json.Marshal(h)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return writeAtomic(path, out)
}

// trim drops the oldest unnamed versions until the history fits.
func (h *uiHistoryFile) trim() {
	size := func() int {
		b, _ := json.Marshal(h)
		return len(b)
	}
	for len(h.Versions) > 1 && (len(h.Versions) > maxVersions || size() > maxHistoryBytes) {
		dropped := false
		// The newest is never dropped: it is the interface on screen.
		for i := 0; i < len(h.Versions)-1; i++ {
			if h.Versions[i].Name == "" {
				h.Versions = append(h.Versions[:i:i], h.Versions[i+1:]...)
				dropped = true
				break
			}
		}
		if !dropped {
			return
		}
	}
}

func sameState(a, b interfaceState) bool {
	return a.Scene == b.Scene && a.Tokens == b.Tokens && a.Texts == b.Texts && a.Behaviour == b.Behaviour
}

func emptyState(s interfaceState) bool {
	return s.Scene == "" && s.Tokens == "" && s.Texts == "" && s.Behaviour == ""
}

func (h uiHistoryFile) find(id int) (uiVersion, bool) {
	for _, v := range h.Versions {
		if v.ID == id {
			return v, true
		}
	}
	return uiVersion{}, false
}

// resolve reads "3" as version 3 and anything else as the name of a version.
func (h uiHistoryFile) resolve(ref string) (uiVersion, error) {
	ref = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(ref), "#"))
	if ref == "" {
		return uiVersion{}, errors.New("say which version: its number (see /ui history) or its name")
	}
	if n, err := strconv.Atoi(ref); err == nil {
		if v, ok := h.find(n); ok {
			return v, nil
		}
		return uiVersion{}, fmt.Errorf("there is no version %d; /ui history lists them (an old one may have been dropped to make room)", n)
	}
	for _, v := range h.Versions {
		if strings.EqualFold(v.Name, ref) {
			return v, nil
		}
	}
	return uiVersion{}, fmt.Errorf("there is no version named %q; /ui history lists them", ref)
}

// recordVersion keeps the interface as it is saved now as a new version. The first time,
// the state it replaced is kept too (the one /ui undo would return to), so the history
// does not begin after the change the user may want back.
func recordVersion(label string) error {
	cur, err := currentInterface()
	if err != nil {
		return err
	}
	h, err := loadUIHistory()
	if err != nil {
		return err
	}
	if n := len(h.Versions); n > 0 && sameState(h.Versions[n-1].State, cur) {
		return nil
	}
	now := historyNow().UTC().Format(time.RFC3339)
	if len(h.Versions) == 0 {
		if b, err := os.ReadFile(previousInterfacePath()); err == nil {
			var prev interfaceState
			if json.Unmarshal(b, &prev) == nil && !emptyState(prev) && !sameState(prev, cur) {
				prev.Packages = nil
				h.Versions = append(h.Versions, uiVersion{ID: h.Next, At: now, Label: "before the history began", State: prev})
				h.Next++
			}
		}
	}
	h.Versions = append(h.Versions, uiVersion{ID: h.Next, At: now, Label: tidyLabel(label), State: cur})
	h.Next++
	return saveUIHistory(h)
}

func tidyLabel(s string) string {
	s = cleanWarnText(s)
	if r := []rune(s); len(r) > maxVersionLabel {
		s = string(r[:maxVersionLabel-1]) + "…"
	}
	return s
}

// partsChanged names the documents that differ between two states.
func partsChanged(from, to interfaceState) string {
	var parts []string
	if from.Scene != to.Scene {
		parts = append(parts, "layout")
	}
	if from.Tokens != to.Tokens {
		parts = append(parts, "colours")
	}
	if from.Texts != to.Texts {
		parts = append(parts, "words")
	}
	if from.Behaviour != to.Behaviour {
		parts = append(parts, "behaviour")
	}
	if len(parts) == 0 {
		return "nothing"
	}
	return strings.Join(parts, ", ")
}

func versionTime(v uiVersion) string {
	t, err := time.Parse(time.RFC3339, v.At)
	if err != nil {
		return v.At
	}
	return t.Local().Format("2006-01-02 15:04")
}

// historyList is /ui history: the versions, newest first.
func historyList() (string, error) {
	h, err := loadUIHistory()
	if err != nil {
		return "", err
	}
	if len(h.Versions) == 0 {
		return "/ui history: no versions yet. Every change you or the model make to the interface is kept here from now on (/ui undo goes back one step).", nil
	}
	cur, _ := currentInterface()
	var sb strings.Builder
	last := h.Versions[len(h.Versions)-1]
	sb.WriteString("/ui history (newest first; /ui history diff|restore|name <number or name>):\n")
	if !sameState(last.State, cur) {
		sb.WriteString("  (the saved interface is not any of these yet: it has changes no version holds)\n")
	}
	shown := 0
	for i := len(h.Versions) - 1; i >= 0; i-- {
		v := h.Versions[i]
		if shown == listedVersions {
			fmt.Fprintf(&sb, "  … and %d older\n", i+1)
			break
		}
		shown++
		var before interfaceState
		if i > 0 {
			before = h.Versions[i-1].State
		}
		mark := "    "
		if i == len(h.Versions)-1 && sameState(v.State, cur) {
			mark = "now "
		}
		name := ""
		if v.Name != "" {
			name = " [" + v.Name + "]"
		}
		fmt.Fprintf(&sb, "  %s#%d  %s  %s (%s)%s\n", mark, v.ID, versionTime(v), v.Label, partsChanged(before, v.State), name)
	}
	return strings.TrimRight(sb.String(), "\n"), nil
}

// partDiff is the change of one document from before to now, "" when it did not change.
func partDiff(title, from, to string, fallback string) string {
	if from == to {
		return ""
	}
	if from == "" {
		from = fallback
	}
	if to == "" {
		to = fallback
	}
	d, err := uiDiffText([]byte(from), []byte(to))
	if err != nil {
		return "  " + title + ": changed (it could not be compared: " + err.Error() + ")\n"
	}
	if d == "" {
		return ""
	}
	return "  " + title + ":\n" + d + "\n"
}

// historyDiff is what changed from a version to the interface saved now.
func historyDiff(ref string) (string, error) {
	h, err := loadUIHistory()
	if err != nil {
		return "", err
	}
	v, err := h.resolve(ref)
	if err != nil {
		return "", err
	}
	cur, err := currentInterface()
	if err != nil {
		return "", err
	}
	body := partDiff("layout", v.State.Scene, cur.Scene, string(defaultscene.JSON)) +
		partDiff("colours", v.State.Tokens, cur.Tokens, "{}") +
		partDiff("words", v.State.Texts, cur.Texts, "{}") +
		partDiff("behaviour", v.State.Behaviour, cur.Behaviour, "{}")
	if body == "" {
		return fmt.Sprintf("/ui history diff: the interface is the same as #%d.", v.ID), nil
	}
	return fmt.Sprintf("/ui history diff: what changed since #%d (%s, %s):\n", v.ID, versionTime(v), v.Label) + strings.TrimRight(body, "\n"), nil
}

// historyName gives a version a name; the same name moves from the version that had it.
func historyName(ref, name string) (string, error) {
	name = strings.TrimSpace(name)
	if len(name) > maxVersionName || !versionNameRe.MatchString(name) {
		return "", fmt.Errorf("the name %q: start with a letter, then letters, digits, spaces, '.', '-' or '_', at most %d characters (a number alone would be read as a version)", name, maxVersionName)
	}
	h, err := loadUIHistory()
	if err != nil {
		return "", err
	}
	if len(h.Versions) == 0 {
		return "", errors.New("there are no versions yet; make a change first")
	}
	idx := len(h.Versions) - 1
	if ref != "" {
		v, err := h.resolve(ref)
		if err != nil {
			return "", err
		}
		for i := range h.Versions {
			if h.Versions[i].ID == v.ID {
				idx = i
			}
		}
	}
	named := 0
	for i := range h.Versions {
		if h.Versions[i].Name != "" && !strings.EqualFold(h.Versions[i].Name, name) {
			named++
		}
	}
	if h.Versions[idx].Name == "" && named >= maxNamed {
		return "", fmt.Errorf("there are already %d named versions, the most; a name is never dropped by itself, so rename one first", maxNamed)
	}
	moved := ""
	for i := range h.Versions {
		if strings.EqualFold(h.Versions[i].Name, name) && i != idx {
			moved = fmt.Sprintf(" (it was on #%d)", h.Versions[i].ID)
			h.Versions[i].Name = ""
		}
	}
	h.Versions[idx].Name = name
	if err := saveUIHistory(h); err != nil {
		return "", err
	}
	return fmt.Sprintf("/ui history: #%d is now called %q%s. /ui history restore %s brings it back.", h.Versions[idx].ID, name, moved, name), nil
}

// makeSaved makes st the saved interface and keeps what was saved as the one /ui undo
// returns to. A restore and an import both go through it.
func makeSaved(st interfaceState) error {
	cur, err := currentInterface()
	if err != nil {
		return err
	}
	cb, _ := json.Marshal(cur)
	if err := os.MkdirAll(configDir(), 0o700); err != nil {
		return err
	}
	if err := writeAtomic(previousInterfacePath(), cb); err != nil {
		return err
	}
	return writeInterface(st)
}

// historyRestore makes a version the saved interface. What was saved is kept as the one
// /ui undo returns to, and the restore is itself a new version, so nothing is lost.
func historyRestore(ref string) (interfaceState, string, error) {
	h, err := loadUIHistory()
	if err != nil {
		return interfaceState{}, "", err
	}
	v, err := h.resolve(ref)
	if err != nil {
		return interfaceState{}, "", err
	}
	if _, _, _, _, err := loadInterface(v.State); err != nil {
		return interfaceState{}, "", fmt.Errorf("#%d no longer loads (%v); it was left alone", v.ID, err)
	}
	if err := makeSaved(v.State); err != nil {
		return interfaceState{}, "", err
	}
	label := fmt.Sprintf("restored #%d", v.ID)
	if v.Name != "" {
		label += " " + strconv.Quote(v.Name)
	}
	return v.State, label, nil
}

// historyOutcome is what one /ui history line decided, for the loop to carry out.
type historyOutcome struct {
	note     string
	err      string
	restored bool
	state    interfaceState
	label    string
}

// runHistoryLine carries out the words after "/ui history". It is pure but for the files,
// so every rule is a plain call in a test.
func runHistoryLine(args string, persists bool) historyOutcome {
	f := strings.Fields(args)
	fail := func(err error) historyOutcome { return historyOutcome{err: "/ui history: " + err.Error()} }
	if len(f) == 0 || f[0] == "list" {
		note, err := historyList()
		if err != nil {
			return fail(err)
		}
		return historyOutcome{note: note}
	}
	switch f[0] {
	case "diff":
		note, err := historyDiff(strings.Join(f[1:], " "))
		if err != nil {
			return fail(err)
		}
		return historyOutcome{note: note}
	case "name":
		if len(f) < 2 {
			return fail(errors.New("name takes a name, or a version and a name: /ui history name before the demo, /ui history name 4 before the demo"))
		}
		ref, name := "", strings.Join(f[1:], " ")
		if _, err := strconv.Atoi(f[1]); err == nil {
			if len(f) < 3 {
				return fail(errors.New("name the version with a word: /ui history name " + f[1] + " before the demo"))
			}
			ref, name = f[1], strings.Join(f[2:], " ")
		}
		note, err := historyName(ref, name)
		if err != nil {
			return fail(err)
		}
		return historyOutcome{note: note}
	case "restore":
		if !persists {
			return historyOutcome{err: "/ui history restore: this scene was started with -scene; restoring changes the saved interface, which is not the one on screen"}
		}
		st, label, err := historyRestore(strings.Join(f[1:], " "))
		if err != nil {
			return fail(err)
		}
		note := "/ui history: " + label + ". /ui undo goes back to what you had. Packages and the saved tests were not touched."
		if err := recordVersion(label); err != nil {
			note += " (not kept as a version: " + err.Error() + ")"
		}
		return historyOutcome{restored: true, state: st, label: label, note: note}
	}
	return fail(fmt.Errorf("%q is not something history does; use list, diff <version>, restore <version> or name [<version>] <name>", f[0]))
}

// uiHistoryLine reports a /ui history line and returns what follows "history".
func uiHistoryLine(input string) (string, bool) {
	f := strings.Fields(input)
	if len(f) < 2 || f[0] != "/ui" || f[1] != "history" {
		return "", false
	}
	return strings.Join(f[2:], " "), true
}

// historyGuide is the HISTORY paragraph of ui_guide: what exists, and the last versions,
// so the model knows what the user can go back to and does not claim it can do it.
func historyGuide() string {
	var sb strings.Builder
	sb.WriteString("HISTORY. The user can go back one step with /ui undo, or to any earlier version with /ui history (list, diff <n>, restore <n or name>, name <name>). You cannot restore or import; do not say you did. /ui export [file] and /ui import <file> move the whole interface as one file (the user types them). Packages and tests are not rewound.\n")
	h, err := loadUIHistory()
	if err != nil || len(h.Versions) == 0 {
		return sb.String()
	}
	sb.WriteString("Last versions:\n")
	from := len(h.Versions) - guideVersions
	if from < 0 {
		from = 0
	}
	for i := len(h.Versions) - 1; i >= from; i-- {
		v := h.Versions[i]
		name := ""
		if v.Name != "" {
			name = " [" + v.Name + "]"
		}
		fmt.Fprintf(&sb, "  #%d %s%s\n", v.ID, v.Label, name)
	}
	return sb.String()
}
