package main

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"strings"
	"testing"
	"time"

	hostv1 "github.com/michiTrader/arxi/host/v1"
)

// steerRig is a live connection to a core whose model is the fake: lines the test
// writes are what the client sends, and the lines read are what the core writes.
type steerRig struct {
	t     *testing.T
	f     *fakeLLM
	to    io.Writer
	from  *bufio.Reader
	close func()
	done  chan error
}

func newSteerRig(t *testing.T, script ...scripted) *steerRig {
	t.Helper()
	f := setUpFake(t)
	f.script = script
	cr, cw := io.Pipe() // client -> core
	sr, sw := io.Pipe() // core -> client
	session := newProtoSession(hostv1.Principal{ID: "p"}, nil)
	rig := &steerRig{t: t, f: f, to: cw, from: bufio.NewReader(sr), done: make(chan error, 1)}
	go func() {
		err := serveConnSessionContext(context.Background(), cr, sw, session)
		sw.Close()
		rig.done <- err
	}()
	rig.close = func() { cw.Close() }
	t.Cleanup(rig.close)
	rig.read() // the hello
	return rig
}

func (r *steerRig) send(v any) {
	r.t.Helper()
	b, _ := json.Marshal(v)
	if _, err := r.to.Write(append(b, '\n')); err != nil {
		r.t.Fatalf("write to the core: %v", err)
	}
}

func (r *steerRig) read() map[string]any {
	r.t.Helper()
	type res struct {
		m   map[string]any
		err error
	}
	c := make(chan res, 1)
	go func() {
		line, err := r.from.ReadString('\n')
		var m map[string]any
		if err == nil {
			err = json.Unmarshal([]byte(line), &m)
		}
		c <- res{m, err}
	}()
	select {
	case x := <-c:
		if x.err != nil {
			r.t.Fatalf("read from the core: %v", x.err)
		}
		return x.m
	case <-time.After(10 * time.Second):
		r.t.Fatal("the core wrote nothing for 10 s")
		return nil
	}
}

// readUntil reads until a line satisfies pred and returns it, with every line before.
func (r *steerRig) readUntil(pred func(map[string]any) bool) (map[string]any, []map[string]any) {
	r.t.Helper()
	var seen []map[string]any
	for {
		m := r.read()
		seen = append(seen, m)
		if pred(m) {
			return m, seen
		}
	}
}

func typeIs(kind string) func(map[string]any) bool {
	return func(m map[string]any) bool { return m["type"] == kind }
}

func userTexts(msgs []map[string]any) []string {
	var out []string
	for _, m := range msgs {
		if m["role"] == "user" {
			if s, ok := m["content"].(string); ok {
				out = append(out, s)
			}
		}
	}
	return out
}

func startSteeredTurn(r *steerRig) {
	r.send(map[string]any{"id": "t1", "type": "chat.send", "params": map[string]any{
		"prompt": "add a blank line above the status bar", "workdir": projectDir(r.t),
		"client_tools": lentTools,
	}})
}

// What the user says while a tool runs reaches the model at its next ask, and the
// turn it joins is the same one: the work before it is kept, not asked for again.
func TestASteerWhileAToolRunsReachesTheModelAtItsNextAsk(t *testing.T) {
	r := newSteerRig(t, scripted{name: "ui_guide", args: `{}`}, scripted{text: "done"})
	startSteeredTurn(r)
	call, before := r.readUntil(typeIs("chat.client_tool"))
	announced := false
	for _, m := range before {
		announced = announced || m["type"] == "chat.steerable"
	}
	if !announced {
		t.Error("the turn never said it takes steering.\nConsequence: a client cannot tell this core from an older one that answers chat.steer as an unknown request, which a client reading mid-turn takes for the turn's own response and aborts the turn.\nRemedy: write chat.steerable when the turn starts (withSteering).")
	}
	r.send(map[string]any{"type": "chat.steer", "text": "use two blank lines, not one"})
	r.send(map[string]any{"type": "chat.client_result", "call_id": call["call_id"], "ok": true, "text": "guide text"})
	resp, seen := r.readUntil(func(m map[string]any) bool { return m["id"] == "t1" })
	if resp["ok"] != true {
		t.Fatalf("the turn failed: %v", resp)
	}
	var steered string
	for _, m := range seen {
		if m["type"] == "chat.steered" {
			steered, _ = m["text"].(string)
		}
	}
	if steered != "use two blank lines, not one" {
		t.Errorf("chat.steered = %q.\nConsequence: the client keeps waiting to learn whether its text was taken and cannot show the user that it was.\nRemedy: write a chat.steered notification when the text joins the conversation.", steered)
	}
	if n := len(r.f.messages); n != 2 {
		t.Fatalf("the model was asked %d times, want 2.\nConsequence: a steer that restarts the turn pays for the work already done a second time.\nRemedy: add the text to the conversation and ask once more; never start the turn over.", n)
	}
	if first := userTexts(r.f.messages[0]); len(first) != 1 {
		t.Errorf("the first ask carried %d user messages, want only the prompt", len(first))
	}
	second := userTexts(r.f.messages[1])
	if len(second) != 2 || !strings.Contains(second[1], "use two blank lines, not one") {
		t.Errorf("the second ask carried %q.\nConsequence: the model carries on with the old plan and the user's correction is ignored until the next message.\nRemedy: append the steering text as a user message after the tool results.", second)
	}
}

