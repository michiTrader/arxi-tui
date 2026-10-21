// Package script is tier 2 of the open interface (docs/DESIGN-OPEN-TUI.md section 5.4): a small
// interpreter for a subset of JavaScript, written here and with no dependency, for the logic
// that has state and that a document or an expression cannot say: a message queue, a state
// machine, a text effect, a custom widget.
//
// The language is the part of JavaScript that models write well and that cannot hurt the
// screen: let and const, if, for, for-of and while, functions and arrow functions, objects,
// arrays, template texts and a small standard library. Three decisions are the point:
//
//   - It cannot freeze or exhaust the program. Every script runs under Limits: a number of
//     steps, a budget of what a call may allocate, a depth of calls, and a cap on the size of
//     the state that is kept between calls. The limits are owned here; none is a hope that the
//     script behaves.
//   - Types are strict, as in tier 1 (package expr). There is no truthiness: a condition must
//     be a yes/no, and `==` does not exist (use `===`). The two mistakes a model makes silently
//     are refused where they happen, with the line to write instead.
//   - What is missing says what to use instead. `class`, `new`, `try`, `switch`, regular
//     expressions and the rest of what was left out are refused with a sentence of the form
//     "`class` is not available. Remedy: use an object with functions." That sentence is the
//     repair loop the evaluation in the design document measures.
//
// A script is loaded once (its top level runs: it declares state and functions) and then its
// functions are called by name, each call under its own limits. Everything the script can
// touch is what the host puts in Globals; nothing else is reachable (no files, no clock, no
// network, no randomness unless the host supplies it).
package script

import (
	"fmt"
	"sort"
	"strings"
)

// Kind classifies an Error.
type Kind string

const (
	KindSyntax  Kind = "syntax"  // the text is not in the language
	KindRuntime Kind = "runtime" // a statement could not be carried out
	KindLimit   Kind = "limit"   // the script was stopped by a limit
)

// Error is a refusal that says where, what happened and what to write instead.
type Error struct {
	Kind Kind
	Name string // the script's name, for the message
	Line int
	Col  int
	Msg  string
}

func (e *Error) Error() string {
	where := e.Name
	if where == "" {
		where = "script"
	}
	if e.Line > 0 {
		where = fmt.Sprintf("%s:%d:%d", where, e.Line, e.Col)
	}
	return fmt.Sprintf("%s: %s error: %s", where, e.Kind, e.Msg)
}

// Limits bound what a script may do. The zero value of a field means its default.
type Limits struct {
	Steps  int // nodes evaluated by one load or one call
	Alloc  int // bytes one load or one call may allocate, cumulative (nothing is freed in it)
	Depth  int // nested calls
	Live   int // bytes of state the script may keep between calls
	String int // longest text, in bytes
}

// DefaultLimits is what a script gets when the host says nothing. A step is cheap, so a screen
// callback that needs more than this is doing too much for one frame.
var DefaultLimits = Limits{Steps: 200_000, Alloc: 4 << 20, Depth: 64, Live: 1 << 20, String: 1 << 20}

func (l Limits) withDefaults() Limits {
	d := DefaultLimits
	if l.Steps > 0 {
		d.Steps = l.Steps
	}
	if l.Alloc > 0 {
		d.Alloc = l.Alloc
	}
	if l.Depth > 0 {
		d.Depth = l.Depth
	}
	if l.Live > 0 {
		d.Live = l.Live
	}
	if l.String > 0 {
		d.String = l.String
	}
	return d
}

// refused maps a word that exists in JavaScript and not here to the sentence that says what to
// use. The parser checks it wherever the word would start a statement, an expression or follow
// an operand.
var refused = map[string]string{
	"class":      "`class` is not available. Remedy: use an object with functions, `const counter = { n: 0, add() { ... } }`, or a function that returns one.",
	"new":        "`new` is not available. Remedy: call a function that returns the value, `makeQueue()`, or write the object or array literal.",
	"this":       "`this` is not available. Remedy: name the object (`const q = {...}`) and use `q.n` inside its functions.",
	"async":      "`async` is not available: a script never waits. Remedy: write a plain function; the host calls it when the event happens.",
	"await":      "`await` is not available: a script never waits. Remedy: keep the state in a variable and handle the next event when it arrives.",
	"yield":      "`yield` is not available. Remedy: keep the state in a variable and return one step per call.",
	"import":     "`import` is not available: a script sees only what the host gives it. Remedy: use the globals listed by the host.",
	"export":     "`export` is not available. Remedy: declare a function at the top level; the host calls it by name.",
	"try":        "`try` is not available. Remedy: check the value before using it (`if (x !== null) {...}`); an error stops the call with a message.",
	"catch":      "`catch` is not available. Remedy: check the value before using it.",
	"finally":    "`finally` is not available. Remedy: do the clean-up after the code that needed it.",
	"throw":      "`throw` is not available. Remedy: return a value that says what went wrong, `return { ok: false, why: '...' }`.",
	"switch":     "`switch` is not available. Remedy: use `if (...) {...} else if (...) {...}`.",
	"case":       "`case` is not available: there is no `switch`. Remedy: use `if (...) {...} else if (...) {...}`.",
	"default":    "`default` is not available: there is no `switch`. Remedy: use `else`.",
	"do":         "`do ... while` is not available. Remedy: use `while (cond) {...}`; write the body once before it if it must run at least once.",
	"delete":     "`delete` is not available. Remedy: build the object again without the key, or set the key to `null`.",
	"with":       "`with` is not available. Remedy: name the object and write `obj.key`.",
	"var":        "`var` is not available. Remedy: use `let` for a value that changes and `const` for one that does not.",
	"in":         "`in` is not available. Remedy: use `obj.key !== undefined`, `arr.includes(x)`, or loop over `Object.keys(obj)` with `for (const k of ...)`.",
	"instanceof": "`instanceof` is not available. Remedy: use `typeof x === 'object'` or `Array.isArray(x)`.",
	"void":       "`void` is not available. Remedy: write the expression alone.",
	"static":     "`static` is not available: there are no classes. Remedy: use a plain function or object.",
	"extends":    "`extends` is not available: there are no classes. Remedy: build an object with `Object.assign({}, base, {...})`.",
	"super":      "`super` is not available: there are no classes. Remedy: call the other object's function by name.",
	"debugger":   "`debugger` is not available. Remedy: remove it.",
	"enum":       "`enum` is not available. Remedy: use an object of constants, `const Mode = { A: 'a', B: 'b' }`.",
}

