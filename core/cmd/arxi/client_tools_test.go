package main

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

const lentTools = `[{"name":"ui_guide","description":"How the interface is built.","schema":{"type":"object","properties":{}}},` +
	`{"name":"ui_edit","description":"Change the interface.","schema":{"type":"object","properties":{"commands":{"type":"array"}}}}]`

// clientRig runs one turn with the client lending tools; call is what the client does.
func clientRig(t *testing.T, call func(clientToolCall) (clientToolResult, error), script ...scripted) (*fakeLLM, []chatToolNotification, []clientToolCall, error) {
	t.Helper()
	f := setUpFake(t)
	f.script = script
	var got []chatToolNotification
	var calls []clientToolCall
	ctx, err := withTools(context.Background(), projectDir(t), "", nil, func(n chatToolNotification) { got = append(got, n) })
	if err != nil {
		t.Fatal(err)
	}
	ctx, err = withClientTools(ctx, lentTools, func(c clientToolCall) (clientToolResult, error) {
		calls = append(calls, c)
		return call(c)
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = chatSendEffort(ctx, "add a blank line above the status bar", "", "", "", "")
	return f, got, calls, err
}

func TestClientToolsAreOfferedAndTheirCallsGoToTheClient(t *testing.T) {
	f, got, calls, err := clientRig(t, func(c clientToolCall) (clientToolResult, error) {
		return clientToolResult{OK: true, Text: "guide text", Summary: "Read the interface guide"}, nil
	}, scripted{name: "ui_guide", args: `{}`})
	if err != nil {
		t.Fatal(err)
	}
	if offered := strings.Join(f.tools[0], ","); offered != "list,read,grep,ui_guide,ui_edit" {
		t.Errorf("offered tools = %q.\nConsequence: a lent tool the model is never told about cannot be called, so the client's capability is unreachable.\nRemedy: append tb.client to definitions().", offered)
	}
	if len(calls) != 1 || calls[0].Name != "ui_guide" || calls[0].CallID != "call_ui_guide" || string(calls[0].Arguments) != `{}` {
		t.Fatalf("client was handed %+v; the call must reach the client with its id and arguments", calls)
	}
	if len(got) != 1 || !got[0].OK || got[0].Summary != "Read the interface guide" {
		t.Errorf("the user sees %+v; a client tool must be reported like any tool, with the client's summary", got)
	}
	last := f.messages[len(f.messages)-1]
	if res := last[len(last)-1]; res["role"] != "tool" || res["content"] != "guide text" {
		t.Errorf("the model was given %v; it must read exactly the text the client returned", res)
	}
}

func TestAFailedClientToolIsReadByTheModelNotFatal(t *testing.T) {
	_, got, _, err := clientRig(t, func(c clientToolCall) (clientToolResult, error) {
		return clientToolResult{OK: false, Text: "SOBRIA.json:3:5: unknown bind", Summary: "The change was refused"}, nil
	}, scripted{name: "ui_edit", args: `{"commands":[]}`})
	if err != nil {
		t.Fatalf("a refused client call ended the turn: %v.\nConsequence: the model never sees the validator's file:line and cannot repair.\nRemedy: return a tool error result, not an error.", err)
	}
	if len(got) != 1 || got[0].OK || got[0].Summary != "The change was refused" {
		t.Errorf("notification = %+v", got)
	}
}

func TestLosingTheClientEndsTheTurn(t *testing.T) {
	_, _, _, err := clientRig(t, func(c clientToolCall) (clientToolResult, error) {
		return clientToolResult{}, errors.New("connection closed")
	}, scripted{name: "ui_edit", args: `{}`})
	if err == nil {
		t.Fatal("the turn went on after the client was lost; a call that may have been half-applied must not be retried blindly")
	}
}

func TestClientToolsCannotShadowCoreTools(t *testing.T) {
	ctx, err := withTools(context.Background(), projectDir(t), "", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"read", "run", "edit", "web_fetch"} {
		raw, _ := json.Marshal([]map[string]any{{"name": name}})
		if _, err := withClientTools(ctx, string(raw), nil); err == nil {
			t.Errorf("a client tool named %q was accepted.\nConsequence: the client would silently redefine a core tool the user's mode governs.\nRemedy: refuse every core tool name, offered or not.", name)
		}
	}
	if _, err := withClientTools(context.Background(), lentTools, nil); err == nil {
		t.Error("client_tools without a workdir was accepted; they travel with the tool loop and have nowhere to run without it")
	}
	if _, err := withClientTools(ctx, `{"not":"a list"}`, nil); err == nil {
		t.Error("a malformed client_tools was accepted")
	}
}

func TestCallClientWaitsForTheMatchingResult(t *testing.T) {
	cs, out := askRig(
		`{"id":"a","type":"schema"}`,
		`{"type":"chat.client_result","call_id":"old","ok":true,"text":"stale"}`,
		`{"type":"chat.client_result","call_id":"c1","ok":true,"text":"done","summary":"Changed 1 line","diff":"    1 + x\n"}`,
	)
	res, err := cs.callClient(clientToolCall{CallID: "c1", Name: "ui_edit", Arguments: json.RawMessage(`{}`)})
	if err != nil || !res.OK || res.Text != "done" || res.Diff != "    1 + x\n" {
		t.Fatalf("res=%+v err=%v", res, err)
	}
	var sent map[string]any
	if err := json.Unmarshal([]byte(strings.TrimSpace(out.String())), &sent); err != nil || sent["type"] != "chat.client_tool" || sent["call_id"] != "c1" {
		t.Errorf("the client was sent %q", out.String())
	}
	if !cs.src.scan() || !strings.Contains(cs.src.cur.text, `"id":"a"`) {
		t.Error("a request that arrived while the client was running the tool was lost; it must be kept for the loop in order")
	}
}
