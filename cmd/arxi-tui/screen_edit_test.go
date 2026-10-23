package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/michiTrader/arxi_tui/internal/driver"
	"github.com/michiTrader/arxi_tui/internal/fold"
	"github.com/michiTrader/arxi_tui/internal/theme"
)

// The model rebuilds a screen through ui_edit: the user sees a diff and approves it, the file
// is written to the profile, and the screen shows it the next time it opens.

// screenBridgeDriver lends the loop an interface bridge. It is not bridgeDriver because
// that one lives in a test file that does not build on Windows, and this must.
type screenBridgeDriver struct {
	testDriver
	br *uiBridge
}

func (d *screenBridgeDriver) InterfaceBridge() *uiBridge { return d.br }

func screensArgs(t *testing.T, m map[string]string) string {
	t.Helper()
	b, err := json.Marshal(map[string]any{"screens": m, "summary": "Rebuild a screen"})
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func screenFile(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(screensDir(), name+".json"))
	if err != nil {
		return ""
	}
	return string(b)
}

func screenRig(t *testing.T, mode string) *bridgeRig {
	t.Helper()
	t.Setenv(configDirEnv, t.TempDir())
	return newBridgeRig(t, mode)
}

// An approved change writes the file.
// Consequence: arxi says "done" and the screen is unchanged, which is the lie ui_edit exists to prevent.
// Remedy: ui_edit must write each screen it was given after the user allows it.
func TestUIEditWritesAScreenAfterTheUserAllowsIt(t *testing.T) {
	r := screenRig(t, "ask")
	res := r.edit(t, screensArgs(t, map[string]string{"flow": replacementScreen}), true)
	if !res.OK || !strings.Contains(res.Text, "applied and saved") {
		t.Fatalf("the change was not applied: %+v", res)
	}
	if got := screenFile(t, "flow"); got != replacementScreen {
		t.Errorf("flow.json = %q, want the scene the model sent", got)
	}
	if len(r.asked) != 1 || !strings.Contains(r.asked[0].Diff, "screens/flow.json") {
		t.Errorf("the user was not shown a diff naming the file: %+v", r.asked)
	}
}

// A refusal by the user writes nothing.
func TestUIEditWritesNothingWhenTheUserRefuses(t *testing.T) {
	r := screenRig(t, "ask")
	res := r.edit(t, screensArgs(t, map[string]string{"flow": replacementScreen}), false)
	if res.OK || screenFile(t, "flow") != "" {
		t.Errorf("a refused change wrote a file: %+v", res)
	}
}

// A scene that does not load is refused before the user is asked and nothing is written.
// Consequence: a broken screen file would be saved, and the user would see the built-in screen plus a notice.
// Remedy: validate with the same rule the loader uses (parseScreen).
func TestUIEditRefusesABrokenScreenAndNamesIt(t *testing.T) {
	r := screenRig(t, "ask")
	res := r.edit(t, screensArgs(t, map[string]string{"flow": `{"root":{"type":"nonsense"}}`}), true)
	if res.OK || !strings.Contains(res.Text, "screens.flow") {
		t.Errorf("a broken screen was not refused with its name: %+v", res)
	}
	if len(r.asked) != 0 || screenFile(t, "flow") != "" {
		t.Errorf("the user was asked, or a file was written, for a broken screen: asked=%d", len(r.asked))
	}
}

func TestUIEditRefusesAnUnknownScreen(t *testing.T) {
	r := screenRig(t, "ask")
	res := r.edit(t, screensArgs(t, map[string]string{"settings": replacementScreen}), true)
	if res.OK || !strings.Contains(res.Text, "not a screen") {
		t.Errorf("an unknown screen was accepted: %+v", res)
	}
}

// Plan mode only says what would change.
func TestUIEditRefusesScreensInPlanMode(t *testing.T) {
	r := screenRig(t, "plan")
	res := r.edit(t, screensArgs(t, map[string]string{"flow": replacementScreen}), true)
	if res.OK || screenFile(t, "flow") != "" {
		t.Errorf("plan mode wrote a screen: %+v", res)
	}
}

// An empty text brings the built-in screen back by removing the file.
func TestUIEditEmptyTextRestoresTheBuiltInScreen(t *testing.T) {
	r := screenRig(t, "ask")
	if res := r.edit(t, screensArgs(t, map[string]string{"flow": replacementScreen}), true); !res.OK {
		t.Fatal(res)
	}
	if res := r.edit(t, screensArgs(t, map[string]string{"flow": ""}), true); !res.OK {
		t.Fatalf("restore failed: %+v", res)
	}
	if screenFile(t, "flow") != "" {
		t.Error("the file is still there after restoring the built-in screen")
	}
}

// Taking away a screen that has no file is nothing to do, not an error and not a question.
func TestUIEditRestoringAScreenWithNoFileIsNothingToChange(t *testing.T) {
	r := screenRig(t, "ask")
	res := r.edit(t, screensArgs(t, map[string]string{"flow": ""}), true)
	if len(r.asked) != 0 || strings.Contains(res.Text, "applied and saved") {
		t.Errorf("asked or applied for a change that is nothing: asked=%d %+v", len(r.asked), res)
	}
}

