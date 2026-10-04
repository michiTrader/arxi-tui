package engine

import (
	"os"
	"strings"
	"testing"

	"github.com/michiTrader/arxi_tui/internal/fold"
	"github.com/michiTrader/arxi_tui/internal/scene"
)

// hubState is the host view state the provider hub publishes on the model picker:
// a title, a window of rows (label + status), one highlighted row and a hint.
func hubState(sel int) fold.State {
	rows := []fold.HubRow{
		{Label: "gemini-3.1-flash-lite [google]"},
		{Label: "✓ gemini-3.6-flash [google] · default"},
		{Label: "gpt-4o [openai]"},
		{Label: "Other…"},
	}
	for i := range rows {
		rows[i].Selected = i == sel
	}
	return fold.State{
		HubTitle:  "Choose the model to chat with (type to filter):",
		HubRows:   rows,
		HubHint:   "type to filter · ↑↓ move · enter select · esc back",
		HubDetail: "3 models available. Enter makes the highlighted one the model the chat uses.",
	}
}

func hubDoc(t *testing.T) *scene.Document {
	t.Helper()
	data, err := os.ReadFile("../../testdata/HUB.json")
	if err != nil {
		t.Fatalf("read HUB.json: %v", err)
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

func TestHubSceneRenders(t *testing.T) {
	doc := hubDoc(t)
	r := Renderer{Width: 80, Height: 24}
	got := r.RenderFrame(doc, hubState(1)).Plain()
	for _, want := range []string{
		"Choose the model to chat with", "  gemini-3.1-flash-lite [google]",
		"→ ✓ gemini-3.6-flash [google] · default", "Other…", "type to filter",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("hub screen lacks %q; got:\n%s", want, got)
		}
	}
	if strings.Contains(got, "[…]") || strings.Contains(got, "UNKNOWN NODE TYPE") {
		t.Errorf("hub screen rendered a placeholder or unknown node:\n%s", got)
	}
}

// TestHubPickerIsPinnedToTheBottom: the choices hang below the input, like the
// slash menu, not above it.
func TestHubPickerIsPinnedToTheBottom(t *testing.T) {
	doc := hubDoc(t)
	r := Renderer{Width: 80, Height: 24}
	lines := strings.Split(r.RenderFrame(doc, hubState(0)).Plain(), "\n")
	input, row := -1, -1
	for i, l := range lines {
		if strings.HasPrefix(l, "┃") {
			input = i
		}
		if strings.Contains(l, "gemini-3.1-flash-lite") {
			row = i
		}
	}
	if input < 0 || row < 0 || row <= input {
		t.Fatalf("picker rows must sit below the input (input line %d, row line %d):\n%s", input, row, strings.Join(lines, "\n"))
	}
}

func TestHubMarkerFollowsTheSelection(t *testing.T) {
	doc := hubDoc(t)
	r := Renderer{Width: 80, Height: 24}
	for sel, want := range map[int]string{0: "gemini-3.1-flash-lite", 2: "gpt-4o"} {
		var marked []string
		for _, line := range strings.Split(r.RenderFrame(doc, hubState(sel)).Plain(), "\n") {
			if strings.HasPrefix(line, "→ ") {
				marked = append(marked, line)
			}
		}
		if len(marked) != 1 || !strings.Contains(marked[0], want) {
			t.Fatalf("selection %d marked %q; want exactly the %s row", sel, marked, want)
		}
	}
}

func TestHubSceneMatchesGolden(t *testing.T) {
	doc := hubDoc(t)
	r := Renderer{Width: 80, Height: 24}
	f := r.RenderFrame(doc, hubState(1))
	for ext, got := range map[string]string{"frame": f.Plain(), "styled": f.Styled()} {
		golden := "../../testdata/HUB." + ext
		if os.Getenv("UPDATE_GOLDEN") == "1" {
			if err := os.WriteFile(golden, []byte(got), 0o644); err != nil {
				t.Fatalf("write golden: %v", err)
			}
			continue
		}
		want, err := os.ReadFile(golden)
		if err != nil {
			t.Fatalf("read golden (run UPDATE_GOLDEN=1 to create): %v", err)
		}
		if got != string(want) {
			t.Errorf("HUB %s frame drifted from its golden.\ngot:\n%s\nwant:\n%s\nremedy: UPDATE_GOLDEN=1 go test ./internal/engine/", ext, got, string(want))
		}
	}
}
