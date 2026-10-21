//go:build !windows

package extv2

import (
	"context"
	"os"
	"strings"
	"testing"
)

// ${text} is the answer of the model. Only an event on turn.done is ever given one, so
// every other place that writes it is refused when the package is saved, with the
// reason and the remedy.
func TestTextPlaceholderIsOnlyForATurnDoneEvent(t *testing.T) {
	ev := func(on string) string {
		return `"events":[{"id":"e","on":"` + on + `","op":{"kind":"exec","argv":["echo","${text}"]}}]`
	}
	accepted(t, autoDoc("", ev("turn.done")), Options{})
	for _, on := range []string{"turn.failed", "turn.started", "shell.ended", "context.high"} {
		refused(t, autoDoc("", ev(on)), Options{}, "${text} is the model's answer", "only an event on turn.done")
	}
	refused(t, autoDoc("", `"panels":[{"id":"p","title":"T","op":{"kind":"exec","argv":["echo","${text}"]}}]`),
		Options{}, "${text} is the model's answer")
	refused(t, autoDoc("", `"commands":[{"name":"c","op":{"kind":"exec","argv":["echo","${text}"]}}]`),
		Options{}, "${text} is the model's answer")
	refused(t, autoDoc("", `"services":[{"name":"w","description":"d","op":{"kind":"exec","argv":["echo","${text}"]}}]`),
		Options{}, "${text} is the model's answer")
	// The program itself and a header may not hold a placeholder at all, ${text} included.
	refused(t, autoDoc("", `"events":[{"id":"e","on":"turn.done","op":{"kind":"exec","argv":["${text}"]}}]`),
		Options{}, "placeholder")
	// A mistyped name is still refused, and the message now lists ${text}.
	refused(t, autoDoc("", `"events":[{"id":"e","on":"turn.done","op":{"kind":"exec","argv":["echo","${answer}"]}}]`),
		Options{}, "${answer}", "${text}")
}

func TestATurnDoneEventIsGivenTheAnswerCleanedAndBounded(t *testing.T) {
	r, _ := pkgDir(t, map[string]string{"say.sh": "#!/bin/sh\nprintf '%s' \"$1\"\n"})
	p := accepted(t, autoDoc("", `"events":[{"id":"e","on":"turn.done","op":{"kind":"exec","argv":["./say.sh","${text}"]}}]`), Options{})
	// An escape sequence in the answer must not reach the program or the screen.
	out, err := r.RunEvent(context.Background(), p, p.Events[0], "  hello \x1b[31mred\x1b[0m world \n")
	if err != nil || out != "hello red world" {
		t.Fatalf("got %q, %v; the answer must arrive trimmed and without escapes", out, err)
	}
	long := strings.Repeat("a", MaxEventTextRunes+500)
	out, err = r.RunEvent(context.Background(), p, p.Events[0], long)
	if err != nil {
		t.Fatal(err)
	}
	if n := len([]rune(out)); n > MaxEventTextRunes {
		t.Errorf("the program was given %d runes, more than the %d an answer is cut to; a long answer must not flood an argument", n, MaxEventTextRunes)
	}
}

// A hole is never filled with nothing: a turn that said nothing (it only used tools) has
// nothing to react to, and that is not an error to warn about.
func TestAnEventThatNeedsTheAnswerDoesNotRunWithoutOne(t *testing.T) {
	r, dir := pkgDir(t, map[string]string{"touch.sh": "#!/bin/sh\necho ran > \"$(dirname \"$0\")/ran\"\nprintf 'x'\n"})
	p := accepted(t, autoDoc("", `"events":[{"id":"e","on":"turn.done","op":{"kind":"exec","argv":["./touch.sh","${text}"]}}]`), Options{})
	for _, answer := range []string{"", "   \n\t", "\x1b[0m"} {
		out, err := r.RunEvent(context.Background(), p, p.Events[0], answer)
		if out != "" || err != nil {
			t.Errorf("answer %q: got %q, %v; want nothing and no error", answer, out, err)
		}
	}
	if _, err := readFile(dir + "/ran"); err == nil {
		t.Error("the program ran for an empty answer; a ${text} that is empty must not start it")
	}
	// An event that does not name ${text} is unaffected: it runs whatever the answer.
	q := accepted(t, autoDoc("", `"events":[{"id":"e","on":"turn.done","op":{"kind":"exec","argv":["./touch.sh"]}}]`), Options{})
	if out, err := r.RunEvent(context.Background(), q, q.Events[0], ""); err != nil || out != "x" {
		t.Errorf("an event with no ${text} must run on an empty answer; got %q, %v", out, err)
	}
}

// The runner checks again what the validator checked: a document that skipped it must
// not hand an answer to an event on another trigger.
func TestRunEventRefusesTextOnAnotherTriggerAtRunTime(t *testing.T) {
	r := &Runner{}
	p := &Package{Protocol: Protocol, ID: "demo"}
	e := Event{ID: "e", On: EventTurnFailed, Op: Op{Kind: KindExec, Argv: []string{"echo", "${text}"}}}
	if _, err := r.RunEvent(context.Background(), p, e, "secret answer"); err == nil || !strings.Contains(err.Error(), "only an event on turn.done") {
		t.Fatalf("got %v; an event on turn.failed must not be given the answer", err)
	}
}

// The consent screen names every power; reading every answer is one.
func TestConsentTextSaysThatAnEventReadsEveryAnswer(t *testing.T) {
	with := accepted(t, autoDoc("", `"events":[{"id":"e","on":"turn.done","op":{"kind":"exec","argv":["echo","${text}"]}}]`), Options{})
	without := accepted(t, autoDoc("", `"events":[{"id":"e","on":"turn.done","op":{"kind":"exec","argv":["echo","hi"]}}]`), Options{})
	const line = "read the text of every answer the model gives"
	if !strings.Contains(strings.Join(with.Capabilities(), "\n"), line) {
		t.Error("a package that reads the answers must say so on the consent screen")
	}
	if strings.Contains(strings.Join(without.Capabilities(), "\n"), line) {
		t.Error("a package that does not read the answers must not claim to")
	}
}

func readFile(p string) ([]byte, error) { return os.ReadFile(p) }
