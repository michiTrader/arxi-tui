package script

import (
	"fmt"
	"math"
	"strings"
)

type binding struct {
	v     any
	konst bool
	init  bool
}

type env struct {
	vars   map[string]*binding
	parent *env
	root   bool // the environment of the language and the host: not part of the script's own state
}

func newEnv(parent *env) *env { return &env{vars: map[string]*binding{}, parent: parent} }

func (e *env) lookup(name string) *binding {
	for c := e; c != nil; c = c.parent {
		if b, ok := c.vars[name]; ok {
			return b
		}
	}
	return nil
}

func (e *env) declare(name string, v any, konst bool) {
	e.vars[name] = &binding{v: v, konst: konst, init: true}
}

type ctl int

const (
	ctlNone ctl = iota
	ctlBreak
	ctlContinue
	ctlReturn
)

type interp struct {
	name  string
	lim   Limits
	steps int
	alloc int
	depth int
	ret   any
	last  pos // the call being made, for errors raised by a function of the library
}

func (it *interp) fail(at pos, format string, a ...any) {
	if at.line == 0 {
		at = it.last
	}
	panic(&Error{Kind: KindRuntime, Name: it.name, Line: at.line, Col: at.col, Msg: fmt.Sprintf(format, a...)})
}

func (it *interp) limit(at pos, format string, a ...any) {
	if at.line == 0 {
		at = it.last
	}
	panic(&Error{Kind: KindLimit, Name: it.name, Line: at.line, Col: at.col, Msg: fmt.Sprintf(format, a...)})
}

func (it *interp) step(at pos) {
	it.steps++
	if it.steps > it.lim.Steps {
		it.limit(at, "the script ran more than %d steps in one call and was stopped. Consequence: a loop that does not end would freeze the screen. Remedy: make the loop end (check its condition changes), or do less per call and keep the rest in a variable for the next call.", it.lim.Steps)
	}
}

func (it *interp) charge(at pos, n int) {
	it.alloc += n
	if it.alloc > it.lim.Alloc {
		it.limit(at, "the script allocated more than %d bytes in one call and was stopped. Remedy: build smaller lists, texts and objects, or reuse one instead of making a new one on every step.", it.lim.Alloc)
	}
}

func (it *interp) checkString(at pos, n int) {
	if n > it.lim.String {
		it.limit(at, "a text of %d bytes is longer than the %d allowed. Remedy: keep less text, or cut it with `slice`.", n, it.lim.String)
	}
}

// ---- statements

func patternNames(p pattern, out *[]string) {
	switch x := p.(type) {
	case *identP:
		*out = append(*out, x.name)
	case *arrayP:
		for _, el := range x.elems {
			if !el.skip {
				patternNames(el.pat, out)
			}
		}
	case *objectP:
		for _, pr := range x.props {
			patternNames(pr.pat, out)
		}
	}
}

// hoist makes the functions of a body callable from its first line and marks its let/const
// names as not yet set, so that a read before the declaration is an error and does not find
// an outer variable of the same name.
func (it *interp) hoist(body []stmt, e *env) {
	for _, s := range body {
		switch d := s.(type) {
		case *funcDecl:
			it.charge(d.pos, 48)
			e.vars[d.name] = &binding{v: &Closure{fn: d.fn, env: e}, init: true}
		case *varDecl:
			var names []string
			for _, dc := range d.decls {
				patternNames(dc.pat, &names)
			}
			for _, n := range names {
				e.vars[n] = &binding{konst: d.konst}
			}
		}
	}
}

func (it *interp) execBlock(body []stmt, e *env) ctl {
	for _, s := range body {
		if c := it.exec(s, e); c != ctlNone {
			return c
		}
	}
	return ctlNone
}

