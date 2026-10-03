package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strconv"

	"github.com/michiTrader/arxi/internal/model"
	"github.com/michiTrader/arxi/internal/modelstore"
	"github.com/michiTrader/arxi/internal/surface"
)

// The `provider` and `model` commands: add, list, enable, disable.
//
// These are the first three commands docs/design §20.1 shows a new user typing,
// and they exist now for a concrete reason: `arxi run start` refuses a real run
// because there is no LLM-backed Executor, and building one needs an endpoint, a
// credential and a model id. This is where those come from.
//
// Flags are parsed by parseInvocation against the registry, for the reason
// trigger.go gives at length: requiredness, defaults and enums are already
// stated in surface.Registry, and a second hand-written copy is the one that
// goes stale — a flag added to the registry would be advertised by `arxi
// surface`, offered in the tool schema, and then dropped on the floor here.

// providerDir is where providers live. A variable so tests do not write into the
// developer's working directory, matching triggerDir.
var providerDir = modelstore.DefaultDir

// openProviders opens the provider store or exits.
//
// It is the only way this package reaches those files, which arch rule 18
// enforces: the store writes 0600 and atomically, refuses an api_key field on
// the way in, and refuses a record whose name disagrees with its filename. A
// command that opened the file itself would get none of that, and the result
// would be a world-readable map to a credential that looks like a working file.
func openProviders() *modelstore.Store {
	s, err := modelstore.Open(providerDir)
	if err != nil {
		fatal(err)
	}
	return s
}

func cmdProvider(args []string) {
	if len(args) == 0 {
		fmt.Fprintf(os.Stderr, "usage: arxi provider add <name> "+
			"[--base-url URL] [--api-key-env VAR] | key <name> | list\n")
		os.Exit(2)
	}
	switch args[0] {
	case "add":
		cmdProviderAdd(args[1:])
	case "key":
		cmdProviderKey(args[1:])
	case "list":
		cmdProviderList(args[1:])
	default:
		// notImplemented rather than "unknown command", because the surface may
		// declare a provider subcommand this build has not written yet, and
		// blaming the user for a typo they did not make is the worst answer.
		notImplemented(append([]string{"provider"}, args...))
	}
}

func cmdModel(args []string) {
	if len(args) == 0 {
		fmt.Fprintf(os.Stderr, "usage: arxi model list | add <provider> <model> | enable <model> | disable <model>\n")
		os.Exit(2)
	}
	switch args[0] {
	case "list":
		cmdModelList(args[1:])
	case "add":
		cmdModelAdd(args[1:])
	case "enable":
		cmdModelEnable(args[1:], true)
	case "disable":
		cmdModelEnable(args[1:], false)
	default:
		notImplemented(append([]string{"model"}, args...))
	}
}

