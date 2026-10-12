//go:build !windows

package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/michiTrader/arxi_tui/internal/driver"
	"github.com/michiTrader/arxi_tui/internal/fold"
	"github.com/michiTrader/arxi_tui/internal/theme"
)

// pkgRig is the bridge with the loop's half played by the test the way the loop does it:
// an approved change is checked for freshness, its packages written and the interface
// saved with what they held before.
type pkgRig struct {
	b     *uiBridge
	asked []driver.Approval
}

func newPkgRig(t *testing.T, mode string) *pkgRig {
	t.Helper()
	cfg := t.TempDir()
	t.Setenv(configDirEnv, cfg)
	t.Setenv(historyEnv, t.TempDir())
	r := &pkgRig{b: newUIBridge()}
	r.b.setMode(mode)
	r.b.publish(builtinDoc(t), theme.SOBRIA(), nil, nil, behaviour{})
	return r
}

func (r *pkgRig) edit(t *testing.T, args string, allow bool) driver.ClientToolResult {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cwd, _ := os.Getwd()
	go func() {
		select {
		case req := <-r.b.apply:
			if req.pkg != nil && !req.pkg.fresh() {
				req.done <- errString("the interface changed while the question was open")
				return
			}
			if err := applyPackageEdit(req.pkg, cwd); err != nil {
				req.done <- err
				return
			}
			var before packageDelta
			if req.pkg != nil {
				before = req.pkg.base
			}
			req.done <- saveInterfaceWith(req.doc, nil, nil, behaviour{}, before)
		case <-ctx.Done():
		}
	}()
	return r.b.call(ctx, driver.ClientToolCall{CallID: "p1", Name: uiToolEdit, Arguments: json.RawMessage(args)},
		func(a driver.Approval) bool { r.asked = append(r.asked, a); return allow })
}

func pkgFile(t *testing.T, id string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(userPackagesDir(), id+".json"))
	if err != nil {
		return ""
	}
	return string(b)
}

// putArgs is a ui_edit call that creates a package with one command.
func putArgs(id, cmd, perms string) string {
	doc := `{"protocol":"ext/v2","id":"` + id + `","name":"` + id + `","version":"1"` + perms +
		`,"commands":[{"name":"` + cmd + `","description":"d","op":{"kind":"builtin","name":"echo"}}]}`
	return `{"packages":[{"put":` + doc + `}],"summary":"add ` + id + `"}`
}

func TestAgentCreatesAPackageThatDoesNothingUntilApproved(t *testing.T) {
	r := newPkgRig(t, "full access")
	res := r.edit(t, putArgs("greet", "greet", ""), true)
	if !res.OK {
		t.Fatalf("refused: %+v", res)
	}
	if pkgFile(t, "greet") == "" {
		t.Fatal("the package was not written")
	}
	// Even in full access the user is asked, and shown what the package can do.
	if len(r.asked) != 1 || !strings.Contains(r.asked[0].Diff, "greet") || !strings.Contains(r.asked[0].Diff, "can only show text") {
		t.Fatalf("asked = %+v; a package is always put to the user, with what it can do", r.asked)
	}
	cwd, _ := os.Getwd()
	set := loadAllPackages(cwd, behaviour{})
	p, ok := set.find("greet")
	if !ok {
		t.Fatalf("not loaded: %s", problemsText(set))
	}
	if p.Granted || p.runnable() {
		t.Fatal("a package the agent wrote is approved; only the user's /package approve may do that")
	}
	if !strings.Contains(res.Text, "/package approve") || !strings.Contains(res.Text, "NOTHING") {
		t.Errorf("the agent was not told to ask the user to approve: %q", res.Text)
	}
}

func TestAgentPackageWithPermissionsShowsThemInTheQuestion(t *testing.T) {
	r := newPkgRig(t, "full access")
	res := r.edit(t, putArgs("web", "web", `,"permissions":{"open":["docs.example.com"],"send":true}`), true)
	if !res.OK {
		t.Fatalf("refused: %+v", res)
	}
	d := r.asked[0].Diff
	if !strings.Contains(d, "open docs.example.com in your browser") || !strings.Contains(d, "send text to the model") {
		t.Errorf("the question does not list the powers:\n%s", d)
	}
}

