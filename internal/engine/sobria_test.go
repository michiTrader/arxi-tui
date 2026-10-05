package engine

import (
	"os"
	"strings"
	"testing"

	"github.com/michiTrader/arxi_tui/internal/fold"
	"github.com/michiTrader/arxi_tui/internal/scene"
)

// TestSobriaSceneRenders verifies the sobria golden scene renders all its
// nodes correctly with the Phase 0 mock events: header, transcript, input,
// status bar. This is the Phase 1 golden for Scene 2.
func TestSobriaSceneRenders(t *testing.T) {
	data, err := os.ReadFile("../../testdata/SOBRIA.json")
	if err != nil {
		t.Fatalf("read SOBRIA.json: %v", err)
	}
	doc, err := scene.ParseDocument(data)
	if err != nil {
		t.Fatalf("ParseDocument: %v", err)
	}
	if err := doc.Validate(); err != nil {
		t.Fatalf("scene validation: %v", err)
	}

	events := []fold.Event{
		{Type: "run.started", Seq: 1, Payload: map[string]any{
			"run_id": "r1", "actor": "user", "budget_usd": 10.0,
			"max_turns": 10, "simulated": false,
		}},
		{Type: "agent.activated", Seq: 2, Payload: map[string]any{"agent": "backend"}},
		{Type: "llm.response", Seq: 3, Payload: map[string]any{
			"agent": "backend", "model": "openai/gpt-4o",
			"text":      "Hola! How can I help you today?",
			"tokens_in": 25, "tokens_out": 35, "cost_usd": 0.001,
		}},
		{Type: "agent.turn_done", Seq: 4, Payload: map[string]any{"agent": "backend"}},
	}
	state := fold.Fold(events)
	state.HostMode = "ask" // host view state: the fold never sets it

	r := Renderer{Width: 80, Height: 24}
	f := r.RenderFrame(doc, state)
	got := f.Plain()

	if strings.Contains(got, "UNKNOWN NODE TYPE") {
		t.Errorf("sobria scene rendered unknown node type:\n%s", got)
	}

	// The header must be the first line.
	if !strings.HasPrefix(got, "Δr×i v0.1.0") {
		t.Errorf("expected header starting with 'Δr×i v0.1.0', got:\n%s", got)
	}

	// The transcript must contain the response.
	if !strings.Contains(got, "Hola! How can I help you today?") {
		t.Errorf("expected transcript to contain the assistant response; got:\n%s", got)
	}

	// The input prompt shows the sobria "┃ " bar. The placeholder text was
	// removed — the empty line is the bar and nothing else — so "ask anything"
	// must not appear anywhere, and the bar itself must.
	if !strings.Contains(got, "┃ ") {
		t.Errorf("expected sobria input prefix '┃ '; got:\n%s", got)
	}
	if strings.Contains(got, "ask anything") {
		t.Errorf("the placeholder text was removed but 'ask anything' still renders; got:\n%s", got)
	}

	// The status bar shows the mode, the model, and (host view state, empty here so
	// absent) the thinking level and cwd. The old spark glyph is gone for good.
	if !strings.Contains(got, "ask · openai/gpt-4o") || strings.Contains(got, "⚡︎") {
		t.Errorf("expected status bar 'ask · openai/gpt-4o' and no spark; got:\n%s", got)
	}

	// The thinking marquee must NOT render when agent.working is false.
	if strings.Contains(got, "Thinking") {
		t.Errorf("thinking marquee should not render when agent is idle; got:\n%s", got)
	}
}

// TestSobriaSceneMarqueeGolden verifies the thinking marquee renders with its
// prefix and suffix when agent.working is true.
func TestSobriaSceneMarqueeGolden(t *testing.T) {
	data, err := os.ReadFile("../../testdata/SOBRIA.json")
	if err != nil {
		t.Fatalf("read SOBRIA.json: %v", err)
	}
	doc, err := scene.ParseDocument(data)
	if err != nil {
		t.Fatalf("ParseDocument: %v", err)
	}

	state := fold.State{
		History: []fold.ChatLine{
			{Role: "user", Text: "hola"},
		},
		ThinkingText: "Looking at the code...",
		AgentWorking: true,
		AgentMode:    "live",
		ModelName:    "openai/gpt-4o",
		UsageIn:      25,
		UsageOut:     35,
		UserInput:    "",
		// The loop builds the label from the wall clock; the fold only carries it.
		HostThinking: "• Thinking (3s) ",
	}
	state.DeriveUsageDelta()

	r := Renderer{Width: 80, Height: 10}
	f := r.RenderFrame(doc, state)
	got := f.Plain()

	// The marquee shows the label and the thinking words after it.
	if !strings.Contains(got, "• Thinking (3s) Looking at the code...") {
		t.Errorf("expected marquee '• Thinking (3s) Looking at the code...'; got:\n%s", got)
	}
}