// cmdProviderAdd implements `arxi provider add <name> [--base-url] [--api-key-env]`.
func cmdProviderAdd(args []string) {
	c := surface.Lookup("provider", "add")
	vals, err := parseInvocation(c, args)
	if err != nil {
		fmt.Fprintf(os.Stderr, "arxi provider add: %v\n", err)
		os.Exit(2)
	}

	// A key on a command line lands in shell history and in the process table.
	// The flag is declared because the protocol accepts it (the TUI sends it in
	// the request body, which is neither), but the CLI refuses it and names the
	// safe way. Exit 2: this is a bad invocation.
	if _, given := vals["api-key"]; given {
		fmt.Fprintf(os.Stderr, "arxi provider add: --api-key is refused on the command line "+
			"(it would be kept in your shell history).\n"+
			"  register the provider, then give the key on standard input:\n"+
			"    arxi provider add %s ...\n    arxi provider key %s\n", vals["name"], vals["name"])
		os.Exit(2)
	}

	// The timestamp is stamped inside registerProvider and passed to internal/model,
	// which arch rule 17 forbids from importing time.
	added, err := registerProvider(vals["name"], vals["base-url"], vals["api-key-env"], "")
	if err != nil {
		// Exit 2 for a bad invocation: a key passed where a variable name
		// belongs lands here, and it is the most important refusal in the
		// command. A store failure is operational and exits 1.
		fmt.Fprintf(os.Stderr, "arxi provider add: %v\n", err)
		var bad badInvocation
		if errors.As(err, &bad) {
			os.Exit(2)
		}
		os.Exit(1)
	}
	p := added.Provider

	// The output names the variable, not the key, and says so — because the
	// whole point of --api-key-env is that the secret is somewhere else, and a
	// user who does not see that will pass the key next time.
	if p.APIKeyEnv == "" {
		fmt.Printf("provider %s registered (%s, no credential)\n", p.Name, p.BaseURL)
	} else {
		fmt.Printf("provider %s registered (key from $%s)\n", p.Name, p.APIKeyEnv)
	}

	// A warning, not an error. An unset variable today is normal — a user
	// registers the provider and exports the key afterwards — but discovering it
	// at the first `run start`, after a blueprint and an agent have been
	// written, wastes the trip.
	if p.APIKeyEnv != "" && os.Getenv(p.APIKeyEnv) == "" {
		fmt.Printf("  note: $%s is not set in this shell, so a run would have no "+
			"credential to send.\n    export %s=<the key>\n", p.APIKeyEnv, p.APIKeyEnv)
	}

	if len(p.Models) == 0 {
		fmt.Printf("  this build does not know what %s serves, so no models were "+
			"added.\n", p.BaseURL)
	} else {
		fmt.Printf("  see what it offers: arxi model list\n")
	}
}

// cmdModelList implements `arxi model list`.
func cmdModelList(args []string) {
	c := surface.Lookup("model", "list")
	vals, err := parseInvocation(c, args)
	if err != nil {
		fmt.Fprintf(os.Stderr, "arxi model list: %v\n", err)
		os.Exit(2)
	}

	ps, err := openProviders().List()
	if err != nil {
		fatal(err)
	}
	rows := model.Rows(ps)

	if vals["json"] == "true" {
		out := make([]map[string]any, 0, len(rows))
		for _, r := range rows {
			out = append(out, map[string]any{
				"name": r.Name, "provider": r.Provider, "enabled": r.Enabled,
			})
		}
		// A named key rather than a bare array, matching trigger list: a later
		// addition does not change the type of the whole document and break
		// every parser at once.
		emitJSON(map[string]any{"models": out})
		return
	}

	if len(rows) == 0 {
		// An empty list is not an error, but it is useless without the next
		// step. `model list` on a fresh checkout is a question, and the answer
		// has to include what to do about it.
		fmt.Println("no models: no providers are registered yet.")
		fmt.Printf("  register one: arxi provider add openai --api-key-env "+
			"OPENAI_API_KEY\n  known providers: %v\n", model.KnownNames())
		return
	}

	// The header is the one docs/design §20.1 prints. It is not decoration:
	// that section is the documented output of this command.
	fmt.Printf("%-24s %-11s %s\n", "NAME", "PROVIDER", "STATUS")
	for _, r := range rows {
		fmt.Printf("%-24s %-11s %s\n", r.Name, r.Provider, r.Status())
	}
}

