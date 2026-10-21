package main

import (
	"context"
	"fmt"
	"strings"
	"sync"

	extv2 "github.com/michiTrader/arxi_tui/internal/ext/v2"
	"github.com/michiTrader/arxi_tui/internal/fold"
)

// This file joins the package store (packages.go) to the loop: what a package adds to the
// / menu, how a typed line finds its command, and how an operation runs without holding
// the interface up. The contract itself, and every limit on what an operation may do, is
// in internal/ext/v2; nothing here widens it.

// hostBuiltins are the host functions a package may name. The set is closed and written
// in Go: a package chooses among them and cannot add one. It is small on purpose, and
// grows only with functions whose power the user already holds by typing a command.
var hostBuiltins = map[string]extv2.BuiltinFunc{
	// echo shows what was typed after the command, which is what a package needs to
	// say a fixed answer without a program of its own.
	"echo": func(_ context.Context, in extv2.Input) (extv2.Outcome, error) {
		return extv2.Outcome{Say: in.Args}, nil
	},
}

// packageNames is what a package command may not be called: the factory commands and
// the names the host answers by hand, and the commands of the user's own behaviour.
func packageNames(b behaviour) packageNamespace {
	mine := map[string]bool{}
	for _, n := range userCommandNames(b) {
		mine[n] = true
	}
	return packageNamespace{
		taken: func(name string) bool {
			return isFactoryCommand(name) || commandAliases[name] || plannedCommands[name] || mine[name]
		},
		builtins: func(name string) bool { _, ok := hostBuiltins[name]; return ok },
	}
}

// loadAllPackages reads the user's packages and the project's, with the consent the user
// gave, and merges them (the user's win over the project's).
func loadAllPackages(cwd string, b behaviour) packageSet {
	grants := readGrants()
	ns := packageNames(b)
	return mergePackages(
		loadPackages(userPackagesDir(), scopeUser, cwd, grants, ns),
		loadPackages(projectPackagesDir(cwd), scopeProject, cwd, grants, ns),
	)
}

// ---- what the menu and the line classifier know ------------------------------------

// The commands of the packages that may run, as the menu and the classifier read them.
// The loop sets them whenever the packages change; like the active behaviour they are
// read by code that has no access to the loop's variables.
var (
	activePkgMu   sync.RWMutex
	activePkgCmds map[string]int // command name -> words it needs
)

// setActivePackages publishes the commands of the packages that may run, so the / menu
// lists them and a typed /name is a command and not an unknown word. The menu itself is
// rebuilt by setActiveBehaviour, which the caller runs next.
func setActivePackages(s packageSet) {
	cmds := map[string]int{}
	for _, p := range s.pkgs {
		if !p.runnable() {
			continue
		}
		for _, c := range p.Commands {
			if !c.Disabled {
				cmds[c.Name] = c.MinArgs
			}
		}
	}
	activePkgMu.Lock()
	activePkgCmds = cmds
	activePkgRows = packageMenu(s)
	activePkgMu.Unlock()
}

// activePkgRows is the menu rows of those commands.
var activePkgRows []fold.SlashMatch

func packageRows() []fold.SlashMatch {
	activePkgMu.RLock()
	defer activePkgMu.RUnlock()
	return append([]fold.SlashMatch(nil), activePkgRows...)
}

func packageMenu(s packageSet) []fold.SlashMatch {
	var out []fold.SlashMatch
	for _, p := range s.pkgs {
		if !p.runnable() {
			continue
		}
		for _, c := range p.Commands {
			if c.Disabled {
				continue
			}
			cat, desc := c.Category, c.Description
			if cat == "" {
				cat = "Custom"
			}
			if desc == "" {
				desc = "from package " + p.ID
			}
			out = append(out, fold.SlashMatch{Name: c.Name, Category: cat, Description: cleanWarnText(desc)})
		}
	}
	return out
}

// packageCommandNames lists the commands of the packages that may run.
func packageCommandNames() []string {
	activePkgMu.RLock()
	defer activePkgMu.RUnlock()
	out := make([]string, 0, len(activePkgCmds))
	for n := range activePkgCmds {
		out = append(out, n)
	}
	return out
}