func (it *interp) exec(s stmt, e *env) ctl {
	switch n := s.(type) {
	case *exprS:
		it.step(n.pos)
		it.eval(n.x, e)
	case *varDecl:
		it.step(n.pos)
		for _, d := range n.decls {
			var v any
			if d.init != nil {
				v = it.eval(d.init, e)
			}
			it.bind(d.pat, v, e, n.konst)
		}
	case *funcDecl:
		it.step(n.pos)
	case *returnS:
		it.step(n.pos)
		var v any
		if n.x != nil {
			v = it.eval(n.x, e)
		}
		it.ret = v
		return ctlReturn
	case *ifS:
		it.step(n.pos)
		if it.cond(it.eval(n.c, e), n.pos, "`if`") {
			return it.exec(n.a, e)
		} else if n.b != nil {
			return it.exec(n.b, e)
		}
	case *blockS:
		it.step(n.pos)
		inner := e
		if n.scoped {
			it.charge(n.pos, 32)
			inner = newEnv(e)
			it.hoist(n.body, inner)
		}
		return it.execBlock(n.body, inner)
	case *whileS:
		for {
			it.step(n.pos)
			if !it.cond(it.eval(n.c, e), n.pos, "`while`") {
				return ctlNone
			}
			switch c := it.exec(n.body, e); c {
			case ctlBreak:
				return ctlNone
			case ctlReturn:
				return c
			}
		}
	case *forS:
		return it.execFor(n, e)
	case *forOfS:
		return it.execForOf(n, e)
	case *breakS:
		it.step(n.pos)
		return ctlBreak
	case *continueS:
		it.step(n.pos)
		return ctlContinue
	default:
		panic(&Error{Kind: KindRuntime, Name: it.name, Msg: fmt.Sprintf("internal error: unknown statement %T", s)})
	}
	return ctlNone
}

func (it *interp) copyEnv(e *env, at pos) *env {
	it.charge(at, 32+16*len(e.vars))
	c := newEnv(e.parent)
	for k, b := range e.vars {
		nb := *b
		c.vars[k] = &nb
	}
	return c
}

func (it *interp) execFor(n *forS, e *env) ctl {
	cur := e
	if n.scoped {
		cur = newEnv(e)
		it.charge(n.pos, 32)
	}
	if n.init != nil {
		it.exec(n.init, cur)
	}
	for {
		it.step(n.pos)
		if n.cond != nil && !it.cond(it.eval(n.cond, cur), n.pos, "`for`") {
			return ctlNone
		}
		switch c := it.exec(n.body, cur); c {
		case ctlBreak:
			return ctlNone
		case ctlReturn:
			return c
		}
		if n.scoped {
			cur = it.copyEnv(cur, n.pos) // a closure made in the body keeps this round's value
		}
		if n.post != nil {
			it.eval(n.post, cur)
		}
	}
}

func (it *interp) execForOf(n *forOfS, e *env) ctl {
	src := it.eval(n.iter, e)
	var items []any
	switch x := src.(type) {
	case *Array:
		items = append([]any(nil), x.elems...)
		it.charge(n.pos, 8*len(items))
	case string:
		for _, r := range x {
			items = append(items, string(r))
		}
		it.charge(n.pos, 16*len(items))
	default:
		it.fail(n.pos, "`for (... of ...)` needs a list or a text but got %s. Remedy: for the keys of an object write `for (const k of Object.keys(obj))`, for key and value `Object.entries(obj)`.", article(src))
	}
	for _, v := range items {
		it.step(n.pos)
		inner := newEnv(e)
		it.charge(n.pos, 32)
		it.bind(n.pat, v, inner, n.konst)
		switch c := it.exec(n.body, inner); c {
		case ctlBreak:
			return ctlNone
		case ctlReturn:
			return c
		}
	}
	return ctlNone
}

func (it *interp) cond(v any, at pos, what string) bool {
	b, ok := v.(bool)
	if !ok {
		it.fail(at, "the condition of %s is %s, not a yes/no. Remedy: compare it: `n !== 0`, `s !== ''`, `x !== null`, `list.length > 0`.", what, article(v))
	}
	return b
}