// A steer that reaches the core after the last tool round has nothing to join: the
// turn is about to answer. It must not leak into the next message.
func TestASteerAfterTheLastRoundIsNotHandedToTheNextTurn(t *testing.T) {
	r := newSteerRig(t, scripted{text: "done"})
	startSteeredTurn(r)
	r.readUntil(func(m map[string]any) bool { return m["id"] == "t1" })
	r.send(map[string]any{"type": "chat.steer", "text": "too late"})
	r.send(map[string]any{"id": "s", "type": "schema"})
	resp, _ := r.readUntil(func(m map[string]any) bool { return m["id"] == "s" })
	if resp["ok"] != true {
		t.Fatalf("a request after a late steer was not answered: %v.\nConsequence: one stray steering line wedges the connection.\nRemedy: drop a steering line the loop reads; it was never taken.", resp)
	}
	r.send(map[string]any{"id": "t2", "type": "chat.send", "params": map[string]any{
		"prompt": "next", "workdir": projectDir(t), "client_tools": lentTools,
	}})
	r.readUntil(func(m map[string]any) bool { return m["id"] == "t2" })
	for _, m := range r.f.messages[len(r.f.messages)-1] {
		if s, _ := m["content"].(string); strings.Contains(s, "too late") {
			t.Errorf("the next turn was given %q.\nConsequence: the model acts on a correction that was meant for a turn that is over.\nRemedy: drop untaken steering lines when the turn ends.", s)
		}
	}
}

// A request written while the turn runs is answered after it, in order, even with a
// steer between: the pump must not reorder or lose what the loop reads next.
func TestRequestsWrittenMidTurnKeepTheirOrderBesideASteer(t *testing.T) {
	r := newSteerRig(t, scripted{name: "ui_guide", args: `{}`}, scripted{text: "done"})
	startSteeredTurn(r)
	call, _ := r.readUntil(typeIs("chat.client_tool"))
	r.send(map[string]any{"id": "a", "type": "schema"})
	r.send(map[string]any{"type": "chat.steer", "text": "wait"})
	r.send(map[string]any{"id": "b", "type": "schema"})
	r.send(map[string]any{"type": "chat.client_result", "call_id": call["call_id"], "ok": true, "text": "x"})
	var ids []string
	for len(ids) < 3 {
		m := r.read()
		if id, _ := m["id"].(string); id != "" {
			ids = append(ids, id)
		}
	}
	if strings.Join(ids, ",") != "t1,a,b" {
		t.Errorf("responses came in order %v, want t1,a,b.\nConsequence: a client matching answers to requests by order gets another request's result.\nRemedy: keep requests read mid-turn in lineSource.deferred, in arrival order.", ids)
	}
}

// Steering without a toolbox has no round boundary to join, so the line is read and
// ignored rather than refused: the connection goes on.
func TestSteeringOnAConnectionWithNoTurnIsHarmless(t *testing.T) {
	r := newSteerRig(t)
	r.send(map[string]any{"type": "chat.steer", "text": "hello?"})
	r.send(map[string]any{"id": "s", "type": "schema"})
	if resp, _ := r.readUntil(func(m map[string]any) bool { return m["id"] == "s" }); resp["ok"] != true {
		t.Fatalf("schema after a stray steer: %v", resp)
	}
}

func TestAnEmptySteerSteersNothing(t *testing.T) {
	r := newSteerRig(t, scripted{name: "ui_guide", args: `{}`}, scripted{text: "done"})
	startSteeredTurn(r)
	call, _ := r.readUntil(typeIs("chat.client_tool"))
	r.send(map[string]any{"type": "chat.steer", "text": "   "})
	r.send(map[string]any{"type": "chat.client_result", "call_id": call["call_id"], "ok": true, "text": "x"})
	_, seen := r.readUntil(func(m map[string]any) bool { return m["id"] == "t1" })
	for _, m := range seen {
		if m["type"] == "chat.steered" {
			t.Error("a blank steer was reported as taken.\nConsequence: the client drops its text and the user believes the turn heard something.\nRemedy: ignore a steer with no text.")
		}
	}
	if got := userTexts(r.f.messages[1]); len(got) != 1 {
		t.Errorf("the model got %q; a blank steer must not add a message", got)
	}
}