// TestSobriaSceneShowsRunActor verifies the status row draws the run's actor
// label when host.run.actor is set, and draws nothing for it when it is empty.
// The two halves are one test on purpose: the empty half is the counterfactual
// that proves the when gate, not the bind, is what keeps the default golden
// still. Without the gate the label node would render an empty string and its
// " · " separator would appear at boot, moving the frame the default golden
// pins; with it, the actor is present only while a run is followed (M2).
func TestSobriaSceneShowsRunActor(t *testing.T) {
	data, err := os.ReadFile("../../testdata/SOBRIA.json")
	if err != nil {
		t.Fatalf("read SOBRIA.json: %v", err)
	}
	doc, err := scene.ParseDocument(data)
	if err != nil {
		t.Fatalf("ParseDocument: %v", err)
	}
	if err := doc.Validate(); err != nil {
		t.Fatalf("scene validation: %v", err)
	}

	// With an actor: the label and its separator render before the mode/model
	// chain, so the bottom bar reads "<actor> · live · openai/gpt-4o · ⚡︎".
	withActor := fold.State{
		HostMode:     "ask",
		ModelName:    "openai/gpt-4o",
		StatusActive: "true",
		RunActor:     "planner",
	}
	r := Renderer{Width: 80, Height: 24}
	got := r.RenderFrame(doc, withActor).Plain()
	if !strings.Contains(got, "planner · ask · openai/gpt-4o") {
		t.Errorf("expected status bar to lead with the actor 'planner · ask · openai/gpt-4o'; got:\n%s", got)
	}

	// Counterfactual: the same state with no actor must not draw the label or its
	// separator, so the row is byte-for-byte what it was before the actor node
	// existed. A leading " · " here would mean the gate leaked and the default
	// golden is unsafe.
	noActor := withActor
	noActor.RunActor = ""
	gotNone := r.RenderFrame(doc, noActor).Plain()
	if strings.Contains(gotNone, "planner") {
		t.Errorf("actor label rendered with an empty host.run.actor; got:\n%s", gotNone)
	}
	if strings.Contains(gotNone, " · live") {
		t.Errorf("the actor separator ' · ' rendered before 'live' with no actor set, so the when gate leaked; got:\n%s", gotNone)
	}
}

// TestSobriaSceneStyledGolden compares the rendered frame with token annotations
// against the golden file. UPDATE_GOLDEN=1 regenerates the fixture.
func TestSobriaSceneStyledGolden(t *testing.T) {
	data, err := os.ReadFile("../../testdata/SOBRIA.json")
	if err != nil {
		t.Fatalf("read SOBRIA.json: %v", err)
	}
	doc, err := scene.ParseDocument(data)
	if err != nil {
		t.Fatalf("ParseDocument: %v", err)
	}

	events := []fold.Event{
		{Type: "run.started", Seq: 1, Payload: map[string]any{
			"run_id": "r1", "actor": "user", "budget_usd": 10.0,
			"max_turns": 10, "simulated": false,
		}},
		{Type: "agent.activated", Seq: 2, Payload: map[string]any{"agent": "backend"}},
		{Type: "llm.response", Seq: 3, Payload: map[string]any{
			"agent": "backend", "model": "openai/gpt-4o",
			"text":      "Hola! How can I help you today?",
			"tokens_in": 25, "tokens_out": 35, "cost_usd": 0.001,
		}},
		{Type: "agent.turn_done", Seq: 4, Payload: map[string]any{"agent": "backend"}},
	}
	state := fold.Fold(events)

	r := Renderer{Width: 80, Height: 24}
	f := r.RenderFrame(doc, state)
	got := f.Styled()

	goldenPath := "../../testdata/SOBRIA.styled"
	if os.Getenv("UPDATE_GOLDEN") == "1" {
		if err := os.WriteFile(goldenPath, []byte(got), 0644); err != nil {
			t.Fatalf("write golden: %v", err)
		}
		return
	}

	want, err := os.ReadFile(goldenPath)
	if err != nil {
		t.Fatalf("read golden %s: %v", goldenPath, err)
	}
	if got != string(want) {
		t.Errorf("sobria scene styled output does not match golden:\n--- got ---\n%s\n--- want ---\n%s", got, string(want))
	}
}

