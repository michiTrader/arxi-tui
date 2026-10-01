package main

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/michiTrader/arxi_tui/internal/driver"
	"github.com/michiTrader/arxi_tui/internal/fold"
)

// These tests pin the host-level grammar and formatting of the K2 provider/model
// commands. The parsing is a pure function of the line (the discipline
// plugin_install_cmd.go follows), so it is pinned directly; the orchestration is
// split so runProviderCmd -- dispatch plus format -- is exercised against a fake
// providerManager without a subprocess, the one thing only a live arxi can
// confirm kept out of the test while everything up to it is proven here.

// fakeProviderManager answers the four verbs with chosen results, so a test can
// drive runProviderCmd through each path (success, refusal, empty) without the
// network. A nil result field with a non-nil err models the core's ok:false.
type fakeProviderManager struct {
	hello      *driver.Hello
	addResult  *driver.ProviderAddResult
	addErr     error
	listResult *driver.ModelListResult
	listErr    error
	enResult   *driver.ModelEnableResult
	enErr      error
	// enOn records the on flag the last SubmitModelEnable saw, so a test can
	// confirm /model disable sent on=false rather than trusting the formatter.
	enOn    bool
	enRef   string
	addSeen driver.ProviderAddParams
}

func (f *fakeProviderManager) Hello() *driver.Hello { return f.hello }

func (f *fakeProviderManager) SubmitProviderAdd(_ context.Context, p driver.ProviderAddParams) (*driver.ProviderAddResult, error) {
	f.addSeen = p
	return f.addResult, f.addErr
}

func (f *fakeProviderManager) SubmitModelList(_ context.Context) (*driver.ModelListResult, error) {
	return f.listResult, f.listErr
}

func (f *fakeProviderManager) SubmitModelEnable(_ context.Context, ref string, on bool) (*driver.ModelEnableResult, error) {
	f.enRef = ref
	f.enOn = on
	return f.enResult, f.enErr
}

// SubmitPrompt and Close let fakeProviderManager double as a Driver, so a
// dispatchProviderCmd test can pass it where the loop passes the real driver and
// exercise the capability assertion against a type that genuinely has the four
// methods. They do nothing: a provider command never calls them.
func (f *fakeProviderManager) SubmitPrompt(context.Context, string) error { return nil }
func (f *fakeProviderManager) Close() error                               { return nil }

// --- parser: matched=false must leave the line for the next dispatch branch ---

// TestProviderParsersDeclineForeignLines pins the one exit that returns
// matched=false: a line that is not this command at all. The dispatch chain
// relies on it to fall through to the slash menu, so a parser that claimed a
// line it does not own would swallow it into a refusal the user never invoked.
func TestProviderParsersDeclineForeignLines(t *testing.T) {
	for _, line := range []string{"/help", "/ui plugin install x", "hello world", "/models", "/providers"} {
		if _, matched, _ := parseProviderCommand(line); matched {
			t.Errorf("parseProviderCommand claimed %q, which is not a /provider command; "+
				"it would swallow the line into a refusal instead of letting the dispatch "+
				"chain reach the right handler", line)
		}
		if _, matched, _ := parseModelCommand(line); matched {
			t.Errorf("parseModelCommand claimed %q, which is not a /model command; "+
				"it would swallow the line into a refusal instead of letting the dispatch "+
				"chain reach the right handler", line)
		}
	}
}

// TestParseProviderAddReadsNameAndFlags pins the add grammar: the name is the
// first token and the two flags are optional and order-independent, carried onto
// ProviderAddParams only when given so an omitted flag is never sent as "" (which
// the core would read as an explicit blank over its own default).
func TestParseProviderAddReadsNameAndFlags(t *testing.T) {
	act, matched, err := parseProviderCommand("/provider add openai --api-key-env OPENAI_API_KEY --base-url https://api.openai.com/v1")
	if !matched || err != nil {
		t.Fatalf("parseProviderCommand refused a well-formed add: matched=%v err=%v", matched, err)
	}
	if act.Verb != "provider.add" {
		t.Fatalf("add parsed to verb %q, not provider.add", act.Verb)
	}
	if act.Add.Name != "openai" {
		t.Errorf("name parsed as %q, want openai", act.Add.Name)
	}
	if act.Add.APIKeyEnv != "OPENAI_API_KEY" {
		t.Errorf("api-key-env parsed as %q, want OPENAI_API_KEY", act.Add.APIKeyEnv)
	}
	if act.Add.BaseURL != "https://api.openai.com/v1" {
		t.Errorf("base-url parsed as %q, want the given endpoint", act.Add.BaseURL)
	}

	// Name only: the two optional flags stay empty so dispatchProviderCmd sends
	// just the name and lets the core fill base-url from its known table.
	act, _, err = parseProviderCommand("/provider add local")
	if err != nil {
		t.Fatalf("name-only add refused: %v", err)
	}
	if act.Add.BaseURL != "" || act.Add.APIKeyEnv != "" {
		t.Errorf("name-only add carried a flag the user never typed: base-url=%q api-key-env=%q; "+
			"an empty string sent on the wire overwrites the core's default with a blank",
			act.Add.BaseURL, act.Add.APIKeyEnv)
	}
}