// bind takes a value apart into the names of a pattern, declaring each in e.
func (it *interp) bind(p pattern, v any, e *env, konst bool) {
	switch x := p.(type) {
	case *identP:
		it.charge(x.pos, 32)
		e.declare(x.name, v, konst)
	case *arrayP:
		a, ok := v.(*Array)
		if !ok {
			it.fail(x.pos, "cannot take %s apart as a list `[a, b]`. Remedy: check it is a list first with `Array.isArray(x)`.", article(v))
		}
		for i, el := range x.elems {
			if el.skip {
				continue
			}
			if el.rest {
				rest := &Array{}
				if i < len(a.elems) {
					rest.elems = append(rest.elems, a.elems[i:]...)
				}
				it.charge(x.pos, 8*len(rest.elems)+24)
				it.bind(el.pat, rest, e, konst)
				break
			}
			var ev any
			if i < len(a.elems) {
				ev = a.elems[i]
			}
			if ev == nil && el.def != nil {
				ev = it.eval(el.def, e)
			}
			it.bind(el.pat, ev, e, konst)
		}
	case *objectP:
		o, ok := v.(*Object)
		if !ok {
			it.fail(x.pos, "cannot take %s apart as an object `{a, b}`. Remedy: check it is not null first.", article(v))
		}
		for _, pr := range x.props {
			ev, _ := o.get(pr.key)
			if ev == nil && pr.def != nil {
				ev = it.eval(pr.def, e)
			}
			it.bind(pr.pat, ev, e, konst)
		}
	}
}

// ---- expressions

func exprName(n node) string {
	switch x := n.(type) {
	case *identX:
		return x.name
	case *memberX:
		return exprName(x.obj) + "." + x.name
	case *indexX:
		return exprName(x.obj) + "[...]"
	case *callX:
		return exprName(x.fn) + "(...)"
	case *constLit:
		return text(x.v)
	}
	return "this value"
}

