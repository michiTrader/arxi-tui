package main

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/michiTrader/arxi_tui/internal/defaultscene"
	"github.com/michiTrader/arxi_tui/internal/driver"
	"github.com/michiTrader/arxi_tui/internal/scene"
	"github.com/michiTrader/arxi_tui/internal/theme"
)

func builtinDoc(t *testing.T) *scene.Document {
	t.Helper()
	doc, err := scene.ParseNamed(defaultscene.Name, defaultscene.JSON)
	if err != nil {
		t.Fatal(err)
	}
	return doc
}

// The order that motivated the bridge: one more blank line between the input bar and
// the status bar, written the way ui_guide's example teaches.
const extraGap = `{"commands":["/ui add node below_input {\"id\":\"input_gap_extra\",\"type\":\"text\",\"text\":\"\"}"],"summary":"Add a blank line under the input bar"}`

// bridgeRig runs ui_edit with the loop's half played by the test: an approved change
// is applied to doc and acknowledged, as the loop's uiApplyCh case does.
type bridgeRig struct {
	b     *uiBridge
	doc   *scene.Document
	asked []driver.Approval
}

func newBridgeRig(t *testing.T, mode string) *bridgeRig {
	t.Helper()
	r := &bridgeRig{b: newUIBridge(), doc: builtinDoc(t)}
	r.b.setMode(mode)
	r.b.publish(r.doc, theme.SOBRIA(), nil, nil)
	return r
}

func (r *bridgeRig) edit(t *testing.T, args string, allow bool) driver.ClientToolResult {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	go func() {
		select {
		case req := <-r.b.apply:
			r.doc = req.doc
			req.done <- nil
		case <-ctx.Done():
		}
	}()
	return r.b.call(ctx, driver.ClientToolCall{CallID: "u1", Name: uiToolEdit, Arguments: json.RawMessage(args)},
		func(a driver.Approval) bool { r.asked = append(r.asked, a); return allow })
}

func TestTheGuideCarriesTheLiveDocumentAndEveryVocabulary(t *testing.T) {
	r := newBridgeRig(t, "ask")
	res := r.b.call(context.Background(), driver.ClientToolCall{Name: uiToolGuide}, nil)
	if !res.OK {
		t.Fatalf("ui_guide failed: %+v", res)
	}
	for _, want := range []string{`"id": "input_gap_bottom"`, "below_input", "user.input", "status.active", "menu.hint", "marquee"} {
		if !strings.Contains(res.Text, want) {
			t.Errorf("the guide lacks %q.\nConsequence: the model edits the interface by guessing, which is the failure the bridge exists to end.\nRemedy: build the guide from the live document and scene/theme vocabularies.", want)
		}
	}
	// The guide is paid for only on the turn that reads it; what rides with every
	// question is the two definitions. Measured at about 1.1k bytes; the ceiling
	// leaves room to improve the wording without letting it grow into a prompt.
	defs, _ := json.Marshal(r.b.definitions())
	if len(defs) > 1600 {
		t.Errorf("the tool definitions are %d bytes and are sent with every question.\nConsequence: a plain \"hola\" pays for the interface feature.\nRemedy: keep the definitions short; put the detail in ui_guide.", len(defs))
	}
}

func TestAskedChangeAddsTheBlankLineUnderTheInputBar(t *testing.T) {
	t.Setenv(configDirEnv, t.TempDir())
	r := newBridgeRig(t, "ask")
	res := r.edit(t, extraGap, true)
	if !res.OK {
		t.Fatalf("the change failed: %+v", res)
	}
	if len(r.asked) != 1 || r.asked[0].Name != uiToolEdit || !strings.Contains(r.asked[0].Diff, `+     {`) &&
		!strings.Contains(r.asked[0].Diff, "input_gap_extra") {
		t.Fatalf("the user was shown %+v; a change to the interface must be shown as a diff before it is made", r.asked)
	}
	ids := childIDs(r.doc)
	at := indexOf(ids, "prompt")
	if at < 0 || at+1 >= len(ids) || ids[at+1] != "input_gap_extra" {
		t.Fatalf("children = %v; the new line must sit right under the input bar", ids)
	}
}

