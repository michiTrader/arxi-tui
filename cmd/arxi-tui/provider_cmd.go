package main

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/michiTrader/arxi_tui/internal/driver"
	"github.com/michiTrader/arxi_tui/internal/fold"
)

// This file is the host-level grammar for `/provider …` and `/model …`, the K2
// slash commands that let the user manage model providers from the TUI without
// dropping to the `arxi provider add` CLI. They live in the host, not the patch
// surface, for the same reason install does (plugin_install_cmd.go argues it):
// they are not source-to-source document transforms. Each one is a network
// round-trip over the serve protocol -- the driver's SubmitProviderAdd /
// SubmitModelList / SubmitModelEnable -- so the pure patch package, which owns
// only offline scene mutation, is the wrong home. They are intercepted in the
// loop before the slash menu, exactly as the plugin verbs are.
//
// The parsing stays a pure function of the line, no loop state and no network,
// the same discipline parsePluginInstall follows: recognising the command and
// extracting its arguments is the decidable part a counterfactual pins exactly,
// and the async round-trip reads a resolved providerAction from here and carries
// no grammar of its own.

// providerAction is one parsed provider/model command, ready for the worker to
// dispatch over the driver. Exactly one shape is populated, chosen by Verb; the
// zero value of the unused fields is never read, because the worker switches on
// Verb before touching Add or Ref.
//
// Verb is the wire verb a command maps to, NOT the user's word: `/provider list`
// and `/model list` both carry "model.list" because model.list is the single
// read for both questions (a provider with its models answers "what providers"
// and "what models" at once, the K2 choice over a separate provider.list). The
// user-facing distinction survives in ProviderView, which the formatter reads to
// collapse the rows to provider names for `/provider list`.
type providerAction struct {
	Verb         string                   // "provider.add" | "model.list" | "model.enable" | "model.disable"
	ProviderView bool                     // true for `/provider list`: format rows as providers, not models
	Add          driver.ProviderAddParams // populated when Verb == "provider.add"
	Ref          string                   // populated when Verb == "model.enable" | "model.disable"

	// OpenScreen marks the bare `/provider` and `/model`: the user asked to SEE the
	// providers, not to read them as a sentence, so the host opens the providers
	// screen and fills it from one model.list round-trip. It rides on Verb
	// "model.list" because that is the single read behind the screen.
	OpenScreen bool
	// Refresh asks the worker to re-read model.list after the command and hand the
	// rows back, so an open providers screen stays in step with what a toggle or an
	// add just changed. It is set by the loop, never by the grammar: the same typed
	// `/model enable x` needs no second round-trip when no screen is showing.
	Refresh bool
}

// stripLeadingVerb removes a leading "/word" or "word" prefix from the line, the
// same normalisation the plugin parsers do for "/ui", so a parser can match on
// the subcommand without re-deciding whether the slash is present. It returns the
// remainder with surrounding space trimmed, and whether the prefix was the one
// asked for -- a line that is not this command at all returns matched=false and
// is left for the next parser in the dispatch chain.
func stripLeadingVerb(line, verb string) (rest string, matched bool) {
	body := strings.TrimSpace(line)
	for _, p := range []string{"/" + verb, verb} {
		if body == p {
			return "", true
		}
		if strings.HasPrefix(body, p+" ") {
			return strings.TrimSpace(body[len(p):]), true
		}
	}
	return "", false
}

// parseProviderCommand recognizes `/provider add <name> [flags]` and
// `/provider list`.
//
// matched reports whether the line IS a `/provider` invocation; a malformed one
// (unknown subcommand, missing name, bad flag) returns matched=true with a
// located refusal in err, so the host names the specific mistake rather than
// letting the line fall through to the slash menu, whose Enter branch would
// silently do nothing on a line its filter does not match (the dead end the
// dispatch-order comment in the loop documents).
func parseProviderCommand(line string) (action providerAction, matched bool, err error) {
	rest, ok := stripLeadingVerb(line, "provider")
	if !ok {
		return providerAction{}, false, nil
	}
	fields := strings.Fields(rest)
	if len(fields) == 0 {
		// Bare `/provider` is the doorway to the providers screen. It used to be a
		// refusal ("needs a subcommand"), which was the right answer for a grammar
		// and the wrong one for a person: the slash menu advertises `provider`, a
		// user who picks it expects a menu, and a one-line notice at the top of the
		// chat is easy to miss -- so the command read as "it sent the word to the
		// chat". The subcommands stay for the typed, scripted use.
		return providerAction{Verb: "model.list", ProviderView: true, OpenScreen: true}, true, nil
	}

	switch fields[0] {
	case "list":
		if len(fields) != 1 {
			return providerAction{}, true, fmt.Errorf(
				"/provider list takes no arguments; did you mean /provider add %s?",
				fields[1])
		}
		return providerAction{Verb: "model.list", ProviderView: true}, true, nil
	case "add":
		add, perr := parseProviderAddArgs(fields[1:])
		if perr != nil {
			return providerAction{}, true, perr
		}
		return providerAction{Verb: "provider.add", Add: add}, true, nil
	default:
		return providerAction{}, true, fmt.Errorf(
			"/provider has no subcommand %q; it takes `add` or `list`", fields[0])
	}
}

