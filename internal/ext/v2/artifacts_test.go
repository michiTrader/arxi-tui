//go:build !windows

package extv2

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
)

const autoHead = `"protocol":"ext/v2","id":"demo","name":"Demo","version":"1"`

func autoDoc(perms, body string) string {
	if perms == "" {
		perms = `{"exec":["sh","echo"],"net":["api.example.com"]}`
	}
	return `{` + autoHead + `,"permissions":` + perms + `,` + body + `}`
}

const sayOp = `{"kind":"exec","argv":["echo","hi"]}`

func TestAnEventNamesWhatItReactsToAndIsRefusedOtherwise(t *testing.T) {
	ok := autoDoc("", `"events":[{"id":"done","on":"turn.done","op":`+sayOp+`}]`)
	p := accepted(t, ok, Options{})
	if len(p.Events) != 1 || p.Events[0].On != EventTurnDone {
		t.Fatalf("parsed %+v", p.Events)
	}
	cases := []struct {
		name, ev string
		want     []string
	}{
		{"unknown event", `{"id":"a","on":"run.started","op":` + sayOp + `}`, []string{"not something a package can react to", "turn.done"}},
		{"no event", `{"id":"a","op":` + sayOp + `}`, []string{"not something a package can react to"}},
		{"bad id", `{"id":"A b","on":"turn.done","op":` + sayOp + `}`, []string{"not usable"}},
		{"is on a turn", `{"id":"a","on":"turn.done","is":"x","op":` + sayOp + `}`, []string{"not a setting", "remove"}},
		{"placeholder", `{"id":"a","on":"turn.done","op":{"kind":"exec","argv":["echo","${args}"]}}`, []string{"nothing to fill it", "by itself"}},
		{"input placeholder", `{"id":"a","on":"turn.done","op":{"kind":"http","url":"https://api.example.com/${input}"}}`, []string{"nothing to fill it"}},
		{"send result", `{"id":"a","on":"turn.done","op":{"kind":"exec","argv":["echo"],"result":"send"}}`, []string{"not available here", "say"}},
		{"outcome result", `{"id":"a","on":"turn.done","op":{"kind":"exec","argv":["echo"],"result":"outcome"}}`, []string{"not available here"}},
		{"transform result", `{"id":"a","on":"turn.done","op":{"kind":"exec","argv":["echo"],"result":"transform"}}`, []string{"not available here"}},
		{"program not declared", `{"id":"a","on":"turn.done","op":{"kind":"exec","argv":["git"]}}`, []string{"permissions.exec"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			refused(t, autoDoc("", `"events":[`+c.ev+`]`), Options{}, c.want...)
		})
	}
	refused(t, autoDoc("", `"events":[`+`{"id":"a","on":"turn.done","op":`+sayOp+`},{"id":"a","on":"turn.failed","op":`+sayOp+`}]`), Options{}, "defined twice")
}

func TestASettingEventIsNarrowedToAValueTheHostKnows(t *testing.T) {
	vals := Options{EventValues: func(on string) ([]string, bool) {
		if on == "effort" {
			return []string{"low", "high", "none"}, true
		}
		return nil, false
	}}
	doc := func(is string) string {
		return autoDoc("", `"events":[{"id":"a","on":"effort","is":"`+is+`","op":`+sayOp+`}]`)
	}
	accepted(t, doc("high"), vals)
	refused(t, doc("max"), vals, `effort is never "max"`, "low, high, none")
	// Without the host's list the pure validator cannot judge the value, and says nothing.
	accepted(t, doc("max"), Options{})
	// Any change, narrowed to nothing, is fine.
	accepted(t, autoDoc("", `"events":[{"id":"a","on":"mode","op":`+sayOp+`}]`), vals)
}

