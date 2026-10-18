package main

import (
	"context"
	"os"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/michiTrader/arxi_tui/internal/fold"
	"github.com/michiTrader/arxi_tui/internal/scene"
	"github.com/michiTrader/arxi_tui/internal/term"
	"github.com/michiTrader/arxi_tui/internal/theme"
)

// ---- helpers ---------------------------------------------------------------------

// barRe is the bar as the user reads it: "find: " or "find 2/5: word". The status row's git
// branch can hold the word "find", so the colon after it is part of the match.
var barRe = regexp.MustCompile(`find(?: \d+/\d+)?: `)

func hasBar(frame string) bool { return barRe.MatchString(frame) }

func findCtrlF() scheduledEvent { return findEv(ctrlCharEvent('f')) }

func findEv(ev term.Event) scheduledEvent { return scheduledEvent{120 * time.Millisecond, ev} }

func findOne(evs ...scheduledEvent) []scheduledEvent { return evs }

func findType(s string) []scheduledEvent {
	var out []scheduledEvent
	for _, r := range s {
		out = append(out, scheduledEvent{40 * time.Millisecond, keyEvent(r)})
	}
	return out
}

// findKeysOf joins the parts, lets the chat settle before the first key and quits at the end.
func findKeysOf(parts ...[]scheduledEvent) []scheduledEvent {
	var out []scheduledEvent
	for _, p := range parts {
		out = append(out, p...)
	}
	if len(out) > 0 {
		out[0].delay = 400 * time.Millisecond
	}
	return append(out, scrollQuit()...)
}

// findLast is the last frame that was drawn before the quit keys changed anything.
func findLast(frames []string) string {
	for i := len(frames) - 1; i >= 0; i-- {
		if !strings.Contains(frames[i], "Ctrl") {
			return frames[i]
		}
	}
	return frames[len(frames)-1]
}

// findFrameWith is the last frame that contains every one of the pieces.
func findFrameWith(frames []string, pieces ...string) (string, bool) {
	for i := len(frames) - 1; i >= 0; i-- {
		ok := true
		for _, p := range pieces {
			if !strings.Contains(frames[i], p) {
				ok = false
				break
			}
		}
		if ok {
			return frames[i], true
		}
	}
	return "", false
}

// chatRows is the frame without the rows that carry the bar, to compare what the chat shows.
func chatRows(f string) string {
	var keep []string
	for _, l := range strings.Split(f, "\n") {
		if !hasBar(l) {
			keep = append(keep, l)
		}
	}
	return strings.Join(keep, "\n")
}

// ---- the real loop ---------------------------------------------------------------

func TestLoopCtrlFOpensTheBarAndTypingJumpsToTheWord(t *testing.T) {
	frames := scrollLoop(t, findKeysOf(findOne(findCtrlF()), findType("line z")))
	if _, ok := findFrameWith(frames, "find: "); !ok {
		t.Fatal("ctrl+f did not open the bar")
	}
	f, ok := findFrameWith(frames, "find 2/2: line z")
	if !ok {
		t.Fatalf("the bar never counted the two hits of the word:\n%s", findLast(frames))
	}
	if !strings.Contains(f, "line Z") || !strings.Contains(f, scrollHint) {
		t.Errorf("the chat did not jump to the newest hit:\n%s", f)
	}
	if first := frames[0]; strings.Contains(first, "line Z") {
		t.Errorf("premise: the latest lines do not show the hit before the search:\n%s", first)
	}
}

