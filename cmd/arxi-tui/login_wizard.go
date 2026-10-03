package main

import (
	"fmt"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"unicode"

	"github.com/michiTrader/arxi_tui/internal/driver"
	"github.com/michiTrader/arxi_tui/internal/fold"
	"github.com/michiTrader/arxi_tui/internal/term"
)

// This file is the host half of Scene 13 (LOGIN): the `/login` wizard. The document
// is testdata/LOGIN.json; what lives here is the state it binds to and the keys that
// move it, as pure functions a test can drive without a terminal.
//
// # The one rule: a key is never displayed, logged or put in the scene state
//
// A secret field lives in exactly one place, loginField.value, inside the wizard the
// loop holds. publish() writes bullets into fold.State, never the value, so the
// renderer, the frame the user sees, the recorded transcript and every golden only
// ever meet the masked text. The value leaves the wizard once, inside a loginAction
// handed to the worker, which sends it to the core over the pipe and drops it. Tests
// pin all three: the masked row, the action, and that no frame contains the key.
//
// Go strings are immutable, so wipe() cannot overwrite the bytes in memory; it drops
// the references so the key stops being reachable from the wizard. That is the most a
// managed language gives, and it is said here so nobody reads wipe as a guarantee it
// is not.

// loginStep is where in the wizard the user is.
type loginStep int

const (
	stepMethod   loginStep = iota // "Select authentication method:"
	stepProvider                  // "Select provider to configure:"
	stepForm                      // the fields of one provider (key, or the Other form)
)

const (
	// loginPageSize is how many provider rows are visible at once. The pager
	// "(n/total)" tells the user where the window sits in the whole list.
	loginPageSize = 10

	loginMethodTitle   = "Select authentication method:"
	loginProviderTitle = "Select provider to configure:"
	loginListHint      = "↑↓ navigate · enter select · escape/ctrl+c cancel"
	loginFormHint      = "tab next field · enter next/save · escape back · ctrl+c cancel · the key is never shown"

	// loginAccountUnavailable is the honest answer for the first menu entry. There is
	// no account service behind it yet; pretending otherwise would be a button that
	// does nothing.
	loginAccountUnavailable = "Signing in with an account is not available yet; choose \"Sign in with an API key\""
)

// loginField is one input of the form.
type loginField struct {
	label    string
	value    string
	secret   bool // shown as bullets
	envName  bool // a variable NAME: shown as bullets if it is not shaped like one
	required bool
	// kind tells validation what the field must hold.
	kind fieldKind
}

type fieldKind int

const (
	kindText fieldKind = iota
	kindName
	kindURL
	kindKey
	kindEnv
	kindModel
	kindPrice
)

// loginItem is one row of the provider list.
type loginItem struct {
	id      string // provider name; "" for Other
	display string
	entry   catalogEntry // zero for extras and Other
	known   bool         // entry is meaningful
	other   bool
}

// loginAction is what a finished form asks the worker to do. It is the ONLY place a
// secret leaves the wizard.
type loginAction struct {
	Existing bool // the provider is already registered: store a key, do not add
	Name     string
	BaseURL  string
	EnvName  string
	Key      string
	Model    string
	In, Out  *float64
}

// loginWizard is the loop-visible state of the open screen. nil means closed.
type loginWizard struct {
	step     loginStep
	sel      int
	statuses map[string]driver.ProviderRow
	items    []loginItem

	// the form in progress
	fields   []loginField
	focus    int
	existing bool
	target   loginItem
}

// newLoginWizard opens on the first step with the given provider rows.
func newLoginWizard(rows []driver.ProviderRow) *loginWizard {
	w := &loginWizard{step: stepMethod}
	w.setProviders(rows)
	return w
}

