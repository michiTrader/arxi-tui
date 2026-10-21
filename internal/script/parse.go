package script

import (
	"fmt"
	"sort"
	"strings"
)

type declKind int

const (
	dLet declKind = iota + 1
	dConst
	dFunc
	dParam
	dHost
)

type ref struct {
	name   string
	at     pos
	assign bool
}

type scope struct {
	decl   map[string]declKind
	refs   []ref
	parent *scope
}

const maxNesting = 120

type parser struct {
	name  string
	toks  []token
	i     int
	sc    *scope
	depth int
	loops int      // loops around the statement being read; function bodies start at 0
	hosts []string // names the host declared, for the message of an undefined name
}

func (p *parser) tok() token { return p.toks[p.i] }
func (p *parser) peekTok(k int) token {
	if p.i+k < len(p.toks) {
		return p.toks[p.i+k]
	}
	return p.toks[len(p.toks)-1]
}
func (p *parser) adv() token {
	t := p.toks[p.i]
	if p.i < len(p.toks)-1 {
		p.i++
	}
	return t
}
func (p *parser) at(t token) pos { return pos{t.line, t.col} }

func (p *parser) fail(t token, format string, a ...any) {
	panic(synErr(p.name, t.line, t.col, format, a...))
}

func (p *parser) isP(s string) bool { t := p.tok(); return t.kind == tPunct && t.text == s }
func (p *parser) isK(s string) bool { t := p.tok(); return t.kind == tKw && t.text == s }
func (p *parser) isWord(s string) bool {
	t := p.tok()
	return (t.kind == tIdent || t.kind == tKw) && t.text == s
}

func (p *parser) eatP(s string) bool {
	if p.isP(s) {
		p.adv()
		return true
	}
	return false
}

func (p *parser) expectP(s, what string) token {
	if !p.isP(s) {
		t := p.tok()
		p.fail(t, "expected `%s` %s but found %s. Remedy: add the `%s`.", s, what, t, s)
	}
	return p.adv()
}

// checkRefused fails if the token is a word that exists in JavaScript and not here.
func (p *parser) checkRefused() {
	t := p.tok()
	if t.kind == tIdent || t.kind == tKw {
		if msg, ok := refusal(t.text); ok {
			p.fail(t, "%s", msg)
		}
	}
}

func refusal(word string) (string, bool) {
	s, ok := refused[word]
	return s, ok
}

// ---- scopes: names are checked where they are written, once the scope that declares them
// is complete (a function may use a name that is declared further down).

func (p *parser) push() { p.sc = &scope{decl: map[string]declKind{}, parent: p.sc} }

// pop closes the scope, resolves what it can and passes the rest up. It reports whether the
// scope declared anything (a block that declares nothing needs no environment at run time).
func (p *parser) pop() bool {
	sc := p.sc
	p.sc = sc.parent
	for _, r := range sc.refs {
		if k, ok := sc.decl[r.name]; ok {
			if r.assign && (k == dConst || k == dHost || k == dFunc) {
				what := "a const"
				switch k {
				case dHost:
					what = "given by the host and read-only"
				case dFunc:
					what = "a function"
				}
				panic(synErr(p.name, r.at.line, r.at.col, "`%s` is %s and cannot be assigned. Remedy: declare it with `let` if it must change, or keep the changing value in another variable.", r.name, what))
			}
			continue
		}
		if p.sc == nil {
			p.undefined(r)
		}
		p.sc.refs = append(p.sc.refs, r)
	}
	return len(sc.decl) > 0
}

func (p *parser) undefined(r ref) {
	if msg, ok := refusedGlobals[r.name]; ok {
		panic(synErr(p.name, r.at.line, r.at.col, "%s", msg))
	}
	var known []string
	for _, n := range p.hosts {
		known = append(known, n)
	}
	for n := range builtinNames {
		known = append(known, n)
	}
	sort.Strings(known)
	msg := fmt.Sprintf("`%s` is not defined.", r.name)
	if near := nearest(r.name, known); len(near) > 0 {
		msg += " Did you mean `" + strings.Join(near, "`, `") + "`?"
	}
	msg += " Remedy: declare it with `let " + r.name + " = ...` before using it"
	if len(p.hosts) > 0 {
		h := append([]string(nil), p.hosts...)
		sort.Strings(h)
		if len(h) > 12 {
			h = append(h[:12], "...")
		}
		msg += ", or use a name the host gives: " + strings.Join(h, ", ")
	}
	msg += "."
	panic(synErr(p.name, r.at.line, r.at.col, "%s", msg))
}

