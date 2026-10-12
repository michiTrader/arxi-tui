//go:build !windows

package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/michiTrader/arxi_tui/internal/defaultscene"
	"github.com/michiTrader/arxi_tui/internal/fold"
	"github.com/michiTrader/arxi_tui/internal/scene"
	"github.com/michiTrader/arxi_tui/internal/theme"
)

// runPackageLoop drives the real loop on the built-in interface (the only one that loads
// packages) with the config folder pointed at a temporary one that holds the given
// packages. approve lists the ids the user has agreed to.
func runPackageLoop(t *testing.T, docs map[string]string, approve []string, script []scheduledEvent) (string, []string) {
	t.Helper()
	return runPackageFolders(t, docs, nil, approve, script)
}

// runPackageFolders is runPackageLoop with packages that are folders too: folders maps
// "id/file" to the file's text ("id/package.json" is the document; a name ending in .sh
// is written executable).
// runPackageLoopIn runs the loop on the built-in interface with whatever the test already
// put in the config folder (it sets nothing up itself).
func runPackageLoopIn(t *testing.T, script []scheduledEvent) (string, []string) {
	t.Helper()
	doc, err := scene.ParseNamed(defaultscene.Name, defaultscene.JSON)
	if err != nil {
		t.Fatal(err)
	}
	tty := newFakeTTY(100, 30, script)
	drv := &testDriver{evCh: make(chan fold.Event, 64), seq: 1000}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := loop(ctx, tty, doc, theme.SOBRIA(), drv.evCh, drv, ""); err != nil {
		t.Fatalf("loop: %v", err)
	}
	return tty.output(), drv.submitted
}

func runPackageFolders(t *testing.T, docs, folders map[string]string, approve []string, script []scheduledEvent) (string, []string) {
	t.Helper()
	return runPackageWithBehaviour(t, "", docs, folders, approve, script)
}

// runPackageWithBehaviour is runPackageFolders with the user's behaviour.json too ("" for
// none).
func runPackageWithBehaviour(t *testing.T, beh string, docs, folders map[string]string, approve []string, script []scheduledEvent) (string, []string) {
	t.Helper()
	cfg := t.TempDir()
	t.Setenv(configDirEnv, cfg)
	if beh != "" {
		write(t, userBehaviourPath(), beh, 0o600)
		t.Cleanup(func() { setActiveBehaviour(behaviour{}) })
	}
	t.Setenv(historyEnv, t.TempDir())
	t.Setenv(sessionsEnv, "")
	for id, body := range docs {
		write(t, filepath.Join(cfg, packagesDirName, id+".json"), body, 0o644)
	}
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
	doc, err := scene.ParseNamed(defaultscene.Name, defaultscene.JSON)
	if err != nil {
		t.Fatal(err)
	}
	tty := newFakeTTY(100, 30, script)
	drv := &testDriver{evCh: make(chan fold.Event, 64), seq: 1000}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := loop(ctx, tty, doc, theme.SOBRIA(), drv.evCh, drv, ""); err != nil {
		t.Fatalf("loop: %v", err)
	}
	return tty.output(), drv.submitted
}

// screen is the last frame with runs of spaces folded, so a sentence the layout wrapped
// still reads as one.
func screen(out string) string { return strings.Join(strings.Fields(lastFrame(out)), " ") }

// frameWith is the plain text of the last frame that shows what, "" when none does.
func frameWith(out, what string) string {
	frames := strings.Split(out, frameBegin)
	for i := len(frames) - 1; i >= 0; i-- {
		if f := strings.Join(strings.Fields(ansi.Strip(frames[i])), " "); strings.Contains(f, what) {
			return f
		}
	}
	return ""
}

const echoPkg = `,"commands":[{"name":"shout","description":"say it back","op":{"kind":"builtin","name":"echo"}}]`

func TestLoopPackageCommandAnswersWithANoteAndSendsNothing(t *testing.T) {
	script := append(enterLine("/shout hello there"), exitKeys()...)
	out, sent := runPackageLoop(t, map[string]string{"loud": pkgDoc("loud", echoPkg)}, []string{"loud"}, script)
	if len(sent) != 0 {
		t.Fatalf("the model was sent %q; a command's answer is the host's, not a prompt", sent)
	}
	if !strings.Contains(screen(out), "· hello there") {
		t.Errorf("the answer is not on screen as a note; last frame:\n%s", lastFrame(out))
	}
}

func TestLoopPackageWithoutApprovalDoesNothingAndSaysWhy(t *testing.T) {
	script := append(enterLine("/shout hello there"), exitKeys()...)
	out, sent := runPackageLoop(t, map[string]string{"loud": pkgDoc("loud", echoPkg)}, nil, script)
	if len(sent) != 0 {
		t.Fatalf("an unapproved package's command reached the model as %q", sent)
	}
	if frameWith(out, "· hello there") != "" {
		t.Error("an unapproved package ran")
	}
	if !strings.Contains(screen(out), "not approved yet") {
		t.Errorf("the user was not told why nothing happened; last frame:\n%s", lastFrame(out))
	}
}