// TestParseProviderAddRefusesMalformed pins each refusal the add grammar makes,
// so a user's typo is named rather than dropped into the slash menu's silent
// no-match. Every case is matched=true (it IS an add) with a non-nil err.
func TestParseProviderAddRefusesMalformed(t *testing.T) {
	cases := []struct {
		name string
		line string
		want string // a substring the refusal must contain
	}{
		{"no name", "/provider add", "needs a provider name"},
		{"flag before name", "/provider add --base-url x", "name first"},
		{"unknown flag", "/provider add foo --token Z", "no flag"},
		{"flag without value", "/provider add foo --base-url", "needs a value"},
		{"unknown subcommand", "/provider frobnicate", "no subcommand"},
		{"list with argument", "/provider list extra", "takes no arguments"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, matched, err := parseProviderCommand(c.line)
			if !matched {
				t.Fatalf("%q was not matched as a /provider command, so its refusal falls "+
					"through to the slash menu's silent no-match instead of being named", c.line)
			}
			if err == nil {
				t.Fatalf("%q parsed without error, but it is malformed", c.line)
			}
			if !strings.Contains(err.Error(), c.want) {
				t.Errorf("refusal of %q = %q, want substring %q", c.line, err.Error(), c.want)
			}
		})
	}
}

// TestParseModelCommand pins the model grammar: list takes no args, enable and
// disable each take exactly one ref and map to the right wire verb.
func TestParseModelCommand(t *testing.T) {
	act, matched, err := parseModelCommand("/model list")
	if !matched || err != nil {
		t.Fatalf("/model list refused: matched=%v err=%v", matched, err)
	}
	if act.Verb != "model.list" || act.ProviderView {
		t.Errorf("/model list parsed to verb=%q providerView=%v, want model.list / false",
			act.Verb, act.ProviderView)
	}

	act, _, err = parseModelCommand("/model enable openai/gpt-5.1")
	if err != nil {
		t.Fatalf("/model enable refused: %v", err)
	}
	if act.Verb != "model.enable" || act.Ref != "openai/gpt-5.1" {
		t.Errorf("/model enable parsed to verb=%q ref=%q, want model.enable / openai/gpt-5.1",
			act.Verb, act.Ref)
	}

	act, _, err = parseModelCommand("/model disable llama3.1")
	if err != nil {
		t.Fatalf("/model disable refused: %v", err)
	}
	if act.Verb != "model.disable" || act.Ref != "llama3.1" {
		t.Errorf("/model disable parsed to verb=%q ref=%q, want model.disable / llama3.1",
			act.Verb, act.Ref)
	}

	for _, c := range []struct{ line, want string }{
		{"/model", "needs a subcommand"},
		{"/model enable", "exactly one model ref"},
		{"/model enable a b", "exactly one model ref"},
		{"/model wat", "no subcommand"},
	} {
		_, matched, err := parseModelCommand(c.line)
		if !matched {
			t.Fatalf("%q not matched as a /model command; its refusal would fall through "+
				"to the slash menu's silent no-match", c.line)
		}
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("refusal of %q = %v, want substring %q", c.line, err, c.want)
		}
	}
}