func (p *parser) declare(name string, k declKind, t pos) {
	if _, dup := p.sc.decl[name]; dup {
		panic(synErr(p.name, t.line, t.col, "`%s` is already declared in this scope. Remedy: use another name, or assign to it with `%s = ...` without `let`.", name, name))
	}
	p.sc.decl[name] = k
}

func (p *parser) use(name string, at pos, assign bool) {
	p.sc.refs = append(p.sc.refs, ref{name, at, assign})
}

// ---- entry

func parseProgram(name, src string, hosts []string) (body []stmt, err error) {
	toks, lerr := lex(name, src, 1, 1)
	if lerr != nil {
		return nil, lerr
	}
	p := &parser{name: name, toks: toks, hosts: hosts}
	defer func() {
		if r := recover(); r != nil {
			if e, ok := r.(*Error); ok {
				err = e
				return
			}
			panic(r)
		}
	}()
	// The outermost scope holds what the host and the language give; the program's own
	// declarations are in the scope above it.
	p.sc = &scope{decl: map[string]declKind{}}
	for _, h := range hosts {
		p.sc.decl[h] = dHost
	}
	for n, k := range builtinNames {
		p.sc.decl[n] = k
	}
	p.push()
	for p.tok().kind != tEOF {
		body = append(body, p.parseStmt())
	}
	p.pop()
	p.pop()
	return body, nil
}

// ---- statements

func (p *parser) enter() {
	p.depth++
	if p.depth > maxNesting {
		p.fail(p.tok(), "this is nested more than %d levels deep. Remedy: move the inner part into a function.", maxNesting)
	}
}
func (p *parser) leave() { p.depth-- }

func (p *parser) endStmt() {
	if p.eatP(";") {
		return
	}
	t := p.tok()
	if t.nl || t.kind == tEOF || (t.kind == tPunct && t.text == "}") {
		return
	}
	p.checkRefused()
	p.fail(t, "expected `;` or a new line after this statement but found %s. Remedy: end the statement with `;` or start %s on a new line.", t, t)
}

func (p *parser) parseStmt() stmt {
	p.enter()
	defer p.leave()
	t := p.tok()
	at := p.at(t)
	if t.kind == tPunct {
		switch t.text {
		case "{":
			return p.parseBlock()
		case ";":
			p.adv()
			return &blockS{pos: at}
		}
	}
	if t.kind == tKw || t.kind == tIdent {
		switch t.text {
		case "let", "const":
			if t.kind == tKw {
				d := p.parseDecl()
				p.endStmt()
				return d
			}
		case "function":
			return p.parseFuncDecl()
		case "return":
			p.adv()
			var x node
			nt := p.tok()
			if !nt.nl && nt.kind != tEOF && !(nt.kind == tPunct && (nt.text == ";" || nt.text == "}")) {
				x = p.parseExpr()
			}
			p.endStmt()
			return &returnS{at, x}
		case "if":
			return p.parseIf()
		case "for":
			return p.parseFor()
		case "while":
			p.adv()
			p.expectP("(", "after `while`")
			c := p.parseExpr()
			p.expectP(")", "after the condition")
			p.loops++
			body := p.parseStmt()
			p.loops--
			return &whileS{at, c, body}
		case "break":
			if p.loops == 0 {
				p.fail(t, "`break` is outside a loop. Remedy: use it inside `for` or `while`; to leave a function write `return`.")
			}
			p.adv()
			p.endStmt()
			return &breakS{at}
		case "continue":
			if p.loops == 0 {
				p.fail(t, "`continue` is outside a loop. Remedy: use it inside `for` or `while`; to leave a function write `return`.")
			}
			p.adv()
			p.endStmt()
			return &continueS{at}
		}
		p.checkRefused()
	}
	x := p.parseExpr()
	p.endStmt()
	return &exprS{at, x}
}

