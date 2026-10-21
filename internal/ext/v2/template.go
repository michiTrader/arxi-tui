package extv2

import (
	"encoding/json"
	"fmt"
	"net"
	"strings"
)

// A template is a string with placeholders: ${args} (everything after the command
// name), ${1}..${9} (the words of it), ${input} (the line a rule is looking at) and
// ${text} (the model's answer, for an event on turn.done only).
// "$$" is a literal dollar sign and any other "$" is itself, so "costs $5" needs no
// escaping. The syntax is chosen so that JSON, which is full of braces, can be written
// without escaping them.
//
// A placeholder that names nothing is refused when the package is validated and never
// becomes an empty string: an operation that silently ran with a hole in it is worse
// than one that did not run.

type tmplPart struct {
	lit string // literal text, when ph is empty
	ph  string // placeholder name, when set
}

func parseTemplate(s string) ([]tmplPart, error) {
	var parts []tmplPart
	var lit strings.Builder
	flush := func() {
		if lit.Len() > 0 {
			parts = append(parts, tmplPart{lit: lit.String()})
			lit.Reset()
		}
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c != '$' {
			lit.WriteByte(c)
			continue
		}
		if i+1 < len(s) && s[i+1] == '$' {
			lit.WriteByte('$')
			i++
			continue
		}
		if i+1 < len(s) && s[i+1] == '{' {
			end := strings.IndexByte(s[i+2:], '}')
			if end < 0 {
				return nil, fmt.Errorf("%q has \"${\" that is never closed, so the placeholder is incomplete; close it with \"}\" or write \"$$\" for a literal dollar sign", s)
			}
			name := s[i+2 : i+2+end]
			if !knownPlaceholder(name) {
				return nil, fmt.Errorf("placeholder ${%s} is not one this contract knows; the placeholders are ${args}, ${1} to ${9}, ${input} and ${text}", name)
			}
			flush()
			parts = append(parts, tmplPart{ph: name})
			i += 2 + end
			continue
		}
		lit.WriteByte('$')
	}
	flush()
	return parts, nil
}

func knownPlaceholder(n string) bool {
	if n == "args" || n == "input" || n == textPlaceholder {
		return true
	}
	return len(n) == 1 && n[0] >= '1' && n[0] <= '9'
}

// checkPlaceholders refuses a placeholder the place it is used cannot fill.
func checkPlaceholders(who string, parts []tmplPart, oc opCtx) error {
	for _, p := range parts {
		if p.ph == "" {
			continue
		}
		switch {
		case p.ph == textPlaceholder && oc.kind == ctxEvent && oc.event == EventTurnDone:
			// The one value an automatic operation may be given: the answer the
			// model just finished. The host never runs the event when there is none.
			continue
		case p.ph == textPlaceholder:
			return fmt.Errorf("%s: ${text} is the model's answer, and only an event on %s has one; here use another value or write it out", who, EventTurnDone)
		case oc.kind.automatic():
			return fmt.Errorf("%s: ${%s} has nothing to fill it, because this runs by itself and the user typed no text; write the value out", who, p.ph)
		case p.ph == "input" && oc.kind != ctxRule:
			return fmt.Errorf("%s: ${input} is the line a rule looks at, and a command has no such line; use ${args} or ${1}", who)
		case p.ph != "input" && oc.kind == ctxRule:
			return fmt.Errorf("%s: ${%s} is for commands, and a rule has no arguments; use ${input}", who, p.ph)
		case len(p.ph) == 1 && p.ph[0] >= '1' && p.ph[0] <= '9' && int(p.ph[0]-'0') > oc.minArgs:
			return fmt.Errorf("%s: ${%s} may be missing when the command is typed, and a hole is never filled with nothing; set \"min_args\" to at least %s", who, p.ph, p.ph)
		}
	}
	return nil
}

func hasPlaceholder(parts []tmplPart) bool {
	for _, p := range parts {
		if p.ph != "" {
			return true
		}
	}
	return false
}

// textPlaceholder names the answer of the model, which a turn.done event may use.
const textPlaceholder = "text"

// MaxEventTextRunes bounds the answer an event is given. It is outside text, so it is
// cleaned like any output and cut with an ellipsis when it is longer.
const MaxEventTextRunes = 4000

// Input is what an operation is run with.
type Input struct {
	Args  string   // the text after the command name, trimmed
	Words []string // Args split on white space
	Line  string   // the line a rule is looking at
	Text  string   // the model's answer (an event on turn.done only), already cleaned
}

func (in Input) value(name string) string {
	switch name {
	case "args":
		return in.Args
	case "input":
		return in.Line
	case textPlaceholder:
		return in.Text
	}
	n := int(name[0] - '0')
	if n >= 1 && n <= len(in.Words) {
		return in.Words[n-1]
	}
	return ""
}

// expand fills parts, passing each value through esc for the place it lands in.
func expand(parts []tmplPart, in Input, esc func(string) string) string {
	var b strings.Builder
	for _, p := range parts {
		if p.ph == "" {
			b.WriteString(p.lit)
			continue
		}
		v := in.value(p.ph)
		if esc != nil {
			v = esc(v)
		}
		b.WriteString(v)
	}
	return b.String()
}

// jsonEscape escapes v for the inside of a JSON string literal.
func jsonEscape(v string) string {
	b, _ := json.Marshal(v)
	return string(b[1 : len(b)-1])
}

// jsonStringsOnly refuses a template whose placeholders are not inside a JSON string
// literal, because a value that lands outside one can be anything (a number, a nested
// object) and escaping it is no protection.
func jsonStringsOnly(who string, parts []tmplPart) error {
	inStr, esc := false, false
	for _, p := range parts {
		if p.ph != "" {
			if !inStr || esc {
				return fmt.Errorf("%s: body places ${%s} outside a JSON string, so the value could change the shape of the request; put it inside quotes, like {\"q\": \"${%s}\"}", who, p.ph, p.ph)
			}
			continue
		}
		for i := 0; i < len(p.lit); i++ {
			c := p.lit[i]
			switch {
			case esc:
				esc = false
			case inStr && c == '\\':
				esc = true
			case c == '"':
				inStr = !inStr
			}
		}
	}
	return nil
}

func looksLikeAddress(host string) bool {
	if net.ParseIP(host) != nil {
		return true
	}
	// A last label made only of digits is not a name any registry hands out; some
	// resolvers read "10.1" or "0x7f.1" as an address, so refuse the shape.
	last := host
	if i := strings.LastIndexByte(host, '.'); i >= 0 {
		last = host[i+1:]
	}
	if last == "" {
		return true
	}
	digits := true
	for _, c := range last {
		if c < '0' || c > '9' {
			digits = false
		}
	}
	return digits || strings.HasPrefix(last, "0x")
}

// isPackagePath reports whether s names a file inside the package: "./tool.sh" or
// "./bin/tool". It is decided on the text alone, the same on every platform, so the
// answer does not depend on where the package is opened.
func isPackagePath(s string) bool {
	if !strings.HasPrefix(s, "./") || strings.HasSuffix(s, "/") {
		return false
	}
	if strings.ContainsAny(s, "\\:\x00") {
		return false
	}
	for _, seg := range strings.Split(s[2:], "/") {
		if seg == "" || seg == "." || seg == ".." {
			return false
		}
	}
	return true
}