// parseProviderAddArgs reads `<name> [--base-url <url>] [--api-key-env <env>]`.
//
// The name is the first token and is required: model.New addresses the record by
// name, so an add with no name has nowhere to write. base-url and api-key-env are
// optional and only sent when given -- the core fills base-url from its known-
// provider table and treats an empty api-key-env as "no credential needed" (a
// local server), so sending "" for an omitted flag would overwrite those
// defaults with a blank the user never typed.
//
// api-key-env carries the NAME of an environment variable, never a key. This
// parser does NOT validate that -- the single enforcement site is arxi's
// validateKeyEnv, which refuses a secret-shaped value, and duplicating it here
// would be a second validator to drift. What this parser does guarantee is that
// a flag value is never echoed back into a notice: a refusal names the flag, not
// the value, so a mistakenly-pasted key never reaches the banner, the log or the
// scrollback.
func parseProviderAddArgs(args []string) (driver.ProviderAddParams, error) {
	if len(args) == 0 {
		return driver.ProviderAddParams{}, fmt.Errorf(
			"/provider add needs a provider name: /provider add <name> " +
				"[--base-url <url>] [--api-key-env <env>]")
	}
	if strings.HasPrefix(args[0], "--") {
		return driver.ProviderAddParams{}, fmt.Errorf(
			"/provider add needs the provider name first, before any flag: "+
				"/provider add <name> %s …", args[0])
	}
	p := driver.ProviderAddParams{Name: args[0]}

	rest := args[1:]
	for i := 0; i < len(rest); i++ {
		flag := rest[i]
		var dst *string
		switch flag {
		case "--base-url":
			dst = &p.BaseURL
		case "--api-key-env":
			dst = &p.APIKeyEnv
		default:
			if strings.HasPrefix(flag, "--") {
				return driver.ProviderAddParams{}, fmt.Errorf(
					"/provider add has no flag %q; it takes --base-url and "+
						"--api-key-env", flag)
			}
			return driver.ProviderAddParams{}, fmt.Errorf(
				"/provider add got an unexpected argument %q; the name comes first, "+
					"then --base-url <url> and --api-key-env <env>", flag)
		}
		if i+1 >= len(rest) {
			// A flag with no value is refused rather than left empty: an empty
			// --api-key-env is a legal "no credential" to the core, so accepting a
			// value-less flag as empty would silently register a credential-free
			// provider the user meant to give a key env, not refuse the typo.
			return driver.ProviderAddParams{}, fmt.Errorf(
				"%s needs a value: %s <value>", flag, flag)
		}
		*dst = rest[i+1]
		i++
	}
	return p, nil
}

// parseModelCommand recognizes `/model list`, `/model enable <ref>` and
// `/model disable <ref>`. matched and the located-refusal contract are the same
// as parseProviderCommand's.
func parseModelCommand(line string) (action providerAction, matched bool, err error) {
	rest, ok := stripLeadingVerb(line, "model")
	if !ok {
		return providerAction{}, false, nil
	}
	fields := strings.Fields(rest)
	if len(fields) == 0 {
		// Bare `/model` opens the same screen as bare `/provider`: the screen lists
		// every model with its enable/disable action, which is what a user reaching
		// for `/model` wants. One screen, two doors, so the two cannot disagree on
		// what the models are.
		return providerAction{Verb: "model.list", OpenScreen: true}, true, nil
	}

	switch fields[0] {
	case "list":
		if len(fields) != 1 {
			return providerAction{}, true, fmt.Errorf(
				"/model list takes no arguments")
		}
		return providerAction{Verb: "model.list"}, true, nil
	case "enable", "disable":
		verb := "model.enable"
		if fields[0] == "disable" {
			verb = "model.disable"
		}
		if len(fields) != 2 {
			return providerAction{}, true, fmt.Errorf(
				"/model %s needs exactly one model ref: /model %s "+
					"<provider/id or id>", fields[0], fields[0])
		}
		return providerAction{Verb: verb, Ref: fields[1]}, true, nil
	default:
		return providerAction{}, true, fmt.Errorf(
			"/model has no subcommand %q; it takes `list`, `enable` or `disable`",
			fields[0])
	}
}

