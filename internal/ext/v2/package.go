// Package extv2 is the light extension layer (docs/DESIGN-EXTENSIBILITY.md).
//
// A package is one JSON document: data, never code. Parsing and validating it runs
// nothing and touches no network. What a package asks the host to do is a list of
// operations (builtin, http, exec) behind a closed permission set the user sees
// before anything runs; what a rule can do to a line the user typed is limited by
// the narrowing rule in pipeline.go.
//
// The package is pure on purpose. It imports no UI, no driver and no scene, so the
// same validator serves the loader, the /ui patch surface and the tests, and a
// refusal reads the same from every door.
package extv2

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"unicode/utf8"
)

// Protocol is the only wire version this package reads. The set is closed: an
// unknown protocol is refused at load, never negotiated.
const Protocol = "ext/v2"

// Limits bound what one document may ask the host to hold. They sit far above what a
// person writes by hand and far below what would make the menu or the loop unusable.
const (
	MaxDocBytes = 64 * 1024
	MaxCommands = 32
	MaxTools    = 16
	MaxEvents   = 16
	MaxPanels   = 8
	MaxServices = 4
	// MaxPanelTitle and MaxPanelRunes bound what a panel puts on the one line it owns.
	MaxPanelTitle = 24
	MaxPanelRunes = 120
	// Panel refresh bounds, in seconds: a faster panel would be a loop in disguise.
	MinPanelEveryS = 30
	MaxPanelEveryS = 3600
	// MaxToolNameTotal bounds the id and the name together: the model sees the tool as
	// pkg_<id>_<name> (5 more characters), and providers refuse a tool name over 64.
	MaxToolNameTotal = 59
	MaxRules         = 16
	MaxPermEntries   = 32
	MaxNameLen       = 48
	MaxDescLen       = 200
	MaxArgvElements  = 32
	MaxArgvElemLen   = 1024
	MaxURLLen        = 2048
	MaxHeaders       = 16
	MaxBodyLen       = 8 * 1024
	MaxWhenLen       = 200
)

var (
	idRe      = regexp.MustCompile(`^[a-z][a-z0-9-]{0,62}$`)
	cmdNameRe = regexp.MustCompile(`^[a-z][a-z0-9-]*$`)
	ruleIDRe  = regexp.MustCompile(`^[a-z][a-z0-9._-]*$`)
	builtinRe = regexp.MustCompile(`^[a-z][a-z0-9._-]*$`)
	hostRe    = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?(\.[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?)+$`)
	execRe    = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._+-]*$`)
)

// Operation kinds. The set is closed: a new capability is a new kind (or a new field
// on one) plus a new permission, never a different package shape.
const (
	KindBuiltin = "builtin"
	KindHTTP    = "http"
	KindExec    = "exec"
	// KindService is reserved for long-lived operations (voice, watchers). It is
	// named here so a package written for it fails with an explanation instead of
	// "unknown kind".
	KindService = "service"
)

// Stages a rule may attach to.
const StageInputPre = "input.pre"

// Operation results: how an operation's output becomes an outcome.
const (
	ResultSay       = "say"       // the output is shown to the user, never sent
	ResultSend      = "send"      // the output is sent to the model (needs permissions.send)
	ResultTransform = "transform" // the output replaces the line (rules only)
	ResultOutcome   = "outcome"   // the output is a JSON outcome
)

// Package is a parsed, not yet validated, extension package.
type Package struct {
	Protocol    string      `json:"protocol"`
	ID          string      `json:"id"`
	Name        string      `json:"name"`
	Version     string      `json:"version"`
	Disabled    bool        `json:"disabled,omitempty"`
	Permissions Permissions `json:"permissions,omitempty"`
	Commands    []Command   `json:"commands,omitempty"`
	Rules       []Rule      `json:"rules,omitempty"`
	Tools       []Tool      `json:"tools,omitempty"`
	Events      []Event     `json:"events,omitempty"`
	Panels      []Panel     `json:"panels,omitempty"`
	Services    []Service   `json:"services,omitempty"`
}

