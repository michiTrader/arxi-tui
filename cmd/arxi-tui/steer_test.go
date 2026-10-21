package main

import (
	"context"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/michiTrader/arxi_tui/internal/driver"
	"github.com/michiTrader/arxi_tui/internal/fold"
	"github.com/michiTrader/arxi_tui/internal/term"
)

// steerCore blocks the first question until its turn is cancelled (after running the tool
// calls scripted for it), and answers every other one at once. It records what each
// question was sent with, so a test can read what the model was told.
type steerCore struct {
	mu      sync.Mutex
	prompts []string
	hist    [][]driver.ChatTurn
	started chan struct{}
	// lateDone is closed once the interrupted turn has said everything it still would.
	lateDone chan struct{}
}

func (c *steerCore) Hello() *driver.Hello { return &driver.Hello{Implemented: []string{"chat.send"}} }
func (c *steerCore) SubmitChatSend(ctx context.Context, p driver.ChatSendParams) (*driver.ChatSendResult, error) {
	c.mu.Lock()
	first := len(c.prompts) == 0
	c.prompts = append(c.prompts, p.Prompt)
	c.hist = append(c.hist, append([]driver.ChatTurn(nil), p.History...))
	c.mu.Unlock()
	if first {
		if p.OnTool != nil {
			p.OnTool(driver.ToolCall{Name: "read", Arg: "main.go", OK: true, Summary: "Read 200 lines", Output: "package main"})
		}
		close(c.started)
		<-ctx.Done()
		// A tool call that finishes after the turn was stopped: it is still reported.
		if p.OnTool != nil {
			p.OnTool(driver.ToolCall{Name: "read", Arg: "LATE-TOOL.go", OK: true, Summary: "late"})
		}
		if c.lateDone != nil {
			close(c.lateDone)
		}
		return &driver.ChatSendResult{Text: "LATE ANSWER OF THE INTERRUPTED TURN", Model: "m", Provider: "p"}, nil
	}
	return &driver.ChatSendResult{Text: "answer to " + p.Prompt, Model: "m", Provider: "p"}, nil
}
func (c *steerCore) historyOf(i int) []driver.ChatTurn {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.hist[i]
}
func (c *steerCore) asked() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.prompts)
}

// Steering stops the turn in flight without a "Cancelled" line, and the next question is
// asked knowing what the stopped turn did and that the user interrupted it.
func TestStopForSteerKeepsWhatTheTurnDidInTheHistoryAndSaysNothing(t *testing.T) {
	core := &steerCore{started: make(chan struct{}), lateDone: make(chan struct{})}
	c, out := chatOn(core)
	if c.stopForSteer() {
		t.Fatal("nothing was running, yet stopForSteer said it stopped a turn")
	}
	if err := c.send(context.Background(), "first"); err != nil {
		t.Fatal(err)
	}
	<-core.started
	if !c.stopForSteer() {
		t.Fatal("a turn was running and stopForSteer did not stop it")
	}
	if c.busyNow() {
		t.Fatal("the session is still busy after the steer.\nConsequence: the line that steers is refused as busy.\nRemedy: stopForSteer frees the session at once, like cancelTurn.")
	}
	if err := c.send(context.Background(), "no, do this instead"); err != nil {
		t.Fatal(err)
	}
	got := drain(out, "agent.turn_done", 2*time.Second)
	select {
	case <-core.lateDone:
	case <-time.After(2 * time.Second):
		t.Fatal("the interrupted turn never wound down")
	}
	// Whatever the stopped turn said late is in the channel by now.
	for more := true; more; {
		select {
		case e := <-out:
			got = append(got, e)
		default:
			more = false
		}
	}
	for _, e := range got {
		if e.Type == "chat.tool" && e.Payload["arg"] == "LATE-TOOL.go" {
			t.Error("a tool call of the stopped turn was drawn after the steer.\nConsequence: it lands among the lines of the turn that replaced it.\nRemedy: post ignores a turn whose context is cancelled.")
		}
		switch e.Type {
		case "chat.cancelled":
			t.Error("a chat.cancelled line was drawn for a steer. Consequence: the user sees a failed request they did not cause. Remedy: stopForSteer posts nothing.")
		case "llm.response":
			if txt, _ := e.Payload["text"].(string); strings.Contains(txt, "LATE ANSWER") {
				t.Errorf("the interrupted turn answered after it was stopped: %q.\nConsequence: its answer lands among the lines of the turn that replaced it.\nRemedy: post ignores a cancelled turn's context.", txt)
			}
		}
	}
	h := core.historyOf(1)
	if len(h) != 1 || h[0].Role != "user" || !strings.HasPrefix(h[0].Text, "first") {
		t.Fatalf("history of the steering question = %+v, want the interrupted request kept", h)
	}
	for _, want := range []string{"read(main.go)", whyInterrupted} {
		if !strings.Contains(h[0].Text, want) {
			t.Errorf("the interrupted request in the history lacks %q:\n%s\nConsequence: the model reads the correction as a message out of nowhere.\nRemedy: unansweredTurn keeps the tools and the reason.", want, h[0].Text)
		}
	}
}

