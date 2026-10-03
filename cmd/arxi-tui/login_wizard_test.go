package main

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/michiTrader/arxi_tui/internal/driver"
	"github.com/michiTrader/arxi_tui/internal/fold"
	"github.com/michiTrader/arxi_tui/internal/scene"
	"github.com/michiTrader/arxi_tui/internal/term"
	"github.com/michiTrader/arxi_tui/internal/theme"
)

// testKey is a fake key shaped like a real one. It must never appear on screen.
const testKey = "sk-or-v1-ZXCVBNM1234567890qwertyuiopASDFGH"

func press(w *loginWizard, t term.KeyType) loginKeyResult {
	return routeLoginKey(w, term.Key{Type: t})
}

func typeInto(w *loginWizard, s string) {
	for _, r := range s {
		routeLoginKey(w, term.Key{Type: term.KeyRunes, Runes: []rune{r}})
	}
}

// openProvider walks the wizard to the form of the provider with the given id.
func openProvider(t *testing.T, w *loginWizard, id string) {
	t.Helper()
	press(w, term.KeyDown) // "Sign in with an API key"
	press(w, term.KeyEnter)
	for i, it := range w.items {
		if it.id == id && !it.other || (id == "" && it.other) {
			w.sel = i
			press(w, term.KeyEnter)
			if w.step != stepForm {
				t.Fatalf("enter on %q did not open a form (step %d)", id, w.step)
			}
			return
		}
	}
	t.Fatalf("no provider %q in the list", id)
}

func published(w *loginWizard) fold.State {
	var st fold.State
	w.publish(&st)
	return st
}

func screenText(st fold.State) string {
	var b strings.Builder
	b.WriteString(st.LoginTitle + "\n" + st.LoginPager + "\n" + st.LoginHint + "\n")
	for _, r := range st.LoginRows {
		b.WriteString(r.Label + " | " + r.Status + "\n")
	}
	return b.String()
}

func TestLoginMethodStepAndAccountUnavailable(t *testing.T) {
	w := newLoginWizard(nil)
	st := published(w)
	if st.LoginTitle != "Select authentication method:" || len(st.LoginRows) != 2 {
		t.Fatalf("method step = %q with %d rows", st.LoginTitle, len(st.LoginRows))
	}
	res := press(w, term.KeyEnter) // first row: account
	if res.notice != loginAccountUnavailable || w.step != stepMethod {
		t.Errorf("account sign-in must be refused in place; got notice %q step %d", res.notice, w.step)
	}
}

func TestLoginProviderListShowsStatusAndPager(t *testing.T) {
	w := newLoginWizard([]driver.ProviderRow{
		{Name: "openrouter", Key: "env", APIKeyEnv: "OPENROUTER_API_KEY"},
		{Name: "groq", Key: "stored"},
		{Name: "mine", Key: "missing", APIKeyEnv: "MINE_KEY"},
	})
	press(w, term.KeyDown)
	press(w, term.KeyEnter)
	st := published(w)
	if st.LoginTitle != "Select provider to configure:" {
		t.Fatalf("title = %q", st.LoginTitle)
	}
	if len(st.LoginRows) != loginPageSize {
		t.Errorf("window shows %d rows; want %d", len(st.LoginRows), loginPageSize)
	}
	if !strings.HasPrefix(st.LoginPager, "(1/") {
		t.Errorf("pager = %q", st.LoginPager)
	}
	text := screenText(st)
	for _, want := range []string{"• unconfigured", "✓ env: OPENROUTER_API_KEY", "✓ key stored"} {
		if !strings.Contains(text, want) {
			t.Errorf("status column lacks %q:\n%s", want, text)
		}
	}
	// Extras the catalog does not know are listed, before Other.
	last := w.items[len(w.items)-1]
	prev := w.items[len(w.items)-2]
	if !last.other || prev.id != "mine" {
		t.Errorf("want registered extras then Other; got %q then %q", prev.id, last.display)
	}
	if got := w.statusText(prev); got != "• key missing (env: MINE_KEY)" {
		t.Errorf("status of a key-less provider = %q", got)
	}
}

