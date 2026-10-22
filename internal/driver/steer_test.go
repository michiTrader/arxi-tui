package driver

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
)

const (
	steerableLine = `{"type":"chat.steerable"}`
	doneLine      = `{"id":"chat-send","ok":true,"result":{"text":"done"}}`
)

func steersSent(sent string) []string {
	var out []string
	for _, l := range strings.Split(strings.TrimSpace(sent), "\n") {
		var m map[string]any
		if json.Unmarshal([]byte(l), &m) == nil && m["type"] == "chat.steer" {
			out = append(out, m["text"].(string))
		}
	}
	return out
}

// A core that says the turn is steerable hands the caller a way to write a steering
// line, and a chat.steered answer is reported as the text the core took.
func TestASteerableTurnLetsTheCallerWriteASteerAndHearItWasTaken(t *testing.T) {
	var sent bytes.Buffer
	d := sessionWithWriter(t, &sent, steerableLine,
		`{"type":"chat.steered","text":"use two lines"}`, doneLine)
	var took []string
	var steerErr error
	_, err := d.SubmitChatSend(context.Background(), ChatSendParams{Prompt: "p", Workdir: "/proj",
		OnSteerable: func(steer func(string) error) { steerErr = steer("use two lines") },
		OnSteered:   func(text string) { took = append(took, text) }})
	if err != nil || steerErr != nil {
		t.Fatalf("err=%v steerErr=%v", err, steerErr)
	}
	if got := steersSent(sent.String()); len(got) != 1 || got[0] != "use two lines" {
		t.Errorf("chat.steer lines written = %q.\nConsequence: the correction never reaches the core, and the user believes it was sent.\nRemedy: write {\"type\":\"chat.steer\",\"text\":...} through the call's write lock.", got)
	}
	if len(took) != 1 || took[0] != "use two lines" {
		t.Errorf("OnSteered got %q.\nConsequence: the caller cannot tell a text the core took from one it must send again, so it either sends twice or loses the text.\nRemedy: pass chat.steered's text to OnSteered.", took)
	}
}

// A core that does not know steering never says so; the caller is never given a way to
// steer, so it keeps to stopping the turn and sending a new one.
func TestACoreThatDoesNotSayItIsSteerableIsNeverSteered(t *testing.T) {
	var sent bytes.Buffer
	d := sessionWithWriter(t, &sent, `{"type":"chat.tool","call_id":"c","name":"list","arg":".","ok":true}`, doneLine)
	called := false
	_, err := d.SubmitChatSend(context.Background(), ChatSendParams{Prompt: "p", Workdir: "/proj",
		OnSteerable: func(func(string) error) { called = true }})
	if err != nil {
		t.Fatal(err)
	}
	if called {
		t.Error("OnSteerable was called without chat.steerable.\nConsequence: the caller thinks it can steer an older core, which answers the line as an unknown request; read mid-turn, that answer ends the turn.\nRemedy: call OnSteerable only on chat.steerable.")
	}
}

// The function outlives the call only as a refusal: once the call returned, the next
// request owns the connection and a stray line would be read as part of it.
func TestASteerAfterTheCallEndedWritesNothing(t *testing.T) {
	var sent bytes.Buffer
	d := sessionWithWriter(t, &sent, steerableLine, doneLine)
	var steer func(string) error
	if _, err := d.SubmitChatSend(context.Background(), ChatSendParams{Prompt: "p", Workdir: "/proj",
		OnSteerable: func(f func(string) error) { steer = f }}); err != nil {
		t.Fatal(err)
	}
	if err := steer("late"); !errors.Is(err, ErrTurnOver) {
		t.Errorf("a late steer returned %v, want ErrTurnOver.\nConsequence: the caller treats the text as sent and never sends it again.\nRemedy: refuse once the call has returned.", err)
	}
	if got := steersSent(sent.String()); len(got) != 0 {
		t.Errorf("a steer was written after the call ended: %q.\nConsequence: the next request on this connection reads the line before its own answer, and the core drops it, but a core that did not would act on it.\nRemedy: set ended under the write lock before returning.", got)
	}
}

// Steering writes from the caller's goroutine while the reading loop writes decisions
// and results; the encoder is shared, so unlocked they interleave bytes of two lines.
// Meaningful under -race.
func TestSteeringWhileDecisionsAreWrittenKeepsEveryLineWhole(t *testing.T) {
	var sent bytes.Buffer
	lines := []string{steerableLine}
	for i := 0; i < 20; i++ {
		lines = append(lines, approvalLine)
	}
	lines = append(lines, doneLine)
	d := sessionWithWriter(t, &sent, lines...)
	var wg sync.WaitGroup
	_, err := d.SubmitChatSend(context.Background(), ChatSendParams{Prompt: "p", Workdir: "/proj", Edits: "ask",
		OnApproval: func(context.Context, Approval) bool { return true },
		OnSteerable: func(steer func(string) error) {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for i := 0; i < 50; i++ {
					_ = steer("x")
				}
			}()
		}})
	if err != nil {
		t.Fatal(err)
	}
	wg.Wait()
	for _, l := range strings.Split(strings.TrimSpace(sent.String()), "\n") {
		var m map[string]any
		if json.Unmarshal([]byte(l), &m) != nil {
			t.Fatalf("a line on the wire is not JSON: %q.\nConsequence: the core reads garbage and ends the connection.\nRemedy: every write goes through the call's write lock.", l)
		}
	}
	if n := len(decisions(sent.String())); n != 20 {
		t.Errorf("%d decisions written, want 20: a steer must not swallow or split a decision", n)
	}
}
