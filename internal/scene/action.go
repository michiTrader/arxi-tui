package scene

import (
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/michiTrader/arxi_tui/internal/verb"
)

// ActionKind is the closed prefix set of an on_press action (BINDS.md §4.8).
//
// It is a closed set because Q18's decision is that the action vocabulary is
// closed per surface: a prefix outside this set is a load-time refusal, not a
// silent no-op. The set is small on purpose — eight live kinds — so the host's
// dispatch switch is exhaustive and a new kind is a signed change to this type,
// never a string the host quietly ignores.
type ActionKind int

const (
	// ActionCmd runs a command line as if the user had typed it (cmd:/agent 5).
	ActionCmd ActionKind = iota
	// ActionFocus sets ui.focus to a node id (focus:reject).
	ActionFocus
	// ActionAnswer answers the agent's pending item with a kind (answer:approve).
	ActionAnswer
	// ActionExt routes a press to a behavioral plugin subprocess as an `action`
	// frame (ext:tick:refresh, DESIGN-BLOCK-I §I-E). The plugin id and the action
	// name are two segments, so an ActionExt carries PluginID as well as Arg. The
	// grammar is signed here (H8) and dispatched by I4: the scene layer only
	// checks the shape (a non-empty plugin id and action); whether a plugin with
	// that id is mounted and granted the capability is a runtime concern the host
	// dispatcher reports on, never a load-time refusal — the same split focus:
	// already lives under (a focus: naming no node reports at press time).
	ActionExt
	// ActionSay shows a local message (say:Saved). The text is the host's own
	// answer to the user: it is sanitised and bounded when shown and is never sent
	// to the model. The scene layer checks only that there is text to show.
	ActionSay
	// ActionOpen sends the user's browser to a web address (open:https://example.com/).
	// The grammar here is the part that needs no host state: https only, no
	// credentials, a plain host name (no address, no port). Whether the host is one a
	// package the user approved may open is a runtime concern, and so is the rule that
	// a hook never opens anything; both are the host dispatcher's to refuse.
	ActionOpen
	// ActionSet changes one of the closed settings (set:effort=max). The scene layer
	// checks the shape and the setting name; the values each setting takes belong to
	// the host (the effort levels depend on the model in use), which refuses the rest.
	ActionSet
	// ActionDo calls a verb of the catalogue (do:shell.stop 2). The verbs, their parameters
	// and who may run them are in internal/verb; this layer checks that the verb exists and
	// the arguments fit (a value still holding a {row.<field>} is checked once it is
	// replaced). Which verbs a button may carry is the validator's rule, and what the
	// verb does is the host's.
	ActionDo
)

// SetSettings are the settings a set: action may change, in a stable order. They are
// the ones a hook can already watch, so the closed set is the same everywhere.
func SetSettings() []string { return []string{"effort", "mode", "style"} }

const (
	// MaxOpenURL bounds an open: address.
	MaxOpenURL = 2000
	// MaxSetValue bounds the value of a set: action.
	MaxSetValue = 32
)

var (
	openHostRe = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?(\.[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?)+$`)
	setValueRe = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]*$`)
)

// ParseOpenURL checks an open: address and returns its host. It is the one reader of
// what an address may be, so the validator at load and the host at press time (after
// a {row.<field>} was expanded into it) cannot disagree. https only; no user or
// password; a registered-looking name, never an IP address, a bare name such as
// localhost, or a port; no whitespace or control characters.
func ParseOpenURL(raw string) (string, error) {
	if raw == "" {
		return "", fmt.Errorf("open: needs an address; write open:https://example.com/")
	}
	if utf8.RuneCountInString(raw) > MaxOpenURL {
		return "", fmt.Errorf("open: address is longer than %d characters", MaxOpenURL)
	}
	for _, r := range raw {
		if unicode.IsControl(r) || unicode.IsSpace(r) || r == '\\' || r > unicode.MaxASCII {
			return "", fmt.Errorf("open: address %q holds a space, control or non-ASCII character; write the address in its encoded form", raw)
		}
	}
	if !strings.HasPrefix(raw, "https://") {
		return "", fmt.Errorf("open: address %q must start with https://; no other scheme can be opened", raw)
	}
	u, err := url.Parse(raw)
	if err != nil {
		return "", fmt.Errorf("open: address %q is not valid: %v", raw, err)
	}
	if u.User != nil || strings.Contains(strings.SplitN(strings.TrimPrefix(raw, "https://"), "/", 2)[0], "@") {
		return "", fmt.Errorf("open: address %q carries a user name or password; an address that hides where it goes is refused", raw)
	}
	if u.Port() != "" || strings.Contains(u.Host, ":") {
		return "", fmt.Errorf("open: address %q names a port; only the plain https address of a host can be opened", raw)
	}
	host := u.Hostname()
	if !openHostRe.MatchString(host) || !hasLetter(host[strings.LastIndexByte(host, '.')+1:]) {
		return "", fmt.Errorf("open: %q is not a host name (an IP address, localhost or a name with odd characters cannot be opened); write a name like example.com", host)
	}
	return host, nil
}

