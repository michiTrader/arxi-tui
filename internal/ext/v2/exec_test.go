//go:build !windows

package extv2

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

// pkgDir lays a package folder on disk with the given files (name -> content), all
// executable, and returns a Runner that resolves "./name" inside it.
func pkgDir(t *testing.T, files map[string]string) (*Runner, string) {
	t.Helper()
	dir := t.TempDir()
	for name, body := range files {
		p := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	return &Runner{PackageDir: func(string) string { return dir }}, dir
}

func execPkg(t *testing.T, argv, extra string) (*Package, Command) {
	t.Helper()
	p := accepted(t, `{`+head+`,"permissions":{"exec":["sh","echo","sleep"]},"commands":[{"name":"a","min_args":0`+extra+`,"op":{"kind":"exec","argv":`+argv+`}}]}`, Options{})
	return p, p.Commands[0]
}

func TestExecRunsAPackageScriptAndReturnsCleanText(t *testing.T) {
	r, _ := pkgDir(t, map[string]string{"hello.sh": "#!/bin/sh\nprintf 'hi %s \\033[31mred\\033[0m\\r\\n' \"$1\"\n"})
	p, c := execPkg(t, `["./hello.sh","${args}"]`, "")
	out, err := r.RunCommand(context.Background(), p, c, "Ana")
	if err != nil {
		t.Fatal(err)
	}
	if out.Say != "hi Ana red" {
		t.Fatalf("Say = %q", out.Say)
	}
}

func TestExecNeverUsesAShell(t *testing.T) {
	// The typed text is one argument. If a shell read it, the file would be created.
	r, dir := pkgDir(t, map[string]string{"echo.sh": "#!/bin/sh\nprintf '%s' \"$1\"\n"})
	p, c := execPkg(t, `["./echo.sh","${args}"]`, "")
	evil := "x; touch " + filepath.Join(dir, "PWNED") + " #"
	out, err := r.RunCommand(context.Background(), p, c, evil)
	if err != nil {
		t.Fatal(err)
	}
	if out.Say != evil {
		t.Fatalf("the text must arrive as one argument, got %q", out.Say)
	}
	if _, err := os.Stat(filepath.Join(dir, "PWNED")); err == nil {
		t.Fatal("a shell interpreted the typed text")
	}
	for _, evil := range []string{"$(touch " + filepath.Join(dir, "PWNED2") + ")", "`touch " + filepath.Join(dir, "PWNED2") + "`", "a | touch " + filepath.Join(dir, "PWNED2")} {
		if _, err := r.RunCommand(context.Background(), p, c, evil); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "PWNED2")); err == nil {
		t.Fatal("a shell interpreted the typed text")
	}
}

func TestExecScrubsTheEnvironment(t *testing.T) {
	r, _ := pkgDir(t, map[string]string{"env.sh": "#!/bin/sh\nenv\n"})
	r.exec.environ = func() []string {
		return []string{"PATH=/usr/bin:/bin", "HOME=/home/x", "OPENAI_API_KEY=sk-secret", "GITHUB_TOKEN=ghp_secret", "AWS_SECRET_ACCESS_KEY=aws", "ARXI_TOKEN=t", "LANG=C"}
	}
	p, c := execPkg(t, `["./env.sh"]`, "")
	out, err := r.RunCommand(context.Background(), p, c, "")
	if err != nil {
		t.Fatal(err)
	}
	for _, leak := range []string{"sk-secret", "ghp_secret", "aws", "ARXI_TOKEN", "OPENAI_API_KEY"} {
		if strings.Contains(out.Say, leak) {
			t.Fatalf("the child saw %q:\n%s", leak, out.Say)
		}
	}
	for _, keep := range []string{"PATH=", "HOME=/home/x", "LANG=C", "ARXI_PACKAGE_DIR="} {
		if !strings.Contains(out.Say, keep) {
			t.Fatalf("the child lacks %q:\n%s", keep, out.Say)
		}
	}
}

func TestExecOptionInjectionNeedsAllowDash(t *testing.T) {
	r, _ := pkgDir(t, map[string]string{"echo.sh": "#!/bin/sh\nprintf '%s' \"$1\"\n"})
	p, c := execPkg(t, `["./echo.sh","${args}"]`, "")
	_, err := r.RunCommand(context.Background(), p, c, "--upload-pack=evil")
	if err == nil || !strings.Contains(err.Error(), "read as an option") || !strings.Contains(err.Error(), "allow_dash") {
		t.Fatalf("got %v", err)
	}
	// Embedded in a longer argument it is not an option, so it is allowed.
	p2, c2 := execPkg(t, `["./echo.sh","q=${args}"]`, "")
	if out, err := r.RunCommand(context.Background(), p2, c2, "-x"); err != nil || out.Say != "q=-x" {
		t.Fatalf("out=%+v err=%v", out, err)
	}
	// The author may say they expect it.
	p3 := accepted(t, `{`+head+`,"commands":[{"name":"a","op":{"kind":"exec","allow_dash":true,"argv":["./echo.sh","${args}"]}}]}`, Options{})
	if out, err := r.RunCommand(context.Background(), p3, p3.Commands[0], "-x"); err != nil || out.Say != "-x" {
		t.Fatalf("out=%+v err=%v", out, err)
	}
}

