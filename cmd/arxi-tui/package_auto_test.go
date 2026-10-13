//go:build !windows

package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// autoRig is a packageAuto over approved packages written to a temporary folder, with
// every message it sends to the loop collected.
type autoRig struct {
	a   *packageAuto
	mu  sync.Mutex
	got []autoMsg
	set packageSet
	cwd string
}

// newAutoRig writes the packages (folders: "id/package.json" is the document, a name
// ending in .sh is executable), approves the ids, and loads them into a packageAuto.
func newAutoRig(t *testing.T, folders map[string]string, approve []string) *autoRig {
	t.Helper()
	cfg := t.TempDir()
	t.Setenv(configDirEnv, cfg)
	for name, body := range folders {
		mode := os.FileMode(0o644)
		if strings.HasSuffix(name, ".sh") {
			mode = 0o755
		}
		write(t, filepath.Join(cfg, packagesDirName, filepath.FromSlash(name)), body, mode)
	}
	cwd, _ := os.Getwd()
	set := loadAllPackages(cwd, behaviour{})
	for _, id := range approve {
		p, ok := set.find(id)
		if !ok {
			t.Fatalf("package %q did not load: %s", id, problemsText(set))
		}
		if err := grantPackage(p, cwd); err != nil {
			t.Fatal(err)
		}
	}
	set = loadAllPackages(cwd, behaviour{})
	ctx, cancel := context.WithCancel(context.Background())
	out := make(chan autoMsg, 64)
	r := &autoRig{set: set, cwd: cwd}
	r.a = newPackageAuto(ctx, out)
	go func() {
		for {
			select {
			case m := <-out:
				r.mu.Lock()
				r.got = append(r.got, m)
				r.mu.Unlock()
			case <-ctx.Done():
				return
			}
		}
	}()
	t.Cleanup(func() {
		r.a.stopAll()
		cancel()
	})
	r.a.reload(set, newPackageRunner(set))
	return r
}

func (r *autoRig) messages() []autoMsg {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]autoMsg(nil), r.got...)
}

func (r *autoRig) notes() string {
	var b strings.Builder
	for _, m := range r.messages() {
		b.WriteString(m.Note + "|")
	}
	return b.String()
}

func (r *autoRig) warns() string {
	var b strings.Builder
	for _, m := range r.messages() {
		b.WriteString(m.Warn + "|")
	}
	return b.String()
}

