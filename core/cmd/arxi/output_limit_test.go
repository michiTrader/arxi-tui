package main

import (
	"context"
	"strings"
	"testing"

	"github.com/michiTrader/arxi/internal/turn"
)

func TestTheOutputCeilingIsGenerousAndTheEnvironmentMovesIt(t *testing.T) {
	t.Setenv(maxOutputEnv, "")
	if got := chatMaxOutput(); got != 16384 {
		t.Fatalf("default = %d, want 16384 (4096 was used up by a reasoning model's thinking)", got)
	}
	t.Setenv(maxOutputEnv, "32000")
	if got := chatMaxOutput(); got != 32000 {
		t.Errorf("env 32000 = %d", got)
	}
	for _, bad := range []string{"abc", "10", "-5", "99999999", "1.5"} {
		t.Setenv(maxOutputEnv, bad)
		if got := chatMaxOutput(); got != defaultMaxOutput {
			t.Errorf("env %q = %d, want the default %d", bad, got, defaultMaxOutput)
		}
	}
}

func TestTheLimitAProviderNamesIsRead(t *testing.T) {
	ref := func(code, msg string) *turn.Refusal { return &turn.Refusal{Code: code, Message: msg} }
	yes := map[string]int{
		"the valid range of max_tokens is [1, 8192]":                  8192,
		"Invalid max_tokens value, the valid range is [ 1 , 393216 ]": 393216,
		"max_tokens: 20000 > 8192, which is the maximum allowed":      8192,
		"max_tokens is too large: at most 4096 tokens are allowed":    4096,
	}
	for msg, want := range yes {
		if got, ok := outputLimitFrom(ref("http_400", msg)); !ok || got != want {
			t.Errorf("%q = %d, %v; want %d", msg, got, ok, want)
		}
	}
	no := []*turn.Refusal{
		nil,
		ref("http_400", "bad request"), // no talk of tokens
		ref("http_401", "the valid range of max_tokens is [1, 8192]"), // not a 400
		ref("http_500", "the valid range of max_tokens is [1, 8192]"),
		ref("http_400", "the valid range of max_tokens is [1, 50]"), // not a believable ceiling
		ref("http_400", "your prompt has too many tokens"),          // no figure
	}
	for i, r := range no {
		if got, ok := outputLimitFrom(r); ok {
			t.Errorf("case %d (%+v) read as a limit of %d", i, r, got)
		}
	}
}

// A provider that takes fewer reply tokens than we ask refuses with a 400 that names its
// limit. The turn asks again with that limit and answers; later asks stay under it.
func TestAProviderThatTakesFewerOutputTokensIsAskedAgainWithItsOwnLimit(t *testing.T) {
	f := setUpFake(t)
	f.failNext, f.failCode = 1, 400
	f.failBody = `{"error":{"message":"Invalid max_tokens value, the valid range of max_tokens is [1, 8192]"}}`
	f.script = []scripted{{name: "list", args: `{"path":"."}`}, {text: "fine"}}
	ctx, _ := collect(t, projectDir(t))
	res, err := chatSendEffort(ctx, "hi", "", "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if res.Text != "fine" {
		t.Fatalf("text = %q", res.Text)
	}
	if len(f.maxTokens) != 3 || f.maxTokens[0] != defaultMaxOutput || f.maxTokens[1] != 8192 || f.maxTokens[2] != 8192 {
		t.Fatalf("max_tokens by ask = %v; want [%d 8192 8192]", f.maxTokens, defaultMaxOutput)
	}
}

// A 400 that names no usable limit is the user's error to see, not something to retry.
func TestABadRequestWithoutALimitIsNotRetried(t *testing.T) {
	f := setUpFake(t)
	f.failNext, f.failCode = 5, 400
	f.failBody = `{"error":{"message":"messages are malformed"}}`
	_, err := chatSendEffort(context.Background(), "hi", "", "", "", "")
	if err == nil || !strings.Contains(err.Error(), "malformed") {
		t.Fatalf("err = %v", err)
	}
	if f.calls != 1 {
		t.Errorf("asks = %d, want 1", f.calls)
	}
}

// A 400 about something else that happens to carry a range is not a token limit.
func TestARangeThatIsNotAboutTokensIsNotALimit(t *testing.T) {
	r := &turn.Refusal{Code: "http_400", Message: "the valid range of temperature is [1, 8192]"}
	if got, ok := outputLimitFrom(r); ok {
		t.Errorf("read as a limit of %d", got)
	}
}

// A provider whose stated limit is not below ours gives nothing to learn: asking again
// would send the same request, so the 400 is shown instead.
func TestALimitNotBelowOursIsNotRetried(t *testing.T) {
	f := setUpFake(t)
	f.failNext, f.failCode = 5, 400
	f.failBody = `{"error":{"message":"the valid range of max_tokens is [1, 999999]"}}`
	if _, err := chatSendEffort(context.Background(), "hi", "", "", "", ""); err == nil {
		t.Fatal("want the 400 as an error")
	}
	if f.calls != 1 {
		t.Errorf("asks = %d, want 1", f.calls)
	}
}
