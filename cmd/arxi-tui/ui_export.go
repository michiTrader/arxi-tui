package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// /ui export and /ui import move the interface between machines and people as ONE file.
// The file holds the four documents of interfaceState (layout, colours, words, behaviour)
// as plain JSON objects, so a person can read it, diff it and send it.
//
// What a file never holds: packages (they are programs with their own permissions, and a
// file from someone else must not install any), the saved tests and the history.
// An import is the user's own typed line, never a tool of the model, and it goes through
// the same validation as a change the model asks for, then through makeSaved, so
// /ui undo brings back what was there.

const (
	exportFormat     = "arxi-interface"
	exportVersion    = 1
	maxImportBytes   = 1 << 20
	defaultExportExt = ".json"
)

// interfaceFile is the exported document. A part that is absent means "the factory one".
type interfaceFile struct {
	Format    string          `json:"format"`
	Version   int             `json:"version"`
	Exported  string          `json:"exported,omitempty"`
	Scene     json.RawMessage `json:"scene,omitempty"`
	Tokens    json.RawMessage `json:"tokens,omitempty"`
	Texts     json.RawMessage `json:"texts,omitempty"`
	Behaviour json.RawMessage `json:"behaviour,omitempty"`
}

// encodeInterfaceFile turns a state into the bytes of an export.
func encodeInterfaceFile(st interfaceState, at time.Time) ([]byte, error) {
	f := interfaceFile{Format: exportFormat, Version: exportVersion, Exported: at.UTC().Format(time.RFC3339)}
	for _, p := range []struct {
		dst  *json.RawMessage
		part string
		name string
	}{{&f.Scene, st.Scene, "layout"}, {&f.Tokens, st.Tokens, "colours"}, {&f.Texts, st.Texts, "words"}, {&f.Behaviour, st.Behaviour, "behaviour"}} {
		if strings.TrimSpace(p.part) == "" {
			continue
		}
		if !json.Valid([]byte(p.part)) {
			return nil, fmt.Errorf("the saved %s is not valid JSON, so it cannot be exported; /ui reset or /ui history restore fixes that", p.name)
		}
		*p.dst = json.RawMessage(p.part)
	}
	out, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(out, '\n'), nil
}

// decodeInterfaceFile reads an export back into a state and checks it loads.
func decodeInterfaceFile(b []byte) (interfaceState, error) {
	var f interfaceFile
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&f); err != nil {
		return interfaceState{}, fmt.Errorf("this is not an arxi interface file (%v)", err)
	}
	if err := dec.Decode(&struct{}{}); err != io.EOF {
		return interfaceState{}, errors.New("this is not an arxi interface file (something follows the document)")
	}
	if f.Format != exportFormat {
		return interfaceState{}, fmt.Errorf("this is not an arxi interface file: its format is %q, not %q", f.Format, exportFormat)
	}
	if f.Version != exportVersion {
		return interfaceState{}, fmt.Errorf("this file is version %d and this arxi reads version %d; update arxi, or ask whoever sent it to export again", f.Version, exportVersion)
	}
	part := func(m json.RawMessage) string {
		s := strings.TrimSpace(string(m))
		if s == "null" {
			return ""
		}
		return s
	}
	st := interfaceState{Scene: part(f.Scene), Tokens: part(f.Tokens), Texts: part(f.Texts), Behaviour: part(f.Behaviour)}
	doc, _, _, _, err := loadInterface(st)
	if err != nil {
		return interfaceState{}, fmt.Errorf("the file's interface does not load: %w", err)
	}
	// A scene with no root parses and validates, and draws nothing: the screen would be
	// empty. loadInterface lets it through, so a file from someone else is checked here.
	if st.Scene != "" && doc.Root == nil {
		return interfaceState{}, errors.New("the file's interface does not load: its layout has no root, so nothing would be drawn")
	}
	return st, nil
}

// sameJSON reports whether two saved parts say the same thing whatever their spacing: the
// file re-indents each document, so an imported part is the same JSON with other whitespace.
func sameJSON(a, b string) bool {
	if a == b {
		return true
	}
	var x, y bytes.Buffer
	if json.Compact(&x, []byte(a)) != nil || json.Compact(&y, []byte(b)) != nil {
		return false
	}
	return x.String() == y.String()
}

// sameInterface is sameState for what went through a file.
func sameInterface(a, b interfaceState) bool {
	return sameJSON(a.Scene, b.Scene) && sameJSON(a.Tokens, b.Tokens) && sameJSON(a.Texts, b.Texts) && sameJSON(a.Behaviour, b.Behaviour)
}

// exportPath resolves what the user typed; "~/" is the home folder and nothing means a
// dated name in the folder the program runs in.
func exportPath(arg string, at time.Time) string {
	arg = strings.TrimSpace(arg)
	if arg == "" {
		return "arxi-interface-" + at.Format("2006-01-02") + defaultExportExt
	}
	if arg == "~" || strings.HasPrefix(arg, "~/") {
		if h, err := os.UserHomeDir(); err == nil {
			return filepath.Join(h, strings.TrimPrefix(strings.TrimPrefix(arg, "~"), "/"))
		}
	}
	return arg
}