// TestSobriaSceneMatchesGolden compares the rendered frame against the
// golden file. UPDATE_GOLDEN=1 regenerates the fixture.
func TestSobriaSceneMatchesGolden(t *testing.T) {
	data, err := os.ReadFile("../../testdata/SOBRIA.json")
	if err != nil {
		t.Fatalf("read SOBRIA.json: %v", err)
	}
	doc, err := scene.ParseDocument(data)
	if err != nil {
		t.Fatalf("ParseDocument: %v", err)
	}

	events := []fold.Event{
		{Type: "run.started", Seq: 1, Payload: map[string]any{
			"run_id": "r1", "actor": "user", "budget_usd": 10.0,
			"max_turns": 10, "simulated": false,
		}},
		{Type: "agent.activated", Seq: 2, Payload: map[string]any{"agent": "backend"}},
		{Type: "llm.response", Seq: 3, Payload: map[string]any{
			"agent": "backend", "model": "openai/gpt-4o",
			"text":      "Hola! How can I help you today?",
			"tokens_in": 25, "tokens_out": 35, "cost_usd": 0.001,
		}},
		{Type: "agent.turn_done", Seq: 4, Payload: map[string]any{"agent": "backend"}},
	}
	state := fold.Fold(events)
	state.HostMode = "ask"

	r := Renderer{Width: 80, Height: 24}
	f := r.RenderFrame(doc, state)
	got := f.Plain()

	goldenPath := "../../testdata/SOBRIA.frame"
	if os.Getenv("UPDATE_GOLDEN") == "1" {
		if err := os.WriteFile(goldenPath, []byte(got), 0644); err != nil {
			t.Fatalf("write golden: %v", err)
		}
		return
	}

	want, err := os.ReadFile(goldenPath)
	if err != nil {
		t.Fatalf("read golden %s: %v", goldenPath, err)
	}
	if got != string(want) {
		t.Errorf("sobria scene does not match golden:\n--- got ---\n%s\n--- want ---\n%s", got, string(want))
	}
}

// The first row is the product mark over a blank row; only the four letters of
// "Δr×i" carry colour (one token each, forming the orange-to-yellow gradient),
// and the rest of the row stays dim.
func TestSobriaBannerGradientAndGap(t *testing.T) {
	data, err := os.ReadFile("../../testdata/SOBRIA.json")
	if err != nil {
		t.Fatal(err)
	}
	doc, err := scene.ParseDocument(data)
	if err != nil {
		t.Fatal(err)
	}
	r := Renderer{Width: 80, Height: 24}
	f := r.RenderFrame(doc, fold.Fold(nil))
	lines := strings.Split(f.Plain(), "\n")
	if !strings.HasPrefix(lines[0], "Δr×i v0.1.0 · Run /help for commands") {
		t.Errorf("banner row = %q", lines[0])
	}
	if strings.TrimSpace(lines[1]) != "" {
		t.Errorf("a blank row must follow the banner, got %q", lines[1])
	}
	styles := map[string]string{}
	for _, sp := range f.Live[0] {
		styles[sp.Text] = sp.Style
	}
	for text, want := range map[string]string{"Δ": "brand.1", "r": "brand.2", "×": "brand.3", "i": "brand.4"} {
		if styles[text] != want {
			t.Errorf("letter %q styled %q, want %q", text, styles[text], want)
		}
	}
	if styles[" v0.1.0 · Run /help for commands"] != "dim" {
		t.Errorf("the tagline must stay dim, got %q", styles[" v0.1.0 · Run /help for commands"])
	}
}

// The bottom bar carries the agent mode, the thinking level and the cwd.
func TestSobriaStatusShowsEffortAndCwd(t *testing.T) {
	data, _ := os.ReadFile("../../testdata/SOBRIA.json")
	doc, err := scene.ParseDocument(data)
	if err != nil {
		t.Fatal(err)
	}
	st := fold.State{HostMode: "ask", ModelName: "p/m", StatusActive: "true", HostEffort: "high", HostCwd: "~/proj"}
	r := Renderer{Width: 80, Height: 24}
	got := r.RenderFrame(doc, st).Plain()
	if !strings.Contains(got, "ask · p/m · high · ~/proj") {
		t.Errorf("status bar wrong:\n%s", got)
	}
	// With the menu open the bar must not also show them.
	st.StatusActive, st.HostEffort, st.HostCwd, st.HostMode = "false", "", "", ""
	if got := r.RenderFrame(doc, st).Plain(); strings.Contains(got, "~/proj") || strings.Contains(got, "ask ·") {
		t.Errorf("cwd leaked while the menu hides the bar:\n%s", got)
	}
}
