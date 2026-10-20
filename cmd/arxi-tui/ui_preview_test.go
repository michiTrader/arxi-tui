package main

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/michiTrader/arxi_tui/internal/driver"
)

func (r *bridgeRig) preview(t *testing.T, args string) driver.ClientToolResult {
	t.Helper()
	return r.b.call(context.Background(), driver.ClientToolCall{CallID: "p1", Name: uiToolPreview, Arguments: json.RawMessage(args)}, nil)
}

// pictureOf is the picture without the first row, which says what was asked ("2 shells").
func pictureOf(res driver.ClientToolResult) string {
	_, rest, _ := strings.Cut(res.Text, "\n")
	return rest
}

func TestPreviewDrawsTheScreenTheUserSees(t *testing.T) {
	r := newBridgeRig(t, "ask")
	res := r.preview(t, `{"input":"hola mundo","chat":[{"role":"user","text":"primera pregunta"},{"role":"assistant","text":"respuesta larga"}]}`)
	if !res.OK {
		t.Fatalf("ui_preview failed: %+v", res)
	}
	for _, want := range []string{"hola mundo", "primera pregunta", "respuesta larga", "screen 80x24"} {
		if !strings.Contains(res.Text, want) {
			t.Errorf("the preview lacks %q:\n%s\nConsequence: the model judges a layout it cannot see and says \"done\" about a screen that is wrong.\nRemedy: draw the live document with the state the call describes.", want, res.Text)
		}
	}
}

func TestPreviewShowsTheStateTheCallDescribes(t *testing.T) {
	r := newBridgeRig(t, "ask")
	idle := r.preview(t, `{}`)
	busy := r.preview(t, `{"working":true,"shells":2}`)
	if strings.Contains(pictureOf(idle), "shell") {
		t.Fatalf("an idle screen shows shells:\n%s", idle.Text)
	}
	if !strings.Contains(pictureOf(busy), "shell 1 · shell 2") {
		t.Errorf("the preview with two shells lacks their list:\n%s\nConsequence: the model cannot check the shells button or list it is asked to place.\nRemedy: put the shells into the state the renderer reads, as the loop does.", busy.Text)
	}
	if !strings.Contains(pictureOf(busy), "Thinking") {
		t.Errorf("a turn in flight shows no Thinking line:\n%s", busy.Text)
	}
}

func TestPreviewHonoursTheWidthAndWarnsOfOverflow(t *testing.T) {
	r := newBridgeRig(t, "ask")
	res := r.preview(t, `{"width":30,"height":12}`)
	if !res.OK || !strings.Contains(res.Text, "screen 30x12") {
		t.Fatalf("width and height were not honoured: %+v", res)
	}
	for i, l := range strings.Split(pictureOf(res), "\n") {
		if strings.HasPrefix(l, "WARNING") {
			continue
		}
		if n := len([]rune(l)); n > 30 {
			t.Errorf("row %d is %d wide in a 30-wide preview: %q", i+1, n, l)
		}
	}
}

func TestPreviewRefusesWhatItCannotDrawAndSaysWhy(t *testing.T) {
	r := newBridgeRig(t, "ask")
	for name, c := range map[string]struct{ args, want string }{
		"width":  {`{"width":5}`, "width must be"},
		"height": {`{"height":500}`, "height"},
		"shells": {`{"shells":99}`, "shells must be"},
		"role":   {`{"chat":[{"role":"bot","text":"x"}]}`, `"bot"`},
		"json":   {`{"width":"wide"}`, "ui_preview takes"},
	} {
		res := r.preview(t, c.args)
		if res.OK || !strings.Contains(res.Text, c.want) {
			t.Errorf("%s: got %+v; want a refusal naming %q.\nConsequence: a silent clamp makes the model trust a picture that is not the one it asked for.\nRemedy: refuse, and say the range.", name, res, c.want)
		}
	}
}