// A compaction is busy but is not a turn: a line must not stop it.
func TestStopForSteerLeavesACompactionAlone(t *testing.T) {
	c, _ := chatOn(&steerCore{started: make(chan struct{})})
	c.mu.Lock()
	c.busy, c.compacting, c.cancel = true, true, func() { t.Error("the compaction was cancelled by a steer") }
	c.mu.Unlock()
	if c.stopForSteer() {
		t.Error("stopForSteer stopped a compaction.\nConsequence: the summary is lost and the history is left half rewritten.\nRemedy: stopForSteer does nothing while c.compacting.")
	}
}

// ---- the real loop ----------------------------------------------------------------------

// steerDriver is a testDriver that can be stopped for a steer, as serveDriver can.
type steerDriver struct {
	*testDriver
	stops int32
}

func (d *steerDriver) StopForSteer() bool { d.stops++; return true }

func newSteerDriver() *steerDriver { return &steerDriver{testDriver: newQueueDriver()} }

func altEnterAfter(d time.Duration) scheduledEvent {
	return scheduledEvent{d, term.Event{Kind: term.EventKey, Key: term.Key{Type: term.KeyEnter, Mod: term.ModAlt}}}
}

func sendLineAlt(s string) []scheduledEvent {
	return append([]scheduledEvent{{300 * time.Millisecond, keyEvent(rune(s[0]))}}, append(typed(s[1:]), altEnterAfter(20*time.Millisecond))...)
}

// Alt+Enter while the agent works stops the answer and sends the line at once; Enter
// queues, as before. The driver is asked to stop the turn before the line goes.
func TestAltEnterWhileTheAgentWorksStopsTheTurnAndSendsTheLineAtOnce(t *testing.T) {
	drv := newSteerDriver()
	frames := queueLoop(t, drv, drv.evCh, append(sendLineAlt("do this instead"), leave(900*time.Millisecond)...), func(send func(fold.Event)) {
		time.Sleep(100 * time.Millisecond)
		send(working())
	})
	if len(drv.submitted) != 1 || drv.submitted[0] != "do this instead" {
		t.Fatalf("submitted = %q, want the line sent at once.\nConsequence: Alt+Enter does not steer.\nRemedy: the alt+enter binding runs do:queue.steer while agent.working.", drv.submitted)
	}
	if drv.stops != 1 {
		t.Errorf("the turn was stopped %d times, want 1.\nConsequence: the new line is sent into a busy session and refused, or the old turn goes on.\nRemedy: queue.steer calls StopForSteer before sending.", drv.stops)
	}
	if firstWith(frames, "(queued)") >= 0 {
		t.Error("a steering line was drawn as waiting in the queue")
	}
}

// With nothing running Alt+Enter is no keymap row at all: it is a plain Enter.
func TestAltEnterWhenIdleIsAPlainEnter(t *testing.T) {
	drv := newSteerDriver()
	queueLoop(t, drv, drv.evCh, append(sendLineAlt("hello"), leave(500*time.Millisecond)...), func(send func(fold.Event)) {})
	if len(drv.submitted) != 1 || drv.submitted[0] != "hello" || drv.stops != 0 {
		t.Errorf("submitted = %q, stops = %d; want one plain send and no stop.\nConsequence: Alt+Enter does something odd when there is nothing to steer.\nRemedy: the alt+enter bindings need agent.working.", drv.submitted, drv.stops)
	}
}