func (p *parser) parseBlock() *blockS {
	t := p.expectP("{", "to open a block")
	p.push()
	b := &blockS{pos: p.at(t)}
	for !p.isP("}") {
		if p.tok().kind == tEOF {
			p.fail(t, "this `{` is never closed. Remedy: add the matching `}`.")
		}
		b.body = append(b.body, p.parseStmt())
	}
	p.adv()
	b.scoped = p.pop()
	return b
}

func (p *parser) parseDecl() *varDecl {
	t := p.adv()
	d := &varDecl{pos: p.at(t), konst: t.text == "const"}
	for {
		nt := p.tok()
		pat := p.parsePattern(dLet, d.konst)
		var init node
		if p.eatP("=") {
			init = p.parseAssign()
		} else if d.konst {
			p.fail(nt, "a `const` needs a value. Remedy: write `const name = value`, or use `let` if the value comes later.")
		} else if _, isId := pat.(*identP); !isId {
			p.fail(nt, "a pattern needs a value to take apart. Remedy: write `= value` after it.")
		}
		d.decls = append(d.decls, declr{pat, init})
		if !p.eatP(",") {
			return d
		}
	}
}

// parsePattern reads a name, [a, b] or {a, b: c} and declares what it binds.
func (p *parser) parsePattern(k declKind, konst bool) pattern {
	if konst && k == dLet {
		k = dConst
	}
	t := p.tok()
	at := p.at(t)
	switch {
	case t.kind == tIdent:
		p.checkRefused()
		p.adv()
		p.declare(t.text, k, at)
		return &identP{at, t.text}
	case t.kind == tPunct && t.text == "[":
		p.adv()
		ap := &arrayP{pos: at}
		for !p.isP("]") {
			if p.isP(",") {
				p.adv()
				ap.elems = append(ap.elems, patElem{skip: true})
				continue
			}
			var el patElem
			if p.eatP("...") {
				el.rest = true
			}
			el.pat = p.parsePattern(k, false)
			if !el.rest && p.eatP("=") {
				el.def = p.parseAssign()
			}
			ap.elems = append(ap.elems, el)
			if !p.eatP(",") {
				break
			}
		}
		p.expectP("]", "to close the pattern")
		return ap
	case t.kind == tPunct && t.text == "{":
		p.adv()
		op := &objectP{pos: at}
		for !p.isP("}") {
			kt := p.tok()
			if kt.kind != tIdent && kt.kind != tKw && kt.kind != tStr {
				p.fail(kt, "expected a name inside `{ }` but found %s. Remedy: write `{ name }` or `{ key: name }`.", kt)
			}
			p.adv()
			pr := objProp{key: kt.text}
			if p.eatP(":") {
				pr.pat = p.parsePattern(k, false)
			} else {
				if kt.kind != tIdent {
					p.fail(kt, "`%s` cannot be a name. Remedy: write `{ %s: other }`.", kt.text, kt.text)
				}
				p.declare(kt.text, k, p.at(kt))
				pr.pat = &identP{p.at(kt), kt.text}
			}
			if p.eatP("=") {
				pr.def = p.parseAssign()
			}
			op.props = append(op.props, pr)
			if !p.eatP(",") {
				break
			}
		}
		p.expectP("}", "to close the pattern")
		return op
	}
	p.checkRefused()
	p.fail(t, "expected a name but found %s. Remedy: write a name such as `count`, or a pattern such as `[a, b]` or `{ a, b }`.", t)
	return nil
}

func (p *parser) parseFuncDecl() stmt {
	t := p.adv()
	if p.isP("*") {
		p.fail(p.tok(), "generators are not available. Remedy: keep the state in a variable and return one step per call.")
	}
	nt := p.tok()
	if nt.kind != tIdent {
		p.fail(nt, "a function declaration needs a name. Remedy: write `function name(a, b) { ... }`, or `(a, b) => ...` for one without a name.")
	}
	p.checkRefused()
	p.adv()
	p.declare(nt.text, dFunc, p.at(nt))
	fn := p.parseFuncRest(p.at(t), nt.text)
	return &funcDecl{p.at(t), nt.text, fn}
}

