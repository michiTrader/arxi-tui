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
