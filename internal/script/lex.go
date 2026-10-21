package script

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
	tTpl
	tIdent
	tKw
	tPunct
)

type tplPart struct {
	lit       string
	src       string
	isExpr    bool
	line, col int
}

type token struct {
	kind      tokKind
	text      string
	num       float64
	parts     []tplPart
	line, col int
	nl        bool // a line break came before this token
}

func (t token) String() string {
	switch t.kind {
	case tEOF:
		return "the end of the script"
	case tStr:
		return "a text"
	case tTpl:
		return "a template text"
	case tNum:
		return "the number " + t.text
	}
	return "`" + t.text + "`"
}

var keywords = map[string]bool{
	"let": true, "const": true, "var": true, "function": true, "return": true, "if": true,
	"else": true, "for": true, "while": true, "break": true, "continue": true, "true": true,
	"false": true, "null": true, "typeof": true, "in": true,
}

// puncts is every punctuator the lexer knows, longest first, so that "===" is not read as
// "==" and "=". Some of them (`==`, `&`, `...`) are read only to be refused with a remedy.
var puncts = []string{
	"===", "!==", "**=", "...", "<<=", ">>=",
	"=>", "==", "!=", "<=", ">=", "&&", "||", "??", "?.", "++", "--", "+=", "-=", "*=", "/=", "%=", "**", "<<", ">>",
	"+", "-", "*", "/", "%", "=", "<", ">", "!", "?", ":", ";", ",", ".", "(", ")", "[", "]", "{", "}", "&", "|", "^", "~",
}

type lexer struct {
	name      string
	src       []rune
	i         int
	line, col int
	toks      []token
	nl        bool
}

func synErr(name string, line, col int, format string, a ...any) *Error {
	return &Error{Kind: KindSyntax, Name: name, Line: line, Col: col, Msg: fmt.Sprintf(format, a...)}
}

func lex(name, src string, line, col int) ([]token, error) {
	lx := &lexer{name: name, src: []rune(src), line: line, col: col}
	for {
		if err := lx.skip(); err != nil {
			return nil, err
		}
		if lx.i >= len(lx.src) {
			lx.toks = append(lx.toks, token{kind: tEOF, line: lx.line, col: lx.col, nl: true})
			return lx.toks, nil
		}
		if err := lx.next(); err != nil {
			return nil, err
		}
	}
}

func (lx *lexer) peek(k int) rune {
	if lx.i+k < len(lx.src) {
		return lx.src[lx.i+k]
	}
	return 0
}

func (lx *lexer) adv() rune {
	r := lx.src[lx.i]
	lx.i++
	if r == '\n' {
		lx.line++
		lx.col = 1
		lx.nl = true
	} else {
		lx.col++
	}
	return r
}

func (lx *lexer) skip() error {
	for lx.i < len(lx.src) {
		r := lx.src[lx.i]
		switch {
		case unicode.IsSpace(r):
			lx.adv()
		case r == '/' && lx.peek(1) == '/':
			for lx.i < len(lx.src) && lx.src[lx.i] != '\n' {
				lx.adv()
			}
		case r == '/' && lx.peek(1) == '*':
			l, c := lx.line, lx.col
			lx.adv()
			lx.adv()
			for {
				if lx.i >= len(lx.src) {
					return synErr(lx.name, l, c, "this /* comment is never closed. Remedy: end it with */.")
				}
				if lx.src[lx.i] == '*' && lx.peek(1) == '/' {
					lx.adv()
					lx.adv()
					break
				}
				lx.adv()
			}
		default:
			return nil
		}
	}
	return nil
}

func isIdentStart(r rune) bool { return r == '_' || r == '$' || unicode.IsLetter(r) }
func isIdentPart(r rune) bool  { return isIdentStart(r) || unicode.IsDigit(r) }

func (lx *lexer) emit(t token) {
	t.nl = lx.nl
	lx.nl = false
	lx.toks = append(lx.toks, t)
}

func (lx *lexer) next() error {
	line, col := lx.line, lx.col
	r := lx.src[lx.i]
	switch {
	case isIdentStart(r):
		start := lx.i
		for lx.i < len(lx.src) && isIdentPart(lx.src[lx.i]) {
			lx.adv()
		}
		word := string(lx.src[start:lx.i])
		k := tIdent
		if keywords[word] {
			k = tKw
		}
		lx.emit(token{kind: k, text: word, line: line, col: col})
	case unicode.IsDigit(r) || (r == '.' && unicode.IsDigit(lx.peek(1))):
		return lx.number(line, col)
	case r == '"' || r == '\'':
		s, err := lx.str(r)
		if err != nil {
			return err
		}
		lx.emit(token{kind: tStr, text: s, line: line, col: col})
	case r == '`':
		return lx.template(line, col)
	default:
		for _, p := range puncts {
			if lx.matches(p) {
				if p == "?." && unicode.IsDigit(lx.peek(2)) {
					continue // `a?.5:1` is a condition, not an optional chain
				}
				for range p {
					lx.adv()
				}
				lx.emit(token{kind: tPunct, text: p, line: line, col: col})
				return nil
			}
		}
		return synErr(lx.name, line, col, "the character %q is not part of the language. Remedy: remove it, or put it inside a text.", string(r))
	}
	return nil
}

func (lx *lexer) matches(p string) bool {
	for k, c := range p {
		if lx.peek(k) != c {
			return false
		}
	}
	return true
}