func TestAPanelIsOneLineWithBoundedRefresh(t *testing.T) {
	op := `"op":` + sayOp
	accepted(t, autoDoc("", `"panels":[{"id":"clock","title":"Clock",`+op+`}]`), Options{})
	accepted(t, autoDoc("", `"panels":[{"id":"clock","title":"Clock","refresh":"turn.done","every_s":30,`+op+`}]`), Options{})
	cases := []struct {
		name, pn string
		want     []string
	}{
		{"no title", `{"id":"c",` + op + `}`, []string{"title is empty"}},
		{"long title", `{"id":"c","title":"` + strings.Repeat("x", MaxPanelTitle+1) + `",` + op + `}`, []string{"longer than"}},
		{"control in title", `{"id":"c","title":"a\u001b[2Jb",` + op + `}`, []string{"control character"}},
		{"bad id", `{"id":"C c","title":"T",` + op + `}`, []string{"not usable"}},
		{"bad refresh", `{"id":"c","title":"T","refresh":"always",` + op + `}`, []string{"not understood", "turn.done"}},
		{"too fast", `{"id":"c","title":"T","every_s":29,` + op + `}`, []string{"outside 30..3600", "all the time"}},
		{"too slow", `{"id":"c","title":"T","every_s":3601,` + op + `}`, []string{"outside 30..3600"}},
		{"negative", `{"id":"c","title":"T","every_s":-5,` + op + `}`, []string{"outside 30..3600"}},
		{"refresh without op", `{"id":"c","title":"T","refresh":"turn.done"}`, []string{"no op", "refresh and every_s"}},
		{"placeholder", `{"id":"c","title":"T","op":{"kind":"exec","argv":["echo","${1}"]}}`, []string{"nothing to fill it"}},
		{"send", `{"id":"c","title":"T","op":{"kind":"exec","argv":["echo"],"result":"send"}}`, []string{"not available here"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) { refused(t, autoDoc("", `"panels":[`+c.pn+`]`), Options{}, c.want...) })
	}
	refused(t, autoDoc("", `"panels":[{"id":"c","title":"T",`+op+`},{"id":"c","title":"U",`+op+`}]`), Options{}, "defined twice")
	// A panel nobody fills is a line that stays empty, which is a mistake to report.
	refused(t, autoDoc("", `"panels":[{"id":"c","title":"T"}]`), Options{}, "no op and no service feeds it")
}

func TestAServiceIsAnExecThatRunsUntilStoppedAndFeedsAtMostOnePanel(t *testing.T) {
	svc := `{"name":"watch","description":"Watch","op":{"kind":"exec","argv":["echo","x"]}}`
	accepted(t, autoDoc("", `"services":[`+svc+`]`), Options{})
	// feeding a panel that has no op of its own
	accepted(t, autoDoc("", `"panels":[{"id":"p","title":"P"}],"services":[{"name":"watch","description":"W","feed":"p","op":{"kind":"exec","argv":["echo"]}}]`), Options{})

	cases := []struct {
		name, body string
		want       []string
	}{
		{"http service", `"services":[{"name":"w","description":"d","op":{"kind":"http","url":"https://api.example.com/x"}}]`, []string{"op is \"exec\""}},
		{"builtin service", `"services":[{"name":"w","description":"d","op":{"kind":"builtin","name":"echo"}}]`, []string{"op is \"exec\""}},
		{"no description", `"services":[{"name":"w","op":{"kind":"exec","argv":["echo"]}}]`, []string{"description is empty"}},
		{"bad name", `"services":[{"name":"W w","description":"d","op":{"kind":"exec","argv":["echo"]}}]`, []string{"not usable", "/service start"}},
		{"timeout", `"services":[{"name":"w","description":"d","op":{"kind":"exec","argv":["echo"],"timeout_ms":100}}]`, []string{"timeout_ms", "runs until"}},
		{"extract", `"services":[{"name":"w","description":"d","op":{"kind":"exec","argv":["echo"],"extract":"a.b"}}]`, []string{"extract"}},
		{"placeholder", `"services":[{"name":"w","description":"d","op":{"kind":"exec","argv":["echo","${args}"]}}]`, []string{"nothing to fill it"}},
		{"send", `"services":[{"name":"w","description":"d","op":{"kind":"exec","argv":["echo"],"result":"send"}}]`, []string{"not available here"}},
		{"feeds nothing", `"services":[{"name":"w","description":"d","feed":"ghost","op":{"kind":"exec","argv":["echo"]}}]`, []string{`"ghost" is not a panel`}},
		{"feeds a panel with an op", `"panels":[{"id":"p","title":"P","op":` + sayOp + `}],"services":[{"name":"w","description":"d","feed":"p","op":{"kind":"exec","argv":["echo"]}}]`, []string{"op of its own", "overwrite"}},
		{"two feed one", `"panels":[{"id":"p","title":"P"}],"services":[{"name":"a","description":"d","feed":"p","op":{"kind":"exec","argv":["echo"]}},{"name":"b","description":"d","feed":"p","op":{"kind":"exec","argv":["echo"]}}]`, []string{"already filled by service a"}},
		{"duplicate", `"services":[` + svc + `,` + svc + `]`, []string{"defined twice"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) { refused(t, autoDoc("", c.body), Options{}, c.want...) })
	}
}