// Permissions is everything a package may reach. Empty means nothing: a package
// that declares none can only run builtins and show text.
type Permissions struct {
	// Net lists the hosts an http operation may call: "api.example.com" (port
	// 443), "api.example.com:8443", or "local:127.0.0.1:8080" for a service on
	// this machine or network (spelled out so the consent screen can say so).
	Net []string `json:"net,omitempty"`
	// Exec lists the bare program names an exec operation may start from the
	// system. A program inside the package needs no entry: it is part of what the
	// digest covers.
	Exec []string `json:"exec,omitempty"`
	// Open lists the https hosts the open: action may send the user's browser to.
	Open []string `json:"open,omitempty"`
	// Send lets a command send text to the model.
	Send bool `json:"send,omitempty"`
}

// Command is an entry of the / menu backed by one operation.
type Command struct {
	Name        string `json:"name"`
	Category    string `json:"category,omitempty"`
	Description string `json:"description,omitempty"`
	Usage       string `json:"usage,omitempty"`
	// MinArgs is how many words the command needs; fewer shows Usage and runs
	// nothing, so a missing argument is never silently an empty string.
	MinArgs  int  `json:"min_args,omitempty"`
	Disabled bool `json:"disabled,omitempty"`
	Op       Op   `json:"op"`
}

// Tool is an operation the agent may call by itself. It is a command the model types
// instead of the user: the same operation, the same guards and the same permissions, with
// the one difference that the model chooses the text it is given and reads what comes
// back. Because of that the user approves the package knowing it holds tools (the consent
// text says so), and what an operation returns is handed to the model as data.
type Tool struct {
	Name string `json:"name"`
	// Description tells the model when to use the tool. It is required: a tool the
	// model cannot tell the purpose of is one it will call at random.
	Description string `json:"description"`
	// Usage says what to pass as args ("a city name"). It is shown to the model.
	Usage string `json:"usage,omitempty"`
	// MinArgs is how many words the model must pass; fewer is refused before anything runs.
	MinArgs  int  `json:"min_args,omitempty"`
	Disabled bool `json:"disabled,omitempty"`
	Op       Op   `json:"op"`
}

// ToolPrefix starts the name the model sees; the host recognises its own tools by it.
const ToolPrefix = "pkg_"

// ModelName is the name the model calls the tool by: pkg_<package id>_<tool name>. Neither
// part holds an underscore, so the name says unambiguously which package it belongs to.
func (t Tool) ModelName(pkgID string) string { return ToolPrefix + pkgID + "_" + t.Name }

// The events a package may react to. The first two end a turn of the chat, the next three
// are a turn starting, the user cancelling one, and a background shell ending by itself; the others
// are the settings the user can change, the same ones a hook of the behaviour layer can
// watch (the host knows each one's values).
const (
	EventTurnDone      = "turn.done"
	EventTurnFailed    = "turn.failed"
	EventTurnStarted   = "turn.started"
	EventTurnCancelled = "turn.cancelled"
	EventShellEnded    = "shell.ended"
	EventEffort        = "effort"
	EventMode          = "mode"
	EventStyle         = "style"
)

// EventKinds lists what Event.On may be.
var EventKinds = []string{EventTurnDone, EventTurnFailed, EventTurnStarted, EventTurnCancelled, EventShellEnded, EventEffort, EventMode, EventStyle}

// IsSettingEvent reports whether an event is a change of a setting, which is the only
// kind that carries a value.
func IsSettingEvent(on string) bool {
	return on == EventEffort || on == EventMode || on == EventStyle
}

// Event is a reaction to something that happened in the interface. It runs one
// operation and shows what it said as a note. That is all it can do: it never sees a
// line the user typed, never sends to the model, and never starts an action, so an event
// cannot start another event or change the setting it watches.
type Event struct {
	ID string `json:"id"`
	On string `json:"on"`
	// Is narrows a setting event to one value ("max"); empty reacts to every change.
	Is       string `json:"is,omitempty"`
	Disabled bool   `json:"disabled,omitempty"`
	Op       Op     `json:"op"`
}