// TestProviderListMapsToModelListInProviderView pins the K2 choice that
// /provider list and /model list are one read: both carry the model.list verb,
// distinguished only by ProviderView so the formatter collapses to providers for
// one and lists models for the other. A separate provider.list verb was the
// alternative the proposal rejected, and a parser that invented one here would
// send a request the core does not implement.
func TestProviderListMapsToModelListInProviderView(t *testing.T) {
	act, matched, err := parseProviderCommand("/provider list")
	if !matched || err != nil {
		t.Fatalf("/provider list refused: matched=%v err=%v", matched, err)
	}
	if act.Verb != "model.list" {
		t.Errorf("/provider list parsed to verb %q, want model.list (the single read)", act.Verb)
	}
	if !act.ProviderView {
		t.Error("/provider list did not set ProviderView, so the formatter would list models " +
			"instead of the providers the user asked for")
	}
}

// TestProviderNoticesNeverEchoTheApiKeyEnvValue pins the §20.1 safety at the one
// place the host composes a notice locally rather than from the core: the
// pending notice shown the instant the command is accepted. api_key_env carries a
// variable NAME, never a key -- but a user can paste a key by mistake, and if the
// pending notice interpolated the flag value it would print that key to the
// screen and the scrollback before the core ever refused it. The notice names the
// provider, never the key env value.
//
// The parser deliberately does NOT validate the value (the single enforcement
// site is arxi's validateKeyEnv), so this guards the blast radius of that choice:
// a secret that slips through the parser must still never reach a notice.
func TestProviderNoticesNeverEchoTheApiKeyEnvValue(t *testing.T) {
	secret := "sk-live-0123456789abcdefghij"
	act, _, err := parseProviderCommand("/provider add openai --api-key-env " + secret)
	if err != nil {
		t.Fatalf("add with a secret-shaped api-key-env refused at parse; the parser must "+
			"pass it through to the core's validateKeyEnv, not refuse it here: %v", err)
	}
	// It IS carried on the params (so the core can refuse it); the guard is that
	// it never reaches a user-visible notice.
	if act.Add.APIKeyEnv != secret {
		t.Fatalf("the parser dropped the api-key-env value; the core cannot refuse what it " +
			"never receives")
	}
	if strings.Contains(act.pendingNotice(), secret) {
		t.Errorf("the pending notice echoed the api-key-env value %q; a pasted key would "+
			"print to the screen and scrollback before the core refused it, which is the "+
			"§20.1 leak the TUI must never cause. notice: %q", secret, act.pendingNotice())
	}
}

// TestRunProviderAddFormatsTheSnapshot pins the success path end to end against
// the fake: the params the parser built reach SubmitProviderAdd, and the returned
// snapshot is formatted into a notice that names the provider, its endpoint and
// its enabled count.
func TestRunProviderAddFormatsTheSnapshot(t *testing.T) {
	fake := &fakeProviderManager{
		addResult: &driver.ProviderAddResult{
			Name: "openai", Protocol: "openai-chat", BaseURL: "https://api.openai.com/v1",
			APIKeyEnv: "OPENAI_API_KEY",
			Models: []driver.ProviderModel{
				{ID: "gpt-5.1", Enabled: true},
				{ID: "gpt-5.1-mini", Enabled: false},
			},
		},
	}
	act, _, _ := parseProviderCommand("/provider add openai --api-key-env OPENAI_API_KEY")
	notice := runProviderCmd(context.Background(), fake, act)

	if fake.addSeen.Name != "openai" || fake.addSeen.APIKeyEnv != "OPENAI_API_KEY" {
		t.Errorf("the parsed params did not reach SubmitProviderAdd: saw %+v", fake.addSeen)
	}
	for _, want := range []string{"openai", "https://api.openai.com/v1", "OPENAI_API_KEY", "2 model", "1 enabled"} {
		if !strings.Contains(notice, want) {
			t.Errorf("provider.add notice %q is missing %q", notice, want)
		}
	}
}

