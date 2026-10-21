package extv2

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/charmbracelet/x/ansi"
)

// Verdicts an outcome may carry. They are ordered by how much of the user's action
// they stop: a line that continues is sent as typed, a transformed one is sent
// changed, a handled one is consumed quietly, a blocked one is consumed with a reason.
const (
	VerdictContinue  = "continue"
	VerdictTransform = "transform"
	VerdictHandled   = "handled"
	VerdictBlock     = "block"
)

// Limits on what an operation may hand back.
const (
	MaxOutputBytes = 64 * 1024
	MaxSayRunes    = 4000
	MaxSayLines    = 40
	MaxLineRunes   = 16000
)

// Outcome is what one operation produced. It is data: nothing in it has been done yet.
// The host decides what to do with each field, and the narrowing rule (pipeline.go)
// decides which of them it will honour.
type Outcome struct {
	Verdict string // for a rule: continue, transform, handled or block
	Text    string // the replacement line of a transform
	Say     string // shown to the user, never sent to the model
	Send    string // sent to the model (a command with permissions.send)
	Reason  string // shown when a line is blocked
}

// BuiltinFunc is a host function a package may name. It runs in the host with the
// host's authority, which is why the set is closed and registered in Go: a package
// chooses among them and never adds one.
type BuiltinFunc func(ctx context.Context, in Input) (Outcome, error)

// Runner runs operations. The zero value is usable and runs only what needs no host
// help; every field is a seam a test or the host fills.
type Runner struct {
	Builtins map[string]BuiltinFunc
	// PackageDir returns where a package's own files live ("" when it has none),
	// so "./tool.sh" resolves inside it and nowhere else.
	PackageDir func(pkgID string) string

	http httpSeams
	exec execSeams
}

// OutcomeJSON is the shape a script or service returns when its operation's result is
// "outcome". Unknown keys are refused: a key the host ignores is a promise nobody keeps.
type outcomeJSON struct {
	Verdict string `json:"verdict"`
	Text    string `json:"text"`
	Say     string `json:"say"`
	Send    string `json:"send"`
	Reason  string `json:"reason"`
}

// finish turns the raw output of an http or exec operation into an Outcome according
// to the operation's result mode. Output is untrusted: it is cleaned here, before
// anything else sees it.
func finish(o Op, oc opCtx, raw []byte, truncated bool) (Outcome, error) {
	result := o.Result
	if result == "" {
		if oc.kind == ctxRule {
			result = ResultTransform
		} else {
			result = ResultSay
		}
	}
	text := string(raw)
	if result == ResultOutcome {
		if truncated {
			return Outcome{}, fmt.Errorf("the output is longer than %d bytes, so it is not a complete JSON outcome; make it shorter", MaxOutputBytes)
		}
		return parseOutcome(raw)
	}
	if o.Extract != "" {
		if truncated {
			return Outcome{}, fmt.Errorf("the output is longer than %d bytes, so \"extract\" could not read it whole; ask the service for less", MaxOutputBytes)
		}
		v, err := extractPath(raw, o.Extract, o.Join)
		if err != nil {
			return Outcome{}, err
		}
		text = v
	}
	switch result {
	case ResultSay:
		return Outcome{Say: CleanText(strings.TrimRight(text, " \r\n\t"), MaxSayRunes, MaxSayLines)}, nil
	case ResultSend:
		return Outcome{Send: CleanText(strings.TrimSpace(text), MaxLineRunes, 0)}, nil
	case ResultTransform:
		return Outcome{Verdict: VerdictTransform, Text: CleanText(strings.TrimSpace(text), MaxLineRunes, 0)}, nil
	}
	return Outcome{}, fmt.Errorf("result %q is not understood", result)
}

func parseOutcome(raw []byte) (Outcome, error) {
	var j outcomeJSON
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&j); err != nil {
		return Outcome{}, fmt.Errorf("the output is not a JSON outcome (%v); it must be one object with verdict, and optionally text, say, send and reason", firstLine(err.Error()))
	}
	if dec.More() {
		return Outcome{}, fmt.Errorf("the output holds more than one JSON value; print exactly one object")
	}
	switch j.Verdict {
	case VerdictContinue, VerdictTransform, VerdictHandled, VerdictBlock:
	case "":
		return Outcome{}, fmt.Errorf("the outcome has no verdict; use one of %s, %s, %s, %s", VerdictContinue, VerdictTransform, VerdictHandled, VerdictBlock)
	default:
		return Outcome{}, fmt.Errorf("verdict %q is not one of %s, %s, %s, %s", j.Verdict, VerdictContinue, VerdictTransform, VerdictHandled, VerdictBlock)
	}
	return Outcome{
		Verdict: j.Verdict,
		Text:    CleanText(strings.TrimSpace(j.Text), MaxLineRunes, 0),
		Say:     CleanText(strings.TrimRight(j.Say, " \r\n\t"), MaxSayRunes, MaxSayLines),
		Send:    CleanText(strings.TrimSpace(j.Send), MaxLineRunes, 0),
		Reason:  CleanText(j.Reason, 400, 1),
	}, nil
}

