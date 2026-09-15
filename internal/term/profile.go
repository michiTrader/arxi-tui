package term

import (
	"os"
	"strings"

	"github.com/michiTrader/arxi_tui/internal/ui"
)

// DetectProfile asks the environment how much colour the terminal can take.
//
// It lives here and not in internal/ui because ui must not import os. That is not
// tidiness: it is what makes the renderer and the byte generator testable without a
// machine, and it means a test that wants a 16-colour terminal passes a Profile
// instead of setting an environment variable and hoping nothing else reads it.
func DetectProfile() ui.Profile {
	// NO_COLOR is a promise with no threshold: any value at all means no colour.
	// https://no-color.org
	if os.Getenv("NO_COLOR") != "" {
		return ui.ProfileMono
	}
	name := strings.ToLower(os.Getenv("TERM"))
	if name == "" || name == "dumb" {
		return ui.ProfileMono
	}
	switch strings.ToLower(os.Getenv("COLORTERM")) {
	case "truecolor", "24bit":
		return ui.ProfileTrueColor
	}
	switch {
	case strings.Contains(name, "truecolor"), strings.Contains(name, "direct"):
		return ui.ProfileTrueColor
	case strings.Contains(name, "256color"):
		return ui.Profile256
	}
	// Everything else gets the sixteen. Termux reports xterm-256color and so lands
	// one line above; a bare "xterm" or a serial console lands here, which is right.
	return ui.ProfileANSI
}