// refusedGlobals are names a model reaches for that do not exist here. They are refused where an
// undeclared name is found, and a host that declares one of them gets it.
var refusedGlobals = map[string]string{
	"require":     "`require` is not available: a script sees only what the host gives it. Remedy: use the globals listed by the host.",
	"Symbol":      "`Symbol` is not available. Remedy: use a text as the key.",
	"Promise":     "`Promise` is not available: a script never waits. Remedy: handle the next event when it arrives.",
	"setTimeout":  "`setTimeout` is not available: a script has no clock of its own. Remedy: use the time the host passes to the function, or have the host call your function on a tick.",
	"setInterval": "`setInterval` is not available: a script has no clock of its own. Remedy: have the host call your function on each tick.",
	"eval":        "`eval` is not available. Remedy: write the code instead of building it as text.",
	"Function":    "`Function` is not available. Remedy: write the function as `(x) => ...`.",
	"console":     "`console` is not available: a script has no terminal. Remedy: return the text, or use the host's `log` if it gives one.",
	"window":      "`window` is not available: a script sees only what the host gives it. Remedy: use the globals listed by the host.",
	"globalThis":  "`globalThis` is not available. Remedy: name what you need; the host's globals are already in scope.",
	"process":     "`process` is not available: a script sees only what the host gives it.",
	"Date":        "`Date` is not available: a script has no clock. Remedy: use the time the host passes to the function.",
	"RegExp":      "`RegExp` is not available. Remedy: use `includes`, `startsWith`, `endsWith`, `indexOf` or `split`.",
	"Map":         "`Map` is not available. Remedy: use an object with text keys, `const seen = {}`.",
	"Set":         "`Set` is not available. Remedy: use an array with `includes`, or an object with text keys.",
	"BigInt":      "`BigInt` is not available. Remedy: use a number; integers are exact up to 9007199254740991.",
	"parseInt":    "`parseInt` is not available. Remedy: use `Number(text)`, which gives `null` when the text is not a number, and `Math.trunc(n)`.",
	"parseFloat":  "`parseFloat` is not available. Remedy: use `Number(text)`, which gives `null` when the text is not a number.",
	"isNaN":       "`isNaN` is not available: a number here is never NaN. Remedy: use `Number(text) === null` to test a text.",
}

// nearest returns up to three of the names closest to want, for the "did you mean" of a name
// that is not defined.
func nearest(want string, names []string) []string {
	type cand struct {
		n string
		d int
	}
	var cs []cand
	seen := map[string]bool{}
	for _, n := range names {
		if seen[n] || n == want {
			continue
		}
		seen[n] = true
		d := editDistance(strings.ToLower(want), strings.ToLower(n))
		limit := 2
		if len(want) > 5 {
			limit = 3
		}
		if d <= limit || strings.HasPrefix(n, want) || strings.HasPrefix(want, n) && len(n) >= 3 {
			cs = append(cs, cand{n, d})
		}
	}
	sort.Slice(cs, func(i, j int) bool {
		if cs[i].d != cs[j].d {
			return cs[i].d < cs[j].d
		}
		return cs[i].n < cs[j].n
	})
	var out []string
	for i := 0; i < len(cs) && i < 3; i++ {
		out = append(out, cs[i].n)
	}
	return out
}

func editDistance(a, b string) int {
	ra, rb := []rune(a), []rune(b)
	prev := make([]int, len(rb)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(ra); i++ {
		cur := make([]int, len(rb)+1)
		cur[0] = i
		for j := 1; j <= len(rb); j++ {
			c := 1
			if ra[i-1] == rb[j-1] {
				c = 0
			}
			cur[j] = min(prev[j]+1, cur[j-1]+1, prev[j-1]+c)
		}
		prev = cur
	}
	return prev[len(rb)]
}