func waitUntil(t *testing.T, what string, ok func() bool) {
	t.Helper()
	for i := 0; i < 500; i++ {
		if ok() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

func execPkg(id, perms, body string) string {
	return pkgDoc(id, `,"permissions":{"exec":["sh"]}`+perms+`,`+body)
}

func eventPkg(on, extra, script string) map[string]string {
	return map[string]string{
		"ev/package.json": execPkg("ev", "", `"events":[{"id":"e","on":"`+on+`"`+extra+`,"op":{"kind":"exec","argv":["./e.sh"]}}]`),
		"ev/e.sh":         "#!/bin/sh\n" + script + "\n",
	}
}

// A turn ending fires turn.done events, and what they say is a note of the host's.
func TestATurnEndingFiresTheTurnDoneEvents(t *testing.T) {
	r := newAutoRig(t, eventPkg("turn.done", "", "printf 'all done'"), []string{"ev"})
	r.a.turnDone()
	waitUntil(t, "the event's note", func() bool { return strings.Contains(r.notes(), "ev/e: all done") })
	// turn.failed is another event and must not have run it.
	before := len(r.messages())
	r.a.turnFailed()
	time.Sleep(150 * time.Millisecond)
	if n := len(r.messages()); n != before {
		t.Errorf("a failed turn fired a turn.done event (%d new messages)", n-before)
	}
}

func TestAFailedTurnFiresTheTurnFailedEvents(t *testing.T) {
	r := newAutoRig(t, eventPkg("turn.failed", "", "printf 'it broke'"), []string{"ev"})
	r.a.turnFailed()
	waitUntil(t, "the event's note", func() bool { return strings.Contains(r.notes(), "it broke") })
	before := len(r.messages())
	r.a.turnDone()
	time.Sleep(150 * time.Millisecond)
	if n := len(r.messages()); n != before {
		t.Errorf("a finished turn fired a turn.failed event")
	}
}

// A setting event reacts to a change, narrowed to the value it names.
func TestASettingEventReactsOnlyToItsValue(t *testing.T) {
	r := newAutoRig(t, eventPkg("effort", `,"is":"high"`, "printf 'thinking hard'"), []string{"ev"})
	r.a.fire("effort", "low")
	time.Sleep(150 * time.Millisecond)
	if len(r.messages()) != 0 {
		t.Fatalf("a change to another value fired it: %v", r.messages())
	}
	r.a.fire("effort", "high")
	waitUntil(t, "the event's note", func() bool { return strings.Contains(r.notes(), "thinking hard") })
	// Another setting with the same value is not this event.
	n := len(r.messages())
	r.a.fire("mode", "high")
	time.Sleep(150 * time.Millisecond)
	if len(r.messages()) != n {
		t.Error("a change of another setting fired it")
	}
}

// An unapproved package, a switched-off event and a revoked package do nothing.
func TestOnlyApprovedPackagesReact(t *testing.T) {
	r := newAutoRig(t, eventPkg("turn.done", "", "printf 'ran'"), nil)
	r.a.turnDone()
	time.Sleep(200 * time.Millisecond)
	if len(r.messages()) != 0 {
		t.Fatalf("a package the user did not approve ran: %v", r.messages())
	}
}

// What an event prints is cleaned and bounded by the runner, and a failing one is a
// warning that names the package, not a crash.
func TestAnEventThatFailsIsAWarningNamingIt(t *testing.T) {
	r := newAutoRig(t, eventPkg("turn.done", "", "echo nope >&2; exit 2"), []string{"ev"})
	r.a.turnDone()
	waitUntil(t, "the warning", func() bool { return strings.Contains(r.warns(), "ev/e") })
}

// A slow event is not started again on top of itself.
func TestASlowEventIsNotStartedTwice(t *testing.T) {
	dir := t.TempDir()
	count := filepath.Join(dir, "n")
	r := newAutoRig(t, eventPkg("turn.done", "", "echo x >> "+count+"; sleep 1; printf done"), []string{"ev"})
	r.a.turnDone()
	r.a.turnDone()
	r.a.turnDone()
	waitUntil(t, "the note", func() bool { return strings.Contains(r.notes(), "done") })
	b, _ := os.ReadFile(count)
	if n := strings.Count(string(b), "x"); n != 1 {
		t.Errorf("the event ran %d times for 3 turns that ended while it was running, want 1", n)
	}
}

const panelPkgBody = `"panels":[{"id":"p","title":"Clock","refresh":"turn.done","op":{"kind":"exec","argv":["./p.sh"]}}]`

func panelPkg(script string) map[string]string {
	return map[string]string{
		"pn/package.json": execPkg("pn", "", panelPkgBody),
		"pn/p.sh":         "#!/bin/sh\n" + script + "\n",
	}
}

// A panel runs when the package loads, again after each turn, and its line is what the
// bar shows, "Title: text".
func TestAPanelShowsItsLineAndRefreshesAfterATurn(t *testing.T) {
	counter := filepath.Join(t.TempDir(), "c")
	r := newAutoRig(t, panelPkg("echo x >> "+counter+"; printf 'run %s' \"$(wc -l < "+counter+" | tr -d ' ')\""), []string{"pn"})
	waitUntil(t, "the first line", func() bool { return r.a.panelsLine() == "Clock: run 1" })
	r.a.turnDone()
	waitUntil(t, "the refreshed line", func() bool { return r.a.panelsLine() == "Clock: run 2" })
}

// A panel with no refresh keeps what it said at load through a turn.
func TestAPanelWithoutRefreshIsNotRunAfterATurn(t *testing.T) {
	counter := filepath.Join(t.TempDir(), "c")
	files := panelPkg("echo x >> " + counter + "; printf 'once'")
	files["pn/package.json"] = execPkg("pn", "", `"panels":[{"id":"p","title":"Clock","op":{"kind":"exec","argv":["./p.sh"]}}]`)
	r := newAutoRig(t, files, []string{"pn"})
	waitUntil(t, "the line", func() bool { return r.a.panelsLine() == "Clock: once" })
	r.a.turnDone()
	time.Sleep(250 * time.Millisecond)
	b, _ := os.ReadFile(counter)
	if n := strings.Count(string(b), "x"); n != 1 {
		t.Errorf("the panel ran %d times, want 1", n)
	}
}

// A timed panel refreshes when its time has come and not before.
func TestATimedPanelRefreshesWhenDue(t *testing.T) {
	counter := filepath.Join(t.TempDir(), "c")
	files := panelPkg("echo x >> " + counter + "; printf 'tick'")
	files["pn/package.json"] = execPkg("pn", "", `"panels":[{"id":"p","title":"T","every_s":30,"op":{"kind":"exec","argv":["./p.sh"]}}]`)
	r := newAutoRig(t, files, []string{"pn"})
	now := time.Now()
	r.a.now = func() time.Time { return now }
	r.a.reload(r.set, newPackageRunner(r.set))
	waitUntil(t, "the first line", func() bool { return r.a.panelsLine() == "T: tick" })
	runs := func() int { b, _ := os.ReadFile(counter); return strings.Count(string(b), "x") }
	base := runs()
	if !r.a.hasTimed() {
		t.Fatal("a panel with every_s must ask for a clock")
	}
	now = now.Add(10 * time.Second)
	r.a.tick()
	time.Sleep(150 * time.Millisecond)
	if runs() != base {
		t.Error("it refreshed before its time")
	}
	now = now.Add(25 * time.Second)
	r.a.tick()
	waitUntil(t, "the due refresh", func() bool { return runs() == base+1 })
}

// A failing panel says so once, not on every refresh.
func TestAFailingPanelWarnsOnceAndShowsFailed(t *testing.T) {
	r := newAutoRig(t, panelPkg("echo bad >&2; exit 1"), []string{"pn"})
	waitUntil(t, "the failed line", func() bool { return r.a.panelsLine() == "Clock: (failed)" })
	r.a.turnDone()
	r.a.turnDone()
	time.Sleep(300 * time.Millisecond)
	if n := strings.Count(r.warns(), "pn/p"); n != 1 {
		t.Errorf("warned %d times for the same failure, want 1: %q", n, r.warns())
	}
}

// Revoking the package takes its panel off the bar.
func TestARevokedPackageLosesItsPanel(t *testing.T) {
	r := newAutoRig(t, panelPkg("printf 'here'"), []string{"pn"})
	waitUntil(t, "the line", func() bool { return r.a.panelsLine() != "" })
	if _, err := revokePackage(scopeUser, r.cwd, "pn"); err != nil {
		t.Fatal(err)
	}
	set := loadAllPackages(r.cwd, behaviour{})
	r.a.reload(set, newPackageRunner(set))
	if got := r.a.panelsLine(); got != "" {
		t.Errorf("a revoked package still shows %q", got)
	}
}

// ---- services ----------------------------------------------------------------

func servicePkg(feed bool, script string) map[string]string {
	panel, f := "", ""
	if feed {
		panel = `"panels":[{"id":"live","title":"Live"}],`
		f = `"feed":"live",`
	}
	return map[string]string{
		"sv/package.json": execPkg("sv", "", panel+`"services":[{"name":"watch","description":"Watch things",`+f+`"op":{"kind":"exec","argv":["./s.sh"]}}]`),
		"sv/s.sh":         "#!/bin/sh\n" + script + "\n",
	}
}

func TestServiceStartsOnlyWhenTheUserTypedIt(t *testing.T) {
	r := newAutoRig(t, servicePkg(false, "printf 'hello\\n'; sleep 30"), []string{"sv"})
	got := runServiceCommand("start watch", r.a, true)
	if !strings.Contains(got, "only works when you type it yourself") {
		t.Fatalf("a synthesised line started a service: %q", got)
	}
	if r.a.runningServices() != 0 {
		t.Fatal("a service is running that the user did not start")
	}
	got = runServiceCommand("start watch", r.a, false)
	if !strings.Contains(got, "Started sv/watch") {
		t.Fatalf("%q", got)
	}
	waitUntil(t, "the service's line", func() bool { return strings.Contains(r.notes(), "sv/watch: hello") })
	if r.a.runningServices() != 1 {
		t.Errorf("running = %d", r.a.runningServices())
	}
}

func TestServiceListShowsStateAndStopEndsIt(t *testing.T) {
	r := newAutoRig(t, servicePkg(false, "sleep 30"), []string{"sv"})
	if l := runServiceCommand("", r.a, false); !strings.Contains(l, "sv/watch · stopped") || !strings.Contains(l, "Watch things") {
		t.Fatalf("list = %q", l)
	}
	runServiceCommand("start watch", r.a, false)
	if l := runServiceCommand("list", r.a, false); !strings.Contains(l, "sv/watch · running") {
		t.Fatalf("list = %q", l)
	}
	if got := runServiceCommand("start watch", r.a, false); !strings.Contains(got, "already running") {
		t.Errorf("started twice: %q", got)
	}
	if got := runServiceCommand("stop watch", r.a, false); !strings.Contains(got, "Stopped sv/watch") {
		t.Fatalf("%q", got)
	}
	waitUntil(t, "it to end", func() bool { return r.a.runningServices() == 0 })
	// The user stopping it is not news: no "ended" line.
	time.Sleep(100 * time.Millisecond)
	if strings.Contains(r.notes(), "ended") {
		t.Errorf("a stop the user asked for was reported as the service ending: %q", r.notes())
	}
	if got := runServiceCommand("stop watch", r.a, false); !strings.Contains(got, "is not running") {
		t.Errorf("%q", got)
	}
}

func TestAServiceThatEndsByItselfIsReported(t *testing.T) {
	r := newAutoRig(t, servicePkg(false, "printf 'bye\\n'"), []string{"sv"})
	runServiceCommand("start watch", r.a, false)
	waitUntil(t, "the end", func() bool { return strings.Contains(r.notes(), "service sv/watch ended") })
}

func TestAServiceThatCrashesIsAWarningWithItsReason(t *testing.T) {
	r := newAutoRig(t, servicePkg(false, "echo boom >&2; exit 3"), []string{"sv"})
	runServiceCommand("start watch", r.a, false)
	waitUntil(t, "the warning", func() bool {
		w := r.warns()
		return strings.Contains(w, "sv/watch") && strings.Contains(w, "boom")
	})
}

// A service with a feed puts its last line in the panel instead of the conversation.
func TestAFeedingServiceFillsItsPanelAndAddsNoNotes(t *testing.T) {
	r := newAutoRig(t, servicePkg(true, "printf 'one\\n'; sleep 0.2; printf 'two\\n'; sleep 30"), []string{"sv"})
	if got := r.a.panelsLine(); got != "" {
		t.Fatalf("a panel nobody filled shows %q", got)
	}
	runServiceCommand("start watch", r.a, false)
	waitUntil(t, "the panel", func() bool { return r.a.panelsLine() == "Live: two" })
	if strings.Contains(r.notes(), "one") || strings.Contains(r.notes(), "two") {
		t.Errorf("a fed service also wrote notes: %q", r.notes())
	}
}

// Revoking the package stops its running services at once.
func TestRevokingAPackageStopsItsService(t *testing.T) {
	r := newAutoRig(t, servicePkg(false, "sleep 30"), []string{"sv"})
	runServiceCommand("start watch", r.a, false)
	if r.a.runningServices() != 1 {
		t.Fatal("not running")
	}
	if _, err := revokePackage(scopeUser, r.cwd, "sv"); err != nil {
		t.Fatal(err)
	}
	set := loadAllPackages(r.cwd, behaviour{})
	r.a.reload(set, newPackageRunner(set))
	waitUntil(t, "the service to stop", func() bool { return r.a.runningServices() == 0 })
	if !strings.Contains(r.notes(), "was stopped") {
		t.Errorf("the user was not told: %q", r.notes())
	}
}

func TestServiceNamesAreResolvedStrictly(t *testing.T) {
	r := newAutoRig(t, servicePkg(false, "sleep 30"), []string{"sv"})
	for in, want := range map[string]string{
		"start":       "name the service",
		"start nope":  "no approved package declares",
		"stop":        "name the service",
		"frobnicate":  "use /service",
		"start sv/xx": "no approved package declares",
	} {
		if got := runServiceCommand(in, r.a, false); !strings.Contains(got, want) {
			t.Errorf("/service %s = %q, want it to say %q", in, got, want)
		}
	}
	if got := runServiceCommand("start sv/watch", r.a, false); !strings.Contains(got, "Started") {
		t.Errorf("pkg/name form: %q", got)
	}
}

// A service of an unapproved package cannot be started.
func TestAnUnapprovedPackagesServiceCannotBeStarted(t *testing.T) {
	r := newAutoRig(t, servicePkg(false, "sleep 30"), nil)
	if got := runServiceCommand("start watch", r.a, false); !strings.Contains(got, "no approved package") {
		t.Errorf("%q", got)
	}
	if r.a.runningServices() != 0 {
		t.Error("it is running")
	}
}

func TestNoPackagesMeansAnHonestEmptyList(t *testing.T) {
	r := newAutoRig(t, nil, nil)
	if got := runServiceCommand("", r.a, false); !strings.Contains(got, "No approved package declares a service") {
		t.Errorf("%q", got)
	}
	if got := runServiceCommand("", nil, false); !strings.Contains(got, "cannot run packages") {
		t.Errorf("nil: %q", got)
	}
}

func TestStopAllEndsEveryService(t *testing.T) {
	r := newAutoRig(t, servicePkg(false, "sleep 30"), []string{"sv"})
	runServiceCommand("start watch", r.a, false)
	r.a.stopAll()
	waitUntil(t, "it to end", func() bool { return r.a.runningServices() == 0 })
}

func TestServiceLineParsing(t *testing.T) {
	for in, want := range map[string]string{"/service": "", "/service start x": "start x", "/service  stop all": "stop all"} {
		if got, ok := serviceLine(in, 0, "All"); !ok || got != want {
			t.Errorf("serviceLine(%q) = %q, %v", in, got, ok)
		}
	}
	if _, ok := serviceLine("/services", 0, "All"); ok {
		t.Error("/services is not /service")
	}
	if _, ok := serviceLine("service start x", 0, "All"); ok {
		t.Error("a line without the slash is not a command")
	}
}

// An answer that was still on its way when the package changed belongs to the old
// package; it must not overwrite what the new one says.
func TestAStalePanelAnswerIsDiscardedAfterAReload(t *testing.T) {
	r := newAutoRig(t, panelPkg("sleep 0.5; printf 'old'"), []string{"pn"})
	time.Sleep(100 * time.Millisecond) // the old job is now running
	script := filepath.Join(os.Getenv(configDirEnv), packagesDirName, "pn", "p.sh")
	write(t, script, "#!/bin/sh\nprintf 'new'\n", 0o755)
	set := loadAllPackages(r.cwd, behaviour{})
	p, ok := set.find("pn")
	if !ok {
		t.Fatal("the changed package did not load")
	}
	if err := grantPackage(p, r.cwd); err != nil {
		t.Fatal(err)
	}
	set = loadAllPackages(r.cwd, behaviour{})
	r.a.reload(set, newPackageRunner(set))
	waitUntil(t, "the new line", func() bool { return r.a.panelsLine() == "Clock: new" })
	time.Sleep(700 * time.Millisecond) // the old answer lands in the meantime
	if got := r.a.panelsLine(); got != "Clock: new" {
		t.Errorf("the old package's answer replaced the new one: %q", got)
	}
}
