package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/michiTrader/arxi/internal/modelstore"
	"github.com/michiTrader/arxi/internal/provider"
)

// fakeLLM is an OpenAI-compatible endpoint on loopback. It records what it was
// sent, so the tests can assert on the wire and not only on the return value.
type fakeLLM struct {
	srv *httptest.Server

	mu       sync.Mutex
	auth     []string
	messages [][]map[string]any
	failNext int // answer this many chat requests with failCode before succeeding
	failCode int
	// failBody replaces the bare "error code: N" body; failHeader sets Retry-After.
	failBody   string
	failHeader string
	calls      int      // chat requests seen
	efforts    []string // reasoning_effort of each chat request ("" when absent)
	thinking   []string // thinking.type of each chat request ("" when absent)
	models     []string
	status     int
	streams    int      // chat requests that asked for server-sent events
	think      []string // reasoning fragments a streamed answer sends first
	script     []scripted
	tools      [][]string // names of the tools offered with each chat request
}

// scripted is one step of a tool conversation: while steps remain, the model
// answers a request by asking for the named tool; afterwards it answers text.
type scripted struct{ name, args, text string }

func newFakeLLM(t *testing.T, models ...string) *fakeLLM {
	t.Helper()
	f := &fakeLLM{models: models, status: 200}
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		f.auth = append(f.auth, r.Header.Get("Authorization"))
		w.Header().Set("Content-Type", "application/json")
		if f.status != 200 {
			w.WriteHeader(f.status)
			io.WriteString(w, `{"error":{"message":"bad key"}}`)
			return
		}
		switch {
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/models"):
			var data []map[string]string
			for _, m := range f.models {
				data = append(data, map[string]string{"id": m})
			}
			json.NewEncoder(w).Encode(map[string]any{"data": data})
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/chat/completions"):
			f.calls++
			if f.failNext > 0 {
				f.failNext--
				if f.failHeader != "" {
					w.Header().Set("Retry-After", f.failHeader)
				}
				w.WriteHeader(f.failCode)
				body := f.failBody
				if body == "" {
					body = "error code: " + strconv.Itoa(f.failCode)
				}
				io.WriteString(w, body)
				return
			}
			var body struct {
				Messages        []map[string]any `json:"messages"`
				ReasoningEffort string           `json:"reasoning_effort"`
				Thinking        *struct {
					Type string `json:"type"`
				} `json:"thinking"`
				Stream bool `json:"stream"`
				Tools  []struct {
					Function struct {
						Name string `json:"name"`
					} `json:"function"`
				} `json:"tools"`
			}
			json.NewDecoder(r.Body).Decode(&body)
			f.messages = append(f.messages, body.Messages)
			f.efforts = append(f.efforts, body.ReasoningEffort)
			switch {
			case body.Thinking != nil:
				f.thinking = append(f.thinking, body.Thinking.Type)
			default:
				f.thinking = append(f.thinking, "")
			}
			var offered []string
			for _, tl := range body.Tools {
				offered = append(offered, tl.Function.Name)
			}
			f.tools = append(f.tools, offered)
			if step := len(f.tools) - 1; step < len(f.script) {
				f.answerScripted(w, f.script[step], body.Stream)
				return
			}
			last, _ := body.Messages[len(body.Messages)-1]["content"].(string)
			if body.Stream {
				f.streams++
				w.Header().Set("Content-Type", "text/event-stream")
				for _, frag := range f.think {
					b, _ := json.Marshal(map[string]any{"choices": []any{map[string]any{"delta": map[string]any{"reasoning_content": frag}}}})
					io.WriteString(w, "data: "+string(b)+"\n\n")
				}
				b, _ := json.Marshal(map[string]any{"id": "x", "choices": []any{map[string]any{"delta": map[string]any{"content": "echo: " + last}, "finish_reason": "stop"}},
					"usage": map[string]int{"prompt_tokens": 3, "completion_tokens": 2}})
				io.WriteString(w, "data: "+string(b)+"\n\ndata: [DONE]\n\n")
				return
			}
			json.NewEncoder(w).Encode(map[string]any{
				"id":      "x",
				"choices": []any{map[string]any{"index": 0, "finish_reason": "stop", "message": map[string]any{"role": "assistant", "content": "echo: " + last}}},
				"usage":   map[string]int{"prompt_tokens": 3, "completion_tokens": 2},
			})
		default:
			w.WriteHeader(404)
		}
	}))
	t.Cleanup(f.srv.Close)
	return f
}

