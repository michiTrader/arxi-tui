package eval

import (
	"strings"
	"testing"
)

// The prompt is an input to the measurement, so these tests are about what the
// model is and is not told. A prompt defect shows up in the results as a model
// deficiency, which is the hardest kind of error to disbelieve.

func TestThePromptCarriesTheOrderTheSceneAndTheVocabulary(t *testing.T) {
	req := PatchRequest{
		Order:       "grey out the footer",
		Base:        []byte(`{"root":{"type":"stack"}}`),
		BaseName:    "SOARIA.json",
		SignedBinds: []string{"chat.history", "model.name"},
		Tokens:      []string{"dim", "bright"},
	}

	got := BuildUserPrompt(req)

	for _, want := range []string{
		"grey out the footer",
		`{"root":{"type":"stack"}}`,
		"SOARIA.json",
		"chat.history",
		"model.name",
		"dim",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("prompt omits %q\n"+
				"consequence: the model is asked to patch a document or use a vocabulary it was never shown, and the resulting failure is recorded against the model rather than the prompt.\n"+
				"remedy: include the order, the base scene and both vocabularies in every request.\n--- prompt ---\n%s", want, got)
		}
	}
}

// TestTheFirstTurnCarriesNoRefusal guards against telling the model it failed
// before it has answered.
func TestTheFirstTurnCarriesNoRefusal(t *testing.T) {
	got := BuildUserPrompt(PatchRequest{Order: "add a row", Base: []byte("{}")})

	if strings.Contains(got, "refused") {
		t.Errorf("the first-turn prompt mentions a refusal\n"+
			"consequence: the model is told it made a mistake it has not made, which invites it to 'fix' a document nobody rejected.\n"+
			"remedy: only render the history section when there is history.\n--- prompt ---\n%s", got)
	}
}

// TestTheRetryPromptCarriesTheAddressedRefusal is the prompt half of the
// measurement PLAN.md specifies.
//
// The runner test proves the refusal reaches the request struct; this proves
// it reaches the text the model actually reads. Both are needed: plumbing the
// verdict into PatchRequest and then not rendering it would pass one and fail
// the user, and the eval would silently be measuring guesswork.
func TestTheRetryPromptCarriesTheAddressedRefusal(t *testing.T) {
	req := PatchRequest{
		Order: "add a row",
		Base:  []byte("{}"),
		History: []Turn{{
			Document: []byte(`{"root":{"type":"text","bind":"model.current"}}`),
			Verdict: Verdict{
				Message:   `SOARIA.json:7:5: unsigned bind "model.current" in node type "text"`,
				Line:      7,
				Addressed: true,
			},
		}},
	}

	got := BuildUserPrompt(req)

	if !strings.Contains(got, "SOARIA.json:7:5") {
		t.Errorf("the retry prompt does not carry the address\n"+
			"consequence: this corpus exists to measure whether a model repairs from a file:line; withholding the address measures address-guessing instead, and the whole addressing effort would go unmeasured.\n"+
			"remedy: render Verdict.Message verbatim into the history section.\n--- prompt ---\n%s", got)
	}
	if !strings.Contains(got, "model.current") {
		t.Errorf("the retry prompt does not show the model its own refused document\n"+
			"consequence: the model is asked to repair a document it cannot see, so the line number in the refusal addresses text it is not looking at.\n"+
			"remedy: include the previous attempt's bytes.\n--- prompt ---\n%s", got)
	}
}

// TestEveryPriorAttemptIsShownNotJustTheLast protects the loop finding.
//
// Looping is one of the outcomes this harness reports. A model shown only its
// most recent attempt cannot see that it is repeating itself, so the harness
// would be scoring a handicap it created rather than a property of the model.
func TestEveryPriorAttemptIsShownNotJustTheLast(t *testing.T) {
	req := PatchRequest{
		Order: "add a row",
		Base:  []byte("{}"),
		History: []Turn{
			{Document: []byte(`{"marker":"first-attempt"}`), Verdict: Verdict{Message: "refused one"}},
			{Document: []byte(`{"marker":"second-attempt"}`), Verdict: Verdict{Message: "refused two"}},
		},
	}

	got := BuildUserPrompt(req)

	for _, want := range []string{"first-attempt", "second-attempt", "refused one", "refused two"} {
		if !strings.Contains(got, want) {
			t.Errorf("prompt omits %q from the repair history\n"+
				"consequence: a model shown only its latest attempt cannot tell it is looping, so the harness reports a loop it helped cause.\n"+
				"remedy: render every prior turn.\n--- prompt ---\n%s", want, got)
		}
	}
}

