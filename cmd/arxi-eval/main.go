// Command arxi-eval runs the Phase 2 eval corpus against a model and reports
// convergence and turns-to-convergence.
//
// It is a separate binary from arxi-tui on purpose. The shipped interface must
// not carry an eval harness, an HTTP client for a model API, or a reason to
// read OPENAI_API_KEY — AGENTS.md's install rule is that the user installs
// arxi, not arxi's dependencies, and a measurement tool is not part of the
// product the user runs.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/michiTrader/arxi_tui/internal/eval"
)

func main() {
	var (
		corpusDir = flag.String("corpus", "testdata/eval", "directory of corpus cases")
		baseDir   = flag.String("scenes", "testdata", "directory of base scenes")
		modelName = flag.String("model", "", "model to evaluate (required)")
		maxTurns  = flag.Int("max-turns", eval.DefaultMaxTurns, "turn budget per case")
		only      = flag.String("only", "", "run only the case with this id")
		verbose   = flag.Bool("v", false, "print each turn's refusal")
	)
	flag.Parse()

	if err := run(*corpusDir, *baseDir, *modelName, *only, *maxTurns, *verbose); err != nil {
		fmt.Fprintf(os.Stderr, "arxi-eval: %v\n", err)
		os.Exit(1)
	}
}

func run(corpusDir, baseDir, modelName, only string, maxTurns int, verbose bool) error {
	cases, err := eval.LoadAll(corpusDir)
	if err != nil {
		return err
	}

	if only != "" {
		filtered := cases[:0]
		for _, c := range cases {
			if c.ID == only {
				filtered = append(filtered, c)
			}
		}
		if len(filtered) == 0 {
			return fmt.Errorf("no case with id %q in %s", only, corpusDir)
		}
		cases = filtered
	}

	model, err := eval.NewOpenAIModelFromEnv(modelName)
	if err != nil {
		return err
	}

	// Ctrl-C stops the run rather than killing it: a partial report is
	// worth more than none, and these runs cost money per turn.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	opt := eval.Options{MaxTurns: maxTurns, BaseDir: baseDir}

	fmt.Printf("model: %s\ncases: %d\nbudget: %d turns\n\n", modelName, len(cases), maxTurns)

	results := make([]eval.Result, 0, len(cases))
	for _, c := range cases {
		res := eval.Run(ctx, model, c, opt)
		results = append(results, res)
		report(res, verbose)
		if ctx.Err() != nil {
			fmt.Println("\ninterrupted; reporting partial results")
			break
		}
	}

	summary := eval.Summarize(results)
	fmt.Printf("\n%s\n", summary)

	// The exit status reports whether the harness ran, not whether the
	// model scored well. PLAN.md sets no threshold deliberately: if the
	// model cannot patch scenes reliably, the instruction is that the
	// document must say so, not that a build turns red. A non-zero exit on
	// a low score would turn this measurement into a gate someone is
	// tempted to tune.
	for _, r := range results {
		if r.Outcome == eval.OutcomeModelError {
			return fmt.Errorf("at least one case did not produce a gradeable answer: %v", r.Err)
		}
	}
	return nil
}

func report(res eval.Result, verbose bool) {
	fmt.Printf("%-28s %-11s %d turn(s)", res.CaseID, res.Outcome, res.Turns)
	if len(res.MissingBinds) > 0 {
		fmt.Printf("  unbound: %s", strings.Join(res.MissingBinds, ", "))
	}
	if res.Err != nil {
		fmt.Printf("  err: %v", res.Err)
	}
	fmt.Println()

	if !verbose {
		return
	}
	for i, turn := range res.History {
		switch {
		case turn.ModelErr != nil:
			fmt.Printf("    turn %d: model error: %v\n", i+1, turn.ModelErr)
		case turn.Verdict.Accepted:
			fmt.Printf("    turn %d: accepted\n", i+1)
		default:
			fmt.Printf("    turn %d: %s\n", i+1, turn.Verdict.Message)
		}
	}
}