// setProviders replaces the credential state and rebuilds the list: the catalog
// first (stable order, so the screen does not reshuffle after a save), then any
// registered provider the catalog does not know, then Other.
func (w *loginWizard) setProviders(rows []driver.ProviderRow) {
	w.statuses = map[string]driver.ProviderRow{}
	for _, r := range rows {
		w.statuses[r.Name] = r
	}
	w.items = w.items[:0]
	for _, e := range loginCatalog {
		w.items = append(w.items, loginItem{id: e.ID, display: e.Display, entry: e, known: true})
	}
	var extra []string
	for name := range w.statuses {
		if _, ok := catalogByID(name); !ok {
			extra = append(extra, name)
		}
	}
	sort.Strings(extra)
	for _, name := range extra {
		w.items = append(w.items, loginItem{id: name, display: name})
	}
	w.items = append(w.items, loginItem{display: "Other…", other: true})
	if w.step == stepProvider && w.sel >= len(w.items) {
		w.sel = len(w.items) - 1
	}
}

// statusText is the right-hand column of a provider row.
func (w *loginWizard) statusText(it loginItem) string {
	if it.other {
		return "name, URL and key of your own"
	}
	r, ok := w.statuses[it.id]
	if !ok {
		return "• unconfigured"
	}
	switch r.Key {
	case "env":
		return "✓ env: " + r.APIKeyEnv
	case "stored":
		return "✓ key stored"
	case "none":
		return "✓ no key needed"
	case "missing":
		if r.APIKeyEnv != "" {
			return "• key missing (env: " + r.APIKeyEnv + ")"
		}
		return "• key missing"
	case "unreadable":
		return "! key folder unreadable"
	}
	return "• configured"
}

// move steps the highlight (list steps) or the focused field (form), wrapping.
func (w *loginWizard) move(delta int) {
	n := w.count()
	if n == 0 {
		return
	}
	if w.step == stepForm {
		w.focus = ((w.focus+delta)%n + n) % n
		return
	}
	w.sel = ((w.sel+delta)%n + n) % n
}

func (w *loginWizard) count() int {
	switch w.step {
	case stepMethod:
		return 2
	case stepProvider:
		return len(w.items)
	}
	return len(w.fields)
}

// wipe drops every typed value. See the file comment for what that can and cannot do.
func (w *loginWizard) wipe() {
	for i := range w.fields {
		w.fields[i].value = ""
	}
	w.fields = nil
	w.focus = 0
}

// openForm builds the fields for the chosen provider.
func (w *loginWizard) openForm(it loginItem) {
	w.target = it
	w.focus = 0
	row, registered := w.statuses[it.id]
	w.existing = registered && !it.other
	var f []loginField
	switch {
	case it.other:
		f = []loginField{
			{label: "Name", required: true, kind: kindName},
			{label: "Base URL", required: true, kind: kindURL},
			{label: "API key", secret: true, kind: kindKey},
			{label: "Env var name", envName: true, kind: kindEnv},
		}
		f = append(f, modelFields()...)
	default:
		f = []loginField{{label: "API key", secret: true, required: true, kind: kindKey}}
		// A provider the core has no model table for registers with no models, so
		// the form asks for one (see login_catalog.go). Skip it when models exist.
		if !it.entry.CoreKnown && (!registered || row.Models == 0) {
			f = append(f, modelFields()...)
		}
	}
	w.fields = f
	w.step = stepForm
}

func modelFields() []loginField {
	return []loginField{
		{label: "Model id", kind: kindModel},
		{label: "Price in (USD / 1M tokens)", kind: kindPrice},
		{label: "Price out (USD / 1M tokens)", kind: kindPrice},
	}
}

// isEnvName reports whether s is shaped like an environment variable NAME. A value
// that is not is rendered masked: the likeliest way to put a secret in this field is
// pasting the key into it.
func isEnvName(s string) bool {
	if s == "" {
		return true
	}
	for i, r := range s {
		switch {
		case r == '_', r >= 'A' && r <= 'Z', r >= 'a' && r <= 'z':
		case r >= '0' && r <= '9' && i > 0:
		default:
			return false
		}
	}
	return true
}

// shown is what a field displays. This is the single function between a typed value
// and the screen.
func (f loginField) shown(focused bool) string {
	var s string
	switch {
	case f.value == "":
		if f.required {
			s = "(required)"
		} else {
			s = "(optional)"
		}
		if focused {
			s = "▏" + s
		}
		return s
	case f.secret, f.envName && !isEnvName(f.value):
		n := len([]rune(f.value))
		if n > 40 {
			n = 40
		}
		s = strings.Repeat("•", n)
	default:
		s = f.value
	}
	if focused {
		s += "▏"
	}
	return s
}

