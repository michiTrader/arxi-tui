package main

import (
	"context"
	"os"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/michiTrader/arxi_tui/internal/fold"
	"github.com/michiTrader/arxi_tui/internal/theme"
)

// The request that failed for a real user, in the words the model has to be able to
// act on: "an animated colourful line around the input bar". The line is the input's
// border; its colour is the style token the border names; the token is an animation.
// This test builds exactly what ui_guide teaches and then looks at the bytes drawn.

const rainbowBorderEdit = `{"behaviour":{"animations":{"rainbow":{"colors":["red","yellow","green","cyan","blue","magenta"],"fps":30,"spread":1}}},` +
	`"commands":["/ui add node above_input {\"id\":\"input_frame\",\"type\":\"box\",\"border\":{\"shape\":\"round\",\"style\":\"rainbow\"},\"children\":[]}","/ui move prompt into input_frame"],"summary":"Rainbow frame around the input"}`

func TestTheGuideTeachesAnAnimatedFrameAroundTheInput(t *testing.T) {
	r := newBridgeRig(t, "allow")
	guide := uiGuide(r.doc, r.b.below)
	for _, want := range []string{"input_frame", `\"style\":\"rainbow\"`, "animation"} {
		if !strings.Contains(guide+behaviourGuide(behaviour{}), want) {
			t.Errorf("the guide never shows %q; a model asked for a moving frame had no example to copy (it tried sixteen times, blind)", want)
		}
	}
}

func TestAnAnimatedFrameAroundTheInputIsAccepted(t *testing.T) {
	r := newBridgeRig(t, "allow")
	res := r.edit(t, rainbowBorderEdit, true)
	if !res.OK {
		t.Fatalf("the edit the guide teaches was refused: %s", res.Text)
	}
	frame := findNodeByID(r.doc, "input_frame")
	if frame == nil || frame.BorderStyleName() != "rainbow" || frame.BorderShape() != "round" ||
		len(frame.Children) != 1 || frame.Children[0].ID != "prompt" {
		t.Fatalf("the input is not inside an animated round frame: %+v", frame)
	}

}

// A refusal must say why on the screen, and the same refusal three times must make the
// model stop and report. Counterfactual: a different refusal in between resets the count.
func TestARefusalShowsItsReasonAndARepeatTellsTheModelToStop(t *testing.T) {
	r := newBridgeRig(t, "allow")
	bad := `{"commands":["/ui add node nowhere {\"id\":\"x\",\"type\":\"text\"}"],"summary":"x"}`
	var last string
	for i := 1; i <= 3; i++ {
		res := r.edit(t, bad, true)
		if res.OK || !strings.HasPrefix(res.Summary, "The change was refused: ") || len(res.Summary) < 40 {
			t.Fatalf("attempt %d: the chat line must carry the reason, got %q", i, res.Summary)
		}
		last = res.Text
		if i < 3 && strings.Contains(res.Text, "Stop calling ui_edit") {
			t.Fatalf("attempt %d: told to stop too early", i)
		}
	}
	if !strings.Contains(last, "Stop calling ui_edit") || !strings.Contains(last, "Never say the change was made") {
		t.Errorf("the third identical refusal must tell the model to report: %s", last)
	}
	r2 := newBridgeRig(t, "allow")
	r2.edit(t, bad, true)
	r2.edit(t, `{"summary":"x"}`, true)
	r2.edit(t, bad, true)
	if res := r2.edit(t, bad, true); strings.Contains(res.Text, "Stop calling ui_edit") {
		t.Error("two separate refusals were counted as three in a row")
	}
}

