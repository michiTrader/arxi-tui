//go:build !windows

package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/michiTrader/arxi_tui/internal/fold"
	"github.com/michiTrader/arxi_tui/internal/term"
)

func loadedWith(t *testing.T, docs map[string]string) (packageSet, string) {
	t.Helper()
	cfg := t.TempDir()
	t.Setenv(configDirEnv, cfg)
	for id, body := range docs {
		write(t, filepath.Join(cfg, packagesDirName, id+".json"), body, 0o644)
	}
	cwd := t.TempDir()
	return loadAllPackages(cwd, behaviour{}), cwd
}

func TestPackageLineParsing(t *testing.T) {
	cases := []struct {
		in   string
		want packageCommand
		ok   bool
	}{
		{"/package", packageCommand{Verb: "list"}, true},
		{"/package list", packageCommand{Verb: "list"}, true},
		{"/package show a", packageCommand{Verb: "show", ID: "a"}, true},
		{"/package approve a", packageCommand{Verb: "approve", ID: "a"}, true},
		{"/package approve a abc", packageCommand{Verb: "approve", ID: "a", Code: "abc"}, true},
		{"/package revoke a", packageCommand{Verb: "revoke", ID: "a"}, true},
		{"/package approve", packageCommand{Verb: "usage"}, true},
		{"/package approve a b c", packageCommand{Verb: "usage"}, true},
		{"/package nonsense", packageCommand{Verb: "usage"}, true},
		{"/packages", packageCommand{}, false},
		{"hello /package", packageCommand{}, false},
	}
	for _, c := range cases {
		got, ok := packageLineOf(c.in, 0, fold.SlashAll)
		if ok != c.ok || (ok && got != c.want) {
			t.Errorf("packageLineOf(%q) = %+v, %v; want %+v, %v", c.in, got, ok, c.want, c.ok)
		}
	}
}

func TestApprovingNeedsTheCodeOfTheVersionShown(t *testing.T) {
	set, cwd := loadedWith(t, map[string]string{"loud": pkgDoc("loud", echoPkg)})
	p, _ := set.find("loud")

	shown := runPackageCommand(packageCommand{Verb: "approve", ID: "loud"}, set, cwd, false)
	if shown.Changed || !strings.Contains(shown.Note, "/package approve loud "+packageCode(p)) {
		t.Fatalf("without a code nothing may change and the code must be shown: %+v", shown)
	}
	if reloaded := loadAllPackages(cwd, behaviour{}); func() bool { q, _ := reloaded.find("loud"); return q.Granted }() {
		t.Fatal("asking to approve approved it")
	}

	wrong := runPackageCommand(packageCommand{Verb: "approve", ID: "loud", Code: "0000000000"}, set, cwd, false)
	if wrong.Changed || wrong.Warn == "" {
		t.Fatalf("a wrong code approved or said nothing: %+v", wrong)
	}

	ok := runPackageCommand(packageCommand{Verb: "approve", ID: "loud", Code: packageCode(p)}, set, cwd, false)
	if !ok.Changed || ok.Warn != "" {
		t.Fatalf("the right code did not approve: %+v", ok)
	}
	reloaded := loadAllPackages(cwd, behaviour{})
	if q, _ := reloaded.find("loud"); !q.Granted {
		t.Fatal("approval was not recorded")
	}
}

func TestApprovalOfAnOldVersionDoesNotCarryToTheEditedOne(t *testing.T) {
	set, cwd := loadedWith(t, map[string]string{"loud": pkgDoc("loud", echoPkg)})
	p, _ := set.find("loud")
	oldCode := packageCode(p)

	// The package is edited (it now asks to send text to the model) after the user
	// looked at it and before they typed the code.
	edited := pkgDoc("loud", echoPkg+`,"permissions":{"send":true}`)
	if err := os.WriteFile(p.Path, []byte(edited), 0o644); err != nil {
		t.Fatal(err)
	}
	set = loadAllPackages(cwd, behaviour{})
	got := runPackageCommand(packageCommand{Verb: "approve", ID: "loud", Code: oldCode}, set, cwd, false)
	if got.Changed {
		t.Fatalf("the code of the old version approved the new one: %+v", got)
	}
}

