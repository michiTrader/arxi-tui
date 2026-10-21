//go:build !windows

package extv2

import (
	"context"
	"testing"
	"time"
)

func TestOutputPostRunsARealProgramWithTheAnswerAsOneArgument(t *testing.T) {
	r, _ := pkgDir(t, map[string]string{"up.sh": "#!/bin/sh\nprintf '%s' \"$1\" | tr a-z A-Z\n"})
	p := accepted(t, `{`+head+`,"rules":[{"id":"up","stage":"output.post","op":{"kind":"exec","argv":["./up.sh","${input}"]}}]}`, Options{})
	start := time.Now()
	res := r.RunOutputPost(context.Background(), []*Package{p}, "done; $(rm -rf /) `x`")
	if res.Text != "DONE; $(RM -RF /) `X`" || len(res.Notes) != 0 {
		t.Fatalf("got %+v: the answer is data in one argument, never a shell line", res)
	}
	if time.Since(start) > PipelineBudget {
		t.Errorf("took longer than the budget")
	}
}
