package extv2

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"
	"unicode/utf8"
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

// MaxPostRunes is the longest answer an output.post rule is given. A rule hands its whole
// result back as the text to show, so cutting what it reads would make it throw the rest of
// the answer away; an answer longer than this is shown as the model said it, with a note.
const MaxPostRunes = 4000

// PostResult is what is shown of one answer of the model.
type PostResult struct {
	// Text is what the user is shown. It is the model's own text unless a rule reworded it.
	Text string
	// Say is local text from the rules that ran, shown as notes.
	Say []string
	// Notes are failures and refusals; a rule that fails leaves the text as it was.
	Notes []Note
	// Ran lists the rules that took part, "package/rule", in order.
	Ran []string
}

// RunOutputPost runs every output.post rule of the given packages over one answer of the
// model, and returns what is to be SHOWN of it.
//
// It is the mirror of RunInputPre with one more restriction: it changes a picture, never a
// fact. The model's answer is what the model said; the host keeps that for the model's own
// memory, the saved session and the events, and a rule only chooses the words the user
// reads. For that reason a rule here can reword the text or add a note, and nothing else:
// it cannot hold the answer back or consume it (the answer has already been given and
// paid for), and a rule that fails leaves the text as it was.
//
// Rules run in the same fixed order (package id, then rule id), each seeing the text the
// previous one produced, under the same budget. Unlike an input line, an answer that
// starts with "/" is ordinary text. An empty answer is left alone.
func (r *Runner) RunOutputPost(ctx context.Context, pkgs []*Package, text string) PostResult {
	res := PostResult{Text: text}
	if strings.TrimSpace(text) == "" {
		return res
	}
	note := func(p *Package, rule Rule, msg string) {
		res.Notes = append(res.Notes, Note{Package: p.ID, Rule: rule.ID, Message: msg})
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

	tooLong := utf8.RuneCountInString(text) > MaxPostRunes
	ran, reportedLong := 0, false
	for _, p := range ordered {
		rules := append([]Rule(nil), p.Rules...)
		sort.SliceStable(rules, func(i, j int) bool { return rules[i].ID < rules[j].ID })
		for _, rule := range rules {
			if rule.Disabled || rule.Stage != StageOutputPost || !rule.When.Match(res.Text) {
				continue
			}
			who := p.ID + "/" + rule.ID
			switch {
			case tooLong:
				if !reportedLong {
					reportedLong = true
					note(p, rule, fmt.Sprintf("skipped: the answer is longer than %d characters, more than a rule can read whole, so it is shown as the model said it", MaxPostRunes))
				}
				continue
			case ran >= MaxRulesPerLine:
				note(p, rule, fmt.Sprintf("skipped: only %d rules run per answer", MaxRulesPerLine))
				continue
			case ctx.Err() != nil:
				note(p, rule, "skipped: the time for rules on one answer ran out")
				continue
			}
			ran++
			res.Ran = append(res.Ran, who)
			out, err := r.safeRule(ctx, p, rule, res.Text)
			if err != nil {
				note(p, rule, err.Error())
				continue
			}
			if out.Send != "" {
				note(p, rule, "a rule cannot send text of its own; use \"text\" to reword the answer")
				continue
			}
			switch out.Verdict {
			case "", VerdictContinue:
			case VerdictTransform:
				if out.Text == "" {
					note(p, rule, "the rule reworded the answer to nothing, so it was kept as the model said it")
					continue
				}
				res.Text = out.Text
			case VerdictHandled, VerdictBlock:
				note(p, rule, fmt.Sprintf("verdict %q holds an answer back, and an answer has already come, so the rule can only reword it (\"transform\"); it was kept as it was", out.Verdict))
				continue
			default:
				note(p, rule, fmt.Sprintf("verdict %q is not understood", out.Verdict))
				continue
			}
			if out.Say != "" {
				res.Say = append(res.Say, out.Say)
			}
		}
	}
	return res
}
