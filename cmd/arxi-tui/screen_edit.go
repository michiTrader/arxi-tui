package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/michiTrader/arxi_tui/internal/scene"
	"github.com/michiTrader/arxi_tui/internal/theme"
)

// The model changes a screen the way it changes everything else: through ui_edit, with the
// user shown a diff and asked. `screens: {"flow": "<json>"}` writes <settings>/screens/flow.json,
// `screens: {"flow": ""}` deletes it so the built-in screen comes back. A screen is read again
// each time it opens (screen_override.go), so an approved change shows on the next open.

// screenEdit is a change to screen files that was drafted and validated but not written.
type screenEdit struct {
	// write maps a screen name to its new text, "" meaning delete the file.
	write map[string]string
	// base is what each file held when the change was drafted ("" when there was none), so
	// the loop can refuse the change if a file moved on while the question was open.
	base map[string]string
	diff string
}

// screenFileText is what the file for name holds now, and whether there is one.
func screenFileText(name string) (string, bool) {
	dir := screensDir()
	if dir == "" {
		return "", false
	}
	data, err := os.ReadFile(filepath.Join(dir, name+".json"))
	if err != nil {
		return "", false
	}
	return string(data), true
}

// screenSource is the text of the screen as it is drawn now: the profile's file when there
// is one that loads, otherwise the built-in. It is what the model reads before it edits.
func screenSource(name string) (string, error) {
	build, ok := screenBuiltins[name]
	if !ok {
		return "", fmt.Errorf("%q is not a screen; the screens are %s", name, screenNames())
	}
	if text, has := screenFileText(name); has {
		if _, err := parseScreen([]byte(text)); err == nil {
			return text, nil
		}
	}
	d, err := build()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(d.Source())), nil
}

func screenNames() string {
	names := make([]string, 0, len(screenBuiltins))
	for n := range screenBuiltins {
		names = append(names, n)
	}
	sort.Strings(names)
	return strings.Join(names, ", ")
}

// draftScreens validates a change to screens against the theme and builds what the user is
// shown. It writes nothing. everyday says this is the user's own interface: with -scene or
// -raw the folder is not read, so a file written now would do nothing and the change is refused.
func draftScreens(want map[string]string, thm *theme.Theme, everyday bool) (*screenEdit, error) {
	if !everyday {
		return nil, errors.New("screens belong to the everyday interface, and this one was started with -scene or -raw; nothing was changed")
	}
	if screensDir() == "" {
		return nil, errors.New("there is no settings folder to keep screens in")
	}
	names := make([]string, 0, len(want))
	for n := range want {
		names = append(names, n)
	}
	sort.Strings(names)
	e := &screenEdit{write: map[string]string{}, base: map[string]string{}}
	var diff strings.Builder
	for _, name := range names {
		text := stripFence(want[name])
		before, hasFile := screenFileText(name)
		now, err := screenSource(name)
		if err != nil {
			return nil, fmt.Errorf("screens.%s: %w", name, err)
		}
		var after string
		if strings.TrimSpace(text) == "" {
			if !hasFile {
				continue // nothing to take away: the built-in is already drawn
			}
			builtin, err := screenBuiltins[name]()
			if err != nil {
				return nil, fmt.Errorf("screens.%s: %w", name, err)
			}
			after = strings.TrimSpace(string(builtin.Source()))
			text = ""
		} else {
			d, err := parseScreen([]byte(text))
			if err != nil {
				return nil, fmt.Errorf("screens.%s: %w (nothing was written)", name, err)
			}
			if errs := scene.ValidateTokens(d, thm); len(errs) > 0 {
				return nil, fmt.Errorf("screens.%s: %v (the defined tokens are listed in ui_guide)", name, errs[0])
			}
			after = strings.TrimSpace(string(d.Source()))
		}
		d, err := uiDiffText([]byte(now), []byte(after))
		if err != nil {
			return nil, fmt.Errorf("screens.%s: %w", name, err)
		}
		if d == "" && text != "" && hasFile {
			continue // the file already says this
		}
		e.write[name] = text
		e.base[name] = before
		fmt.Fprintf(&diff, "      screens/%s.json:\n%s", name, d)
	}
	if len(e.write) == 0 {
		return nil, nil
	}
	e.diff = diff.String()
	return e, nil
}

// fresh reports whether every file the change touches still holds what it held when the
// change was drafted.
func (e *screenEdit) fresh() bool {
	for name, want := range e.base {
		got, _ := screenFileText(name)
		if got != want {
			return false
		}
	}
	return true
}

// applyScreenEdit writes (or deletes) the files. On a failure part-way it puts back what it
// had changed, so a change is all or nothing.
func applyScreenEdit(e *screenEdit) error {
	if e == nil {
		return nil
	}
	dir := screensDir()
	if dir == "" {
		return errors.New("there is no settings folder to keep screens in")
	}
	var done []string
	undo := func() {
		for _, n := range done {
			p := filepath.Join(dir, n+".json")
			if old := e.base[n]; old == "" {
				_ = os.Remove(p)
			} else {
				_ = writeAtomic(p, []byte(old))
			}
		}
	}
	names := make([]string, 0, len(e.write))
	for n := range e.write {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		p := filepath.Join(dir, n+".json")
		if e.write[n] == "" {
			if err := os.Remove(p); err != nil && !os.IsNotExist(err) {
				undo()
				return fmt.Errorf("the %s screen could not be removed: %w", n, err)
			}
		} else {
			if err := os.MkdirAll(dir, 0o700); err != nil {
				undo()
				return fmt.Errorf("the screens folder could not be made: %w", err)
			}
			if err := writeAtomic(p, []byte(e.write[n])); err != nil {
				undo()
				return fmt.Errorf("the %s screen could not be written: %w", n, err)
			}
		}
		done = append(done, n)
	}
	return nil
}

// screensGuide is the SCREENS chapter of ui_guide.
func screensGuide() string {
	return "SCREENS. Besides the main scene, /provider, /team, /flow, /auto and the community installer are screens, each a scene document of its own: " + screenNames() + ". " +
		"To redo one pass screens: {\"flow\": \"<the whole new scene as JSON>\"}; \"\" brings the built-in one back. Read the one in use first with ui_guide {\"screen\":\"flow\"}, because a screen may only use the binds it is given: hub, team, flow and auto get hub.title, hub.rows (row.line, row.status), hub.hint, hub.detail and host.scene.error; the installer gets the community.* binds. " +
		"Keys are not part of a screen: the app still handles them, so you can change how it looks, not what a key does. A screen is read again each time it opens, so the change shows the next time the user opens it. ui_preview does not draw screens; say what you changed and ask the user to open it. /ui undo does not undo a screen; passing \"\" does."
}