func TestADeclinedChangeLeavesTheInterfaceAlone(t *testing.T) {
	r := newBridgeRig(t, "auto")
	before := r.doc
	res := r.edit(t, extraGap, false)
	if res.OK || r.doc != before || len(r.asked) != 1 {
		t.Fatalf("res=%+v asked=%d changed=%v; auto must still ask, and a no must change nothing", res, len(r.asked), r.doc != before)
	}
}

func TestPlanModeRefusesAndFullAccessDoesNotAsk(t *testing.T) {
	r := newBridgeRig(t, "plan")
	if res := r.edit(t, extraGap, true); res.OK || len(r.asked) != 0 {
		t.Errorf("plan mode changed or asked: %+v", res)
	}
	r = newBridgeRig(t, "full access")
	if res := r.edit(t, extraGap, false); !res.OK || len(r.asked) != 0 {
		t.Errorf("full access asked or failed: %+v asked=%d", res, len(r.asked))
	}
}

func TestARefusalNamesTheLineAndChangesNothing(t *testing.T) {
	for _, tc := range []struct{ name, args, want string }{
		{"unsigned bind", `{"commands":["/ui add node below_input {\"id\":\"x\",\"type\":\"text\",\"bind\":\"tasks.list\"}"]}`, "tasks.list"},
		{"unknown token", `{"commands":["/ui style prompt nope.token"]}`, "nope.token"},
		{"unknown key", `{"commands":["/ui add node below_input {\"id\":\"x\",\"type\":\"text\",\"colour\":\"red\"}"]}`, "colour"},
		{"input removed", `{"scene":"{\"root\":{\"type\":\"stack\",\"children\":[{\"type\":\"text\",\"text\":\"hi\"}]}}"}`, "user.input"},
		{"view-state verb", `{"commands":["/ui hide status"]}`, "for the user to type"},
		{"bad JSON", `{"scene":"{\"root\": "}`, "SOBRIA.json"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := newBridgeRig(t, "full access")
			before := r.doc
			res := r.edit(t, tc.args, true)
			if res.OK || r.doc != before {
				t.Fatalf("accepted: %+v.\nConsequence: an invalid interface reaches the screen, or a change the engine ignores is reported as made.\nRemedy: draftScene must hold the draft to the boot validator, the theme and the input rule.", res)
			}
			if !strings.Contains(res.Text, tc.want) {
				t.Errorf("the model was told %q; it must name %q so the repair turn has something to fix", res.Text, tc.want)
			}
		})
	}
}

func TestTheUsersInterfaceIsSavedBootedAndUndone(t *testing.T) {
	t.Setenv(configDirEnv, t.TempDir())
	r := newBridgeRig(t, "full access")
	if res := r.edit(t, extraGap, true); !res.OK {
		t.Fatal(res)
	}
	if err := saveInterface(r.doc, nil, nil); err != nil {
		t.Fatal(err)
	}
	doc, notice, err := resolveBootScene(builtinScene)
	if err != nil || notice != "" || indexOf(childIDs(doc), "input_gap_extra") < 0 {
		t.Fatalf("boot gave %v (notice %q, err %v); a saved interface must come back next session", childIDs(doc), notice, err)
	}
	if !persistsScene(doc) {
		t.Error("the saved interface must stay saved when it changes again")
	}
	undone, _, _, err := undoInterface()
	if err != nil || indexOf(childIDs(undone), "input_gap_extra") >= 0 {
		t.Fatalf("undo gave %v, %v; the first undo returns the built-in interface", childIDs(undone), err)
	}
	redone, _, _, err := undoInterface()
	if err != nil || indexOf(childIDs(redone), "input_gap_extra") < 0 {
		t.Fatalf("a second undo gave %v, %v; it must bring the change back", childIDs(redone), err)
	}
	if _, _, _, err := resetInterface(); err != nil {
		t.Fatal(err)
	}
	if doc, _, _ := resolveBootScene(builtinScene); doc.Name() != defaultscene.Name {
		t.Errorf("after /ui reset the boot scene is %s; want the built-in one", doc.Name())
	}
}