// packageNeedsArgs reports whether picking /name from the menu should leave the line open
// for the words it needs, as /send does, instead of running it with none.
func packageNeedsArgs(name string) bool {
	activePkgMu.RLock()
	defer activePkgMu.RUnlock()
	return activePkgCmds[name] > 0
}

// knownCommandNames is every name the user may have added: their own behaviour's and the
// packages'.
func knownCommandNames(b behaviour) []string {
	return append(userCommandNames(b), packageCommandNames()...)
}

// ---- finding the command a line names ---------------------------------------------

// packageLine resolves what Enter means on the / line: the package command the line
// names (with the words after it), or, for a bare "/..." with the menu open, the
// highlighted row when it is a package command. ok is false for anything else, so every
// other command keeps its own path.
func (s packageSet) packageLine(input string, sel int, cat string) (name, args string, ok bool) {
	body := strings.TrimSpace(input)
	if !strings.HasPrefix(body, "/") {
		return "", "", false
	}
	body = strings.TrimSpace(body[1:])
	word, rest := body, ""
	if i := strings.IndexAny(body, " \t\r\n"); i >= 0 {
		word, rest = body[:i], strings.TrimSpace(body[i:])
	}
	if _, _, found := s.commandOwner(word); found {
		return word, rest, true
	}
	if rest != "" || word == "" {
		return "", "", false
	}
	matches := fold.FilterSlashCategory(word, fold.NormalizeSlashCategory(word, cat))
	if len(matches) == 0 {
		return "", "", false
	}
	for _, m := range matches {
		if m.Name == word {
			return "", "", false // an exact name of another command belongs to it
		}
	}
	if sel < 0 || sel >= len(matches) {
		sel = len(matches) - 1
	}
	if _, _, found := s.commandOwner(matches[sel].Name); found {
		return matches[sel].Name, "", true
	}
	return "", "", false
}

// inertOwner explains a package that declares /name but cannot run it, so the user is
// told why a command they may have seen documented does nothing.
func (s packageSet) inertOwner(name string) (string, bool) {
	for _, p := range s.pkgs {
		if p.runnable() {
			continue
		}
		for _, c := range p.Commands {
			if c.Name != name {
				continue
			}
			switch {
			case p.Disabled:
				return fmt.Sprintf("/%s belongs to the package %q, which is switched off.", cleanWarnText(name), p.ID), true
			case !p.Granted:
				return fmt.Sprintf("/%s belongs to the package %q, which you have not approved yet (or which changed since you did), so it did not run.", cleanWarnText(name), p.ID), true
			}
		}
	}
	return "", false
}

// hasRules reports whether any package that may run has an input.pre rule to apply.
func (s packageSet) hasRules() bool { return s.hasRuleOn(extv2.StageInputPre) }

// hasPostRules reports whether any package that may run has an output.post rule, which is
// what decides whether the loop holds an answer back for a moment before showing it.
func (s packageSet) hasPostRules() bool { return s.hasRuleOn(extv2.StageOutputPost) }

func (s packageSet) hasRuleOn(stage string) bool {
	for _, p := range s.pkgs {
		if !p.runnable() {
			continue
		}
		for _, r := range p.Rules {
			if !r.Disabled && r.Stage == stage {
				return true
			}
		}
	}
	return false
}

// ---- running without holding the interface up -------------------------------------

// newPackageRunner makes a runner for the packages as they are now. The folders are copied
// into it, so a worker never reads the loop's variable while the loop changes it.
func newPackageRunner(s packageSet) *extv2.Runner {
	dirs := map[string]string{}
	for _, p := range s.pkgs {
		if p.Dir != "" {
			dirs[p.ID] = p.Dir
		}
	}
	return &extv2.Runner{
		Builtins:   hostBuiltins,
		PackageDir: func(id string) string { return dirs[id] },
	}
}

// pkgOutcome is what a command worker hands back to the loop.
type pkgOutcome struct {
	pkg, command string
	out          extv2.Outcome
	err          error
}