// parseFuncRest reads (params) { body } with `function name` already read.
func (p *parser) parseFuncRest(at pos, name string) *funcLit {
	p.expectP("(", "to open the parameters")
	p.push()
	fn := &funcLit{pos: at, name: name}
	fn.params = p.parseParams(")")
	p.expectP(")", "to close the parameters")
	bt := p.expectP("{", "to open the body of the function")
	saveLoops := p.loops
	p.loops = 0
	defer func() { p.loops = saveLoops }()
	for !p.isP("}") {
		if p.tok().kind == tEOF {
			p.fail(bt, "this `{` is never closed. Remedy: add the matching `}`.")
		}
		fn.body = append(fn.body, p.parseStmt())
	}
	p.adv()
	p.pop()
	return fn
}

func (p *parser) parseParams(close string) []param {
	var ps []param
	for !p.isP(close) {
		var pr param
		if p.eatP("...") {
			pr.rest = true
		}
		pr.pat = p.parsePattern(dParam, false)
		if !pr.rest && p.eatP("=") {
			pr.def = p.parseAssign()
		}
		ps = append(ps, pr)
		if pr.rest && !p.isP(close) {
			p.fail(p.tok(), "the `...rest` parameter must be the last one.")
		}
		if !p.eatP(",") {
			break
		}
	}
	return ps
}

func (p *parser) parseIf() stmt {
	t := p.adv()
	p.expectP("(", "after `if`")
	c := p.parseExpr()
	p.expectP(")", "after the condition")
	s := &ifS{pos: p.at(t), c: c}
	s.a = p.parseStmt()
	if p.isK("else") {
		p.adv()
		s.b = p.parseStmt()
	}
	return s
}

func (p *parser) parseFor() stmt {
	t := p.adv()
	at := p.at(t)
	p.expectP("(", "after `for`")
	p.push()
	defer p.pop()
	var init stmt
	switch {
	case p.isP(";"):
	case p.isK("let") || p.isK("const"):
		kt := p.tok()
		// for (const x of xs)
		if nt := p.peekTok(1); nt.kind == tIdent || (nt.kind == tPunct && (nt.text == "[" || nt.text == "{")) {
			save := p.i
			p.adv()
			konst := kt.text == "const"
			pat := p.parsePatternForOf(konst)
			if p.isWord("of") {
				p.adv()
				iter := p.parseAssign()
				p.expectP(")", "after the list")
				p.loops++
				body := p.parseStmt()
				p.loops--
				return &forOfS{at, konst, pat, iter, body}
			}
			if p.isK("in") {
				p.fail(p.tok(), "%s", refused["in"])
			}
			p.i = save
			// not a for-of: undo the declarations made while reading the pattern
			p.sc.decl = map[string]declKind{}
		}
		init = p.parseDecl()
	default:
		init = &exprS{at, p.parseExpr()}
	}
	p.expectP(";", "after the start of the loop")
	var cond node
	if !p.isP(";") {
		cond = p.parseExpr()
	}
	p.expectP(";", "after the condition of the loop")
	var post node
	if !p.isP(")") {
		post = p.parseExpr()
	}
	p.expectP(")", "to close the loop header")
	p.loops++
	body := p.parseStmt()
	p.loops--
	_, isDecl := init.(*varDecl)
	return &forS{pos: at, init: init, cond: cond, post: post, body: body, scoped: isDecl}
}

func (p *parser) parsePatternForOf(konst bool) pattern {
	k := dLet
	if konst {
		k = dConst
	}
	return p.parsePattern(k, false)
}

// ---- expressions

func (p *parser) parseExpr() node {
	p.enter()
	defer p.leave()
	return p.parseAssign()
}

var assignOps = map[string]bool{"=": true, "+=": true, "-=": true, "*=": true, "/=": true, "%=": true, "**=": true}

func (p *parser) parseAssign() node {
	if p.isArrowStart() {
		return p.parseArrow()
	}
	l := p.parseCond()
	t := p.tok()
	if t.kind == tPunct && assignOps[t.text] {
		switch tg := l.(type) {
		case *identX:
			// The reference parsed as a read is moved to a write.
			p.markAssign(tg)
		case *memberX, *indexX:
		default:
			p.fail(t, "the left side of `%s` is not something that can be assigned to. Remedy: assign to a name, `obj.key` or `list[i]`.", t.text)
		}
		p.adv()
		v := p.parseAssign()
		return &assignX{p.at(t), t.text, l, v}
	}
	return l
}