func TestLoopEnterGoesToTheOlderHitAndDownComesBack(t *testing.T) {
	enter := findEv(enterEvent())
	down := findEv(arrowEvent(term.KeyDown))
	frames := scrollLoop(t, findKeysOf(findOne(findCtrlF()), findType("line z"), findOne(enter)))
	newest, ok1 := findFrameWith(frames, "find 2/2: line z")
	older, ok2 := findFrameWith(frames, "find 1/2: line z")
	if !ok1 || !ok2 {
		t.Fatalf("enter did not move to the older hit (2/2 seen %v, 1/2 seen %v)", ok1, ok2)
	}
	if chatRows(newest) == chatRows(older) {
		t.Error("the chat did not move to the older hit")
	}
	// The oldest hit is the end of the line: enter again stays on it.
	frames = scrollLoop(t, findKeysOf(findOne(findCtrlF()), findType("line z"), findOne(enter, enter, enter)))
	if f := findLast(frames); !strings.Contains(f, "find 1/2: line z") {
		t.Errorf("past the oldest hit the bar says something else:\n%s", f)
	}
	// Down walks back to the newest.
	frames = scrollLoop(t, findKeysOf(findOne(findCtrlF()), findType("line z"), findOne(enter, down)))
	if f := findLast(frames); !strings.Contains(f, "find 2/2: line z") {
		t.Errorf("down did not return to the newest hit:\n%s", f)
	}
	// ... and does not go below it.
	frames = scrollLoop(t, findKeysOf(findOne(findCtrlF()), findType("line z"), findOne(down, down)))
	if f := findLast(frames); !strings.Contains(f, "find 2/2: line z") {
		t.Errorf("down below the newest hit changed the bar:\n%s", f)
	}
}

func TestLoopTypingInTheBarNeverReachesTheDraft(t *testing.T) {
	t.Setenv(configDirEnv, t.TempDir())
	t.Setenv(historyEnv, t.TempDir())
	script := append(typed("hi"), findCtrlF())
	script = append(script, findType("abc")...)
	script = append(script, findEv(enterEvent()), findEv(arrowEvent(term.KeyBackspace)), findEv(arrowEvent(term.KeyEscape)))
	script = append(script, scheduledEvent{80 * time.Millisecond, enterEvent()})
	_, sub := runLoopDoc(t, factorySobria, append(script, exitKeys()...))
	if len(sub) != 1 || sub[0] != "hi" {
		t.Fatalf("submitted = %q; the draft must be exactly what was typed outside the bar", sub)
	}
}

func TestLoopEscClosesTheBarAndCtrlFBringsTheWordBack(t *testing.T) {
	esc := findEv(arrowEvent(term.KeyEscape))
	frames := scrollLoop(t, findKeysOf(findOne(findCtrlF()), findType("line z"), findOne(esc)))
	if f := findLast(frames); hasBar(f) {
		t.Errorf("esc left the bar on screen:\n%s", f)
	}
	frames = scrollLoop(t, findKeysOf(findOne(findCtrlF()), findType("line z"), findOne(esc, findCtrlF())))
	if f := findLast(frames); !strings.Contains(f, "find 2/2: line z") {
		t.Errorf("ctrl+f again did not look for the kept word:\n%s", f)
	}
}

func TestLoopCtrlFAgainClosesTheBar(t *testing.T) {
	frames := scrollLoop(t, findKeysOf(findOne(findCtrlF()), findType("line z"), findOne(findCtrlF())))
	if f := findLast(frames); hasBar(f) {
		t.Errorf("ctrl+f on an open bar did not close it:\n%s", f)
	}
}

func TestLoopAPasteIsTheWord(t *testing.T) {
	frames := scrollLoop(t, findKeysOf(findOne(findCtrlF(), findEv(pasteEvent("line z")))))
	f := findLast(frames)
	if !strings.Contains(f, "find 2/2: line z") {
		t.Errorf("a paste with the bar open did not become the word:\n%s", f)
	}
	if strings.Contains(f, "Pasted") {
		t.Errorf("the paste went to the draft as well:\n%s", f)
	}
}

func TestLoopAWordWithNoHitIsCountedAsZero(t *testing.T) {
	frames := scrollLoop(t, findKeysOf(findOne(findCtrlF()), findType("qqqq")))
	if f := findLast(frames); !strings.Contains(f, "find 0/0: qqqq") {
		t.Errorf("a word that is nowhere must read 0/0:\n%s", f)
	}
}