func TestLoginWindowFollowsSelection(t *testing.T) {
	for _, c := range []struct{ sel, n, lo int }{{0, 20, 0}, {5, 20, 0}, {10, 20, 5}, {19, 20, 10}, {2, 4, 0}} {
		lo, hi := window(c.sel, c.n, 10)
		if c.n <= 10 {
			if lo != 0 || hi != c.n {
				t.Errorf("window(%d,%d) = %d,%d", c.sel, c.n, lo, hi)
			}
			continue
		}
		if lo != c.lo || hi-lo != 10 || c.sel < lo || c.sel >= hi {
			t.Errorf("window(%d,%d) = %d,%d; want lo %d and the highlight inside", c.sel, c.n, lo, hi, c.lo)
		}
	}
}

// TestLoginPublishesBulletsOnly is the property the whole feature turns on: whatever
// is typed into the key field, the published state holds bullets, never the key, and
// the bullet count is capped.
func TestLoginPublishesBulletsOnly(t *testing.T) {
	w := newLoginWizard(nil)
	openProvider(t, w, "openrouter")
	typeInto(w, testKey)
	text := screenText(published(w))
	if strings.Contains(text, testKey) || strings.Contains(text, testKey[:6]) {
		t.Fatalf("the key (or a prefix of it) is on the screen:\n%s", text)
	}
	if !strings.Contains(text, "••••") {
		t.Errorf("no bullets shown for a typed key:\n%s", text)
	}
	if strings.Count(text, "•") > 40+1 { // 40 bullets (+ none elsewhere)
		t.Errorf("bullets are not capped at 40:\n%s", text)
	}

	// Exactness below the cap: 5 characters show 5 bullets.
	w2 := newLoginWizard(nil)
	openProvider(t, w2, "openrouter")
	typeInto(w2, "abcde")
	if got := w2.fields[0].shown(false); got != "•••••" {
		t.Errorf("5 characters show %q", got)
	}
}

func TestLoginPasteGoesToTheFieldAndStripsNewlines(t *testing.T) {
	w := newLoginWizard(nil)
	openProvider(t, w, "openrouter")
	w.paste(" " + testKey + "\r\n")
	if got := w.fields[0].value; got != testKey {
		t.Errorf("pasted key not cleaned: %q", got)
	}
	if strings.Contains(screenText(published(w)), testKey) {
		t.Error("pasted key reached the screen")
	}
}

func TestLoginEnvNameFieldMasksAKeyPastedIntoIt(t *testing.T) {
	w := newLoginWizard(nil)
	openProvider(t, w, "")
	for w.fields[w.focus].label != "Env var name" {
		press(w, term.KeyTab)
	}
	typeInto(w, "MY_API_KEY")
	if got := w.fields[w.focus].shown(false); got != "MY_API_KEY" {
		t.Errorf("a real variable name should be readable; got %q", got)
	}
	w.fields[w.focus].value = ""
	typeInto(w, testKey)
	if strings.Contains(screenText(published(w)), testKey) {
		t.Error("a key pasted into the env var field is on the screen")
	}
}

func TestLoginRefusalsNameTheFieldNeverTheValue(t *testing.T) {
	w := newLoginWizard(nil)
	openProvider(t, w, "")
	// Name + a bad URL that embeds a secret-looking string.
	typeInto(w, "acme")
	press(w, term.KeyTab)
	typeInto(w, testKey) // not a URL
	for w.focus != len(w.fields)-1 {
		press(w, term.KeyTab)
	}
	res := press(w, term.KeyEnter)
	if res.dispatch != nil {
		t.Fatal("a form with a bad URL was dispatched")
	}
	if !strings.Contains(res.notice, "Base URL") || strings.Contains(res.notice, testKey) {
		t.Errorf("refusal %q must name the field and never the value", res.notice)
	}

	// A key typed into the env var NAME field is refused without echoing it.
	w = newLoginWizard(nil)
	openProvider(t, w, "")
	typeInto(w, "acme")
	press(w, term.KeyTab)
	typeInto(w, "https://api.acme.test/v1")
	press(w, term.KeyTab)
	press(w, term.KeyTab)
	typeInto(w, testKey)
	for w.focus != len(w.fields)-1 {
		press(w, term.KeyTab)
	}
	res = press(w, term.KeyEnter)
	if res.dispatch != nil || strings.Contains(res.notice, testKey) || !strings.Contains(res.notice, "Env var name") {
		t.Errorf("env-var refusal = %q (dispatch %v)", res.notice, res.dispatch)
	}
}

