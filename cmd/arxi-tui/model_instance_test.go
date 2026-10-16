package main

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/michiTrader/arxi_tui/internal/driver"
	"github.com/michiTrader/arxi_tui/internal/fold"
	"github.com/michiTrader/arxi_tui/internal/scene"
	"github.com/michiTrader/arxi_tui/internal/term"
	"github.com/michiTrader/arxi_tui/internal/theme"
)

// The core keeps ONE default model per machine. Two arxi running side by side used to
// share it: a /model pick in one moved the other. These tests pin that the model is
// per instance — each chat turn names it — and that the core's file is only the
// starting point.

// modelDrv is a hub driver that remembers the model the loop told it to chat with.
type modelDrv struct {
	hubTestDriver
	mu    sync.Mutex
	model string
	sets  int
}

func (d *modelDrv) SetModel(ref string) {
	d.mu.Lock()
	d.model, d.sets = ref, d.sets+1
	d.mu.Unlock()
}
func (d *modelDrv) chatModel() string {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.model
}

// otherArxiCore plays "another arxi on the same machine": from its Nth read of the default
// on, the shared default is something else.
type otherArxiCore struct {
	*modelCore
	mu    sync.Mutex
	reads int
	after int
	to    string
}

func (c *otherArxiCore) SubmitModelDefault(ctx context.Context, ref string) (*driver.ModelDefaultResult, error) {
	if ref == "" {
		c.mu.Lock()
		c.reads++
		if c.reads > c.after {
			c.modelCore.mu.Lock()
			c.modelCore.def = c.to
			c.modelCore.mu.Unlock()
		}
		c.mu.Unlock()
	}
	return c.modelCore.SubmitModelDefault(ctx, ref)
}

func runModelLoop(t *testing.T, drv *modelDrv, script []scheduledEvent) string {
	t.Helper()
	doc, err := scene.ParseDocument([]byte(factorySobria))
	if err != nil {
		t.Fatal(err)
	}
	tty := newFakeTTY(100, 30, script)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := loop(ctx, tty, doc, theme.SOBRIA(), drv.evCh, drv, ""); err != nil {
		t.Fatalf("loop: %v", err)
	}
	return tty.output()
}

func newModelDrv(core hubCore) *modelDrv {
	return &modelDrv{hubTestDriver: hubTestDriver{testDriver: testDriver{evCh: make(chan fold.Event, 64)}, hub: core}}
}

// Another arxi changing the shared default must not move this one: after its own pick,
// the next read of the core (opening /model) still says the other model, and this
// instance keeps sending the one it picked.
func TestLoopAnotherInstancesPickDoesNotMoveThisOne(t *testing.T) {
	core := &otherArxiCore{modelCore: &modelCore{def: "openai/gpt-4o", models: sampleModels().models}, after: 1, to: "tokenharbor/deepseek-v4.1-pro"}
	drv := newModelDrv(core)
	script := append([]scheduledEvent{}, typed("/model ")...)
	script = append(script, scheduledEvent{300 * time.Millisecond, ctrlCharEvent('c')}, scheduledEvent{50 * time.Millisecond, ctrlCharEvent('c')})
	out := runModelLoop(t, drv, script)
	if core.current() != "tokenharbor/deepseek-v4.1-pro" {
		t.Fatalf("the test did not play the other instance: shared default is %q", core.current())
	}
	if got := drv.chatModel(); got != "openai/gpt-4o" {
		t.Errorf("this instance chats with %q after another one changed the shared default; want it to keep openai/gpt-4o", got)
	}
	if !strings.Contains(stripANSI(out), "gpt-4o") {
		t.Errorf("the status bar should keep naming gpt-4o:\n%s", lastFrame(out))
	}
}

