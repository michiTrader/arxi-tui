package driver

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
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

func TestChatSendRelaysToolCallsInOrderAndAsksForTheFolder(t *testing.T) {
	var sent bytes.Buffer
	d := sessionWithWriter(t, &sent,
		`{"type":"chat.tool","call_id":"c1","name":"list","arg":".","ok":true,"summary":"Listed 2 entries","output":"a\nb"}`,
		`{"type":"chat.tool","call_id":"c2","name":"read","arg":".env","ok":false,"summary":"looks like it holds secrets"}`,
		`{"id":"chat-send","ok":true,"result":{"text":"done","model":"m","provider":"p"}}`)
	var got []ToolCall
	res, err := d.SubmitChatSend(context.Background(), ChatSendParams{Prompt: "look", Workdir: "/proj", OnTool: func(c ToolCall) { got = append(got, c) }})
	if err != nil || res.Text != "done" {
		t.Fatalf("res=%+v err=%v", res, err)
	}
	if len(got) != 2 || got[0].Name != "list" || got[0].ID != "c1" || !got[0].OK || got[0].Output != "a\nb" ||
		got[1].Name != "read" || got[1].OK || got[1].Summary != "looks like it holds secrets" {
		t.Fatalf("tool calls = %+v", got)
	}
	if !strings.Contains(sent.String(), `"workdir":"/proj"`) || strings.Contains(sent.String(), "stream_thinking") {
		t.Fatalf("request = %s", sent.String())
	}
}

func TestChatSendWithToolsAndThinkingAsksForBoth(t *testing.T) {
	var sent bytes.Buffer
	d := sessionWithWriter(t, &sent,
		`{"type":"chat.thinking","text":"hm"}`,
		`{"type":"chat.tool","call_id":"c1","name":"grep","arg":"x","ok":true,"summary":"No matches"}`,
		`{"id":"chat-send","ok":true,"result":{"text":"done"}}`)
	var think []string
	var tools int
	_, err := d.SubmitChatSend(context.Background(), ChatSendParams{Prompt: "p", Workdir: "/proj",
		OnThinking: func(f string) { think = append(think, f) }, OnTool: func(ToolCall) { tools++ }})
	if err != nil || len(think) != 1 || tools != 1 {
		t.Fatalf("err=%v think=%v tools=%d", err, think, tools)
	}
	if !strings.Contains(sent.String(), `"stream_thinking":true`) || !strings.Contains(sent.String(), `"workdir":"/proj"`) {
		t.Fatalf("request = %s", sent.String())
	}
}

func TestChatSendAgainstACoreWithoutToolsSaysSo(t *testing.T) {
	d := session(t, `{"id":"chat-send","ok":false,"error":{"code":"bad_params","message":"unknown parameter workdir for chat.send"}}`)
	_, err := d.SubmitChatSend(context.Background(), ChatSendParams{Prompt: "p", Workdir: "/proj"})
	if !errors.Is(err, ErrToolsUnsupported) {
		t.Fatalf("err = %v; want ErrToolsUnsupported", err)
	}
}

func TestChatSendWithToolsFallsBackFromStreamingKeepingTheFolder(t *testing.T) {
	var sent bytes.Buffer
	d := sessionWithWriter(t, &sent,
		`{"id":"chat-send","ok":false,"error":{"code":"bad_params","message":"unknown parameter stream_thinking for chat.send"}}`,
		`{"type":"chat.tool","call_id":"c1","name":"list","arg":".","ok":true,"summary":"Listed 1 entry"}`,
		`{"id":"chat-send","ok":true,"result":{"text":"plain"}}`)
	var tools int
	res, err := d.SubmitChatSend(context.Background(), ChatSendParams{Prompt: "p", Workdir: "/proj",
		OnThinking: func(string) {}, OnTool: func(ToolCall) { tools++ }})
	if err != nil || res.Text != "plain" || tools != 1 {
		t.Fatalf("res=%+v err=%v tools=%d", res, err, tools)
	}
	lines := strings.Split(strings.TrimSpace(sent.String()), "\n")
	if len(lines) != 2 || strings.Contains(lines[1], "stream_thinking") || !strings.Contains(lines[1], `"workdir":"/proj"`) {
		t.Fatalf("the retry must keep the folder and drop the stream: %v", lines)
	}
}