func (lx *lexer) number(line, col int) error {
	start := lx.i
	if lx.src[lx.i] == '0' && (lx.peek(1) == 'x' || lx.peek(1) == 'X') {
		lx.adv()
		lx.adv()
		for lx.i < len(lx.src) && strings.ContainsRune("0123456789abcdefABCDEF_", lx.src[lx.i]) {
			lx.adv()
		}
	} else {
		for lx.i < len(lx.src) && (unicode.IsDigit(lx.src[lx.i]) || lx.src[lx.i] == '_') {
			lx.adv()
		}
		if lx.peek(0) == '.' {
			lx.adv()
			for lx.i < len(lx.src) && (unicode.IsDigit(lx.src[lx.i]) || lx.src[lx.i] == '_') {
				lx.adv()
			}
		}
		if lx.peek(0) == 'e' || lx.peek(0) == 'E' {
			k := 1
			if lx.peek(1) == '+' || lx.peek(1) == '-' {
				k = 2
			}
			if unicode.IsDigit(lx.peek(k)) {
				for ; k > 0; k-- {
					lx.adv()
				}
				for lx.i < len(lx.src) && unicode.IsDigit(lx.src[lx.i]) {
					lx.adv()
				}
			}
		}
	}
	text := strings.ReplaceAll(string(lx.src[start:lx.i]), "_", "")
	var f float64
	if strings.HasPrefix(text, "0x") || strings.HasPrefix(text, "0X") {
		n, err := strconv.ParseUint(text[2:], 16, 64)
		if err != nil {
			return synErr(lx.name, line, col, "`%s` is not a number. Remedy: write hexadecimal digits after 0x.", text)
		}
		f = float64(n)
	} else {
		var err error
		f, err = strconv.ParseFloat(text, 64)
		if err != nil {
			return synErr(lx.name, line, col, "`%s` is not a number. Remedy: write it like 12, 1.5 or 2e3.", text)
		}
	}
	if lx.i < len(lx.src) && isIdentStart(lx.src[lx.i]) {
		return synErr(lx.name, line, col, "a number is followed by letters (`%s%c...`). Remedy: put a space or an operator between them.", text, lx.src[lx.i])
	}
	lx.emit(token{kind: tNum, text: text, num: f, line: line, col: col})
	return nil
}

// escape reads the character after a backslash (the backslash is already consumed).
func (lx *lexer) escape(sb *strings.Builder) error {
	if lx.i >= len(lx.src) {
		return synErr(lx.name, lx.line, lx.col, "a text ends right after a backslash. Remedy: write \\\\ for a backslash.")
	}
	e := lx.adv()
	switch e {
	case 'n':
		sb.WriteByte('\n')
	case 't':
		sb.WriteByte('\t')
	case 'r':
		sb.WriteByte('\r')
	case '0':
		sb.WriteByte(0)
	case 'x', 'u':
		n := 2
		if e == 'u' {
			n = 4
		}
		if lx.i+n > len(lx.src) {
			return synErr(lx.name, lx.line, lx.col, "\\%c needs %d hexadecimal digits.", e, n)
		}
		v, err := strconv.ParseUint(string(lx.src[lx.i:lx.i+n]), 16, 32)
		if err != nil {
			return synErr(lx.name, lx.line, lx.col, "\\%c needs %d hexadecimal digits.", e, n)
		}
		for k := 0; k < n; k++ {
			lx.adv()
		}
		sb.WriteRune(rune(v))
	case '\n':
	default:
		sb.WriteRune(e)
	}
	return nil
}

func (lx *lexer) str(q rune) (string, error) {
	line, col := lx.line, lx.col
	lx.adv()
	var sb strings.Builder
	for {
		if lx.i >= len(lx.src) || lx.src[lx.i] == '\n' {
			return "", synErr(lx.name, line, col, "this text is never closed. Remedy: end it with %c on the same line, or use a template text with backticks for several lines.", q)
		}
		r := lx.adv()
		switch r {
		case q:
			return sb.String(), nil
		case '\\':
			if err := lx.escape(&sb); err != nil {
				return "", err
			}
		default:
			sb.WriteRune(r)
		}
	}
}

func (lx *lexer) template(line, col int) error {
	lx.adv()
	var parts []tplPart
	var sb strings.Builder
	flush := func() {
		if sb.Len() > 0 {
			parts = append(parts, tplPart{lit: sb.String()})
			sb.Reset()
		}
	}
	for {
		if lx.i >= len(lx.src) {
			return synErr(lx.name, line, col, "this template text is never closed. Remedy: end it with a backtick.")
		}
		r := lx.adv()
		switch {
		case r == '`':
			flush()
			lx.emit(token{kind: tTpl, parts: parts, line: line, col: col})
			return nil
		case r == '\\':
			if err := lx.escape(&sb); err != nil {
				return err
			}
		case r == '$' && lx.peek(0) == '{':
			flush()
			lx.adv()
			el, ec := lx.line, lx.col
			start := lx.i
			depth := 1
			for depth > 0 {
				if lx.i >= len(lx.src) {
					return synErr(lx.name, el, ec, "this ${ is never closed. Remedy: end the expression with }.")
				}
				c := lx.src[lx.i]
				switch c {
				case '{':
					depth++
				case '}':
					depth--
				case '"', '\'', '`':
					q := c
					lx.adv()
					for lx.i < len(lx.src) && lx.src[lx.i] != q {
						if lx.src[lx.i] == '\\' {
							lx.adv()
						}
						if lx.i < len(lx.src) {
							lx.adv()
						}
					}
				}
				if lx.i < len(lx.src) {
					lx.adv()
				}
			}
			src := string(lx.src[start : lx.i-1])
			if strings.TrimSpace(src) == "" {
				return synErr(lx.name, el, ec, "this ${} is empty. Remedy: put an expression inside, or remove it.")
			}
			parts = append(parts, tplPart{src: src, isExpr: true, line: el, col: ec})
		default:
			sb.WriteRune(r)
		}
	}
}
