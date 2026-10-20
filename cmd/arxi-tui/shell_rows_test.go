package main

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/michiTrader/arxi_tui/internal/defaultscene"
	"github.com/michiTrader/arxi_tui/internal/fold"
	"github.com/michiTrader/arxi_tui/internal/patch"
	"github.com/michiTrader/arxi_tui/internal/scene"
	"github.com/michiTrader/arxi_tui/internal/term"
	"github.com/michiTrader/arxi_tui/internal/theme"
)

// simRegistry is n pretend shells, as the simulation makes them, that a test can then
// bend into the states and outputs it needs.
func simRegistry(n int) *shellRegistry {
	return newSimShells(n, func(string) {})
}

func setOutput(s *shell, text string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.buf = []byte(text)
	s.written = int64(len(text))
}

func TestShellRowsHaveOneRowPerShellWithTheNumbersTheUserTypes(t *testing.T) {
	r := simRegistry(3)
	r.shells[1].info.State, r.shells[1].info.Code = shellExited, 2
	r.shells[1].info.Ended = r.shells[1].info.Started.Add(90 * time.Second)
	r.shells[2].info.State = shellStopped
	rows := shellRows(r)
	if len(rows) != 3 {
		t.Fatalf("%d rows for 3 shells", len(rows))
	}
	want := []struct {
		id, state string
		running   bool
	}{{"1", "running", true}, {"2", "exited 2", false}, {"3", "stopped", false}}
	for i, w := range want {
		if rows[i].ID != w.id || rows[i].State != w.state || rows[i].Running != w.running {
			t.Errorf("row %d = %+v, want id %s state %q running %v\nConsequence: a list of shells shows the wrong number to /shell show, or a stopped shell as running.\nRemedy: build the row from shellInfo, the same source /shell uses.", i, rows[i], w.id, w.state, w.running)
		}
	}
	if rows[1].Age != "1m30s" {
		t.Errorf("an ended shell's age is %q, want how long it ran (1m30s) and not how long ago it started", rows[1].Age)
	}
	if rows[0].Command != "simulated command 1" {
		t.Errorf("command %q", rows[0].Command)
	}
}

func TestTheLastLineIsTheLastThingPrintedAndCannotCarryEscapesOrControls(t *testing.T) {
	r := simRegistry(1)
	setOutput(r.shells[0], "first\n\x1b[31mred\x1b[0m\x07 bell \u202eevil\n\n   \n")
	got := shellRows(r)[0].Last
	if got != "red bell evil" {
		t.Errorf("last = %q, want %q\nConsequence: what a program prints reaches the terminal as an escape sequence or reorders the line (bidi), on a screen the user trusts.\nRemedy: pass the output through cleanShellText before it becomes a row.", got, "red bell evil")
	}
	setOutput(r.shells[0], "")
	if got := shellRows(r)[0].Last; got != "" {
		t.Errorf("a shell that printed nothing has last %q", got)
	}
}

func TestLongFieldsAreCutAndALineBreakInACommandStaysOneLine(t *testing.T) {
	r := simRegistry(1)
	r.shells[0].info.Command = "echo one\necho two\t" + strings.Repeat("x", 200)
	setOutput(r.shells[0], strings.Repeat("y", 500)+"\n")
	row := shellRows(r)[0]
	if strings.ContainsAny(row.Command, "\n\t") || len([]rune(row.Command)) > maxShellRowShown {
		t.Errorf("command field %q (%d runes)\nConsequence: a multi-line field breaks the row layout and a long one pushes the state off screen.\nRemedy: collapse whitespace and cut to %d.", row.Command, len([]rune(row.Command)), maxShellRowShown)
	}
	if len([]rune(row.Last)) != maxShellRowLast || !strings.HasSuffix(row.Last, "…") {
		t.Errorf("last has %d runes, want %d ending in an ellipsis: %q", len([]rune(row.Last)), maxShellRowLast, row.Last)
	}
}

func TestMoreThanTwentyShellsShowTheNewestTwenty(t *testing.T) {
	r := simRegistry(maxShellRows + 5)
	rows := shellRows(r)
	if len(rows) != maxShellRows {
		t.Fatalf("%d rows, want %d", len(rows), maxShellRows)
	}
	if rows[0].ID != "6" || rows[len(rows)-1].ID != "25" {
		t.Errorf("first %s last %s, want 6 and 25\nConsequence: the list hides the shell the user just started and keeps the oldest.\nRemedy: keep the tail of the list.", rows[0].ID, rows[len(rows)-1].ID)
	}
}

func TestNoRegistryAndNoShellHostGiveNoRows(t *testing.T) {
	if rows := shellRows(nil); rows != nil {
		t.Errorf("nil registry: %v", rows)
	}
	if rows := runningShellRows(&testDriver{}); rows != nil {
		t.Errorf("a driver with no shells: %v", rows)
	}
	if rows := shellRows(simRegistry(0)); len(rows) != 0 {
		t.Errorf("empty registry: %v", rows)
	}
}

