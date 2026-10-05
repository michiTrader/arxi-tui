package driver

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"
)

// The recorded responses below are the shapes core/cmd/arxi/provider_manage.go
// returns; the real-core end-to-end test in cmd/arxi-tui is what proves they match.

func TestProviderUpdateSendsOnlyWhatChanged(t *testing.T) {
	var sent bytes.Buffer
	d := sessionWithWriter(t, &sent, `{"id":"provider-update","ok":true,"result":{"name":"groq","base_url":"https://x/v1","key_stored":true}}`)
	res, err := d.SubmitProviderUpdate(context.Background(), ProviderUpdateParams{Name: "groq", BaseURL: "https://x/v1", APIKey: "k-123"})
	if err != nil {
		t.Fatalf("SubmitProviderUpdate: %v", err)
	}
	if !res.KeyStored {
		t.Fatalf("key_stored lost: %+v", res)
	}
	line := sent.String()
	if !strings.Contains(line, `"type":"provider.update"`) || !strings.Contains(line, `"base_url":"https://x/v1"`) {
		t.Fatalf("request does not carry the verb and the URL: %s", line)
	}
	if strings.Contains(line, "api_key_env") {
		t.Fatalf("an unchanged field was sent, which would overwrite the stored value: %s", line)
	}
}

func TestProviderUpdateNeedsAName(t *testing.T) {
	d := session(t)
	if _, err := d.SubmitProviderUpdate(context.Background(), ProviderUpdateParams{}); err == nil {
		t.Fatal("an unnamed update was sent")
	}
}

func TestProviderRemoveAndModelRemoveDecode(t *testing.T) {
	d := session(t,
		`{"id":"provider-remove","ok":true,"result":{"name":"groq","removed":true}}`,
		`{"id":"model-remove","ok":true,"result":{"provider":"groq","model":"llama","removed":true}}`)
	pr, err := d.SubmitProviderRemove(context.Background(), "groq")
	if err != nil || !pr.Removed {
		t.Fatalf("provider.remove: %+v, %v", pr, err)
	}
	mr, err := d.SubmitModelRemove(context.Background(), "groq/llama")
	if err != nil || mr.Model != "llama" {
		t.Fatalf("model.remove: %+v, %v", mr, err)
	}
}

func TestModelDiscoverReportsFoundAndAdded(t *testing.T) {
	d := session(t, `{"id":"model-discover","ok":true,"result":{"provider":"openrouter","found":3,"added":2,"models":["a","b","c"]}}`)
	res, err := d.SubmitModelDiscover(context.Background(), "openrouter")
	if err != nil {
		t.Fatalf("SubmitModelDiscover: %v", err)
	}
	if res.Found != 3 || res.Added != 2 || len(res.Models) != 3 {
		t.Fatalf("got %+v", res)
	}
}

func TestModelDefaultReadsWithAnEmptyRefAndSetsWithOne(t *testing.T) {
	var sent bytes.Buffer
	d := sessionWithWriter(t, &sent,
		`{"id":"model-default","ok":true,"result":{"default":""}}`,
		`{"id":"model-default","ok":true,"result":{"default":"groq/llama","provider":"groq","model":"llama"}}`)
	r, err := d.SubmitModelDefault(context.Background(), "")
	if err != nil || r.Default != "" {
		t.Fatalf("read: %+v, %v", r, err)
	}
	r, err = d.SubmitModelDefault(context.Background(), "groq/llama")
	if err != nil || r.Default != "groq/llama" || r.Provider != "groq" {
		t.Fatalf("set: %+v, %v", r, err)
	}
	lines := strings.Split(strings.TrimSpace(sent.String()), "\n")
	if len(lines) != 2 || strings.Contains(lines[0], `"model"`) || !strings.Contains(lines[1], `"model":"groq/llama"`) {
		t.Fatalf("a read must send no model and a set must send it: %v", lines)
	}
}

func TestChatSendCarriesHistoryAsAJSONString(t *testing.T) {
	var sent bytes.Buffer
	d := sessionWithWriter(t, &sent, `{"id":"chat-send","ok":true,"result":{"text":"hi","model":"llama","provider":"groq","input_tokens":5,"output_tokens":2}}`)
	res, err := d.SubmitChatSend(context.Background(), ChatSendParams{
		Prompt: "hello", History: []ChatTurn{{Role: "user", Text: "a"}, {Role: "assistant", Text: "b"}},
	})
	if err != nil {
		t.Fatalf("SubmitChatSend: %v", err)
	}
	if res.Text != "hi" || res.InputTokens != 5 || res.Provider != "groq" {
		t.Fatalf("got %+v", res)
	}
	var req struct {
		Params map[string]any `json:"params"`
	}
	if err := json.Unmarshal(bytes.TrimSpace(sent.Bytes()), &req); err != nil {
		t.Fatalf("request is not JSON: %v: %s", err, sent.String())
	}
	h, ok := req.Params["history"].(string)
	if !ok {
		t.Fatalf("history must be a string, got %T", req.Params["history"])
	}
	var turns []ChatTurn
	if err := json.Unmarshal([]byte(h), &turns); err != nil || len(turns) != 2 {
		t.Fatalf("history does not round-trip: %v %v", turns, err)
	}
	if _, has := req.Params["model"]; has {
		t.Fatal("no model was chosen, so none may be sent: the core picks the default")
	}
}

