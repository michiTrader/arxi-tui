package main

import (
	"fmt"
	"os/exec"
	"runtime"
	"strings"

	"github.com/michiTrader/arxi_tui/internal/scene"
)

// This file is the host's half of the open: action (BINDS.md §4.8). The scene layer has
// already refused an address that is not plain https to a host name; what is left is
// the part that needs the host's state: may this line open that host at all, and how.

// rawActionMark prefixes a press action that is not a command line when it is handed to
// the loop's queue, so a say:/open:/set: action is never mistaken for typed text (and a
// typed "open:..." line is never mistaken for an action).
const rawActionMark = "\x00action:"

// openHosts is every host a package the user approved may open, with the package that
// named it, so the answer to "why not" can say who would have to ask.
func (s packageSet) openHosts() map[string]string {
	out := map[string]string{}
	for _, p := range s.pkgs {
		if !p.runnable() {
			continue
		}
		for _, h := range p.Permissions.Open {
			if _, dup := out[h]; !dup {
				out[h] = p.ID
			}
		}
	}
	return out
}

// openRefusal returns why an open: action may not run now, or "" when it may. Two rules
// belong to the host and not to the address: a hook never opens anything (it runs with
// no key pressed, and nothing may take the user's browser anywhere unasked), and the
// host must be one a package the user approved has listed in permissions.open.
func openRefusal(act scene.Action, fromHook bool, s packageSet) string {
	if fromHook {
		return "open: is refused when a hook runs it; a hook fires with no key pressed, and only a key or a command you type may take your browser somewhere"
	}
	if _, ok := s.openHosts()[act.Host]; !ok {
		return fmt.Sprintf("open: %s is not a host any approved package may open; add it to permissions.open of a package and approve that package with /package approve", act.Host)
	}
	return ""
}

// openCommand is the program that opens an address on this system. It is argv, never a
// shell line, so the address cannot become a command.
func openCommand(goos, url string) []string {
	switch goos {
	case "darwin":
		return []string{"open", url}
	case "windows":
		return []string{"rundll32", "url.dll,FileProtocolHandler", url}
	default:
		return []string{"xdg-open", url}
	}
}

// openBrowser starts the browser; a test replaces it so nothing really opens.
var openBrowser = func(url string) error {
	argv := openCommand(runtime.GOOS, url)
	c := exec.Command(argv[0], argv[1:]...)
	if err := c.Start(); err != nil {
		return err
	}
	go func() { _ = c.Wait() }()
	return nil
}

// runOpen carries out an open: action that passed openRefusal and returns what to tell
// the user: always the full address, so a click is never a surprise about where it went.
func runOpen(act scene.Action) (note, warn string) {
	if err := openBrowser(act.Arg); err != nil {
		return "", "open: could not start a browser for " + act.Arg + ": " + strings.TrimSpace(err.Error())
	}
	return "Opening " + act.Arg + " in your browser.", ""
}