// TestRunModelListFormatsBothViews pins the two framings of one read: /model list
// lists each model with its on/off state, /provider list collapses to the
// distinct provider names. The same rows feed both, so the discriminator is
// ProviderView alone.
func TestRunModelListFormatsBothViews(t *testing.T) {
	rows := &driver.ModelListResult{Models: []driver.ModelRow{
		{Provider: "openai", ID: "gpt-5.1", Enabled: true},
		{Provider: "openai", ID: "gpt-5.1-mini", Enabled: false},
		{Provider: "local", ID: "llama3.1", Enabled: true},
	}}
	fake := &fakeProviderManager{listResult: rows}

	modelNotice := runProviderCmd(context.Background(), fake, providerAction{Verb: "model.list"})
	for _, want := range []string{"openai/gpt-5.1 [on]", "openai/gpt-5.1-mini [off]", "local/llama3.1 [on]", "3 model"} {
		if !strings.Contains(modelNotice, want) {
			t.Errorf("/model list notice %q missing %q", modelNotice, want)
		}
	}

	provNotice := runProviderCmd(context.Background(), fake, providerAction{Verb: "model.list", ProviderView: true})
	if !strings.Contains(provNotice, "2 provider") {
		t.Errorf("/provider list notice %q did not collapse to the 2 distinct providers", provNotice)
	}
	// The provider view must not spell out individual model ids -- that is the
	// model view's job, and listing them here is the drift the two views exist to
	// avoid.
	if strings.Contains(provNotice, "gpt-5.1") {
		t.Errorf("/provider list notice %q listed a model id; it should name providers only", provNotice)
	}
}

// TestRunModelListNamesTheEmptyAnswer pins that an empty result reads as "nothing
// registered yet" rather than a blank line that looks like a command that did
// nothing. Both views get their own guidance toward /provider add.
func TestRunModelListNamesTheEmptyAnswer(t *testing.T) {
	fake := &fakeProviderManager{listResult: &driver.ModelListResult{}}

	modelNotice := runProviderCmd(context.Background(), fake, providerAction{Verb: "model.list"})
	if !strings.Contains(modelNotice, "no models") || !strings.Contains(modelNotice, "/provider add") {
		t.Errorf("empty /model list notice %q did not name the empty state and point at /provider add", modelNotice)
	}

	provNotice := runProviderCmd(context.Background(), fake, providerAction{Verb: "model.list", ProviderView: true})
	if !strings.Contains(provNotice, "no providers") {
		t.Errorf("empty /provider list notice %q did not name the empty state", provNotice)
	}
}

// TestRunModelEnableReadsChanged pins that the notice reflects res.Changed: a
// model already in the target state says "was already", a real toggle says "is
// now". Reporting a change that did not happen is how a user concludes the
// setting is broken, the reason the core returns changed at all. It also pins
// that /model disable sends on=false -- a disable that sent on=true would enable
// the model it was asked to turn off.
func TestRunModelEnableReadsChanged(t *testing.T) {
	changed := &fakeProviderManager{enResult: &driver.ModelEnableResult{
		Provider: "openai", Model: "gpt-5.1", Enabled: false, Changed: true}}
	notice := runProviderCmd(context.Background(), changed, providerAction{Verb: "model.disable", Ref: "openai/gpt-5.1"})
	if changed.enOn {
		t.Error("/model disable sent on=true; disable must send on=false or it enables the model it meant to turn off")
	}
	if !strings.Contains(notice, "is now disabled") {
		t.Errorf("a real disable did not report the change: %q", notice)
	}

	already := &fakeProviderManager{enResult: &driver.ModelEnableResult{
		Provider: "openai", Model: "gpt-5.1", Enabled: false, Changed: false}}
	notice = runProviderCmd(context.Background(), already, providerAction{Verb: "model.disable", Ref: "openai/gpt-5.1"})
	if !strings.Contains(notice, "already disabled") {
		t.Errorf("a no-op disable claimed a change it did not make: %q", notice)
	}
}

// TestRunProviderCmdSurfacesRefusals pins that a core refusal (ok:false, surfaced
// by the driver as an error) reaches the notice rather than being swallowed into
// a success that reads as done. This is the path a secret-shaped api_key_env
// takes: the core refuses it, and the user must see why.
func TestRunProviderCmdSurfacesRefusals(t *testing.T) {
	fake := &fakeProviderManager{addErr: fmt.Errorf("provider add refused: api_key_env looks like the key itself")}
	act, _, _ := parseProviderCommand("/provider add openai")
	notice := runProviderCmd(context.Background(), fake, act)
	if !strings.Contains(notice, "refused") {
		t.Errorf("a provider.add refusal did not reach the notice; the user would see nothing "+
			"explaining why the add failed: %q", notice)
	}
}

// --- dispatchProviderCmd: the loop's single entry point and its guards ---