func TestChatSendRefusalKeepsTheCoresWords(t *testing.T) {
	d := session(t, `{"id":"chat-send","ok":false,"error":{"code":"bad_params","message":"no provider is set up yet: add one with /provider"}}`)
	_, err := d.SubmitChatSend(context.Background(), ChatSendParams{Prompt: "hello"})
	ref, ok := err.(*Refusal)
	if !ok {
		t.Fatalf("got %T, want *Refusal", err)
	}
	if !strings.Contains(ref.Message, "no provider is set up yet") {
		t.Fatalf("the core's message was lost: %q", ref.Message)
	}
}

func TestChatSendRefusesAnEmptyPromptLocally(t *testing.T) {
	d := session(t)
	if _, err := d.SubmitChatSend(context.Background(), ChatSendParams{}); err == nil {
		t.Fatal("an empty prompt was sent")
	}
}

func TestChatSendCarriesTheThinkingLevelOnlyWhenSet(t *testing.T) {
	for _, tc := range []struct {
		effort string
		want   bool
	}{{"", false}, {"high", true}} {
		var sent bytes.Buffer
		d := sessionWithWriter(t, &sent, `{"id":"chat-send","ok":true,"result":{"text":"hi","model":"m","provider":"p","input_tokens":1,"output_tokens":1}}`)
		if _, err := d.SubmitChatSend(context.Background(), ChatSendParams{Prompt: "hello", Effort: tc.effort}); err != nil {
			t.Fatalf("SubmitChatSend: %v", err)
		}
		var req struct {
			Params map[string]any `json:"params"`
		}
		if err := json.Unmarshal(bytes.TrimSpace(sent.Bytes()), &req); err != nil {
			t.Fatalf("request is not JSON: %v", err)
		}
		got, has := req.Params["effort"]
		if has != tc.want || (has && got != tc.effort) {
			t.Errorf("effort %q: params[effort] = %v (present %v)", tc.effort, got, has)
		}
	}
}

func TestChatSendNeverAsksToStreamUnlessWatched(t *testing.T) {
	var sent bytes.Buffer
	d := sessionWithWriter(t, &sent, `{"id":"chat-send","ok":true,"result":{"text":"hi"}}`)
	if _, err := d.SubmitChatSend(context.Background(), ChatSendParams{Prompt: "hello"}); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(sent.String(), "stream_thinking") {
		t.Fatalf("an unwatched turn asked for a stream: %s", sent.String())
	}
}

func TestChatSendHandsThinkingNotificationsToTheWatcher(t *testing.T) {
	var sent bytes.Buffer
	d := sessionWithWriter(t, &sent,
		`{"type":"chat.thinking","text":"Let me "}`,
		`{"type":"something.else","text":"ignored"}`,
		`{"type":"chat.thinking","text":"think."}`,
		`{"id":"chat-send","ok":true,"result":{"text":"done","model":"m","provider":"p"}}`)
	var got []string
	res, err := d.SubmitChatSend(context.Background(), ChatSendParams{Prompt: "hello", OnThinking: func(f string) { got = append(got, f) }})
	if err != nil {
		t.Fatal(err)
	}
	if res.Text != "done" || strings.Join(got, "|") != "Let me |think." {
		t.Fatalf("text=%q thinking=%q", res.Text, got)
	}
	if !strings.Contains(sent.String(), `"stream_thinking":true`) {
		t.Fatalf("the request did not ask for the stream: %s", sent.String())
	}
}

func TestChatSendFallsBackAgainstACoreThatDoesNotStream(t *testing.T) {
	var sent bytes.Buffer
	d := sessionWithWriter(t, &sent,
		`{"id":"chat-send","ok":false,"error":{"code":"bad_params","message":"unknown parameter stream_thinking for chat.send"}}`,
		`{"id":"chat-send","ok":true,"result":{"text":"plain"}}`)
	called := false
	res, err := d.SubmitChatSend(context.Background(), ChatSendParams{Prompt: "hello", OnThinking: func(string) { called = true }})
	if err != nil || res.Text != "plain" || called {
		t.Fatalf("res=%+v err=%v called=%v", res, err, called)
	}
	lines := strings.Split(strings.TrimSpace(sent.String()), "\n")
	if len(lines) != 2 || strings.Contains(lines[1], "stream_thinking") {
		t.Fatalf("the retry must be the plain request: %v", lines)
	}
}

func TestChatSendWatchedRefusalKeepsTheCoresWords(t *testing.T) {
	d := session(t, `{"id":"chat-send","ok":false,"error":{"code":"failed","message":"the provider is down"}}`)
	_, err := d.SubmitChatSend(context.Background(), ChatSendParams{Prompt: "hello", OnThinking: func(string) {}})
	ref, ok := err.(*Refusal)
	if !ok || !strings.Contains(ref.Message, "the provider is down") {
		t.Fatalf("err = %#v", err)
	}
}