// runExport carries out "/ui export [file]".
func runExport(args string) historyOutcome {
	fail := func(s string) historyOutcome { return historyOutcome{err: "/ui export: " + s} }
	st, err := currentInterface()
	if err != nil {
		return fail(err.Error())
	}
	if emptyState(st) {
		return fail("the interface is the built-in one, so there is nothing of yours to export. Change something first.")
	}
	at := historyNow()
	path := exportPath(args, at)
	data, err := encodeInterfaceFile(st, at)
	if err != nil {
		return fail(err.Error())
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		if os.IsExist(err) {
			return fail(path + " already exists and was left alone. Choose another name: /ui export other-name.json")
		}
		return fail(err.Error())
	}
	if _, err := f.Write(data); err != nil {
		f.Close()
		os.Remove(path)
		return fail(err.Error())
	}
	if err := f.Close(); err != nil {
		os.Remove(path)
		return fail(err.Error())
	}
	return historyOutcome{note: fmt.Sprintf("/ui export: wrote %s (%s). It holds the layout, colours, words and behaviour; not packages, tests or history. /ui import %s loads it.", path, partsHeld(st), path)}
}

// partsHeld names the documents a state has something in.
func partsHeld(st interfaceState) string {
	var p []string
	for _, x := range []struct{ name, v string }{{"layout", st.Scene}, {"colours", st.Tokens}, {"words", st.Texts}, {"behaviour", st.Behaviour}} {
		if x.v != "" {
			p = append(p, x.name)
		}
	}
	return strings.Join(p, ", ")
}

// behaviourSummary says what a behaviour document adds, by name, so an import never adds
// commands, keys or hooks without the user seeing them. A file from someone else is
// code the user did not write: what it binds is listed, not only counted.
func behaviourSummary(bh behaviour) string {
	if bh.empty() {
		return ""
	}
	var names, keys []string
	for _, c := range bh.Commands {
		names = append(names, "/"+c.Name)
	}
	for k := range bh.Keys {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	s := fmt.Sprintf("; it adds %d command(s)", len(bh.Commands))
	if len(names) > 0 {
		s += " " + strings.Join(names, " ")
	}
	s += fmt.Sprintf(", %d key(s)", len(bh.Keys))
	if len(keys) > 0 {
		s += " " + strings.Join(keys, " ")
	}
	return s + fmt.Sprintf(", %d hook(s), %d animation(s)", len(bh.Hooks), len(bh.Animations))
}

// runImport carries out "/ui import <file>".
func runImport(args string, persists bool) historyOutcome {
	fail := func(s string) historyOutcome { return historyOutcome{err: "/ui import: " + s} }
	path := exportPath(args, historyNow())
	if strings.TrimSpace(args) == "" {
		return fail("say which file: /ui import arxi-interface.json")
	}
	if !persists {
		return fail("this scene was started with -scene; an import changes the saved interface, which is not the one on screen")
	}
	fi, err := os.Stat(path)
	if err != nil {
		// The reason is written here, not taken from the system: Windows answers
		// in the language of the machine ("El sistema no puede encontrar el
		// archivo especificado"), so a user whose interface is English reads a
		// refusal in Spanish, and a test that pins the wording would pin it per
		// platform. The common case has a name the user can act on.
		if os.IsNotExist(err) {
			return fail(path + ": no such file")
		}
		return fail(err.Error())
	}
	if fi.IsDir() {
		return fail(path + " is a folder, not a file")
	}
	if fi.Size() > maxImportBytes {
		return fail(fmt.Sprintf("%s is %d bytes; an interface file is never more than %d", path, fi.Size(), maxImportBytes))
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return fail(err.Error())
	}
	st, err := decodeInterfaceFile(b)
	if err != nil {
		return fail(path + ": " + err.Error() + ". Nothing was changed.")
	}
	_, _, _, bh, _ := loadInterface(st)
	cur, err := currentInterface()
	if err != nil {
		return fail(err.Error())
	}
	if sameInterface(cur, st) {
		return fail(path + " is the interface you already have; nothing changed")
	}
	if err := makeSaved(st); err != nil {
		return fail(err.Error())
	}
	label := "imported " + filepath.Base(path)
	note := fmt.Sprintf("/ui import: %s loaded (%s)%s. It REPLACED your layout, colours, words and behaviour; /ui undo goes back. Packages and tests were not touched.", path, partsHeld(st), behaviourSummary(bh))
	if err := recordVersion(label); err != nil {
		note += " (not kept as a version: " + err.Error() + ")"
	}
	return historyOutcome{restored: true, state: st, label: label, note: note}
}

// uiStateLine reports a typed /ui line about the saved interface as a whole: history,
// export or import. It only looks at the words, so the loop can ask before the key is Enter.
func uiStateLine(input string) (verb, args string, ok bool) {
	if a, yes := uiHistoryLine(input); yes {
		return "history", a, true
	}
	f := strings.Fields(input)
	if len(f) >= 2 && f[0] == "/ui" && (f[1] == "export" || f[1] == "import") {
		return f[1], strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(input), "/ui")), f[1])), true
	}
	return "", "", false
}

// runStateLine carries out what uiStateLine found.
func runStateLine(verb, args string, persists bool) historyOutcome {
	switch verb {
	case "export":
		return runExport(args)
	case "import":
		return runImport(args, persists)
	}
	return runHistoryLine(args, persists)
}
