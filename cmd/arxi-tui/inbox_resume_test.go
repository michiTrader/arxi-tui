package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The tests below run the test binary itself as a stand-in for the core: with
// FAKE_ARXI set, TestMain-free helper behaviour is selected by TestFakeArxi.
func fakeArxi(t *testing.T, mode string) (string, func()) {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	return exe, func() {}
}

// TestFakeArxi is not a test: when FAKE_ARXI is set the process pretends to be the
// core's `inbox approve|reject --resume` command.
func TestFakeArxi(t *testing.T) {
	mode := os.Getenv("FAKE_ARXI")
	if mode == "" {
		t.Skip("helper process")
	}
	args := os.Args
	for i, a := range args {
		if a == "--" {
			args = args[i+1:]
			break
		}
	}
	if log := os.Getenv("FAKE_ARXI_ARGS"); log != "" {
		_ = os.WriteFile(log, []byte(strings.Join(args, "\x00")), 0o600)
	}
	switch mode {
	case "ok":
		os.Stdout.WriteString("approved. backend unblocked (r1 seq 7)\n  continuing run r1\nrun r1 succeeded (seq 19)\n")
		os.Exit(0)
	case "saved-then-fail":
		os.Stdout.WriteString("approved. backend unblocked (r1 seq 7)\n")
		os.Stderr.WriteString("arxi inbox: answered, but the run cannot be continued: run r1 predates effective-config.v1.json\n")
		os.Exit(3)
	case "refuse":
		os.Stderr.WriteString("arxi inbox approve: no pending question \"inb-7\" in any run under runs.\n  see what is waiting: arxi inbox\n")
		os.Exit(1)
	}
	os.Exit(0)
}

func runFakeAnswer(t *testing.T, mode string, args []string) (saved int, err error, argv []string) {
	t.Helper()
	exe, _ := fakeArxi(t, mode)
	logf := t.TempDir() + "/args"
	t.Setenv("FAKE_ARXI", mode)
	t.Setenv("FAKE_ARXI_ARGS", logf)
	full := append([]string{"-test.run=^TestFakeArxi$", "--"}, args...)
	err = runAnswer(context.Background(), exe, t.TempDir(), full, func() { saved++ })
	if b, rerr := os.ReadFile(logf); rerr == nil {
		argv = strings.Split(string(b), "\x00")
	}
	return saved, err, argv
}

func TestAnswerArgsNameTheRunAndResume(t *testing.T) {
	got := strings.Join(answerArgs("approve", "inb-7", "r1", ""), " ")
	if got != "inbox approve inb-7 --run r1 --resume" {
		t.Errorf("approve args = %q", got)
	}
	got = strings.Join(answerArgs("reject", "inb-7", "r1", "too risky"), "|")
	if got != "inbox|reject|inb-7|--run|r1|--resume|--reason|too risky" {
		t.Errorf("reject args = %q", got)
	}
}

func TestAnAnswerIsReportedSavedOnceAndFinishesCleanly(t *testing.T) {
	saved, err, argv := runFakeAnswer(t, "ok", answerArgs("approve", "inb-7", "r1", ""))
	if err != nil || saved != 1 {
		t.Fatalf("saved=%d err=%v", saved, err)
	}
	if strings.Join(argv, " ") != "inbox approve inb-7 --run r1 --resume" {
		t.Errorf("the core received %q", argv)
	}
}

func TestARunThatCannotGoOnStillReportsTheSavedAnswer(t *testing.T) {
	saved, err, _ := runFakeAnswer(t, "saved-then-fail", answerArgs("approve", "inb-7", "r1", ""))
	if saved != 1 {
		t.Errorf("the answer was saved before the failure, saved=%d", saved)
	}
	if err == nil || !strings.Contains(err.Error(), "cannot be continued") {
		t.Fatalf("err = %v", err)
	}
}

func TestARefusedAnswerIsOnePlainSentence(t *testing.T) {
	saved, err, _ := runFakeAnswer(t, "refuse", answerArgs("approve", "inb-7", "r1", ""))
	if saved != 0 || err == nil {
		t.Fatalf("saved=%d err=%v", saved, err)
	}
	msg := err.Error()
	if strings.Contains(msg, "arxi ") || strings.Contains(msg, "see") {
		t.Errorf("command-line teaching leaked into %q", msg)
	}
	if !strings.Contains(msg, "no pending question") {
		t.Errorf("the reason is missing: %q", msg)
	}
}