// Panel is one line the package keeps on the screen. Its operation runs when the package
// loads, again after each turn when Refresh is "turn.done", and every EveryS seconds when
// that is set. A panel with no operation is filled by a service (Service.Feed).
type Panel struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	// Refresh is "" (only when the package loads) or "turn.done".
	Refresh  string `json:"refresh,omitempty"`
	EveryS   int    `json:"every_s,omitempty"`
	Disabled bool   `json:"disabled,omitempty"`
	Op       *Op    `json:"op,omitempty"`
}

// Service is a program that keeps running until the user stops it. It is started by the
// user typing /service start, never by the package or by a key, button or hook, and each
// line it prints becomes a note or, with Feed, the text of one of the package's panels.
type Service struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	// Feed names a panel of this package that the output fills; empty shows each line
	// as a note.
	Feed     string `json:"feed,omitempty"`
	Disabled bool   `json:"disabled,omitempty"`
	Op       Op     `json:"op"`
}

// Rule is a reaction to a stage of the pipeline.
type Rule struct {
	ID       string `json:"id"`
	Stage    string `json:"stage"`
	When     *When  `json:"when,omitempty"`
	OnError  string `json:"on_error,omitempty"` // "continue" (default) or "block"
	Disabled bool   `json:"disabled,omitempty"`
	Op       Op     `json:"op"`
}

// When narrows the lines a rule looks at. Every field present must match.
type When struct {
	Prefix   string `json:"prefix,omitempty"`
	Contains string `json:"contains,omitempty"`
	Matches  string `json:"matches,omitempty"` // RE2, bounded
}

// Op is one thing to do. Which fields apply depends on Kind; the validator refuses a
// field that does not apply, so a document never carries a setting that is ignored.
type Op struct {
	Kind   string `json:"kind"`
	Result string `json:"result,omitempty"`
	// TimeoutMS bounds http and exec; 0 means the default.
	TimeoutMS int `json:"timeout_ms,omitempty"`

	// builtin
	Name string `json:"name,omitempty"`

	// http
	Method       string            `json:"method,omitempty"`
	URL          string            `json:"url,omitempty"`
	Headers      map[string]string `json:"headers,omitempty"`
	Body         string            `json:"body,omitempty"`
	BodyEncoding string            `json:"body_encoding,omitempty"` // json (default), form, text

	// exec
	Argv      []string `json:"argv,omitempty"`
	AllowDash bool     `json:"allow_dash,omitempty"`

	// Extract picks one value out of a JSON output (http or exec) by a dotted
	// path such as "current.temp" or "items.0.name", so a third-party API that
	// knows nothing of this contract can still back a command.
	Extract string `json:"extract,omitempty"`
}

// Parse reads a package from bytes. It is strict: an unknown key is an error, because
// a key that is silently ignored is a setting the author believes is in force.
func Parse(data []byte) (*Package, error) {
	if len(data) > MaxDocBytes {
		return nil, fmt.Errorf("package is %d bytes, the most is %d; split it into several packages", len(data), MaxDocBytes)
	}
	if !utf8.Valid(data) {
		return nil, fmt.Errorf("package is not valid UTF-8; save it as UTF-8 JSON")
	}
	var top map[string]json.RawMessage
	first := json.NewDecoder(bytes.NewReader(data))
	if err := first.Decode(&top); err != nil {
		return nil, fmt.Errorf("package is not a JSON object: %v; write one object with protocol, id, name and version", err)
	}
	if first.More() {
		return nil, fmt.Errorf("package has more than one JSON value, so the second would be ignored; keep exactly one object")
	}
	var p Package
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&p); err != nil {
		return nil, fmt.Errorf("package is malformed: %v; check the field names against docs/DESIGN-EXTENSIBILITY.md", err)
	}
	return &p, nil
}

// Options carry what only the host knows when a package is validated.
type Options struct {
	// ReservedCommands are names a package command may not take (the factory
	// commands and the commands of other packages).
	ReservedCommands func(name string) bool
	// Builtins reports whether the host registered a builtin by that name; nil
	// skips the check (the pure validator does not know the host).
	Builtins func(name string) bool
	// EventValues lists the values a setting event may be narrowed to ("effort" ->
	// minimal, low, ...); nil skips the check, because the pure validator does not know
	// the host's settings.
	EventValues func(on string) ([]string, bool)
}

