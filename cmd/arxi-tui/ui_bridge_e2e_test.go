package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/michiTrader/arxi_tui/internal/theme"
)

// scriptedInterfaceModel is an OpenAI-compatible service whose model does what a
// real one should when asked to change the interface: read the guide, propose the
// change, then answer. It records the tools it was offered and what each tool
// returned, so the test can check the model was given the guide only when it asked.
type scriptedInterfaceModel struct {
	// edit is the ui_edit arguments the model sends; empty means extraGap.
	edit    string
	mu      sync.Mutex
	step    int
	offered [][]string
	results []string
	systems []string
}

func (m *scriptedInterfaceModel) serve(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if strings.HasSuffix(r.URL.Path, "/models") {
			fmt.Fprint(w, `{"data":[{"id":"m1"}]}`)
			return
		}
		body, _ := io.ReadAll(r.Body)
		var req struct {
			Messages []map[string]any `json:"messages"`
			Tools    []struct {
				Function struct {
					Name string `json:"name"`
				} `json:"function"`
			} `json:"tools"`
		}
		_ = json.Unmarshal(body, &req)
		m.mu.Lock()
		defer m.mu.Unlock()
		var names []string
		for _, tl := range req.Tools {
			names = append(names, tl.Function.Name)
		}
		m.offered = append(m.offered, names)
		if len(req.Messages) > 0 {
			sys, _ := req.Messages[0]["content"].(string)
			m.systems = append(m.systems, sys)
			if last := req.Messages[len(req.Messages)-1]; last["role"] == "tool" {
				c, _ := last["content"].(string)
				m.results = append(m.results, c)
			}
		}
		call := func(id, name, args string) {
			b, _ := json.Marshal(args)
			fmt.Fprintf(w, `{"id":"x","model":"m1","choices":[{"index":0,"message":{"role":"assistant","content":null,`+
				`"tool_calls":[{"id":%q,"type":"function","function":{"name":%q,"arguments":%s}}]},"finish_reason":"tool_calls"}],`+
				`"usage":{"prompt_tokens":1,"completion_tokens":1}}`, id, name, b)
		}
		m.step++
		edit := m.edit
		if edit == "" {
			edit = extraGap
		}
		switch m.step {
		case 1:
			call("c1", uiToolGuide, `{}`)
		case 2:
			call("c2", uiToolEdit, edit)
		default:
			fmt.Fprint(w, `{"id":"x","model":"m1","choices":[{"index":0,"message":{"role":"assistant","content":"Done: there is now a blank line under the input bar."},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1}}`)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

// TestTheAgentChangesTheInterfaceThroughTheRealCore is the reported failure, fixed,
// with the real core process: the user asks for a blank line between the input bar
// and the status bar; the model reads the guide, proposes the change, the user is
// asked and allows it, and the change comes back to the loop to be applied.
func TestTheAgentChangesTheInterfaceThroughTheRealCore(t *testing.T) {
	bin := buildCore(t)
	work := t.TempDir()
	t.Setenv("ARXI_SECRETS_DIR", filepath.Join(work, "secrets"))
	t.Setenv("ARXI_PROVIDERS_DIR", filepath.Join(work, "providers"))
	t.Setenv("ARXI_BIN", bin)
	t.Setenv("HOME", work)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(work, "cfg"))
	t.Setenv(configDirEnv, filepath.Join(work, "arxi"))
	t.Chdir(work)

	model := &scriptedInterfaceModel{}
	srv := model.serve(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	drv, evCh, err := openServeDriver(ctx, bin)
	if err != nil {
		t.Fatalf("openServeDriver: %v", err)
	}
	defer drv.Close()
	sd := drv.(*serveDriver)
	hub := sd.Hub()
	if out := runHubWork(ctx, hub, hubWork{Op: opAdd, Name: "fake", BaseURL: srv.URL + "/v1", Key: "sk-test-0123456789abcdefghijklmnop"}); !out.ok {
		t.Fatalf("adding the provider: %q", out.notice)
	}
	if out := runHubWork(ctx, hub, hubWork{Op: opDefault, Ref: "fake/m1", Close: true}); !out.ok {
		t.Fatalf("choosing the model: %q", out.notice)
	}

	br := sd.InterfaceBridge()
	doc := builtinDoc(t)
	br.publish(doc, theme.SOBRIA(), nil)
	if err := sd.SubmitPrompt(ctx, "add a blank line between the input bar and the status bar"); err != nil {
		t.Fatal(err)
	}
	var asked, answered string
	applied := false
	deadline := time.After(30 * time.Second)
	for answered == "" {
		select {
		case ev := <-evCh:
			switch ev.Type {
			case "chat.error":
				t.Fatalf("chat failed: %v", ev.Payload["text"])
			case "chat.approval":
				asked, _ = ev.Payload["diff"].(string)
				sd.Decide(true)
			case "llm.response":
				answered, _ = ev.Payload["text"].(string)
			}
		case req := <-br.apply:
			if req.base != doc {
				t.Error("the change was drafted from a document other than the one on screen")
			}
			doc, applied = req.doc, true
			req.done <- nil
		case <-deadline:
			t.Fatal("the turn never finished")
		}
	}
	if !strings.Contains(asked, "input_gap_extra") {
		t.Errorf("the user was asked about %q; the question must carry the diff of the interface", asked)
	}
	if !applied || indexOf(childIDs(doc), "input_gap_extra") != indexOf(childIDs(doc), "prompt")+1 {
		t.Fatalf("applied=%v children=%v; the blank line must land right under the input bar", applied, childIDs(doc))
	}
	model.mu.Lock()
	defer model.mu.Unlock()
	if got := strings.Join(model.offered[0], ","); !strings.Contains(got, "ui_guide,ui_edit") {
		t.Errorf("offered %q; the model must be lent the interface tools on every turn", got)
	}
	if strings.Contains(model.systems[0], "below_input") {
		t.Error("the interface guide is in the standing prompt; it must be paid for only by the turn that reads it")
	}
	if len(model.results) < 2 || !strings.Contains(model.results[0], `"id": "input_gap_bottom"`) || !strings.Contains(model.results[1], "applied and saved") {
		t.Errorf("tool results = %q; the guide must carry the live document and the edit must report it was applied", model.results)
	}
}

// TestTheAgentRecoloursItsRepliesThroughTheRealCore is the second report, fixed:
// "change the blue words you send me to purple". The model is told it runs inside
// arxi-tui, reads the guide (which names markdown.code as cyan), proposes colours, the
// user allows it, and the loop receives the new colour layer.
func TestTheAgentRecoloursItsRepliesThroughTheRealCore(t *testing.T) {
	bin := buildCore(t)
	work := t.TempDir()
	t.Setenv("ARXI_SECRETS_DIR", filepath.Join(work, "secrets"))
	t.Setenv("ARXI_PROVIDERS_DIR", filepath.Join(work, "providers"))
	t.Setenv("ARXI_BIN", bin)
	t.Setenv("HOME", work)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(work, "cfg"))
	t.Setenv(configDirEnv, filepath.Join(work, "arxi"))
	t.Chdir(work)

	model := &scriptedInterfaceModel{edit: `{"colors":{"markdown.code":"fg=magenta","markdown.link":"fg=magenta underline"},"summary":"Purple instead of blue"}`}
	srv := model.serve(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	drv, evCh, err := openServeDriver(ctx, bin)
	if err != nil {
		t.Fatalf("openServeDriver: %v", err)
	}
	defer drv.Close()
	sd := drv.(*serveDriver)
	if out := runHubWork(ctx, sd.Hub(), hubWork{Op: opAdd, Name: "fake", BaseURL: srv.URL + "/v1", Key: "sk-test-0123456789abcdefghijklmnop"}); !out.ok {
		t.Fatalf("adding the provider: %q", out.notice)
	}
	if out := runHubWork(ctx, sd.Hub(), hubWork{Op: opDefault, Ref: "fake/m1", Close: true}); !out.ok {
		t.Fatalf("choosing the model: %q", out.notice)
	}
	br := sd.InterfaceBridge()
	br.publish(builtinDoc(t), theme.SOBRIA(), nil)
	if err := sd.SubmitPrompt(ctx, "en la tui, cambia los colores de las palabras en azul que me envias a morado"); err != nil {
		t.Fatal(err)
	}
	var asked, answered string
	var colors userTokens
	deadline := time.After(30 * time.Second)
	for answered == "" {
		select {
		case ev := <-evCh:
			switch ev.Type {
			case "chat.error":
				t.Fatalf("chat failed: %v", ev.Payload["text"])
			case "chat.approval":
				asked, _ = ev.Payload["diff"].(string)
				sd.Decide(true)
			case "llm.response":
				answered, _ = ev.Payload["text"].(string)
			}
		case req := <-br.apply:
			colors = req.colors
			req.done <- nil
		case <-deadline:
			t.Fatal("the turn never finished")
		}
	}
	if !strings.Contains(asked, "- markdown.code: fg=cyan") || !strings.Contains(asked, "+ markdown.code: fg=magenta") {
		t.Errorf("the user was asked about %q; a colour change must show what it was and what it becomes", asked)
	}
	if got := withUserTokens(theme.SOBRIA(), colors).Resolve("markdown.code").String(); got != "fg=magenta" {
		t.Fatalf("after the change markdown.code is %q; the loop must receive the purple layer", got)
	}
	model.mu.Lock()
	defer model.mu.Unlock()
	if !strings.Contains(model.systems[0], "You are running inside arxi-tui") {
		t.Error("the standing prompt does not say the model runs inside arxi-tui; it searched the project instead in the real report")
	}
	if len(model.results) < 1 || !strings.Contains(model.results[0], "markdown.code = fg=cyan") {
		t.Errorf("the guide the model read did not name markdown.code as cyan: %.200q", model.results)
	}
}
