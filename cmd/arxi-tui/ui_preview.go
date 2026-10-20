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
	if a.Width < previewMinW || a.Width > previewMaxW || a.Height < previewMinH || a.Height > previewMaxH {
		return fail(fmt.Sprintf("width must be %d..%d and height %d..%d (a phone is about 40x20)", previewMinW, previewMaxW, previewMinH, previewMaxH))
	}
	if a.Shells < 0 || a.Shells > maxShells {
		return fail(fmt.Sprintf("shells must be 0..%d", maxShells))
	}
	if len(a.Chat) > previewMaxChat {
		return fail(fmt.Sprintf("chat takes at most %d lines", previewMaxChat))
	}
	for _, l := range a.Chat {
		switch strings.ToLower(strings.TrimSpace(l.Role)) {
		case "user", "assistant", "note", "error", "warn":
		default:
			return fail(fmt.Sprintf("chat role %q is not one of user, assistant, note, error, warn", l.Role))
		}
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
