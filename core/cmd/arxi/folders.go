package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/michiTrader/arxi/internal/chattools"
	"github.com/michiTrader/arxi/internal/turn"
)

// Folders beyond the project's.
//
// The file tools stay inside the project (what they read goes to a remote provider),
// and the model used to be told "outside the project folder, so it is not available"
// with nothing to do about it, while the user only needed the library next to the
// script. The way out is a question, not a setting: when a call names a path that is
// outside, the user is asked once to open the folder it belongs to. The folder is the
// nearest repository or project above the path, so one answer covers its siblings, and
// never a drive or the home folder (chattools.SuggestFolder).
//
// The core keeps nothing between turns, so the client remembers what the user opened
// and sends it back as `folders` on the next turn.

// withFolders opens the folders the client already holds for this session and sets the
// policy for the ones not yet opened. It is a step on top of withTools.
func withFolders(ctx context.Context, rawFolders, outside string) (context.Context, error) {
	tb := toolsFrom(ctx)
	switch outside {
	case "", editsDeny:
		outside = editsDeny
	case editsAsk, editsAllow:
	default:
		return ctx, badInvocation{fmt.Errorf("outside must be deny, ask or allow, not %q", outside)}
	}
	if tb == nil {
		if rawFolders != "" || outside != editsDeny {
			return ctx, badInvocation{errors.New("folders and outside need a workdir: they travel with the tools")}
		}
		return ctx, nil
	}
	c := *tb
	c.outside = outside
	if rawFolders != "" {
		var dirs []string
		if err := json.Unmarshal([]byte(rawFolders), &dirs); err != nil {
			return ctx, badInvocation{fmt.Errorf("folders is not a JSON list of paths: %w", err)}
		}
		for _, d := range dirs {
			box, err := c.box.WithDir(d)
			if err != nil {
				// A folder that vanished or became too wide is not carried over; the
				// model is asked again if it needs it.
				continue
			}
			c.box = box
		}
	}
	return context.WithValue(ctx, toolsKey{}, &c), nil
}

// openFolderFor is called before a file tool runs. When the call names a path outside
// the folders in use it settles, according to the policy, whether the folder is opened.
// A nil result and nil error means: go on and run the call (the path is inside, or the
// folder was just opened). Otherwise it is the call's result: the refusal.
func openFolderFor(tb *chatToolbox, call *turn.ToolCall,
	fail func(string) (*turn.ToolResult, error)) (*turn.ToolResult, error) {
	path := chattools.PathArg(call.Arguments)
	if !tb.box.Outside(path) {
		return nil, nil
	}
	// With no policy the old wording stands: the file tool itself refuses.
	if tb.outside != editsAsk && tb.outside != editsAllow {
		return nil, nil
	}
	dir, err := tb.box.SuggestFolder(path)
	if err != nil {
		return fail(err.Error())
	}
	if tb.outside == editsAsk {
		if tb.ask == nil {
			return fail(path + " is outside the project and this connection cannot ask the user to open it")
		}
		allowed, err := tb.ask(chatApprovalNotification{
			CallID: call.ID, Name: "folder", Arg: dir,
			Summary: "Let the model use this folder for the rest of the session (" + call.Name + " " + path + ")",
		})
		if err != nil {
			return nil, err
		}
		if !allowed {
			return fail("the user did not open " + dir + ", so " + path + " is not available")
		}
	}
	box, err := tb.box.WithDir(dir)
	if err != nil {
		return fail(err.Error())
	}
	tb.box = box
	return nil, nil
}