// providerManager is the optional capability a Driver has when it speaks the
// serve protocol: it can round-trip the K2 provider verbs and report the hello
// the gate reads. serveDriver (*driver.NDJSONDriver) implements it; the mock
// driver does not, because the mock replays a fixed log and has no core to
// register a provider with. The loop asserts for it rather than widening Driver,
// the same shape actorLabeler uses, so a fake with no core is not forced to
// stub four methods it can only answer with a lie.
type providerManager interface {
	Hello() *driver.Hello
	SubmitProviderAdd(ctx context.Context, p driver.ProviderAddParams) (*driver.ProviderAddResult, error)
	SubmitModelList(ctx context.Context) (*driver.ModelListResult, error)
	SubmitModelEnable(ctx context.Context, ref string, on bool) (*driver.ModelEnableResult, error)
}

// loginManager is what the /login wizard needs on top of providerManager: store a key
// for a registered provider, read the credential state, and add a model by hand. It
// is a separate interface so a fake that only manages providers is not forced to stub
// verbs it cannot answer, and so the loop can refuse /login on an older core.
type loginManager interface {
	providerManager
	SubmitProviderKey(ctx context.Context, name, apiKey string) (*driver.ProviderKeyResult, error)
	SubmitProviderList(ctx context.Context) (*driver.ProviderListResult, error)
	SubmitModelAdd(ctx context.Context, p driver.ModelAddParams) (*driver.ProviderAddResult, error)
}

// providerCmdOutcome is the worker's one message back to the loop: the notice to
// show when the round-trip finished. It carries a plain string rather than the
// typed result because the loop's only job is to display it and clear busy --
// the formatting (success or refusal) happens on the worker, off the loop, so a
// slow format never competes with a repaint.
type providerCmdOutcome struct {
	notice string
	// models and hasModels carry the fresh model.list rows when the action asked for
	// them (OpenScreen or Refresh). hasModels is separate from len(models) because
	// an empty list is a real answer -- "no provider yet" -- that must clear the
	// screen, whereas a failed read carries no rows at all and must leave the last
	// good list standing.
	models    []fold.ProviderModel
	hasModels bool
	// open says the rows should OPEN the providers screen, not merely refresh one
	// that is showing. Without it a user who pressed Esc while a refresh was in
	// flight would have the screen reopen under them when the answer landed.
	open bool
}

// noLiveCoreNotice is the one sentence for "this process has no arxi core". It is
// a constant because two places must say exactly the same thing -- the refusal of
// a typed command and the banner on the providers screen that opens anyway -- and
// a user who reads one and then the other should not wonder whether two different
// things are wrong.
const noLiveCoreNotice = "provider management needs a live arxi core; set ARXI_BIN to an " +
	"arxi binary and restart so the TUI connects to a core that can " +
	"register providers"

// pendingNotice is the line shown the instant a command is accepted, before its
// round-trip returns. It names the act in flight so the user sees the command
// was taken rather than a frozen banner, the same role "fetching <url> …" plays
// for install.
func (a providerAction) pendingNotice() string {
	switch a.Verb {
	case "provider.add":
		return "/provider add: registering " + a.Add.Name + " …"
	case "model.list":
		if a.ProviderView {
			return "/provider list: reading providers …"
		}
		return "/model list: reading models …"
	case "model.enable":
		return "/model enable: enabling " + a.Ref + " …"
	case "model.disable":
		return "/model disable: disabling " + a.Ref + " …"
	default:
		return "provider command: working …"
	}
}