func TestTheLimitsOnEventsPanelsAndServicesAreExact(t *testing.T) {
	many := func(n int, one func(i int) string) string {
		var parts []string
		for i := 0; i < n; i++ {
			parts = append(parts, one(i))
		}
		return strings.Join(parts, ",")
	}
	ev := func(i int) string { return `{"id":"e` + string(rune('a'+i)) + `","on":"turn.done","op":` + sayOp + `}` }
	accepted(t, autoDoc("", `"events":[`+many(MaxEvents, ev)+`]`), Options{})
	refused(t, autoDoc("", `"events":[`+many(MaxEvents+1, ev)+`]`), Options{}, "the most is 16")
	pn := func(i int) string { return `{"id":"p` + string(rune('a'+i)) + `","title":"T","op":` + sayOp + `}` }
	accepted(t, autoDoc("", `"panels":[`+many(MaxPanels, pn)+`]`), Options{})
	refused(t, autoDoc("", `"panels":[`+many(MaxPanels+1, pn)+`]`), Options{}, "the most is 8")
	sv := func(i int) string {
		return `{"name":"s` + string(rune('a'+i)) + `","description":"d","op":{"kind":"exec","argv":["echo"]}}`
	}
	accepted(t, autoDoc("", `"services":[`+many(MaxServices, sv)+`]`), Options{})
	refused(t, autoDoc("", `"services":[`+many(MaxServices+1, sv)+`]`), Options{}, "the most is 4")
}

func TestConsentTextNamesWhatRunsByItself(t *testing.T) {
	p := accepted(t, autoDoc("", `"events":[{"id":"a","on":"turn.done","op":`+sayOp+`},{"id":"b","on":"turn.done","op":`+sayOp+`}],
"panels":[{"id":"p","title":"Clock","op":`+sayOp+`}],
"services":[{"name":"w","description":"d","op":{"kind":"exec","argv":["echo"]}}]`), Options{})
	all := strings.Join(p.Capabilities(), "\n")
	for _, want := range []string{"react by itself, with nothing typed, when turn.done happens", "keep a line on your screen (Clock)", "keeps running after you start it with /service start (w)"} {
		if !strings.Contains(all, want) {
			t.Errorf("the consent text lacks %q:\n%s", want, all)
		}
	}
	if strings.Count(all, "turn.done") != 1 {
		t.Errorf("one event kind named twice in the consent text:\n%s", all)
	}
}

func TestDigestCoversTheNewArtifactsSoEditingOneAsksAgain(t *testing.T) {
	base := autoDoc("", `"events":[{"id":"a","on":"turn.done","op":`+sayOp+`}]`)
	edited := autoDoc("", `"events":[{"id":"a","on":"turn.failed","op":`+sayOp+`}]`)
	if accepted(t, base, Options{}).Digest() == accepted(t, edited, Options{}).Digest() {
		t.Fatal("changing what an event reacts to must change the digest, or an old approval would cover it")
	}
	pb := autoDoc("", `"panels":[{"id":"p","title":"A","op":`+sayOp+`}]`)
	pe := autoDoc("", `"panels":[{"id":"p","title":"B","op":`+sayOp+`}]`)
	if accepted(t, pb, Options{}).Digest() == accepted(t, pe, Options{}).Digest() {
		t.Fatal("changing a panel must change the digest")
	}
	sb := autoDoc("", `"services":[{"name":"w","description":"d","op":{"kind":"exec","argv":["echo","a"]}}]`)
	se := autoDoc("", `"services":[{"name":"w","description":"d","op":{"kind":"exec","argv":["echo","b"]}}]`)
	if accepted(t, sb, Options{}).Digest() == accepted(t, se, Options{}).Digest() {
		t.Fatal("changing what a service runs must change the digest")
	}
}

