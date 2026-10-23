package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/michiTrader/arxi_tui/internal/ext"
	"github.com/michiTrader/arxi_tui/internal/scene"
)

// screenBuiltins is every screen a profile may replace, by the name of its file. The
// loader, the model's ui_edit and the guide all read this one list.
var screenBuiltins = map[string]func() (*scene.Document, error){
	"hub":       loadHubScene,
	"team":      loadTeamScene,
	"flow":      loadFlowScene,
	"auto":      loadAutoScene,
	"installer": ext.LiveInstallerScene,
}

// parseScreen is the one rule for "this file is a usable screen": it parses, has a root,
// validates, and carries no warning. A warning is what the engine says when it will draw a
// placeholder or ignore a key (an unknown node type, a misspelled property): for the main
// scene that is tolerated, but a screen a profile wrote must draw what it says, so a screen
// with one is refused and the built-in one is shown with the reason.
func parseScreen(data []byte) (*scene.Document, error) {
	d, err := scene.ParseDocument(data)
	if err != nil {
		return nil, err
	}
	if err := d.RefuseEmpty(); err != nil {
		return nil, err
	}
	if err := d.Validate(); err != nil {
		return nil, err
	}
	if w := d.Warnings(); len(w) > 0 {
		return nil, fmt.Errorf("%s (the engine would ignore it)", w[0].String())
	}
	return d, nil
}

// A screen is a scene document that replaces the scene on display while it is open: the
// providers hub, /team, /flow, /auto. They ship inside the binary, and a profile may replace
// any of them by putting a document of the same name in the settings folder:
//
//	<settings>/screens/hub.json     the /provider screen
//	<settings>/screens/team.json    /team
//	<settings>/screens/flow.json    /flow
//	<settings>/screens/auto.json    /auto
//
// The replacement draws the same data (the binds each screen publishes) and presses the same
// verbs; only the look, the order and the places of the nodes are the profile's. What stays in
// Go is what the screen does with a press, never how it looks (docs/DESIGN-OPEN-TUI.md 5.6).
//
// A replacement that cannot be used never leaves the user without the screen: it is refused
// with the reason and the built-in one is shown. -raw and -scene boot exactly what was asked
// for, so they never read this folder (callers pass allow=false for them).

// screensDir is the folder of replacement screens, "" when there is nowhere to keep them.
func screensDir() string {
	d := configDir()
	if d == "" {
		return ""
	}
	return filepath.Join(d, "screens")
}

// loadScreen returns the document for the screen called name: the profile's replacement when
// one exists and is valid, otherwise the built-in one. notice says why a replacement was not
// used ("" when there was none or it was used); it is addressed to the user.
func loadScreen(name string, builtin func() (*scene.Document, error), allow bool) (doc *scene.Document, notice string, err error) {
	if allow {
		if dir := screensDir(); dir != "" {
			path := filepath.Join(dir, name+".json")
			if data, readErr := os.ReadFile(path); readErr == nil {
				d, parseErr := parseScreen(data)
				if parseErr == nil {
					return d, "", nil
				}
				notice = fmt.Sprintf("your %s screen (%s) could not be used: %v (delete the file to get the built-in one back)", name, path, parseErr)
			} else if !os.IsNotExist(readErr) {
				notice = fmt.Sprintf("your %s screen (%s) could not be read: %v", name, path, readErr)
			}
		}
	}
	doc, err = builtin()
	return doc, notice, err
}