func TestLoopBarKeepsPageKeys(t *testing.T) {
	frames := scrollLoop(t, findKeysOf(findOne(findCtrlF()), findType("qq"), findOne(findEv(arrowEvent(term.KeyPgUp)))))
	f := findLast(frames)
	if !strings.Contains(f, scrollHint) || !strings.Contains(f, "find 0/0: qq") {
		t.Errorf("page up with the bar open must scroll and leave the bar alone:\n%s", f)
	}
}

func TestLoopAShortcutOnCtrlFBeatsTheFinder(t *testing.T) {
	// Only the named built-in scene reads the user's shortcuts, so the loop runs on that one.
	seedBehaviour(t, `{"keys":{"ctrl+f":"cmd:/effort high"}}`)
	frames := scrollLoopKeepingConfig(t, findKeysOf(findOne(findCtrlF()), findType("zz")))
	for _, f := range frames {
		if hasBar(f) {
			t.Fatalf("the user's own shortcut on ctrl+f lost to the finder:\n%s", f)
		}
	}
}

// The same loop without the shortcut opens the bar: the test above is not passing for some
// other reason.
func TestLoopWithoutThatShortcutTheNamedSceneOpensTheBar(t *testing.T) {
	t.Setenv(configDirEnv, t.TempDir())
	frames := scrollLoopKeepingConfig(t, findKeysOf(findOne(findCtrlF())))
	if _, ok := findFrameWith(frames, "find: "); !ok {
		t.Fatal("premise: the named scene opens the bar on ctrl+f")
	}
}

func TestLoopSlashMenuKeepsCtrlF(t *testing.T) {
	t.Setenv(configDirEnv, t.TempDir())
	t.Setenv(historyEnv, t.TempDir())
	out, _ := runLoopDoc(t, factorySobria, append(append(typed("/ef"), findCtrlF()), exitKeys()...))
	if hasBar(stripANSI(out)) {
		t.Error("ctrl+f opened the bar while the slash menu was open")
	}
}

func TestLoopASceneWithoutABarLeavesCtrlFAlone(t *testing.T) {
	t.Setenv(configDirEnv, t.TempDir())
	t.Setenv(historyEnv, t.TempDir())
	out, _ := runLoopDoc(t, factoryRAW, append(append(typed("ab"), findCtrlF()), exitKeys()...))
	if hasBar(stripANSI(out)) {
		t.Error("a scene with no find bar still opened one")
	}
}

// ---- the finder on its own -------------------------------------------------------

func TestFindBarIsOnlyAskedForByAPromptThatHasOne(t *testing.T) {
	for name, tc := range map[string]struct {
		doc  string
		want bool
	}{
		"the built-in scene":  {factorySobria, true},
		"a scene with no bar": {factoryRAW, false},
		"a suffix on another bind": {`{ "root": { "type": "stack", "children": [
			{ "type": "input", "bind": "user.input", "suffix": { "type": "text", "bind": "host.thinking" } } ] } }`, false},
	} {
		doc, err := scene.ParseDocument([]byte(tc.doc))
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if got := hasFindBar(doc); got != tc.want {
			t.Errorf("%s: hasFindBar = %v want %v", name, got, tc.want)
		}
	}
	if hasFindBar(nil) {
		t.Error("no scene, no bar")
	}
}