// answerScripted asks for one tool, in the plain shape or as the fragments a
// stream sends, so both wires are exercised.
func (f *fakeLLM) answerScripted(w http.ResponseWriter, st scripted, stream bool) {
	id := "call_" + st.name
	if !stream {
		json.NewEncoder(w).Encode(map[string]any{
			"id": "x",
			"choices": []any{map[string]any{"index": 0, "finish_reason": "tool_calls", "message": map[string]any{
				"role": "assistant", "content": nil,
				"tool_calls": []any{map[string]any{"id": id, "type": "function", "function": map[string]any{"name": st.name, "arguments": st.args}}},
			}}},
			"usage": map[string]int{"prompt_tokens": 10, "completion_tokens": 5},
		})
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	send := func(v any) {
		b, _ := json.Marshal(v)
		io.WriteString(w, "data: "+string(b)+"\n\n")
	}
	delta := func(d map[string]any, finish string) map[string]any {
		c := map[string]any{"delta": d}
		if finish != "" {
			c["finish_reason"] = finish
		}
		return map[string]any{"choices": []any{c}}
	}
	half := len(st.args) / 2
	send(delta(map[string]any{"tool_calls": []any{map[string]any{"index": 0, "id": id, "type": "function", "function": map[string]any{"name": st.name, "arguments": ""}}}}, ""))
	send(delta(map[string]any{"tool_calls": []any{map[string]any{"index": 0, "function": map[string]any{"arguments": st.args[:half]}}}}, ""))
	send(delta(map[string]any{"tool_calls": []any{map[string]any{"index": 0, "function": map[string]any{"arguments": st.args[half:]}}}}, "tool_calls"))
	send(map[string]any{"choices": []any{}, "usage": map[string]int{"prompt_tokens": 10, "completion_tokens": 5}})
	io.WriteString(w, "data: [DONE]\n\n")
}

func (f *fakeLLM) url() string { return f.srv.URL + "/v1" }

func TestChatNeedsAProviderAndSaysSo(t *testing.T) {
	isolate(t)
	_, err := chatSend(context.Background(), "hi", "", "", "")
	if err == nil || !strings.Contains(err.Error(), "no provider is set up") {
		t.Fatalf("err = %v; want the no-provider remedy", err)
	}
}

func TestChatWithAProviderButNoModelsPointsAtDiscovery(t *testing.T) {
	isolate(t)
	f := newFakeLLM(t)
	if _, err := registerProvider("fake", f.url(), "", ""); err != nil {
		t.Fatal(err)
	}
	_, err := chatSend(context.Background(), "hi", "", "", "")
	if err == nil || !strings.Contains(err.Error(), "no model is available") {
		t.Fatalf("err = %v; want the no-model remedy", err)
	}
}

func TestDiscoverAddsTheModelsTheEndpointServesOnceAndOnlyOnce(t *testing.T) {
	isolate(t)
	f := newFakeLLM(t, "fake-small", "fake-big", "bad id")
	if _, err := registerProvider("fake", f.url(), "", ""); err != nil {
		t.Fatal(err)
	}
	got, err := discoverModels(context.Background(), "fake")
	if err != nil {
		t.Fatal(err)
	}
	if got.Found != 2 || got.Added != 2 {
		t.Fatalf("discover = %+v; want 2 found and 2 added (an id with a space is unusable and dropped)", got)
	}
	again, err := discoverModels(context.Background(), "fake")
	if err != nil || again.Added != 0 {
		t.Fatalf("second discover = %+v, %v; want nothing new", again, err)
	}
	rows, _ := listProviders()
	if rows[0].Models != 2 {
		t.Fatalf("models = %d; want 2", rows[0].Models)
	}
}

func TestDiscoverKeepsWhatTheUserSetOnExistingModels(t *testing.T) {
	isolate(t)
	f := newFakeLLM(t, "fake-small", "fake-big")
	if _, err := registerProvider("fake", f.url(), "", ""); err != nil {
		t.Fatal(err)
	}
	in, out := 1.5, 2.5
	if _, err := addModel("fake", "fake-big", &in, &out); err != nil {
		t.Fatal(err)
	}
	if _, err := setModelEnabled(map[string]any{"model": "fake/fake-big"}, false); err != nil {
		t.Fatal(err)
	}
	if _, err := discoverModels(context.Background(), "fake"); err != nil {
		t.Fatal(err)
	}
	store, _ := providerStore()
	p, _ := store.Load("fake")
	for _, m := range p.Models {
		if m.ID == "fake-big" && (m.Enabled || m.Price == nil || m.Price.InUSDPerMTok != 1.5) {
			t.Fatalf("discover overwrote the user's settings on fake-big: %+v", m)
		}
	}
}

func TestDiscoverFailureNamesTheProviderAndKeepsTheKeyOut(t *testing.T) {
	isolate(t)
	f := newFakeLLM(t)
	f.status = 401
	if _, err := registerProvider("fake", f.url(), "", leakyKey); err != nil {
		t.Fatal(err)
	}
	_, err := discoverModels(context.Background(), "fake")
	if err == nil || !strings.Contains(err.Error(), "401") {
		t.Fatalf("err = %v; want the HTTP status", err)
	}
	assertNoKey(t, "the discover error", err)
	if got := f.auth[0]; got != "Bearer "+leakyKey {
		t.Fatalf("the stored key was not sent as the bearer: %q", got)
	}
}

func TestChatSendsTheDefaultModelWithHistoryAndSystem(t *testing.T) {
	isolate(t)
	f := newFakeLLM(t, "fake-small", "fake-big")
	if _, err := registerProvider("fake", f.url(), "", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := discoverModels(context.Background(), "fake"); err != nil {
		t.Fatal(err)
	}
	// Two enabled models and no default: guessing would bill the wrong one.
	if _, err := chatSend(context.Background(), "hi", "", "", ""); err == nil || !strings.Contains(err.Error(), "choose one with /model") {
		t.Fatalf("err = %v; want the choose-a-model remedy", err)
	}
	if ref, err := defaultModel("fake-big"); err != nil || ref != "fake/fake-big" {
		t.Fatalf("defaultModel = %q, %v", ref, err)
	}
	hist := `[{"role":"user","text":"one"},{"role":"assistant","text":"two"}]`
	res, err := chatSend(context.Background(), "three", hist, "be brief", "")
	if err != nil {
		t.Fatal(err)
	}
	if res.Text != "echo: three" || res.Model != "fake-big" || res.Provider != "fake" || res.OutputTokens != 2 {
		t.Fatalf("result = %+v", res)
	}
	msgs := f.messages[0]
	var roles []string
	for _, m := range msgs {
		roles = append(roles, m["role"].(string))
	}
	if strings.Join(roles, ",") != "system,user,assistant,user" {
		t.Fatalf("roles on the wire = %v; want system,user,assistant,user", roles)
	}
}

func TestChatReportsAProviderRefusalInsteadOfStayingSilent(t *testing.T) {
	isolate(t)
	f := newFakeLLM(t, "fake-small")
	if _, err := registerProvider("fake", f.url(), "", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := discoverModels(context.Background(), "fake"); err != nil {
		t.Fatal(err)
	}
	f.status = 401
	_, err := chatSend(context.Background(), "hi", "", "", "")
	if err == nil || !strings.Contains(err.Error(), "bad key") {
		t.Fatalf("err = %v; want the provider's own reason", err)
	}
}

func TestChatRefusesAnEmptyMessageAndABadHistory(t *testing.T) {
	isolate(t)
	if _, err := chatSend(context.Background(), "  ", "", "", ""); err == nil {
		t.Error("an empty message was accepted")
	}
	if _, err := chatSend(context.Background(), "x", "{not json", "", ""); err == nil {
		t.Error("a malformed history was accepted")
	}
}

func TestDefaultModelIsRefusedWhenDisabledAndForgottenWhenRemoved(t *testing.T) {
	isolate(t)
	f := newFakeLLM(t, "a", "b")
	if _, err := registerProvider("fake", f.url(), "", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := discoverModels(context.Background(), "fake"); err != nil {
		t.Fatal(err)
	}
	if _, err := setModelEnabled(map[string]any{"model": "b"}, false); err != nil {
		t.Fatal(err)
	}
	if _, err := defaultModel("b"); err == nil {
		t.Error("a disabled model became the default")
	}
	if _, err := defaultModel("a"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := removeModel("a"); err != nil {
		t.Fatal(err)
	}
	if ref, err := defaultModel(""); err != nil || ref != "" {
		t.Fatalf("default after removing its model = %q, %v; want none", ref, err)
	}
}

func TestRemovingAProviderTakesItsKeyAndItsDefaultWithIt(t *testing.T) {
	_, secrets := isolate(t)
	f := newFakeLLM(t, "a")
	if _, err := registerProvider("fake", f.url(), "", leakyKey); err != nil {
		t.Fatal(err)
	}
	if _, err := discoverModels(context.Background(), "fake"); err != nil {
		t.Fatal(err)
	}
	if _, err := defaultModel("a"); err != nil {
		t.Fatal(err)
	}
	if err := removeProvider("FAKE"); err != nil {
		t.Fatal(err)
	}
	if rows, _ := listProviders(); len(rows) != 0 {
		t.Fatalf("providers left: %v", rows)
	}
	entries, _ := os.ReadDir(secrets)
	if len(entries) != 0 {
		t.Fatalf("the stored key outlived its provider: %v", entries)
	}
	store, _ := modelstore.Open(providerDir)
	if d, _ := store.Default(); d != "" {
		t.Fatalf("default = %q after its provider was removed", d)
	}
	if err := removeProvider("fake"); err == nil {
		t.Error("removing a provider twice reported success")
	}
}

func TestUpdateChangesTheEndpointAndKeyButNotTheModels(t *testing.T) {
	isolate(t)
	f := newFakeLLM(t, "a", "b")
	if _, err := registerProvider("fake", f.url(), "", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := discoverModels(context.Background(), "fake"); err != nil {
		t.Fatal(err)
	}
	p, stored, err := updateProvider("fake", f.url()+"/", "FAKE_KEY", leakyKey)
	if err != nil || !stored {
		t.Fatalf("update = %v, stored %v", err, stored)
	}
	if p.BaseURL != f.url() || p.APIKeyEnv != "FAKE_KEY" || len(p.Models) != 2 {
		t.Fatalf("provider after update = %+v", p)
	}
	assertNoKey(t, "the update result", p)
	if _, _, err := updateProvider("fake", "http://example.com/v1", "", ""); err == nil {
		t.Error("plain http to a remote host was accepted")
	}
	if _, _, err := updateProvider("nope", "https://x.test/v1", "", ""); err == nil {
		t.Error("updating an unknown provider reported success")
	}
	if p, _, err := updateProvider("fake", "", "none", ""); err != nil || p.APIKeyEnv != "" {
		t.Fatalf("clearing the key variable: %+v, %v", p, err)
	}
}

func TestTheNewVerbsAnswerOverTheProtocol(t *testing.T) {
	isolate(t)
	f := newFakeLLM(t, "m1")
	for _, step := range []struct {
		typ    string
		params map[string]any
	}{
		{"provider.add", map[string]any{"name": "fake", "base_url": f.url()}},
		{"model.discover", map[string]any{"provider": "fake"}},
		{"model.default", map[string]any{"model": "m1"}},
		{"chat.send", map[string]any{"prompt": "ping"}},
		{"provider.update", map[string]any{"name": "fake", "base_url": f.url()}},
		{"model.remove", map[string]any{"model": "m1"}},
		{"provider.remove", map[string]any{"name": "fake"}},
	} {
		h, ok := protoHandlers[step.typ]
		if !ok {
			t.Fatalf("%s has no handler", step.typ)
		}
		if _, err := h(step.params); err != nil {
			t.Fatalf("%s: %v", step.typ, err)
		}
	}
}

func TestChatSendsTheThinkingLevelOnlyWhenAsked(t *testing.T) {
	isolate(t)
	f := newFakeLLM(t, "fake-small")
	if _, err := registerProvider("fake", f.url(), "", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := discoverModels(context.Background(), "fake"); err != nil {
		t.Fatal(err)
	}
	for _, effort := range []string{"", "auto", "AUTO", "high", " Low ", "medium"} {
		if _, err := chatSendEffort(context.Background(), "hi", "", "", "", effort); err != nil {
			t.Fatalf("effort %q: %v", effort, err)
		}
	}
	want := []string{"", "", "", "high", "low", "medium"}
	if strings.Join(f.efforts, ",") != strings.Join(want, ",") {
		t.Errorf("reasoning_effort on the wire = %q, want %q", f.efforts, want)
	}
}

// TestChatThinkingLevelsFollowTheModel pins the per-model rules end to end: a
// DeepSeek V4 model has no medium, switches thinking off with the `thinking`
// field and takes max; a level the model lacks is refused before billing, with
// the levels it does take named.
func TestChatThinkingLevelsFollowTheModel(t *testing.T) {
	isolate(t)
	f := newFakeLLM(t, "deepseek-v4.1-flash")
	if _, err := registerProvider("fake", f.url(), "", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := discoverModels(context.Background(), "fake"); err != nil {
		t.Fatal(err)
	}
	for _, effort := range []string{"max", "low", "off"} {
		if _, err := chatSendEffort(context.Background(), "hi", "", "", "", effort); err != nil {
			t.Fatalf("effort %q: %v", effort, err)
		}
	}
	if got, want := strings.Join(f.efforts, ","), "max,low,"; got != want {
		t.Errorf("reasoning_effort = %q, want %q", got, want)
	}
	if got, want := strings.Join(f.thinking, ","), ",,disabled"; got != want {
		t.Errorf("thinking.type = %q, want %q", got, want)
	}
	for _, bad := range []string{"medium", "minimal", "xhigh"} {
		_, err := chatSendEffort(context.Background(), "hi", "", "", "", bad)
		if err == nil || !strings.Contains(err.Error(), "off, low, high, max") {
			t.Errorf("effort %q: err = %v; want a refusal listing off, low, high, max", bad, err)
		}
	}
	if len(f.efforts) != 3 {
		t.Errorf("the provider was called %d times, want 3 (refusals must not bill)", len(f.efforts))
	}
}

func TestChatRefusesAnUnknownThinkingLevelBeforeBilling(t *testing.T) {
	isolate(t)
	f := newFakeLLM(t, "fake-small")
	if _, err := registerProvider("fake", f.url(), "", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := discoverModels(context.Background(), "fake"); err != nil {
		t.Fatal(err)
	}
	_, err := chatSendEffort(context.Background(), "hi", "", "", "", "ultra")
	if err == nil || !strings.Contains(err.Error(), "ultra") {
		t.Fatalf("err = %v; want a refusal naming the bad level", err)
	}
	if len(f.efforts) != 0 {
		t.Errorf("the provider was called %d times for a refused request", len(f.efforts))
	}
}

func fastRetries(t *testing.T) {
	t.Helper()
	saved, savedMax, savedWait, savedMargin := chatBackoff, chatMaxBackoff, chatMaxWait, chatRetryMargin
	chatBackoff, chatMaxBackoff, chatRetryMargin = time.Millisecond, 8*time.Millisecond, time.Millisecond
	t.Cleanup(func() {
		chatBackoff, chatMaxBackoff, chatMaxWait, chatRetryMargin = saved, savedMax, savedWait, savedMargin
	})
}

func chatProvider(t *testing.T) *fakeLLM {
	t.Helper()
	isolate(t)
	fastRetries(t)
	f := newFakeLLM(t, "fake-small")
	if _, err := registerProvider("fake", f.url(), "", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := discoverModels(context.Background(), "fake"); err != nil {
		t.Fatal(err)
	}
	return f
}

func TestChatRetriesAGatewayTimeoutAndThenAnswers(t *testing.T) {
	for _, code := range []int{504, 502, 503, 429} {
		f := chatProvider(t)
		f.failNext, f.failCode = 2, code
		res, err := chatSend(context.Background(), "hi", "", "", "")
		if err != nil {
			t.Fatalf("HTTP %d twice then ok: %v", code, err)
		}
		if res.Text != "echo: hi" || f.calls != 3 {
			t.Errorf("HTTP %d: text %q after %d calls, want the answer on the 3rd", code, res.Text, f.calls)
		}
	}
}

func TestChatGivesUpWithAPlainSentenceAfterTheLastTry(t *testing.T) {
	f := chatProvider(t)
	f.failNext, f.failCode = 99, 504
	_, err := chatSend(context.Background(), "hi", "", "", "")
	if err == nil {
		t.Fatal("a provider that always times out must end in an error")
	}
	if f.calls != chatAttempts {
		t.Errorf("tried %d times, want %d", f.calls, chatAttempts)
	}
	for _, want := range []string{"timed out or is overloaded", "504", fmt.Sprintf("tried %d times", chatAttempts), "/effort", "/model"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the error should mention %q: %v", want, err)
		}
	}
}

// A 429 whose message says "Retry in 27s" is waited out, not given up on: the user must
// never have to type "retry" by hand.
func TestChatRetriesARateLimitUntilItClears(t *testing.T) {
	f := chatProvider(t)
	f.failNext, f.failCode = 4, 429
	// The wait the provider names in its message is honoured; 1ms keeps the test fast.
	f.failBody = `{"error":{"type":"rate_limit_error","message":"Rate limit exceeded. Retry in 1ms."}}`
	var notes []chatRetryNotification
	ctx := withRetryNotice(context.Background(), func(n chatRetryNotification) { notes = append(notes, n) })
	res, err := chatSendEffort(ctx, "hi", "", "", "", "")
	if err != nil {
		t.Fatalf("429 four times then ok: %v", err)
	}
	if res.Text != "echo: hi" || f.calls != 5 {
		t.Errorf("text %q after %d calls, want the answer on the 5th", res.Text, f.calls)
	}
	if len(notes) != 4 {
		t.Fatalf("%d retry notices, want 4", len(notes))
	}
	for i, n := range notes {
		if n.Type != "chat.retry" || n.Status != "429" || n.Attempt != i+2 || n.Of != chatAttempts || n.WaitMs <= 0 {
			t.Errorf("notice %d = %+v", i, n)
		}
	}
}

func TestChatRetryHonoursRetryAfterHeader(t *testing.T) {
	f := chatProvider(t)
	f.failNext, f.failCode, f.failHeader = 1, 429, "2"
	var notes []chatRetryNotification
	ctx := withRetryNotice(context.Background(), func(n chatRetryNotification) { notes = append(notes, n) })
	chatBackoff, chatMaxBackoff = time.Hour, time.Hour // would hang if the header were ignored
	chatMaxWait, chatRetryMargin = 5*time.Second, time.Second
	start := time.Now()
	if _, err := chatSendEffort(ctx, "hi", "", "", "", ""); err != nil {
		t.Fatal(err)
	}
	if len(notes) != 1 || notes[0].WaitMs != 3000 {
		t.Fatalf("notices = %+v, want one wait of 3000ms (2s asked + 1s margin)", notes)
	}
	if time.Since(start) < 2*time.Second {
		t.Error("the call did not wait for what the provider asked")
	}
}

func TestChatDoesNotWaitForeverOnALongRetryAfter(t *testing.T) {
	f := chatProvider(t)
	f.failNext, f.failCode, f.failHeader = 99, 429, "3600"
	_, err := chatSendEffort(context.Background(), "hi", "", "", "", "")
	if err == nil || f.calls != 1 {
		t.Fatalf("err=%v calls=%d; an hour-long ask must end the call at once", err, f.calls)
	}
	if !strings.Contains(err.Error(), "longer than") {
		t.Errorf("the error should say the provider asked for too long: %v", err)
	}
}

func TestChatDoesNotRetryABillingWall(t *testing.T) {
	for _, code := range []int{402, 429} {
		f := chatProvider(t)
		f.failNext, f.failCode = 99, code
		f.failBody = `{"error":{"type":"billing_error","message":"This model is currently available only with a subscription."}}`
		_, err := chatSendEffort(context.Background(), "hi", "", "", "", "")
		if err == nil || f.calls != 1 {
			t.Fatalf("HTTP %d billing: err=%v calls=%d, want one call", code, err, f.calls)
		}
		if code == 429 && !strings.Contains(err.Error(), "billing, not the moment") {
			t.Errorf("a billing wall should be explained as such: %v", err)
		}
	}
}

func TestRetryWaitGrowsAndCaps(t *testing.T) {
	saved, savedMax := chatBackoff, chatMaxBackoff
	chatBackoff, chatMaxBackoff = 2*time.Second, 30*time.Second
	t.Cleanup(func() { chatBackoff, chatMaxBackoff = saved, savedMax })
	var got []time.Duration
	for n := 1; n <= 7; n++ {
		d, ok := retryWait(n, 0)
		if !ok {
			t.Fatal("a backoff without an ask is always allowed")
		}
		got = append(got, d)
	}
	want := []time.Duration{2, 4, 8, 16, 30, 30, 30}
	for i := range want {
		if got[i] != want[i]*time.Second {
			t.Errorf("wait before retry %d = %v, want %v", i+1, got[i], want[i]*time.Second)
		}
	}
}

func TestChatDoesNotRetryWhatRetryingCannotFix(t *testing.T) {
	for _, code := range []int{400, 401, 403, 404, 500} {
		f := chatProvider(t)
		f.failNext, f.failCode = 99, code
		if _, err := chatSend(context.Background(), "hi", "", "", ""); err == nil {
			t.Fatalf("HTTP %d must be an error", code)
		}
		if f.calls != 1 {
			t.Errorf("HTTP %d was tried %d times; a refusal retrying cannot fix must not be repeated", code, f.calls)
		}
	}
}

func TestChatStopsRetryingWhenCancelled(t *testing.T) {
	f := chatProvider(t)
	chatBackoff, chatMaxBackoff = time.Hour, time.Hour
	f.failNext, f.failCode = 99, 504
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	start := time.Now()
	if _, err := chatSend(ctx, "hi", "", "", ""); err == nil {
		t.Fatal("a cancelled chat must be an error")
	}
	if time.Since(start) > 5*time.Second {
		t.Error("the wait between tries ignored the cancellation")
	}
}

func setUpFake(t *testing.T) *fakeLLM {
	t.Helper()
	isolate(t)
	f := newFakeLLM(t, "fake-small")
	if _, err := registerProvider("fake", f.url(), "", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := discoverModels(context.Background(), "fake"); err != nil {
		t.Fatal(err)
	}
	return f
}

func TestChatStreamsTheThinkingAndStillReturnsTheWholeAnswer(t *testing.T) {
	f := setUpFake(t)
	f.think = []string{"The user ", "says hi."}
	var got []string
	ctx := provider.WithThinking(context.Background(), func(s string) { got = append(got, s) })
	res, err := chatSendEffort(ctx, "hi", "", "", "", "high")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(got, "|") != "The user |says hi." {
		t.Errorf("thinking fragments = %q", got)
	}
	if res.Text != "echo: hi" || res.InputTokens != 3 || res.OutputTokens != 2 {
		t.Errorf("result = %+v; the streamed answer must carry the text and the usage block", res)
	}
	if f.streams != 1 || f.efforts[0] != "high" {
		t.Errorf("streams=%d efforts=%q; the level must ride along on a streamed request too", f.streams, f.efforts)
	}
}

func TestChatDoesNotStreamUnlessAsked(t *testing.T) {
	f := setUpFake(t)
	f.think = []string{"never seen"}
	if _, err := chatSendEffort(context.Background(), "hi", "", "", "", ""); err != nil {
		t.Fatal(err)
	}
	if f.streams != 0 {
		t.Errorf("a plain chat turn streamed (%d); only a turn that asked to watch the thinking may", f.streams)
	}
}

func TestStreamedChatKeepsTheRetries(t *testing.T) {
	f := setUpFake(t)
	f.failNext, f.failCode = 1, 504
	ctx := provider.WithThinking(context.Background(), func(string) {})
	res, err := chatSendEffort(ctx, "hi", "", "", "", "")
	if err != nil {
		t.Fatalf("a 504 on a streamed turn was not retried: %v", err)
	}
	if res.Text != "echo: hi" || f.calls != 2 {
		t.Errorf("text=%q calls=%d; want the answer after one retry", res.Text, f.calls)
	}
}
