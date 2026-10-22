package main

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/michiTrader/arxi_tui/internal/verb"
)

// verbEnv is what a verb works on. typed is true only for a line the user typed
// themselves; a button, a shortcut, a command or a hook of their behaviour never sets it.
type verbEnv struct {
	shells *shellRegistry
	dir    string
	typed  bool
	// ui carries out the verbs that change the loop's own view (the expanded tools, the find
	// bar). Only the loop has that state, so it hands this in; nil means there is no screen.
	ui func(verb string) string
	// script runs a user script (scripts.go) and returns the note to show. Only the loop
	// has the behaviour layer, the facts and the action queue it needs; nil means no screen.
	script func(name, text string) string
}

// verbImpl is what each verb of internal/verb does. A test holds the two lists together:
// a verb without an entry here, or an entry without a verb, fails.
var verbImpl = map[string]func(args []string, env verbEnv) string{
	"shell.start": verbShellStart,
	"shell.stop":  verbShellStop,
	"shell.read":  verbShellRead,
	"view.expand": func(_ []string, env verbEnv) string { return verbUI("view.expand", env) },
	"find.toggle": func(_ []string, env verbEnv) string { return verbUI("find.toggle", env) },
	"queue.push":  func(_ []string, env verbEnv) string { return verbUI("queue.push", env) },
	"focus.input": func(_ []string, env verbEnv) string { return verbUI("focus.input", env) },
	"queue.steer": func(_ []string, env verbEnv) string { return verbUI("queue.steer", env) },
	"script.run":  verbScriptRun,
}

// verbScriptRun splits "<name> [text]" and hands it to the loop.
func verbScriptRun(args []string, env verbEnv) string {
	if env.script == nil {
		return "script.run: this session has no screen to run a script on; nothing was done"
	}
	name, text, _ := strings.Cut(strings.TrimSpace(args[0]), " ")
	return env.script(name, strings.TrimSpace(text))
}

// queuedVerbEnv is the environment of a verb that came out of the loop's action queue. What
// reaches the queue was made by a button, a key, a command or a hook, never typed, so typed
// is false: a typed verb that got past every earlier check still starts nothing.
func queuedVerbEnv(drv any, dir string) verbEnv {
	env := verbEnv{dir: dir}
	if h, ok := drv.(shellHost); ok {
		env.shells = h.Shells()
	}
	return env
}

// runVerb carries out one parsed call and returns what to show, as a note. It is the one
// place that enforces a verb's protection, so a door that forgot to check cannot start
// what only the user may start.
func runVerb(c verb.Call, env verbEnv) string {
	if c.Verb.Protection == verb.Typed && !env.typed {
		return fmt.Sprintf("%s only works when you type it yourself; a shortcut, button or command of the interface cannot start it. Nothing was done.", c.Verb.Name)
	}
	fn, ok := verbImpl[c.Verb.Name]
	if !ok {
		return c.Verb.Name + " has no implementation in this build; nothing was done"
	}
	return fn(c.Args, env)
}

// runVerbLine parses a `do:` argument and runs it.
func runVerbLine(line string, env verbEnv) string {
	c, err := verb.Parse(line)
	if err != nil {
		return err.Error()
	}
	return runVerb(c, env)
}

// verbUI runs a verb that changes the view. Without a screen to change (a line queued by a
// hook of a session that draws nothing) it says so and does nothing.
func verbUI(name string, env verbEnv) string {
	if env.ui == nil {
		return name + ": this session has no screen to change; nothing was done"
	}
	return env.ui(name)
}

func verbShellStart(args []string, env verbEnv) string {
	if env.shells == nil {
		return "/shell: this session has no tools to run commands with"
	}
	info, err := env.shells.start(args[0], env.dir)
	if err != nil {
		return "/shell: " + err.Error()
	}
	return fmt.Sprintf("/shell: started shell %d: %s\nIt runs in the background; /shell show %d reads it, /shell stop %d ends it.", info.ID, info.Command, info.ID, info.ID)
}

func verbShellStop(args []string, env verbEnv) string {
	r := env.shells
	if r == nil {
		return "/shell: this session has no tools to run commands with"
	}
	if args[0] == "all" {
		if n := r.stopAll(); n > 0 {
			return fmt.Sprintf("/shell: stopped %d shell(s)", n)
		}
		return "/shell: no shell was running"
	}
	id, _ := strconv.Atoi(args[0])
	if !r.stop(id) {
		return "/shell: shell " + args[0] + " is not running"
	}
	return fmt.Sprintf("/shell: stopped shell %d", id)
}

func verbShellRead(args []string, env verbEnv) string {
	r := env.shells
	if r == nil {
		return "/shell: this session has no tools to run commands with"
	}
	id, _ := strconv.Atoi(args[0])
	lines := 30
	if len(args) > 1 {
		lines, _ = strconv.Atoi(args[1])
	}
	s, ok := r.find(id)
	if !ok {
		return fmt.Sprintf("/shell: there is no shell %d", id)
	}
	text, _ := s.output(false, maxShellRead)
	info := s.snapshot()
	head := fmt.Sprintf("shell %d (%s): %s", info.ID, shellStateWord(info), info.Command)
	if strings.TrimSpace(text) == "" {
		return head + "\n(no output yet)"
	}
	return head + "\n" + lastLines(text, lines)
}

// verbsGuide is the VERBS chapter of ui_guide. It is written from the catalogue, so a verb
// added there is told to the model without anyone remembering to document it.
func verbsGuide() string {
	var b strings.Builder
	b.WriteString("VERBS. A button's on_press, and a key or command of the user's, may call a verb of the program with do:<verb> <arguments>. Inside a list row, {row.<field>} fills an argument. A verb marked typed starts something that keeps running, so only a line the user types may start it: a button or key cannot carry it. (A /shell start line that did not come from the user's own typing is refused the same way.)\n")
	for _, v := range verb.All() {
		fmt.Fprintf(&b, "  do:%s  (%s) %s\n", v.Usage(), v.Protection, v.Doc)
		for _, p := range v.Params {
			fmt.Fprintf(&b, "      <%s> %s\n", p.Name, p.Doc)
		}
	}
	return b.String()
}
