package main

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/michiTrader/arxi_tui/internal/defaultscene"
	"github.com/michiTrader/arxi_tui/internal/fold"
	"github.com/michiTrader/arxi_tui/internal/scene"
	"github.com/michiTrader/arxi_tui/internal/term"
	"github.com/michiTrader/arxi_tui/internal/theme"
)

// queueLoop runs the real loop on the built-in scene. feed plays what the core would say,
// on its own goroutine; script is what the user presses. It returns the frames (without
// colour) and, once the loop has ended, what the driver was asked to send.
func queueLoop(t *testing.T, drv Driver, evCh chan fold.Event, script []scheduledEvent, feed func(send func(fold.Event))) []string {
	t.Helper()
	t.Setenv(configDirEnv, t.TempDir())
	t.Setenv(historyEnv, t.TempDir())
	t.Setenv(sessionsEnv, "")
	doc, err := scene.ParseNamed(defaultscene.Name, defaultscene.JSON)
	if err != nil {
		t.Fatal(err)
	}
	tty := newFakeTTY(100, 30, script)
	go feed(func(e fold.Event) { evCh <- e })
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := loop(ctx, tty, doc, theme.SOBRIA(), evCh, drv, ""); err != nil {
		t.Fatalf("loop: %v", err)
	}
	tty.mu.Lock()
	defer tty.mu.Unlock()
	out := make([]string, len(tty.frames))
	for i, f := range tty.frames {
		out[i] = stripANSI(f)
	}
	return out
}

func working() fold.Event {
	return fold.Event{Type: "agent.activated", Seq: 10, Payload: map[string]any{"agent": "assistant"}}
}

func turnDone(seq int64) fold.Event {
	return fold.Event{Type: "agent.turn_done", Seq: seq, Payload: map[string]any{"agent": "assistant"}}
}

func enterAfter(d time.Duration) scheduledEvent { return scheduledEvent{d, enterEvent()} }

func leave(after time.Duration) []scheduledEvent {
	return []scheduledEvent{{after, ctrlCharEvent('c')}, {50 * time.Millisecond, ctrlCharEvent('c')}}
}

func sendLine(s string) []scheduledEvent {
	return append([]scheduledEvent{{300 * time.Millisecond, keyEvent(rune(s[0]))}}, append(typed(s[1:]), enterAfter(20*time.Millisecond))...)
}

func newQueueDriver() *testDriver { return &testDriver{evCh: make(chan fold.Event, 64), seq: 1000} }

// The line typed while an answer is being written is not lost and is not sent into a busy
// session: it waits, and goes out when the answer ends.
func TestEnterWhileTheAgentWorksQueuesTheLineAndSendsItWhenTheAnswerEnds(t *testing.T) {
	drv := newQueueDriver()
	frames := queueLoop(t, drv, drv.evCh, append(sendLine("second question"), leave(1600*time.Millisecond)...), func(send func(fold.Event)) {
		time.Sleep(100 * time.Millisecond)
		send(working())
		time.Sleep(900 * time.Millisecond) // Enter was pressed at about 0.4 s
		if drv.submittedCount() != 0 {
			t.Errorf("the line reached the driver while the agent worked. Consequence: a busy session refuses it and the text is lost. Remedy: Enter while agent.working must queue.push.")
		}
		send(turnDone(20))
	})
	if len(drv.submitted) != 1 || drv.submitted[0] != "second question" {
		t.Fatalf("submitted = %q, want the queued line sent once after the answer ended.\nConsequence: a message written while the agent worked never reaches it.\nRemedy: drain the queue on agent.turn_done.", drv.submitted)
	}
	if i := lastWith(frames, "queued (1 waiting)"); i < 0 {
		t.Error("no note said the line was queued. Consequence: the user cannot tell the line was kept. Remedy: queue.push says so in the conversation.")
	}
}