func (it *interp) eval(n node, e *env) any {
	switch x := n.(type) {
	case *constLit:
		it.step(x.pos)
		return x.v
	case *identX:
		it.step(x.pos)
		b := e.lookup(x.name)
		if b == nil {
			it.fail(x.pos, "`%s` is not defined.", x.name)
		}
		if !b.init {
			it.fail(x.pos, "`%s` is used before the line that declares it has run. Remedy: move the `let`/`const` above its first use.", x.name)
		}
		return b.v
	case *tplLit:
		it.step(x.pos)
		var sb strings.Builder
		for _, p := range x.parts {
			if p.x == nil {
				sb.WriteString(p.lit)
			} else {
				sb.WriteString(text(it.eval(p.x, e)))
			}
			it.checkString(x.pos, sb.Len())
		}
		it.charge(x.pos, sb.Len()+16)
		return sb.String()
	case *arrayLit:
		it.step(x.pos)
		a := &Array{}
		for _, el := range x.elems {
			v := it.eval(el.x, e)
			if el.spread {
				src, ok := v.(*Array)
				if !ok {
					it.fail(x.pos, "`...` in a list needs a list but got %s. Remedy: spread a list, or write the value by itself.", article(v))
				}
				it.charge(x.pos, 8*len(src.elems))
				a.elems = append(a.elems, src.elems...)
			} else {
				it.charge(x.pos, 8)
				a.elems = append(a.elems, v)
			}
		}
		it.charge(x.pos, 24)
		return a
	case *objectLit:
		it.step(x.pos)
		o := newObject()
		it.charge(x.pos, 48)
		for _, p := range x.props {
			if p.spread {
				v := it.eval(p.value, e)
				src, ok := v.(*Object)
				if !ok {
					if v == nil {
						continue
					}
					it.fail(x.pos, "`...` in an object needs an object but got %s. Remedy: spread an object, or write `key: value`.", article(v))
				}
				for _, k := range src.keys {
					it.charge(x.pos, 32+len(k))
					o.set(k, src.m[k])
				}
				continue
			}
			key := p.key
			if p.keyX != nil {
				kv := it.eval(p.keyX, e)
				switch k := kv.(type) {
				case string:
					key = k
				case float64:
					key = formatNumber(k)
				default:
					it.fail(x.pos, "an object key must be a text or a number but got %s. Remedy: use `String(x)`.", article(kv))
				}
			}
			it.charge(x.pos, 32+len(key))
			o.set(key, it.eval(p.value, e))
		}
		return o
	case *funcLit:
		it.step(x.pos)
		it.charge(x.pos, 48)
		return &Closure{fn: x, env: e}
	case *unaryX:
		return it.evalUnary(x, e)
	case *updateX:
		it.step(x.pos)
		old := it.eval(x.target, e)
		f, ok := old.(float64)
		if !ok {
			it.fail(x.pos, "`%s` needs a number but `%s` is %s. Remedy: start it from a number, `let n = 0`.", x.op, exprName(x.target), article(old))
		}
		nv := f + 1
		if x.op == "--" {
			nv = f - 1
		}
		it.store(x.target, nv, e, x.pos)
		if x.prefix {
			return nv
		}
		return f
	case *binaryX:
		it.step(x.pos)
		l := it.eval(x.l, e)
		r := it.eval(x.r, e)
		return it.binary(x.op, l, r, x.pos, x.l, x.r)
	case *logicalX:
		return it.evalLogical(x, e)
	case *condX:
		it.step(x.pos)
		if it.cond(it.eval(x.c, e), x.pos, "`? :`") {
			return it.eval(x.a, e)
		}
		return it.eval(x.b, e)
	case *assignX:
		it.step(x.pos)
		var v any
		if x.op == "=" {
			v = it.eval(x.value, e)
		} else {
			old := it.eval(x.target, e)
			rhs := it.eval(x.value, e)
			v = it.binary(strings.TrimSuffix(x.op, "="), old, rhs, x.pos, x.target, x.value)
		}
		it.store(x.target, v, e, x.pos)
		return v
	case *memberX:
		it.step(x.pos)
		o := it.eval(x.obj, e)
		if o == nil && x.optional {
			return nil
		}
		return it.member(o, x.name, x.pos, x.obj)
	case *indexX:
		it.step(x.pos)
		o := it.eval(x.obj, e)
		if o == nil && x.optional {
			return nil
		}
		return it.index(o, it.eval(x.idx, e), x.pos, x.obj)
	case *callX:
		return it.evalCall(x, e)
	}
	panic(&Error{Kind: KindRuntime, Name: it.name, Msg: fmt.Sprintf("internal error: unknown expression %T", n)})
}

func (it *interp) evalUnary(x *unaryX, e *env) any {
	it.step(x.pos)
	v := it.eval(x.x, e)
	switch x.op {
	case "!":
		b, ok := v.(bool)
		if !ok {
			it.fail(x.pos, "`!` needs a yes/no but `%s` is %s. Remedy: compare it: `x === null`, `n === 0`, `s === ''`, `list.length === 0`.", exprName(x.x), article(v))
		}
		return !b
	case "-", "+":
		f, ok := v.(float64)
		if !ok {
			it.fail(x.pos, "unary `%s` needs a number but `%s` is %s. Remedy: convert a text with `Number(text)`.", x.op, exprName(x.x), article(v))
		}
		if x.op == "-" {
			if f == 0 {
				return 0.0
			}
			return -f
		}
		return f
	case "typeof":
		return typeName(v)
	}
	panic(&Error{Kind: KindRuntime, Name: it.name, Msg: "internal error: unary " + x.op})
}

func (it *interp) evalLogical(x *logicalX, e *env) any {
	it.step(x.pos)
	l := it.eval(x.l, e)
	if x.op == "??" {
		if l != nil {
			return l
		}
		return it.eval(x.r, e)
	}
	lb, ok := l.(bool)
	if !ok {
		it.fail(x.pos, "`%s` needs a yes/no on its left but `%s` is %s. Remedy: compare it: `x !== null`, `n > 0`, `s !== ''`. To use a default for a missing value write `x ?? default`.", x.op, exprName(x.l), article(l))
	}
	if (x.op == "&&" && !lb) || (x.op == "||" && lb) {
		return lb
	}
	r := it.eval(x.r, e)
	if _, ok := r.(bool); !ok {
		it.fail(x.pos, "`%s` needs a yes/no on its right but `%s` is %s. Remedy: compare it: `x !== null`, `n > 0`. To pick a value use `cond ? a : b`.", x.op, exprName(x.r), article(r))
	}
	return r
}

