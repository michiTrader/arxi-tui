package main

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"strings"
	"testing"
	"time"
)

// askRig is a connection as seen by a request that has to ask the user: what the
// server writes lands in out, and what the client "sends" is the lines given.
func askRig(lines ...string) (*connStreams, *syncBuffer) {
	out := &syncBuffer{}
	cw := &connWriter{enc: json.NewEncoder(out)}
	cs := newConnStreams(context.Background(), cw)
	cs.src = newLineSource(bufio.NewScanner(strings.NewReader(strings.Join(lines, "\n") + "\n")))
	return cs, out
}

func approval(id string) chatApprovalNotification {
	return chatApprovalNotification{CallID: id, Name: "edit", Arg: "main.go", Summary: "Added 1 line", Diff: "    1 + x\n"}
}

func TestAskReturnsTheUsersDecision(t *testing.T) {
	for _, tc := range []struct {
		name, line string
		want       bool
	}{
		{"allow", `{"type":"chat.decision","call_id":"c1","allow":true}`, true},
		{"deny", `{"type":"chat.decision","call_id":"c1","allow":false}`, false},
		{"silent means no", `{"type":"chat.decision","call_id":"c1"}`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cs, out := askRig(tc.line)
			got, err := cs.ask(approval("c1"))
			if err != nil || got != tc.want {
				t.Fatalf("got=%v err=%v, want %v", got, err, tc.want)
			}
			var sent map[string]any
			if err := json.Unmarshal([]byte(strings.TrimSpace(out.String())), &sent); err != nil {
				t.Fatalf("what was asked is not JSON: %v: %q", err, out.String())
			}
			if sent["type"] != "chat.approval" || sent["call_id"] != "c1" || sent["diff"] != "    1 + x\n" {
				t.Errorf("question = %v", sent)
			}
			if _, hasID := sent["id"]; hasID {
				t.Error("a notification must not carry an id: a client would take it for a response")
			}
		})
	}
}

// A request that arrives while the user is deciding is not lost and not reordered:
// the loop gets it next, before anything the client sends later.
func TestAskKeepsEarlyRequestsInOrder(t *testing.T) {
	cs, _ := askRig(
		`{"id":"a","type":"schema"}`,
		`{"type":"chat.decision","call_id":"stale","allow":true}`,
		`{"id":"b","type":"schema"}`,
		`{"type":"chat.decision","call_id":"c1","allow":true}`,
		`{"id":"c","type":"schema"}`,
	)
	got, err := cs.ask(approval("c1"))
	if err != nil || !got {
		t.Fatalf("got=%v err=%v; a stale decision must not answer this question", got, err)
	}
	var ids []string
	for cs.src.scan() {
		var r protoRequest
		_ = json.Unmarshal([]byte(cs.src.cur.text), &r)
		ids = append(ids, r.ID)
	}
	if strings.Join(ids, ",") != "a,b,c" {
		t.Errorf("requests after the decision = %v, want a,b,c in order", ids)
	}
}

func TestAskRefusesWhenTheConnectionEnds(t *testing.T) {
	cs, _ := askRig() // the client sends nothing and hangs up
	got, err := cs.ask(approval("c1"))
	if got || err == nil || !strings.Contains(err.Error(), "nothing was changed") {
		t.Fatalf("got=%v err=%v; a closed connection must never count as permission", got, err)
	}
}

func TestAskWaitsForTheAnswer(t *testing.T) {
	pr, pw := io.Pipe()
	out := &syncBuffer{}
	cs := newConnStreams(context.Background(), &connWriter{enc: json.NewEncoder(out)})
	cs.src = newLineSource(bufio.NewScanner(pr))
	done := make(chan bool, 1)
	go func() {
		ok, _ := cs.ask(approval("c9"))
		done <- ok
	}()
	select {
	case <-done:
		t.Fatal("ask returned before the user answered")
	case <-time.After(100 * time.Millisecond):
	}
	io.WriteString(pw, `{"type":"chat.decision","call_id":"c9","allow":true}`+"\n")
	select {
	case ok := <-done:
		if !ok {
			t.Error("the decision was lost")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("ask did not return after the decision")
	}
}

func TestAskWithoutAReaderRefuses(t *testing.T) {
	cs := newConnStreams(context.Background(), &connWriter{enc: json.NewEncoder(&syncBuffer{})})
	if ok, err := cs.ask(approval("c1")); ok || err == nil {
		t.Errorf("ok=%v err=%v; direct handlers have nobody to ask", ok, err)
	}
	var nilStreams *connStreams
	if ok, err := nilStreams.ask(approval("c1")); ok || err == nil {
		t.Errorf("ok=%v err=%v", ok, err)
	}
}