func TestShippedProgramsInEventsPanelsAndServicesAreNamedInTheConsentText(t *testing.T) {
	for name, body := range map[string]string{
		"event":   `"events":[{"id":"a","on":"turn.done","op":{"kind":"exec","argv":["./x.sh"]}}]`,
		"panel":   `"panels":[{"id":"p","title":"T","op":{"kind":"exec","argv":["./x.sh"]}}]`,
		"service": `"services":[{"name":"w","description":"d","op":{"kind":"exec","argv":["./x.sh"]}}]`,
	} {
		p := accepted(t, autoDoc(`{}`, body), Options{})
		if !strings.Contains(strings.Join(p.Capabilities(), "\n"), "run programs shipped inside the package") {
			t.Errorf("%s: a script shipped in the package is a power the user must be told about", name)
		}
	}
}

// ---- running ----

func TestRunEventAndRunPanelReturnOnlyText(t *testing.T) {
	r, _ := pkgDir(t, map[string]string{
		"say.sh":  "#!/bin/sh\nprintf 'line one\\nline two\\n'\n",
		"json.sh": "#!/bin/sh\nprintf '{\"verdict\":\"transform\",\"text\":\"x\"}'\n",
	})
	p := accepted(t, autoDoc(`{}`, `"events":[{"id":"e","on":"turn.done","op":{"kind":"exec","argv":["./say.sh"]}}],
"panels":[{"id":"p","title":"P","op":{"kind":"exec","argv":["./say.sh"]}}]`), Options{})
	out, err := r.RunEvent(context.Background(), p, p.Events[0])
	if err != nil || out != "line one\nline two" {
		t.Fatalf("event = %q, %v", out, err)
	}
	line, err := r.RunPanel(context.Background(), p, p.Panels[0])
	if err != nil || line != "line one" {
		t.Fatalf("a panel is one line; got %q, %v", line, err)
	}
}

func TestRunPanelWithoutAnOpIsAnErrorNotAnEmptyLine(t *testing.T) {
	r := &Runner{}
	p := accepted(t, autoDoc("", `"panels":[{"id":"p","title":"P"}],"services":[{"name":"w","description":"d","feed":"p","op":{"kind":"exec","argv":["echo"]}}]`), Options{})
	if _, err := r.RunPanel(context.Background(), p, p.Panels[0]); err == nil || !strings.Contains(err.Error(), "service fills it") {
		t.Fatalf("got %v", err)
	}
}

func TestAnAutomaticOperationThatReturnsMoreThanTextIsRefusedAtRunTime(t *testing.T) {
	// A document that skipped the validator (or a validator bug) must still not let
	// an event send or rewrite: the runner checks again.
	r := &Runner{Builtins: map[string]BuiltinFunc{
		"sneaky": func(context.Context, Input) (Outcome, error) { return Outcome{Say: "ok", Send: "do this"}, nil },
		"verdict": func(context.Context, Input) (Outcome, error) {
			return Outcome{Verdict: VerdictBlock, Reason: "x"}, nil
		},
	}}
	p := &Package{Protocol: Protocol, ID: "demo"}
	for _, name := range []string{"sneaky", "verdict"} {
		e := Event{ID: "e", On: EventTurnDone, Op: Op{Kind: KindBuiltin, Name: name}}
		if _, err := r.RunEvent(context.Background(), p, e); !errors.Is(err, ErrNotSigned) {
			t.Errorf("event %s: got %v, want ErrNotSigned", name, err)
		}
		pn := Panel{ID: "p", Title: "T", Op: &Op{Kind: KindBuiltin, Name: name}}
		if _, err := r.RunPanel(context.Background(), p, pn); !errors.Is(err, ErrNotSigned) {
			t.Errorf("panel %s: got %v, want ErrNotSigned", name, err)
		}
	}
}

func TestPanelLineIsOneCleanBoundedLine(t *testing.T) {
	if got := PanelLine("  first \x1b[31mred\x1b[0m\nsecond"); got != "first red" {
		t.Fatalf("got %q", got)
	}
	long := PanelLine(strings.Repeat("é", MaxPanelRunes+50))
	if n := len([]rune(long)); n > MaxPanelRunes {
		t.Fatalf("a panel line is %d runes, the most is %d", n, MaxPanelRunes)
	}
	if PanelLine("\n\n") != "" {
		t.Fatal("blank text is an empty line")
	}
}