// dispatchProviderCmd is the loop's single entry point for a parsed
// provider/model command. It handles the parse error, the busy guard, the
// capability assertion and the hello gate in that order, and on success launches
// the worker and returns the pending notice. It returns the notice to show and
// never blocks: the round-trip runs on the worker startProviderCmd spawns.
//
// The order is deliberate and mirrors install's: report a parse error first (it
// is the user's typo, cheapest to fix), then refuse a second command while one
// is in flight (two round-trips would race the one response reader and interleave
// their notices), then the capability and gate refusals (which say the feature is
// unavailable on this connection, not that the command was wrong).
func dispatchProviderCmd(ctx context.Context, drv Driver, act providerAction, perr error, busy *bool, done chan<- providerCmdOutcome) string {
	if perr != nil {
		return perr.Error()
	}
	if *busy {
		return "a provider command is already running; wait for it to finish before starting another"
	}
	if refusal := providerCoreRefusal(drv); refusal != "" {
		return refusal
	}
	pm := drv.(providerManager)
	*busy = true
	startProviderCmd(ctx, pm, act, done)
	return act.pendingNotice()
}

// providerCoreRefusal says why this connection cannot manage providers, or returns
// "" when it can. It is the capability assertion and the hello gate that
// dispatchProviderCmd runs, lifted out so the loop can ask the same question before
// opening the providers screen: a screen that opens is still useful without a core
// (it tells the user what is missing, in place), but only if the sentence it shows
// is the one the typed command would have refused with.
//
// The mock driver (ARXI_BIN unset) has no core to manage providers with. Naming that
// is the honest refusal: the feature is real but this process is not connected to a
// kernel that can serve it, so point at the fix rather than let a round-trip hang
// against a driver that cannot answer.
func providerCoreRefusal(drv Driver) string {
	pm, ok := drv.(providerManager)
	if !ok {
		return noLiveCoreNotice
	}
	if err := requireProviderVerbs(pm.Hello()); err != nil {
		return err.Error()
	}
	return ""
}

// parseProviderOrModel runs the two grammars in the order the loop does, so the
// slash menu's pick and a typed line resolve through one function and cannot
// diverge on what `/provider` or `/model` means.
func parseProviderOrModel(line string) (act providerAction, matched bool, err error) {
	if act, matched, err = parseProviderCommand(line); matched {
		return act, true, err
	}
	return parseModelCommand(line)
}

// startProviderCmd runs the command's round-trip on a worker goroutine and sends
// the formatted notice back on done, exactly once. It is the provider analogue of
// startInstall: the network work is kept off the loop so a slow or hung core
// never freezes the frame or the panic gesture (invariant 6). done is buffered by
// its caller so the worker never blocks on the send even if the loop is mid-
// repaint when the response arrives.
func startProviderCmd(ctx context.Context, pm providerManager, act providerAction, done chan<- providerCmdOutcome) {
	go func() {
		done <- runProviderWork(ctx, pm, act)
	}()
}

// runProviderWork is the worker's body, split from the goroutine so it is testable
// without a channel. It returns the notice and, when the action asked for them, the
// fresh model rows.
//
// OpenScreen is ONE model.list whose rows feed the screen, and its notice stays
// empty on success because the screen is the answer; formatting the list into a
// sentence as well would put the same information on screen twice. Every other
// action keeps its own notice, and Refresh adds a second read afterwards so an open
// screen reflects the change the command just made. A failed refresh keeps the
// command's own notice and leaves hasModels false: the command succeeded, and
// reporting a list error as if the toggle had failed would send the user to retry
// something that worked.
func runProviderWork(ctx context.Context, pm providerManager, act providerAction) providerCmdOutcome {
	if act.OpenScreen {
		res, err := pm.SubmitModelList(ctx)
		if err != nil {
			return providerCmdOutcome{notice: "/provider: " + err.Error()}
		}
		out := providerCmdOutcome{models: modelsFromRows(res.Models), hasModels: true, open: true}
		if len(out.models) == 0 {
			out.notice = providersEmptyNotice
		}
		return out
	}
	out := providerCmdOutcome{notice: runProviderCmd(ctx, pm, act)}
	if act.Refresh {
		if res, err := pm.SubmitModelList(ctx); err == nil {
			out.models = modelsFromRows(res.Models)
			out.hasModels = true
		}
	}
	return out
}

