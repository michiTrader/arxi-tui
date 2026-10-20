package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/michiTrader/arxi_tui/internal/defaultscene"
	"github.com/michiTrader/arxi_tui/internal/driver"
	"github.com/michiTrader/arxi_tui/internal/engine"
	"github.com/michiTrader/arxi_tui/internal/fold"
	"github.com/michiTrader/arxi_tui/internal/scene"
)

// This file is ui_preview: the model looking at the screen it is editing.
//
// # Why it exists
//
// ui_edit answers "applied and saved", and that says the document was valid, not that the
// screen looks right. A model that cannot see the result says "done" about a layout that
// has the bar under the wrong row or a row cut at the edge; the user finds out, and asks
// again. With ui_preview the model draws the screen as text at a width and state it
// chooses (a turn in flight, three shells, a long line typed, a chat that is long
// enough to scroll) and fixes what it sees before it speaks.
//
// # Why a proposal can be previewed
//
// Passing change draws the screen as it would be after that ui_edit, through the same
// draft and the same validator, and applies nothing and asks nothing. The user is asked
// once, about a change the model has already looked at.
//
// # What it does not show, said in the answer
//
// Colours (plain text has none), and reworded texts of a proposal (the words in use are a
// table the loop is reading while the turn runs, so a proposal cannot swap them for one
// frame). The answer says so when it applies.

const (
	previewMinW, previewMaxW = 20, 200
	previewMinH, previewMaxH = 6, 80
	previewDefW, previewDefH = 80, 24
	// previewMaxChat caps the lines a call may put in the chat: the answer is read by a
	// model and is paid for in tokens.
	previewMaxChat = 40
)

type previewArgs struct {
	Width   int             `json:"width"`
	Height  int             `json:"height"`
	Working bool            `json:"working"`
	Shells  int             `json:"shells"`
	Input   string          `json:"input"`
	Inspect bool            `json:"inspect"`
	Keys    json.RawMessage `json:"keys"`
	Chat    []previewLine   `json:"chat"`
	Change  json.RawMessage `json:"change"`
	// Save, Expect and Forget keep and delete a test of the interface (ui_scenarios.go).
	Save   string   `json:"save"`
	Expect []string `json:"expect"`
	Forget string   `json:"forget"`
}

type previewLine struct {
	Role string `json:"role"`
	Text string `json:"text"`
}

// preview draws the screen on screen, or the one a proposed change would give.
func (b *uiBridge) preview(ctx context.Context, raw json.RawMessage) (res driver.ClientToolResult) {
	fail := func(text string) driver.ClientToolResult {
		return driver.ClientToolResult{Arg: "interface", Summary: "The preview was not drawn", Text: text}
	}
	var a previewArgs
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &a); err != nil {
			return fail("ui_preview takes {width, height, working, shells, input, inspect, keys, chat, change}: " + err.Error())
		}
	}
	if a.Width == 0 {
		a.Width = previewDefW
	}
	if a.Height == 0 {
		a.Height = previewDefH
	}
	if msg := previewFactsError(a.Width, a.Height, a.Shells, a.Chat); msg != "" {
		return fail(msg)
	}
	doc, _, _, _, _ := b.snapshot()
	if doc == nil {
		return fail("the interface is not on screen yet; try again")
	}
	notes := []string{"plain text; colours are not shown"}
	_, _, curColors, _, curTexts := b.snapshot()
	layers := uiDraft{doc: doc, colors: curColors, texts: curTexts, beh: b.behaviourNow()}
	keys, kerr := simKeysArg(a.Keys)
	if kerr != nil {
		return fail(kerr.Error())
	}
	if a.Save != "" || a.Forget != "" {
		return b.keepScenario(ctx, a, keys)
	}
	if len(a.Expect) > 0 {
		return fail("expect belongs to a test: pass save:\"<name>\" with it, or drop it")
	}
	if len(keys) > 0 {
		if _, err := parseSimKeys(keys); err != nil {
			return fail(err.Error())
		}
		if a.Inspect {
			return fail("inspect describes the screen before any key; ask for it without keys")
		}
	}
	if len(a.Change) > 0 && string(a.Change) != "null" {
		var d uiDraft
		if r := b.editFlow(ctx, a.Change, nil, &d); !r.OK {
			return r
		}
		if d.doc != nil {
			doc = d.doc
			layers = d
		}
		if d.words && len(keys) == 0 {
			notes = append(notes, "the proposal rewords texts; the screen below still has the words in use")
		}
		notes = append([]string{"AS IT WOULD BE after the change; nothing was applied"}, notes...)
	}
	if len(keys) > 0 {
		return b.pressKeys(ctx, a, keys, layers, notes)
	}
	defer func() {
		// A scene the engine cannot draw must come back as a refusal the model can read,
		// not as the end of the turn.
		if p := recover(); p != nil {
			res = fail(fmt.Sprintf("the screen could not be drawn: %v", p))
		}
	}()

	state := previewState(a)
	r := engine.Renderer{Width: a.Width, Height: a.Height}
	r.Look = chatLook()
	r.Narrow = narrowMode()
	r.PromptStyle = loadPromptStyle(stylePath())
	r.InputMaxRows = settingNumber("input.max_rows", 0, minInputRows, maxInputRows)
	frame := r.RenderFrame(doc, state)

	var out strings.Builder
	fmt.Fprintf(&out, "screen %dx%d", a.Width, a.Height)
	if a.Working {
		out.WriteString(", turn in flight")
	}
	if a.Shells > 0 {
		out.WriteString(", " + shellsLabel(a.Shells))
	}
	out.WriteString(" (" + strings.Join(notes, "; ") + ")\n")
	out.WriteString(frame.Plain())
	out.WriteString("\n")
	if a.Inspect {
		out.WriteString(inspectText(doc, state))
	}
	if bad := frame.Overflow(); len(bad) > 0 {
		fmt.Fprintf(&out, "WARNING: rows %v are wider than %d columns and the terminal will wrap or cut them.\n", oneBased(bad), a.Width)
	}
	return driver.ClientToolResult{OK: true, Text: out.String(), Arg: "interface", Summary: "Looked at the screen"}
}