// Validate refuses a package the host could not honour. Every message says what is
// wrong, what it would have caused, and what to write instead, because the reader is a
// model repairing its own draft.
func (p *Package) Validate(opt Options) error {
	if p == nil {
		return fmt.Errorf("package is missing")
	}
	if p.Protocol != Protocol {
		return fmt.Errorf("protocol %q is not supported, so the package would not load; write \"protocol\": %q", p.Protocol, Protocol)
	}
	if !idRe.MatchString(p.ID) {
		return fmt.Errorf("id %q is not a legal identifier; it names the package and keys its consent, so use lowercase letters, digits and '-', starting with a letter", p.ID)
	}
	if err := checkLabel("name", p.Name, MaxNameLen, true); err != nil {
		return err
	}
	if err := checkLabel("version", p.Version, 32, true); err != nil {
		return err
	}
	if err := p.Permissions.validate(); err != nil {
		return err
	}
	if len(p.Commands) > MaxCommands {
		return fmt.Errorf("commands: %d defined, the most is %d; remove some or split the package", len(p.Commands), MaxCommands)
	}
	if len(p.Rules) > MaxRules {
		return fmt.Errorf("rules: %d defined, the most is %d; remove some or split the package", len(p.Rules), MaxRules)
	}
	if len(p.Tools) > MaxTools {
		return fmt.Errorf("tools: %d defined, the most is %d; remove some or split the package", len(p.Tools), MaxTools)
	}
	if len(p.Commands) == 0 && len(p.Rules) == 0 && len(p.Tools) == 0 && len(p.Events) == 0 && len(p.Panels) == 0 && len(p.Services) == 0 {
		return fmt.Errorf("package %q has no commands, rules, tools, events, panels or services, so it would do nothing; add one of each kind it needs", p.ID)
	}
	names := map[string]bool{}
	for i, c := range p.Commands {
		if err := p.validateCommand(i, c, opt); err != nil {
			return err
		}
		if names[c.Name] {
			return fmt.Errorf("commands: /%s is defined twice, so one of them could never run; keep one", c.Name)
		}
		names[c.Name] = true
	}
	ids := map[string]bool{}
	for i, r := range p.Rules {
		if err := p.validateRule(i, r, opt); err != nil {
			return err
		}
		if ids[r.ID] {
			return fmt.Errorf("rules: id %q is defined twice, so a patch could not tell them apart; give each rule its own id", r.ID)
		}
		ids[r.ID] = true
	}
	tools := map[string]bool{}
	for i, t := range p.Tools {
		if err := p.validateTool(i, t, opt); err != nil {
			return err
		}
		if tools[t.Name] {
			return fmt.Errorf("tools: %s is defined twice, so the model could not tell them apart; keep one", t.Name)
		}
		tools[t.Name] = true
	}
	return p.validateAutomatic(opt)
}

func (p *Package) validateTool(i int, t Tool, opt Options) error {
	who := fmt.Sprintf("tools[%d]", i)
	if len(t.Name) > MaxNameLen || !cmdNameRe.MatchString(t.Name) {
		return fmt.Errorf("%s: name %q is not usable; the model sees it as %s<id>_<name>, so use lowercase letters, digits and '-', starting with a letter", who, t.Name, ToolPrefix)
	}
	who = "tool " + t.Name
	if len(p.ID)+len(t.Name) > MaxToolNameTotal {
		return fmt.Errorf("%s: the package id and the tool name together are %d characters, the most is %d, because the model sees %s<id>_<name> and providers refuse a tool name over 64 characters; shorten one", who, len(p.ID)+len(t.Name), MaxToolNameTotal, ToolPrefix)
	}
	if err := checkLabel(who+" description", t.Description, MaxDescLen, true); err != nil {
		return err
	}
	if err := checkLabel(who+" usage", t.Usage, MaxDescLen, false); err != nil {
		return err
	}
	if t.MinArgs < 0 || t.MinArgs > 9 {
		return fmt.Errorf("%s: min_args %d is outside 0..9; the placeholders are ${1} to ${9}", who, t.MinArgs)
	}
	if t.MinArgs > 0 && t.Usage == "" {
		return fmt.Errorf("%s: min_args is %d but there is no usage, so the model would not know what to pass; add a \"usage\" such as \"a city name\"", who, t.MinArgs)
	}
	return p.validateOp(who, t.Op, opCtx{kind: ctxTool, minArgs: t.MinArgs}, opt)
}