// publish writes the wizard's view onto the folded state the renderer reads. It is
// the only thing that does, and it writes only masked text.
func (w *loginWizard) publish(state *fold.State) {
	var rows []fold.LoginRow
	total, at := w.count(), 0
	switch w.step {
	case stepMethod:
		state.LoginTitle, state.LoginHint = loginMethodTitle, loginListHint
		at = w.sel
		for i, l := range [][2]string{
			{"Sign in with an account", "unavailable"},
			{"Sign in with an API key", ""},
		} {
			rows = append(rows, fold.LoginRow{Label: l[0], Status: l[1], Selected: i == w.sel})
		}
	case stepProvider:
		state.LoginTitle, state.LoginHint = loginProviderTitle, loginListHint
		at = w.sel
		lo, hi := window(w.sel, total, loginPageSize)
		for i := lo; i < hi; i++ {
			rows = append(rows, fold.LoginRow{
				Label: w.items[i].display, Status: w.statusText(w.items[i]), Selected: i == w.sel})
		}
	case stepForm:
		state.LoginTitle = "Configure " + w.target.display + ":"
		state.LoginHint = loginFormHint
		at = w.focus
		for i, f := range w.fields {
			rows = append(rows, fold.LoginRow{
				Label: f.label, Status: f.shown(i == w.focus), Selected: i == w.focus})
		}
	}
	state.LoginRows = rows
	state.LoginPager = ""
	if total > 0 {
		state.LoginPager = fmt.Sprintf("(%d/%d)", at+1, total)
	}
}

// window is the [lo,hi) slice of a list of n rows, size rows tall, that keeps the
// highlight visible and does not move until the highlight reaches an edge.
func window(sel, n, size int) (lo, hi int) {
	if n <= size {
		return 0, n
	}
	lo = sel - size/2
	if lo < 0 {
		lo = 0
	}
	if lo+size > n {
		lo = n - size
	}
	return lo, lo + size
}

// loginKeyResult is what one keypress asks the loop to do.
type loginKeyResult struct {
	close    bool         // leave the wizard
	notice   string       // a message for the banner ("" = leave it)
	clear    bool         // clear the banner
	dispatch *loginAction // run on the worker
}

// routeLoginKey applies one key to the wizard.
func routeLoginKey(w *loginWizard, k term.Key) loginKeyResult {
	switch k.Type {
	case term.KeyEscape:
		return w.back()
	case term.KeyUp:
		w.move(-1)
		return loginKeyResult{}
	case term.KeyDown:
		w.move(1)
		return loginKeyResult{}
	case term.KeyTab:
		if w.step == stepForm {
			if k.Mod&term.ModShift != 0 {
				w.move(-1)
			} else {
				w.move(1)
			}
		}
		return loginKeyResult{}
	case term.KeyEnter:
		return w.enter()
	case term.KeyBackspace:
		w.edit(func(s string) string { return dropLastRune(s) })
		return loginKeyResult{}
	case term.KeyRunes:
		if w.step != stepForm {
			return loginKeyResult{}
		}
		if k.Mod&term.ModCtrl != 0 {
			if len(k.Runes) == 1 && k.Runes[0] == 'u' {
				w.edit(func(string) string { return "" })
			}
			return loginKeyResult{}
		}
		if k.Mod&term.ModAlt != 0 {
			return loginKeyResult{}
		}
		w.edit(func(s string) string { return s + cleanFieldText(string(k.Runes)) })
	}
	return loginKeyResult{}
}

func dropLastRune(s string) string {
	r := []rune(s)
	if len(r) == 0 {
		return s
	}
	return string(r[:len(r)-1])
}

// cleanFieldText removes everything a single-line field cannot hold: whitespace and
// control characters. A key never contains either, so stripping them is what makes a
// pasted key with a trailing newline work instead of fail.
func cleanFieldText(s string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsSpace(r) || unicode.IsControl(r) {
			return -1
		}
		return r
	}, s)
}

