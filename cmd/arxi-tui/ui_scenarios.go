package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// This file is the saved tests of the interface (ui_preview's save, expect and forget):
// the checks that something WORKS, kept so that a later change cannot break it unseen.
//
// # Why it exists
//
// ui_preview with keys lets the model check a button once. The next change, ten minutes or
// ten conversations later, can break it, and nobody looks again until the user presses the
// button and nothing happens. A saved test is that same check, kept, and run again after
// every ui_edit the model makes; the answer of the edit says which ones broke.
//
// # What a test is
//
// The facts and keys of a ui_preview call, and expect: pieces of text that must be in what
// the call answers (the screen after the keys, and the lines under it: what was sent to
// the model, which address the browser was asked for, the clipboard, where the program
// ended). A piece that starts with "!" must NOT be there. A test with nothing to expect
// could not fail, so it is refused.
//
// # Why it is its own file, not part of the interface
//
// The interface is one document that /ui undo takes back whole. The tests are what the
// interface must satisfy, not part of it: if undoing a change makes a test fail, that is
// the signal the tests exist to give. They live in ui_tests.json in the config folder.
//
// # What it does not do, said where the model reads it
//
// It does not rerun after what the user types (/ui commands, /ui undo): only after the
// model's ui_edit. It does not roll back a change that breaks a test: the change is
// applied and the answer says what broke. Every run is the isolated copy of ui_simulate.go,
// so packages and the project's own settings are not in it.

const (
	maxScenarios        = 12
	maxScenarioExpect   = 8
	maxScenarioExpectLn = 200
	maxScenarioName     = 40
	// scenariosTotalLimit bounds all the runs of one ui_edit: each is a copy of the program.
	scenariosTotalLimit = 60 * time.Second
	// failScreens is how many failing tests show their screen in an answer; the model
	// pays for it in tokens, and ui_preview with the same keys shows any other.
	failScreens = 2
)

var scenarioNameRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9 ._-]*$`)

// scenario is one saved test.
type scenario struct {
	Name    string        `json:"name"`
	Width   int           `json:"width,omitempty"`
	Height  int           `json:"height,omitempty"`
	Working bool          `json:"working,omitempty"`
	Shells  int           `json:"shells,omitempty"`
	Input   string        `json:"input,omitempty"`
	Chat    []previewLine `json:"chat,omitempty"`
	Keys    []string      `json:"keys,omitempty"`
	Expect  []string      `json:"expect"`
}

type scenarioFile struct {
	Tests []scenario `json:"tests"`
}

func scenariosPath() string {
	d := configDir()
	if d == "" {
		return ""
	}
	return filepath.Join(d, "ui_tests.json")
}

// loadScenarios reads the saved tests; no file is no tests.
func loadScenarios() ([]scenario, error) {
	path := scenariosPath()
	if path == "" {
		return nil, nil
	}
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var f scenarioFile
	dec := json.NewDecoder(strings.NewReader(string(data)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&f); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	if len(f.Tests) > maxScenarios {
		return nil, fmt.Errorf("%s: %d tests, the most is %d", path, len(f.Tests), maxScenarios)
	}
	for i := range f.Tests {
		if err := f.Tests[i].validate(); err != nil {
			return nil, fmt.Errorf("%s: test %d: %w", path, i+1, err)
		}
	}
	return f.Tests, nil
}

// saveScenarios keeps the list; an empty list removes the file.
func saveScenarios(list []scenario) error {
	path := scenariosPath()
	if path == "" {
		return errNoConfigDir
	}
	if len(list) == 0 {
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return err
		}
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	out, err := json.MarshalIndent(scenarioFile{Tests: list}, "", "  ")
	if err != nil {
		return err
	}
	return writeAtomic(path, append(out, '\n'))
}

// validate says what is wrong with a test in words the model can act on.
func (s scenario) validate() error {
	if len(s.Name) > maxScenarioName || !scenarioNameRe.MatchString(s.Name) {
		return fmt.Errorf("the test name %q: use letters, digits, spaces, '.', '-' or '_', at most %d characters (e.g. \"go button opens the menu\")", s.Name, maxScenarioName)
	}
	if len(s.Expect) == 0 {
		return errors.New("a test with nothing to expect cannot fail; pass expect: [\"text that must be on the screen\", \"!text that must not\"]")
	}
	if len(s.Expect) > maxScenarioExpect {
		return fmt.Errorf("expect takes at most %d pieces of text", maxScenarioExpect)
	}
	for _, e := range s.Expect {
		piece := strings.TrimPrefix(e, "!")
		if strings.TrimSpace(piece) == "" {
			return errors.New("an expect piece is empty; write the text of the screen to look for (a leading ! means it must not be there)")
		}
		if strings.ContainsAny(piece, "\n\r") {
			return errors.New("an expect piece is text of one row of the screen; split it into one piece per row")
		}
		if len([]rune(piece)) > maxScenarioExpectLn {
			return fmt.Errorf("an expect piece may be at most %d characters", maxScenarioExpectLn)
		}
	}
	w, h := s.size()
	if msg := previewFactsError(w, h, s.Shells, s.Chat); msg != "" {
		return errors.New(msg)
	}
	if _, err := parseSimKeys(s.Keys); err != nil {
		return err
	}
	return nil
}

func (s scenario) size() (int, int) {
	w, h := s.Width, s.Height
	if w == 0 {
		w = previewDefW
	}
	if h == 0 {
		h = previewDefH
	}
	return w, h
}

// checkExpect returns what of expect the body does not satisfy, one sentence each.
func checkExpect(body string, expect []string) []string {
	var bad []string
	for _, e := range expect {
		if rest, absent := strings.CutPrefix(e, "!"); absent {
			if strings.Contains(body, rest) {
				bad = append(bad, fmt.Sprintf("%q must not be there and it is", rest))
			}
			continue
		}
		if !strings.Contains(body, e) {
			bad = append(bad, fmt.Sprintf("expected %q and it is not there", e))
		}
	}
	return bad
}

// scenarioOutcome is one run of one test.
type scenarioOutcome struct {
	s        scenario
	failures []string
	body     string // the screen and the lines under it
	err      string // set when the test could not be run at all
}

func (o scenarioOutcome) passed() bool { return o.err == "" && len(o.failures) == 0 }

// runScenario runs one test on st in the isolated copy.
func runScenario(ctx context.Context, st interfaceState, s scenario) scenarioOutcome {
	out := scenarioOutcome{s: s}
	w, h := s.size()
	cwd, _ := os.Getwd()
	res, err := simulate(ctx, simRequest{Width: w, Height: h, Shells: s.Shells, Working: s.Working,
		Input: s.Input, Chat: s.Chat, Keys: s.Keys, Cwd: cwd, State: st})
	if err != nil {
		out.err = err.Error()
		return out
	}
	if res.Err != "" {
		out.err = res.Err
		return out
	}
	out.body = simBody(res, s.Keys)
	out.failures = checkExpect(out.body, s.Expect)
	return out
}

// scenarioNote runs every saved test on st and says how they went, "" when there are none.
// It is added to the answer of a ui_edit that was applied.
func (b *uiBridge) scenarioNote(ctx context.Context, st interfaceState) string {
	list, err := loadScenarios()
	if err != nil {
		return "\nTESTS were not run: " + err.Error()
	}
	if len(list) == 0 {
		return ""
	}
	ctx, cancel := context.WithTimeout(ctx, scenariosTotalLimit)
	defer cancel()
	outs := make([]scenarioOutcome, 0, len(list))
	for _, s := range list {
		if ctx.Err() != nil {
			outs = append(outs, scenarioOutcome{s: s, err: fmt.Sprintf("not run: all the tests had %s", scenariosTotalLimit)})
			continue
		}
		outs = append(outs, runScenario(ctx, st, s))
	}
	return "\n" + scenarioReport(outs)
}

// scenarioReport is the answer about a set of runs.
func scenarioReport(outs []scenarioOutcome) string {
	var names []string
	bad := 0
	for _, o := range outs {
		names = append(names, fmt.Sprintf("%q", o.s.Name))
		if !o.passed() {
			bad++
		}
	}
	if bad == 0 {
		return fmt.Sprintf("TESTS: all %d pass after this change (%s).", len(outs), strings.Join(names, ", "))
	}
	var sb strings.Builder
	fmt.Fprintf(&sb, "TESTS: %d of %d FAIL after this change. The change IS applied; fix it, or /ui undo, or replace a test that is out of date (save with the same name) or delete it (forget).\n", bad, len(outs))
	shown := 0
	for _, o := range outs {
		switch {
		case o.err != "":
			fmt.Fprintf(&sb, "  NOT RUN %q: %s\n", o.s.Name, o.err)
		case len(o.failures) > 0:
			fmt.Fprintf(&sb, "  FAIL %q (keys: %s): %s\n", o.s.Name, keysLabel(o.s.Keys), strings.Join(o.failures, "; "))
			if shown < failScreens {
				shown++
				for _, line := range strings.Split(o.body, "\n") {
					sb.WriteString("    | " + line + "\n")
				}
			}
		}
	}
	if shown < bad-countNotRun(outs) {
		sb.WriteString("  (the others: call ui_preview with their keys to see their screens)\n")
	}
	return strings.TrimRight(sb.String(), "\n")
}

func countNotRun(outs []scenarioOutcome) int {
	n := 0
	for _, o := range outs {
		if o.err != "" {
			n++
		}
	}
	return n
}

func keysLabel(keys []string) string {
	if len(keys) == 0 {
		return "none"
	}
	return strings.Join(keys, " ")
}

// scenariosGuide is the TESTS chapter of ui_guide with the tests saved now.
func scenariosGuide() string {
	var sb strings.Builder
	sb.WriteString(`TESTS. A test keeps a check that something WORKS and is run again after every ui_edit you make, so a later change cannot break it unseen. Save one with ui_preview: save:"<name>", the facts and keys you would pass to look (width, height, working, shells, input, chat, keys) and expect: the texts that must be on the screen after the keys. A text starting with ! must NOT be there. expect also matches the lines the answer adds under the screen (SENT TO THE MODEL, BROWSER WAS ASKED, CLIPBOARD, THE PROGRAM ENDED). Save it once the change works (the answer says if it passes now), or before, to watch it fail and then pass. The same name replaces a test; forget:"<name>" deletes one. At most 12; not in plan mode. They are NOT rerun after what the user types, and a failing test does not undo the change: the answer of ui_edit says what broke.
`)
	list, err := loadScenarios()
	switch {
	case err != nil:
		sb.WriteString("Saved tests cannot be read: " + err.Error() + "\n")
	case len(list) == 0:
		sb.WriteString("Saved tests now: none.\n")
	default:
		sb.WriteString("Saved tests now:\n")
		for _, s := range list {
			fmt.Fprintf(&sb, "  %q keys: %s; expect: %s\n", s.Name, keysLabel(s.Keys), quoteAll(s.Expect))
		}
	}
	return sb.String()
}
