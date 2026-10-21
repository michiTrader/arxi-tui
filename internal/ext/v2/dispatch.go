package extv2

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

// UsageError is returned when a command is typed with fewer words than it needs. The
// operation did not run.
type UsageError struct{ Usage, Name string }

func (e *UsageError) Error() string {
	if e.Usage != "" {
		return fmt.Sprintf("/%s needs more: nothing ran. Usage: %s", e.Name, e.Usage)
	}
	return fmt.Sprintf("/%s needs more: nothing ran.", e.Name)
}

// ErrNotSigned is returned when an operation names a power the package did not declare.
// The validator refuses those documents; the runner checks again because the document
// it is given may not have come through the validator.
var ErrNotSigned = errors.New("not declared by the package")

// RunCommand runs a package command with the text typed after its name.
func (r *Runner) RunCommand(ctx context.Context, p *Package, c Command, args string) (Outcome, error) {
	args = strings.TrimSpace(args)
	words := strings.Fields(args)
	if len(words) < c.MinArgs {
		return Outcome{}, &UsageError{Usage: c.Usage, Name: c.Name}
	}
	in := Input{Args: args, Words: words}
	out, err := r.run(ctx, p, c.Op, opCtx{kind: ctxCommand, minArgs: c.MinArgs}, in)
	if err != nil {
		return Outcome{}, err
	}
	// What a command may hand back is narrower than what an outcome can carry: it has
	// no line to rewrite, and it may send to the model only with the permission.
	out.Verdict, out.Text = "", ""
	if out.Send != "" && !p.Permissions.Send {
		return Outcome{}, fmt.Errorf("%w: /%s tried to send text to the model, and package %q did not ask for that; the text was not sent", ErrNotSigned, c.Name, p.ID)
	}
	return out, nil
}

// MaxToolArgs bounds what the model may pass to a tool, in bytes.
const MaxToolArgs = 2000

// RunTool runs a tool with the text the model passed. It returns the text the model reads
// back. Like a command it fails before running anything when the words are too few; unlike
// one it never has a line to send or to rewrite, so anything but text is refused.
func (r *Runner) RunTool(ctx context.Context, p *Package, t Tool, args string) (string, error) {
	args = strings.TrimSpace(args)
	if len(args) > MaxToolArgs {
		return "", fmt.Errorf("the text is %d bytes, the most a tool takes is %d; pass less", len(args), MaxToolArgs)
	}
	words := strings.Fields(args)
	if len(words) < t.MinArgs {
		return "", fmt.Errorf("%s needs at least %d words in args (%s); nothing ran", t.Name, t.MinArgs, t.Usage)
	}
	out, err := r.run(ctx, p, t.Op, opCtx{kind: ctxTool, minArgs: t.MinArgs}, Input{Args: args, Words: words})
	if err != nil {
		return "", err
	}
	if out.Send != "" || out.Verdict != "" || out.Text != "" {
		return "", fmt.Errorf("%w: tool %s produced something other than text, and a tool can only return text; nothing was used", ErrNotSigned, t.Name)
	}
	return out.Say, nil
}

// RunRule runs one rule against the line the user typed.
func (r *Runner) RunRule(ctx context.Context, p *Package, rule Rule, line string) (Outcome, error) {
	return r.run(ctx, p, rule.Op, opCtx{kind: ctxRule}, Input{Line: line})
}

func (r *Runner) run(ctx context.Context, p *Package, o Op, oc opCtx, in Input) (Outcome, error) {
	switch o.Kind {
	case KindBuiltin:
		fn, ok := r.Builtins[o.Name]
		if !ok {
			return Outcome{}, fmt.Errorf("this app has no builtin %q, so nothing ran; the package needs a newer version of the app, or a different name", o.Name)
		}
		out, err := fn(ctx, in)
		if err != nil {
			return Outcome{}, err
		}
		out.Say = CleanText(out.Say, MaxSayRunes, MaxSayLines)
		out.Send = CleanText(strings.TrimSpace(out.Send), MaxLineRunes, 0)
		out.Text = CleanText(strings.TrimSpace(out.Text), MaxLineRunes, 0)
		out.Reason = CleanText(out.Reason, 400, 1)
		return out, nil
	case KindHTTP:
		return r.runHTTP(ctx, p, o, oc, in)
	case KindExec:
		return r.runExec(ctx, p, o, oc, in)
	}
	return Outcome{}, fmt.Errorf("operation kind %q cannot run", o.Kind)
}

// RunEvent runs the operation of an event. Nothing was typed, so it has no text of the
// user's to work with, and what comes back is only ever shown: an event cannot send to
// the model, rewrite a line, or start anything.
//
// answer is what the model just said, and only an event on turn.done may be given it
// (as ${text}). An event whose operation uses ${text} does not run when the answer is
// empty, because a hole is never filled with nothing: it returns no text and no error,
// since a turn that only used tools has nothing to react to and that is not a failure.
// An event on anything else that uses ${text} is refused, as the validator would have.
func (r *Runner) RunEvent(ctx context.Context, p *Package, e Event, answer string) (string, error) {
	in := Input{}
	if opUsesText(e.Op) {
		if e.On != EventTurnDone {
			return "", fmt.Errorf("%w: event %s uses ${text}, which only an event on %s has; nothing ran", ErrNotSigned, e.ID, EventTurnDone)
		}
		in.Text = CleanText(strings.TrimSpace(answer), MaxEventTextRunes, 0)
		if in.Text == "" {
			return "", nil
		}
	}
	out, err := r.run(ctx, p, e.Op, opCtx{kind: ctxEvent, event: e.On}, in)
	if err != nil {
		return "", err
	}
	return onlyText("event "+e.ID, out)
}

// opUsesText reports whether the operation places ${text} anywhere it can be filled: the
// url, the body or an argument. (A header or the program name may not hold any placeholder.)
func opUsesText(o Op) bool {
	for _, s := range append([]string{o.URL, o.Body}, o.Argv...) {
		parts, err := parseTemplate(s)
		if err != nil {
			continue
		}
		for _, pt := range parts {
			if pt.ph == textPlaceholder {
				return true
			}
		}
	}
	return false
}

// RunPanel runs the operation of a panel and returns its one line. A panel with no
// operation is filled by a service and has nothing to run.
func (r *Runner) RunPanel(ctx context.Context, p *Package, pn Panel) (string, error) {
	if pn.Op == nil {
		return "", fmt.Errorf("panel %s has no op; a service fills it", pn.ID)
	}
	out, err := r.run(ctx, p, *pn.Op, opCtx{kind: ctxPanel}, Input{})
	if err != nil {
		return "", err
	}
	text, err := onlyText("panel "+pn.ID, out)
	if err != nil {
		return "", err
	}
	return PanelLine(text), nil
}

// onlyText accepts an outcome only when it is text to show. What runs by itself has no
// line to rewrite and no right to send, so anything else is refused, not trimmed.
func onlyText(who string, out Outcome) (string, error) {
	if out.Send != "" || out.Verdict != "" || out.Text != "" {
		return "", fmt.Errorf("%w: %s produced something other than text, and what runs by itself can only show text; nothing was used", ErrNotSigned, who)
	}
	return out.Say, nil
}

// PanelLine makes text fit the one line a panel owns: the first line, cleaned, cut to
// MaxPanelRunes.
func PanelLine(s string) string {
	return CleanText(strings.TrimSpace(firstLine(strings.TrimSpace(s))), MaxPanelRunes, 1)
}