func TestExecTimeoutKillsTheWholeProcessGroup(t *testing.T) {
	r, dir := pkgDir(t, map[string]string{"slow.sh": "#!/bin/sh\nsleep 30 &\necho $! > \"$ARXI_PACKAGE_DIR/child.pid\"\nsleep 30\n"})
	p, c := execPkg(t, `["./slow.sh"]`, "")
	c.Op.TimeoutMS = 300
	start := time.Now()
	_, err := r.RunCommand(context.Background(), p, c, "")
	if err == nil || !strings.Contains(err.Error(), "did not finish in time") {
		t.Fatalf("got %v", err)
	}
	if time.Since(start) > 3*time.Second {
		t.Fatalf("the timeout took %v", time.Since(start))
	}
	data, rerr := os.ReadFile(filepath.Join(dir, "child.pid"))
	if rerr != nil {
		t.Fatal(rerr)
	}
	var pid int
	for _, ch := range strings.TrimSpace(string(data)) {
		pid = pid*10 + int(ch-'0')
	}
	deadline := time.Now().Add(2 * time.Second)
	for {
		if syscall.Kill(pid, 0) != nil {
			return // gone
		}
		if time.Now().After(deadline) {
			_ = syscall.Kill(pid, syscall.SIGKILL)
			t.Fatalf("helper process %d outlived the timeout", pid)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestExecOutputIsBoundedWithoutBlockingTheChild(t *testing.T) {
	r, _ := pkgDir(t, map[string]string{"flood.sh": "#!/bin/sh\nhead -c 5000000 /dev/zero | tr '\\0' 'x'\n"})
	p, c := execPkg(t, `["./flood.sh"]`, "")
	start := time.Now()
	out, err := r.RunCommand(context.Background(), p, c, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Say) > MaxSayRunes+4 || !strings.HasSuffix(out.Say, "…") {
		t.Fatalf("len=%d", len(out.Say))
	}
	if time.Since(start) > 4*time.Second {
		t.Fatal("a flooding child was not drained promptly")
	}
}

func TestExecFailureReportsStderrCleanly(t *testing.T) {
	r, _ := pkgDir(t, map[string]string{"fail.sh": "#!/bin/sh\necho 'bad thing \033[31m' >&2\nexit 3\n"})
	p, c := execPkg(t, `["./fail.sh"]`, "")
	_, err := r.RunCommand(context.Background(), p, c, "")
	if err == nil || !strings.Contains(err.Error(), "fail.sh failed: bad thing") || strings.Contains(err.Error(), "\x1b") {
		t.Fatalf("got %v", err)
	}
}

func TestExecProgramConfinement(t *testing.T) {
	r, dir := pkgDir(t, map[string]string{"ok.sh": "#!/bin/sh\necho ok\n"})
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "evil.sh"), []byte("#!/bin/sh\necho evil\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	// A link inside the package that points outside it is how a file inside a folder
	// reaches one that the digest never covered.
	if err := os.Symlink(filepath.Join(outside, "evil.sh"), filepath.Join(dir, "link.sh")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(dir, "linkdir")); err != nil {
		t.Fatal(err)
	}
	mk := func(prog string) (*Package, Command) {
		return &Package{Protocol: Protocol, ID: "demo", Name: "d", Version: "1"}, Command{Name: "a", Op: Op{Kind: KindExec, Argv: []string{prog}}}
	}
	for _, prog := range []string{"./link.sh", "./linkdir/evil.sh", "./../x.sh", "./missing.sh", "/bin/sh", "sh"} {
		p, c := mk(prog)
		if _, err := r.RunCommand(context.Background(), p, c, ""); err == nil {
			t.Errorf("%s must be refused", prog)
		}
	}
	p, c := mk("./link.sh")
	if _, err := r.RunCommand(context.Background(), p, c, ""); err == nil || !strings.Contains(err.Error(), "leads outside the package folder") {
		t.Errorf("a link out of the package must say so: %v", err)
	}
	p, c = mk("./ok.sh")
	if out, err := r.RunCommand(context.Background(), p, c, ""); err != nil || out.Say != "ok" {
		t.Errorf("out=%+v err=%v", out, err)
	}
	// A package that was not installed as a folder has no files to run.
	p, c = mk("./ok.sh")
	if _, err := (&Runner{}).RunCommand(context.Background(), p, c, ""); err == nil || !strings.Contains(err.Error(), "not installed as a folder") {
		t.Errorf("got %v", err)
	}
}

func TestExecSystemProgramsNeedThePermission(t *testing.T) {
	if _, err := exec.LookPath("echo"); err != nil {
		t.Skip("no echo")
	}
	p := &Package{Protocol: Protocol, ID: "demo", Name: "d", Version: "1", Permissions: Permissions{Exec: []string{"echo"}}}
	c := Command{Name: "a", Op: Op{Kind: KindExec, Argv: []string{"echo", "${args}"}}}
	out, err := (&Runner{}).RunCommand(context.Background(), p, c, "hello")
	if err != nil || out.Say != "hello" {
		t.Fatalf("out=%+v err=%v", out, err)
	}
	p.Permissions.Exec = nil
	if _, err := (&Runner{}).RunCommand(context.Background(), p, c, "hello"); err == nil || !strings.Contains(err.Error(), "not in permissions.exec") {
		t.Fatalf("got %v", err)
	}
}

func TestExecOutcomeJSON(t *testing.T) {
	r, _ := pkgDir(t, map[string]string{
		"ok.sh":   "#!/bin/sh\necho '{\"verdict\":\"transform\",\"text\":\"fixed \\u001b[2J line\",\"say\":\"note\"}'\n",
		"bad.sh":  "#!/bin/sh\necho 'not json'\n",
		"key.sh":  "#!/bin/sh\necho '{\"verdict\":\"continue\",\"run\":\"rm -rf /\"}'\n",
		"verb.sh": "#!/bin/sh\necho '{\"verdict\":\"allow\"}'\n",
	})
	run := func(file string) (Outcome, error) {
		p := accepted(t, `{`+head+`,"rules":[{"id":"r","stage":"input.pre","op":{"kind":"exec","result":"outcome","argv":["./`+file+`"]}}]}`, Options{})
		return r.RunRule(context.Background(), p, p.Rules[0], "hello")
	}
	out, err := run("ok.sh")
	if err != nil || out.Verdict != "transform" || out.Text != "fixed  line" || out.Say != "note" {
		t.Fatalf("out=%+v err=%v", out, err)
	}
	for file, want := range map[string]string{"bad.sh": "not a JSON outcome", "key.sh": "unknown field", "verb.sh": `verdict "allow"`} {
		if _, err := run(file); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%s: err=%v want %q", file, err, want)
		}
	}
}

func TestExecRuleReceivesTheLineAsOneArgument(t *testing.T) {
	r, _ := pkgDir(t, map[string]string{"up.sh": "#!/bin/sh\nprintf '%s' \"$1\" | tr a-z A-Z\n"})
	p := accepted(t, `{`+head+`,"rules":[{"id":"up","stage":"input.pre","op":{"kind":"exec","argv":["./up.sh","${input}"]}}]}`, Options{})
	out, err := r.RunRule(context.Background(), p, p.Rules[0], "hello world; $(x)")
	if err != nil || out.Verdict != VerdictTransform || out.Text != "HELLO WORLD; $(X)" {
		t.Fatalf("out=%+v err=%v", out, err)
	}
}

func TestUsageErrorRunsNothing(t *testing.T) {
	r, dir := pkgDir(t, map[string]string{"touch.sh": "#!/bin/sh\ntouch \"$ARXI_PACKAGE_DIR/RAN\"\n"})
	p := accepted(t, `{`+head+`,"commands":[{"name":"a","min_args":1,"usage":"/a <x>","op":{"kind":"exec","argv":["./touch.sh","${1}"]}}]}`, Options{})
	_, err := r.RunCommand(context.Background(), p, p.Commands[0], "   ")
	var ue *UsageError
	if err == nil || !strings.Contains(err.Error(), "Usage: /a <x>") || !errorsAs(err, &ue) {
		t.Fatalf("got %v", err)
	}
	if _, serr := os.Stat(filepath.Join(dir, "RAN")); serr == nil {
		t.Fatal("a command with a missing argument must not run")
	}
}

func TestExecOutcomeModeRefusesOutputBeyondTheCap(t *testing.T) {
	// A JSON outcome cut at the cap is not a whole document; it must be refused
	// rather than parsed from a prefix. Without the cap this 100 KB output would
	// be read in full.
	r, _ := pkgDir(t, map[string]string{"big.sh": "#!/bin/sh\nprintf '{\"verdict\":\"continue\",\"say\":\"'\nhead -c 100000 /dev/zero | tr '\\0' 'x'\nprintf '\"}'\n"})
	p := accepted(t, `{`+head+`,"permissions":{},"commands":[{"name":"a","min_args":0,"op":{"kind":"exec","argv":["./big.sh"],"result":"outcome"}}]}`, Options{})
	_, err := r.RunCommand(context.Background(), p, p.Commands[0], "")
	if err == nil || !strings.Contains(err.Error(), "longer than") {
		t.Fatalf("got %v", err)
	}
}

func TestExecRefusesNULInAnArgument(t *testing.T) {
	r, _ := pkgDir(t, map[string]string{"echo.sh": "#!/bin/sh\necho \"$1\"\n"})
	p, c := execPkg(t, `["./echo.sh","${args}"]`, "")
	_, err := r.RunCommand(context.Background(), p, c, "a\x00b")
	if err == nil || !strings.Contains(err.Error(), "NUL") {
		t.Fatalf("got %v", err)
	}
}

// osStat is os.Stat under a name the service tests share.
func osStat(p string) (os.FileInfo, error) { return os.Stat(p) }