func TestNoCoreMeansASentenceNotACrash(t *testing.T) {
	d := &serveDriver{}
	if err := d.AnswerAndResume(context.Background(), "approve", "i", "", nil); err == nil || !strings.Contains(err.Error(), "no arxi core") {
		t.Errorf("err = %v", err)
	}
	d = &serveDriver{bin: "x"}
	if err := d.AnswerAndResume(context.Background(), "approve", "i", "", nil); err == nil || !strings.Contains(err.Error(), "no run is being followed") {
		t.Errorf("err = %v", err)
	}
}

// legacyBlockedLog is a run that waits on one bash approval and predates the
// frozen execution contract: the core can still record an answer for it but can
// not carry it on, which is exactly the "saved, then could not go on" path.
const legacyBlockedLog = `{"id":"e1","seq":1,"type":"run.started","payload":{"actor":"team","run_id":"r1","budget_usd":5.0}}
{"id":"e2","seq":2,"type":"stage.entered","payload":{"stage":"execute","index":0}}
{"id":"e3","seq":3,"type":"agent.activated","actor":"backend","payload":{"agent":"backend"}}
{"id":"e4","seq":4,"type":"authorization.requested","source":"runtime","actor":"backend","payload":{"schema":"arxi.authorization/v1","authorization_id":"authorization-1","inbox_id":"inbox-1","requester_principal":"agent:backend","suspension_id":"suspension-1","parent_work_id":"parent-1","provider_call_id":"call-1","tool":"bash","argument_digest":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","action_digest":"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb","tool_schema_version":"arxi.tool.bash/v1","policy_version":"policy-1","workspace_profile_id":"workspace-1","expires_at":"2099-09-12T00:00:00Z","after_ms":60000}}
{"id":"e5","seq":5,"type":"inbox.created","source":"runtime","payload":{"inbox_id":"inbox-1","kind":"tool_approval","question":"allow bash?","agent":"backend","on_timeout":"deny","authorization_id":"authorization-1","action_digest":"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"}}
`

// TestRealCoreRecordsAnswersAndSaysSoInPlainWords runs the real core's answer
// command the way the TUI does: the approval is saved and the failure to carry on
// comes back as a sentence; a rejection needs its reason; an unknown item is
// refused without command-line teaching.
func TestRealCoreRecordsAnswersAndSaysSoInPlainWords(t *testing.T) {
	bin := buildCore(t)
	work := t.TempDir()
	t.Setenv("ARXI_PROVIDERS_DIR", filepath.Join(work, "providers"))
	run := filepath.Join(work, "runs", "r1")
	if err := os.MkdirAll(run, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(run, "events.ndjson"), []byte(legacyBlockedLog), 0o644); err != nil {
		t.Fatal(err)
	}
	snapshot := "name: team\nmembers:\n  - name: backend\n    tools: [read, write, bash]\nstages:\n  - name: execute\n    advance_when: all\n"
	if err := os.WriteFile(filepath.Join(run, "blueprint.snapshot.yaml"), []byte(snapshot), 0o644); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()

	saved := 0
	err := runAnswer(ctx, bin, work, answerArgs("approve", "inbox-1", "r1", ""), func() { saved++ })
	if saved != 1 {
		t.Fatalf("the approval must be reported saved once, got %d (err %v)", saved, err)
	}
	if err == nil || strings.Contains(err.Error(), "arxi ") {
		t.Fatalf("a run that cannot go on must say so in plain words, got %v", err)
	}
	b, _ := os.ReadFile(filepath.Join(run, "events.ndjson"))
	if !strings.Contains(string(b), `"inbox.replied"`) {
		t.Errorf("the answer is not in the log:\n%s", b)
	}

	// The same item cannot be answered twice, and the refusal is one sentence.
	err = runAnswer(ctx, bin, work, answerArgs("approve", "inbox-1", "r1", ""), nil)
	if err == nil || strings.Contains(err.Error(), "arxi ") || strings.Contains(err.Error(), "see ") {
		t.Errorf("second answer = %v", err)
	}
	// A run the core does not know is refused the same way.
	err = runAnswer(ctx, bin, work, answerArgs("approve", "inbox-1", "ghost", ""), nil)
	if err == nil || !strings.Contains(err.Error(), "no pending") {
		t.Errorf("unknown run = %v", err)
	}
}