func TestFinderKeys(t *testing.T) {
	plain := func(ty term.KeyType) term.Key { return term.Key{Type: ty} }
	r := func(c rune, m term.Mod) term.Key { return term.Key{Type: term.KeyRunes, Runes: []rune{c}, Mod: m} }
	ctrlF := r('f', term.ModCtrl)

	var closed finder
	if !closed.owns(ctrlF, true) || closed.owns(ctrlF, false) {
		t.Error("a closed finder owns ctrl+f only when the prompt can open it")
	}
	if closed.owns(plain(term.KeyEnter), true) || closed.owns(r('a', 0), true) {
		t.Error("a closed finder must leave every other key alone")
	}
	open := finder{open: true}
	if !open.owns(ctrlF, false) {
		t.Error("an open finder must always be able to close")
	}
	for _, k := range []term.Key{plain(term.KeyEscape), plain(term.KeyEnter), plain(term.KeyUp), plain(term.KeyDown), plain(term.KeyBackspace), r('a', 0), r('A', term.ModShift)} {
		if !open.owns(k, true) {
			t.Errorf("an open finder must own %+v", k)
		}
	}
	for _, k := range []term.Key{
		{Type: term.KeyEnter, Mod: term.ModShift}, {Type: term.KeyUp, Mod: term.ModCtrl}, {Type: term.KeyPgUp},
		{Type: term.KeyEnd}, r('c', term.ModCtrl), r('a', term.ModAlt), {Type: term.KeyRunes},
	} {
		if open.owns(k, true) {
			t.Errorf("an open finder must leave %+v to the usual chain", k)
		}
	}

	var f finder
	f.key(ctrlF)
	if !f.open || f.jump {
		t.Fatalf("ctrl+f opens, and with no word there is nothing to jump to: %+v", f)
	}
	f.key(r('a', 0))
	f.key(r('b', 0))
	if f.word != "ab" || !f.jump {
		t.Fatalf("typing: %+v", f)
	}
	f.jump = false
	f.key(plain(term.KeyEnter))
	f.key(plain(term.KeyUp))
	if f.back != 2 || !f.jump {
		t.Errorf("enter and up go to older hits: %+v", f)
	}
	f.key(plain(term.KeyDown))
	f.key(plain(term.KeyDown))
	f.key(plain(term.KeyDown))
	if f.back != 0 {
		t.Errorf("down never goes below the newest hit: %+v", f)
	}
	f.back = 3
	f.key(plain(term.KeyBackspace))
	if f.word != "a" || f.back != 0 {
		t.Errorf("backspace deletes a rune and restarts at the newest hit: %+v", f)
	}
	f.key(plain(term.KeyBackspace))
	f.key(plain(term.KeyBackspace))
	if f.word != "" {
		t.Errorf("backspace on an empty word: %+v", f)
	}
	f.word = "keep"
	f.key(plain(term.KeyEscape))
	if f.open || f.word != "keep" {
		t.Errorf("esc closes and keeps the word: %+v", f)
	}
	f.key(ctrlF)
	if !f.open || !f.jump || f.back != 0 {
		t.Errorf("ctrl+f with a kept word looks for it at once: %+v", f)
	}
	f.key(ctrlF)
	if f.open {
		t.Error("ctrl+f on an open bar closes it")
	}
}

func TestFinderWordIsOneLineAndBounded(t *testing.T) {
	var f finder
	f.add("a\nb\r\tc\x01d\x7f")
	if f.word != "a b  cd" {
		t.Errorf("word = %q; control characters are dropped and line breaks become spaces", f.word)
	}
	f = finder{}
	f.add(strings.Repeat("x", findMaxWord))
	f.add("y")
	if got := len([]rune(f.word)); got != findMaxWord || strings.Contains(f.word, "y") {
		t.Errorf("the word is bounded to %d runes, got %d", findMaxWord, got)
	}
	f = finder{}
	f.add("\x01")
	if f.word != "" || f.jump {
		t.Errorf("nothing printable is nothing: %+v", f)
	}
	f = finder{}
	f.add(strings.Repeat("é", findMaxWord))
	if len([]rune(f.word)) != findMaxWord {
		t.Error("the bound counts runes, not bytes")
	}
}

func TestFinderLabel(t *testing.T) {
	if got := (&finder{word: "x", total: 3}).label(); got != "" {
		t.Errorf("a closed finder has no label, got %q", got)
	}
	if got := (&finder{open: true}).label(); got != "find: " {
		t.Errorf("no word: %q", got)
	}
	if got := (&finder{open: true, word: "ab", total: 5, back: 1}).label(); got != "find 4/5: ab " {
		t.Errorf("label = %q", got)
	}
	if got := (&finder{open: true, word: "ab", total: 0, back: 2}).label(); got != "find 0/0: ab " {
		t.Errorf("no hits must read 0/0 even with a stale back, got %q", got)
	}
	setTexts(t, "find.label", "buscar")
	if got := (&finder{open: true, word: "ab", total: 2}).label(); got != "buscar 2/2: ab " {
		t.Errorf("the label ignores the text registry: %q", got)
	}
}