func TestLoginEscapeWipesTheKeyAndSteps(t *testing.T) {
	w := newLoginWizard(nil)
	openProvider(t, w, "openrouter")
	typeInto(w, testKey)
	press(w, term.KeyEscape)
	if w.step != stepProvider || w.fields != nil {
		t.Fatalf("esc from the form: step %d, fields %v", w.step, w.fields)
	}
	if strings.Contains(screenText(published(w)), "••") {
		t.Error("bullets survived the wipe")
	}
	if res := press(w, term.KeyEscape); res.close || w.step != stepMethod {
		t.Errorf("second esc should go back to the method step; close=%v step=%d", res.close, w.step)
	}
	if res := press(w, term.KeyEscape); !res.close {
		t.Error("third esc should close the wizard")
	}
}

func TestLoginSaveBuildsAnActionForACatalogProvider(t *testing.T) {
	w := newLoginWizard(nil)
	openProvider(t, w, "openrouter")
	// OpenRouter is not core-known, so the form also asks for a model.
	typeInto(w, testKey)
	for w.focus != len(w.fields)-1 {
		press(w, term.KeyTab)
	}
	res := press(w, term.KeyEnter)
	// A model id is optional, so the save goes through.
	if res.dispatch == nil {
		t.Fatalf("save did not dispatch: %q", res.notice)
	}
	a := res.dispatch
	if a.Name != "openrouter" || a.BaseURL != "https://openrouter.ai/api/v1" || a.Key != testKey || a.Existing {
		t.Errorf("action = %+v", *a)
	}
}

func TestLoginCoreKnownProviderAsksForTheKeyOnly(t *testing.T) {
	w := newLoginWizard(nil)
	openProvider(t, w, "anthropic")
	if len(w.fields) != 1 || w.fields[0].label != "API key" {
		t.Fatalf("fields = %+v", w.fields)
	}
	if res := press(w, term.KeyEnter); res.dispatch != nil || !strings.Contains(res.notice, "API key is required") {
		t.Errorf("an empty key must be refused naming the field: %+v", res)
	}
}

func TestLoginRegisteredProviderUsesProviderKey(t *testing.T) {
	w := newLoginWizard([]driver.ProviderRow{{Name: "openrouter", Key: "missing", Models: 3}})
	openProvider(t, w, "openrouter")
	typeInto(w, testKey)
	res := press(w, term.KeyEnter)
	if res.dispatch == nil || !res.dispatch.Existing || res.dispatch.Name != "openrouter" {
		t.Fatalf("want an Existing action; got %+v / %q", res.dispatch, res.notice)
	}
}

func TestLoginEnvVarTakesPrecedenceNotice(t *testing.T) {
	w := newLoginWizard([]driver.ProviderRow{{Name: "openrouter", Key: "env", APIKeyEnv: "OPENROUTER_API_KEY", Models: 1}})
	press(w, term.KeyDown)
	press(w, term.KeyEnter)
	for i, it := range w.items {
		if it.id == "openrouter" {
			w.sel = i
		}
	}
	res := press(w, term.KeyEnter)
	if !strings.Contains(res.notice, "OPENROUTER_API_KEY") || !strings.Contains(res.notice, "precedence") {
		t.Errorf("notice = %q", res.notice)
	}
}