func TestAgentCannotApproveByEditingTheGrants(t *testing.T) {
	r := newPkgRig(t, "full access")
	if res := r.edit(t, putArgs("greet", "greet", ""), true); !res.OK {
		t.Fatal(res)
	}
	// Editing the package afterwards changes its hash, so even a grant the user gave
	// to the first version does not carry over.
	cwd, _ := os.Getwd()
	set := loadAllPackages(cwd, behaviour{})
	p, _ := set.find("greet")
	if err := grantPackage(p, cwd); err != nil {
		t.Fatal(err)
	}
	if res := r.edit(t, putArgs("greet", "greet", `,"permissions":{"send":true}`), true); !res.OK {
		t.Fatal(res)
	}
	set = loadAllPackages(cwd, behaviour{})
	if p, _ := set.find("greet"); p.Granted {
		t.Fatal("a changed package kept the approval given to the earlier version")
	}
}

func TestAgentPackageRefusedWithTheReasonAndNothingWritten(t *testing.T) {
	cases := map[string]string{
		"existing command":   putArgs("zz", "help", ""),
		"bad id":             `{"packages":[{"put":{"protocol":"ext/v2","id":"Bad Id","name":"x","version":"1","commands":[{"name":"x","op":{"kind":"builtin","name":"echo"}}]}}]}`,
		"unknown builtin":    `{"packages":[{"put":{"protocol":"ext/v2","id":"zz","name":"x","version":"1","commands":[{"name":"zz","op":{"kind":"builtin","name":"rm-rf"}}]}}]}`,
		"unlisted host":      `{"packages":[{"put":{"protocol":"ext/v2","id":"zz","name":"x","version":"1","commands":[{"name":"zz","op":{"kind":"http","url":"https://evil.example.org/"}}]}}]}`,
		"unknown key":        `{"packages":[{"put":{"protocol":"ext/v2","id":"zz","name":"x","version":"1","surprise":1,"commands":[{"name":"zz","op":{"kind":"builtin","name":"echo"}}]}}]}`,
		"two keys in one":    `{"packages":[{"remove":"a","disable":"b"}]}`,
		"empty list":         `{"packages":[]}`,
		"remove unknown":     `{"packages":[{"remove":"ghost"}]}`,
		"not a package list": `{"packages":{"put":1}}`,
	}
	for name, args := range cases {
		r := newPkgRig(t, "full access")
		res := r.edit(t, args, true)
		if res.OK {
			t.Errorf("%s: accepted: %+v", name, res)
			continue
		}
		if len(r.asked) != 0 {
			t.Errorf("%s: the user was asked about a draft that cannot be valid", name)
		}
		if ents, _ := os.ReadDir(userPackagesDir()); len(ents) != 0 {
			t.Errorf("%s: something was written", name)
		}
		if !strings.Contains(res.Text, "Nothing was changed") {
			t.Errorf("%s: the refusal does not say nothing changed: %q", name, res.Text)
		}
	}
}

func TestAgentCannotTakeAnotherPackagesCommandName(t *testing.T) {
	r := newPkgRig(t, "full access")
	if res := r.edit(t, putArgs("one", "same", ""), true); !res.OK {
		t.Fatal(res)
	}
	res := r.edit(t, putArgs("two", "same", ""), true)
	if res.OK || pkgFile(t, "two") != "" {
		t.Fatalf("a second package took /same: %+v", res)
	}
	// Replacing the package that owns the name is fine.
	if res := r.edit(t, putArgs("one", "same", `,"permissions":{"send":true}`), true); !res.OK {
		t.Fatalf("replacing its own package was refused: %+v", res)
	}
}