func hasLetter(s string) bool {
	for _, r := range s {
		if r >= 'a' && r <= 'z' {
			return true
		}
	}
	return false
}

// Action is a parsed on_press value: one of the closed kinds and its argument.
//
// One type read by both the validator and the host dispatcher, for the reason
// Scroll/FocusGlow are single named types: the grammar the validator refuses and
// the grammar the host routes are the same declaration, so they cannot drift. A
// second parser at the dispatch site would be a second answer to "what is a
// legal action", and the two would disagree the first time the vocabulary grew.
type Action struct {
	Kind ActionKind
	// Arg is the command line, node id, kind, or — for ActionExt — the action
	// name. It is the whole content after the prefix, except that ActionExt splits
	// off the plugin id into PluginID and leaves the action name here. It may still
	// contain {row.<field>} interpolation tokens (Q20); the host substitutes them
	// at press time against the row scope, so Arg is stored verbatim and not
	// expanded here.
	Arg string
	// PluginID is the target plugin's id for an ActionExt (the <plugin-id> of
	// ext:<plugin-id>:<action>), and empty for every other kind. It is a separate
	// field rather than left inside Arg so the host dispatcher routes by id without
	// re-splitting the string — a second split would be a second reader of the
	// grammar, the drift this one-parser design exists to prevent.
	PluginID string
	// Setting is the setting of an ActionSet (the "effort" of set:effort=max), and
	// empty for every other kind; Arg holds its value. For an ActionOpen, Arg is the
	// address and Host its host name, so the dispatcher checks the host without
	// parsing the address a second time.
	Setting string
	Host    string
}

// answerKinds is the closed kind set an `answer:` action may name (BINDS.md
// §4.8). It mirrors the arxi core's inbox verbs (inbox.approve/reject/reply) so
// a scene author and the core agree on what a button means; a kind outside this
// set is refused with an address rather than sent to the core to be rejected.
var answerKinds = map[string]bool{
	"approve": true,
	"reject":  true,
	"reply":   true,
}

// AnswerKinds returns the legal `answer:` kinds in a stable order, for messages
// and tests. Freshly built per call so a caller cannot mutate the set.
func AnswerKinds() []string {
	return []string{"approve", "reject", "reply"}
}