func TestLoginOtherFormValidation(t *testing.T) {
	fill := func(w *loginWizard, label, v string) {
		for w.fields[w.focus].label != label {
			press(w, term.KeyTab)
		}
		w.fields[w.focus].value = v
	}
	base := func() *loginWizard {
		w := newLoginWizard(nil)
		openProvider(t, w, "")
		fill(w, "Name", "acme")
		fill(w, "Base URL", "https://api.acme.test/v1")
		fill(w, "API key", testKey)
		return w
	}

	w := base()
	act, msg := w.submit()
	if msg != "" || act.Name != "acme" || act.Key != testKey {
		t.Fatalf("valid Other form: %+v %q", act, msg)
	}

	w = base()
	fill(w, "Price in (USD / 1M tokens)", "1")
	if _, msg := w.submit(); !strings.Contains(msg, "both prices") {
		t.Errorf("one price alone: %q", msg)
	}
	fill(w, "Price out (USD / 1M tokens)", "2")
	if _, msg := w.submit(); !strings.Contains(msg, "Model id") {
		t.Errorf("a price without a model: %q", msg)
	}
	fill(w, "Model id", "m")
	act, msg = w.submit()
	if msg != "" || act.In == nil || *act.In != 1 || *act.Out != 2 {
		t.Errorf("priced model: %+v %q", act, msg)
	}
	fill(w, "Price in (USD / 1M tokens)", "-1")
	if _, msg := w.submit(); !strings.Contains(msg, "0 or more") {
		t.Errorf("negative price: %q", msg)
	}
	fill(w, "Price in (USD / 1M tokens)", "0")
	fill(w, "Price out (USD / 1M tokens)", "0")
	if act, msg := w.submit(); msg != "" || *act.In != 0 {
		t.Errorf("zero is a valid price: %+v %q", act, msg)
	}

	w = base()
	fill(w, "API key", "")
	if _, msg := w.submit(); !strings.Contains(msg, "API key") {
		t.Errorf("neither key nor env var: %q", msg)
	}
	fill(w, "Env var name", "ACME_KEY")
	if act, msg := w.submit(); msg != "" || act.EnvName != "ACME_KEY" || act.Key != "" {
		t.Errorf("env var only: %+v %q", act, msg)
	}
}

func TestLoginLocalNeedsNoKey(t *testing.T) {
	w := newLoginWizard(nil)
	press(w, term.KeyDown)
	press(w, term.KeyEnter)
	for i, it := range w.items {
		if it.id == "local" {
			w.sel = i
		}
	}
	res := press(w, term.KeyEnter)
	if res.dispatch == nil || res.dispatch.Name != "local" || res.dispatch.Key != "" {
		t.Errorf("local should register without a key: %+v %q", res.dispatch, res.notice)
	}
}

func TestLoginTabCyclesFields(t *testing.T) {
	w := newLoginWizard(nil)
	openProvider(t, w, "")
	n := len(w.fields)
	press(w, term.KeyTab)
	if w.focus != 1 {
		t.Fatalf("tab -> %d", w.focus)
	}
	routeLoginKey(w, term.Key{Type: term.KeyTab, Mod: term.ModShift})
	routeLoginKey(w, term.Key{Type: term.KeyTab, Mod: term.ModShift})
	if w.focus != n-1 {
		t.Errorf("shift-tab from 0 should wrap to %d; got %d", n-1, w.focus)
	}
}

// ---- worker ---------------------------------------------------------------

// fakeLogin records the order of calls and can fail chosen steps.
type fakeLogin struct {
	fakeProviderManager
	calls     []string
	keyArg    string
	keyErr    error
	addErr    error
	modelErr  error
	listErr   error
	listRows  []driver.ProviderRow
	keyStored bool
}

func (f *fakeLogin) SubmitProviderAdd(_ context.Context, p driver.ProviderAddParams) (*driver.ProviderAddResult, error) {
	f.calls = append(f.calls, "add")
	f.keyArg = p.APIKey
	if f.addErr != nil {
		return nil, f.addErr
	}
	return &driver.ProviderAddResult{Name: p.Name, KeyStored: f.keyStored}, nil
}
func (f *fakeLogin) SubmitProviderKey(_ context.Context, name, key string) (*driver.ProviderKeyResult, error) {
	f.calls = append(f.calls, "key")
	f.keyArg = key
	if f.keyErr != nil {
		return nil, f.keyErr
	}
	return &driver.ProviderKeyResult{Name: name, KeyStored: true}, nil
}
func (f *fakeLogin) SubmitProviderList(context.Context) (*driver.ProviderListResult, error) {
	f.calls = append(f.calls, "list")
	if f.listErr != nil {
		return nil, f.listErr
	}
	return &driver.ProviderListResult{Providers: f.listRows}, nil
}
func (f *fakeLogin) SubmitModelAdd(_ context.Context, p driver.ModelAddParams) (*driver.ProviderAddResult, error) {
	f.calls = append(f.calls, "model")
	if f.modelErr != nil {
		return nil, f.modelErr
	}
	return &driver.ProviderAddResult{}, nil
}

