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
// handshake has not run); an undeclared verb means a core built before the provider
// verbs existed; a declared-but-unimplemented verb means a build that declares them
// but has not wired the executor. Both of the last two are cured the same way -- rebuild
// the core from its current source -- and the message says so in the command to run.
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

	// An undeclared verb means a core built BEFORE the provider verbs existed -- the
	// same program, an older build -- so the fix is to rebuild it, and the message says
	// so in the words a person acts on. (It used to say "wrong kernel" and dump the
	// whole declared list, which sent a user hunting for a different program.) Every
	// missing verb is named, not just the first, so one rebuild is enough.
	var missing []string
	for _, v := range providerVerbs {
		if !declared[v] {
			missing = append(missing, v)
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("this arxi core is too old to manage providers (it lacks %s); "+
			"rebuild it from this repository with: git pull, then cd core and go build -o ../arxi ./cmd/arxi (on Windows: -o ..\\arxi.exe)",
			strings.Join(missing, ", "))
	}

	for _, v := range providerVerbs {
		if !implemented[v] {
			return fmt.Errorf("this arxi core declares %q but does not implement it "+
				"(it answers not_implemented, and that never changes for a given build); "+
				"rebuild it from this repository with: git pull, then cd core and go build -o ../arxi ./cmd/arxi (on Windows: -o ..\\arxi.exe)", v)
		}
	}

	return nil
}

// loginVerbs are the verbs the /login wizard needs beyond the provider set. They are
// gated together for the same all-or-nothing reason providerVerbs are: a core that
// stores keys but cannot list their state would show every provider as unconfigured.
var loginVerbs = []string{"provider.add", "provider.key", "provider.list", "model.add"}

// requireLoginVerbs decides from the hello whether this core can run the /login
// wizard. The remedy names the exact rebuild command, because the usual cause is a
// core binary built before the key verbs existed.
func requireLoginVerbs(hello *driver.Hello) error {
	if hello == nil {
		return fmt.Errorf(
			"cmd/arxi-tui/provider_cmd_gate.go: no hello to gate on; the handshake " +
				"must complete before requireLoginVerbs")
	}
	implemented := map[string]bool{}
	for _, t := range hello.Implemented {
		implemented[t] = true
	}
	var missing []string
	for _, v := range loginVerbs {
		if !implemented[v] {
			missing = append(missing, v)
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf(
			"the connected core does not implement %s, so /login cannot store keys. "+
				"remedy: rebuild it with `cd core && go build -o arxi ./cmd/arxi` "+
				"and point ARXI_BIN at it",
			strings.Join(missing, ", "))
	}
	return nil
}