func (it *interp) num(at pos, op string, v any, side node) float64 {
	f, ok := v.(float64)
	if !ok {
		hint := "convert a text with `Number(text)`"
		if op == "+" {
			hint = "to join texts use a template `${a}${b}` or `String(x)`; to add, make both numbers"
		}
		it.fail(at, "`%s` needs numbers but `%s` is %s. Remedy: %s.", op, exprName(side), article(v), hint)
	}
	return f
}

func (it *interp) finiteResult(at pos, f float64, op string) float64 {
	if math.IsNaN(f) || math.IsInf(f, 0) {
		it.fail(at, "the result of `%s` is not a finite number (a division by zero, or a value too large). Remedy: check the divisor first, `if (d !== 0) {...}`.", op)
	}
	return f
}

func (it *interp) binary(op string, l, r any, at pos, ln, rn node) any {
	switch op {
	case "===":
		return strictEqual(l, r)
	case "!==":
		return !strictEqual(l, r)
	case "+":
		if ls, ok := l.(string); ok {
			rs, ok := r.(string)
			if !ok {
				it.fail(at, "`+` joins texts only with texts but `%s` is %s. Remedy: write a template text `%s${%s}` or `ls + String(x)`.", exprName(rn), article(r), "...", exprName(rn))
			}
			it.checkString(at, len(ls)+len(rs))
			it.charge(at, len(ls)+len(rs)+16)
			return ls + rs
		}
		if _, ok := r.(string); ok {
			it.fail(at, "`+` adds numbers only to numbers but `%s` is %s. Remedy: write a template text `${%s}...` or `String(x) + text`.", exprName(ln), article(l), exprName(ln))
		}
		return it.finiteResult(at, it.num(at, op, l, ln)+it.num(at, op, r, rn), op)
	case "-":
		return it.finiteResult(at, it.num(at, op, l, ln)-it.num(at, op, r, rn), op)
	case "*":
		return it.finiteResult(at, it.num(at, op, l, ln)*it.num(at, op, r, rn), op)
	case "/":
		a, b := it.num(at, op, l, ln), it.num(at, op, r, rn)
		if b == 0 {
			it.fail(at, "division by zero. Remedy: check the divisor first, `if (%s !== 0) {...}`.", exprName(rn))
		}
		return it.finiteResult(at, a/b, op)
	case "%":
		a, b := it.num(at, op, l, ln), it.num(at, op, r, rn)
		if b == 0 {
			it.fail(at, "remainder of a division by zero. Remedy: check the divisor first, `if (%s !== 0) {...}`.", exprName(rn))
		}
		return it.finiteResult(at, math.Mod(a, b), op)
	case "**":
		return it.finiteResult(at, math.Pow(it.num(at, op, l, ln), it.num(at, op, r, rn)), op)
	case "<", ">", "<=", ">=":
		if ls, ok := l.(string); ok {
			rs, ok := r.(string)
			if !ok {
				it.fail(at, "`%s` compares a text only with a text but `%s` is %s.", op, exprName(rn), article(r))
			}
			switch op {
			case "<":
				return ls < rs
			case ">":
				return ls > rs
			case "<=":
				return ls <= rs
			}
			return ls >= rs
		}
		a, b := it.num(at, op, l, ln), it.num(at, op, r, rn)
		switch op {
		case "<":
			return a < b
		case ">":
			return a > b
		case "<=":
			return a <= b
		}
		return a >= b
	}
	panic(&Error{Kind: KindRuntime, Name: it.name, Msg: "internal error: operator " + op})
}