func (p *parser) markAssign(id *identX) {
	for i := len(p.sc.refs) - 1; i >= 0; i-- {
		r := &p.sc.refs[i]
		if r.name == id.name && r.at == id.pos {
			r.assign = true
			return
		}
	}
}

func (p *parser) isArrowStart() bool {
	t := p.tok()
	if t.kind == tIdent {
		n := p.peekTok(1)
		return n.kind == tPunct && n.text == "=>"
	}
	if t.kind == tPunct && t.text == "(" {
		depth := 0
		for k := p.i; k < len(p.toks); k++ {
			tk := p.toks[k]
			if tk.kind == tPunct {
				switch tk.text {
				case "(", "[", "{":
					depth++
				case ")", "]", "}":
					depth--
					if depth == 0 {
						n := p.toks[min(k+1, len(p.toks)-1)]
						return n.kind == tPunct && n.text == "=>"
					}
				}
			}
			if tk.kind == tEOF {
				return false
			}
		}
	}
	return false
}

func (p *parser) parseArrow() node {
	t := p.tok()
	fn := &funcLit{pos: p.at(t)}
	p.push()
	if t.kind == tIdent {
		p.checkRefused()
		p.adv()
		p.declare(t.text, dParam, p.at(t))
		fn.params = []param{{pat: &identP{p.at(t), t.text}}}
	} else {
		p.adv()
		fn.params = p.parseParams(")")
		p.expectP(")", "to close the parameters")
	}
	p.expectP("=>", "after the parameters")
	saveLoops := p.loops
	p.loops = 0
	defer func() { p.loops = saveLoops }()
	if p.isP("{") {
		bt := p.adv()
		for !p.isP("}") {
			if p.tok().kind == tEOF {
				p.fail(bt, "this `{` is never closed. Remedy: add the matching `}`.")
			}
			fn.body = append(fn.body, p.parseStmt())
		}
		p.adv()
	} else {
		fn.expr = p.parseAssign()
	}
	p.pop()
	return fn
}

func (p *parser) parseCond() node {
	c := p.parseBinary(0)
	if p.isP("?") {
		t := p.adv()
		a := p.parseAssign()
		p.expectP(":", "in the `? :` condition")
		b := p.parseAssign()
		return &condX{p.at(t), c, a, b}
	}
	return c
}

var binPrec = map[string]int{
	"??": 1, "||": 2, "&&": 3,
	"===": 4, "!==": 4,
	"<": 5, ">": 5, "<=": 5, ">=": 5,
	"+": 6, "-": 6,
	"*": 7, "/": 7, "%": 7,
	"**": 8,
}

var bitwise = map[string]bool{"&": true, "|": true, "^": true, "<<": true, ">>": true, "~": true}

func (p *parser) parseBinary(min int) node {
	l := p.parseUnary()
	for {
		t := p.tok()
		if t.kind == tKw && (t.text == "in") || t.kind == tIdent && t.text == "instanceof" {
			p.fail(t, "%s", refused[t.text])
		}
		if t.kind != tPunct {
			return l
		}
		switch {
		case t.text == "==" || t.text == "!=":
			p.fail(t, "`%s` is not available: it converts values silently. Remedy: use `%s=`, which is true only when the type and the value are the same.", t.text, t.text)
		case bitwise[t.text]:
			p.fail(t, "the bitwise operator `%s` is not available. Remedy: use arithmetic: `Math.floor(n / 2)`, `n %% 2`, or a yes/no variable.", t.text)
		}
		prec, ok := binPrec[t.text]
		if !ok || prec < min {
			return l
		}
		p.adv()
		var r node
		if t.text == "**" {
			r = p.parseBinary(prec) // right associative
		} else {
			r = p.parseBinary(prec + 1)
		}
		switch t.text {
		case "&&", "||", "??":
			l = &logicalX{p.at(t), t.text, l, r}
		default:
			if t.text == "===" || t.text == "!==" {
				p.checkTypeof(t, l, r)
			}
			l = &binaryX{p.at(t), t.text, l, r}
		}
	}
}

