package main

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/michiTrader/arxi_tui/internal/driver"
	"github.com/michiTrader/arxi_tui/internal/fold"
	"github.com/michiTrader/arxi_tui/internal/term"
)

func TestTheEmbeddedHubSceneMatchesTheFixture(t *testing.T) {
	want, err := os.ReadFile("../../testdata/HUB.json")
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(factoryHub) != strings.TrimSpace(string(want)) {
		t.Error("the embedded hub scene drifted from testdata/HUB.json")
	}
	if _, err := loadHubScene(); err != nil {
		t.Errorf("loadHubScene: %v", err)
	}
}

func typeInto(h *providerHub, s string) {
	for _, r := range s {
		routeHubKey(h, term.Key{Type: term.KeyRunes, Runes: []rune{r}})
	}
}

func TestTypingOtherFindsTheOtherRow(t *testing.T) {
	for _, word := range []string{"other", "otro"} {
		h, _ := newHub(hubData{}, hubOpenProviders)
		h.setLevel(lvCatalog)
		typeInto(h, word)
		items := h.items()
		if got := items[h.sel].id; got != "other" {
			t.Errorf("typing %q highlights %q; want the Other… row", word, got)
		}
	}
}

func TestTheKeyIsOnlyEverShownAsBullets(t *testing.T) {
	h, _ := newHub(hubData{}, hubOpenProviders)
	e, _ := catalogByID("openrouter")
	h.openForm(newAddForm(e))
	const key = "sk-secret-123456"
	h.paste(key)
	var st fold.State
	h.publish(&st)
	all := st.UserInput + st.HubTitle + st.HubHint + st.HubDetail
	for _, r := range st.HubRows {
		all += r.Label + r.Status
	}
	if strings.Contains(all, key) || strings.Contains(all, "secret") {
		t.Errorf("the key reached the published state: %q", all)
	}
}

type fakeChat struct {
	hello *driver.Hello
	res   *driver.ChatSendResult
	err   error
}

func (f fakeChat) Hello() *driver.Hello { return f.hello }
func (f fakeChat) SubmitChatSend(ctx context.Context, p driver.ChatSendParams) (*driver.ChatSendResult, error) {
	return f.res, f.err
}

func TestChatFailureIsReportedNotSwallowed(t *testing.T) {
	out := make(chan fold.Event, 16)
	c := newChatSession(fakeChat{hello: &driver.Hello{Implemented: []string{"chat.send"}},
		err: &driver.Refusal{Code: "failed", Message: "no provider is set up yet: add one with /provider"}}, out)
	if err := c.send(context.Background(), "hi"); err != nil {
		t.Fatal(err)
	}
	// The failure is a line of the conversation (a chat.error event), not a banner.
	deadline := time.After(2 * time.Second)
	for {
		select {
		case ev := <-out:
			if ev.Type != "chat.error" {
				continue
			}
			if m, _ := ev.Payload["text"].(string); !strings.Contains(m, "/provider") {
				t.Errorf("error text %q", m)
			}
			st := fold.Fold([]fold.Event{ev})
			if len(st.History) != 1 || st.History[0].Role != "error" {
				t.Errorf("the error did not land in the chat history: %+v", st.History)
			}
			return
		case <-deadline:
			t.Fatal("no error reached the conversation")
		}
	}
}

func TestChatAnswerBecomesEvents(t *testing.T) {
	out := make(chan fold.Event, 16)
	c := newChatSession(fakeChat{hello: &driver.Hello{Implemented: []string{"chat.send"}},
		res: &driver.ChatSendResult{Text: "ok", Model: "m", Provider: "p", InputTokens: 1, OutputTokens: 2}}, out)
	if err := c.send(context.Background(), "hi"); err != nil {
		t.Fatal(err)
	}
	var types []string
	for len(types) < 4 {
		select {
		case ev := <-out:
			types = append(types, ev.Type)
			if ev.Type == "llm.response" && ev.Payload["model"] != "p/m" {
				t.Errorf("model %v", ev.Payload["model"])
			}
		case <-time.After(2 * time.Second):
			t.Fatalf("events so far: %v", types)
		}
	}
}

func TestChatRefusesAnOldCoreWithTheRemedy(t *testing.T) {
	c := newChatSession(fakeChat{hello: &driver.Hello{}}, make(chan fold.Event, 1))
	err := c.send(context.Background(), "hi")
	if err == nil || !strings.Contains(err.Error(), "rebuild") {
		t.Errorf("err = %v", err)
	}
	if errors.Is(err, errChatBusy) {
		t.Error("wrong error")
	}
}

func TestHubCommands(t *testing.T) {
	for line, want := range map[string]hubOpen{"/provider": hubOpenProviders, "/providers": hubOpenProviders, "/login": hubOpenProviders} {
		if got, ok := hubCommand(line); !ok || got != want {
			t.Errorf("%s -> %v %v", line, got, ok)
		}
	}
	for _, line := range []string{"/model", "/models", "/model deepseek"} {
		if _, ok := hubCommand(line); ok {
			t.Errorf("%s must not open the provider hub", line)
		}
	}
	if _, ok := hubCommand("hello"); ok {
		t.Error("plain text is not a hub command")
	}
}