func (w *loginWizard) edit(fn func(string) string) {
	if w.step != stepForm || w.focus < 0 || w.focus >= len(w.fields) {
		return
	}
	w.fields[w.focus].value = fn(w.fields[w.focus].value)
}

// paste inserts clipboard text into the focused field. It never reaches the chat
// input: the wizard owns the keyboard while it is open.
func (w *loginWizard) paste(text string) {
	if w.step != stepForm {
		return
	}
	clean := cleanFieldText(text)
	w.edit(func(s string) string { return s + clean })
}

func (w *loginWizard) back() loginKeyResult {
	switch w.step {
	case stepForm:
		w.wipe()
		w.step = stepProvider
		return loginKeyResult{clear: true}
	case stepProvider:
		w.step = stepMethod
		w.sel = 1
		return loginKeyResult{clear: true}
	}
	return loginKeyResult{close: true, clear: true}
}

func (w *loginWizard) enter() loginKeyResult {
	switch w.step {
	case stepMethod:
		if w.sel == 0 {
			return loginKeyResult{notice: loginAccountUnavailable}
		}
		w.step, w.sel = stepProvider, 0
		return loginKeyResult{clear: true}
	case stepProvider:
		it := w.items[w.sel]
		if it.known && it.entry.NoKey {
			// Nothing to type: register it and be done.
			if _, ok := w.statuses[it.id]; ok {
				return loginKeyResult{notice: it.display + " is already configured; it needs no key"}
			}
			return loginKeyResult{dispatch: &loginAction{Name: it.id}}
		}
		w.openForm(it)
		if r, ok := w.statuses[it.id]; ok && r.Key == "env" {
			return loginKeyResult{notice: r.APIKeyEnv + " is set in the environment and takes precedence over a stored key"}
		}
		return loginKeyResult{clear: true}
	}
	if w.focus < len(w.fields)-1 {
		w.focus++
		return loginKeyResult{}
	}
	act, msg := w.submit()
	if msg != "" {
		return loginKeyResult{notice: msg}
	}
	return loginKeyResult{dispatch: &act}
}

// submit validates the form and builds the action. The returned message names the
// FIELD that is wrong and never its value: the value may be a secret, and a refusal
// that quotes it would put the secret in the banner.
func (w *loginWizard) submit() (loginAction, string) {
	get := func(label string) string {
		for _, f := range w.fields {
			if f.label == label {
				return strings.TrimSpace(f.value)
			}
		}
		return ""
	}
	for _, f := range w.fields {
		v := strings.TrimSpace(f.value)
		if f.required && v == "" {
			return loginAction{}, f.label + " is required"
		}
		switch f.kind {
		case kindURL:
			u, err := url.Parse(v)
			if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
				return loginAction{}, f.label + " must start with http:// or https://"
			}
		case kindEnv:
			if !isEnvName(v) {
				return loginAction{}, f.label + " is the NAME of a variable (like MY_API_KEY), not the key itself; type the key in the API key field"
			}
		case kindPrice:
			if v != "" {
				if n, err := strconv.ParseFloat(v, 64); err != nil || n < 0 {
					return loginAction{}, f.label + " must be a number, 0 or more"
				}
			}
		}
	}
	act := loginAction{
		Existing: w.existing,
		Name:     w.target.id,
		BaseURL:  w.target.entry.BaseURL,
		Key:      get("API key"),
		Model:    get("Model id"),
	}
	if w.target.other {
		act.Name, act.BaseURL, act.EnvName = get("Name"), get("Base URL"), get("Env var name")
		if act.Key == "" && act.EnvName == "" {
			return loginAction{}, "give an API key, or the name of the environment variable that holds it"
		}
	}
	pin, pout := get("Price in (USD / 1M tokens)"), get("Price out (USD / 1M tokens)")
	if (pin == "") != (pout == "") {
		return loginAction{}, "give both prices or neither; one alone would price the other direction at zero"
	}
	if pin != "" {
		if act.Model == "" {
			return loginAction{}, "Model id is required when a price is given"
		}
		in, _ := strconv.ParseFloat(pin, 64)
		out, _ := strconv.ParseFloat(pout, 64)
		act.In, act.Out = &in, &out
	}
	return act, ""
}
