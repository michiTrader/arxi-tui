// Package expr is tier 1 of the open interface (docs/DESIGN-OPEN-TUI.md section 5.4): a small
// expression language for conditions and short calculations, such as
//
//	focus == 'prompt' && input.at_end && shells.count > 0
//
// It exists so that a key, a menu entry or a format can say WHEN it applies as data, instead of
// the answer being decided in Go. It is deliberately small and cannot loop, so every expression
// ends: there are no functions, no assignment and no recursion, and the size of an expression
// and of its result are bounded.
//
// Three properties are the point of the package:
//
//   - Names are checked when the expression is compiled, against a schema the host supplies
//     (name -> kind). A misspelt name is refused where it is written, with the nearest known
//     names, and not found out when a key is pressed.
//   - Types are strict. There is no truthiness: `when: "shells"` is refused with the
//     comparison to write instead, because "is that a number, a text or a yes/no" is exactly
//     the question a model gets wrong silently.
//   - Every refusal names what would have happened and what to write instead (the repair loop
//     the evaluation in docs/DESIGN-OPEN-TUI.md measures).
package expr

import (
	"fmt"
	"sort"
	"strings"
	"unicode/utf8"
)

// Kind is the type of a value.
type Kind int

const (
	Bool Kind = iota + 1
	Int
	String
)

func (k Kind) String() string {
	switch k {
	case Bool:
		return "yes/no"
	case Int:
		return "number"
	case String:
		return "text"
	}
	return "unknown"
}

// Limits. An expression is read by people and models, so these are generous for any real
// condition and tight for anything that is a program in disguise.
const (
	MaxSourceRunes = 400
	MaxNodes       = 120
	MaxDepth       = 24
	// MaxInt keeps arithmetic far from overflow: a result past it is an error, not a wrap.
	MaxInt = 1 << 40
	// MaxStringRunes bounds a text built by "+".
	MaxStringRunes = 1000
)

// Value is a typed value: a yes/no, a whole number or a text.
type Value struct {
	K Kind
	B bool
	I int64
	S string
}

// B, N and S build values.
func B(v bool) Value   { return Value{K: Bool, B: v} }
func N(v int64) Value  { return Value{K: Int, I: v} }
func S(v string) Value { return Value{K: String, S: v} }

func (v Value) String() string {
	switch v.K {
	case Bool:
		return fmt.Sprint(v.B)
	case Int:
		return fmt.Sprint(v.I)
	case String:
		return v.S
	}
	return ""
}

// Schema says which names exist and of what kind. A name is dotted lowercase words:
// "input.at_end", "shells.count".
type Schema map[string]Kind

// Program is a compiled expression. It is immutable and safe to share between goroutines.
type Program struct {
	src  string
	root node
	kind Kind
	uses []string
}

// Source is the text the program was compiled from.
func (p *Program) Source() string { return p.src }

// Kind is what the program yields.
func (p *Program) Kind() Kind { return p.kind }

// Names lists the names the program reads, sorted.
func (p *Program) Names() []string { return append([]string(nil), p.uses...) }

// Compile reads src, checks every name against schema and every operator against the kinds it
// is given, and checks that the result is of the kind want. The error says where and what to
// write instead.
func Compile(src string, schema Schema, want Kind) (*Program, error) {
	if strings.TrimSpace(src) == "" {
		return nil, fmt.Errorf("the expression is empty, so it would never say anything; write a condition such as shells.count > 0")
	}
	if n := utf8.RuneCountInString(src); n > MaxSourceRunes {
		return nil, fmt.Errorf("the expression is %d characters, more than the %d allowed; split it into smaller conditions", n, MaxSourceRunes)
	}
	toks, err := lex(src)
	if err != nil {
		return nil, err
	}
	p := &parser{toks: toks, src: src}
	root, err := p.parseExpr(0)
	if err != nil {
		return nil, err
	}
	if t := p.peek(); t.kind != tEOF {
		return nil, p.errAt(t, "%q is not expected here, after a complete expression; join conditions with && or ||", t.text)
	}
	if p.nodes > MaxNodes {
		return nil, fmt.Errorf("the expression has %d parts, more than the %d allowed; split it into smaller conditions", p.nodes, MaxNodes)
	}
	c := &checker{schema: schema, src: src, used: map[string]bool{}}
	k, err := c.check(root)
	if err != nil {
		return nil, err
	}
	if k != want {
		return nil, fmt.Errorf("the expression gives a %s, and a %s is needed here%s", k, want, wantHint(src, k, want))
	}
	uses := make([]string, 0, len(c.used))
	for n := range c.used {
		uses = append(uses, n)
	}
	sort.Strings(uses)
	return &Program{src: src, root: root, kind: k, uses: uses}, nil
}

func wantHint(src string, got, want Kind) string {
	x := strings.TrimSpace(src)
	if strings.ContainsAny(x, " ") {
		x = "(" + x + ")"
	}
	if utf8.RuneCountInString(x) > 40 {
		x = "x"
	}
	switch {
	case want == Bool && got == Int:
		return fmt.Sprintf("; compare it, for example `%s > 0` or `%s == 1`", x, x)
	case want == Bool && got == String:
		return fmt.Sprintf("; compare it, for example `%s == 'name'`", x)
	}
	return ""
}

// Lookup returns the current value of a name. ok is false when there is none right now.
type Lookup func(name string) (v Value, ok bool)

// Eval runs the program. It fails, and says so, when a name has no value or has a value of
// another kind than the schema promised, or when a calculation leaves the allowed range; it
// never guesses.
func (p *Program) Eval(get Lookup) (Value, error) {
	return eval(p.root, get)
}

// EvalBool is Eval for a program compiled with want == Bool.
func (p *Program) EvalBool(get Lookup) (bool, error) {
	if p.kind != Bool {
		return false, fmt.Errorf("the expression gives a %s, not a yes/no", p.kind)
	}
	v, err := p.Eval(get)
	if err != nil {
		return false, err
	}
	return v.B, nil
}