var typeofNames = []string{"number", "string", "boolean", "object", "array", "function", "null"}

// checkTypeof refuses `typeof x === 'undefined'`: a misspelt type name is a comparison that is
// silently always false.
func (p *parser) checkTypeof(op token, l, r node) {
	check := func(a, b node) {
		u, ok := a.(*unaryX)
		if !ok || u.op != "typeof" {
			return
		}
		c, ok := b.(*constLit)
		if !ok {
			return
		}
		s, ok := c.v.(string)
		if !ok {
			return
		}
		for _, n := range typeofNames {
			if s == n {
				return
			}
		}
		p.fail(op, "`typeof` never gives '%s', so this is always the same answer. Remedy: compare with one of %s; a missing value is `null`, test it with `x === null`.", s, "'"+strings.Join(typeofNames, "', '")+"'")
	}
	check(l, r)
	check(r, l)
}

func (p *parser) parseUnary() node {
	p.enter()
	defer p.leave()
	t := p.tok()
	at := p.at(t)
	if t.kind == tPunct {
		switch t.text {
		case "!", "-", "+":
			p.adv()
			return &unaryX{at, t.text, p.parseUnary()}
		case "~":
			p.fail(t, "the bitwise operator `~` is not available. Remedy: use arithmetic.")
		case "++", "--":
			p.adv()
			x := p.parseUnary()
			p.requireTarget(t, x)
			return &updateX{at, t.text, true, x}
		}
	}
	if t.kind == tKw && t.text == "typeof" {
		p.adv()
		return &unaryX{at, "typeof", p.parseUnary()}
	}
	return p.parsePostfix()
}

func (p *parser) requireTarget(t token, x node) {
	switch tg := x.(type) {
	case *identX:
		p.markAssign(tg)
	case *memberX, *indexX:
	default:
		p.fail(t, "`%s` needs a name, `obj.key` or `list[i]` to change. Remedy: write `i++` on a variable.", t.text)
	}
}

func (p *parser) parsePostfix() node {
	x := p.parsePrimary()
	for {
		t := p.tok()
		if t.kind != tPunct {
			return x
		}
		at := p.at(t)
		switch t.text {
		case ".":
			p.adv()
			nt := p.tok()
			if nt.kind != tIdent && nt.kind != tKw {
				p.fail(nt, "expected a property name after `.` but found %s. Remedy: write `obj.name`.", nt)
			}
			p.adv()
			x = &memberX{at, x, nt.text, false}
		case "?.":
			p.adv()
			switch {
			case p.isP("("):
				p.adv()
				x = &callX{at, x, p.parseArgs(), true}
			case p.isP("["):
				p.adv()
				idx := p.parseExpr()
				p.expectP("]", "to close the index")
				x = &indexX{at, x, idx, true}
			default:
				nt := p.tok()
				if nt.kind != tIdent && nt.kind != tKw {
					p.fail(nt, "expected a property name after `?.` but found %s.", nt)
				}
				p.adv()
				x = &memberX{at, x, nt.text, true}
			}
		case "[":
			if t.nl {
				return x
			}
			p.adv()
			idx := p.parseExpr()
			p.expectP("]", "to close the index")
			x = &indexX{at, x, idx, false}
		case "(":
			if t.nl {
				return x
			}
			p.adv()
			x = &callX{at, x, p.parseArgs(), false}
		case "++", "--":
			if t.nl {
				return x
			}
			p.adv()
			p.requireTarget(t, x)
			x = &updateX{at, t.text, false, x}
		default:
			return x
		}
	}
}

func (p *parser) parseArgs() []elem {
	var args []elem
	for !p.isP(")") {
		var e elem
		if p.eatP("...") {
			e.spread = true
		}
		e.x = p.parseAssign()
		args = append(args, e)
		if !p.eatP(",") {
			break
		}
	}
	p.expectP(")", "to close the call")
	return args
}