func TestSynthesisedLinesCanLookButNeverDecide(t *testing.T) {
	set, cwd := loadedWith(t, map[string]string{"loud": pkgDoc("loud", echoPkg)})
	p, _ := set.find("loud")
	if err := grantPackage(p, cwd); err != nil {
		t.Fatal(err)
	}
	set = loadAllPackages(cwd, behaviour{})

	for _, c := range []packageCommand{
		{Verb: "approve", ID: "loud", Code: packageCode(p)},
		{Verb: "revoke", ID: "loud"},
	} {
		got := runPackageCommand(c, set, cwd, true)
		if got.Changed || got.Warn == "" {
			t.Errorf("%s from a synthesised line was honoured: %+v", c.Verb, got)
		}
	}
	if q, _ := loadAllPackages(cwd, behaviour{}).find("loud"); !q.Granted {
		t.Error("a synthesised revoke removed the approval")
	}
	if got := runPackageCommand(packageCommand{Verb: "list"}, set, cwd, true); got.Note == "" {
		t.Error("listing must still work from a menu row")
	}
}

func TestRevokeStopsThePackage(t *testing.T) {
	set, cwd := loadedWith(t, map[string]string{"loud": pkgDoc("loud", echoPkg)})
	p, _ := set.find("loud")
	_ = grantPackage(p, cwd)
	set = loadAllPackages(cwd, behaviour{})
	got := runPackageCommand(packageCommand{Verb: "revoke", ID: "loud"}, set, cwd, false)
	if !got.Changed {
		t.Fatalf("%+v", got)
	}
	if _, _, ok := loadAllPackages(cwd, behaviour{}).commandOwner("shout"); ok {
		t.Error("a revoked package's command still answers")
	}
}

func TestShowSaysWhatThePackageCanDo(t *testing.T) {
	rule := `{"id":"r","stage":"input.pre","op":{"kind":"http","method":"POST","url":"https://api.example.com/x","body":"{\"q\":\"${input}\"}"}}`
	doc := pkgDoc("net", `,"permissions":{"net":["api.example.com"],"send":true},"commands":[{"name":"w","description":"weather","op":{"kind":"http","method":"GET","url":"https://api.example.com/${args}"}}],"rules":[`+rule+`]`)
	set, cwd := loadedWith(t, map[string]string{"net": doc})
	if len(set.pkgs) != 1 {
		t.Fatalf("did not load: %s", problemsText(set))
	}
	text := runPackageCommand(packageCommand{Verb: "show", ID: "net"}, set, cwd, false).Note
	for _, want := range []string{"api.example.com", "send text to the model", "read and rewrite every line you type", "/w", "not approved"} {
		if !strings.Contains(text, want) {
			t.Errorf("show does not say %q:\n%s", want, text)
		}
	}
}

// The consent screen tells an output.post rule apart from an input.pre one, and says the model
// keeps its own words, so the user approves what it really does.
func TestShowSaysAnOutputPostRuleRewordsOnlyWhatYouSee(t *testing.T) {
	doc := pkgDoc("tidy", `,"permissions":{"exec":["sh"]},"rules":[{"id":"r","stage":"output.post","op":{"kind":"exec","argv":["sh","-c","cat"]}}]`)
	set, cwd := loadedWith(t, map[string]string{"tidy": doc})
	if len(set.pkgs) != 1 {
		t.Fatalf("did not load: %s", problemsText(set))
	}
	text := runPackageCommand(packageCommand{Verb: "show", ID: "tidy"}, set, cwd, false).Note
	for _, want := range []string{"read every answer the model gives", "the model and the saved session keep its own words", "Rule r on output.post", "the model keeps its own words"} {
		if !strings.Contains(text, want) {
			t.Errorf("show does not say %q:\n%s", want, text)
		}
	}
	if strings.Contains(text, "every line you type") {
		t.Errorf("an output.post package does not read what you type, and the screen says it does:\n%s", text)
	}
}