// Several steers between two asks join as one message, oldest first: one ask, one
// message, so the model reads them as the user wrote them.
func TestSeveralSteersJoinInOrderAsOneMessage(t *testing.T) {
	r := newSteerRig(t, scripted{name: "ui_guide", args: `{}`}, scripted{text: "done"})
	startSteeredTurn(r)
	call, _ := r.readUntil(typeIs("chat.client_tool"))
	r.send(map[string]any{"type": "chat.steer", "text": "first thing"})
	r.send(map[string]any{"type": "chat.steer", "text": "second thing"})
	r.send(map[string]any{"type": "chat.client_result", "call_id": call["call_id"], "ok": true, "text": "x"})
	r.readUntil(func(m map[string]any) bool { return m["id"] == "t1" })
	got := userTexts(r.f.messages[1])
	if len(got) != 2 || strings.Index(got[1], "first thing") < 0 || strings.Index(got[1], "first thing") > strings.Index(got[1], "second thing") {
		t.Errorf("the model read %q; want both, in the order written, in one message", got)
	}
}

// With a tool the core runs itself nothing waits on the client, so nothing reads the
// connection while the model thinks: only the read-ahead pump can see a steer written
// then. This is the case the pump exists for.
func TestASteerWrittenWhileTheModelThinksIsSeenWithoutAClientTool(t *testing.T) {
	r := newSteerRig(t, scripted{name: "list", args: `{"path":"."}`}, scripted{text: "done"})
	r.f.delay = 300 * time.Millisecond
	r.send(map[string]any{"id": "t1", "type": "chat.send", "params": map[string]any{
		"prompt": "look around", "workdir": projectDir(t),
	}})
	r.send(map[string]any{"type": "chat.steer", "text": "only the top folder"})
	_, seen := r.readUntil(func(m map[string]any) bool { return m["id"] == "t1" })
	var steered bool
	for _, m := range seen {
		steered = steered || m["type"] == "chat.steered"
	}
	if !steered {
		t.Fatal("a steer written during the model's ask was never taken.\nConsequence: with core tools nothing reads the connection mid-turn, so the correction sits unread until the turn is over and then is dropped.\nRemedy: start the connection's pump (withSteering) before the turn's first ask.")
	}
	if got := userTexts(r.f.messages[1]); len(got) != 2 || !strings.Contains(got[1], "only the top folder") {
		t.Errorf("the second ask carried %q, want the prompt and the steer", got)
	}
}

// Requests the pump read while the turn looked for steers are still the loop's, in the
// order they were written. Written directly against lineSource because the race that
// puts them there in a live connection is too narrow to hit on purpose.
func TestRequestsReadWhileLookingForSteersAreKeptInOrder(t *testing.T) {
	lines := []string{`{"id":"a","type":"schema"}`, `{"type":"chat.steer","text":"x"}`, `{"id":"b","type":"schema"}`}
	s := newLineSource(bufio.NewScanner(strings.NewReader(strings.Join(lines, "\n") + "\n")))
	s.startPump()
	t.Cleanup(s.stop)
	deadline := time.Now().Add(2 * time.Second)
	for len(s.feed) < len(lines) && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if got := s.takeSteers(); len(got) != 1 || !strings.Contains(got[0].text, `"x"`) {
		t.Fatalf("takeSteers = %v, want the one steer", got)
	}
	var ids []string
	for s.scan() {
		var r protoRequest
		_ = json.Unmarshal([]byte(s.cur.text), &r)
		ids = append(ids, r.ID)
	}
	if strings.Join(ids, ",") != "a,b" {
		t.Errorf("the loop got requests %v, want a,b.\nConsequence: looking for a steer would swallow the requests around it, and a client waits forever for answers that were never given.\nRemedy: keep every line takeSteers reads that is not a steer in lineSource.deferred, in order.", ids)
	}
}

// What a turn that ended on an error never took must not reach the next one.
func TestEndingTheTurnForgetsTheSteersItNeverTook(t *testing.T) {
	cs, _ := askRig()
	cs.src.steers = []sourceLine{{text: `{"type":"chat.steer","text":"old"}`}}
	cs.endSteering()
	if got := cs.takeSteerTexts(); len(got) != 0 {
		t.Errorf("the next turn would be given %q.\nConsequence: the model acts on a correction meant for a turn that failed, and the user sees an answer to something they never said to this one.\nRemedy: clear lineSource.steers when handleChatSendThinking returns.", got)
	}
}
