package main

import (
	"context"
	"encoding/json"
	"errors"
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

// What a turn may do to files without being stopped: the core's own three policies.
// "deny" (the default) gives the model the read tools only.
const (
	editsDeny  = "deny"
	editsAsk   = "ask"
	editsAllow = "allow"
)

// chatToolbox is what a chat turn carries when the caller gave it a folder: the
// tools, where to report each call, and what to do about a change to a file.
type chatToolbox struct {
	box    *chattools.Toolbox
	report func(chatToolNotification)
	// edits is the policy for write and edit: deny (not offered), ask (the user
	// sees the diff and decides first) or allow.
	edits string
	// runs is the same for the run tool: deny (not offered), ask (the user sees the
	// command and decides first) or allow.
	runs string
	// ask puts a change to the user. It is nil when the connection cannot ask, and
	// a change that needs asking is then refused.
	ask func(chatApprovalNotification) (bool, error)
}

// withTools returns a context whose chat turn may look into dir, and change files
// there as far as edits (deny, ask or allow) permits. ask is how a change reaches the
// user under "ask".
func withTools(ctx context.Context, dir, edits string, ask func(chatApprovalNotification) (bool, error),
	report func(chatToolNotification)) (context.Context, error) {
	switch edits {
	case "", editsDeny:
		edits = editsDeny
	case editsAsk, editsAllow:
	default:
		return ctx, badInvocation{fmt.Errorf("edits must be deny, ask or allow, not %q", edits)}
	}
	box, err := chattools.New(dir)
	if err != nil {
		return ctx, badInvocation{fmt.Errorf("the working folder cannot be used: %w", err)}
	}
	if edits != editsDeny {
		box = box.WithEdits()
	}
	return context.WithValue(ctx, toolsKey{}, &chatToolbox{box: box, report: report, edits: edits, runs: editsDeny, ask: ask}), nil
}

// withRuns lets the turn's model run commands in the folder withTools opened, as far
// as runs (deny, ask or allow) permits. It is a second step so that a caller that
// never heard of commands keeps the toolbox it had.
func withRuns(ctx context.Context, runs string) (context.Context, error) {
	tb := toolsFrom(ctx)
	switch runs {
	case "", editsDeny:
		return ctx, nil
	case editsAsk, editsAllow:
	default:
		return ctx, badInvocation{fmt.Errorf("runs must be deny, ask or allow, not %q", runs)}
	}
	if tb == nil {
		return ctx, badInvocation{errors.New("runs needs a workdir to run the commands in")}
	}
	c := *tb
	c.box, c.runs = tb.box.WithRuns(), runs
	return context.WithValue(ctx, toolsKey{}, &c), nil
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
	// Diff is, for a change to a file, what changed (see chattools.Result.Diff).
	Diff string `json:"diff,omitempty"`
}

// toolsHint tells the model what it can do and how paths are read.
func toolsHint(root, edits string) string {
	h := "You can look at the user's project with the tools list, read and grep. " +
		"Paths are relative to the project root (" + filepath.Base(root) + "). " +
		"Look before you answer questions about the code, and do not guess file contents."
	if edits != editsDeny {
		h += " You can change files with edit (replace text that appears once) and write (create or replace a file); " +
			"read a file before you edit it. The user may decline a change: if so, do not try the same change again, " +
			"say what you would have done and ask what they prefer."
	}
	return h
}

// runsHint tells the model about the run tool, when it has one.
func runsHint(runs string) string {
	if runs == editsDeny || runs == "" {
		return ""
	}
	return "You can run shell commands in the project folder with run (the shell is " + chattools.ShellName() + "). " +
		"Commands have no keyboard, so never start anything that waits for input or runs forever. " +
		"Prefer read, grep and edit over cat, grep and sed. If the user declines a command, do not try it again: say what you wanted to learn and ask."
}

// definitions are the tools offered to the model under this toolbox's policy.
func (tb *chatToolbox) definitions() []turn.ToolDefinition {
	defs := chattools.Definitions()
	if tb.edits != editsDeny {
		defs = append(defs, chattools.EditDefinitions()...)
	}
	if tb.runs == editsAsk || tb.runs == editsAllow {
		defs = append(defs, chattools.RunDefinitions()...)
	}
	out := make([]turn.ToolDefinition, 0, len(defs))
	for _, d := range defs {
		out = append(out, turn.ToolDefinition{Name: d.Name, Description: d.Description, InputSchema: d.Schema})
	}
	return out
}

// runToolLoop asks the model, runs the tools it requests, and asks again until
// it answers in plain text. complete performs one request (with its retries).
func runToolLoop(ctx context.Context, tb *chatToolbox, req *turn.Request,
	complete func(turn.Request) (turn.Response, error)) (turn.Response, error) {
	req.Tools = append(req.Tools, tb.definitions()...)
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
			res, err := runOneTool(tb, call)
			if err != nil {
				// The user could not be asked (the connection is gone): the turn is over.
				return resp, err
			}
			results = append(results, turn.ContentBlock{Type: turn.BlockToolResult, ToolResult: res})
		}
		req.Messages = append(req.Messages, turn.Message{Role: turn.RoleTool, Content: results})
	}
}