func (p *Package) validateCommand(i int, c Command, opt Options) error {
	who := fmt.Sprintf("commands[%d]", i)
	if len(c.Name) > MaxNameLen || !cmdNameRe.MatchString(c.Name) {
		return fmt.Errorf("%s: name %q is not usable; it is typed as /name, so use lowercase letters, digits and '-', starting with a letter", who, c.Name)
	}
	who = "command /" + c.Name
	if opt.ReservedCommands != nil && opt.ReservedCommands(c.Name) {
		return fmt.Errorf("%s: /%s is already a command of the app, and a package cannot take its place; pick another name", who, c.Name)
	}
	for _, f := range []struct{ n, v string }{{"description", c.Description}, {"category", c.Category}, {"usage", c.Usage}} {
		if err := checkLabel(who+" "+f.n, f.v, MaxDescLen, false); err != nil {
			return err
		}
	}
	if c.MinArgs < 0 || c.MinArgs > 9 {
		return fmt.Errorf("%s: min_args %d is outside 0..9; the placeholders are {1} to {9}", who, c.MinArgs)
	}
	if c.MinArgs > 0 && c.Usage == "" {
		return fmt.Errorf("%s: min_args is %d but there is no usage, so a short line could only say \"needs more\"; add a \"usage\" such as \"/%s <text>\"", who, c.MinArgs, c.Name)
	}
	return p.validateOp(who, c.Op, opCtx{kind: ctxCommand, minArgs: c.MinArgs}, opt)
}

func (p *Package) validateRule(i int, r Rule, opt Options) error {
	who := fmt.Sprintf("rules[%d]", i)
	if len(r.ID) > MaxNameLen || !ruleIDRe.MatchString(r.ID) {
		return fmt.Errorf("%s: id %q is not usable; use lowercase letters, digits, '.', '-' or '_', starting with a letter", who, r.ID)
	}
	who = "rule " + r.ID
	if r.Stage != StageInputPre {
		return fmt.Errorf("%s: stage %q does not exist, so the rule would never run; the stages are %s", who, r.Stage, StageInputPre)
	}
	switch r.OnError {
	case "", "continue", "block":
	default:
		return fmt.Errorf("%s: on_error %q is not understood; use \"continue\" (the line goes on as typed) or \"block\" (the line is held back)", who, r.OnError)
	}
	if r.When != nil {
		if err := r.When.validate(who); err != nil {
			return err
		}
	}
	return p.validateOp(who, r.Op, opCtx{kind: ctxRule}, opt)
}

func (w *When) validate(who string) error {
	if w.Prefix == "" && w.Contains == "" && w.Matches == "" {
		return fmt.Errorf("%s: \"when\" is empty, so it would match every line; fill prefix, contains or matches, or leave \"when\" out on purpose", who)
	}
	for _, f := range []struct{ n, v string }{{"prefix", w.Prefix}, {"contains", w.Contains}, {"matches", w.Matches}} {
		if utf8.RuneCountInString(f.v) > MaxWhenLen {
			return fmt.Errorf("%s: when.%s is longer than %d characters", who, f.n, MaxWhenLen)
		}
	}
	if strings.HasPrefix(strings.TrimSpace(w.Prefix), "/") {
		return fmt.Errorf("%s: when.prefix starts with '/', but a rule never sees a / line (commands are resolved first), so it would never match; match plain text", who)
	}
	if w.Matches != "" {
		if _, err := regexp.Compile(w.Matches); err != nil {
			return fmt.Errorf("%s: when.matches is not a valid expression: %v; it uses RE2 syntax", who, err)
		}
	}
	return nil
}