// A pick made here wins over whatever the shared default says, and is what the next
// turns ask for.
func TestLoopAPickMadeHereIsTheModelAsked(t *testing.T) {
	core := &modelCore{def: "openai/gpt-4o", models: sampleModels().models}
	drv := newModelDrv(core)
	script := append([]scheduledEvent{}, typed("/model ")...)
	script = append(script, scheduledEvent{300 * time.Millisecond, keyEvent('x')})
	script = append(script, scheduledEvent{5 * time.Millisecond, term.Event{Kind: term.EventKey, Key: term.Key{Type: term.KeyBackspace}}})
	script = append(script, typed("deeps flash")...)
	script = append(script, scheduledEvent{50 * time.Millisecond, enterEvent()})
	script = append(script, scheduledEvent{300 * time.Millisecond, ctrlCharEvent('c')}, scheduledEvent{50 * time.Millisecond, ctrlCharEvent('c')})
	runModelLoop(t, drv, script)
	if got := drv.chatModel(); got != "tokenharbor/deepseek-v4.1-flash:free" {
		t.Errorf("after the pick this instance chats with %q", got)
	}
}

// A model removed or disabled meanwhile is not kept: it falls back to the core's default.
func TestAdoptModel(t *testing.T) {
	d := sampleModels() // default openai/gpt-4o; "openai/hidden" is disabled
	cases := []struct{ name, current, picked, want string }{
		{"first read takes the core's default", "", "", "openai/gpt-4o"},
		{"keeps its own model over the shared default", "tokenharbor/deepseek-v4.1-pro", "", "tokenharbor/deepseek-v4.1-pro"},
		{"a pick made here wins", "openai/gpt-4o", "tokenharbor/deepseek-v4.1-pro", "tokenharbor/deepseek-v4.1-pro"},
		{"a disabled model is dropped", "openai/hidden", "", "openai/gpt-4o"},
		{"a model that no longer exists is dropped", "gone/model", "", "openai/gpt-4o"},
	}
	for _, c := range cases {
		if got := adoptModel(c.current, d, c.picked); got != c.want {
			t.Errorf("%s: adoptModel(%q, _, %q) = %q, want %q", c.name, c.current, c.picked, got, c.want)
		}
	}
}

// Each turn names the model, the compaction too, and two sessions do not share it.
func TestChatTurnsNameTheModelOfTheirOwnSession(t *testing.T) {
	coreA, coreB := &seenChat{}, &seenChat{}
	outA, outB := make(chan fold.Event, 64), make(chan fold.Event, 64)
	a, b := newChatSession(coreA, outA), newChatSession(coreB, outB)
	a.setModel("openai/gpt-4o")
	b.setModel("tokenharbor/deepseek-v4.1-pro")
	sendAndWait(t, a, outA, "hola uno", "agent.turn_done")
	sendAndWait(t, b, outB, "hola dos", "agent.turn_done")
	if got := coreA.last().Model; got != "openai/gpt-4o" {
		t.Errorf("session A asked for %q", got)
	}
	if got := coreB.last().Model; got != "tokenharbor/deepseek-v4.1-pro" {
		t.Errorf("session B asked for %q", got)
	}
	a.setModel("")
	sendAndWait(t, a, outA, "otra", "agent.turn_done")
	if got := coreA.last().Model; got != "" {
		t.Errorf("an unset model must send nothing (the core decides), got %q", got)
	}
	// /clear keeps the model: it is a setting, not conversation.
	a.setModel("openai/gpt-4o")
	a.reset()
	sendAndWait(t, a, outA, "tras clear", "agent.turn_done")
	if got := coreA.last().Model; got != "openai/gpt-4o" {
		t.Errorf("/clear dropped the model: %q", got)
	}
}

// The summary request of /compact asks the model of THIS session, not the core's shared default.
func TestCompactAsksTheModelOfThisSession(t *testing.T) {
	core := &summariser{reply: "GOAL: ship v2"}
	c, out := chatWith(core, longHistory(6, 1000))
	c.setModel("openai/gpt-4o")
	if err := c.compact(context.Background(), ""); err != nil {
		t.Fatal(err)
	}
	collect(out, "agent.turn_done")
	if got := core.last().Model; got != "openai/gpt-4o" {
		t.Errorf("the compaction asked for model %q; want this session's openai/gpt-4o", got)
	}
}
