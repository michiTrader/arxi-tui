package script

import (
	"fmt"
	"sort"
)

// HostFunc is a function the host gives a script. Arguments arrive as plain Go values (nil,
// bool, float64, string, []any, map[string]any) and so must the result. An error becomes a
// runtime error of the script, at the line of the call.
type HostFunc func(args []any) (any, error)

// Config is what the host decides about a script.
type Config struct {
	// Name appears in every message ("queue.js:3:5: runtime error: ...").
	Name string
	// Globals are the names the script may use besides the language's own. A HostFunc is a
	// function; any other value is read-only data (copied in).
	Globals map[string]any
	Limits  Limits
}

// Script is a loaded script: its top level has run and its functions can be called by name.
// It is not safe for concurrent use; a host that calls it from two goroutines locks.
type Script struct {
	name  string
	lim   Limits
	root  *env
	prog  *env
	hosts []string
}

// Load parses src, checks every name in it and runs its top level under the limits. Nothing
// in a script runs before Load returns, except its top level.
func Load(src string, cfg Config) (*Script, error) {
	lim := cfg.Limits.withDefaults()
	hosts := make([]string, 0, len(cfg.Globals))
	for n := range cfg.Globals {
		if _, clash := builtinNames[n]; clash {
			return nil, fmt.Errorf("script %q: the host global %q has the name of something the language gives. Remedy: choose another name", cfg.Name, n)
		}
		hosts = append(hosts, n)
	}
	sort.Strings(hosts)
	body, err := parseProgram(cfg.Name, src, hosts)
	if err != nil {
		return nil, err
	}
	s := &Script{name: cfg.Name, lim: lim, hosts: hosts}
	s.root = newEnv(nil)
	s.root.root = true
	installBuiltins(s.root)
	for _, n := range hosts {
		v := cfg.Globals[n]
		if hf, ok := v.(HostFunc); ok {
			s.root.declare(n, hostNative(n, hf), true)
			continue
		}
		if hf, ok := v.(func([]any) (any, error)); ok {
			s.root.declare(n, hostNative(n, HostFunc(hf)), true)
			continue
		}
		iv, err := importValue(v, 0)
		if err != nil {
			return nil, fmt.Errorf("script %q: the host global %q: %w", cfg.Name, n, err)
		}
		s.root.declare(n, iv, true)
	}
	s.prog = newEnv(s.root)
	if err := s.run(func(it *interp) {
		it.hoist(body, s.prog)
		it.execBlock(body, s.prog)
	}); err != nil {
		return nil, err
	}
	return s, nil
}

func hostNative(name string, hf HostFunc) *Native {
	return &Native{name: name, fn: func(it *interp, args []any) any {
		out := make([]any, len(args))
		for i, a := range args {
			ev, err := exportValue(a, 0)
			if err != nil {
				it.fail(pos{}, "`%s` was given an argument it cannot take: %v.", name, err)
			}
			out[i] = ev
		}
		res, err := hf(out)
		if err != nil {
			it.fail(pos{}, "`%s` failed: %v.", name, err)
		}
		iv, err := importValue(res, 0)
		if err != nil {
			it.fail(pos{}, "`%s` returned a value a script cannot hold: %v.", name, err)
		}
		return iv
	}}
}

// run executes f under fresh counters and turns every way it can stop into an *Error.
func (s *Script) run(f func(it *interp)) (err error) {
	it := &interp{name: s.name, lim: s.lim}
	defer func() {
		if r := recover(); r != nil {
			if e, ok := r.(*Error); ok {
				err = e
				return
			}
			err = &Error{Kind: KindRuntime, Name: s.name, Msg: fmt.Sprintf("internal error: %v. This is a bug in the interpreter, not in the script.", r)}
		}
	}()
	f(it)
	if n := envSize(s.prog, map[any]bool{}, s.lim.Live); n > s.lim.Live {
		return &Error{Kind: KindLimit, Name: s.name, Msg: fmt.Sprintf("the state the script keeps (about %d bytes) is larger than the %d allowed. Remedy: keep less between calls: drop old entries from lists, and cut long texts.", n, s.lim.Live)}
	}
	return nil
}

// Has reports whether the script declared a function with this name.
func (s *Script) Has(name string) bool {
	b := s.prog.vars[name]
	if b == nil {
		return false
	}
	_, ok := b.v.(*Closure)
	return ok
}

// Functions lists the names of the functions the script declared, sorted.
func (s *Script) Functions() []string {
	var out []string
	for n, b := range s.prog.vars {
		if _, ok := b.v.(*Closure); ok {
			out = append(out, n)
		}
	}
	sort.Strings(out)
	return out
}

// Call runs the function `name` with the arguments and returns what it returns, as plain Go.
func (s *Script) Call(name string, args ...any) (any, error) {
	b := s.prog.vars[name]
	if b == nil || !b.init {
		msg := fmt.Sprintf("the script has no function `%s`.", name)
		if near := nearest(name, s.Functions()); len(near) > 0 {
			msg += " Did you mean `" + near[0] + "`?"
		}
		return nil, &Error{Kind: KindRuntime, Name: s.name, Msg: msg + " Remedy: declare `function " + name + "(...) { ... }` at the top level."}
	}
	fn, ok := b.v.(*Closure)
	if !ok {
		return nil, &Error{Kind: KindRuntime, Name: s.name, Msg: fmt.Sprintf("`%s` is %s, not a function.", name, article(b.v))}
	}
	in := make([]any, len(args))
	for i, a := range args {
		iv, err := importValue(a, 0)
		if err != nil {
			return nil, &Error{Kind: KindRuntime, Name: s.name, Msg: fmt.Sprintf("argument %d of `%s`: %v.", i+1, name, err)}
		}
		in[i] = iv
	}
	var out any
	err := s.run(func(it *interp) {
		r := it.call(fn, in, pos{})
		ev, err := exportValue(r, 0)
		if err != nil {
			it.fail(pos{}, "`%s` returned a value the host cannot take: %v.", name, err)
		}
		out = ev
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// Eval is a convenience for a one-shot script: it loads src and returns the value of the
// function `main()`, or nil when there is none. It is what a test of the language uses.
func Eval(src string, cfg Config) (any, error) {
	s, err := Load(src, cfg)
	if err != nil {
		return nil, err
	}
	if !s.Has("main") {
		return nil, nil
	}
	return s.Call("main")
}