// ParseAction parses an on_press value into the closed action grammar (BINDS.md
// §4.8), returning a plain error the caller addresses with file:line.
//
// It is the single reader of the grammar: the validator calls it to refuse a
// malformed action at load, and the host calls it to route a press. The error
// messages name the consequence and the remedy because they reach the author
// through the validator and the Phase 2 repair loop reads them.
//
// `ext:<plugin-id>:<action>` is the behavioral-plugin arm (DESIGN-BLOCK-I §I-E,
// I4): a press routed to a subprocess over NDJSON. The grammar is checked here —
// a non-empty plugin id and a non-empty action name, the two segments the
// `action` frame is built from — but whether a plugin with that id is mounted and
// was granted the capability is a runtime concern the host dispatcher reports on,
// never a load-time refusal, exactly as a focus: naming no node reports at press
// time rather than failing to load.
func ParseAction(s string) (Action, error) {
	prefix, arg, ok := strings.Cut(s, ":")
	if !ok {
		return Action{}, fmt.Errorf("on_press %q has no action prefix; it must be one of cmd:<command>, focus:<node>, answer:<kind>, ext:<plugin-id>:<action>, say:<text>, open:<https address>, set:<setting>=<value>, do:<verb> <arguments> (BINDS.md §4.8)", s)
	}
	switch prefix {
	case "cmd":
		if arg == "" {
			return Action{}, fmt.Errorf("on_press %q names a cmd: action with no command; the command line is the whole content of the action (BINDS.md §4.8)", s)
		}
		return Action{Kind: ActionCmd, Arg: arg}, nil
	case "focus":
		if arg == "" {
			return Action{}, fmt.Errorf("on_press %q names a focus: action with no node id; a focus action sets ui.focus to a node's id (BINDS.md §4.8)", s)
		}
		return Action{Kind: ActionFocus, Arg: arg}, nil
	case "answer":
		if arg == "" {
			return Action{}, fmt.Errorf("on_press %q names an answer: action with no kind; the legal kinds are %s (BINDS.md §4.8)", s, strings.Join(AnswerKinds(), ", "))
		}
		if !answerKinds[arg] {
			return Action{}, fmt.Errorf("on_press %q answers with kind %q, which is not in the closed answer vocabulary %s (BINDS.md §4.8); the kinds mirror the core's inbox verbs", s, arg, strings.Join(AnswerKinds(), ", "))
		}
		return Action{Kind: ActionAnswer, Arg: arg}, nil
	case "ext":
		// The argument is <plugin-id>:<action>. Cut on the first colon only: the
		// plugin id cannot contain a colon (the manifest id grammar is
		// [a-z][a-z0-9-]*), so a later colon belongs to the action name and is
		// kept there rather than splitting it off.
		pluginID, action, ok := strings.Cut(arg, ":")
		if !ok || pluginID == "" {
			return Action{}, fmt.Errorf("on_press %q names an ext: action with no plugin id; the form is ext:<plugin-id>:<action>, routing a press to the plugin's subprocess (BINDS.md §4.8, DESIGN-BLOCK-I §I-E)", s)
		}
		if action == "" {
			return Action{}, fmt.Errorf("on_press %q names an ext: action on plugin %q with no action name; the form is ext:<plugin-id>:<action>, and the action is the name the plugin registered (BINDS.md §4.8, DESIGN-BLOCK-I §I-E)", s, pluginID)
		}
		return Action{Kind: ActionExt, PluginID: pluginID, Arg: action}, nil
	case "say":
		if strings.TrimSpace(arg) == "" {
			return Action{}, fmt.Errorf("on_press %q names a say: action with no text; write say:<text to show> (BINDS.md §4.8)", s)
		}
		return Action{Kind: ActionSay, Arg: arg}, nil
	case "open":
		host, err := ParseOpenURL(arg)
		if err != nil {
			return Action{}, fmt.Errorf("on_press %q: %w (BINDS.md §4.8)", s, err)
		}
		return Action{Kind: ActionOpen, Arg: arg, Host: host}, nil
	case "set":
		setting, value, ok := strings.Cut(arg, "=")
		if !ok || setting == "" || value == "" {
			return Action{}, fmt.Errorf("on_press %q must read set:<setting>=<value>, with a setting from %s (BINDS.md §4.8)", s, strings.Join(SetSettings(), ", "))
		}
		known := false
		for _, n := range SetSettings() {
			known = known || n == setting
		}
		if !known {
			return Action{}, fmt.Errorf("on_press %q sets %q, which a set: action cannot change; the settings are %s (BINDS.md §4.8)", s, setting, strings.Join(SetSettings(), ", "))
		}
		if len(value) > MaxSetValue || !setValueRe.MatchString(value) {
			return Action{}, fmt.Errorf("on_press %q gives the value %q, which is not a plain lowercase word of at most %d characters (BINDS.md §4.8)", s, value, MaxSetValue)
		}
		return Action{Kind: ActionSet, Setting: setting, Arg: value}, nil
	case "do":
		if err := verb.Check(arg); err != nil {
			return Action{}, fmt.Errorf("on_press %q: %w (BINDS.md §4.8)", s, err)
		}
		return Action{Kind: ActionDo, Arg: arg}, nil
	default:
		return Action{}, fmt.Errorf("on_press %q uses an unknown action prefix %q; the closed set is cmd:, focus:, answer:, ext:, say:, open:, set:, do: (BINDS.md §4.8)", s, prefix)
	}
}

// interpolationTokens returns the {row.<field>} references inside an on_press
// argument, so the validator can check each against the enclosing template's row
// schema exactly as it checks a bare row.* bind (§4.7 / Q20). A `{...}` token
// that is not a row.* reference is left for the host to resolve and is not a
// scene-schema concern, so only the row.* ones are returned.
func interpolationTokens(arg string) []string {
	var out []string
	rest := arg
	for {
		open := strings.IndexByte(rest, '{')
		if open < 0 {
			return out
		}
		rest = rest[open+1:]
		close := strings.IndexByte(rest, '}')
		if close < 0 {
			return out
		}
		token := rest[:close]
		if strings.HasPrefix(token, "row.") {
			out = append(out, token)
		}
		rest = rest[close+1:]
	}
}