func (p *parser) parsePrimary() node {
	t := p.tok()
	at := p.at(t)
	switch t.kind {
	case tNum:
		p.adv()
		return &constLit{at, t.num}
	case tStr:
		p.adv()
		return &constLit{at, t.text}
	case tTpl:
		p.adv()
		return p.template(t)
	case tKw:
		switch t.text {
		case "true":
			p.adv()
			return &constLit{at, true}
		case "false":
			p.adv()
			return &constLit{at, false}
		case "null":
			p.adv()
			return &constLit{at, nil}
		case "function":
			p.adv()
			if p.isP("*") {
				p.fail(p.tok(), "generators are not available. Remedy: keep the state in a variable and return one step per call.")
			}
			name := ""
			if p.tok().kind == tIdent {
				name = p.adv().text
			}
			return p.parseFuncRest(at, name)
		}
		p.checkRefused()
		p.fail(t, "%s cannot start an expression here. Remedy: write a value, a name or a call.", t)
	case tIdent:
		p.checkRefused()
		p.adv()
		if t.text == "undefined" {
			return &constLit{at, nil}
		}
		p.use(t.text, at, false)
		return &identX{at, t.text}
	case tPunct:
		switch t.text {
		case "(":
			p.adv()
			x := p.parseExpr()
			p.expectP(")", "to close the parenthesis")
			return x
		case "[":
			p.adv()
			a := &arrayLit{pos: at}
			for !p.isP("]") {
				var e elem
				if p.eatP("...") {
					e.spread = true
				}
				e.x = p.parseAssign()
				a.elems = append(a.elems, e)
				if !p.eatP(",") {
					break
				}
			}
			p.expectP("]", "to close the list")
			return a
		case "{":
			return p.parseObject()
		}
	}
	p.fail(t, "expected a value but found %s. Remedy: write a number, a text, a name, a list `[...]` or an object `{...}` here.", t)
	return nil
}

func (p *parser) parseObject() node {
	t := p.adv()
	o := &objectLit{pos: p.at(t)}
	for !p.isP("}") {
		kt := p.tok()
		if p.eatP("...") {
			o.props = append(o.props, propX{value: p.parseAssign(), spread: true})
		} else {
			var pr propX
			switch {
			case kt.kind == tIdent || kt.kind == tKw || kt.kind == tStr:
				p.adv()
				pr.key = kt.text
			case kt.kind == tNum:
				p.adv()
				pr.key = formatNumber(kt.num)
			case kt.kind == tPunct && kt.text == "[":
				p.adv()
				pr.keyX = p.parseAssign()
				p.expectP("]", "to close the computed key")
			default:
				p.fail(kt, "expected a key but found %s. Remedy: write `name: value`, `'text key': value` or `[expr]: value`.", kt)
			}
			switch {
			case p.isP(":"):
				p.adv()
				pr.value = p.parseAssign()
			case p.isP("("):
				if pr.keyX != nil {
					p.fail(p.tok(), "a method cannot have a computed key. Remedy: write `[expr]: (args) => ...`.")
				}
				pr.value = p.parseFuncRest(p.at(kt), pr.key)
			case kt.kind == tIdent && pr.keyX == nil:
				if msg, ok := refusal(kt.text); ok {
					p.fail(kt, "%s", msg)
				}
				p.use(kt.text, p.at(kt), false)
				pr.value = &identX{p.at(kt), kt.text}
			default:
				p.fail(p.tok(), "expected `:` and a value after the key but found %s. Remedy: write `key: value`.", p.tok())
			}
			o.props = append(o.props, pr)
		}
		if !p.eatP(",") {
			break
		}
	}
	p.expectP("}", "to close the object")
	return o
}

// template turns the pieces the lexer cut into expressions parsed in the scope of the text.
func (p *parser) template(t token) node {
	tl := &tplLit{pos: p.at(t)}
	for _, part := range t.parts {
		if !part.isExpr {
			tl.parts = append(tl.parts, tplPartX{lit: part.lit})
			continue
		}
		toks, err := lex(p.name, part.src, part.line, part.col)
		if err != nil {
			panic(err)
		}
		saveToks, saveI := p.toks, p.i
		p.toks, p.i = toks, 0
		x := p.parseExpr()
		if p.tok().kind != tEOF {
			p.fail(p.tok(), "expected the end of `${...}` but found %s. Remedy: put one expression inside `${ }`.", p.tok())
		}
		p.toks, p.i = saveToks, saveI
		tl.parts = append(tl.parts, tplPartX{x: x})
	}
	return tl
}
