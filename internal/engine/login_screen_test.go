package engine

import (
	"os"
	"strings"
	"testing"

	"github.com/michiTrader/arxi_tui/internal/fold"
	"github.com/michiTrader/arxi_tui/internal/scene"
)

// loginState is the host view state the /login wizard publishes on the provider
// step: a title, a window of rows with a status column, one highlighted row, a pager
// and a hint. The key never appears here -- the wizard publishes bullets only.
func loginState(sel int) fold.State {
	rows := []fold.LoginRow{
		{Label: "Anthropic (Claude)", Status: "• unconfigured"},
		{Label: "OpenAI", Status: "✓ env: OPENAI_API_KEY"},
		{Label: "OpenRouter", Status: "✓ key stored"},
		{Label: "Local server (Ollama)", Status: "• unconfigured"},
	}
	for i := range rows {
		rows[i].Selected = i == sel
	}
	return fold.State{
		LoginTitle: "Select provider to configure:",
		LoginRows:  rows,
		LoginPager: "(2/17)",
		LoginHint:  "↑↓ navigate · enter select · escape/ctrl+c cancel",
	}
}

func loginDoc(t *testing.T) *scene.Document {
	t.Helper()
	data, err := os.ReadFile("../../testdata/LOGIN.json")
	if err != nil {
		t.Fatalf("read LOGIN.json: %v", err)
	}
	doc, err := scene.ParseDocument(data)
	if err != nil {
		t.Fatalf("ParseDocument: %v", err)
	}
	if err := doc.Validate(); err != nil {
		t.Fatalf("scene validation: %v", err)
	}
	return doc
}

func TestLoginSceneRenders(t *testing.T) {
	doc := loginDoc(t)
	r := Renderer{Width: 80, Height: 24}
	got := r.RenderFrame(doc, loginState(1)).Plain()
	for _, want := range []string{
		"Select provider to configure:", "Anthropic (Claude)", "✓ env: OPENAI_API_KEY",
		"• unconfigured", "(2/17)", "escape/ctrl+c cancel",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("login screen lacks %q; got:\n%s", want, got)
		}
	}
	if strings.Contains(got, "[…]") || strings.Contains(got, "UNKNOWN NODE TYPE") {
		t.Errorf("login screen rendered a placeholder or unknown node:\n%s", got)
	}
}

// TestLoginHighlightFollowsTheSelection: the gutter sits on the selected row and on
// no other, and moving the selection moves it.
func TestLoginHighlightFollowsTheSelection(t *testing.T) {
	doc := loginDoc(t)
	r := Renderer{Width: 80, Height: 24}
	for sel, want := range map[int]string{0: "Anthropic", 2: "OpenRouter"} {
		var marked []string
		for _, line := range strings.Split(r.RenderFrame(doc, loginState(sel)).Plain(), "\n") {
			if strings.HasPrefix(line, "> ") {
				marked = append(marked, line)
			}
		}
		if len(marked) != 1 || !strings.Contains(marked[0], want) {
			t.Fatalf("selection %d marked %q; want exactly the %s row", sel, marked, want)
		}
	}
}

// TestLoginShowsBulletsNotKeys: the form step shows a masked value in the status
// column as-is; the renderer adds nothing of its own.
func TestLoginShowsBulletsNotKeys(t *testing.T) {
	doc := loginDoc(t)
	st := fold.State{
		LoginTitle: "Configure OpenRouter:",
		LoginRows:  []fold.LoginRow{{Label: "API key", Status: "••••••••", Selected: true}},
		LoginHint:  "the key is never shown",
	}
	r := Renderer{Width: 80, Height: 24}
	got := r.RenderFrame(doc, st).Plain()
	if !strings.Contains(got, "••••••••") {
		t.Errorf("masked value missing:\n%s", got)
	}
}

func TestLoginSceneMatchesGolden(t *testing.T) {
	doc := loginDoc(t)
	r := Renderer{Width: 80, Height: 24}
	f := r.RenderFrame(doc, loginState(1))
	for name, got := range map[string]string{"LOGIN.frame": f.Plain(), "LOGIN.styled": f.Styled()} {
		path := "../../testdata/" + name
		if os.Getenv("UPDATE_GOLDEN") == "1" {
			if err := os.WriteFile(path, []byte(got), 0644); err != nil {
				t.Fatalf("write golden: %v", err)
			}
			continue
		}
		want, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read golden %s: %v", path, err)
		}
		if got != string(want) {
			t.Errorf("%s does not match golden:\n--- got ---\n%s\n--- want ---\n%s", name, got, want)
		}
	}
}
