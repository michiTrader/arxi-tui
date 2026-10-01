package main

import (
	"fmt"
	"strings"

	"github.com/michiTrader/arxi_tui/internal/driver"
)

// providerVerbs is the set a provider/model slash command needs the connected
// core to implement, named once here so the gate below and any future dispatch
// site cannot drift on the spelling of the four verbs the whole K2 integration
// turns on. It mirrors runStartType, which names run.start once for the same
// reason -- one source for the string the request builder and the gate share.
//
// All four are required together rather than per-command: provider.add,
// model.list, model.enable and model.disable are one capability from the user's
// side (manage providers and the models under them), and a core that implements
// three of them is a half-wired build the TUI cannot honestly offer the feature
// against. Gating on the whole set means a /model disable never discovers a
// missing executor only after the user typed it -- the capability is present or
// the feature is refused up front, the same all-or-nothing requireRunStart makes
// for the run surface.
var providerVerbs = []string{"provider.add", "model.list", "model.enable", "model.disable"}

// requireProviderVerbs is the K2 analogue of requireRunStart: it reads the hello
// the core sent at connect and decides whether this connection can manage
// providers at all, before a worker goroutine commits to a round-trip the core
// will only answer with not_implemented.
//
// The check is on the hello's `implemented` list, not its `types` list, and the
// distinction is the one M1b paid for: surface v1 DECLARES these verbs on every
// build (they are in `types` because the surface defines them), but a build that
// has not wired their executors answers not_implemented -- which is permanent for
// that binary, so sending anyway and waiting on the response is a hang the user
// cannot retry out of. Gating on `implemented` turns that into a named refusal at
// the moment the command is typed.
//
// The three refused states are kept distinct because their remedies differ, the
// same split requireRunStart draws: a nil hello is a caller ordering bug (the
// handshake has not run); an undeclared verb means the connected surface is not
// the v1 vocabulary this host speaks (the wrong kernel); a declared-but-
// unimplemented verb means the right surface on a build that has not wired the
// executor -- the one PR #117 wires, so an older core is exactly the build that
// lands here.
func requireProviderVerbs(hello *driver.Hello) error {
	if hello == nil {
		return fmt.Errorf(
			"cmd/arxi-tui/provider_cmd_gate.go: no hello to gate on; the handshake " +
				"must complete before requireProviderVerbs, since the implemented list " +
				"is what tells a wired provider verb apart from a declared-but-" +
				"unimplemented one")
	}

	declared := map[string]bool{}
	for _, t := range hello.Types {
		declared[t] = true
	}
	implemented := map[string]bool{}
	for _, t := range hello.Implemented {
		implemented[t] = true
	}

	for _, v := range providerVerbs {
		if !declared[v] {
			return fmt.Errorf(
				"cmd/arxi-tui/provider_cmd_gate.go: the connected core does not declare "+
					"%q; its surface is not the vocabulary this host manages providers "+
					"over, so it is the wrong kernel. remedy: connect to an arxi build "+
					"whose surface declares the provider verbs (declared types: %s)",
				v, strings.Join(hello.Types, ", "))
		}
	}

	for _, v := range providerVerbs {
		if !implemented[v] {
			return fmt.Errorf(
				"cmd/arxi-tui/provider_cmd_gate.go: the connected core declares %q but "+
					"does not implement it (it answers not_implemented), so this build "+
					"cannot manage providers from the TUI. not_implemented is permanent "+
					"for a binary, so retrying will not help. remedy: connect to an arxi "+
					"build that implements the provider verbs -- the ones PR #117 wires "+
					"(implemented verbs on this build: %s)",
				v, strings.Join(hello.Implemented, ", "))
		}
	}

	return nil
}