// Match reports whether the rule looks at line. A nil When looks at every line.
func (w *When) Match(line string) bool {
	if w == nil {
		return true
	}
	if w.Prefix != "" && !strings.HasPrefix(line, w.Prefix) {
		return false
	}
	if w.Contains != "" && !strings.Contains(line, w.Contains) {
		return false
	}
	if w.Matches != "" {
		re, err := regexp.Compile(w.Matches)
		if err != nil || !re.MatchString(line) {
			return false
		}
	}
	return true
}

func (pm Permissions) validate() error {
	if len(pm.Net) > MaxPermEntries || len(pm.Exec) > MaxPermEntries || len(pm.Open) > MaxPermEntries {
		return fmt.Errorf("permissions: more than %d entries in one list; a package that needs that much should be split", MaxPermEntries)
	}
	seen := map[string]bool{}
	for _, e := range pm.Net {
		if _, err := parseNetPerm(e); err != nil {
			return fmt.Errorf("permissions.net %q: %w", e, err)
		}
		if seen[e] {
			return fmt.Errorf("permissions.net lists %q twice; keep one", e)
		}
		seen[e] = true
	}
	seen = map[string]bool{}
	for _, e := range pm.Exec {
		if !execRe.MatchString(e) {
			return fmt.Errorf("permissions.exec %q is not a bare program name, so it could name a path outside the package; write the name only, like \"git\"", e)
		}
		if seen[e] {
			return fmt.Errorf("permissions.exec lists %q twice; keep one", e)
		}
		seen[e] = true
	}
	seen = map[string]bool{}
	for _, e := range pm.Open {
		if !hostRe.MatchString(e) || looksLikeAddress(e) {
			return fmt.Errorf("permissions.open %q is not a host name; write the name only, like \"example.com\" (https, no path)", e)
		}
		if seen[e] {
			return fmt.Errorf("permissions.open lists %q twice; keep one", e)
		}
		seen[e] = true
	}
	return nil
}

// netPerm is a parsed permissions.net entry.
type netPerm struct {
	host  string
	port  string // "443" by default
	local bool
}

func (n netPerm) hostport() string { return n.host + ":" + n.port }

func parseNetPerm(e string) (netPerm, error) {
	var n netPerm
	rest := e
	if strings.HasPrefix(rest, "local:") {
		n.local = true
		rest = strings.TrimPrefix(rest, "local:")
	}
	host, port := rest, "443"
	if i := strings.LastIndexByte(rest, ':'); i >= 0 && !strings.HasSuffix(rest, "]") {
		host, port = rest[:i], rest[i+1:]
	}
	if n.local {
		if !strings.Contains(rest, ":") {
			return n, fmt.Errorf("a local entry names its port, so another service on that machine is not reachable by accident; write local:<host>:<port>")
		}
		if host == "" || strings.ContainsAny(host, "/@?#[] ") {
			return n, fmt.Errorf("the host of a local entry is a name or address without brackets, path or credentials, like local:127.0.0.1:8080")
		}
	} else if !hostRe.MatchString(host) || looksLikeAddress(host) {
		return n, fmt.Errorf("it is not a host name; write the name only, like \"api.example.com\" (a bare address needs the local: form, and a wildcard is never allowed)")
	}
	if !portOK(port) {
		return n, fmt.Errorf("the port %q is not a number from 1 to 65535", port)
	}
	n.host = strings.ToLower(host)
	n.port = port
	return n, nil
}

func portOK(s string) bool {
	if s == "" || len(s) > 5 {
		return false
	}
	v := 0
	for _, c := range s {
		if c < '0' || c > '9' {
			return false
		}
		v = v*10 + int(c-'0')
	}
	return v >= 1 && v <= 65535
}

