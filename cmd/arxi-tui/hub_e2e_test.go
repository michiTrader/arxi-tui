package main

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/michiTrader/arxi_tui/internal/fold"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
)

// buildCore compiles the vendored core (core/ is a nested module).
func buildCore(t *testing.T) string {
	t.Helper()
	if testing.Short() {
		t.Skip("builds the core; skipped with -short")
	}
	name := "arxi"
	if runtime.GOOS == "windows" {
		name += ".exe" // Windows will not execute a file without its extension
	}
	bin := filepath.Join(t.TempDir(), name)
	cmd := exec.Command("go", "build", "-o", bin, "./cmd/arxi")
	cmd.Dir = "../../core"
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("building the core: %v\n%s", err, out)
	}
	return bin
}

// fakeLLM is a stand-in for an OpenAI-compatible service: it lists two models and
// answers chat completions, and it records the Authorization header it was sent.
func fakeLLM(t *testing.T, wantKey string) (*httptest.Server, *[]string) {
	t.Helper()
	var mu sync.Mutex
	var bad []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer "+wantKey {
			mu.Lock()
			bad = append(bad, r.URL.Path+" sent "+got)
			mu.Unlock()
			http.Error(w, `{"error":{"message":"bad key"}}`, http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasSuffix(r.URL.Path, "/models"):
			fmt.Fprint(w, `{"data":[{"id":"fast-1"},{"id":"smart-2"}]}`)
		case strings.HasSuffix(r.URL.Path, "/chat/completions"):
			fmt.Fprint(w, `{"id":"x","model":"smart-2","choices":[{"index":0,"message":{"role":"assistant","content":"hello from the fake model"},"finish_reason":"stop"}],"usage":{"prompt_tokens":7,"completion_tokens":5}}`)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv, &bad
}

// TestProviderToChatAgainstTheRealCore is the whole user story with the real core
// process and a fake HTTP service: nothing configured -> chat says so; add a provider
// with a key -> its models are discovered -> pick one as the default -> chat answers.
// It then reads the disk: the key is in the key file (0600) and NOT in the provider
// record, and it never reached anything the screen would show.
func TestProviderToChatAgainstTheRealCore(t *testing.T) {
	bin := buildCore(t)
	work := t.TempDir()
	secrets := filepath.Join(work, "secrets")
	t.Setenv("ARXI_SECRETS_DIR", secrets)
	// Providers live in the config folder now; keep this test inside its own directory.
	t.Setenv("ARXI_PROVIDERS_DIR", filepath.Join(work, "providers"))
	t.Setenv("ARXI_BIN", bin)
	t.Setenv("HOME", work)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(work, "cfg"))
	t.Chdir(work)

	const testKey = "sk-test-0123456789abcdefghijklmnop"
	srv, bad := fakeLLM(t, testKey)

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	drv, evCh, err := openServeDriver(ctx, bin)
	if err != nil {
		t.Fatalf("openServeDriver: %v", err)
	}
	defer drv.Close()
	sd := drv.(*serveDriver)

	// 1. Nothing is set up: the chat must say so, not stay silent.
	if err := sd.SubmitPrompt(ctx, "hi"); err != nil {
		t.Fatalf("SubmitPrompt: %v", err)
	}
	{
		deadline := time.After(10 * time.Second)
		for got := false; !got; {
			select {
			case ev := <-evCh:
				if ev.Type != "chat.error" {
					continue
				}
				got = true
				if msg, _ := ev.Payload["text"].(string); !strings.Contains(msg, "provider") {
					t.Errorf("the no-provider error does not mention a provider: %q", msg)
				}
			case <-deadline:
				t.Fatal("chatting with no provider produced no message at all")
			}
		}
	}
	drainEvents(evCh)

	// 2. Add a provider: key stored, models discovered.
	hub := sd.Hub()
	out := runHubWork(ctx, hub, hubWork{Op: opAdd, Name: "fake", BaseURL: srv.URL + "/v1", Key: testKey})
	if !out.ok || !out.hasData {
		t.Fatalf("adding the provider failed: %q", out.notice)
	}
	if strings.Contains(out.notice, testKey) {
		t.Errorf("the notice carries the key: %q", out.notice)
	}
	if got := len(out.data.models); got != 2 {
		t.Fatalf("expected 2 discovered models, got %d (%q)", got, out.notice)
	}

	// 3. Pick the default; the hub then offers it as the current one.
	out = runHubWork(ctx, hub, hubWork{Op: opDefault, Ref: "fake/smart-2", Close: true})
	if !out.ok || out.data.def != "fake/smart-2" {
		t.Fatalf("setting the default failed: %q (def %q)", out.notice, out.data.def)
	}

	// 4. Chat now answers, and the status bar learns which model did.
	if err := sd.SubmitPrompt(ctx, "hello"); err != nil {
		t.Fatalf("SubmitPrompt: %v", err)
	}
	var answered, model string
	deadline := time.After(15 * time.Second)
	for answered == "" {
		select {
		case ev := <-evCh:
			if ev.Type == "chat.error" {
				t.Fatalf("chat failed: %v", ev.Payload["text"])
			}
			if ev.Type == "llm.response" {
				answered, _ = ev.Payload["text"].(string)
				model, _ = ev.Payload["model"].(string)
			}
		case <-deadline:
			t.Fatal("no answer arrived")
		}
	}
	if answered != "hello from the fake model" || model != "fake/smart-2" {
		t.Errorf("answer %q from model %q", answered, model)
	}
	if len(*bad) > 0 {
		t.Errorf("the service saw a wrong credential: %v", *bad)
	}

	// 5. The disk: the key is in the key file only.
	keyFile := filepath.Join(secrets, "fake.key")
	got, err := os.ReadFile(keyFile)
	if err != nil {
		t.Fatalf("the key file was not written: %v", err)
	}
	if strings.TrimSpace(string(got)) != testKey {
		t.Errorf("the key file holds %q", got)
	}
	if runtime.GOOS != "windows" {
		if fi, err := os.Stat(keyFile); err != nil || fi.Mode().Perm() != 0o600 {
			t.Errorf("key file mode/stat = %v / %v; want 0600", fi, err)
		}
	}
	rec, err := os.ReadFile(filepath.Join(work, "providers", "fake.json"))
	if err != nil {
		t.Fatalf("provider record missing: %v", err)
	}
	if strings.Contains(string(rec), testKey) {
		t.Error("the API key is in the provider record")
	}
}

func drainEvents(ch <-chan fold.Event) {
	for {
		select {
		case <-ch:
		case <-time.After(100 * time.Millisecond):
			return
		}
	}
}