const approvalLine = `{"type":"chat.approval","call_id":"c9","name":"edit","arg":"main.go","summary":"Added 1 line","diff":"- a\n+ b"}`

func decisions(sent string) []map[string]any {
	var out []map[string]any
	for _, l := range strings.Split(strings.TrimSpace(sent), "\n") {
		var m map[string]any
		if json.Unmarshal([]byte(l), &m) == nil && m["type"] == "chat.decision" {
			out = append(out, m)
		}
	}
	return out
}

func TestChatSendPutsEachChangeToTheUserAndRelaysTheDecision(t *testing.T) {
	for _, allow := range []bool{true, false} {
		var sent bytes.Buffer
		d := sessionWithWriter(t, &sent, approvalLine,
			`{"type":"chat.tool","call_id":"c9","name":"edit","arg":"main.go","ok":true,"summary":"Added 1 line","diff":"- a\n+ b"}`,
			`{"id":"chat-send","ok":true,"result":{"text":"done"}}`)
		var asked []Approval
		var tools []ToolCall
		_, err := d.SubmitChatSend(context.Background(), ChatSendParams{Prompt: "p", Workdir: "/proj", Edits: "ask",
			OnTool: func(c ToolCall) { tools = append(tools, c) },
			OnApproval: func(_ context.Context, a Approval) bool {
				asked = append(asked, a)
				return allow
			}})
		if err != nil {
			t.Fatal(err)
		}
		if len(asked) != 1 || asked[0].CallID != "c9" || asked[0].Name != "edit" || asked[0].Arg != "main.go" || asked[0].Diff != "- a\n+ b" {
			t.Fatalf("asked = %+v", asked)
		}
		dec := decisions(sent.String())
		if len(dec) != 1 || dec[0]["call_id"] != "c9" || dec[0]["allow"] != allow {
			t.Fatalf("decisions = %v (allow=%v)", dec, allow)
		}
		if len(tools) != 1 || tools[0].Diff != "- a\n+ b" {
			t.Errorf("the diff of the applied change must reach OnTool: %+v", tools)
		}
		if !strings.Contains(sent.String(), `"edits":"ask"`) {
			t.Errorf("request = %s", sent.String())
		}
	}
}

func TestChatSendDeclinesChangesWhenNobodyCanDecide(t *testing.T) {
	var sent bytes.Buffer
	d := sessionWithWriter(t, &sent, approvalLine, `{"id":"chat-send","ok":true,"result":{"text":"done"}}`)
	if _, err := d.SubmitChatSend(context.Background(), ChatSendParams{Prompt: "p", Workdir: "/proj", Edits: "ask"}); err != nil {
		t.Fatal(err)
	}
	if dec := decisions(sent.String()); len(dec) != 1 || dec[0]["allow"] != false {
		t.Fatalf("a change with no one to ask must be declined: %v", dec)
	}
}

func TestChatSendAgainstACoreWithoutEditsSaysSo(t *testing.T) {
	d := session(t, `{"id":"chat-send","ok":false,"error":{"code":"bad_params","message":"unknown parameter edits for chat.send"}}`)
	_, err := d.SubmitChatSend(context.Background(), ChatSendParams{Prompt: "p", Workdir: "/proj", Edits: "ask"})
	if !errors.Is(err, ErrEditsUnsupported) {
		t.Fatalf("err = %v; want ErrEditsUnsupported", err)
	}
}

func TestChatSendNeverSendsEditsWithoutAFolder(t *testing.T) {
	var sent bytes.Buffer
	d := sessionWithWriter(t, &sent, `{"id":"chat-send","ok":true,"result":{"text":"hi"}}`)
	if _, err := d.SubmitChatSend(context.Background(), ChatSendParams{Prompt: "p", Edits: "allow"}); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(sent.String(), "edits") {
		t.Fatalf("request = %s", sent.String())
	}
}