// store writes v to a name, obj.key or list[i].
func (it *interp) store(target node, v any, e *env, at pos) {
	switch t := target.(type) {
	case *identX:
		b := e.lookup(t.name)
		if b == nil {
			it.fail(at, "`%s` is not defined.", t.name)
		}
		if !b.init {
			it.fail(at, "`%s` is assigned before the line that declares it has run.", t.name)
		}
		b.v = v
	case *memberX:
		o := it.eval(t.obj, e)
		obj, ok := o.(*Object)
		if !ok {
			it.fail(at, "cannot set `.%s` on `%s`, which is %s. Remedy: only objects have keys; make `%s` an object `{}` first.", t.name, exprName(t.obj), article(o), exprName(t.obj))
		}
		it.setKey(obj, t.name, v, at)
	case *indexX:
		o := it.eval(t.obj, e)
		idx := it.eval(t.idx, e)
		switch c := o.(type) {
		case *Array:
			i := it.arrayIndex(idx, at, len(c.elems)+1, t.obj)
			if i == len(c.elems) {
				it.charge(at, 8)
				c.elems = append(c.elems, v)
			} else {
				c.elems[i] = v
			}
		case *Object:
			it.setKey(c, it.keyText(idx, at), v, at)
		default:
			it.fail(at, "cannot set an element of `%s`, which is %s. Remedy: only lists and objects can be changed.", exprName(t.obj), article(o))
		}
	}
}

func (it *interp) setKey(o *Object, k string, v any, at pos) {
	if _, ok := o.m[k]; !ok {
		it.charge(at, 32+len(k))
	}
	o.set(k, v)
}

func (it *interp) keyText(idx any, at pos) string {
	switch k := idx.(type) {
	case string:
		return k
	case float64:
		return formatNumber(k)
	}
	it.fail(at, "an object key must be a text or a number but got %s. Remedy: use `String(x)`.", article(idx))
	return ""
}

func (it *interp) arrayIndex(idx any, at pos, limit int, obj node) int {
	f, ok := idx.(float64)
	if !ok || f != math.Trunc(f) {
		it.fail(at, "the index of `%s` must be a whole number but is %s. Remedy: use `Math.floor(x)`.", exprName(obj), article(idx))
	}
	if f < 0 || f >= float64(limit) {
		it.fail(at, "index %s is outside `%s` (it has %d element(s)). Remedy: check `i < list.length` first; to add at the end use `list.push(x)`.", formatNumber(f), exprName(obj), limit-1)
	}
	return int(f)
}

func (it *interp) member(o any, name string, at pos, from node) any {
	switch x := o.(type) {
	case nil:
		it.fail(at, "cannot read `.%s` of `%s` because it is null. Remedy: check `%s !== null` first, or write `%s?.%s` to get null instead of an error.", name, exprName(from), exprName(from), exprName(from), name)
	case *Object:
		v, _ := x.get(name)
		return v
	case *Array:
		if name == "length" {
			return float64(len(x.elems))
		}
		if m, ok := arrayMethods[name]; ok {
			return &Native{name: name, fn: func(it *interp, args []any) any { return m(it, x, args, at) }}
		}
		it.noMember(at, "lists", name, arrayMethodNames(), "length")
	case string:
		if name == "length" {
			return float64(len([]rune(x)))
		}
		if m, ok := stringMethods[name]; ok {
			return &Native{name: name, fn: func(it *interp, args []any) any { return m(it, x, args, at) }}
		}
		it.noMember(at, "texts", name, stringMethodNames(), "length")
	case float64:
		if name == "toFixed" {
			return &Native{name: name, fn: func(it *interp, args []any) any { return numberToFixed(it, x, args, at) }}
		}
		it.noMember(at, "numbers", name, []string{"toFixed"})
	case bool:
		it.fail(at, "a yes/no has no `.%s`.", name)
	case *Native:
		if x.props != nil {
			if v, ok := x.props.get(name); ok {
				return v
			}
		}
		it.fail(at, "`%s` has no `.%s`.", x.name, name)
	case *Closure:
		it.fail(at, "a function has no `.%s`. Remedy: call it with `()`.", name)
	}
	return nil
}