func TestLoginWorkOrderIsAddModelList(t *testing.T) {
	f := &fakeLogin{keyStored: true, listRows: []driver.ProviderRow{{Name: "acme", Key: "stored"}}}
	out := runLoginWork(context.Background(), f, loginAction{Name: "acme", BaseURL: "https://a.test/v1", Key: testKey, Model: "m"})
	if got := strings.Join(f.calls, ","); got != "add,model,list" {
		t.Errorf("call order = %s", got)
	}
	if !out.saved || !out.hasRows || out.notice != "✓ key stored for acme" {
		t.Errorf("outcome = %+v", out)
	}
	if f.keyArg != testKey {
		t.Error("the key did not reach the core")
	}
}

func TestLoginWorkExistingUsesProviderKey(t *testing.T) {
	f := &fakeLogin{}
	out := runLoginWork(context.Background(), f, loginAction{Existing: true, Name: "acme", Key: testKey})
	if strings.Join(f.calls, ",") != "key,list" || !out.saved {
		t.Errorf("calls %v outcome %+v", f.calls, out)
	}
}

// TestLoginScrubRemovesTheKeyFromErrors is the counterfactual for the worker: a
// transport error that echoes the key must not reach the banner.
func TestLoginScrubRemovesTheKeyFromErrors(t *testing.T) {
	f := &fakeLogin{addErr: errors.New("write failed for " + testKey)}
	out := runLoginWork(context.Background(), f, loginAction{Name: "acme", Key: testKey})
	if strings.Contains(out.notice, testKey) || !strings.Contains(out.notice, "[key hidden]") {
		t.Errorf("notice leaks the key: %q", out.notice)
	}
	if out.saved {
		t.Error("a failed save must not wipe the form")
	}
	if got := scrub("x "+testKey+" y", testKey); strings.Contains(got, testKey) {
		t.Errorf("scrub left the key: %q", got)
	}
	if scrub("abc", "") != "abc" {
		t.Error("scrub with an empty secret must be a no-op")
	}
}

func TestLoginWorkModelFailureKeepsTheSave(t *testing.T) {
	f := &fakeLogin{modelErr: errors.New("bad model " + testKey)}
	out := runLoginWork(context.Background(), f, loginAction{Name: "acme", Key: testKey, Model: "m"})
	if !out.saved || !strings.Contains(out.notice, `the model "m" was not added`) || strings.Contains(out.notice, testKey) {
		t.Errorf("outcome = %+v", out)
	}
}

func TestLoginWorkFailedRefreshStillSaved(t *testing.T) {
	f := &fakeLogin{listErr: errors.New("boom")}
	out := runLoginWork(context.Background(), f, loginAction{Name: "acme", Key: testKey})
	if !out.saved || out.hasRows {
		t.Errorf("outcome = %+v", out)
	}
}

func TestLoginGateRefusesAnOldCore(t *testing.T) {
	old := &driver.Hello{Type: "hello", Implemented: []string{"provider.add", "model.list"}}
	err := requireLoginVerbs(old)
	if err == nil || !strings.Contains(err.Error(), "provider.key") || !strings.Contains(err.Error(), "go build") {
		t.Errorf("gate error = %v", err)
	}
	if requireLoginVerbs(nil) == nil {
		t.Error("a nil hello must be refused")
	}
	full := &driver.Hello{Implemented: loginVerbs}
	if err := requireLoginVerbs(full); err != nil {
		t.Errorf("full core refused: %v", err)
	}
}

func TestIsLoginCommand(t *testing.T) {
	for in, want := range map[string]bool{"/login": true, " /login ": true, "/login sk-123": false, "/logins": false, "login": false, "": false} {
		if isLoginCommand(in) != want {
			t.Errorf("isLoginCommand(%q) = %v", in, !want)
		}
	}
}

func TestTheEmbeddedLoginSceneMatchesTheFixture(t *testing.T) {
	want, err := os.ReadFile("../../testdata/LOGIN.json")
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(want)) != strings.TrimSpace(factoryLogin) {
		t.Error("factoryLogin drifted from testdata/LOGIN.json")
	}
	if _, err := loadLoginScene(); err != nil {
		t.Error(err)
	}
}

// ---- the loop --------------------------------------------------------------

type loginLoopDriver struct {
	fakeLogin
	evCh      chan fold.Event
	submitted []string
}