func TestChatSendSendsRunsOnlyWithAFolder(t *testing.T) {
	var sent bytes.Buffer
	d := sessionWithWriter(t, &sent, `{"id":"chat-send","ok":true,"result":{"text":"hi"}}`)
	if _, err := d.SubmitChatSend(context.Background(), ChatSendParams{Prompt: "p", Runs: "allow"}); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(sent.String(), "runs") {
		t.Fatalf("a request without a folder must not carry runs: %s", sent.String())
	}

	sent.Reset()
	d = sessionWithWriter(t, &sent, `{"id":"chat-send","ok":true,"result":{"text":"hi"}}`)
	if _, err := d.SubmitChatSend(context.Background(), ChatSendParams{Prompt: "p", Workdir: "/proj", Runs: "ask"}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(sent.String(), `"runs":"ask"`) {
		t.Fatalf("request = %s", sent.String())
	}
}

func TestChatSendPutsACommandToTheUserLikeAChange(t *testing.T) {
	var sent bytes.Buffer
	d := sessionWithWriter(t, &sent,
		`{"type":"chat.approval","call_id":"r1","name":"run","arg":"go test ./...","summary":"in /proj"}`,
		`{"id":"chat-send","ok":true,"result":{"text":"done"}}`)
	var asked []Approval
	_, err := d.SubmitChatSend(context.Background(), ChatSendParams{Prompt: "p", Workdir: "/proj", Runs: "ask",
		OnApproval: func(_ context.Context, a Approval) bool { asked = append(asked, a); return true }})
	if err != nil {
		t.Fatal(err)
	}
	if len(asked) != 1 || asked[0].Name != "run" || asked[0].Arg != "go test ./..." || asked[0].Diff != "" {
		t.Fatalf("asked = %+v", asked)
	}
	if dec := decisions(sent.String()); len(dec) != 1 || dec[0]["call_id"] != "r1" || dec[0]["allow"] != true {
		t.Fatalf("decisions = %v", dec)
	}
}

func TestChatSendAgainstACoreWithoutRunsSaysSo(t *testing.T) {
	// The message a core gives when it knows edits but not runs: the list of what it
	// does take names "edits" too, and must not be mistaken for the refusal.
	d := session(t, `{"id":"chat-send","ok":false,"error":{"code":"bad_params","message":"chat.send does not take runs. It takes: edits, effort, history, model, prompt, stream_thinking, system, workdir. Unknown parameters are refused"}}`)
	_, err := d.SubmitChatSend(context.Background(), ChatSendParams{Prompt: "p", Workdir: "/proj", Edits: "ask", Runs: "ask"})
	if !errors.Is(err, ErrRunsUnsupported) {
		t.Fatalf("err = %v; want ErrRunsUnsupported", err)
	}
	if errors.Is(err, ErrEditsUnsupported) {
		t.Fatal("a core that takes edits must not be reported as lacking them")
	}
}

func TestChatSendAgainstACoreWithoutEditsNorRunsReportsEdits(t *testing.T) {
	d := session(t, `{"id":"chat-send","ok":false,"error":{"code":"bad_params","message":"chat.send does not take edits, runs. It takes: effort, history, model, prompt, stream_thinking, system, workdir."}}`)
	_, err := d.SubmitChatSend(context.Background(), ChatSendParams{Prompt: "p", Workdir: "/proj", Edits: "ask", Runs: "ask"})
	if !errors.Is(err, ErrEditsUnsupported) {
		t.Fatalf("err = %v; want ErrEditsUnsupported", err)
	}
}

func TestChatSendSendsWebOnlyWithAFolder(t *testing.T) {
	var sent bytes.Buffer
	d := sessionWithWriter(t, &sent, `{"id":"chat-send","ok":true,"result":{"text":"hi"}}`)
	if _, err := d.SubmitChatSend(context.Background(), ChatSendParams{Prompt: "p", Web: "allow"}); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(sent.String(), "web") {
		t.Fatalf("a request without a folder must not carry web: %s", sent.String())
	}

	sent.Reset()
	d = sessionWithWriter(t, &sent, `{"id":"chat-send","ok":true,"result":{"text":"hi"}}`)
	if _, err := d.SubmitChatSend(context.Background(), ChatSendParams{Prompt: "p", Workdir: "/proj", Web: "ask"}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(sent.String(), `"web":"ask"`) {
		t.Fatalf("request = %s", sent.String())
	}
}

func TestChatSendAgainstACoreWithoutWebSaysSo(t *testing.T) {
	// A core that takes edits and runs but not web: the list of what it does take names
	// them, and must not be mistaken for the refusal.
	d := session(t, `{"id":"chat-send","ok":false,"error":{"code":"bad_params","message":"chat.send does not take web. It takes: edits, effort, history, model, prompt, runs, stream_thinking, system, workdir. Unknown parameters are refused"}}`)
	_, err := d.SubmitChatSend(context.Background(), ChatSendParams{Prompt: "p", Workdir: "/proj", Edits: "ask", Runs: "ask", Web: "ask"})
	if !errors.Is(err, ErrWebUnsupported) {
		t.Fatalf("err = %v; want ErrWebUnsupported", err)
	}
	if errors.Is(err, ErrEditsUnsupported) || errors.Is(err, ErrRunsUnsupported) {
		t.Fatal("a core that takes edits and runs must not be reported as lacking them")
	}
}

func TestChatSendPutsAPageToTheUserLikeACommand(t *testing.T) {
	var sent bytes.Buffer
	d := sessionWithWriter(t, &sent,
		`{"type":"chat.approval","call_id":"w1","name":"web_fetch","arg":"https://example.com/docs","summary":"Read example.com"}`,
		`{"id":"chat-send","ok":true,"result":{"text":"done"}}`)
	var asked []Approval
	_, err := d.SubmitChatSend(context.Background(), ChatSendParams{Prompt: "p", Workdir: "/proj", Web: "ask",
		OnApproval: func(_ context.Context, a Approval) bool { asked = append(asked, a); return false }})
	if err != nil {
		t.Fatal(err)
	}
	if len(asked) != 1 || asked[0].Name != "web_fetch" || asked[0].Arg != "https://example.com/docs" {
		t.Fatalf("asked = %+v", asked)
	}
	if dec := decisions(sent.String()); len(dec) != 1 || dec[0]["call_id"] != "w1" || dec[0]["allow"] != false {
		t.Fatalf("decisions = %v", dec)
	}
}

func TestBlueprintValidateDecodesTheTeamAndSendsThePath(t *testing.T) {
	var sent bytes.Buffer
	d := sessionWithWriter(t, &sent, `{"id":"blueprint-validate","ok":true,"result":{"name":"duo","sha":"abc","workspace":"worktree","workspace_reason":"backend writes",`+
		`"stages":[{"name":"build","advance_when":"all","on_timeout":"escalate","timeout_ms":60000}],`+
		`"members":[{"name":"backend","role":"implementer","model":"m","tools":["read"],"stages":["build"]},{"name":"sec","advisory":true}],`+
		`"watchers":[{"agent":"sec","pattern":"run.quiescent","action":"notify"}]}}`)
	info, err := d.SubmitBlueprintValidate(context.Background(), "/p/agents/duo.yaml")
	if err != nil {
		t.Fatalf("SubmitBlueprintValidate: %v", err)
	}
	if info.Name != "duo" || len(info.Stages) != 1 || info.Stages[0].TimeoutMs != 60000 {
		t.Fatalf("stages lost: %+v", info)
	}
	if len(info.Members) != 2 || info.Members[0].Role != "implementer" || info.Members[0].Stages[0] != "build" || !info.Members[1].Advisory {
		t.Fatalf("members lost: %+v", info.Members)
	}
	if len(info.Watchers) != 1 || info.Watchers[0].Action != "notify" {
		t.Fatalf("watchers lost: %+v", info.Watchers)
	}
	if line := sent.String(); !strings.Contains(line, `"type":"blueprint.validate"`) || !strings.Contains(line, `"path":"/p/agents/duo.yaml"`) {
		t.Fatalf("request = %s", line)
	}
	if _, err := d.SubmitBlueprintValidate(context.Background(), ""); err == nil {
		t.Fatal("an empty path was sent")
	}
}

func TestAgentCreateSendsOnlyWhatWasFilledAndDecodes(t *testing.T) {
	var sent bytes.Buffer
	d := sessionWithWriter(t, &sent,
		`{"id":"agent-create","ok":true,"result":{"name":"backend","path":"agents/backend.yaml","tools":["read","write"],"role_note":"role \"x\" is not defined"}}`)
	r, err := d.SubmitAgentCreate(context.Background(), AgentCreateParams{Name: "backend", Tools: []string{"read", "write"}})
	if err != nil || r.Path != "agents/backend.yaml" || r.RoleNote == "" {
		t.Fatalf("got %+v, %v", r, err)
	}
	line := sent.String()
	if !strings.Contains(line, `"type":"agent.create"`) || !strings.Contains(line, `"tools":"read,write"`) {
		t.Fatalf("request = %s", line)
	}
	for _, unwanted := range []string{`"model"`, `"role"`, `"advisory"`} {
		if strings.Contains(line, unwanted) {
			t.Errorf("an empty field was sent (%s), which would override the core's defaults: %s", unwanted, line)
		}
	}
	if _, err := d.SubmitAgentCreate(context.Background(), AgentCreateParams{Name: "  "}); err == nil {
		t.Fatal("an unnamed agent was sent")
	}
}

func TestBlueprintCreateSendsMembersAndStagesAsLists(t *testing.T) {
	var sent bytes.Buffer
	d := sessionWithWriter(t, &sent,
		`{"id":"blueprint-create","ok":true,"result":{"name":"duo","path":"agents/duo.yaml","members":["a","b"],"stages":["work"]}}`)
	r, err := d.SubmitBlueprintCreate(context.Background(), BlueprintCreateParams{Name: "duo", Members: []string{"a", "b"}})
	if err != nil || len(r.Stages) != 1 {
		t.Fatalf("got %+v, %v", r, err)
	}
	if line := sent.String(); !strings.Contains(line, `"members":"a,b"`) || strings.Contains(line, `"stages"`) {
		t.Fatalf("request = %s", line)
	}
	if _, err := d.SubmitBlueprintCreate(context.Background(), BlueprintCreateParams{Name: "x"}); err == nil {
		t.Fatal("a team with no members was sent")
	}
}

func TestAgentListDecodesMembersAndAKeptError(t *testing.T) {
	d := session(t, `{"id":"agent-list","ok":true,"result":{"agents":[`+
		`{"name":"bad","path":"agents/bad.yaml","members":[],"error":"nope"},`+
		`{"name":"duo","path":"agents/duo.yaml","sha":"s","members":[{"name":"a","role":"r","tools":["read"]},{"name":"b"}],"stages":["x","y"]}]}}`)
	r, err := d.SubmitAgentList(context.Background())
	if err != nil || len(r.Agents) != 2 {
		t.Fatalf("got %+v, %v", r, err)
	}
	if r.Agents[0].Error != "nope" || len(r.Agents[1].Members) != 2 || r.Agents[1].Members[0].Role != "r" || len(r.Agents[1].Stages) != 2 {
		t.Fatalf("decoded = %+v", r.Agents)
	}
}

func TestTriggerListDecodesRecordAndNext(t *testing.T) {
	d := session(t, `{"id":"trigger-list","ok":true,"result":{"triggers":[`+
		`{"record":{"name":"nightly","on":"every:30m","then":"run start duo -- x","budget":2,"budget_period":"day","status":"active","last_status":"ok","last_fired_at":"2026-10-07T10:00:00Z"},"next":"2026-10-07T10:30:00Z"},`+
		`{"record":{"name":"old","status":"paused"},"next_absent":"paused","missed":3}]}}`)
	r, err := d.SubmitTriggerList(context.Background())
	if err != nil || len(r.Triggers) != 2 {
		t.Fatalf("got %+v, %v", r, err)
	}
	a, b := r.Triggers[0], r.Triggers[1]
	if a.Record.Name != "nightly" || a.Record.Budget != 2 || a.Record.BudgetPeriod != "day" || a.Next == "" || a.Record.LastStatus != "ok" {
		t.Fatalf("decoded = %+v", a)
	}
	if b.NextAbsent != "paused" || b.Missed != 3 || b.Record.Status != "paused" {
		t.Fatalf("decoded = %+v", b)
	}
}

func TestTriggerCreateSendsEveryFieldAndRefusesWhatTheCoreWould(t *testing.T) {
	var sent bytes.Buffer
	d := sessionWithWriter(t, &sent,
		`{"id":"trigger-create","ok":true,"result":{"record":{"name":"n","status":"active"},"next":"2026-10-07T10:30:00Z"}}`)
	r, err := d.SubmitTriggerCreate(context.Background(), TriggerCreateParams{Name: "n", On: "every:1h", Then: "run start duo -- x", Budget: 1.5, BudgetPeriod: "week"})
	if err != nil || r.Record.Name != "n" {
		t.Fatalf("got %+v, %v", r, err)
	}
	line := sent.String()
	for _, want := range []string{`"type":"trigger.create"`, `"on":"every:1h"`, `"then":"run start duo -- x"`, `"budget":1.5`, `"budget_period":"week"`} {
		if !strings.Contains(line, want) {
			t.Errorf("request lacks %s: %s", want, line)
		}
	}
	if _, err := d.SubmitTriggerCreate(context.Background(), TriggerCreateParams{Name: " ", Budget: 1}); err == nil {
		t.Fatal("an unnamed trigger was sent")
	}
	if _, err := d.SubmitTriggerCreate(context.Background(), TriggerCreateParams{Name: "x"}); err == nil {
		t.Fatal("a trigger with no spend ceiling was sent")
	}
}

func TestTriggerPauseSendsTheName(t *testing.T) {
	var sent bytes.Buffer
	d := sessionWithWriter(t, &sent,
		`{"id":"trigger-pause","ok":true,"result":{"record":{"name":"n","status":"paused"},"next_absent":"paused"}}`)
	r, err := d.SubmitTriggerPause(context.Background(), "n")
	if err != nil || r.Record.Status != "paused" {
		t.Fatalf("got %+v, %v", r, err)
	}
	if line := sent.String(); !strings.Contains(line, `"type":"trigger.pause"`) || !strings.Contains(line, `"name":"n"`) {
		t.Fatalf("request = %s", line)
	}
	if _, err := d.SubmitTriggerPause(context.Background(), ""); err == nil {
		t.Fatal("an empty name was sent")
	}
}

func TestTriggerResumeSendsTheName(t *testing.T) {
	var sent bytes.Buffer
	d := sessionWithWriter(t, &sent,
		`{"id":"trigger-resume","ok":true,"result":{"record":{"name":"n","status":"active"},"next":"2026-01-01T00:00:00Z"}}`)
	r, err := d.SubmitTriggerResume(context.Background(), "n")
	if err != nil || r.Record.Status != "active" || r.Next == "" {
		t.Fatalf("got %+v, %v", r, err)
	}
	if line := sent.String(); !strings.Contains(line, `"type":"trigger.resume"`) || !strings.Contains(line, `"name":"n"`) {
		t.Fatalf("request = %s", line)
	}
	if _, err := d.SubmitTriggerResume(context.Background(), " "); err == nil {
		t.Fatal("an empty name was sent")
	}
}

func TestBlueprintStageSendsOnlyWhatChangesAndAZeroTimeout(t *testing.T) {
	var sent bytes.Buffer
	d := sessionWithWriter(t, &sent,
		`{"id":"blueprint-stage","ok":true,"result":{"name":"duo","stages":[{"name":"review","advance_when":"any","on_timeout":"escalate"}]}}`)
	zero := int64(0)
	r, err := d.SubmitBlueprintStage(context.Background(), BlueprintStageParams{Name: "duo", Stage: "review", AdvanceWhen: "any", TimeoutMs: &zero})
	if err != nil || len(r.Stages) != 1 || r.Stages[0].AdvanceWhen != "any" {
		t.Fatalf("got %+v, %v", r, err)
	}
	line := sent.String()
	for _, want := range []string{`"type":"blueprint.stage"`, `"stage":"review"`, `"advance_when":"any"`, `"timeout_ms":0`} {
		if !strings.Contains(line, want) {
			t.Errorf("request %s lacks %s", line, want)
		}
	}
	if strings.Contains(line, "on_timeout") {
		t.Errorf("an unchanged field was sent: %s", line)
	}
	if _, err := d.SubmitBlueprintStage(context.Background(), BlueprintStageParams{Name: "duo"}); err == nil {
		t.Fatal("a request without a stage was sent")
	}
}

func TestBlueprintMemberSendsOnlyWhatChangesAndEmptyMeansRemove(t *testing.T) {
	var sent bytes.Buffer
	d := sessionWithWriter(t, &sent,
		`{"id":"blueprint-member","ok":true,"result":{"name":"duo","members":[{"name":"dev","model":"p/m","advisory":true}]}}`)
	model, none, yes := "p/m", []string{}, true
	r, err := d.SubmitBlueprintMember(context.Background(), BlueprintMemberParams{
		Name: "duo", Member: "dev", Model: &model, Tools: &none, Advisory: &yes})
	if err != nil || len(r.Members) != 1 || r.Members[0].Model != "p/m" || !r.Members[0].Advisory {
		t.Fatalf("got %+v, %v", r, err)
	}
	line := sent.String()
	for _, want := range []string{`"type":"blueprint.member"`, `"member":"dev"`, `"model":"p/m"`, `"tools":""`, `"advisory":true`} {
		if !strings.Contains(line, want) {
			t.Errorf("request %s lacks %s", line, want)
		}
	}
	if strings.Contains(line, `"role"`) {
		t.Errorf("an unchanged field was sent: %s", line)
	}
	if _, err := d.SubmitBlueprintMember(context.Background(), BlueprintMemberParams{Name: "duo"}); err == nil {
		t.Fatal("a request without a member was sent")
	}
}

func TestBlueprintWatchSendsTheRuleAndOnlyWhatIsSet(t *testing.T) {
	var sent bytes.Buffer
	d := sessionWithWriter(t, &sent,
		`{"id":"blueprint-watch","ok":true,"result":{"name":"duo","watchers":[{"agent":"b","pattern":"stage.*","action":"run_tool","tool":"read"}]}}`)
	r, err := d.SubmitBlueprintWatch(context.Background(), BlueprintWatchParams{
		Name: "duo", Agent: "b", Pattern: "stage.*", Action: "run_tool", Tool: "read"})
	if err != nil || len(r.Watchers) != 1 || r.Watchers[0].Tool != "read" {
		t.Fatalf("got %+v, %v", r, err)
	}
	line := sent.String()
	for _, want := range []string{`"type":"blueprint.watch"`, `"agent":"b"`, `"pattern":"stage.*"`, `"action":"run_tool"`, `"tool":"read"`} {
		if !strings.Contains(line, want) {
			t.Errorf("request %s lacks %s", line, want)
		}
	}
	if strings.Contains(line, "remove") {
		t.Errorf("a remove flag was sent for a save: %s", line)
	}
	sent.Reset()
	if _, err := d.SubmitBlueprintWatch(context.Background(), BlueprintWatchParams{Name: "duo", Agent: "b", Pattern: "stage.*", Remove: true}); err == nil {
		// the fake session has no second answer; what matters is the request
	}
	if line := sent.String(); !strings.Contains(line, `"remove":true`) || strings.Contains(line, `"action"`) {
		t.Errorf("a removal must send the pair and the flag only: %s", line)
	}
	if _, err := d.SubmitBlueprintWatch(context.Background(), BlueprintWatchParams{Name: "duo", Agent: "b"}); err == nil {
		t.Fatal("a request without events to watch was sent")
	}
}

func TestChatSendRunsClientToolsAndAnswersTheCore(t *testing.T) {
	var sent bytes.Buffer
	d := sessionWithWriter(t, &sent,
		`{"type":"chat.client_tool","call_id":"u1","name":"ui_edit","arguments":{"commands":["/ui set x text hi"]}}`,
		`{"id":"chat-send","ok":true,"result":{"text":"done"}}`)
	var calls []ClientToolCall
	res, err := d.SubmitChatSend(context.Background(), ChatSendParams{Prompt: "p", Workdir: "/proj",
		ClientTools: []ClientToolDef{{Name: "ui_edit", Description: "Change the interface."}},
		OnClientTool: func(_ context.Context, c ClientToolCall) ClientToolResult {
			calls = append(calls, c)
			return ClientToolResult{OK: true, Text: "applied", Summary: "Changed the interface", Diff: "    1 + x\n"}
		}})
	if err != nil || res.Text != "done" {
		t.Fatalf("res=%+v err=%v", res, err)
	}
	if len(calls) != 1 || calls[0].CallID != "u1" || !strings.Contains(string(calls[0].Arguments), "/ui set x text hi") {
		t.Fatalf("the client tool was called with %+v; it must get the call id and the raw arguments", calls)
	}
	lines := strings.Split(strings.TrimSpace(sent.String()), "\n")
	if len(lines) != 2 || !strings.Contains(lines[0], `"client_tools":"[{\"name\":\"ui_edit\"`) {
		t.Fatalf("request = %v; the lent tools must ride as the client_tools string", lines)
	}
	var answer map[string]any
	if json.Unmarshal([]byte(lines[1]), &answer) != nil || answer["type"] != "chat.client_result" ||
		answer["call_id"] != "u1" || answer["ok"] != true || answer["text"] != "applied" || answer["diff"] != "    1 + x\n" {
		t.Fatalf("answer = %s; the core waits for exactly this line and the turn hangs without it", lines[1])
	}
}

func TestChatSendWithoutAClientToolHandlerFailsTheCallNotTheTurn(t *testing.T) {
	var sent bytes.Buffer
	d := sessionWithWriter(t, &sent,
		`{"type":"chat.client_tool","call_id":"u1","name":"ui_edit","arguments":{}}`,
		`{"id":"chat-send","ok":true,"result":{"text":"done"}}`)
	if _, err := d.SubmitChatSend(context.Background(), ChatSendParams{Prompt: "p", Workdir: "/proj"}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(sent.String(), `"type":"chat.client_result"`) || !strings.Contains(sent.String(), `"ok":false`) {
		t.Fatalf("sent %s; an unanswered client call would leave the core waiting forever", sent.String())
	}
}

func TestChatSendNamesAnOldCoreThatRefusesClientTools(t *testing.T) {
	d := session(t, `{"id":"chat-send","ok":false,"error":{"code":"bad_params","message":"chat.send does not take client_tools. It takes: edits, workdir, web"}}`)
	_, err := d.SubmitChatSend(context.Background(), ChatSendParams{Prompt: "p", Workdir: "/proj",
		ClientTools: []ClientToolDef{{Name: "ui_edit"}}})
	if !errors.Is(err, ErrClientToolsUnsupported) {
		t.Fatalf("err = %v; the caller needs ErrClientToolsUnsupported to retry without the client tools", err)
	}
}

func TestModelUpdateSendsOnlyWhatChanged(t *testing.T) {
	var sent bytes.Buffer
	d := sessionWithWriter(t, &sent, `{"id":"model-update","ok":true,"result":{"provider":"groq","model":"new","was":"old","changed":true}}`)
	newID := "new"
	res, err := d.SubmitModelUpdate(context.Background(), ModelUpdateParams{Ref: "groq/old", NewID: &newID})
	if err != nil {
		t.Fatalf("SubmitModelUpdate: %v", err)
	}
	if res.Model != "new" || res.Was != "old" || !res.Changed {
		t.Fatalf("answer lost: %+v", res)
	}
	line := sent.String()
	if !strings.Contains(line, `"type":"model.update"`) || !strings.Contains(line, `"id":"new"`) {
		t.Fatalf("request does not carry the verb and the new id: %s", line)
	}
	if strings.Contains(line, `"in"`) || strings.Contains(line, "no_price") {
		t.Fatalf("an unchanged price was sent, which would overwrite the declared one: %s", line)
	}
}

func TestModelUpdateRefusesOnePriceAloneAndAnUnnamedModel(t *testing.T) {
	d := session(t)
	in := 1.0
	if _, err := d.SubmitModelUpdate(context.Background(), ModelUpdateParams{Ref: "a/b", In: &in}); err == nil {
		t.Error("one price alone was sent; it would price the other direction at zero")
	}
	if _, err := d.SubmitModelUpdate(context.Background(), ModelUpdateParams{}); err == nil {
		t.Error("an update without a model was sent")
	}
}