// ExpandRowInterpolation substitutes the {row.<field>} tokens in an on_press
// argument with the values of the row scope the press happened in. It is the
// press-time half of the {row.<field>} interpolation validateOnPress checks at
// load (Q20): the extraction (interpolationTokens) and the schema check run at
// load, and this is the substitution E4/H8 parked. A template's on_press is
// stored verbatim -- Action.Arg keeps the braces -- because the element it was
// instantiated for is only known when a specific row is pressed, so the fields
// are resolved here against that row rather than when the action was parsed.
//
// row is keyed by the full token, "row.<field>" -> value, the same keying
// resolveBindRow uses for a bare row.* bind, so the display side and the press
// side read one row scope and cannot disagree on what "row.role" resolves to.
//
// An absent row.* field is an error, not an empty substitution. validateOnPress
// has already refused any {row.<field>} the element schema does not declare, and
// the scope is built from that same schema, so a token missing at press time is
// a scope/schema drift inside the host, not an operator omission. This project
// holds a wrong frame -- a run addressed by cmd:/agent whose id was silently
// blanked to the empty string, hitting a different run or none -- to be worse
// than a loud refusal, so the missing field is named rather than expanded to "".
// A {...} token that is not a row.* reference is left verbatim, exactly as
// interpolationTokens ignores it: those belong to a later host resolver, and
// consuming them here would strip a string the author wrote for that resolver.
func ExpandRowInterpolation(arg string, row map[string]string) (string, error) {
	var b strings.Builder
	rest := arg
	for {
		open := strings.IndexByte(rest, '{')
		if open < 0 {
			b.WriteString(rest)
			return b.String(), nil
		}
		b.WriteString(rest[:open])
		rest = rest[open+1:]
		close := strings.IndexByte(rest, '}')
		if close < 0 {
			// An unterminated '{' is not an interpolation token: interpolationTokens
			// stops at it and treats the tail as literal, so this pass writes the
			// brace and the remainder back verbatim rather than inventing a token.
			b.WriteByte('{')
			b.WriteString(rest)
			return b.String(), nil
		}
		token := rest[:close]
		if strings.HasPrefix(token, "row.") {
			v, ok := row[token]
			if !ok {
				return "", fmt.Errorf("on_press interpolates {%s}, but the row scope has no such field; a {row.<field>} validateOnPress accepted must be present in the scope built from the same element schema, so its absence is a scope/schema drift and expanding it to the empty string would address the wrong run (BINDS.md §4.7)", token)
			}
			b.WriteString(v)
		} else {
			// Not a row.* reference: interpolationTokens leaves these for a later
			// host resolver, so this pass must write the literal {token} back rather
			// than consume it -- the string that resolver sees must be the one the
			// author wrote, not one this substitution silently edited.
			b.WriteByte('{')
			b.WriteString(token)
			b.WriteByte('}')
		}
		rest = rest[close+1:]
	}
}

// Row references in drawn text (D1, option b). A node inside a row_template may write
// {row.<field>} in its text, title or placeholder and it is replaced, per row, by that
// row's value, the same vocabulary and the same schema check as on_press (BINDS.md §4.7).
//
// To show the characters themselves, double them: {{row.id}} draws {row.id}. Only that
// prefix is special, so ordinary braces in text are untouched.

// rowTextRefs walks a drawn string and reports the {row.<field>} tokens in it (without
// braces) and whether a reference was opened and never closed. A {{row.…}} escape is
// not a reference.
func rowTextRefs(s string) (tokens []string, unclosed bool) {
	for i := 0; i < len(s); {
		switch {
		case strings.HasPrefix(s[i:], "{{row."):
			// Escaped: skip to its closing "}}" if it has one, else past the prefix.
			if end := strings.Index(s[i+2:], "}}"); end >= 0 {
				i += 2 + end + 2
			} else {
				i += len("{{row.")
			}
		case strings.HasPrefix(s[i:], "{row."):
			end := strings.IndexByte(s[i:], '}')
			if end < 0 {
				return tokens, true
			}
			tokens = append(tokens, s[i+1:i+end])
			i += end + 1
		default:
			i++
		}
	}
	return tokens, false
}

// ExpandRowText replaces each {row.<field>} in a drawn string with the row's value and
// each {{row.<field>}} with the literal {row.<field>}. A field the row does not have is
// left as written, visibly: the validator has already refused it at load, so reaching it
// means the scope drifted from the schema, and a blank would hide that.
func ExpandRowText(s string, row map[string]string) string {
	if !strings.Contains(s, "{") {
		return s
	}
	var b strings.Builder
	for i := 0; i < len(s); {
		switch {
		case strings.HasPrefix(s[i:], "{{row."):
			if end := strings.Index(s[i+2:], "}}"); end >= 0 {
				b.WriteString("{" + s[i+2:i+2+end] + "}")
				i += 2 + end + 2
			} else {
				b.WriteString("{row.")
				i += len("{{row.")
			}
		case strings.HasPrefix(s[i:], "{row."):
			end := strings.IndexByte(s[i:], '}')
			if end < 0 {
				b.WriteString(s[i:])
				return b.String()
			}
			token := s[i+1 : i+end]
			if v, ok := row[token]; ok {
				b.WriteString(v)
			} else {
				b.WriteString(s[i : i+end+1])
			}
			i += end + 1
		default:
			b.WriteByte(s[i])
			i++
		}
	}
	return b.String()
}
