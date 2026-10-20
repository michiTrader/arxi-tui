package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/michiTrader/arxi_tui/internal/driver"
	"github.com/michiTrader/arxi_tui/internal/engine"
	"github.com/michiTrader/arxi_tui/internal/fold"
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
			return fail("ui_preview takes {width, height, working, shells, input, inspect, chat, change}: " + err.Error())
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
	if len(a.Change) > 0 && string(a.Change) != "null" {
		var d uiDraft
		if r := b.editFlow(ctx, a.Change, nil, &d); !r.OK {
			return r
		}
		if d.doc != nil {
			doc = d.doc
		}
		if d.words {
			notes = append(notes, "the proposal rewords texts; the screen below still has the words in use")
		}
		notes = append([]string{"AS IT WOULD BE after the change; nothing was applied"}, notes...)
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
