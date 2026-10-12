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

// RunEvent runs the operation of an event. Nothing was typed, so it has no text to work
// with, and what comes back is only ever shown: an event cannot send to the model, rewrite
// a line, or start anything.
func (r *Runner) RunEvent(ctx context.Context, p *Package, e Event) (string, error) {
	out, err := r.run(ctx, p, e.Op, opCtx{kind: ctxEvent}, Input{})
	if err != nil {
		return "", err
	}
	return onlyText("event "+e.ID, out)
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