// TestDispatchRefusesWithoutALiveCore pins the mock-driver path: a driver that
// does not satisfy providerManager (the mock, used when ARXI_BIN is unset) must
// be told the feature needs a live core rather than hang a round-trip against a
// driver that cannot answer. busy must stay false so the one notice is the whole
// result.
func TestDispatchRefusesWithoutALiveCore(t *testing.T) {
	busy := false
	done := make(chan providerCmdOutcome, 1)
	mock := &mockDriver{eventCh: make(chan fold.Event, 1)}
	act, _, _ := parseProviderCommand("/provider add openai")

	notice := dispatchProviderCmd(context.Background(), mock, act, nil, &busy, done)
	if !strings.Contains(notice, "ARXI_BIN") {
		t.Errorf("a provider command on the mock driver did not point at ARXI_BIN; the user "+
			"cannot tell the feature needs a live core: %q", notice)
	}
	if busy {
		t.Error("dispatch set busy on a driver that cannot run the command; a stuck busy flag " +
			"would refuse every later command with 'already running'")
	}
}

// TestDispatchRefusesAParseError pins that a parse error short-circuits before
// any capability or gate check: the user's typo is the first thing to report,
// and no worker is launched.
func TestDispatchRefusesAParseError(t *testing.T) {
	busy := false
	done := make(chan providerCmdOutcome, 1)
	fake := &fakeProviderManager{hello: allProviderVerbs()}
	perr := fmt.Errorf("no name given")

	notice := dispatchProviderCmd(context.Background(), fake, providerAction{}, perr, &busy, done)
	if notice != perr.Error() {
		t.Errorf("dispatch did not surface the parse error first: %q", notice)
	}
	if busy {
		t.Error("dispatch set busy on a parse error; nothing was sent, so busy must stay false")
	}
}

// TestDispatchRefusesWhenBusy pins the single-in-flight rule: a second command
// while one is running is refused rather than racing the one response reader.
func TestDispatchRefusesWhenBusy(t *testing.T) {
	busy := true
	done := make(chan providerCmdOutcome, 1)
	fake := &fakeProviderManager{hello: allProviderVerbs()}
	act, _, _ := parseProviderCommand("/provider list")

	notice := dispatchProviderCmd(context.Background(), fake, act, nil, &busy, done)
	if !strings.Contains(notice, "already running") {
		t.Errorf("a second command while busy was not refused: %q", notice)
	}
}

// TestDispatchGatesOnTheHello pins that a core missing a provider verb is refused
// at dispatch, before a worker sends a request the core answers not_implemented.
// The hello here declares the verbs but implements none, the declared-but-
// unimplemented trap requireProviderVerbs catches.
func TestDispatchGatesOnTheHello(t *testing.T) {
	busy := false
	done := make(chan providerCmdOutcome, 1)
	fake := &fakeProviderManager{hello: &driver.Hello{
		Type:        "hello",
		Types:       providerVerbs,
		Implemented: []string{"schema"},
	}}
	act, _, _ := parseProviderCommand("/provider list")

	notice := dispatchProviderCmd(context.Background(), fake, act, nil, &busy, done)
	if !strings.Contains(notice, "not_implemented") {
		t.Errorf("dispatch did not gate on the hello; a command would be sent to a core that "+
			"answers not_implemented and the user would wait on nothing: %q", notice)
	}
	if busy {
		t.Error("dispatch set busy on a gated command; no worker ran, so busy must stay false")
	}
}

// TestDispatchLaunchesTheWorkerOnSuccess pins the happy path: a parsed command on
// a fully wired core sets busy, shows the pending notice, and the worker delivers
// the formatted result on done. Reading done is what proves the goroutine ran and
// used the capability it was handed.
func TestDispatchLaunchesTheWorkerOnSuccess(t *testing.T) {
	busy := false
	done := make(chan providerCmdOutcome, 1)
	fake := &fakeProviderManager{
		hello:      allProviderVerbs(),
		listResult: &driver.ModelListResult{Models: []driver.ModelRow{{Provider: "openai", ID: "gpt-5.1", Enabled: true}}},
	}
	act, _, _ := parseProviderCommand("/provider list")

	notice := dispatchProviderCmd(context.Background(), fake, act, nil, &busy, done)
	if !busy {
		t.Fatal("dispatch did not set busy before launching the worker; a second command could " +
			"start and race the response reader")
	}
	if !strings.Contains(notice, "reading providers") {
		t.Errorf("dispatch did not show the pending notice: %q", notice)
	}

	out := <-done
	if !strings.Contains(out.notice, "1 provider") {
		t.Errorf("the worker's outcome did not carry the formatted result: %q", out.notice)
	}
}