// extractPath reads one value out of a JSON document by a dotted path. A name selects
// a key and a number selects a list position. Only a single scalar is a value: an
// object or a list is refused, because showing "map[...]" is never what was meant.
func extractPath(raw []byte, path, join string) (string, error) {
	var v any
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	if err := dec.Decode(&v); err != nil {
		return "", fmt.Errorf("the output is not JSON (%v), so extract %q cannot read it", firstLine(err.Error()), path)
	}
	segs := strings.Split(path, ".")
	for i, seg := range segs {
		if seg != "*" {
			continue
		}
		// "*" walks every entry of the list before it and joins what each one yields.
		list, err := walkPath(v, segs[:i], path)
		if err != nil {
			return "", err
		}
		items, ok := list.([]any)
		if !ok {
			return "", fmt.Errorf("\"*\" in %q needs a list, and the value before it is not one", path)
		}
		if len(items) == 0 {
			return "", fmt.Errorf("the list before \"*\" in %q is empty, so there is nothing to join", path)
		}
		if len(items) > MaxJoined {
			return "", fmt.Errorf("the list before \"*\" in %q has %d entries, more than the %d that are joined; ask the service for less", path, len(items), MaxJoined)
		}
		sep, ok := joinSeparators[join]
		if !ok {
			return "", fmt.Errorf("join %q is not understood; use %s", join, strings.Join(JoinKinds, ", "))
		}
		parts := make([]string, 0, len(items))
		for n, it := range items {
			leaf, err := walkPath(it, segs[i+1:], path)
			if err != nil {
				return "", fmt.Errorf("entry %d of the list: %w", n, err)
			}
			s, err := scalarText(leaf, path)
			if err != nil {
				return "", fmt.Errorf("entry %d of the list: %w", n, err)
			}
			parts = append(parts, s)
		}
		return strings.Join(parts, sep), nil
	}
	leaf, err := walkPath(v, segs, path)
	if err != nil {
		return "", err
	}
	return scalarText(leaf, path)
}

// walkPath follows names and list positions from cur; it never meets a "*".
func walkPath(cur any, segs []string, path string) (any, error) {
	for _, seg := range segs {
		switch node := cur.(type) {
		case map[string]any:
			next, ok := node[seg]
			if !ok {
				return nil, fmt.Errorf("the output has no %q at %q; the keys there are %s", seg, path, keyList(node))
			}
			cur = next
		case []any:
			i, err := strconv.Atoi(seg)
			if err != nil || i < 0 || i >= len(node) {
				return nil, fmt.Errorf("%q is not a position in a list of %d at %q", seg, len(node), path)
			}
			cur = node[i]
		default:
			return nil, fmt.Errorf("%q reaches a single value before %q ends; shorten the path", path, seg)
		}
	}
	return cur, nil
}

// scalarText is the text of a single value; an object or a list is never one.
func scalarText(cur any, path string) (string, error) {
	switch s := cur.(type) {
	case string:
		return s, nil
	case json.Number:
		return s.String(), nil
	case bool:
		return strconv.FormatBool(s), nil
	case nil:
		return "", fmt.Errorf("the value at %q is null, so there is nothing to show", path)
	}
	return "", fmt.Errorf("the value at %q is an object or a list, not a single value; extend the path to one of its entries", path)
}

func keyList(m map[string]any) string {
	var ks []string
	for k := range m {
		ks = append(ks, k)
		if len(ks) == 12 {
			break
		}
	}
	if len(ks) == 0 {
		return "(none)"
	}
	// stable output for the message
	for i := 1; i < len(ks); i++ {
		for j := i; j > 0 && ks[j] < ks[j-1]; j-- {
			ks[j], ks[j-1] = ks[j-1], ks[j]
		}
	}
	return strings.Join(ks, ", ")
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	return CleanText(s, 160, 1)
}

// CleanText makes text from outside safe to show or to forward. It removes escape
// sequences and control characters (a terminal would act on them instead of showing
// them), the characters that reorder text on screen, and invalid UTF-8; it turns
// CRLF into a line break and tabs into spaces; and it cuts the result to maxRunes
// runes and maxLines lines (0 means no line limit), ending with an ellipsis when it
// cut. Line breaks survive, so a program's table still reads as one.
func CleanText(s string, maxRunes, maxLines int) string {
	s = strings.ToValidUTF8(s, "")
	s = ansi.Strip(s)
	s = strings.ReplaceAll(s, "\r\n", "\n")
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		switch {
		case r == '\n':
			b.WriteByte('\n')
		case r == '\t':
			b.WriteString("    ")
		case r == '\r':
			b.WriteByte('\n')
		case unicode.IsControl(r) || isBidi(r):
			continue
		default:
			b.WriteRune(r)
		}
	}
	out := b.String()
	cut := false
	if maxLines > 0 {
		lines := strings.Split(out, "\n")
		if len(lines) > maxLines {
			out = strings.Join(lines[:maxLines], "\n")
			cut = true
		}
	}
	if utf8.RuneCountInString(out) > maxRunes {
		out = string([]rune(out)[:maxRunes-1])
		cut = true
	}
	if cut {
		out = strings.TrimRight(out, " \n") + "…"
	}
	return out
}