func TestInputWrapRoomGivesTheBarItsCells(t *testing.T) {
	doc, err := scene.ParseDocument([]byte(factorySobria))
	if err != nil {
		t.Fatal(err)
	}
	base := inputWrapRoom(doc, 80, "")
	if got := inputWrapRoom(doc, 80, "find: "); got != base-6 {
		t.Errorf("room with a 6-cell bar = %d want %d", got, base-6)
	}
	if got := inputWrapRoom(doc, 80, strings.Repeat("x", 100)); got != base-40 {
		t.Errorf("a long bar takes half the pane at most: room %d want %d", got, base-40)
	}
	if got := inputWrapRoom(nil, 80, "find: "); got != 80 {
		t.Errorf("no scene: room %d want 80", got)
	}
}

// The caret is parked in the bar: the terminal is told, with the cursor-position escape the
// host really writes, to sit on the last column of the row. Nothing but the bar ever puts it
// there, so seeing that escape only while the bar is open is the proof.
func TestLoopTheTerminalCaretGoesToTheBar(t *testing.T) {
	t.Setenv(configDirEnv, t.TempDir())
	t.Setenv(historyEnv, t.TempDir())
	atEdge := regexp.MustCompile(`\x1b\[\d+;80H`)
	closed, _ := runLoopDoc(t, factorySobria, append(typed("hi"), exitKeys()...))
	if atEdge.MatchString(closed) {
		t.Fatal("premise: with no bar the caret is never on the last column")
	}
	open, _ := runLoopDoc(t, factorySobria, append(append(typed("hi"), findCtrlF()), append(typed("x"), exitKeys()...)...))
	if !atEdge.MatchString(open) {
		t.Error("with the bar open the terminal caret was never put on the bar")
	}
}

// The jump happens once per key, not once per repaint: a reader who looks for a word and then
// goes to the latest line (End) must stay there with the bar still open.
func TestLoopTheJumpHappensOnceNotOnEveryRepaint(t *testing.T) {
	frames := scrollLoop(t, findKeysOf(findOne(findCtrlF()), findType("line z"), findOne(findEv(arrowEvent(term.KeyEnd)))))
	f := findLast(frames)
	if !hasBar(f) {
		t.Fatalf("the bar closed on End:\n%s", f)
	}
	if strings.Contains(f, scrollHint) {
		t.Errorf("End did not take the chat to its latest line (the finder pulled it back to the hit):\n%s", f)
	}
}

// ---- an interface saved before the bar existed ------------------------------------

// savedBeforeTheBar is the built-in scene as it was before ctrl+f: what a user who ever
// changed anything with /ui has saved in ~/.arxi/scene.json.
func savedBeforeTheBar(t *testing.T) string {
	t.Helper()
	old := strings.Replace(factorySobria, `,
    "suffix": { "type": "text", "bind": "host.find", "style": {"style": "find.bar"} }`, "", 1)
	if old == factorySobria || strings.Contains(old, "host.find") {
		t.Fatal("premise: could not make the old scene")
	}
	if doc, err := scene.ParseDocument([]byte(old)); err != nil || hasFindBar(doc) {
		t.Fatalf("premise: the old scene must parse and have no bar (%v)", err)
	}
	return old
}

