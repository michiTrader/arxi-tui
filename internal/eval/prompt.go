package eval

import (
	"fmt"
	"strings"
)

// Prompt construction, kept apart from transport.
//
// The prompt is an input to the measurement, not an implementation detail of
// whichever API is being called. Separating it has two consequences that
// matter: it can be tested without a network, and when two models are
// compared the thing being varied is the model rather than the wording.

// SystemPrompt is the instruction every turn carries.
//
// It states the output contract first because that is the failure that costs a
// whole turn: a model that explains its reasoning in prose produces bytes the
// parser refuses, and the refusal it gets back is a JSON syntax error, which
// is a confusing thing to be told when you thought you were being helpful.
const SystemPrompt = `You edit terminal user interfaces that are JSON documents.

You will be given an order in the user's own words, the current scene document,
and the vocabulary the engine accepts. Return the COMPLETE new scene document.

Rules:
- Reply with the JSON document and nothing else. No prose, no markdown fences.
- Every "bind" and "when" value must come from the signed bind list. A bind
  that is not on the list is refused at load time.
- Every style token must come from the token list. The token namespace is open
  in principle, but only the listed tokens exist in the active theme.
- Change as little as the order requires. Do not remove existing nodes unless
  the order asks for it.
- If you are given a previous attempt and the engine's refusal, the refusal
  names a file, a line and a reason. Read the line it names and fix that.`

// BuildUserPrompt renders one turn's request.
//
// The refusal history goes last, closest to where the model starts writing,
// because that is the instruction that changed since the previous turn. On a
// first turn there is no history and the prompt is just the order, the scene
// and the vocabulary.
func BuildUserPrompt(req PatchRequest) string {
	var b strings.Builder

	fmt.Fprintf(&b, "Order: %s\n\n", req.Order)

	name := req.BaseName
	if name == "" {
		name = "scene.json"
	}
	fmt.Fprintf(&b, "Current scene (%s):\n%s\n\n", name, string(req.Base))

	fmt.Fprintf(&b, "Signed binds (the only values allowed in \"bind\" and \"when\"):\n")
	for _, bind := range req.SignedBinds {
		fmt.Fprintf(&b, "  %s\n", bind)
	}
	b.WriteString("\n")

	fmt.Fprintf(&b, "Style tokens defined by the active theme:\n")
	for _, tok := range req.Tokens {
		fmt.Fprintf(&b, "  %s\n", tok)
	}

	if len(req.History) == 0 {
		return b.String()
	}

	// Every previous turn, not just the last one. A model that is looping
	// cannot see that it is looping if it is only ever shown its most
	// recent attempt — and looping is one of the outcomes this harness
	// reports, so making it invisible to the model would be measuring a
	// handicap the harness created.
	b.WriteString("\nPrevious attempts were refused by the engine.\n")
	for i, turn := range req.History {
		fmt.Fprintf(&b, "\nAttempt %d:\n%s\n", i+1, string(turn.Document))
		if turn.ModelErr != nil {
			continue
		}
		fmt.Fprintf(&b, "Engine refused: %s\n", turn.Verdict.Message)
	}
	b.WriteString("\nReturn a corrected complete document.")

	return b.String()
}

// StripFence removes a markdown code fence if the model wrapped its answer in
// one.
//
// This is a deliberate, narrow leniency and it is worth saying why it is not
// cheating. The measurement is whether a model can patch a scene and repair
// from a file:line refusal — not whether it can suppress a formatting habit
// that every chat-tuned model has. Scoring a correct document as a syntax
// error because of three backticks would report a capability failure that is
// really a presentation one, and every case would fail for the same
// uninteresting reason.
//
// The leniency stops there: anything else the model says around the document
// is left in place and fails to parse, because prose mixed into a document is
// a real defect in the output contract rather than a wrapper around it.
func StripFence(body []byte) []byte {
	s := strings.TrimSpace(string(body))
	if !strings.HasPrefix(s, "```") {
		return body
	}

	// Drop the opening fence line, which may carry a language tag.
	if nl := strings.IndexByte(s, '\n'); nl >= 0 {
		s = s[nl+1:]
	} else {
		return body
	}

	if end := strings.LastIndex(s, "```"); end >= 0 {
		s = s[:end]
	}
	return []byte(strings.TrimSpace(s))
}