// cmdModelEnable implements both `model enable` and `model disable`.
//
// One function for both because they differ by a single boolean, and two copies
// of this would drift in the way that matters: the ambiguity refusal and the
// "already enabled" report are the parts worth getting right, and the second
// copy is where they would be missing.
func cmdModelEnable(args []string, on bool) {
	verb := "disable"
	if on {
		verb = "enable"
	}

	c := surface.Lookup("model", verb)
	vals, err := parseInvocation(c, args)
	if err != nil {
		fmt.Fprintf(os.Stderr, "arxi model %s: %v\n", verb, err)
		os.Exit(2)
	}
	ref := vals["model"]

	store := openProviders()

	// Owner and not Resolve: Resolve refuses a DISABLED model, which is exactly
	// what `model enable` is for. Using it here would leave the command unable
	// to enable anything — and the failure would look like the feature working.
	p, id, err := store.Owner(ref)
	if err != nil {
		fatal(err)
	}

	changed, err := p.SetEnabled(id, on)
	if err != nil {
		fatal(err)
	}
	if !changed {
		// Reported, and exit 0. Nothing is wrong: the model is already in the
		// state asked for. Printing a success that did nothing is how a user
		// concludes the setting does not work; failing would break an idempotent
		// script that enables a model at every deploy.
		fmt.Printf("model %s/%s is already %sd\n", p.Name, id, verb)
		return
	}

	if err := store.Save(p); err != nil {
		fatal(err)
	}
	fmt.Printf("model %s %sd\n", id, verb)
}

// cmdProviderKey implements `arxi provider key <name>`: the key is read from
// standard input, never from argv.
func cmdProviderKey(args []string) {
	c := surface.Lookup("provider", "key")
	vals, err := parseInvocation(c, args)
	if err != nil {
		fmt.Fprintf(os.Stderr, "arxi provider key: %v\n", err)
		os.Exit(2)
	}
	if _, given := vals["api-key"]; given {
		fmt.Fprintf(os.Stderr, "arxi provider key: --api-key is refused on the command line "+
			"(it would be kept in your shell history).\n"+
			"  give the key on standard input:  printenv MY_KEY | arxi provider key %s\n", vals["name"])
		os.Exit(2)
	}
	key, err := stdinKey(os.Stdin, os.Stderr)
	if err != nil {
		fmt.Fprintf(os.Stderr, "arxi provider key: %v\n", err)
		os.Exit(2)
	}
	if err := setProviderKey(vals["name"], key); err != nil {
		fmt.Fprintf(os.Stderr, "arxi provider key: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("key for provider %s stored privately (never shown again)\n", vals["name"])
}

// cmdProviderList implements `arxi provider list [--json]`.
func cmdProviderList(args []string) {
	c := surface.Lookup("provider", "list")
	vals, err := parseInvocation(c, args)
	if err != nil {
		fmt.Fprintf(os.Stderr, "arxi provider list: %v\n", err)
		os.Exit(2)
	}
	rows, err := listProviders()
	if err != nil {
		fatal(err)
	}
	if vals["json"] == "true" {
		out, _ := json.Marshal(map[string]any{"providers": rows})
		fmt.Println(string(out))
		return
	}
	if len(rows) == 0 {
		fmt.Println("no providers registered; add one with: arxi provider add <name> --base-url URL")
		return
	}
	for _, r := range rows {
		fmt.Printf("%-20s %-10s %d models  %s\n", r.Name, r.Key, r.Models, r.BaseURL)
	}
}

// cmdModelAdd implements `arxi model add <provider> <model> [--in N --out N]`.
func cmdModelAdd(args []string) {
	c := surface.Lookup("model", "add")
	vals, err := parseInvocation(c, args)
	if err != nil {
		fmt.Fprintf(os.Stderr, "arxi model add: %v\n", err)
		os.Exit(2)
	}
	num := func(k string) (*float64, error) {
		v, ok := vals[k]
		if !ok {
			return nil, nil
		}
		f, err := strconv.ParseFloat(v, 64)
		if err != nil {
			return nil, fmt.Errorf("--%s %q is not a number", k, v)
		}
		return &f, nil
	}
	in, err := num("in")
	if err == nil {
		var out *float64
		out, err = num("out")
		if err == nil {
			_, err = addModel(vals["provider"], vals["model"], in, out)
		}
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "arxi model add: %v\n", err)
		os.Exit(2)
	}
	fmt.Printf("model %s added to %s\n", vals["model"], vals["provider"])
}