// ---- services ----

type lineSink struct {
	mu    sync.Mutex
	lines []string
}

func (s *lineSink) add(l string) { s.mu.Lock(); s.lines = append(s.lines, l); s.mu.Unlock() }
func (s *lineSink) get() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.lines...)
}

func svcPkg(t *testing.T, script string) (*Runner, *Package, Service) {
	t.Helper()
	r, _ := pkgDir(t, map[string]string{"svc.sh": script})
	p := accepted(t, autoDoc(`{}`, `"services":[{"name":"w","description":"d","op":{"kind":"exec","argv":["./svc.sh"]}}]`), Options{})
	return r, p, p.Services[0]
}

func waitDone(t *testing.T, h *ServiceHandle, d time.Duration) {
	t.Helper()
	select {
	case <-h.Done():
	case <-time.After(d):
		t.Fatal("the service did not end in time")
	}
}

func TestAServiceDeliversItsLinesCleanedAndEndsWhenTheProgramDoes(t *testing.T) {
	r, p, sv := svcPkg(t, "#!/bin/sh\nprintf 'one\\n'\nprintf '\\033[31mtwo\\033[0m\\r\\n'\nprintf '\\n'\nprintf 'three'\n")
	var sink lineSink
	h, err := r.StartService(context.Background(), p, sv, sink.add)
	if err != nil {
		t.Fatal(err)
	}
	waitDone(t, h, 5*time.Second)
	if err := h.Err(); err != nil {
		t.Fatalf("a clean exit is not an error: %v", err)
	}
	got := strings.Join(sink.get(), "|")
	if got != "one|two|three" {
		t.Fatalf("lines = %q (colour stripped, blank lines skipped, the last line without a newline kept)", got)
	}
}

func TestAServiceThatFailsReportsWhyOnceItHasEnded(t *testing.T) {
	r, p, sv := svcPkg(t, "#!/bin/sh\necho oops >&2\nexit 3\n")
	h, err := r.StartService(context.Background(), p, sv, func(string) {})
	if err != nil {
		t.Fatal(err)
	}
	waitDone(t, h, 5*time.Second)
	if err := h.Err(); err == nil || !strings.Contains(err.Error(), "stopped on its own") || !strings.Contains(err.Error(), "oops") {
		t.Fatalf("got %v", err)
	}
}

func TestStopEndsAServiceAndEveryHelperItStarted(t *testing.T) {
	dir := t.TempDir()
	marker := dir + "/alive"
	// The program starts a helper that would touch a file after a pause. Stopping the
	// service must stop the helper too, or "stopped" would be a lie.
	script := "#!/bin/sh\n(sleep 1; touch " + marker + ") &\nprintf 'up\\n'\nsleep 30\n"
	r, p, sv := svcPkg(t, script)
	var sink lineSink
	h, err := r.StartService(context.Background(), p, sv, sink.add)
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(3 * time.Second)
	for len(sink.get()) == 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if len(sink.get()) == 0 {
		t.Fatal("the service printed nothing")
	}
	h.Stop()
	h.Stop() // twice is harmless
	waitDone(t, h, 5*time.Second)
	if err := h.Err(); err != nil {
		t.Fatalf("a service the user stopped did not fail: %v", err)
	}
	time.Sleep(1500 * time.Millisecond)
	if _, err := osStat(marker); err == nil {
		t.Fatal("the helper outlived the service: the whole process group must be stopped")
	}
}

func TestACancelledContextEndsTheServiceToo(t *testing.T) {
	r, p, sv := svcPkg(t, "#!/bin/sh\nsleep 30\n")
	ctx, cancel := context.WithCancel(context.Background())
	h, err := r.StartService(ctx, p, sv, func(string) {})
	if err != nil {
		t.Fatal(err)
	}
	cancel()
	waitDone(t, h, 5*time.Second)
}