func (it *interp) noMember(at pos, what, name string, names []string, extra ...string) {
	names = append(append([]string(nil), names...), extra...)
	msg := fmt.Sprintf("%s have no `.%s`.", what, name)
	if near := nearest(name, names); len(near) > 0 {
		msg += " Did you mean `" + strings.Join(near, "`, `") + "`?"
	}
	it.fail(at, "%s Remedy: use one of the names the language gives %s.", msg, what)
}

func (it *interp) index(o, idx any, at pos, from node) any {
	switch x := o.(type) {
	case nil:
		it.fail(at, "cannot read an element of `%s` because it is null. Remedy: check `%s !== null` first, or write `%s?.[i]`.", exprName(from), exprName(from), exprName(from))
	case *Array:
		f, ok := idx.(float64)
		if !ok || f != math.Trunc(f) {
			it.fail(at, "the index of `%s` must be a whole number but is %s. Remedy: use `Math.floor(x)`.", exprName(from), article(idx))
		}
		if f < 0 || f >= float64(len(x.elems)) {
			return nil
		}
		return x.elems[int(f)]
	case string:
		f, ok := idx.(float64)
		if !ok || f != math.Trunc(f) {
			it.fail(at, "the index of a text must be a whole number but is %s.", article(idx))
		}
		rs := []rune(x)
		if f < 0 || f >= float64(len(rs)) {
			return nil
		}
		return string(rs[int(f)])
	case *Object:
		v, _ := x.get(it.keyText(idx, at))
		return v
	}
	it.fail(at, "cannot index `%s`, which is %s. Remedy: only lists, texts and objects have elements.", exprName(from), article(o))
	return nil
}

func (it *interp) evalCall(x *callX, e *env) any {
	it.step(x.pos)
	defer func(prev pos) { it.last = prev }(it.last)
	it.last = x.pos
	fn := it.eval(x.fn, e)
	if fn == nil && x.optional {
		return nil
	}
	var args []any
	for _, a := range x.args {
		v := it.eval(a.x, e)
		if a.spread {
			arr, ok := v.(*Array)
			if !ok {
				it.fail(x.pos, "`...` in a call needs a list but got %s.", article(v))
			}
			args = append(args, arr.elems...)
		} else {
			args = append(args, v)
		}
	}
	it.charge(x.pos, 8*len(args))
	switch f := fn.(type) {
	case *Closure, *Native:
		return it.call(f, args, x.pos)
	}
	it.fail(x.pos, "`%s` is not a function (it is %s). Remedy: only functions can be followed by `(...)`.", exprName(x.fn), article(fn))
	return nil
}

// call runs a function value with arguments, under the depth limit.
func (it *interp) call(fn any, args []any, at pos) any {
	switch f := fn.(type) {
	case *Native:
		it.step(at)
		return f.fn(it, args)
	case *Closure:
		it.depth++
		if it.depth > it.lim.Depth {
			it.limit(at, "calls are nested more than %d deep and the script was stopped. Remedy: use a loop instead of a function that calls itself.", it.lim.Depth)
		}
		defer func() { it.depth-- }()
		it.charge(at, 64)
		fe := newEnv(f.env)
		for i, p := range f.fn.params {
			if p.rest {
				rest := &Array{}
				if i < len(args) {
					rest.elems = append(rest.elems, args[i:]...)
				}
				it.bind(p.pat, rest, fe, false)
				break
			}
			var v any
			if i < len(args) {
				v = args[i]
			}
			if v == nil && p.def != nil {
				v = it.eval(p.def, fe)
			}
			it.bind(p.pat, v, fe, false)
		}
		if f.fn.expr != nil {
			return it.eval(f.fn.expr, fe)
		}
		it.hoist(f.fn.body, fe)
		it.ret = nil
		if c := it.execBlock(f.fn.body, fe); c == ctlReturn {
			r := it.ret
			it.ret = nil
			return r
		}
		return nil
	}
	it.fail(at, "this is not a function (it is %s).", article(fn))
	return nil
}