// startPackageCommand runs one command on a worker. A crash in a builtin is an error of
// that command, not of the host.
func startPackageCommand(ctx context.Context, r *extv2.Runner, p installedPackage, c extv2.Command, args string, done chan<- pkgOutcome) {
	go func() {
		res := pkgOutcome{pkg: p.ID, command: c.Name}
		defer func() {
			if rec := recover(); rec != nil {
				res.out, res.err = extv2.Outcome{}, fmt.Errorf("the command crashed (%v)", rec)
			}
			done <- res
		}()
		res.out, res.err = r.RunCommand(ctx, p.Package, c, args)
	}()
}

// pipeOutcome is what the input.pre worker hands back.
type pipeOutcome struct {
	original string
	res      extv2.Result
}

// startInputPre runs the rules over one plain-text line on a worker, so a slow rule
// never freezes the loop or the panic gesture. The runner is bounded by PipelineBudget.
func startInputPre(ctx context.Context, r *extv2.Runner, pkgs []*extv2.Package, line string, done chan<- pipeOutcome) {
	go func() {
		out := pipeOutcome{original: line, res: extv2.Result{Line: line}}
		defer func() {
			if rec := recover(); rec != nil {
				out.res = extv2.Result{Line: line, Notes: []extv2.Note{{Message: fmt.Sprintf("the rules crashed (%v); the line was sent as typed", rec)}}}
			}
			done <- out
		}()
		out.res = r.RunInputPre(ctx, pkgs, line)
	}()
}

// postOutcome is what the output.post worker hands back: the answer's event as the model
// made it, the text to show instead, and what the rules had to say.
type postOutcome struct {
	event fold.Event
	epoch int
	res   extv2.PostResult
}

// answerShown is the event the chat draws for an answer: the same event with the text the
// rules chose. The model's own event is not touched (the payload is copied, as
// sentPaste.forChat does), because the session log and the model's memory keep its words.
func answerShown(e fold.Event, text string) fold.Event {
	payload := make(map[string]any, len(e.Payload))
	for k, v := range e.Payload {
		payload[k] = v
	}
	payload["text"] = text
	e.Payload = payload
	return e
}

// startOutputPost runs the output.post rules over one answer on a worker, so a slow rule
// never freezes the loop or the panic gesture. The runner is bounded by PipelineBudget, and
// a crash leaves the answer as the model said it.
func startOutputPost(ctx context.Context, r *extv2.Runner, pkgs []*extv2.Package, e fold.Event, epoch int, done chan<- postOutcome) {
	text, _ := e.Payload["text"].(string)
	go func() {
		out := postOutcome{event: e, epoch: epoch, res: extv2.PostResult{Text: text}}
		defer func() {
			if rec := recover(); rec != nil {
				out.res = extv2.PostResult{Text: text, Notes: []extv2.Note{{Message: fmt.Sprintf("the rules crashed (%v); the answer is shown as the model said it", rec)}}}
			}
			done <- out
		}()
		out.res = r.RunOutputPost(ctx, pkgs, text)
	}()
}

// commandResult turns what a command did into what the user sees: notes (what it said),
// warnings (why it failed), and the text it may send to the model.
func commandResult(o pkgOutcome) (notes, warnings []string, send string) {
	if o.err != nil {
		if ue, ok := o.err.(*extv2.UsageError); ok {
			return nil, []string{cleanWarnText(ue.Error())}, ""
		}
		return nil, []string{"/" + cleanWarnText(o.command) + ": " + cleanWarnText(o.err.Error())}, ""
	}
	if o.out.Say != "" {
		notes = append(notes, o.out.Say)
	}
	send = o.out.Send
	if strings.HasPrefix(strings.TrimSpace(send), "/") {
		// A line that starts with "/" is a command, and the chat session reads one as
		// a switch of mode. What a package sends is text for the model, never a command.
		return notes, []string{"/" + cleanWarnText(o.command) + ": it tried to send a line that starts with \"/\", which is a command and not text for the model, so nothing was sent."}, ""
	}
	if o.out.Say == "" && send == "" {
		notes = append(notes, "/"+cleanWarnText(o.command)+": it ran and had nothing to say.")
	}
	return notes, nil, send
}