func (d *loginLoopDriver) SubmitPrompt(_ context.Context, text string) error {
	d.submitted = append(d.submitted, text)
	return nil
}

func newLoginLoopDriver() *loginLoopDriver {
	d := &loginLoopDriver{}
	d.hello = &driver.Hello{Type: "hello", Types: loginVerbs, Implemented: loginVerbs}
	d.keyStored = true
	return d
}

// runLoginScript plays a script against the real loop with a login-capable driver
// and returns everything the loop drew.
func runLoginScript(t *testing.T, drv *loginLoopDriver, script []scheduledEvent) string {
	t.Helper()
	doc, err := scene.ParseDocument([]byte(factorySobria))
	if err != nil {
		t.Fatalf("ParseDocument: %v", err)
	}
	tty := newFakeTTY(100, 30, script)
	drv.evCh = make(chan fold.Event, 64)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := loop(ctx, tty, doc, theme.SOBRIA(), drv.evCh, drv, ""); err != nil {
		t.Fatalf("loop returned error: %v", err)
	}
	return tty.output()
}

func gap(ev term.Event) scheduledEvent { return scheduledEvent{40 * time.Millisecond, ev} }

func TestLoginLoopEndToEnd(t *testing.T) {
	drv := newLoginLoopDriver()
	drv.listRows = []driver.ProviderRow{{Name: "openrouter", Key: "stored", Models: 2}}
	script := typeLine("/login")
	script = append(script,
		gap(enterEvent()),
		gap(arrowEvent(term.KeyDown)), gap(enterEvent()), // method: API key
		gap(arrowEvent(term.KeyDown)), gap(arrowEvent(term.KeyDown)), gap(arrowEvent(term.KeyDown)), // -> openrouter
		gap(enterEvent()),
		gap(pasteEvent(testKey+"\n")),
		gap(enterEvent()), // save (only the key field needs filling)
	)
	script = append(script, quit()...)
	out := runLoginScript(t, drv, script)

	if len(drv.submitted) != 0 {
		t.Errorf("login keystrokes reached the chat: %q", drv.submitted)
	}
	if drv.keyArg != testKey {
		t.Errorf("the core received %q; want the pasted key", drv.keyArg)
	}
	if strings.Contains(out, testKey) || strings.Contains(out, testKey[:12]) {
		t.Error("the key (or a prefix) was written to the screen")
	}
	for _, want := range []string{"Select authentication method:", "Select provider to configure:", "••••", "key stored"} {
		if !strings.Contains(out, want) {
			t.Errorf("screen never showed %q", want)
		}
	}
}

func TestLoginLoopKeystrokesNeverReachTheChat(t *testing.T) {
	drv := newLoginLoopDriver()
	script := typeLine("/login")
	script = append(script, gap(enterEvent()), gap(keyEvent('h')), gap(keyEvent('i')), gap(enterEvent()))
	script = append(script, quit()...)
	runLoginScript(t, drv, script)
	if len(drv.submitted) != 0 {
		t.Errorf("typed text in the wizard reached the chat: %q", drv.submitted)
	}
}

func TestLoginLoopSlashMenuPickOpensTheWizard(t *testing.T) {
	drv := newLoginLoopDriver()
	script := []scheduledEvent{gap(keyEvent('/')), gap(arrowEvent(term.KeyUp)), gap(enterEvent())}
	script = append(script, quit()...)
	out := runLoginScript(t, drv, script)
	if !strings.Contains(out, "Select authentication method:") || len(drv.submitted) != 0 {
		t.Errorf("menu pick did not open the wizard (submitted %q)", drv.submitted)
	}
}

func TestLoginLoopOldCoreIsRefusedWithTheRemedy(t *testing.T) {
	drv := newLoginLoopDriver()
	drv.hello = &driver.Hello{Type: "hello", Implemented: []string{"provider.add"}}
	script := typeLine("/login")
	script = append(script, gap(enterEvent()))
	script = append(script, quit()...)
	out := runLoginScript(t, drv, script)
	if strings.Contains(out, "Select authentication method:") {
		t.Error("the wizard opened on a core that cannot store keys")
	}
	if !strings.Contains(out, "go build") {
		t.Error("the refusal does not name the rebuild command")
	}
}