func TestAChattyServiceIsRationedAndTheDroppedLinesAreCounted(t *testing.T) {
	r, p, sv := svcPkg(t, "#!/bin/sh\ni=0\nwhile [ $i -lt 400 ]; do echo line$i; i=$((i+1)); done\n")
	var sink lineSink
	h, err := r.StartService(context.Background(), p, sv, sink.add)
	if err != nil {
		t.Fatal(err)
	}
	waitDone(t, h, 10*time.Second)
	shown, dropped := len(sink.get()), h.Dropped()
	if shown+int(dropped) != 400 {
		t.Fatalf("every line is either shown or counted as dropped: %d + %d != 400", shown, dropped)
	}
	if shown > ServiceBurst+10 || dropped == 0 {
		t.Fatalf("400 lines in an instant must be rationed to about the burst of %d: shown %d, dropped %d", ServiceBurst, shown, dropped)
	}
}

func TestALongLineIsCutAndDoesNotBreakTheNextOne(t *testing.T) {
	r, p, sv := svcPkg(t, "#!/bin/sh\nhead -c 20000 /dev/zero | tr '\\0' 'a'\nprintf '\\nnext\\n'\n")
	var sink lineSink
	h, err := r.StartService(context.Background(), p, sv, sink.add)
	if err != nil {
		t.Fatal(err)
	}
	waitDone(t, h, 5*time.Second)
	lines := sink.get()
	if len(lines) != 2 || lines[1] != "next" {
		t.Fatalf("lines = %d, last %q: the rest of a long line must be discarded, not read as a new line", len(lines), lines[len(lines)-1])
	}
	if n := len([]rune(lines[0])); n > MaxServiceLineRunes {
		t.Fatalf("a line of %d runes, the most is %d", n, MaxServiceLineRunes)
	}
}

func TestAServiceDoesNotInheritTheHostEnvironment(t *testing.T) {
	r, p, sv := svcPkg(t, "#!/bin/sh\nprintf 'key=%s\\n' \"$SECRET_KEY\"\n")
	r.exec.environ = func() []string { return []string{"PATH=/usr/bin:/bin", "SECRET_KEY=hunter2"} }
	var sink lineSink
	h, err := r.StartService(context.Background(), p, sv, sink.add)
	if err != nil {
		t.Fatal(err)
	}
	waitDone(t, h, 5*time.Second)
	if got := strings.Join(sink.get(), "|"); got != "key=" {
		t.Fatalf("the service saw the host's secret: %q", got)
	}
}

func TestAServiceCannotStartAProgramTheUserWasNotTold(t *testing.T) {
	r := &Runner{}
	p := &Package{Protocol: Protocol, ID: "demo", Permissions: Permissions{Exec: []string{"echo"}}}
	sv := Service{Name: "w", Description: "d", Op: Op{Kind: KindExec, Argv: []string{"rm", "-rf", "x"}}}
	if _, err := r.StartService(context.Background(), p, sv, func(string) {}); err == nil || !strings.Contains(err.Error(), "permissions.exec") {
		t.Fatalf("got %v", err)
	}
	sv.Op = Op{Kind: KindHTTP, URL: "https://api.example.com/"}
	if _, err := r.StartService(context.Background(), p, sv, func(string) {}); err == nil {
		t.Fatal("a service that is not an exec must not start")
	}
	sv.Op = Op{Kind: KindExec, Argv: []string{"echo", "${args}"}}
	if _, err := r.StartService(context.Background(), p, sv, func(string) {}); err == nil || !strings.Contains(err.Error(), "placeholder") {
		t.Fatalf("a placeholder in a service must be refused at run time too, got %v", err)
	}
}

func TestTheTokenBucketRefillsWithTime(t *testing.T) {
	now := time.Unix(1000, 0)
	b := newBucket(3, 2, func() time.Time { return now })
	for i := 0; i < 3; i++ {
		if !b.take() {
			t.Fatalf("burst token %d refused", i)
		}
	}
	if b.take() {
		t.Fatal("past the burst, nothing is left")
	}
	now = now.Add(500 * time.Millisecond) // 2/s -> 1 token
	if !b.take() || b.take() {
		t.Fatal("half a second at 2 per second is exactly one token")
	}
	now = now.Add(time.Hour)
	for i := 0; i < 3; i++ {
		if !b.take() {
			t.Fatalf("after a long rest the bucket is full again (%d)", i)
		}
	}
	if b.take() {
		t.Fatal("a long rest never stores more than the burst")
	}
}
