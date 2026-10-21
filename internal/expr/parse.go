package expr

import (
	"fmt"
	"strconv"
	"strings"
	"unicode"
)

type tokKind int

const (
	tEOF tokKind = iota
	tNum
	tStr
	tName // dotted lowercase words, or the words true and false
	tOp   // && || ! == != < <= > >= + - * / % ( )
)

type token struct {
	kind tokKind
	text string
	pos  int // rune offset, for messages
}

func lex(src string) ([]token, error) {
	rs := []rune(src)
	var out []token
	for i := 0; i < len(rs); {
		c := rs[i]
		switch {
		case unicode.IsSpace(c):
			i++
		case c >= '0' && c <= '9':
			j := i
			for j < len(rs) && rs[j] >= '0' && rs[j] <= '9' {
				j++
			}
			if j < len(rs) && (rs[j] == '.' || unicode.IsLetter(rs[j])) {
				return nil, fmt.Errorf("at %d: %q is not a whole number; only whole numbers exist here", i+1, string(rs[i:j+1]))
			}
			out = append(out, token{tNum, string(rs[i:j]), i})
			i = j
		case c == '\'' || c == '"':
			j := i + 1
			var b strings.Builder
			for ; j < len(rs) && rs[j] != c; j++ {
				if rs[j] == '\\' && j+1 < len(rs) && (rs[j+1] == c || rs[j+1] == '\\') {
					j++
				}
				b.WriteRune(rs[j])
			}
			if j >= len(rs) {
				return nil, fmt.Errorf("at %d: the text starting here never closes; end it with %c", i+1, c)
			}
			out = append(out, token{tStr, b.String(), i})
			i = j + 1
		case c == '_' || (c >= 'a' && c <= 'z'):
			j := i
			for j < len(rs) && (rs[j] == '_' || rs[j] == '.' || (rs[j] >= 'a' && rs[j] <= 'z') || (rs[j] >= '0' && rs[j] <= '9')) {
				j++
			}
			if rs[j-1] == '.' || strings.Contains(string(rs[i:j]), "..") {
				return nil, fmt.Errorf("at %d: %q has a stray '.'; a name is words joined by single dots, like input.at_end", i+1, string(rs[i:j]))
			}
			out = append(out, token{tName, string(rs[i:j]), i})
			i = j
		case c >= 'A' && c <= 'Z':
			return nil, fmt.Errorf("at %d: names are lowercase, and %q has a capital; write it in lowercase", i+1, string(c))
		default:
			two := ""
			if i+1 < len(rs) {
				two = string(rs[i : i+2])
			}
			switch two {
			case "&&", "||", "==", "!=", "<=", ">=":
				out = append(out, token{tOp, two, i})
				i += 2
				continue
			}
			switch c {
			case '!', '<', '>', '+', '-', '*', '/', '%', '(', ')':
				out = append(out, token{tOp, string(c), i})
				i++
			case '=':
				return nil, fmt.Errorf("at %d: a single = would be an assignment, and nothing here assigns; to compare write ==", i+1)
			case '&', '|':
				return nil, fmt.Errorf("at %d: a single %c is not an operator here; write %c%c for \"and\"/\"or\"", i+1, c, c, c)
			default:
				return nil, fmt.Errorf("at %d: %q is not part of the expression language (names, numbers, 'text', comparisons, && || !, + - * / %%)", i+1, string(c))
			}
		}
	}
	return append(out, token{tEOF, "", len(rs)}), nil
}

// node is a part of an expression.
type node interface{}

type (
	litNode  struct{ v Value }
	nameNode struct {
		name string
		pos  int
	}
	unNode struct {
		op  string
		x   node
		pos int
	}
	binNode struct {
		op   string
		l, r node
		pos  int
	}
)

type parser struct {
	toks  []token
	i     int
	src   string
	nodes int
	depth int
}

func (p *parser) peek() token { return p.toks[p.i] }
func (p *parser) next() token {
	t := p.toks[p.i]
	if t.kind != tEOF {
		p.i++
	}
	return t
}

func (p *parser) errAt(t token, f string, a ...any) error {
	return fmt.Errorf("at %d: %s", t.pos+1, fmt.Sprintf(f, a...))
}

// Binding powers, lowest first.
var binPower = map[string]int{
	"||": 1, "&&": 2,
	"==": 3, "!=": 3, "<": 4, "<=": 4, ">": 4, ">=": 4,
	"+": 5, "-": 5, "*": 6, "/": 6, "%": 6,
}

func (p *parser) parseExpr(min int) (node, error) {
	p.depth++
	defer func() { p.depth-- }()
	if p.depth > MaxDepth {
		return nil, fmt.Errorf("the expression is nested more than %d deep; flatten it or split it", MaxDepth)
	}
	left, err := p.parseUnary()
	if err != nil {
		return nil, err
	}
	for {
		t := p.peek()
		pw, ok := binPower[t.text]
		if t.kind != tOp || !ok || pw <= min {
			return left, nil
		}
		p.next()
		right, err := p.parseExpr(pw)
		if err != nil {
			return nil, err
		}
		p.nodes++
		left = binNode{t.text, left, right, t.pos}
	}
}

func (p *parser) parseUnary() (node, error) {
	t := p.next()
	p.nodes++
	switch t.kind {
	case tNum:
		n, err := strconv.ParseInt(t.text, 10, 64)
		if err != nil || n > MaxInt {
			return nil, p.errAt(t, "%s is larger than the %d allowed", t.text, int64(MaxInt))
		}
		return litNode{N(n)}, nil
	case tStr:
		return litNode{S(t.text)}, nil
	case tName:
		switch t.text {
		case "true":
			return litNode{B(true)}, nil
		case "false":
			return litNode{B(false)}, nil
		}
		return nameNode{t.text, t.pos}, nil
	case tOp:
		switch t.text {
		case "(":
			x, err := p.parseExpr(0)
			if err != nil {
				return nil, err
			}
			if c := p.next(); c.text != ")" || c.kind != tOp {
				return nil, p.errAt(c, "a ( was opened and is not closed; add the )")
			}
			return x, nil
		case "!", "-":
			x, err := p.parseUnary()
			if err != nil {
				return nil, err
			}
			return unNode{t.text, x, t.pos}, nil
		}
	case tEOF:
		return nil, p.errAt(t, "the expression ends where a value was expected; add a name, a number or a 'text' after the last operator")
	}
	return nil, p.errAt(t, "%q is not expected here; a value (name, number, 'text') or ( or ! was expected", t.text)
}