func TestAgentCannotChangeFolderOrProjectPackages(t *testing.T) {
	r := newPkgRig(t, "full access")
	write(t, filepath.Join(userPackagesDir(), "fold", "package.json"), pkgDoc("fold", echoPkg), 0o644)
	for _, args := range []string{`{"packages":[{"remove":"fold"}]}`, `{"packages":[{"disable":"fold"}]}`, putArgs("fold", "other", "")} {
		if res := r.edit(t, args, true); res.OK {
			t.Errorf("%s changed a folder package: %+v", args, res)
		}
	}
	if _, err := os.Stat(filepath.Join(userPackagesDir(), "fold", "package.json")); err != nil {
		t.Fatal("the folder package was touched")
	}
}

func TestAgentCannotChangeAProjectPackage(t *testing.T) {
	t.Chdir(t.TempDir())
	r := newPkgRig(t, "full access")
	cwd, _ := os.Getwd()
	// The project's settings folder must exist before projectPackagesDir names a
	// packages folder inside it; without it the helper answers "" and the file would
	// land loose in the working directory, a package the interface never sees.
	if err := os.MkdirAll(filepath.Join(cwd, projectDirName), 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(projectPackagesDir(cwd), "proj.json")
	if path == "proj.json" {
		t.Fatal("the project packages folder was not found, so this test would prove nothing")
	}
	write(t, path, pkgDoc("proj", echoPkg), 0o644)
	if _, ok := loadAllPackages(cwd, behaviour{}).find("proj"); !ok {
		t.Fatal("the project package is not loaded, so refusing it would be for another reason")
	}
	for _, args := range []string{`{"packages":[{"remove":"proj"}]}`, `{"packages":[{"disable":"proj"}]}`, `{"packages":[{"enable":"proj"}]}`} {
		res := r.edit(t, args, true)
		if res.OK || !strings.Contains(res.Text, "belongs to the project folder") {
			t.Errorf("%s: want a refusal that names the project folder, got %+v", args, res)
		}
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatal("the project package was touched")
	}
}

func TestANewPackageIsRefusedWhenTheUserCreatedOneMeanwhile(t *testing.T) {
	r := newPkgRig(t, "full access")
	args := putArgs("fresh", "fresh", "")
	cwd, _ := os.Getwd()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	go func() {
		req := <-r.b.apply
		if !req.pkg.fresh() {
			req.done <- errString("the interface changed while the question was open")
			return
		}
		req.done <- applyPackageEdit(req.pkg, cwd)
	}()
	res := r.b.call(ctx, driver.ClientToolCall{Name: uiToolEdit, Arguments: json.RawMessage(args)}, func(driver.Approval) bool {
		write(t, filepath.Join(userPackagesDir(), "fresh.json"), pkgDoc("fresh", echoPkg), 0o644)
		return true
	})
	if res.OK || !strings.Contains(res.Text, "changed while the question was open") {
		t.Fatalf("a package the user created meanwhile was overwritten: %+v", res)
	}
}

func TestAgentDisableAndRemoveNeedNoQuestionInFullAccessButEnableDoes(t *testing.T) {
	r := newPkgRig(t, "full access")
	if res := r.edit(t, putArgs("greet", "greet", ""), true); !res.OK {
		t.Fatal(res)
	}
	r.asked = nil
	if res := r.edit(t, `{"packages":[{"disable":"greet"}]}`, true); !res.OK || len(r.asked) != 0 {
		t.Fatalf("disable: %+v asked=%d; switching off only narrows", res, len(r.asked))
	}
	if !strings.Contains(pkgFile(t, "greet"), `"disabled": true`) {
		t.Fatal("not switched off on disk")
	}
	if res := r.edit(t, `{"packages":[{"enable":"greet"}]}`, true); !res.OK || len(r.asked) != 1 {
		t.Fatalf("enable: %+v asked=%d; switching on is asked", res, len(r.asked))
	}
	r.asked = nil
	if res := r.edit(t, `{"packages":[{"remove":"greet"}]}`, true); !res.OK || len(r.asked) != 0 {
		t.Fatalf("remove: %+v asked=%d", res, len(r.asked))
	}
	if pkgFile(t, "greet") != "" {
		t.Fatal("not removed")
	}
}

func TestAgentRemoveWithdrawsTheApproval(t *testing.T) {
	r := newPkgRig(t, "full access")
	if res := r.edit(t, putArgs("greet", "greet", ""), true); !res.OK {
		t.Fatal(res)
	}
	cwd, _ := os.Getwd()
	p, _ := loadAllPackages(cwd, behaviour{}).find("greet")
	if err := grantPackage(p, cwd); err != nil {
		t.Fatal(err)
	}
	if res := r.edit(t, `{"packages":[{"remove":"greet"}]}`, true); !res.OK {
		t.Fatal(res)
	}
	if _, left := readGrants()[grantKey(scopeUser, cwd, "greet")]; left {
		t.Error("the approval outlived the package; an id used again later must be a new question")
	}
}

func TestADeclinedPackageChangeWritesNothing(t *testing.T) {
	r := newPkgRig(t, "full access")
	if res := r.edit(t, putArgs("greet", "greet", ""), false); res.OK || pkgFile(t, "greet") != "" {
		t.Fatalf("a package the user declined was written: %+v", res)
	}
}

func TestPackageChangeIsRefusedWhenTheFolderMovedOnMeanwhile(t *testing.T) {
	r := newPkgRig(t, "full access")
	if res := r.edit(t, putArgs("greet", "greet", ""), true); !res.OK {
		t.Fatal(res)
	}
	// The user edits the file by hand while the question is open.
	args := putArgs("greet", "greet", `,"permissions":{"send":true}`)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cwd, _ := os.Getwd()
	go func() {
		req := <-r.b.apply
		if req.pkg.fresh() {
			req.done <- applyPackageEdit(req.pkg, cwd)
			return
		}
		req.done <- errString("the interface changed while the question was open")
	}()
	res := r.b.call(ctx, driver.ClientToolCall{Name: uiToolEdit, Arguments: json.RawMessage(args)}, func(driver.Approval) bool {
		write(t, filepath.Join(userPackagesDir(), "greet.json"), pkgDoc("greet", echoPkg), 0o644)
		return true
	})
	if res.OK || !strings.Contains(res.Text, "changed while the question was open") {
		t.Fatalf("a stale package change went through: %+v", res)
	}
	if strings.Contains(pkgFile(t, "greet"), "send") {
		t.Error("the agent's version overwrote the user's hand edit")
	}
}

func TestUndoBringsBackOnlyThePackagesTheLastChangeTouched(t *testing.T) {
	r := newPkgRig(t, "full access")
	mine := pkgDoc("mine", echoPkg)
	write(t, filepath.Join(userPackagesDir(), "mine.json"), mine, 0o644)
	if res := r.edit(t, putArgs("greet", "greet", ""), true); !res.OK {
		t.Fatal(res)
	}
	// The user writes another package by hand after the agent's change.
	late := pkgDoc("late", `,"commands":[{"name":"late","op":{"kind":"builtin","name":"echo"}}]`)
	write(t, filepath.Join(userPackagesDir(), "late.json"), late, 0o644)

	if _, _, _, _, err := undoInterface(); err != nil {
		t.Fatal(err)
	}
	if pkgFile(t, "greet") != "" {
		t.Error("undo left the package the agent added")
	}
	if pkgFile(t, "mine") != mine || pkgFile(t, "late") != late {
		t.Error("undo touched a package the last change never named")
	}
	// A second undo is a redo.
	if _, _, _, _, err := undoInterface(); err != nil {
		t.Fatal(err)
	}
	if pkgFile(t, "greet") == "" {
		t.Error("the second undo did not bring the package back")
	}
}

func TestUndoPutsBackAReplacedPackageAsItWas(t *testing.T) {
	r := newPkgRig(t, "full access")
	if res := r.edit(t, putArgs("greet", "greet", ""), true); !res.OK {
		t.Fatal(res)
	}
	first := pkgFile(t, "greet")
	if res := r.edit(t, putArgs("greet", "greet", `,"permissions":{"send":true}`), true); !res.OK {
		t.Fatal(res)
	}
	if _, _, _, _, err := undoInterface(); err != nil {
		t.Fatal(err)
	}
	if pkgFile(t, "greet") != first {
		t.Errorf("undo did not restore the earlier document:\n%s", pkgFile(t, "greet"))
	}
}

// Found with a real model: it quoted a made-up code after the id (the host shows the real
// one), and gave a builtin a "result" the validator refuses. The guide now says both.
func TestGuideTellsTheModelToLeaveTheCodeToTheHostAndResultOutOfBuiltins(t *testing.T) {
	g := packagesGuide(packageSet{})
	for _, want := range []string{"with nothing after the id", "Never make up or quote a code", "leave it out of a builtin"} {
		if !strings.Contains(g, want) {
			t.Errorf("the packages guide does not say %q", want)
		}
	}
}

func TestGuideTeachesPackagesAndListsWhatIsThere(t *testing.T) {
	r := newPkgRig(t, "ask")
	write(t, filepath.Join(userPackagesDir(), "mine.json"), pkgDoc("mine", echoPkg), 0o644)
	g := r.b.guideText()
	for _, want := range []string{"PACKAGES.", `{"put": <package>}`, "permissions", "/package approve", "echo", "mine (user, single document): not approved", "/shout"} {
		if !strings.Contains(g, want) {
			t.Errorf("the guide does not say %q", want)
		}
	}
	var def struct {
		Properties map[string]json.RawMessage `json:"properties"`
	}
	for _, d := range r.b.definitions() {
		if d.Name == uiToolEdit {
			if err := json.Unmarshal(d.Schema, &def); err != nil {
				t.Fatal(err)
			}
		}
	}
	if _, ok := def.Properties["packages"]; !ok {
		t.Error("ui_edit does not offer packages")
	}
}

func TestPlanModeRefusesPackageChanges(t *testing.T) {
	r := newPkgRig(t, "plan")
	if res := r.edit(t, putArgs("greet", "greet", ""), true); res.OK || pkgFile(t, "greet") != "" {
		t.Fatalf("plan mode wrote a package: %+v", res)
	}
}

// ---- through the real loop ----------------------------------------------------------

type bridgeDriver struct {
	testDriver
	br *uiBridge
}

func (d *bridgeDriver) InterfaceBridge() *uiBridge { return d.br }

// The agent adds a package through the loop: it is saved, reloaded and listed but does
// not run, the user is told to approve it, and after they do the command answers.
func TestLoopAgentAddsAPackageThenTheUserApprovesAndUsesIt(t *testing.T) {
	cfg := t.TempDir()
	t.Setenv(configDirEnv, cfg)
	t.Setenv(historyEnv, t.TempDir())
	t.Setenv(sessionsEnv, "")
	br := newUIBridge()
	br.setMode("full access")
	drv := &bridgeDriver{testDriver: testDriver{evCh: make(chan fold.Event, 64), seq: 1000}, br: br}

	asked := make(chan driver.Approval, 1)
	result := make(chan driver.ClientToolResult, 1)
	go func() {
		time.Sleep(400 * time.Millisecond)
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		result <- br.call(ctx, driver.ClientToolCall{Name: uiToolEdit, Arguments: json.RawMessage(putArgs("greet", "greet", ""))},
			func(a driver.Approval) bool { asked <- a; return true })
	}()
	// Typing /greet before approval must not run it; after the agent's change and the
	// user's approval it must.
	script := []scheduledEvent{{900 * time.Millisecond, ctrlCharEvent('c')}}
	script = append(script, enterLine("/greet before")...)
	tty := newFakeTTY(100, 30, script)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	doc := builtinDoc(t)
	go func() {
		<-time.After(3 * time.Second)
	}()
	if err := loop(ctx, tty, doc, theme.SOBRIA(), drv.evCh, drv, ""); err != nil {
		t.Fatalf("loop: %v", err)
	}
	res := <-result
	if !res.OK {
		t.Fatalf("the edit failed: %+v", res)
	}
	<-asked
	out := tty.output()
	if frameWith(out, "was saved and does nothing until you approve it") == "" {
		t.Errorf("the user was not told to approve; last frame:\n%s", lastFrame(out))
	}
	if frameWith(out, "· before") != "" {
		t.Error("the package ran without approval")
	}
	if len(drv.submitted) != 0 {
		t.Errorf("sent %q", drv.submitted)
	}
}