func TestPreviewOfAProposalShowsItWithoutApplyingOrAsking(t *testing.T) {
	t.Setenv(configDirEnv, t.TempDir())
	r := newBridgeRig(t, "ask")
	before := r.preview(t, `{}`)
	res := r.preview(t, `{"change":`+extraGap+`}`)
	if !res.OK {
		t.Fatalf("the proposal could not be previewed: %+v", res)
	}
	if !strings.Contains(res.Text, "AS IT WOULD BE") {
		t.Errorf("the preview does not say it shows a proposal:\n%s\nConsequence: the model reports a change it never made.\nRemedy: label it.", res.Text)
	}
	if pictureOf(res) == pictureOf(before) {
		t.Errorf("the extra row did not change the picture:\n%s", res.Text)
	}
	if len(r.asked) != 0 {
		t.Errorf("a preview asked the user: %+v", r.asked)
	}
	if after := r.preview(t, `{}`); after.Text != before.Text {
		t.Errorf("a preview of a proposal changed the screen on screen.\nConsequence: looking would be the same as doing, and the user's approval would mean nothing.\nRemedy: draw the draft and drop it.")
	}
	select {
	case req := <-r.b.apply:
		t.Fatalf("a preview handed a change to the loop: %+v", req)
	default:
	}
}

func TestPreviewOfAnInvalidProposalNamesTheFaultAndChangesNothing(t *testing.T) {
	r := newBridgeRig(t, "ask")
	res := r.preview(t, `{"change":{"scene":"{not json"}}`)
	if res.OK || !strings.Contains(res.Text, "Nothing was changed") {
		t.Fatalf("got %+v; want a refusal that says nothing changed", res)
	}
	// Looking again and again is not the model repeating a refused edit.
	for i := 0; i < 4; i++ {
		res = r.preview(t, `{"change":{"scene":"{not json"}}`)
	}
	r.b.mu.Lock()
	counted := r.b.refusals
	r.b.mu.Unlock()
	if counted != 0 || strings.Contains(res.Text, "Stop calling ui_edit") {
		t.Errorf("the preview used up the refusal counter of ui_edit.\nConsequence: a model that looks is told to stop trying to edit.\nRemedy: previews do not count refusals.")
	}
}

func TestPreviewWorksInPlanMode(t *testing.T) {
	r := newBridgeRig(t, "plan")
	if res := r.preview(t, `{"change":`+extraGap+`}`); !res.OK {
		t.Errorf("a preview was refused in plan mode: %+v\nConsequence: plan mode, where the model only talks about changes, cannot show them.\nRemedy: a preview applies nothing, so it is allowed.", res)
	}
}

func TestPreviewIsOneOfTheInterfaceToolsAndStaysShort(t *testing.T) {
	r := newBridgeRig(t, "ask")
	var names []string
	for _, d := range r.b.definitions() {
		names = append(names, d.Name)
	}
	if strings.Join(names, ",") != "ui_guide,ui_edit,ui_preview" {
		t.Errorf("tools = %v", names)
	}
	defs, _ := json.Marshal(r.b.definitions())
	if len(defs) > 2500 {
		t.Errorf("the tool definitions are %d bytes and ride with every question.\nConsequence: a plain \"hola\" pays for the interface feature.\nRemedy: shorten the descriptions.", len(defs))
	}
}

func TestPreviewIsNamedInTheChat(t *testing.T) {
	found := false
	for _, n := range chatToolNames {
		found = found || n == uiToolPreview
	}
	if !found {
		t.Errorf("ui_preview is not in chatToolNames.\nConsequence: its line in the chat shows the raw tool name, and the user cannot reword it.\nRemedy: add it there and to toolTitles.")
	}
	if toolDigestEntry(driver.ToolCall{Name: uiToolPreview, OK: true}) != "" {
		t.Errorf("looking at the screen is put in the history digest of what the agent did; it is reading, not doing")
	}
}

func TestTheGuideTellsTheModelToLookBeforeSayingDone(t *testing.T) {
	r := newBridgeRig(t, "ask")
	res := r.b.call(context.Background(), driver.ClientToolCall{Name: uiToolGuide}, nil)
	if !strings.Contains(res.Text, "ui_preview") {
		t.Errorf("the guide does not mention ui_preview.\nConsequence: the tool is lent but the model has no reason to call it, and keeps saying done unseen.\nRemedy: say when to use it in the LAYOUT part of uiGuide.")
	}
}
