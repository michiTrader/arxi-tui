package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/michiTrader/arxi/internal/surface"
)

// The CLI face of provider_manage.go. Each command parses against the registry
// (parseInvocation), calls the shared function and prints one plain line.

// cliFail reports a failure the way the other provider commands do: exit 2 for
// a bad invocation, 1 for an operational failure.
func cliFail(verb string, err error) {
	fmt.Fprintf(os.Stderr, "arxi %s: %v\n", verb, err)
	var bad badInvocation
	if errors.As(err, &bad) {
		os.Exit(2)
	}
	os.Exit(1)
}

// refuseKeyFlag rejects --api-key on the command line, for the reason
// cmdProviderAdd gives: shell history and the process table.
func refuseKeyFlag(verb string, vals map[string]string) {
	if _, given := vals["api-key"]; given {
		fmt.Fprintf(os.Stderr, "arxi %s: --api-key is refused on the command line "+
			"(it would be kept in your shell history).\n"+
			"  give the key on standard input:  printenv MY_KEY | arxi provider key %s\n", verb, vals["name"])
		os.Exit(2)
	}
}

func cmdProviderUpdate(args []string) {
	vals, err := parseInvocation(surface.Lookup("provider", "update"), args)
	if err != nil {
		fmt.Fprintf(os.Stderr, "arxi provider update: %v\n", err)
		os.Exit(2)
	}
	refuseKeyFlag("provider update", vals)
	p, _, err := updateProvider(vals["name"], vals["base-url"], vals["api-key-env"], "")
	if err != nil {
		cliFail("provider update", err)
	}
	fmt.Printf("provider %s updated (%s)\n", p.Name, p.BaseURL)
}

func cmdProviderRemove(args []string) {
	vals, err := parseInvocation(surface.Lookup("provider", "remove"), args)
	if err != nil {
		fmt.Fprintf(os.Stderr, "arxi provider remove: %v\n", err)
		os.Exit(2)
	}
	if err := removeProvider(vals["name"]); err != nil {
		cliFail("provider remove", err)
	}
	fmt.Printf("provider %s removed, with its models and its stored key\n", cleanName(vals["name"]))
}

func cmdModelDiscover(args []string) {
	vals, err := parseInvocation(surface.Lookup("model", "discover"), args)
	if err != nil {
		fmt.Fprintf(os.Stderr, "arxi model discover: %v\n", err)
		os.Exit(2)
	}
	res, err := discoverModels(context.Background(), vals["provider"])
	if err != nil {
		cliFail("model discover", err)
	}
	fmt.Printf("%s serves %d models; %d new added\n", res.Provider, res.Found, res.Added)
}

func cmdModelRemove(args []string) {
	vals, err := parseInvocation(surface.Lookup("model", "remove"), args)
	if err != nil {
		fmt.Fprintf(os.Stderr, "arxi model remove: %v\n", err)
		os.Exit(2)
	}
	p, id, err := removeModel(vals["model"])
	if err != nil {
		cliFail("model remove", err)
	}
	fmt.Printf("model %s removed from %s\n", id, p)
}

func cmdModelDefault(args []string) {
	vals, err := parseInvocation(surface.Lookup("model", "default"), args)
	if err != nil {
		fmt.Fprintf(os.Stderr, "arxi model default: %v\n", err)
		os.Exit(2)
	}
	ref, err := defaultModel(vals["model"])
	if err != nil {
		cliFail("model default", err)
	}
	switch {
	case vals["model"] != "":
		fmt.Printf("default model: %s\n", ref)
	case ref == "":
		fmt.Println("no default model chosen; pick one with: arxi model default <model>")
	default:
		fmt.Printf("default model: %s\n", ref)
	}
}

// cmdChat implements `arxi chat send <prompt> [--model M] [--system S] [--effort E]`.
func cmdChat(args []string) {
	if len(args) == 0 || args[0] != "send" {
		if len(args) > 0 {
			notImplemented(append([]string{"chat"}, args...))
		}
		fmt.Fprintf(os.Stderr, "usage: arxi chat send <prompt> [--model M] [--system S]\n")
		os.Exit(2)
	}
	vals, err := parseInvocation(surface.Lookup("chat", "send"), args[1:])
	if err != nil {
		fmt.Fprintf(os.Stderr, "arxi chat send: %v\n", err)
		os.Exit(2)
	}
	res, err := chatSendEffort(context.Background(), vals["prompt"], vals["history"], vals["system"], vals["model"], vals["effort"])
	if err != nil {
		cliFail("chat send", err)
	}
	fmt.Println(strings.TrimRight(res.Text, "\n"))
}