// TestStripFenceUnwrapsAMarkdownFence keeps a presentation habit from being
// scored as a capability failure.
func TestStripFenceUnwrapsAMarkdownFence(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"json tag", "```json\n{\"a\":1}\n```", `{"a":1}`},
		{"no tag", "```\n{\"a\":1}\n```", `{"a":1}`},
		{"leading space", "  ```json\n{\"a\":1}\n```  ", `{"a":1}`},
		{"unfenced is untouched", `{"a":1}`, `{"a":1}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := string(StripFence([]byte(tc.in)))
			if got != tc.want {
				t.Errorf("StripFence(%q) = %q, want %q\n"+
					"consequence: a correct document wrapped in backticks would be scored as a JSON syntax error, so every case fails for a formatting habit rather than a capability gap.\n"+
					"remedy: strip the fence, and only the fence.", tc.in, got, tc.want)
			}
		})
	}
}

// TestStripFenceLeavesProseAlone keeps the leniency narrow.
//
// Unwrapping a fence is presentation. Silently salvaging JSON out of a
// paragraph would be the harness answering the question the model was asked —
// the output contract says "the document and nothing else", and a model that
// ignores it has produced a real defect that must be scored.
func TestStripFenceLeavesProseAlone(t *testing.T) {
	in := "Here is the updated scene:\n{\"a\":1}\nHope that helps!"
	if got := string(StripFence([]byte(in))); got != in {
		t.Errorf("StripFence modified prose-wrapped output\n"+
			"consequence: the harness would be repairing the model's answer for it, so the output contract would be measured as satisfied when it was violated.\n"+
			"remedy: only strip a leading fence; leave everything else to the parser.\n  got: %q", got)
	}
}

// TestAGatewayRefusalIsNotAModelAnswer is a regression test for a measured
// incident, and the reason it is worth keeping is that every layer behaved as
// designed while the conclusion was false.
//
// The first real run of the corpus reported "0/3 converged, looped=3". Every
// case had actually been answered by the API gateway with "Free-plan credits
// can't be used with the Genspark API", delivered with HTTP 200 and
// finish_reason "stop" — shaped exactly like a successful completion. The
// runner graded the prose as the model's document, refused it as invalid JSON,
// saw the identical prose again, and concluded the model was looping.
//
// The runner already separates model_error from a score so that a transport
// problem cannot depress the number a shipping decision rests on. That
// separation was defeated by a failure arriving as a 200, which is the lesson:
// a status-code check is not a transport check.
func TestAGatewayRefusalIsNotAModelAnswer(t *testing.T) {
	// The exact envelope observed, reduced to the fields that matter.
	raw := []byte(`{"choices":[{"message":{"role":"assistant","content":"Free-plan credits can't be used with the Genspark API / LLM proxy. Please visit https://example.invalid/pricing to subscribe or purchase credits."}}],"x_genspark":{"code":"free_plan_block","audience":"free"}}`)
	content := "Free-plan credits can't be used with the Genspark API / LLM proxy. Please visit https://example.invalid/pricing to subscribe or purchase credits."

	if err := gatewayRefusal(raw, content); err == nil {
		t.Fatalf("a gateway plan-block was accepted as the model's answer\n" +
			"consequence: this exact response produced a report of '0/3 converged, looped=3' — a billing wall read as evidence that the model cannot patch scenes, which is the most misleading result this harness can produce because it looks like a finding.\n" +
			"remedy: detect the vendor error envelope and report it as a transport error, never as a score.")
	}
}

// TestAModelReturningBadJSONIsStillTheModelsFailure keeps the detection above
// from becoming an excuse.
//
// Over-reaching here is the mirror-image error: if malformed output were
// written off as a transport problem, real failures would vanish from the
// scores and the corpus would report a capability the model does not have.
func TestAModelReturningBadJSONIsStillTheModelsFailure(t *testing.T) {
	raw := []byte(`{"choices":[{"message":{"content":"{ \"root\": { }"}}]}`)
	content := `{ "root": { }`

	if err := gatewayRefusal(raw, content); err != nil {
		t.Errorf("malformed JSON from the model was reported as a gateway failure: %v\n"+
			"consequence: genuine model failures would be excused as transport noise and disappear from the scores, inflating the measured capability.\n"+
			"remedy: only treat a reply as a gateway message when it is not a document at all.", err)
	}
}

// TestProseThatIsNotABillingMessageStaysTheModelsProblem pins the boundary
// from the other side: a chatty model is a model that violated the output
// contract, and the corpus must score that.
func TestProseThatIsNotABillingMessageStaysTheModelsProblem(t *testing.T) {
	content := "Sure! Here is the updated scene you asked for."
	if err := gatewayRefusal([]byte(`{"choices":[{"message":{"content":"..."}}]}`), content); err != nil {
		t.Errorf("ordinary prose was classified as a gateway failure: %v\n"+
			"consequence: a model ignoring the 'document and nothing else' contract would not be scored for it.\n"+
			"remedy: restrict the vendor-independent check to service-message markers.", err)
	}
}
