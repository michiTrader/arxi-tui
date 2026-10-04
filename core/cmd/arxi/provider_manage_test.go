package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/michiTrader/arxi/internal/modelstore"
)

// fakeLLM is an OpenAI-compatible endpoint on loopback. It records what it was
// sent, so the tests can assert on the wire and not only on the return value.
type fakeLLM struct {
	srv *httptest.Server

	mu       sync.Mutex
	auth     []string
	messages [][]map[string]any
	efforts  []string // reasoning_effort of each chat request ("" when absent)
	models   []string
	status   int
}

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
			var body struct {
				Messages        []map[string]any `json:"messages"`
				ReasoningEffort string           `json:"reasoning_effort"`
			}
			json.NewDecoder(r.Body).Decode(&body)
			f.messages = append(f.messages, body.Messages)
			f.efforts = append(f.efforts, body.ReasoningEffort)
			last, _ := body.Messages[len(body.Messages)-1]["content"].(string)
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
	for _, effort := range []string{"", "auto", "AUTO", "high", " Low ", "minimal", "medium"} {
		if _, err := chatSendEffort(context.Background(), "hi", "", "", "", effort); err != nil {
			t.Fatalf("effort %q: %v", effort, err)
		}
	}
	want := []string{"", "", "", "high", "low", "minimal", "medium"}
	if strings.Join(f.efforts, ",") != strings.Join(want, ",") {
		t.Errorf("reasoning_effort on the wire = %q, want %q", f.efforts, want)
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
