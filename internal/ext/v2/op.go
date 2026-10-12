package extv2

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
)

type ctxKind int

const (
	ctxCommand ctxKind = iota
	ctxRule
	ctxTool
)

// opCtx is where an operation sits, which decides which placeholders and results it
// may use.
type opCtx struct {
	kind    ctxKind
	minArgs int
}

// Timeouts, in milliseconds. A rule runs on every line the user types, so it gets far
// less time than a command the user asked for by name.
const (
	DefaultCommandTimeoutMS = 5000
	MaxCommandTimeoutMS     = 15000
	DefaultRuleTimeoutMS    = 1500
	MaxRuleTimeoutMS        = 2000
)

var (
	headerNameRe = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9-]{0,63}$`)
	// forbiddenHeaders are the ones the transport owns: letting a package set them is
	// how a request gets smuggled or its target changed.
	forbiddenHeaders = map[string]bool{
		"host": true, "content-length": true, "transfer-encoding": true, "connection": true,
		"upgrade": true, "te": true, "trailer": true, "proxy-authorization": true,
		"proxy-connection": true, "expect": true, "keep-alive": true,
	}
	extractRe = regexp.MustCompile(`^[A-Za-z0-9_-]+(\.[A-Za-z0-9_-]+)*$`)
)

func (p *Package) validateOp(who string, o Op, oc opCtx, opt Options) error {
	if o.TimeoutMS < 0 {
		return fmt.Errorf("%s: timeout_ms %d is negative; leave it out for the default", who, o.TimeoutMS)
	}
	max := MaxCommandTimeoutMS
	if oc.kind == ctxRule {
		max = MaxRuleTimeoutMS
	}
	if o.TimeoutMS > max {
		return fmt.Errorf("%s: timeout_ms %d is more than the %d allowed here; a slower operation holds the interface up", who, o.TimeoutMS, max)
	}
	switch o.Kind {
	case KindBuiltin:
		return p.validateBuiltin(who, o, oc, opt)
	case KindHTTP:
		return p.validateHTTP(who, o, oc)
	case KindExec:
		return p.validateExec(who, o, oc)
	case KindService:
		return fmt.Errorf("%s: kind \"service\" is reserved for long-lived operations such as voice and is not available yet; use \"http\" or \"exec\" for a single request", who)
	case "":
		return fmt.Errorf("%s: op has no kind, so nothing would run; the kinds are %s, %s and %s", who, KindBuiltin, KindHTTP, KindExec)
	default:
		return fmt.Errorf("%s: op kind %q does not exist, so nothing would run; the kinds are %s, %s and %s", who, o.Kind, KindBuiltin, KindHTTP, KindExec)
	}
}

// refuseFields names the fields of other kinds that were set, so a document never
// carries a setting that is ignored.
func refuseFields(who string, o Op, kind string) error {
	var stray []string
	add := func(set bool, name string) {
		if set {
			stray = append(stray, name)
		}
	}
	if kind != KindBuiltin {
		add(o.Name != "", "name")
	}
	if kind != KindHTTP {
		add(o.Method != "", "method")
		add(o.URL != "", "url")
		add(len(o.Headers) > 0, "headers")
		add(o.Body != "", "body")
		add(o.BodyEncoding != "", "body_encoding")
	}
	if kind != KindExec {
		add(len(o.Argv) > 0, "argv")
		add(o.AllowDash, "allow_dash")
	}
	if kind == KindBuiltin {
		add(o.Result != "", "result")
		add(o.Extract != "", "extract")
		add(o.TimeoutMS != 0, "timeout_ms")
	}
	if len(stray) > 0 {
		return fmt.Errorf("%s: a %q operation does not use %s, and a setting that is ignored is one the author thinks is in force; remove %s", who, kind, strings.Join(stray, ", "), strings.Join(stray, ", "))
	}
	return nil
}

func (p *Package) validateResult(who string, o Op, oc opCtx) error {
	allowed := []string{ResultSay, ResultSend, ResultOutcome}
	def := ResultSay
	switch oc.kind {
	case ctxRule:
		allowed = []string{ResultTransform, ResultOutcome}
		def = ResultTransform
	case ctxTool:
		// What a tool returns goes back to the model that called it, and nowhere else:
		// there is no line to rewrite and no second channel to send on.
		allowed = []string{ResultSay}
	}
	r := o.Result
	if r == "" {
		r = def
	}
	ok := false
	for _, a := range allowed {
		if a == r {
			ok = true
		}
	}
	if !ok {
		return fmt.Errorf("%s: result %q is not available here; use one of %s", who, r, strings.Join(allowed, ", "))
	}
	if r == ResultSend && !p.Permissions.Send {
		return fmt.Errorf("%s: result \"send\" sends text to the model, and the package has not asked for that; add \"send\": true to permissions so the user is told, or use \"say\"", who)
	}
	if r == ResultOutcome && o.Extract != "" {
		return fmt.Errorf("%s: extract and result \"outcome\" both read the output, and only one can; drop extract, or pick another result", who)
	}
	if o.Extract != "" && (len(o.Extract) > 200 || !extractRe.MatchString(o.Extract)) {
		return fmt.Errorf("%s: extract %q is not a dotted path; write names and list positions joined by dots, like \"current.temp\" or \"items.0.name\"", who, o.Extract)
	}
	return nil
}

func (p *Package) validateBuiltin(who string, o Op, oc opCtx, opt Options) error {
	if err := refuseFields(who, o, KindBuiltin); err != nil {
		return err
	}
	if o.Name == "" || len(o.Name) > MaxNameLen || !builtinRe.MatchString(o.Name) {
		return fmt.Errorf("%s: a builtin names the host function it calls, like \"effort.set\"; %q is not a name", who, o.Name)
	}
	if opt.Builtins != nil && !opt.Builtins(o.Name) {
		return fmt.Errorf("%s: this app has no builtin %q, so the operation could never run; check the name against the list in docs/DESIGN-EXTENSIBILITY.md", who, o.Name)
	}
	return nil
}

func (p *Package) validateHTTP(who string, o Op, oc opCtx) error {
	if err := refuseFields(who, o, KindHTTP); err != nil {
		return err
	}
	if err := p.validateResult(who, o, oc); err != nil {
		return err
	}
	method := o.Method
	if method == "" {
		method = "GET"
	}
	if method != "GET" && method != "POST" {
		return fmt.Errorf("%s: method %q is not allowed; a package may GET or POST, because the other methods change things on the far side without the user having seen a form", who, method)
	}
	if o.URL == "" {
		return fmt.Errorf("%s: an http operation needs a url", who)
	}
	if len(o.URL) > MaxURLLen {
		return fmt.Errorf("%s: url is longer than %d characters", who, MaxURLLen)
	}
	parts, err := parseTemplate(o.URL)
	if err != nil {
		return fmt.Errorf("%s: url: %w", who, err)
	}
	if err := checkPlaceholders(who+" url", parts, oc); err != nil {
		return err
	}
	if _, err := p.checkURLTarget(who, parts); err != nil {
		return err
	}
	if len(o.Headers) > MaxHeaders {
		return fmt.Errorf("%s: %d headers, the most is %d", who, len(o.Headers), MaxHeaders)
	}
	for k, v := range o.Headers {
		if !headerNameRe.MatchString(k) {
			return fmt.Errorf("%s: header name %q is not usable; use letters, digits and '-'", who, k)
		}
		if forbiddenHeaders[strings.ToLower(k)] {
			return fmt.Errorf("%s: header %q belongs to the transport, and letting a package set it is how a request is redirected or smuggled; remove it", who, k)
		}
		if err := checkHeaderValue(v); err != nil {
			return fmt.Errorf("%s: header %q: %w", who, k, err)
		}
		if strings.Contains(v, "${") {
			return fmt.Errorf("%s: header %q uses a placeholder; a value typed by the user must not decide a header, so write it literally", who, k)
		}
	}
	if o.Body != "" && method != "POST" {
		return fmt.Errorf("%s: a body is only sent with POST; set \"method\": \"POST\", or remove the body", who)
	}
	enc := o.BodyEncoding
	if enc == "" {
		enc = "json"
	}
	if o.Body == "" && o.BodyEncoding != "" {
		return fmt.Errorf("%s: body_encoding is set but there is no body", who)
	}
	if enc != "json" && enc != "form" && enc != "text" {
		return fmt.Errorf("%s: body_encoding %q is not understood; use json, form or text", who, enc)
	}
	if o.Body != "" {
		if len(o.Body) > MaxBodyLen {
			return fmt.Errorf("%s: body is longer than %d bytes", who, MaxBodyLen)
		}
		bp, err := parseTemplate(o.Body)
		if err != nil {
			return fmt.Errorf("%s: body: %w", who, err)
		}
		if err := checkPlaceholders(who+" body", bp, oc); err != nil {
			return err
		}
		if enc == "json" {
			if err := jsonStringsOnly(who, bp); err != nil {
				return err
			}
			probe := expand(bp, Input{Args: "x", Words: []string{"x", "x", "x", "x", "x", "x", "x", "x", "x"}, Line: "x"}, jsonEscape)
			if !json.Valid([]byte(probe)) {
				return fmt.Errorf("%s: body is not valid JSON, so the far side would refuse it; check the braces and quotes", who)
			}
		}
	}
	return nil
}

func checkHeaderValue(v string) error {
	if len(v) > 512 {
		return fmt.Errorf("value is longer than 512 bytes")
	}
	for _, r := range v {
		if r < 0x20 && r != '\t' || r == 0x7f {
			return fmt.Errorf("value holds a control character, which would split the request; remove it")
		}
	}
	return nil
}

// urlTarget is what the static part of a url template says about where it goes.
type urlTarget struct {
	scheme string
	perm   netPerm
}

// checkURLTarget validates the literal start of the url: scheme, no credentials, a
// host that no placeholder can touch, and a permission that names it.
func (p *Package) checkURLTarget(who string, parts []tmplPart) (urlTarget, error) {
	var t urlTarget
	if len(parts) == 0 || parts[0].ph != "" {
		return t, fmt.Errorf("%s: the url must begin with its scheme and host written out, so a typed value can never choose where the request goes", who)
	}
	lit := parts[0].lit
	scheme, rest, ok := strings.Cut(lit, "://")
	if !ok {
		return t, fmt.Errorf("%s: url %q has no scheme; start it with https://", who, lit)
	}
	end := strings.IndexAny(rest, "/?#")
	if end < 0 {
		if rest == "" && len(parts) > 1 {
			return t, fmt.Errorf("%s: the url must begin with its scheme and host written out, so a typed value can never choose where the request goes", who)
		}
		if len(parts) > 1 {
			return t, fmt.Errorf("%s: a placeholder follows the host directly, so a typed value could extend it; put a '/' after the host first", who)
		}
		end = len(rest)
	}
	authority := rest[:end]
	if strings.ContainsAny(authority, "@\\ ") || authority == "" {
		return t, fmt.Errorf("%s: the host part %q holds credentials or spaces, or is empty; write only the host, like https://api.example.com/path", who, authority)
	}
	if strings.Contains(authority, "[") {
		return t, fmt.Errorf("%s: a bracketed address is not accepted; use a host name", who)
	}
	host, port := authority, ""
	if i := strings.LastIndexByte(authority, ':'); i >= 0 {
		host, port = authority[:i], authority[i+1:]
	}
	host = strings.ToLower(host)
	switch scheme {
	case "https":
		if port == "" {
			port = "443"
		}
	case "http":
		if port == "" {
			port = "80"
		}
	default:
		return t, fmt.Errorf("%s: scheme %q is not allowed; use https (http only for a local: permission)", who, scheme)
	}
	if !portOK(port) {
		return t, fmt.Errorf("%s: port %q is not a number from 1 to 65535", who, port)
	}
	for _, e := range p.Permissions.Net {
		np, err := parseNetPerm(e)
		if err != nil || np.host != host || np.port != port {
			continue
		}
		if scheme == "http" && !np.local {
			break
		}
		t.scheme, t.perm = scheme, np
		return t, nil
	}
	if scheme == "http" {
		return t, fmt.Errorf("%s: http://%s would send the request in the clear; use https, or declare the host as \"local:%s:%s\" in permissions.net if it really is on this machine", who, authority, host, port)
	}
	return t, fmt.Errorf("%s: %s:%s is not in permissions.net, so the request would be refused when it ran; add %q to permissions.net so the user is told before it can be reached", who, host, port, permSpelling(host, port, scheme))
}

func permSpelling(host, port, scheme string) string {
	if scheme == "https" && port == "443" {
		return host
	}
	return host + ":" + port
}

func (p *Package) validateExec(who string, o Op, oc opCtx) error {
	if err := refuseFields(who, o, KindExec); err != nil {
		return err
	}
	if err := p.validateResult(who, o, oc); err != nil {
		return err
	}
	if len(o.Argv) == 0 {
		return fmt.Errorf("%s: argv is empty, so there is nothing to run; the first element is the program", who)
	}
	if len(o.Argv) > MaxArgvElements {
		return fmt.Errorf("%s: argv has %d elements, the most is %d", who, len(o.Argv), MaxArgvElements)
	}
	prog := o.Argv[0]
	if strings.Contains(prog, "$") {
		return fmt.Errorf("%s: the program (argv[0]) uses a placeholder, so typed text could choose what runs; write the program out", who)
	}
	if isPackagePath(prog) {
		// inside the package: covered by the digest
	} else if !contains(p.Permissions.Exec, prog) {
		if strings.ContainsAny(prog, `/\`) || strings.Contains(prog, "..") {
			return fmt.Errorf("%s: program %q is a path outside the package, so the digest cannot vouch for it; use \"./name\" for a file inside the package, or a bare name listed in permissions.exec", who, prog)
		}
		return fmt.Errorf("%s: program %q is not in permissions.exec, so it would be refused when it ran; add %q to permissions.exec so the user is told, or use \"./%s\" for a file shipped in the package", who, prog, prog, prog)
	}
	for i, a := range o.Argv {
		if len(a) > MaxArgvElemLen {
			return fmt.Errorf("%s: argv[%d] is longer than %d bytes", who, i, MaxArgvElemLen)
		}
		if strings.ContainsRune(a, 0) {
			return fmt.Errorf("%s: argv[%d] holds a NUL byte, which ends a string for the system and hides what follows", who, i)
		}
		parts, err := parseTemplate(a)
		if err != nil {
			return fmt.Errorf("%s: argv[%d]: %w", who, i, err)
		}
		if err := checkPlaceholders(fmt.Sprintf("%s argv[%d]", who, i), parts, oc); err != nil {
			return err
		}
	}
	return nil
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}