// checkLabel refuses text a terminal would act on instead of showing.
func checkLabel(what, s string, max int, required bool) error {
	if required && strings.TrimSpace(s) == "" {
		return fmt.Errorf("%s is empty; it is shown to the user, so write one", what)
	}
	if !utf8.ValidString(s) {
		return fmt.Errorf("%s is not valid UTF-8", what)
	}
	if utf8.RuneCountInString(s) > max {
		return fmt.Errorf("%s is longer than %d characters; shorten it", what, max)
	}
	for _, r := range s {
		if r < 0x20 || r == 0x7f || (r >= 0x80 && r < 0xa0) || isBidi(r) {
			return fmt.Errorf("%s contains the control character U+%04X; the terminal would act on it instead of showing it", what, r)
		}
	}
	return nil
}

func isBidi(r rune) bool {
	return (r >= 0x202A && r <= 0x202E) || (r >= 0x2066 && r <= 0x2069) ||
		r == 0x200E || r == 0x200F || r == 0x061C
}

// Digest is a hash of the package's meaning, independent of how the file is
// formatted. A consent grant is bound to it, so editing anything a grant covers asks
// again.
func (p *Package) Digest() string {
	b, err := json.Marshal(p)
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// Encode writes the package as indented JSON, so the file a person opens reads the
// same every time and a diff shows only what changed.
func (p *Package) Encode() []byte {
	out, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return []byte("{}\n")
	}
	return append(out, '\n')
}

// Capabilities lists, in plain words, what the package can do. It is what a consent
// screen shows, so every power a validated package can use has a line here.
func (p *Package) Capabilities() []string {
	var out []string
	for _, e := range p.Permissions.Net {
		if n, err := parseNetPerm(e); err == nil {
			if n.local {
				out = append(out, "reach "+n.hostport()+" on this machine or network")
			} else {
				out = append(out, "send requests to "+n.hostport())
			}
		}
	}
	for _, e := range p.Permissions.Exec {
		out = append(out, "run the program "+e+" from your system")
	}
	if p.hasPackageExec() {
		out = append(out, "run programs shipped inside the package")
	}
	for _, e := range p.Permissions.Open {
		out = append(out, "open "+e+" in your browser")
	}
	if p.Permissions.Send {
		out = append(out, "send text to the model")
	}
	if len(p.Rules) > 0 {
		out = append(out, "read and rewrite every line you type before it is sent")
	}
	if len(p.Tools) > 0 {
		var names []string
		for _, t := range p.Tools {
			names = append(names, t.Name)
		}
		out = append(out, "let the model call "+strings.Join(names, ", ")+" by itself, with text it chooses, and read what they return")
	}
	if len(p.Events) > 0 {
		var on []string
		seen := map[string]bool{}
		for _, e := range p.Events {
			if !seen[e.On] {
				seen[e.On] = true
				on = append(on, e.On)
			}
		}
		out = append(out, "react by itself, with nothing typed, when "+strings.Join(on, ", ")+" happens, and show you a note")
	}
	if len(p.Panels) > 0 {
		var titles []string
		for _, pn := range p.Panels {
			titles = append(titles, pn.Title)
		}
		out = append(out, "keep a line on your screen ("+strings.Join(titles, ", ")+") and refresh it by itself")
	}
	if len(p.Services) > 0 {
		var names []string
		for _, sv := range p.Services {
			names = append(names, sv.Name)
		}
		out = append(out, "run a program that keeps running after you start it with /service start ("+strings.Join(names, ", ")+") until you stop it, and show what it prints")
	}
	return out
}

func (p *Package) hasPackageExec() bool {
	each := func(o Op) bool {
		return o.Kind == KindExec && len(o.Argv) > 0 && isPackagePath(o.Argv[0])
	}
	for _, c := range p.Commands {
		if each(c.Op) {
			return true
		}
	}
	for _, r := range p.Rules {
		if each(r.Op) {
			return true
		}
	}
	for _, t := range p.Tools {
		if each(t.Op) {
			return true
		}
	}
	for _, e := range p.Events {
		if each(e.Op) {
			return true
		}
	}
	for _, pn := range p.Panels {
		if pn.Op != nil && each(*pn.Op) {
			return true
		}
	}
	for _, sv := range p.Services {
		if each(sv.Op) {
			return true
		}
	}
	return false
}