// ---- the document may draw and press them --------------------------------------------

const shellListScene = `{"root":{"type":"stack","children":[
  {"id":"chat","type":"markdown","bind":"chat.history","grow":1},
  {"id":"shell_list","type":"list","bind":"host.shell.rows",
   "row_template":{"id":"shell_row","type":"text","text":"{row.id} {row.state} {row.command} | {row.last}","on_press":"cmd:/shell show {row.id}"}},
  {"id":"prompt","type":"input","bind":"user.input","placeholder":"say something"}]}}`

func TestASceneMayDrawAndPressEveryFieldOfAShellRow(t *testing.T) {
	doc := `{"root":{"type":"list","bind":"host.shell.rows","row_template":{"type":"text","text":"{row.id} {row.command} {row.state} {row.age} {row.last}","when":"row.running","on_press":"cmd:/shell show {row.id}"}}}`
	d, err := scene.ParseDocument([]byte(doc))
	if err != nil {
		t.Fatal(err)
	}
	if err := d.Validate(); err != nil {
		t.Errorf("a list over host.shell.rows is refused: %v\nConsequence: the model is told the bind exists (BINDS.md) and its scene is rejected.\nRemedy: sign the bind and its row schema in validate.go.", err)
	}
}

func TestAFieldTheRowDoesNotHaveIsRefusedAtLoad(t *testing.T) {
	doc := `{"root":{"type":"list","bind":"host.shell.rows","row_template":{"type":"text","text":"{row.pid}"}}}`
	d, err := scene.ParseDocument([]byte(doc))
	if err != nil {
		t.Fatal(err)
	}
	if err := d.Validate(); err == nil {
		t.Error("{row.pid} passed validation: a typo in a field name would draw nothing and report success")
	}
}

// loopWithShells runs the real loop with a driver that owns the given registry.
func loopWithShells(t *testing.T, sceneJSON string, reg *shellRegistry, script []scheduledEvent) (string, *shellTestDriver) {
	t.Helper()
	t.Setenv(configDirEnv, t.TempDir())
	t.Setenv(historyEnv, t.TempDir())
	t.Setenv(sessionsEnv, "")
	doc, err := scene.ParseDocument([]byte(sceneJSON))
	if err != nil {
		t.Fatal(err)
	}
	drv := &shellTestDriver{testDriver: testDriver{evCh: make(chan fold.Event, 64), seq: 1000}, reg: reg}
	tty := newFakeTTY(100, 30, script)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := loop(ctx, tty, doc, theme.SOBRIA(), drv.evCh, drv, ""); err != nil {
		t.Fatalf("loop: %v", err)
	}
	return tty.output(), drv
}

// Scenario 3 of docs/DESIGN-OPEN-TUI.md, the half phase B can do: a list with one row per
// shell, and choosing a row shows what that shell printed. (A window for the output is
// phase D; here the output is the note /shell show already writes in the chat.)
func TestScenario3ChoosingAShellRowShowsItsOutput(t *testing.T) {
	reg := simRegistry(2)
	setOutput(reg.shells[0], "server listening\n")
	setOutput(reg.shells[1], "compiling...\ndone in 3s\n")
	script := []scheduledEvent{
		{80 * time.Millisecond, arrowEvent(term.KeyTab)},
		{20 * time.Millisecond, arrowEvent(term.KeyTab)},
		{20 * time.Millisecond, enterEvent()},
		{150 * time.Millisecond, ctrlCharEvent('c')},
		{50 * time.Millisecond, ctrlCharEvent('c')},
	}
	out, drv := loopWithShells(t, shellListScene, reg, script)
	frames := strings.Split(out, frameBegin)
	first := frames[1]
	for _, want := range []string{"1 running simulated command 1 | server listening", "2 running simulated command 2 | done in 3s"} {
		if !strings.Contains(stripANSI(first), want) {
			t.Errorf("the list lacks %q:\n%s\nConsequence: the scene is told one row per shell exists and draws nothing.\nRemedy: publish state.HostShellRows each frame.", want, lastFrame(out))
		}
	}
	if len(drv.submitted) != 0 {
		t.Errorf("pressing a row sent %q to the model; it must run /shell, not ask the model", drv.submitted)
	}
	if got := lastFrame(out); !strings.Contains(got, "done in 3s") || !strings.Contains(got, "shell 2") {
		t.Errorf("choosing row 2 did not show shell 2's output:\n%s\nConsequence: the list is decoration, a row cannot be used to read a shell.\nRemedy: expand {row.id} in on_press from the row that was pressed.", got)
	}
}