// One line goes out per finished answer, in the order they were written: the second waits
// for the answer to the first, it is not sent on top of it.
func TestTwoQueuedLinesGoOutInOrderOneAnswerAtATime(t *testing.T) {
	drv := newQueueDriver()
	script := append(sendLine("first"), sendLine("second")...)
	queueLoop(t, drv, drv.evCh, append(script, leave(3000*time.Millisecond)...), func(send func(fold.Event)) {
		time.Sleep(100 * time.Millisecond)
		send(working())
		time.Sleep(1000 * time.Millisecond)
		send(turnDone(20)) // answer 0 ends: "first" goes out
		time.Sleep(700 * time.Millisecond)
		if n := drv.submittedCount(); n != 1 {
			t.Errorf("%d lines sent after one answer ended, want 1.\nConsequence: the second line is sent on top of the answer to the first.\nRemedy: drain one message per agent.turn_done.", n)
		}
		send(working()) // the core starts answering "first"
		time.Sleep(300 * time.Millisecond)
		send(turnDone(30)) // ... and ends: "second" goes out
	})
	if len(drv.submitted) != 2 || drv.submitted[0] != "first" || drv.submitted[1] != "second" {
		t.Fatalf("submitted = %q, want [first second].\nConsequence: queued lines are lost or reordered.\nRemedy: msgQ is a FIFO drained on each turn_done.", drv.submitted)
	}
}

// The user stopped the answer: sending the queue on would answer a question they just
// stopped, and dropping it would lose what they wrote. It comes back on the line.
func TestAStoppedAnswerGivesTheQueuedLinesBackOnTheLine(t *testing.T) {
	drv := newQueueDriver()
	frames := queueLoop(t, drv, drv.evCh, append(sendLine("keep me"), leave(1600*time.Millisecond)...), func(send func(fold.Event)) {
		time.Sleep(100 * time.Millisecond)
		send(working())
		time.Sleep(900 * time.Millisecond)
		send(fold.Event{Type: "chat.cancelled", Seq: 20})
	})
	if len(drv.submitted) != 0 {
		t.Fatalf("submitted = %q after the answer was stopped. Consequence: a line the user wrote for an answer they cancelled is sent anyway. Remedy: give the queue back on chat.cancelled.", drv.submitted)
	}
	// The first Ctrl-C that leaves the program clears the line, so the line is read in the
	// first frame that says it came back, not in a later one.
	i := -1
	for j, f := range frames {
		if strings.Contains(f, "back on the line") {
			i = j
			break
		}
	}
	if i < 0 {
		t.Fatal("nothing said the queued line came back. Remedy: say so in the conversation.")
	}
	if !strings.Contains(frames[i], "┃ keep me") {
		t.Errorf("the line did not come back on the input.\nConsequence: what the user wrote is gone.\nRemedy: giveBackQueue puts it on the input.\nframe:\n%s", frames[i])
	}
}

func TestAFailedAnswerGivesTheQueuedLinesBackToo(t *testing.T) {
	drv := newQueueDriver()
	frames := queueLoop(t, drv, drv.evCh, append(sendLine("keep me"), leave(1600*time.Millisecond)...), func(send func(fold.Event)) {
		time.Sleep(100 * time.Millisecond)
		send(working())
		time.Sleep(900 * time.Millisecond)
		send(fold.Event{Type: "agent.failed", Seq: 20, Payload: map[string]any{"agent": "assistant"}})
	})
	if len(drv.submitted) != 0 {
		t.Fatalf("submitted = %q after the answer failed. Remedy: give the queue back on agent.failed.", drv.submitted)
	}
	i := firstWith(frames, "back on the line")
	if i < 0 || !strings.Contains(frames[i], "┃ keep me") {
		t.Errorf("the line did not come back after a failed answer. Consequence: what the user wrote is stuck in a queue nothing drains.\nRemedy: giveBackQueue on agent.failed.")
	}
}

func firstWith(frames []string, piece string) int {
	for i, f := range frames {
		if strings.Contains(f, piece) {
			return i
		}
	}
	return -1
}

// A queued line is a line sent: it is in the history, so Up brings it back.
func TestAQueuedLineIsInTheHistory(t *testing.T) {
	drv := newQueueDriver()
	script := append(sendLine("remember me"), scheduledEvent{100 * time.Millisecond, arrowEvent(term.KeyUp)})
	frames := queueLoop(t, drv, drv.evCh, append(script, leave(500*time.Millisecond)...), func(send func(fold.Event)) {
		time.Sleep(100 * time.Millisecond)
		send(working())
	})
	// The note is in the chat and the line is on the input again only after Up: before Enter
	// the note was not there, after it the input was empty.
	back := false
	for _, f := range frames {
		if strings.Contains(f, "queued (1 waiting)") && strings.Contains(f, "┃ remember me") {
			back = true
		}
	}
	if !back {
		t.Errorf("Up after queueing did not bring the line back.\nConsequence: a line sent while the agent worked is the only one missing from the history.\nRemedy: queue.push adds the line to the history.")
	}
}

