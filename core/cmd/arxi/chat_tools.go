package main

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/michiTrader/arxi/internal/chattools"
	"github.com/michiTrader/arxi/internal/turn"
)

const (
	// maxToolRounds bounds how many times one answer may go back to the model
	// after running tools. A model that never stops asking is cut off.
	maxToolRounds = 16
	// maxToolOutputEvent caps the tool output carried on a notification; the
	// model still receives the whole (already capped) output.
	maxToolOutputEvent = 8 << 10
)

type toolsKey struct{}

// chatToolbox is what a chat turn carries when the caller gave it a folder: the
// read-only tools and where to report each call.
type chatToolbox struct {
	box    *chattools.Toolbox
	report func(chatToolNotification)
}

// withTools returns a context whose chat turn may look into dir.
func withTools(ctx context.Context, dir string, report func(chatToolNotification)) (context.Context, error) {
	box, err := chattools.New(dir)
	if err != nil {
		return ctx, badInvocation{fmt.Errorf("the working folder cannot be used: %w", err)}
	}
	return context.WithValue(ctx, toolsKey{}, &chatToolbox{box: box, report: report}), nil
}

func toolsFrom(ctx context.Context) *chatToolbox {
	tb, _ := ctx.Value(toolsKey{}).(*chatToolbox)
	return tb
}

// chatToolNotification is written to the connection for every tool call. It has
// a type and no id, so a client never mistakes it for a response; the model's
// own call id travels as call_id.
type chatToolNotification struct {
	Type    string `json:"type"`
	CallID  string `json:"call_id"`
	Name    string `json:"name"`
	Arg     string `json:"arg"`
	OK      bool   `json:"ok"`
	Summary string `json:"summary"`
	Output  string `json:"output,omitempty"`
}

// toolsHint tells the model what it can do and how paths are read.
func toolsHint(root string) string {
	return "You can look at the user's project with the tools list, read and grep. " +
		"Paths are relative to the project root (" + filepath.Base(root) + "). " +
		"Look before you answer questions about the code, and do not guess file contents."
}

// runToolLoop asks the model, runs the tools it requests, and asks again until
// it answers in plain text. complete performs one request (with its retries).
func runToolLoop(ctx context.Context, tb *chatToolbox, req *turn.Request,
	complete func(turn.Request) (turn.Response, error)) (turn.Response, error) {
	for _, d := range chattools.Definitions() {
		req.Tools = append(req.Tools, turn.ToolDefinition{Name: d.Name, Description: d.Description, InputSchema: d.Schema})
	}
	for round := 0; ; round++ {
		if round >= maxToolRounds {
			req.Tools = nil
			req.Messages = append(req.Messages, turn.Message{Role: turn.RoleUser, Content: []turn.ContentBlock{{
				Type: turn.BlockText,
				Text: "You have used all the tool calls allowed for one answer. Answer now with what you have found.",
			}}})
		}
		resp, err := complete(*req)
		if err != nil {
			return resp, err
		}
		var calls []*turn.ToolCall
		for _, b := range resp.Content {
			if b.Type == turn.BlockToolCall && b.ToolCall != nil {
				calls = append(calls, b.ToolCall)
			}
		}
		if len(calls) == 0 || resp.FinishReason == turn.FinishRefusal || len(req.Tools) == 0 {
			return resp, nil
		}
		if err := ctx.Err(); err != nil {
			return resp, err
		}
		req.Messages = append(req.Messages, turn.Message{Role: turn.RoleAssistant, Content: resp.Content})
		var results []turn.ContentBlock
		for _, call := range calls {
			results = append(results, turn.ContentBlock{Type: turn.BlockToolResult, ToolResult: runOneTool(tb, call)})
		}
		req.Messages = append(req.Messages, turn.Message{Role: turn.RoleTool, Content: results})
	}
}

// runOneTool runs a single call. A failure is not fatal: the model reads it as
// the result, so it can try another path or tell the user.
func runOneTool(tb *chatToolbox, call *turn.ToolCall) *turn.ToolResult {
	res, err := tb.box.Run(call.Name, call.Arguments)
	n := chatToolNotification{Type: "chat.tool", CallID: call.ID, Name: call.Name, Arg: res.Arg}
	if n.Arg == "" {
		n.Arg = argOf(call.Arguments)
	}
	out := &turn.ToolResult{CallID: call.ID}
	if err != nil {
		out.IsError = true
		out.Content = []turn.ContentBlock{{Type: turn.BlockText, Text: "error: " + err.Error()}}
		n.Summary = err.Error()
	} else {
		out.Content = []turn.ContentBlock{{Type: turn.BlockText, Text: res.Text}}
		n.OK, n.Summary, n.Output = true, res.Summary, clip(res.Text, maxToolOutputEvent)
	}
	if tb.report != nil {
		tb.report(n)
	}
	return out
}

// argOf picks the argument worth showing from a call's raw arguments.
func argOf(raw json.RawMessage) string {
	var a struct {
		Path    string `json:"path"`
		Pattern string `json:"pattern"`
	}
	_ = json.Unmarshal(raw, &a)
	if a.Path != "" {
		return a.Path
	}
	return a.Pattern
}

func clip(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return strings.ToValidUTF8(s[:n], "") + "\n[output cut]"
}