func TestABrokenSavedInterfaceFallsBackToTheBuiltInOneAndSaysHow(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(configDirEnv, dir)
	if err := os.WriteFile(userScenePath(), []byte(`{"root": `), 0o600); err != nil {
		t.Fatal(err)
	}
	doc, notice, err := resolveBootScene(builtinScene)
	if err != nil || doc.Name() != defaultscene.Name || !strings.Contains(notice, "/ui reset") {
		t.Fatalf("boot gave %s, notice %q, err %v.\nConsequence: one bad save would cost the user their everyday interface, with no way back from inside.\nRemedy: fall back to the built-in scene (never raw) and name /ui reset.", doc.Name(), notice, err)
	}
	if doc, _, _ := resolveBootScene(""); doc.Name() == defaultscene.Name {
		t.Error("-raw must stay the raw scene whatever is saved: the escape hatch cannot depend on the settings folder")
	}
}

func TestAChangeDraftedFromAStaleScreenIsRefusedByTheLoopRule(t *testing.T) {
	// The loop compares req.base with what is on screen. Here the screen moves on
	// between the draft and the apply, as when the user types a /ui command while
	// the question is open.
	r := newBridgeRig(t, "full access")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	go func() {
		req := <-r.b.apply
		if req.base != r.doc {
			req.done <- nil
			return
		}
		req.done <- errString("the interface changed while the question was open")
	}()
	res := r.b.call(ctx, driver.ClientToolCall{Name: uiToolEdit, Arguments: json.RawMessage(extraGap)}, nil)
	if res.OK || !strings.Contains(res.Text, "changed while the question was open") {
		t.Fatalf("res = %+v; an apply refused by the loop must reach the model as a failure", res)
	}
}

type errString string

func (e errString) Error() string { return string(e) }

func childIDs(doc *scene.Document) []string {
	var ids []string
	for _, c := range doc.Root.Children {
		ids = append(ids, c.ID)
	}
	return ids
}

func indexOf(xs []string, x string) int {
	for i, v := range xs {
		if v == x {
			return i
		}
	}
	return -1
}

// The order from the second report: "change the blue words you send me to purple".
// The coloured words in a reply are markdown.code and markdown.link (cyan by default);
// the guide must say so, and ui_edit must recolour them.
func TestTheAgentRecoloursTheWordsInItsReplies(t *testing.T) {
	t.Setenv(configDirEnv, t.TempDir())
	r := newBridgeRig(t, "ask")
	guide := r.b.call(context.Background(), driver.ClientToolCall{Name: uiToolGuide}, nil).Text
	for _, want := range []string{"markdown.code = fg=cyan", "inline `code` in answers", "Purple: magenta"} {
		if !strings.Contains(guide, want) {
			t.Errorf("the guide lacks %q.\nConsequence: the model cannot tell which token paints the coloured words in its replies, and guesses or gives up.\nRemedy: list every token with its value and role (colorTable).", want)
		}
	}
	var got userTokens
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	go func() {
		req := <-r.b.apply
		got = req.colors
		req.done <- nil
	}()
	res := r.b.call(ctx, driver.ClientToolCall{Name: uiToolEdit,
		Arguments: json.RawMessage(`{"colors":{"markdown.code":"fg=magenta","markdown.link":"fg=magenta underline"},"summary":"Purple instead of blue"}`)},
		func(a driver.Approval) bool { r.asked = append(r.asked, a); return true })
	if !res.OK {
		t.Fatalf("recolouring failed: %+v", res)
	}
	if len(r.asked) != 1 || !strings.Contains(r.asked[0].Diff, "- markdown.code: fg=cyan") || !strings.Contains(r.asked[0].Diff, "+ markdown.code: fg=magenta") {
		t.Fatalf("the user was shown %+v; a colour change must be shown as was/now before it is made", r.asked)
	}
	if s := withUserTokens(theme.SOBRIA(), got).Resolve("markdown.code"); s.String() != "fg=magenta" {
		t.Errorf("markdown.code resolves to %q after the change", s.String())
	}
}