// previewFactsError is what is wrong with the facts a preview, or a saved test, is asked
// to draw, "" when they can be drawn.
func previewFactsError(width, height, shells int, chat []previewLine) string {
	if width < previewMinW || width > previewMaxW || height < previewMinH || height > previewMaxH {
		return fmt.Sprintf("width must be %d..%d and height %d..%d (a phone is about 40x20)", previewMinW, previewMaxW, previewMinH, previewMaxH)
	}
	if shells < 0 || shells > maxShells {
		return fmt.Sprintf("shells must be 0..%d", maxShells)
	}
	if len(chat) > previewMaxChat {
		return fmt.Sprintf("chat takes at most %d lines", previewMaxChat)
	}
	for _, l := range chat {
		switch strings.ToLower(strings.TrimSpace(l.Role)) {
		case "user", "assistant", "note", "error", "warn":
		default:
			return fmt.Sprintf("chat role %q is not one of user, assistant, note, error, warn", l.Role)
		}
	}
	return ""
}

// previewState is the folded state a screen with these facts would be drawn from. The
// host fields are the ones the loop fills on every repaint (main.go), set the way it
// sets them.
func previewState(a previewArgs) fold.State {
	st := fold.Fold(nil)
	for _, l := range a.Chat {
		st.History = append(st.History, fold.ChatLine{Role: strings.ToLower(strings.TrimSpace(l.Role)), Text: l.Text})
	}
	st.AgentWorking = a.Working
	if a.Working {
		st.HostThinking = thinkingLabel(3_000_000_000)
	}
	st.UserInput = a.Input
	st.UserInputCaret = len([]rune(a.Input))
	cwd, _ := os.Getwd()
	st.HostCwd = shortCwd(cwd)
	st.HostMode = "ask"
	st.HostShells = shellsLabel(a.Shells)
	var names []string
	for i := 1; i <= a.Shells; i++ {
		names = append(names, fmt.Sprintf("shell %d", i))
	}
	st.HostShellList = strings.Join(names, " · ")
	st.HostShellRows = shellRows(newSimShells(a.Shells, func(string) {}))
	return st
}

func oneBased(rows []int) []int {
	out := make([]int, len(rows))
	for i, r := range rows {
		out[i] = r + 1
	}
	return out
}

// pressKeys runs the keys in the isolated copy (ui_simulate.go) and writes what came out.
func (b *uiBridge) pressKeys(ctx context.Context, a previewArgs, keys []string, l uiDraft, notes []string) driver.ClientToolResult {
	cwd, _ := os.Getwd()
	req := simRequest{Width: a.Width, Height: a.Height, Shells: a.Shells, Working: a.Working,
		Input: a.Input, Chat: a.Chat, Keys: keys, Cwd: cwd,
		State: interfaceOf(l.doc, l.colors, l.texts, l.beh)}
	res, err := simulate(ctx, req)
	if err != nil {
		return driver.ClientToolResult{Arg: "interface", Summary: "The keys were not pressed", Text: err.Error()}
	}
	if res.Err != "" {
		return driver.ClientToolResult{Arg: "interface", Summary: "The keys were not pressed", Text: res.Err}
	}
	var out strings.Builder
	fmt.Fprintf(&out, "screen %dx%d after pressing: %s (%s)\n", a.Width, a.Height, strings.Join(keys, " "),
		strings.Join(append(notes, "a copy of the program ran the keys: nothing was sent, run or opened for real; packages are not in it"), "; "))
	out.WriteString(simBody(res, keys))
	out.WriteString("\n")
	if res.Notice != "" {
		fmt.Fprintf(&out, "NOTE: %s\n", res.Notice)
	}
	return driver.ClientToolResult{OK: true, Text: out.String(), Arg: "interface", Summary: "Pressed keys and looked at the screen"}
}

func quoteAll(list []string) string {
	q := make([]string, len(list))
	for i, s := range list {
		q[i] = quoteShort(s)
	}
	return strings.Join(q, ", ")
}

