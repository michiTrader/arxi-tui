package main

import (
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/michiTrader/arxi_tui/internal/engine"
	"github.com/michiTrader/arxi_tui/internal/fold"
	"github.com/michiTrader/arxi_tui/internal/scene"
)

// A local warning is the host talking to the user about the line they just typed. It
// is a plain chat.warn event that the loop appends to its own log: it never goes
// through the driver's channel, so it is not saved with the conversation and it is
// not part of what the model is told. These tests pin the properties that make that
// safe; the loop wiring that calls localWarning arrives with the commands that need it.

func warnText(t *testing.T, e fold.Event) string {
	t.Helper()
	s, ok := e.Payload["text"].(string)
	if !ok {
		t.Fatalf("the warning has no text payload: %+v", e.Payload)
	}
	return s
}

func TestLocalWarningIsAChatWarnEvent(t *testing.T) {
	e := localWarning("Unknown command /nonsense.")
	if e.Type != "chat.warn" {
		t.Fatalf("type = %q, want chat.warn.\nconsequence: the fold has no case for any other type, so the user would see nothing.\nremedy: reuse chat.warn; do not invent a local event type.", e.Type)
	}
	if got := warnText(t, e); got != "Unknown command /nonsense." {
		t.Errorf("text = %q, want it unchanged when it is already plain", got)
	}
}

func TestLocalWarningStripsEscapesAndControls(t *testing.T) {
	cases := []struct{ name, in, want string }{
		{"colour sequence", "bad \x1b[31mred\x1b[0m word", "bad red word"},
		{"cursor move", "a\x1b[2J\x1b[Hb", "ab"},
		{"osc title with bel", "x\x1b]0;owned\x07y", "xy"},
		{"osc title with st", "x\x1b]0;owned\x1b\\y", "xy"},
		{"bare bell and nul", "a\x07b\x00c", "abc"},
		{"carriage return cannot overwrite the line", "real\rfake", "real fake"},
		{"newline and tab become one space", "a\nb\tc", "a b c"},
		{"c1 control", "a\u009bb", "ab"},
		{"bidi override", "evil\u202egpj.exe", "evilgpj.exe"},
		{"invalid utf-8", "a\xffb", "ab"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := warnText(t, localWarning(c.in))
			if got != c.want {
				t.Errorf("localWarning(%q) = %q, want %q.\nconsequence: the warning repeats what the user typed, and a raw escape in it would be written to the terminal as a command.\nremedy: strip escape sequences and control characters before the text becomes an event.", c.in, got, c.want)
			}
		})
	}
}

func TestLocalWarningIsBounded(t *testing.T) {
	long := strings.Repeat("é", 5000)
	got := warnText(t, localWarning("Unknown command /"+long))
	if n := utf8.RuneCountInString(got); n > localWarnMax {
		t.Errorf("the warning is %d runes, the most is %d.\nconsequence: a pasted megabyte would fill the transcript with one warning and be re-folded on every frame.\nremedy: cut the text and end it with an ellipsis.", n, localWarnMax)
	}
	if !strings.HasSuffix(got, "…") {
		t.Errorf("a cut warning must say it was cut; got tail %q", got[len(got)-4:])
	}
	if !utf8.ValidString(got) {
		t.Errorf("the cut split a character: %q", got)
	}
}

func TestLocalWarningIsVisibleInTheTranscript(t *testing.T) {
	doc, err := scene.ParseDocument([]byte(factoryRAW))
	if err != nil {
		t.Fatal(err)
	}
	evs := []fold.Event{
		{Type: "run.prompt", Seq: 1, Payload: map[string]any{"text": "hello"}},
		{Type: "llm.response", Seq: 2, Payload: map[string]any{"text": "hi there"}},
		localWarning("Unknown command /nonsense. It was not sent to the model."),
	}
	r := engine.Renderer{Width: 80, Height: 24}
	got := r.RenderFrame(doc, fold.Fold(evs)).Plain()
	if !strings.Contains(got, "! Unknown command /nonsense.") {
		t.Errorf("the warning is not drawn with the warning marker in the transcript:\n%s", got)
	}
}

func TestLocalWarningNeverReachesTheModel(t *testing.T) {
	warn := localWarning("Unknown command /secret-looking-text.")
	// A warning between a question and its answer, and one after an unanswered
	// question, are the two places a naive history builder would pick it up.
	evs := []fold.Event{
		{Type: "run.prompt", Payload: map[string]any{"text": "q1"}},
		warn,
		{Type: "llm.response", Payload: map[string]any{"text": "a1"}},
		{Type: "run.prompt", Payload: map[string]any{"text": "q2"}},
		warn,
		{Type: "chat.error", Payload: map[string]any{"text": "boom"}},
	}
	for _, turn := range historyFromEvents(evs) {
		if strings.Contains(turn.Text, "secret-looking-text") {
			t.Errorf("the model would be told the host's own warning (%s turn): %q", turn.Role, turn.Text)
		}
	}
}

func TestLocalWarningIsNotCopiedWithTheConversation(t *testing.T) {
	evs := []fold.Event{
		{Type: "run.prompt", Payload: map[string]any{"text": "hello"}},
		{Type: "llm.response", Payload: map[string]any{"text": "hi there"}},
		localWarning("Unknown command /nonsense."),
	}
	h := fold.Fold(evs).History
	for _, pick := range []string{"all", "last", "1"} {
		text, _, err := copyText(h, pick)
		if err != nil {
			t.Fatalf("copyText(%q): %v", pick, err)
		}
		if strings.Contains(text, "nonsense") {
			t.Errorf("/copy %s put the host's warning on the clipboard: %q", pick, text)
		}
	}
}

func TestAppendLocalWarningKeepsTheLogOrderedAndUntouched(t *testing.T) {
	log := []fold.Event{
		{Type: "run.prompt", Seq: 7, Payload: map[string]any{"text": "q"}},
		{Type: "llm.response", Seq: 9, Payload: map[string]any{"text": "a"}},
	}
	got := appendLocalWarning(log, "Unknown command /x.")
	if len(log) != 2 {
		t.Fatalf("the caller's log was changed: %d events", len(log))
	}
	if len(got) != 3 || got[2].Type != "chat.warn" || got[2].Seq != 10 {
		t.Fatalf("got %+v, want the warning last with Seq 10 (after the last event)", got)
	}
	if first := appendLocalWarning(nil, "x"); len(first) != 1 || first[0].Type != "chat.warn" {
		t.Errorf("a warning on an empty log must be the only event, got %+v", first)
	}
}

// The warning is drawn with the old and new history intact around it, and after /clear
// replaces the log with nothing the transcript has no trace of it.
func TestLocalWarningDisappearsWhenTheLogIsReplaced(t *testing.T) {
	log := appendLocalWarning([]fold.Event{
		{Type: "run.prompt", Seq: 1, Payload: map[string]any{"text": "hello"}},
	}, "Unknown command /nonsense.")
	if h := fold.Fold(log).History; len(h) != 2 || h[1].Role != "warn" {
		t.Fatalf("history before = %+v, want the question then the warning", h)
	}
	log = nil // what /clear does to the loop's log
	if h := fold.Fold(log).History; len(h) != 0 {
		t.Errorf("history after the log was replaced = %+v, want empty", h)
	}
}