func TestAColourChangeIsRefusedWhenItWouldNotDraw(t *testing.T) {
	for _, tc := range []struct{ name, colors, want string }{
		{"typo token", `{"markdown.cod":"fg=magenta"}`, "markdown.cod"},
		{"not a colour", `{"markdown.code":"fg=purpleish"}`, "purpleish"},
		{"not a style", `{"markdown.code":"magenta"}`, "fg="},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := newBridgeRig(t, "full access")
			res := r.edit(t, `{"colors":`+tc.colors+`}`, true)
			if res.OK || !strings.Contains(res.Text, tc.want) {
				t.Fatalf("res = %+v.\nConsequence: a colour that cannot draw is reported as made, and the screen does not change.\nRemedy: applyColors must refuse unknown tokens and unreadable styles, naming them.", res)
			}
		})
	}
}

func TestColoursAreSavedUndoneAndResetWithTheLayout(t *testing.T) {
	t.Setenv(configDirEnv, t.TempDir())
	doc := builtinDoc(t)
	purple, err := applyColors(nil, theme.SOBRIA(), map[string]string{"markdown.code": "fg=magenta"})
	if err != nil {
		t.Fatal(err)
	}
	if err := saveInterface(doc, purple, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(userScenePath()); !os.IsNotExist(err) {
		t.Error("a colour-only change saved a copy of the built-in layout; the user would stop receiving layout fixes from new releases")
	}
	got, err := loadUserTokens()
	if err != nil || got["markdown.code"].String() != "fg=magenta" {
		t.Fatalf("saved colours read back as %v, %v", got, err)
	}
	undone, back, _, err := undoInterface()
	if err != nil || len(back) != 0 || undone == nil || undone.Root == nil {
		t.Fatalf("undo gave colours %v, %v; it must return the built-in look with no user colours", back, err)
	}
	_, again, _, err := undoInterface()
	if err != nil || again["markdown.code"].String() != "fg=magenta" {
		t.Fatalf("a second undo gave %v, %v; it must bring the purple back", again, err)
	}
	if _, cols, _, err := resetInterface(); err != nil || len(cols) != 0 {
		t.Fatalf("reset gave %v, %v", cols, err)
	}
	if _, err := os.Stat(userThemePath()); !os.IsNotExist(err) {
		t.Error("/ui reset left the saved colours on disk; the next session would come back purple")
	}
}

func TestTheModelIsToldItRunsInsideArxiTUI(t *testing.T) {
	for _, want := range []string{"arxi-tui", "TUI", "colours", "ui_guide", "never search their files"} {
		if !strings.Contains(uiSystemHint, want) {
			t.Errorf("the standing hint lacks %q.\nConsequence: a real model asked about \"the tui\" searched the project eleven times and offered to write a theme file there.\nRemedy: name the app and what the words mean in uiSystemHint.", want)
		}
	}
	if len(uiSystemHint) > 400 {
		t.Errorf("the hint is %d bytes and rides with every question; keep the how in ui_guide", len(uiSystemHint))
	}
}

func TestColorCommandReadsTokenAndStyle(t *testing.T) {
	if tok, style, ok := uiColorCommand("/ui color markdown.code fg=magenta bold"); !ok || tok != "markdown.code" || style != "fg=magenta bold" {
		t.Errorf("got %q %q %v", tok, style, ok)
	}
	if tok, style, ok := uiColorCommand("/ui color markdown.code"); !ok || tok != "markdown.code" || style != "" {
		t.Errorf("a bare token must mean \"back to the default\": %q %q %v", tok, style, ok)
	}
	if _, _, ok := uiColorCommand("/ui colorful"); ok {
		t.Error("/ui colorful is not a colour command")
	}
}