// A picture cannot wait in the queue (the marker would outlive its picture): the line is
// kept on the input, no picture is left attached for another message, and the user is told.
func TestALineWithAPictureIsKeptOnTheInputNotQueued(t *testing.T) {
	p := writePic(t, "wait.png", pngBytes(t))
	drv := newQueueDriver()
	var script []scheduledEvent
	script = append(script, scheduledEvent{300 * time.Millisecond, keyEvent('/')})
	script = append(script, typed("image "+p)...)
	script = append(script, enterAfter(50*time.Millisecond))
	script = append(script, typed("look")...)
	script = append(script, enterAfter(50*time.Millisecond))
	frames := queueLoop(t, drv, drv.evCh, append(script, leave(600*time.Millisecond)...), func(send func(fold.Event)) {
		time.Sleep(100 * time.Millisecond)
		send(working())
	})
	if firstWith(frames, "cannot wait in the queue") < 0 {
		t.Error("nothing said a line with a picture cannot be queued. Remedy: queue.push refuses it and says why.")
	}
	if lastWith(frames, "queued (") >= 0 {
		t.Error("a line with a picture was queued. Consequence: the picture would be sent with a different message, or lost.")
	}
	if n := len(drv.attached); n == 0 || len(drv.attached[n-1]) != 0 {
		t.Errorf("pictures still attached after the refusal: %v. Consequence: they would ride with the next unrelated message.", drv.attached)
	}
}

// When nothing is being written Enter sends at once, as it always did.
func TestEnterWhenIdleStillSendsAtOnce(t *testing.T) {
	drv := newQueueDriver()
	queueLoop(t, drv, drv.evCh, append(sendLine("hello"), leave(500*time.Millisecond)...), func(send func(fold.Event)) {})
	if len(drv.submitted) != 1 || drv.submitted[0] != "hello" {
		t.Fatalf("submitted = %q, want hello sent at once. Consequence: the queue swallowed an ordinary send.", drv.submitted)
	}
}

// A command is not a message: it runs at once even while an answer is written, and is
// never queued as text for the model.
func TestACommandWhileTheAgentWorksIsNotQueued(t *testing.T) {
	drv := newQueueDriver()
	frames := queueLoop(t, drv, drv.evCh, append(sendLine("/effort high"), leave(900*time.Millisecond)...), func(send func(fold.Event)) {
		time.Sleep(100 * time.Millisecond)
		send(working())
	})
	if len(drv.submitted) != 0 {
		t.Errorf("submitted = %q; a command must not reach the model", drv.submitted)
	}
	if lastWith(frames, "queued (") >= 0 {
		t.Error("a command was queued as a message. Consequence: it would be sent to the model as text. Remedy: the binding must not apply while the slash menu is open.")
	}
}

func TestAnEmptyLineWhileTheAgentWorksQueuesNothing(t *testing.T) {
	drv := newQueueDriver()
	frames := queueLoop(t, drv, drv.evCh, append([]scheduledEvent{enterAfter(400 * time.Millisecond)}, leave(900*time.Millisecond)...), func(send func(fold.Event)) {
		time.Sleep(100 * time.Millisecond)
		send(working())
	})
	if lastWith(frames, "queued (") >= 0 {
		t.Error("an empty line was queued")
	}
}

// A question waits for y/n: Enter is not the answer and the line is not a message for later.
func TestEnterWhileAChangeWaitsForAnAnswerQueuesNothing(t *testing.T) {
	drv := &approvingDriver{testDriver: *newQueueDriver()}
	frames := queueLoop(t, drv, drv.evCh, append(sendLine("later"), leave(900*time.Millisecond)...), func(send func(fold.Event)) {
		time.Sleep(100 * time.Millisecond)
		send(working())
	})
	if lastWith(frames, "queued (") >= 0 {
		t.Error("a line was queued while a question waited. Consequence: text typed for the question would be sent as a message. Remedy: the binding is off while approval.pending.")
	}
}