// Switching model leaves a line in the conversation and one sentence for the model,
// once. Counterfactual: picking the model already in use records nothing.
func TestSwitchingModelIsOnTheRecordAndTheModelIsTold(t *testing.T) {
	out := make(chan fold.Event, 8)
	c := &chatSession{out: out}
	if got := c.noteModelSwitch(context.Background(), "a/one"); got != "model changed to a/one" {
		t.Fatalf("first model: %q", got)
	}
	<-out
	c.takeModelNote()
	if got := c.noteModelSwitch(context.Background(), "a/one"); got != "" || len(out) != 0 {
		t.Fatalf("the same model recorded a switch: %q", got)
	}
	line := c.noteModelSwitch(context.Background(), "b/two")
	ev := <-out
	if line != "model changed from a/one to b/two" || ev.Type != "chat.warn" || ev.Payload["text"] != line {
		t.Fatalf("the switch must be a chat line: %q %+v", line, ev)
	}
	note := c.takeModelNote()
	if !strings.Contains(note, "a/one") || !strings.Contains(note, "b/two") || !strings.Contains(note, "not by you") {
		t.Errorf("the model is not told who wrote the earlier replies: %q", note)
	}
	if c.takeModelNote() != "" {
		t.Error("the note must be given once")
	}
}

// The frame the guide teaches must be drawn in several colours across ticks.
func TestTheGuidesAnimatedFrameChangesColourOnScreen(t *testing.T) {
	r := newBridgeRig(t, "allow")
	res := r.edit(t, rainbowBorderEdit, true)
	if !res.OK {
		t.Fatalf("refused: %s", res.Text)
	}
	t.Setenv(configDirEnv, t.TempDir())
	_ = os.MkdirAll(configDir(), 0o700)
	beh := `{"animations":{"rainbow":{"colors":["red","green","blue"],"fps":30,"spread":0}}}`
	if err := os.WriteFile(userBehaviourPath(), []byte(beh), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { setActiveBehaviour(behaviour{}) })
	if err := os.WriteFile(userScenePath(), r.doc.Source(), 0o600); err != nil {
		t.Fatal(err)
	}
	drv := &effortDriver{testDriver: testDriver{evCh: make(chan fold.Event, 64)}}
	script := []scheduledEvent{{700 * time.Millisecond, ctrlCharEvent('c')}, {50 * time.Millisecond, ctrlCharEvent('c')}}
	tty := newFakeTTY(110, 30, script)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := loop(ctx, tty, r.doc, theme.SOBRIA(), drv.evCh, drv, ""); err != nil {
		t.Fatalf("loop: %v", err)
	}
	out := tty.output()
	edge := regexp.MustCompile(`\x1b\[([0-9;]*)m[╭╮╰╯─]`)
	colours := map[string]bool{}
	for _, m := range edge.FindAllStringSubmatch(out, -1) {
		colours[m[1]] = true
	}
	if len(colours) < 2 {
		t.Errorf("the frame was drawn in %d colour(s) over the run; an animated frame shows several: %v", len(colours), colours)
	}
}

// The edit the model actually made: the animation on the box's "style" and a plain
// "round" border. Valid, applied, and the frame stays white. The tool must say so
// instead of reporting a clean success the model repeats to the user.
func TestAnAnimationOnTheBoxStyleButNotTheBorderIsReportedNotCelebrated(t *testing.T) {
	r := newBridgeRig(t, "allow")
	res := r.edit(t, `{"behaviour":{"animations":{"rainbow":{"colors":["red","yellow","green"],"spread":1}}},`+
		`"commands":["/ui add node above_input {\"id\":\"input_frame\",\"type\":\"box\",\"border\":\"round\",\"style\":{\"style\":\"rainbow\"},\"children\":[]}","/ui move prompt into input_frame"],"summary":"x"}`, true)
	if !res.OK {
		t.Fatalf("refused: %s", res.Text)
	}
	if !strings.Contains(res.Text, "is NOT animated") || !strings.Contains(res.Text, `/ui set input_frame border {"shape":"round","style":"rainbow"}`) {
		t.Fatalf("the model was told nothing about the frame staying plain: %s", res.Text)
	}
	// counterfactual: the right form carries no warning
	r2 := newBridgeRig(t, "allow")
	if res := r2.edit(t, rainbowBorderEdit, true); !res.OK || strings.Contains(res.Text, "NOT animated") {
		t.Fatalf("a correct frame was reported as broken: %+v", res)
	}
}
