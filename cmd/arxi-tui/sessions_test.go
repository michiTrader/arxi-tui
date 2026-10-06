package main

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/michiTrader/arxi_tui/internal/fold"
)

func ev(typ, text string) fold.Event {
	return fold.Event{Type: typ, Payload: map[string]any{"text": text}}
}

func newLog(t *testing.T) (*sessionLog, string) {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "sessions")
	l := newSessionLog(dir)
	n := 0
	l.now = func() time.Time { n++; return time.Date(2026, 10, 6, 12, 0, n, 0, time.UTC) }
	return l, dir
}

func chat(l *sessionLog, q, a string) {
	l.Record(ev("run.prompt", q))
	l.Record(ev("llm.response", a))
}

func TestSessionRoundTripKeepsTheConversationAndTheHistory(t *testing.T) {
	l, dir := newLog(t)
	chat(l, "hello", "hi there")
	l.Record(fold.Event{Type: "chat.tool", Payload: map[string]any{"name": "read_file", "arg": "a.go", "ok": true}})
	chat(l, "and now?", "now this")

	list := listSessions(dir)
	if len(list) != 1 || list[0].Title != "hello" {
		t.Fatalf("list = %+v", list)
	}
	evs, err := loadSession(dir, list[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(evs) != 5 {
		t.Fatalf("events = %d, want 5", len(evs))
	}
	for i, e := range evs {
		if e.Seq != int64(i+1) {
			t.Errorf("event %d has seq %d", i, e.Seq)
		}
	}
	// The same fold the screen uses rebuilds the transcript.
	st := fold.Fold(evs)
	var roles []string
	for _, h := range st.History {
		roles = append(roles, h.Role)
	}
	if got := strings.Join(roles, ","); got != "user,assistant,tool,user,assistant" {
		t.Errorf("transcript roles = %s", got)
	}
	h := historyFromEvents(evs)
	if len(h) != 4 || h[0].Text != "hello" || h[1].Text != "hi there" || h[3].Text != "now this" {
		t.Errorf("history = %+v", h)
	}
}

func TestSessionOnlyKeepsWhatBelongsInAConversation(t *testing.T) {
	l, dir := newLog(t)
	l.Record(ev("chat.warn", "stray notice before any question"))
	if len(listSessions(dir)) != 0 {
		t.Fatal("a notice with no question must not create a conversation")
	}
	l.Record(ev("run.prompt", "q"))
	l.Record(ev("chat.thinking", "secret reasoning"))
	l.Record(ev("chat.approval", "waiting"))
	l.Record(ev("llm.response", "a"))
	list := listSessions(dir)
	b, _ := os.ReadFile(filepath.Join(dir, list[0].ID+".jsonl"))
	if strings.Contains(string(b), "reasoning") || strings.Contains(string(b), "waiting") {
		t.Errorf("thinking and pending approvals are not saved: %s", b)
	}
}

func TestSessionClearStartsANewFile(t *testing.T) {
	l, dir := newLog(t)
	chat(l, "first", "a")
	l.End()
	chat(l, "second", "b")
	if got := len(listSessions(dir)); got != 2 {
		t.Fatalf("conversations = %d, want 2", got)
	}
}

func TestSessionContinueAppendsToTheOldFile(t *testing.T) {
	l, dir := newLog(t)
	chat(l, "first", "a")
	id := listSessions(dir)[0].ID
	l.End()
	l.Continue(id)
	chat(l, "more", "b")
	if got := len(listSessions(dir)); got != 1 {
		t.Fatalf("conversations = %d, want 1", got)
	}
	evs, _ := loadSession(dir, id)
	if len(evs) != 4 {
		t.Errorf("events = %d, want 4", len(evs))
	}
}

func TestSessionDamagedAndForeignLinesAreSkipped(t *testing.T) {
	l, dir := newLog(t)
	chat(l, "q", "a")
	id := listSessions(dir)[0].ID
	f, _ := os.OpenFile(filepath.Join(dir, id+".jsonl"), os.O_APPEND|os.O_WRONLY, 0o600)
	f.WriteString("{\"type\":\"run.started\",\"payload\":{}}\n")
	f.WriteString("{\"type\":\"llm.resp") // a crash cut the last line
	f.Close()
	evs, err := loadSession(dir, id)
	if err != nil || len(evs) != 2 {
		t.Fatalf("evs = %d, err = %v; want the 2 good events", len(evs), err)
	}
}

func TestSessionLoadRefusesBadIDs(t *testing.T) {
	_, dir := newLog(t)
	for _, id := range []string{"", "../x", "..\\x", "a/b", "20261006-120000-zzzz", "nope"} {
		if _, err := loadSession(dir, id); err == nil {
			t.Errorf("id %q must be refused", id)
		}
	}
	if _, err := loadSession(dir, "20261006-120000-abcd"); err == nil {
		t.Error("a conversation that is not there must say so")
	}
}

func TestSessionPrivacyAndOff(t *testing.T) {
	l, dir := newLog(t)
	chat(l, "q", "a")
	if runtime.GOOS != "windows" {
		st, _ := os.Stat(filepath.Join(dir, listSessions(dir)[0].ID+".jsonl"))
		if st.Mode().Perm() != 0o600 {
			t.Errorf("file mode %v, want 0600", st.Mode().Perm())
		}
		dst, _ := os.Stat(dir)
		if dst.Mode().Perm() != 0o700 {
			t.Errorf("folder mode %v, want 0700", dst.Mode().Perm())
		}
	}
	t.Setenv(sessionsEnv, "off")
	if sessionsDir() != "" {
		t.Error("ARXI_SESSIONS=off must turn saving off")
	}
	off := newSessionLog(sessionsDir())
	if err := off.Record(ev("run.prompt", "x")); err != nil {
		t.Errorf("a log that is off must be silent: %v", err)
	}
}

func TestSessionLongTextIsCutAndTheFileHasALimit(t *testing.T) {
	l, dir := newLog(t)
	l.Record(ev("run.prompt", "q"))
	l.Record(ev("llm.response", strings.Repeat("é", sessionFieldM)))
	b, _ := os.ReadFile(filepath.Join(dir, listSessions(dir)[0].ID+".jsonl"))
	if len(b) > 3*sessionFieldM {
		t.Errorf("a long answer was stored whole: %d bytes", len(b))
	}
	evs, err := loadSession(dir, listSessions(dir)[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := evs[1].Payload["text"].(string); !strings.HasSuffix(got, "…") || !strings.Contains(got, "é") {
		t.Errorf("the cut must end in an ellipsis on a whole character")
	}
	l.size = sessionFileM
	before, _ := os.ReadFile(filepath.Join(dir, listSessions(dir)[0].ID+".jsonl"))
	l.Record(ev("run.prompt", "one more"))
	after, _ := os.ReadFile(filepath.Join(dir, listSessions(dir)[0].ID+".jsonl"))
	if len(after) != len(before) {
		t.Error("a conversation at its size limit must stop growing")
	}
}

func TestSessionOldOnesArePruned(t *testing.T) {
	l, dir := newLog(t)
	for i := 0; i < 5; i++ {
		chat(l, "q", "a")
		l.End()
	}
	pruneSessions(dir, 3, "")
	if got := len(listSessions(dir)); got != 3 {
		t.Errorf("kept %d, want 3", got)
	}
}

func TestSessionWriteFailureIsReportedOnce(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "file")
	os.WriteFile(dir, []byte("x"), 0o600) // a file where the folder should be
	l := newSessionLog(filepath.Join(dir, "sessions"))
	err := l.Record(ev("run.prompt", "q"))
	if err == nil || !strings.Contains(err.Error(), "/resume") {
		t.Fatalf("first failure must be reported: %v", err)
	}
	if err := l.Record(ev("run.prompt", "q")); err != nil {
		t.Errorf("the second failure must be silent: %v", err)
	}
}

func TestResumeMenuRowsAndPick(t *testing.T) {
	l, dir := newLog(t)
	if rows := resumeMenuData(dir, time.Now()); len(rows) != 1 || rows[0].Ref != "" {
		t.Errorf("empty = %+v", rows)
	}
	chat(l, "how do I write a test in Go?", "like this")
	l.End()
	chat(l, "summarize the news", "here")
	rows := resumeMenuData(dir, time.Now())
	if len(rows) != 2 || rows[0].Name != "summarize the news" {
		t.Fatalf("newest must be first: %+v", rows)
	}
	if got := filterModels(rows, "test go"); len(got) != 1 || got[0].Name != "how do I write a test in Go?" {
		t.Errorf("typing filters by title: %+v", got)
	}
	if id := resumePick(rows[1].Ref); !sessionIDRe.MatchString(id) {
		t.Errorf("a pick must return the id: %q", id)
	}
	if off := resumeMenuData("", time.Now()); !strings.Contains(off[0].Name, "off") {
		t.Errorf("off = %+v", off)
	}
}

func TestSessionAge(t *testing.T) {
	now := time.Date(2026, 10, 6, 12, 0, 0, 0, time.UTC)
	for d, want := range map[time.Duration]string{
		10 * time.Second: "just now", 5 * time.Minute: "5 min ago", 3 * time.Hour: "3 h ago", 72 * time.Hour: "3 d ago",
	} {
		if got := sessionAge(now, now.Add(-d)); got != want {
			t.Errorf("%v -> %q, want %q", d, got, want)
		}
	}
	if got := sessionAge(now, now.Add(-90*24*time.Hour)); got != "2026-07-08" {
		t.Errorf("old = %q", got)
	}
}