func saveScene(t *testing.T, js string) {
	t.Helper()
	t.Setenv(configDirEnv, t.TempDir())
	if err := os.MkdirAll(configDir(), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(userScenePath(), []byte(js), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestASavedInterfaceWithoutTheBarGetsIt(t *testing.T) {
	saveScene(t, savedBeforeTheBar(t))
	doc, notice, err := resolveBootScene(builtinScene)
	if err != nil || notice != "" {
		t.Fatalf("boot: %v %q", err, notice)
	}
	if !hasFindBar(doc) {
		t.Fatal("a scene saved before ctrl+f booted with no find bar: ctrl+f would do nothing")
	}
	if doc.Name() != userScenePath() || !persistsScene(doc) {
		t.Errorf("the scene must stay the user's saved one, name=%q", doc.Name())
	}
}

func TestASavedInterfaceThatDrawsItsOwnSuffixIsLeftAlone(t *testing.T) {
	own := strings.Replace(savedBeforeTheBar(t), `"type": "input"`, `"type": "input", "suffix": {"type":"text","text":"mine"}`, 1)
	doc, err := scene.ParseDocument([]byte(own))
	if err != nil || findUserInput(doc.Root).Suffix == nil {
		t.Fatalf("premise: %v", err)
	}
	saveScene(t, own)
	got, _, err := resolveBootScene(builtinScene)
	if err != nil {
		t.Fatal(err)
	}
	if hasFindBar(got) || findUserInput(got.Root).Suffix.Text != "mine" {
		t.Error("the user's own suffix was replaced by the find bar")
	}
}

func TestWithFindBarLeavesAScenelessOrBarredDocumentAlone(t *testing.T) {
	if withFindBar(nil) != nil {
		t.Error("nil stays nil")
	}
	doc, _ := scene.ParseDocument([]byte(factorySobria))
	if withFindBar(doc) != doc {
		t.Error("a scene that already has the bar must be returned as it is")
	}
	raw, _ := scene.ParseDocument([]byte(`{ "root": { "type": "stack", "children": [ { "type": "text", "text": "hi" } ] } }`))
	if withFindBar(raw) != raw {
		t.Error("a scene with no prompt has nowhere for the bar")
	}
}

// The whole path a user walks: the old saved scene on disk, the real loop, ctrl+f.
func TestLoopCtrlFOpensTheBarOnAnInterfaceSavedBeforeIt(t *testing.T) {
	saveScene(t, savedBeforeTheBar(t))
	doc, _, err := resolveBootScene(builtinScene)
	if err != nil {
		t.Fatal(err)
	}
	frames := loopFrames(t, doc, findKeysOf(findOne(findCtrlF())))
	if _, ok := findFrameWith(frames, "find: "); !ok {
		t.Fatalf("ctrl+f did nothing on the saved interface:\n%s", findLast(frames))
	}
}

// A scene that has no bar at all says why, instead of nothing.
func TestLoopCtrlFOnASceneWithoutABarSaysWhy(t *testing.T) {
	t.Setenv(configDirEnv, t.TempDir())
	doc, err := scene.ParseDocument([]byte(factoryRAW))
	if err != nil {
		t.Fatal(err)
	}
	frames := loopFrames(t, doc, findKeysOf(findOne(findCtrlF())))
	if _, ok := findFrameWith(frames, "ctrl+f needs the find bar"); !ok {
		t.Fatalf("ctrl+f on a scene with no bar was silent:\n%s", findLast(frames))
	}
}

// loopFrames is scrollLoopKeepingConfig for a document the test chose.
func loopFrames(t *testing.T, doc *scene.Document, keys []scheduledEvent) []string {
	t.Helper()
	drv := newCompactDriver(1_000_000, 2)
	var long string
	for i := 0; i < 60; i++ {
		long += "line " + string(rune('A'+i%26)) + "\n\n"
	}
	for i, e := range []fold.Event{
		{Type: "run.prompt", Actor: "assistant", Payload: map[string]any{"text": "go"}},
		{Type: "llm.response", Actor: "assistant", Payload: map[string]any{"text": long, "model": "openai/gpt-4o",
			"tokens_in": float64(10), "tokens_out": float64(5), "context_tokens": float64(15)}},
		{Type: "agent.turn_done", Actor: "assistant"},
	} {
		e.Seq = int64(3000 + i)
		drv.evCh <- e
	}
	tty := newFakeTTY(100, 20, keys)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := loop(ctx, tty, doc, theme.SOBRIA(), drv.evCh, drv, ""); err != nil {
		t.Fatalf("loop: %v", err)
	}
	tty.mu.Lock()
	defer tty.mu.Unlock()
	out := make([]string, len(tty.frames))
	for i, f := range tty.frames {
		out[i] = stripANSI(f)
	}
	return out
}
