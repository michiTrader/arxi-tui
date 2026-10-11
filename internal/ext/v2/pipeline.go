package extv2

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"
)

// Limits of one pass of input.pre over one line.
const (
	MaxRulesPerLine = 8
	// PipelineBudget is the time every rule shares for one line. A user types and
	// waits; a slow rule must not turn Enter into a hang.
	PipelineBudget = 2 * time.Second
)

// Note is one thing the pipeline wants the user to be able to see: a rule that
// failed, was skipped, or had its output refused. The host shows these as local
// warnings, never to the model.
type Note struct {
	Package string
	Rule    string
	Message string
}

// Result is what happens to a plain-text line before it goes to the model.
type Result struct {
	// Line is the text to send. Meaningful only when neither Handled nor Blocked.
	Line string
	// Handled: a rule consumed the line; nothing is sent.
	Handled bool
	// Blocked: a rule held the line back; Reason says why.
	Blocked bool
	Reason  string
	// Say is local text to show the user, from the rules that ran.
	Say []string
	// Notes are failures and refusals, all of which are fail-open unless the rule
	// asked on_error "block".
	Notes []Note
	// Ran lists the rules that took part, "package/rule", in order.
	Ran []string
}

// Send reports whether the line should still go to the model.
func (r Result) Send() bool { return !r.Handled && !r.Blocked }

// RunInputPre runs every input.pre rule of the given packages over one line.
//
// The line is a plain-text prompt. A line that starts with "/" is a command, which the
// host resolves from its registry; rules never see it, so a rule cannot intercept,
// shadow or re-route /help, /ui or any other way out. An empty line is left alone.
//
// Rules run in a fixed order, package id then rule id, each one seeing the text the
// previous one produced. Verdicts only ever narrow what the user asked for: a rule can
// reword a line, consume it, or hold it back with a reason; it cannot turn it into a
// command, make it empty, or send something else for the user.
func (r *Runner) RunInputPre(ctx context.Context, pkgs []*Package, line string) Result {
	res := Result{Line: line}
	if strings.TrimSpace(line) == "" || strings.HasPrefix(strings.TrimSpace(line), "/") {
		return res
	}
	var ordered []*Package
	for _, p := range pkgs {
		if p != nil && !p.Disabled {
			ordered = append(ordered, p)
		}
	}
	sort.SliceStable(ordered, func(i, j int) bool { return ordered[i].ID < ordered[j].ID })

	ctx, cancel := context.WithTimeout(ctx, PipelineBudget)
	defer cancel()

	ran := 0
	for _, p := range ordered {
		rules := append([]Rule(nil), p.Rules...)
		sort.SliceStable(rules, func(i, j int) bool { return rules[i].ID < rules[j].ID })
		for _, rule := range rules {
			if rule.Disabled || rule.Stage != StageInputPre || !rule.When.Match(res.Line) {
				continue
			}
			who := p.ID + "/" + rule.ID
			fail := func(msg string) bool {
				res.Notes = append(res.Notes, Note{Package: p.ID, Rule: rule.ID, Message: msg})
				if rule.OnError == "block" {
					res.Blocked = true
					res.Reason = fmt.Sprintf("%s could not check this line (%s), and is set to hold the line back when that happens", who, msg)
					return true
				}
				return false
			}
			switch {
			case ran >= MaxRulesPerLine:
				if fail(fmt.Sprintf("skipped: only %d rules run per line", MaxRulesPerLine)) {
					return res
				}
				continue
			case ctx.Err() != nil:
				if fail("skipped: the time for rules on one line ran out") {
					return res
				}
				continue
			}
			ran++
			res.Ran = append(res.Ran, who)
			out, err := r.safeRule(ctx, p, rule, res.Line)
			if err != nil {
				if fail(err.Error()) {
					return res
				}
				continue
			}
			if out.Send != "" {
				// Sending is what a command does, on the user's explicit request. A
				// rule that could send would write the user's side of the conversation.
				if fail("a rule cannot send text of its own; use \"text\" to reword the line") {
					return res
				}
				continue
			}
			switch out.Verdict {
			case "", VerdictContinue:
				if out.Say != "" {
					res.Say = append(res.Say, out.Say)
				}
			case VerdictTransform:
				t := out.Text
				switch {
				case t == "":
					if fail("the rule reworded the line to nothing, so the line was kept as typed") {
						return res
					}
					continue
				case strings.HasPrefix(strings.TrimSpace(t), "/"):
					if fail("the rule reworded the line into a command, which a rule may not do, so the line was kept as typed") {
						return res
					}
					continue
				}
				res.Line = t
				if out.Say != "" {
					res.Say = append(res.Say, out.Say)
				}
			case VerdictHandled:
				res.Handled = true
				if out.Say != "" {
					res.Say = append(res.Say, out.Say)
				}
				return res
			case VerdictBlock:
				res.Blocked = true
				res.Reason = out.Reason
				if res.Reason == "" {
					res.Reason = who + " held this line back"
				}
				return res
			default:
				if fail(fmt.Sprintf("verdict %q is not understood", out.Verdict)) {
					return res
				}
			}
		}
	}
	return res
}

// safeRule runs a rule and turns a panic in a builtin into an error: one broken rule
// must not take the whole host down with it.
func (r *Runner) safeRule(ctx context.Context, p *Package, rule Rule, line string) (out Outcome, err error) {
	defer func() {
		if rec := recover(); rec != nil {
			out, err = Outcome{}, fmt.Errorf("the rule crashed (%v)", rec)
		}
	}()
	return r.RunRule(ctx, p, rule, line)
}