func TestListNamesTheProblemsAndTheState(t *testing.T) {
	cfg := t.TempDir()
	t.Setenv(configDirEnv, cfg)
	write(t, filepath.Join(cfg, packagesDirName, "ok.json"), pkgDoc("ok", echoPkg), 0o644)
	write(t, filepath.Join(cfg, packagesDirName, "bad.json"), `{"protocol":`, 0o644)
	set := loadAllPackages(t.TempDir(), behaviour{})
	text := runPackageCommand(packageCommand{Verb: "list"}, set, "", false).Note
	if !strings.Contains(text, "ok 1") && !strings.Contains(text, "ok ") || !strings.Contains(text, "not approved") || !strings.Contains(text, "bad.json") {
		t.Errorf("list is missing the package, its state or the broken file:\n%s", text)
	}
}

// ---- through the real loop ----------------------------------------------------

func TestLoopApproveThenUseThenEditAsksAgain(t *testing.T) {
	cfg := t.TempDir()
	t.Setenv(configDirEnv, cfg)
	t.Setenv(historyEnv, t.TempDir())
	t.Setenv(sessionsEnv, "")
	path := filepath.Join(cfg, packagesDirName, "loud.json")
	write(t, path, pkgDoc("loud", echoPkg), 0o644)
	cwd, _ := os.Getwd()
	p, _ := loadAllPackages(cwd, behaviour{}).find("loud")

	script := enterLine("/shout one")
	// A refused "/" line stays in the input so it can be fixed; clear it first.
	for range "/shout one" {
		script = append(script, scheduledEvent{5 * time.Millisecond, term.Event{Kind: term.EventKey, Key: term.Key{Type: term.KeyBackspace}}})
	}
	script = append(script, enterLine("/package approve loud "+packageCode(p))...)
	script = append(script, enterLine("/shout two")...)
	script = append(script, exitKeys()...)
	out, sent := runPackageLoopIn(t, script)
	if len(sent) != 0 {
		t.Fatalf("sent %q", sent)
	}
	if frameWith(out, "not approved yet") == "" {
		t.Errorf("before approving, /shout must say why it did nothing")
	}
	if frameWith(out, "· two") == "" {
		t.Errorf("after approving, /shout two did not answer; last frame:\n%s", lastFrame(out))
	}
	if frameWith(out, "· one") != "" {
		t.Errorf("/shout one ran before the package was approved")
	}
}

func TestLoopShortcutCannotApproveAPackage(t *testing.T) {
	cfg := t.TempDir()
	t.Setenv(configDirEnv, cfg)
	t.Setenv(historyEnv, t.TempDir())
	t.Setenv(sessionsEnv, "")
	write(t, filepath.Join(cfg, packagesDirName, "loud.json"), pkgDoc("loud", echoPkg), 0o644)
	cwd, _ := os.Getwd()
	p, _ := loadAllPackages(cwd, behaviour{}).find("loud")
	// A behaviour (which a model may have written) binds F5 to the approval, code and all.
	if err := os.WriteFile(userBehaviourPath(), []byte(`{"keys":{"f5":"cmd:/package approve loud `+packageCode(p)+`"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	script := append([]scheduledEvent{fkey(term.KeyF5)}, enterLine("/shout hi")...)
	script = append(script, exitKeys()...)
	out, _ := runPackageLoopIn(t, script)
	if frameWith(out, "· hi") != "" {
		t.Fatal("a shortcut approved a package and it ran")
	}
	if frameWith(out, "only works when you type it yourself") == "" {
		t.Errorf("the user was not told the shortcut was refused; last frame:\n%s", lastFrame(out))
	}
	if q, _ := loadAllPackages(cwd, behaviour{}).find("loud"); q.Granted {
		t.Fatal("the grant was written")
	}
}

func TestPackageIsInTheRegistry(t *testing.T) {
	if !isFactoryCommand("package") {
		t.Fatal("/package is not in fold.Commands, so a package or a user command could take the name and the menu would not list it")
	}
}
