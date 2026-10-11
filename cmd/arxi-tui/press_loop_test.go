package main

import (
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/michiTrader/arxi_tui/internal/term"
)

// pressDocWith is a scene with a prompt and one pressable node whose on_press is
// the given action, so a loop test can press it with Tab then Enter.
func pressDocWith(onPress string) string {
	return `{"root":{"type":"stack","children":[
	  ` + factoryNoticeNode + `,
	  {"id":"chat","type":"markdown","bind":"chat.history","grow":1},
	  {"id":"btn","type":"text","text":"[ go ]","on_press":"` + onPress + `"},
	  {"id":"prompt","type":"input","bind":"user.input","placeholder":"> "}
	]}}`
}

// pressBtn moves focus to the button (Tab from the input) and presses it.
func pressBtn() []scheduledEvent {
	return []scheduledEvent{
		{10 * time.Millisecond, arrowEvent(term.KeyTab)},
		{10 * time.Millisecond, enterEvent()},
	}
}

// A button and the keyboard are one door: the same line gives the same model
// text, exactly "hi".
func TestLoopButtonAndKeyboardSendTheSameText(t *testing.T) {
	_, viaButton := runLoopDoc(t, pressDocWith("cmd:/send hi"), append(pressBtn(), exitKeys()...))
	_, viaKeys := runLoopDoc(t, pressDocWith("focus:prompt"), append(enterLine("/send hi"), exitKeys()...))
	want := []string{"hi"}
	if !reflect.DeepEqual(viaButton, want) {
		t.Errorf("button cmd:/send hi submitted %q, want %q", viaButton, want)
	}
	if !reflect.DeepEqual(viaKeys, want) {
		t.Errorf("typed /send hi submitted %q, want %q", viaKeys, want)
	}
}

// /send is the explicit door for a line that starts with "/".
func TestLoopButtonSendOfASlashLineIsLiteral(t *testing.T) {
	_, got := runLoopDoc(t, pressDocWith("cmd:/send /help"), append(pressBtn(), exitKeys()...))
	if want := []string{"/help"}; !reflect.DeepEqual(got, want) {
		t.Errorf("submitted %q, want the literal %q", got, want)
	}
}

// A button naming a command that does not exist warns like the typed line and
// sends nothing: before this, it sent the text minus the slash.
func TestLoopButtonWithAnUnknownCommandWarnsAndSendsNothing(t *testing.T) {
	out, got := runLoopDoc(t, pressDocWith("cmd:/nonsense"), append(pressBtn(), exitKeys()...))
	if len(got) != 0 {
		t.Errorf("a button sent %q to the model", got)
	}
	if !strings.Contains(ansi.Strip(out), "Unknown command /nonsense.") {
		t.Errorf("no warning for a button on an unknown command; output:\n%s", ansi.Strip(out))
	}
}

// A button on a real local command runs the command, not the model.
func TestLoopButtonRunsALocalCommand(t *testing.T) {
	out, got := runLoopDoc(t, pressDocWith("cmd:/focus nowhere"), append(pressBtn(), exitKeys()...))
	if len(got) != 0 {
		t.Errorf("a button on /focus sent %q to the model", got)
	}
	if !strings.Contains(ansi.Strip(out), `no node with id "nowhere"`) {
		t.Errorf("the button did not run /focus; output:\n%s", ansi.Strip(out))
	}
}