func TestLoopPackageCommandThatNeedsWordsAsksForThem(t *testing.T) {
	need := `,"commands":[{"name":"shout","min_args":1,"usage":"/shout <text>","op":{"kind":"builtin","name":"echo"}}]`
	script := append(enterLine("/shout"), exitKeys()...)
	out, sent := runPackageLoop(t, map[string]string{"loud": pkgDoc("loud", need)}, []string{"loud"}, script)
	if len(sent) != 0 {
		t.Fatalf("sent %q", sent)
	}
	if !strings.Contains(screen(out), "needs more") {
		t.Errorf("no usage shown; last frame:\n%s", lastFrame(out))
	}
}

const rulePkgDoc = `,"permissions":{"exec":["sh"]},"rules":[{"id":"r","stage":"input.pre","op":{"kind":"exec","argv":["./r.sh","${input}"],"result":"outcome"}}]`

func rulePackage(script string) map[string]string {
	return map[string]string{
		"gate/package.json": pkgDoc("gate", rulePkgDoc),
		"gate/r.sh":         "#!/bin/sh\n" + script + "\n",
	}
}

func TestLoopRuleRewordsAPlainLine(t *testing.T) {
	folders := rulePackage(`printf '{"verdict":"transform","text":"[checked] %s"}' "$1"`)
	script := append(enterLine("plain words"), exitKeys()...)
	_, sent := runPackageFolders(t, nil, folders, []string{"gate"}, script)
	if len(sent) != 1 || sent[0] != "[checked] plain words" {
		t.Fatalf("sent %q, want the line reworded by the rule", sent)
	}
}

func TestLoopRuleNeverSeesACommandLine(t *testing.T) {
	// A rule that would block everything, and a line that is a command: the rule is not
	// asked, so the user's own commands cannot be intercepted, and the unknown one is
	// answered by the host.
	folders := rulePackage(`printf '{"verdict":"block","reason":"no"}'`)
	script := append(enterLine("/nonsense"), exitKeys()...)
	out, sent := runPackageLoop2(t, folders, script)
	if len(sent) != 0 {
		t.Fatalf("sent %q", sent)
	}
	if frameWith(out, "Held back by a rule") != "" || !strings.Contains(screen(out), "Unknown command /nonsense") {
		t.Errorf("a command line was shown to a rule, or the host did not answer it; last frame:\n%s", lastFrame(out))
	}
}

func runPackageLoop2(t *testing.T, folders map[string]string, script []scheduledEvent) (string, []string) {
	t.Helper()
	return runPackageFolders(t, nil, folders, []string{"gate"}, script)
}

func TestLoopRuleCanHoldALineBackAndTheUserKeepsIt(t *testing.T) {
	folders := rulePackage(`printf '{"verdict":"block","reason":"contains a secret"}'`)
	script := append(enterLine("my password is hunter2"), exitKeys()...)
	out, sent := runPackageLoop2(t, folders, script)
	if len(sent) != 0 {
		t.Fatalf("a held line was sent: %q", sent)
	}
	f := frameWith(out, "contains a secret")
	if f == "" {
		t.Fatalf("the reason is not shown; last frame:\n%s", lastFrame(out))
	}
	kept := false
	for _, fr := range strings.Split(out, frameBegin) {
		g := strings.Join(strings.Fields(ansi.Strip(fr)), " ")
		if strings.Contains(g, "contains a secret") && strings.Contains(g, "hunter2") {
			kept = true
		}
	}
	if !kept {
		t.Errorf("the held line is gone from the input; the user would have to type it again")
	}
}

func TestLoopRuleThatFailsLetsTheLinePassAndSaysSo(t *testing.T) {
	folders := rulePackage(`exit 3`)
	script := append(enterLine("hello"), exitKeys()...)
	out, sent := runPackageLoop2(t, folders, script)
	if len(sent) != 1 || sent[0] != "hello" {
		t.Fatalf("sent %q, want the line as typed (a broken rule fails open)", sent)
	}
	if !strings.Contains(screen(out), "rule gate/r") {
		t.Errorf("the failure is not shown; last frame:\n%s", lastFrame(out))
	}
}

func TestLoopRuleOfAnUnapprovedPackageDoesNotRun(t *testing.T) {
	folders := rulePackage(`printf '{"verdict":"block"}'`)
	script := append(enterLine("hello"), exitKeys()...)
	_, sent := runPackageFolders(t, nil, folders, nil, script)
	if len(sent) != 1 || sent[0] != "hello" {
		t.Fatalf("sent %q; a package the user did not approve must not touch their lines", sent)
	}
}

func TestLoopPackageCommandIsInTheMenu(t *testing.T) {
	script := append(typed("/shou"), exitKeys()...)
	out, _ := runPackageLoop(t, map[string]string{"loud": pkgDoc("loud", echoPkg)}, []string{"loud"}, script)
	if frameWith(out, "say it back") == "" {
		t.Errorf("the command is not in the / menu; last frame:\n%s", lastFrame(out))
	}
}