// runProviderCmd performs the one round-trip the action names and formats the
// result or refusal into a single notice line. It is split from startProviderCmd
// so the dispatch-and-format logic is testable against a fake providerManager
// without a goroutine or a channel -- the pure part, the same seam startRun keeps
// from its worker.
func runProviderCmd(ctx context.Context, pm providerManager, act providerAction) string {
	switch act.Verb {
	case "provider.add":
		res, err := pm.SubmitProviderAdd(ctx, act.Add)
		if err != nil {
			return "/provider add: " + err.Error()
		}
		return formatProviderAdd(res)
	case "model.list":
		res, err := pm.SubmitModelList(ctx)
		if err != nil {
			if act.ProviderView {
				return "/provider list: " + err.Error()
			}
			return "/model list: " + err.Error()
		}
		return formatModelList(res, act.ProviderView)
	case "model.enable", "model.disable":
		on := act.Verb == "model.enable"
		res, err := pm.SubmitModelEnable(ctx, act.Ref, on)
		if err != nil {
			return "/" + strings.TrimPrefix(act.Verb, "model.") + " model: " + err.Error()
		}
		return formatModelEnable(res, on)
	default:
		// Unreachable: the parsers set Verb from a closed switch. Named rather than
		// panicking because a future parser that adds a verb and forgets the worker
		// should get a visible notice, not a crash that takes the TUI down.
		return "provider command: unknown verb " + act.Verb
	}
}

// formatProviderAdd renders the registered provider snapshot as one line. It
// names the provider, its protocol and endpoint, and how many models it brought
// enabled, so the user sees what model.New stamped (the first model enabled, the
// rest disabled) without a second /model list. api_key_env is shown because it is
// a variable NAME, not a secret -- the §20.1 invariant is exactly that only the
// name ever leaves the user's machine.
func formatProviderAdd(res *driver.ProviderAddResult) string {
	enabled := 0
	for _, m := range res.Models {
		if m.Enabled {
			enabled++
		}
	}
	key := res.APIKeyEnv
	if key == "" {
		key = "(none)"
	}
	return fmt.Sprintf(
		"/provider add: registered %s (%s, %s) key-env %s with %d model(s), %d enabled",
		res.Name, res.Protocol, res.BaseURL, key, len(res.Models), enabled)
}

// formatModelList renders model.list's rows as one line. For /model list it lists
// each model with an on/off mark; for /provider list it collapses to the distinct
// provider names, because the user asked which providers exist, not which models.
// An empty result is named rather than shown as a blank, so "no providers yet"
// reads as the honest answer it is and not as a command that did nothing.
func formatModelList(res *driver.ModelListResult, providerView bool) string {
	if len(res.Models) == 0 {
		if providerView {
			return "/provider list: no providers registered yet; add one with " +
				"/provider add <name>"
		}
		return "/model list: no models yet; register a provider with " +
			"/provider add <name>"
	}

	if providerView {
		seen := map[string]bool{}
		var names []string
		for _, r := range res.Models {
			if !seen[r.Provider] {
				seen[r.Provider] = true
				names = append(names, r.Provider)
			}
		}
		sort.Strings(names)
		return fmt.Sprintf("/provider list: %d provider(s): %s",
			len(names), strings.Join(names, ", "))
	}

	rows := make([]string, 0, len(res.Models))
	for _, r := range res.Models {
		mark := "off"
		if r.Enabled {
			mark = "on"
		}
		rows = append(rows, fmt.Sprintf("%s/%s [%s]", r.Provider, r.ID, mark))
	}
	sort.Strings(rows)
	return fmt.Sprintf("/model list: %d model(s): %s",
		len(res.Models), strings.Join(rows, ", "))
}

// formatModelEnable renders a toggle's result. It reads res.Changed so an
// already-in-the-target-state model says so rather than claiming a change it did
// not make -- a command that reports a change it did not perform is how a user
// concludes the setting is broken, the reason arxi's SetEnabled returns changed
// at all.
func formatModelEnable(res *driver.ModelEnableResult, on bool) string {
	verb := "enabled"
	if !on {
		verb = "disabled"
	}
	ref := res.Model
	if res.Provider != "" {
		ref = res.Provider + "/" + res.Model
	}
	if !res.Changed {
		return fmt.Sprintf("/model %s: %s was already %s", verb, ref, verb)
	}
	return fmt.Sprintf("/model %s: %s is now %s", verb, ref, verb)
}