func TestAShellThatEndsDropsTheRunningFlagButStaysInTheList(t *testing.T) {
	reg := simRegistry(2)
	reg.shells[0].info.State, reg.shells[0].info.Code = shellExited, 0
	reg.shells[0].info.Ended = time.Now()
	// live is drawn only for a row that runs; the plain line is drawn for every row.
	doc := `{"root":{"type":"stack","children":[
  {"type":"list","bind":"host.shell.rows","row_template":{"type":"stack","children":[
    {"type":"text","text":"row {row.id} {row.state}"},
    {"type":"text","text":"live {row.id}","when":"row.running"}]}},
  {"id":"prompt","type":"input","bind":"user.input"}]}}`
	out, _ := loopWithShells(t, doc, reg, append([]scheduledEvent{{80 * time.Millisecond, arrowEvent(term.KeyTab)}}, exitKeys()...))
	f := lastFrame(out)
	for _, want := range []string{"row 1 exited 0", "row 2 running", "live 2"} {
		if !strings.Contains(f, want) {
			t.Errorf("the frame lacks %q:\n%s", want, f)
		}
	}
	if strings.Contains(f, "live 1") {
		t.Errorf("an ended shell is drawn as running:\n%s\nConsequence: the user presses stop on something that already ended.\nRemedy: row.running is true only while the shell runs.", f)
	}
}

// factoryWithShellList is the factory scene with a list of shells above the prompt, so the
// slash menu exists in it (the small scenes of these tests have none).
func factoryWithShellList(t *testing.T) string {
	t.Helper()
	doc := builtinDoc(t)
	res, err := patch.Apply(doc.Name(), doc.Source(), `/ui add node above prompt {"id":"shell_list","type":"list","bind":"host.shell.rows","row_template":{"type":"text","text":"ROW {row.id} {row.command}"}}`)
	if err != nil {
		t.Fatal(err)
	}
	return string(res.Source)
}

func TestOpeningTheMenuHidesTheShellRowsLikeTheRestOfTheStatusLine(t *testing.T) {
	reg := simRegistry(1)
	script := []scheduledEvent{{80 * time.Millisecond, arrowEvent(term.KeyTab)}}
	script = append(script, typed("/")...)
	script = append(script, scheduledEvent{200 * time.Millisecond, ctrlCharEvent('c')}, scheduledEvent{50 * time.Millisecond, ctrlCharEvent('c')})
	out, _ := loopWithShells(t, factoryWithShellList(t), reg, script)
	var withRows, withMenu, both int
	for _, f := range strings.Split(out, frameBegin)[1:] {
		f = stripANSI(f)
		rows, menu := strings.Contains(f, "ROW 1 simulated command 1"), strings.Contains(f, "esc close")
		if rows {
			withRows++
		}
		if menu {
			withMenu++
		}
		if rows && menu {
			both++
		}
	}
	if withRows == 0 || withMenu == 0 {
		t.Fatalf("the test does not see both screens (frames with rows %d, with the menu %d), so it proves nothing", withRows, withMenu)
	}
	if both != 0 {
		t.Errorf("%d frames draw the shell list and the open menu together\nConsequence: the list and the menu fight for the same rows of the screen.\nRemedy: clear HostShellRows where the loop clears HostShellList while the menu is open.", both)
	}
}

func TestThePreviewShowsTheRowsOfTheShellsItWasAskedFor(t *testing.T) {
	r := newBridgeRig(t, "ask")
	add := `/ui add node above prompt {\"id\":\"shell_list\",\"type\":\"list\",\"bind\":\"host.shell.rows\",\"row_template\":{\"type\":\"text\",\"text\":\"[{row.id}] {row.command}\"}}`
	res := r.preview(t, `{"change":{"commands":["`+add+`"]},"shells":2}`)
	if !res.OK {
		t.Fatalf("preview: %+v", res)
	}
	for _, want := range []string{"[1] simulated command 1", "[2] simulated command 2"} {
		if !strings.Contains(res.Text, want) {
			t.Errorf("the preview lacks %q:\n%s\nConsequence: the model builds a shell list it cannot check, and says done about a screen that may be empty.\nRemedy: fill HostShellRows in the preview state, as the loop does.", want, res.Text)
		}
	}
}

func TestTheFactorySceneDoesNotDrawTheRowsYet(t *testing.T) {
	reg := simRegistry(2)
	out, _ := loopWithShells(t, string(defaultscene.JSON), reg, append([]scheduledEvent{{80 * time.Millisecond, arrowEvent(term.KeyTab)}}, exitKeys()...))
	if f := lastFrame(out); strings.Contains(f, "simulated command") {
		t.Errorf("the default screen changed: it draws the command of a shell\n%s\nConsequence: the factory goldens move before the profile phase.\nRemedy: add the bind only; leave the default scene as it is.", f)
	}
}