// The ninth line is kept on the input and the user is told, not silently dropped.
func TestTheQueueHasALimitAndTheLineIsKeptWhenItIsFull(t *testing.T) {
	drv := newQueueDriver()
	var script []scheduledEvent
	for i := 0; i < queueMax+1; i++ {
		script = append(script, sendLine("m"+string(rune('a'+i)))...)
	}
	frames := queueLoop(t, drv, drv.evCh, append(script, leave(800*time.Millisecond)...), func(send func(fold.Event)) {
		time.Sleep(100 * time.Millisecond)
		send(working())
	})
	all := strings.Join(frames, "\n")
	if !strings.Contains(all, "messages are already waiting") {
		t.Error("the full queue said nothing. Remedy: a notice when queueMax is reached.")
	}
	if i := lastWith(frames, "queued (8 waiting)"); i < 0 {
		t.Error("the eighth line was not queued")
	}
	if lastWith(frames, "queued (9 waiting)") >= 0 {
		t.Error("a ninth line was queued past the limit")
	}
}

func TestEnterIsAKeyOfTheKeymapAndTheKeysMenuSaysSo(t *testing.T) {
	if !keymapTakes(term.Key{Type: term.KeyEnter}) {
		t.Fatal("the factory keymap does not name enter")
	}
}

// A queue given back must be empty: if the answer that follows ended and the lines were
// still there, they would be sent as well as being on the input (twice).
func TestLinesGivenBackAreNotSentAgainWhenALaterAnswerEnds(t *testing.T) {
	drv := newQueueDriver()
	queueLoop(t, drv, drv.evCh, append(sendLine("only once"), leave(2600*time.Millisecond)...), func(send func(fold.Event)) {
		time.Sleep(100 * time.Millisecond)
		send(working())
		time.Sleep(900 * time.Millisecond)
		send(fold.Event{Type: "chat.cancelled", Seq: 20})
		time.Sleep(300 * time.Millisecond)
		send(working())
		time.Sleep(300 * time.Millisecond)
		send(turnDone(30))
	})
	if len(drv.submitted) != 0 {
		t.Fatalf("submitted = %q. Consequence: a line the user took back is also sent by the next answer's end.\nRemedy: giveBackQueue empties msgQ.", drv.submitted)
	}
}

// /clear starts a new conversation: a line queued for the old one must not be sent into it.
func TestClearForgetsTheQueue(t *testing.T) {
	drv := newQueueDriver()
	script := append(sendLine("old topic"), scheduledEvent{50 * time.Millisecond, keyEvent('/')})
	script = append(script, typed("clear")...)
	script = append(script, enterAfter(50*time.Millisecond))
	queueLoop(t, drv, drv.evCh, append(script, leave(2600*time.Millisecond)...), func(send func(fold.Event)) {
		time.Sleep(100 * time.Millisecond)
		send(working())
		time.Sleep(1500 * time.Millisecond)
		send(turnDone(30))
	})
	if len(drv.submitted) != 0 {
		t.Fatalf("submitted = %q after /clear. Consequence: a message for the old conversation lands in the new one.\nRemedy: /clear and /resume set msgQ = nil.", drv.submitted)
	}
}

// A node holding the focus owns Enter: pressing it must press the node, not queue and not
// send the line, even while the agent works. The button here sends "pressed" with /send;
// the line typed first must still be on the input afterwards, not queued.
func TestEnterOnAFocusedButtonPressesItWhileTheAgentWorks(t *testing.T) {
	for _, busy := range []bool{false, true} {
		name := "idle"
		if busy {
			name = "working"
		}
		t.Run(name, func(t *testing.T) {
			t.Setenv(configDirEnv, t.TempDir())
			t.Setenv(historyEnv, t.TempDir())
			d, err := scene.ParseDocument([]byte(pressDocWith("cmd:/send pressed")))
			if err != nil {
				t.Fatal(err)
			}
			drv := newQueueDriver()
			script := []scheduledEvent{{300 * time.Millisecond, keyEvent('x')}}
			script = append(script, pressBtn()...)
			tty := newFakeTTY(80, 24, append(script, leave(700*time.Millisecond)...))
			if busy {
				go func() { time.Sleep(100 * time.Millisecond); drv.evCh <- working() }()
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if err := loop(ctx, tty, d, theme.SOBRIA(), drv.evCh, drv, ""); err != nil {
				t.Fatalf("loop: %v", err)
			}
			if len(drv.submitted) != 1 || drv.submitted[0] != "pressed" {
				t.Errorf("submitted = %q, want [pressed]. Consequence: with the agent %s, Enter on a focused button does not press it (the queue took the key, or the line was sent instead). Remedy: the enter binding is off while ui.focused.", drv.submitted, name)
			}
			if strings.Contains(tty.output(), "queued (") {
				t.Errorf("the line was queued while a button held the focus (agent %s)", name)
			}
		})
	}
}