// A driver that cannot be steered keeps the line and says so: nothing is lost.
func TestAltEnterOnADriverThatCannotSteerKeepsTheLine(t *testing.T) {
	drv := newQueueDriver()
	frames := queueLoop(t, drv, drv.evCh, append(sendLineAlt("keep this"), leave(600*time.Millisecond)...), func(send func(fold.Event)) {
		time.Sleep(100 * time.Millisecond)
		send(working())
	})
	if len(drv.submitted) != 0 {
		t.Errorf("submitted = %q on a driver that cannot steer.\nConsequence: the line goes into a busy session and is lost.\nRemedy: queue.steer refuses when the driver has no StopForSteer.", drv.submitted)
	}
	if firstWith(frames, "cannot be steered") < 0 {
		t.Error("nothing said the session cannot be steered. Remedy: queue.steer sets a notice.")
	}
}

// The waiting lines are not the correction: they keep waiting while the steer goes.
func TestSteeringLeavesTheWaitingLinesWaiting(t *testing.T) {
	drv := newSteerDriver()
	script := append(sendLine("waits"), sendLineAlt("steers")...)
	queueLoop(t, drv, drv.evCh, append(script, leave(2000*time.Millisecond)...), func(send func(fold.Event)) {
		time.Sleep(100 * time.Millisecond)
		send(working())
		time.Sleep(1100 * time.Millisecond)
		send(turnDone(30))
	})
	if len(drv.submitted) != 2 || drv.submitted[0] != "steers" || drv.submitted[1] != "waits" {
		t.Fatalf("submitted = %q, want [steers waits].\nConsequence: the line that waited is lost or sent before the correction.\nRemedy: queue.steer sends its own line and leaves msgQ alone.", drv.submitted)
	}
}

// With the setting on steer the roles swap: Enter steers, Alt+Enter waits.
func TestTheSettingSwapsWhatEnterAndAltEnterDo(t *testing.T) {
	drv := newSteerDriver()
	cfg := t.TempDir()
	if err := os.WriteFile(cfg+"/behaviour.json", []byte(`{"enter_while_busy":"steer"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	script := append(sendLine("enter steers"), sendLineAlt("alt waits")...)
	frames := queueLoopIn(t, cfg, drv, drv.evCh, append(script, leave(1200*time.Millisecond)...), func(send func(fold.Event)) {
		time.Sleep(100 * time.Millisecond)
		send(working())
	})
	if len(drv.submitted) != 1 || drv.submitted[0] != "enter steers" || drv.stops != 1 {
		t.Errorf("submitted = %q, stops = %d; want Enter to steer with the setting on steer.\nConsequence: enter_while_busy has no effect.\nRemedy: busy.steers comes from the behaviour layer.", drv.submitted, drv.stops)
	}
	if firstWith(frames, "┃ alt waits  (queued)") < 0 {
		t.Error("Alt+Enter did not queue the line with the setting on steer")
	}
}

// A line with a picture can steer: unlike a queued line it is sent at once, so the picture
// goes with it and is not dropped.
func TestSteeringWithAPictureSendsThePictureWithTheLine(t *testing.T) {
	p := writePic(t, "steer.png", pngBytes(t))
	drv := newSteerDriver()
	var script []scheduledEvent
	script = append(script, scheduledEvent{300 * time.Millisecond, keyEvent('/')})
	script = append(script, typed("image "+p)...)
	script = append(script, enterAfter(50*time.Millisecond))
	script = append(script, typed("look")...)
	script = append(script, altEnterAfter(50*time.Millisecond))
	queueLoop(t, drv, drv.evCh, append(script, leave(700*time.Millisecond)...), func(send func(fold.Event)) {
		time.Sleep(100 * time.Millisecond)
		send(working())
	})
	if len(drv.submitted) != 1 {
		t.Fatalf("submitted = %q, want the steering line sent once", drv.submitted)
	}
	n := len(drv.attached)
	if n == 0 || len(drv.attached[n-1]) == 0 {
		t.Errorf("the last pictures handed to the driver = %v.\nConsequence: the picture is dropped from a line that was sent, and the model never sees it.\nRemedy: queue.steer keeps the pictures set for the line (sendNow keepPictures).", drv.attached)
	}
}