// interfaceOf is the saved form of an interface held in memory: what saveInterfaceWith
// writes, so the copy finds it as the real program would at start-up.
func interfaceOf(doc *scene.Document, u userTokens, tx userTexts, bh behaviour) interfaceState {
	var st interfaceState
	if doc != nil && !sameDocument(doc.Source(), defaultscene.JSON) {
		st.Scene = string(doc.Source())
	}
	if len(u) > 0 {
		st.Tokens = string(encodeUserTokens(u))
	}
	if len(tx) > 0 {
		st.Texts = string(encodeUserTexts(tx))
	}
	if !bh.empty() {
		st.Behaviour = string(encodeBehaviour(bh))
	}
	return st
}

// simBody is what the keys did, as the model reads it and as a saved test is checked
// against: the screen after them, and a line for each thing that left the program.
func simBody(res simResult, keys []string) string {
	var out strings.Builder
	out.WriteString(res.Screen + "\n")
	if res.QuitAt > 0 {
		fmt.Fprintf(&out, "THE PROGRAM ENDED at key %d (%s).\n", res.QuitAt, keys[res.QuitAt-1])
	}
	if len(res.Submitted) > 0 {
		fmt.Fprintf(&out, "SENT TO THE MODEL (%d): %s\n", len(res.Submitted), quoteAll(res.Submitted))
	}
	if len(res.Opened) > 0 {
		fmt.Fprintf(&out, "BROWSER WAS ASKED TO OPEN: %s\n", quoteAll(res.Opened))
	}
	if len(res.Copied) > 0 {
		fmt.Fprintf(&out, "CLIPBOARD WAS GIVEN: %s\n", quoteAll(res.Copied))
	}
	return strings.TrimRight(out.String(), "\n")
}

// keepScenario saves, replaces or deletes a test of the interface, and says how the test
// does on the interface on screen now.
func (b *uiBridge) keepScenario(ctx context.Context, a previewArgs, keys []string) driver.ClientToolResult {
	fail := func(text string) driver.ClientToolResult {
		return driver.ClientToolResult{Arg: "interface", Summary: "The test was not kept", Text: text}
	}
	b.mu.Lock()
	policy := b.policy
	b.mu.Unlock()
	if policy == policyDeny {
		return fail("tests may not be changed in plan mode; say which test you would save instead")
	}
	if a.Save != "" && a.Forget != "" {
		return fail("pass save or forget, not both")
	}
	if len(a.Change) > 0 && string(a.Change) != "null" {
		return fail("a test is kept for the interface on screen: apply the change with ui_edit first (or save the test before it, to watch it fail and then pass)")
	}
	if a.Inspect {
		return fail("inspect describes a screen; it is not part of a test")
	}
	list, err := loadScenarios()
	if err != nil {
		return fail(err.Error() + "\nFix or delete that file; no test was changed.")
	}
	if a.Forget != "" {
		for i, s := range list {
			if s.Name == a.Forget {
				list = append(list[:i:i], list[i+1:]...)
				if err := saveScenarios(list); err != nil {
					return fail("the test could not be deleted: " + err.Error())
				}
				return driver.ClientToolResult{OK: true, Arg: "interface", Summary: "Deleted a test", Text: fmt.Sprintf("deleted the test %q; %d left", a.Forget, len(list))}
			}
		}
		return fail(fmt.Sprintf("there is no test named %q; the saved ones are listed in ui_guide", a.Forget))
	}
	s := scenario{Name: a.Save, Width: a.Width, Height: a.Height, Working: a.Working, Shells: a.Shells,
		Input: a.Input, Chat: a.Chat, Keys: keys, Expect: a.Expect}
	if err := s.validate(); err != nil {
		return fail(err.Error())
	}
	replaced := false
	for i := range list {
		if list[i].Name == s.Name {
			list[i], replaced = s, true
		}
	}
	if !replaced {
		if len(list) >= maxScenarios {
			return fail(fmt.Sprintf("there are already %d tests, the most; forget one first (forget:\"<name>\")", maxScenarios))
		}
		list = append(list, s)
	}
	if err := saveScenarios(list); err != nil {
		return fail("the test could not be saved: " + err.Error())
	}
	doc, _, colors, _, texts := b.snapshot()
	out := runScenario(ctx, interfaceOf(doc, colors, texts, b.behaviourNow()), s)
	verb := "saved"
	if replaced {
		verb = "replaced"
	}
	var text strings.Builder
	fmt.Fprintf(&text, "%s the test %q (%d saved). It is run again after every ui_edit you make.\n", verb, s.Name, len(list))
	switch {
	case out.err != "":
		fmt.Fprintf(&text, "It could not be run now: %s", out.err)
	case out.passed():
		text.WriteString("It PASSES on the interface on screen now.")
	default:
		fmt.Fprintf(&text, "It FAILS on the interface on screen now: %s\nThis is what it saw:\n%s", strings.Join(out.failures, "; "), out.body)
	}
	return driver.ClientToolResult{OK: true, Arg: "interface", Summary: verb + " a test", Text: text.String()}
}