// The model reads a screen before it rewrites it: the file in use, or the built-in one.
func TestUIGuideReadsAScreenAndNamesTheScreens(t *testing.T) {
	r := screenRig(t, "ask")
	res := r.b.call(context.Background(), driver.ClientToolCall{Name: uiToolGuide, Arguments: json.RawMessage(`{"screen":"flow"}`)}, nil)
	if !res.OK || !strings.Contains(res.Text, "hub.rows") {
		t.Fatalf("the built-in flow screen was not given: %+v", res)
	}
	if err := os.MkdirAll(screensDir(), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(screensDir(), "flow.json"), []byte(replacementScreen), 0o600); err != nil {
		t.Fatal(err)
	}
	res = r.b.call(context.Background(), driver.ClientToolCall{Name: uiToolGuide, Arguments: json.RawMessage(`{"screen":"flow"}`)}, nil)
	if !strings.Contains(res.Text, "PROFILE HUB") {
		t.Errorf("the file in use was not what the model was given: %+v", res)
	}
	if bad := r.b.call(context.Background(), driver.ClientToolCall{Name: uiToolGuide, Arguments: json.RawMessage(`{"screen":"nope"}`)}, nil); bad.OK {
		t.Error("an unknown screen was read")
	}
	if g := r.b.guideText(); !strings.Contains(g, "SCREENS.") || !strings.Contains(g, "installer") {
		t.Error("the guide does not teach the screens")
	}
}

// A file that moved while the question was open is not overwritten.
func TestAScreenChangeDraftedFromAnOldFileIsStale(t *testing.T) {
	t.Setenv(configDirEnv, t.TempDir())
	e, err := draftScreens(map[string]string{"flow": replacementScreen}, nil, true)
	if err != nil || e == nil {
		t.Fatalf("draft: %v %v", e, err)
	}
	if !e.fresh() {
		t.Fatal("a draft is stale before anything moved")
	}
	if err := os.MkdirAll(screensDir(), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(screensDir(), "flow.json"), []byte(`{"root":{"type":"text","text":"other"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if e.fresh() {
		t.Error("the file changed under the draft and it still counts as fresh; the loop would overwrite someone's edit")
	}
}

// -scene and -raw never read the folder, so a screen written there would do nothing.
func TestScreensAreRefusedOutsideTheEverydayInterface(t *testing.T) {
	t.Setenv(configDirEnv, t.TempDir())
	if _, err := draftScreens(map[string]string{"flow": replacementScreen}, nil, false); err == nil {
		t.Error("a screen was drafted for an interface that never reads the folder")
	}
}

// All of it through the real loop: the model rebuilds /flow, and /flow then shows it.
// Consequence: each part passes alone and the whole never works, so "ask arxi to rebuild it" is a claim, not a feature.
// Remedy: the loop must apply uiApply.screens, and /flow must read the file it wrote.
func TestLoopTheModelRebuildsFlowAndTheUserOpensIt(t *testing.T) {
	t.Setenv(configDirEnv, t.TempDir())
	t.Setenv(historyEnv, t.TempDir())
	t.Setenv(sessionsEnv, "")
	br := newUIBridge()
	br.setMode("full access")
	drv := &screenBridgeDriver{testDriver: testDriver{evCh: make(chan fold.Event, 64), seq: 1000}, br: br}
	tty := newOpenFakeTTY(100, 24, nil)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- loop(ctx, tty, builtinDoc(t), theme.SOBRIA(), drv.evCh, drv, "") }()

	waitFor := func(want string) {
		t.Helper()
		deadline := time.Now().Add(30 * time.Second)
		for !strings.Contains(stripANSI(tty.output()), want) && time.Now().Before(deadline) {
			time.Sleep(5 * time.Millisecond)
		}
		if !strings.Contains(stripANSI(tty.output()), want) {
			t.Fatalf("never saw %q:\n%s", want, stripANSI(tty.output()))
		}
	}
	waitFor("Run /help")
	cctx, ccancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer ccancel()
	flowText := strings.Replace(replacementScreen, "PROFILE HUB", "MODEL MADE FLOW", 1)
	res := br.call(cctx, driver.ClientToolCall{Name: uiToolEdit, Arguments: json.RawMessage(screensArgs(t, map[string]string{"flow": flowText}))},
		func(driver.Approval) bool { return true })
	if !res.OK {
		t.Fatalf("the model's change failed: %+v", res)
	}
	for _, ev := range append(typed("/flow"), at(20, enterEvent())) {
		time.Sleep(ev.delay)
		tty.events <- ev.event
	}
	waitFor("MODEL MADE FLOW")
	for _, ev := range exitKeys() {
		time.Sleep(ev.delay)
		tty.events <- ev.event
	}
	select {
	case <-done:
	case <-time.After(30 * time.Second):
		t.Fatal("the loop did not quit")
	}
}
