package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/michiTrader/arxi_tui/internal/scene"
)

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
				d, parseErr := scene.ParseDocument(data)
				if parseErr == nil {
					parseErr = d.Validate()
				}
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