// runOneTool runs a single call. A failure is not fatal: the model reads it as the
// result, so it can try another path or tell the user. The one error returned is
// fatal: a change that had to be put to the user, who could not be reached.
func runOneTool(tb *chatToolbox, call *turn.ToolCall) (*turn.ToolResult, error) {
	n := chatToolNotification{Type: "chat.tool", CallID: call.ID, Name: call.Name, Arg: argOf(call.Arguments)}
	out := &turn.ToolResult{CallID: call.ID}
	fail := func(msg string) (*turn.ToolResult, error) {
		out.IsError = true
		out.Content = []turn.ContentBlock{{Type: turn.BlockText, Text: "error: " + msg}}
		n.Summary = msg
		if tb.report != nil {
			tb.report(n)
		}
		return out, nil
	}

	switch {
	case chattools.Runs(call.Name):
		if tb.runs == editsDeny {
			return fail(call.Name + " is not available in this mode: the model may not run commands")
		}
		prev, err := tb.box.PreviewRun(call.Arguments)
		if err != nil {
			return fail(err.Error())
		}
		n.Arg = prev.Arg
		// A command is always put to the user under ask: unlike a change to a file, there
		// is no diff that could turn out to be empty.
		if tb.runs == editsAsk {
			if tb.ask == nil {
				return fail("this command needs the user's approval and this connection cannot ask for it, so it was not run")
			}
			allowed, err := tb.ask(chatApprovalNotification{
				CallID: call.ID, Name: call.Name, Arg: prev.Arg, Summary: prev.Summary,
			})
			if err != nil {
				return nil, err
			}
			if !allowed {
				return fail("the user did not allow this command, so it was not run")
			}
		}
	case chattools.Mutating(call.Name):
		if tb.edits == editsDeny {
			return fail(call.Name + " is not available in this mode: the model may look at files but not change them")
		}
		prev, err := tb.box.Preview(call.Name, call.Arguments)
		if err != nil {
			return fail(err.Error())
		}
		n.Arg = prev.Arg
		// A change that changes nothing needs no permission.
		if tb.edits == editsAsk && prev.Diff != "" {
			if tb.ask == nil {
				return fail("this change needs the user's approval and this connection cannot ask for it, so it was not made")
			}
			allowed, err := tb.ask(chatApprovalNotification{
				CallID: call.ID, Name: call.Name, Arg: prev.Arg, Summary: prev.Summary, Diff: prev.Diff,
			})
			if err != nil {
				return nil, err
			}
			if !allowed {
				return fail("the user did not allow this change, so the file was not changed")
			}
		}
	}

	res, err := tb.box.Run(call.Name, call.Arguments)
	if res.Arg != "" {
		n.Arg = res.Arg
	}
	if err != nil {
		return fail(err.Error())
	}
	out.Content = []turn.ContentBlock{{Type: turn.BlockText, Text: res.Text}}
	// A command that ran and failed is still a result for the model to read, but the
	// user is shown it as a failure.
	out.IsError = res.Failed
	n.OK, n.Summary, n.Output, n.Diff = !res.Failed, res.Summary, clip(res.Text, maxToolOutputEvent), res.Diff
	if tb.report != nil {
		tb.report(n)
	}
	return out, nil
}

// argOf picks the argument worth showing from a call's raw arguments.
func argOf(raw json.RawMessage) string {
	var a struct {
		Path    string `json:"path"`
		Pattern string `json:"pattern"`
		Command string `json:"command"`
	}
	_ = json.Unmarshal(raw, &a)
	if a.Command != "" {
		return a.Command
	}
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
