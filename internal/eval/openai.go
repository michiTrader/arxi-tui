package eval

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

// An OpenAI-compatible Model, written against the HTTP API directly.
//
// No SDK, and that is the install rule from AGENTS.md rather than a
// preference: "the user installs arxi, not arxi's dependencies", and "any new
// dependency names its cost in the commit that adds it". The chat-completions
// request is a small JSON POST; taking a client library for it would add a
// dependency tree to the shipped binary in exchange for code this file
// already fits in.
//
// This adapter is also the only part of the eval package that touches a
// network, which is what keeps the loop and the scoring provable offline.

// OpenAIModel calls an OpenAI-compatible chat-completions endpoint.
type OpenAIModel struct {
	APIKey  string
	BaseURL string
	Model   string
	Client  *http.Client
	// Temperature is a pointer so that "unset" and "zero" are different
	// requests. Zero is the value a reproducible eval wants, and a plain
	// float64 could not express leaving the server's default alone.
	Temperature *float64
}

// NewOpenAIModelFromEnv builds a model from the environment.
//
// It fails loudly when the key is absent rather than returning a model that
// errors on first use: a run that produces model_error for every case looks
// like a finding, and the operator would read a configuration mistake as
// evidence about the model.
func NewOpenAIModelFromEnv(model string) (*OpenAIModel, error) {
	key := os.Getenv("OPENAI_API_KEY")
	if key == "" {
		return nil, fmt.Errorf("OPENAI_API_KEY is not set; without it every case would score as model_error and the run would read as a finding about the model rather than a missing key")
	}
	base := os.Getenv("OPENAI_BASE_URL")
	if base == "" {
		base = "https://api.openai.com/v1"
	}
	if model == "" {
		return nil, fmt.Errorf("no model name given; the model under test must be recorded with the scores, so it is never defaulted")
	}
	zero := 0.0
	return &OpenAIModel{
		APIKey:      key,
		BaseURL:     strings.TrimRight(base, "/"),
		Model:       model,
		Client:      &http.Client{Timeout: 120 * time.Second},
		Temperature: &zero,
	}, nil
}

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type chatRequest struct {
	Model       string        `json:"model"`
	Messages    []chatMessage `json:"messages"`
	Temperature *float64      `json:"temperature,omitempty"`
}

type chatResponse struct {
	Choices []struct {
		Message chatMessage `json:"message"`
	} `json:"choices"`
	Error *struct {
		Message string `json:"message"`
		Type    string `json:"type"`
	} `json:"error,omitempty"`
}

// Patch implements Model.
func (m *OpenAIModel) Patch(ctx context.Context, req PatchRequest) ([]byte, error) {
	body, err := json.Marshal(chatRequest{
		Model:       m.Model,
		Temperature: m.Temperature,
		Messages: []chatMessage{
			{Role: "system", Content: SystemPrompt},
			{Role: "user", Content: BuildUserPrompt(req)},
		},
	})
	if err != nil {
		return nil, fmt.Errorf("encode request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, m.BaseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+m.APIKey)

	client := m.Client
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != http.StatusOK {
		// The body is included because the status alone does not
		// distinguish a bad key from a bad model name, and both are
		// operator mistakes that would otherwise be reported as the
		// model failing every case.
		return nil, fmt.Errorf("chat completions: %s: %s", resp.Status, truncate(string(raw), 400))
	}

	var parsed chatResponse
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}
	if parsed.Error != nil {
		return nil, fmt.Errorf("chat completions: %s", parsed.Error.Message)
	}
	if len(parsed.Choices) == 0 {
		return nil, fmt.Errorf("chat completions returned no choices")
	}

	content := parsed.Choices[0].Message.Content
	if err := gatewayRefusal(raw, content); err != nil {
		return nil, err
	}

	return StripFence([]byte(content)), nil
}

// gatewayRefusal detects a proxy or gateway that answered instead of the
// model, and turns it into a transport error.
//
// This exists because of a measured incident, not a hypothetical. The first
// real run of the corpus reported 0/3 converged, looped=3 — a damning-looking
// result. Every case had in fact been answered by the gateway with "Free-plan
// credits can't be used with the Genspark API", delivered with HTTP 200 and
// finish_reason "stop", i.e. shaped exactly like a successful completion. The
// runner graded that prose as the model's document, refused it as invalid
// JSON, watched the identical prose arrive again, and correctly concluded the
// model was looping.
//
// Every layer behaved as designed and the conclusion was still false, which is
// the point worth keeping: the runner already separates model_error from a
// score precisely so a transport problem cannot depress the number a shipping
// decision rests on, and that separation was defeated by a failure that
// arrives as a 200. A status-code check is not a transport check.
//
// The detection is deliberately narrow — a vendor error envelope, or an
// unfenced reply that is not JSON at all and reads like a service message.
// A model that returns bad JSON must still be scored as a model that returned
// bad JSON; the danger of over-reaching here is excusing real failures, which
// would inflate the scores in the other direction.
func gatewayRefusal(raw []byte, content string) error {
	// The strongest signal: a vendor envelope alongside the choices.
	var envelope struct {
		Genspark *struct {
			Code       string `json:"code"`
			UpgradeURL string `json:"upgrade_url"`
		} `json:"x_genspark"`
	}
	if err := json.Unmarshal(raw, &envelope); err == nil && envelope.Genspark != nil && envelope.Genspark.Code != "" {
		return fmt.Errorf("the API gateway answered instead of the model (%s): %s",
			envelope.Genspark.Code, truncate(content, 200))
	}

	// Weaker, vendor-independent signal: the reply is not a document at
	// all and names the account rather than the scene. Checked only when
	// the content does not even begin like JSON, so a malformed document
	// is still the model's own failure.
	trimmed := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(content), "```"))
	if trimmed == "" {
		return fmt.Errorf("the model returned an empty completion")
	}
	if trimmed[0] == '{' || trimmed[0] == '[' {
		return nil
	}
	lower := strings.ToLower(trimmed)
	for _, marker := range []string{
		"credits can't be used",
		"credits cannot be used",
		"quota",
		"rate limit",
		"subscribe or purchase",
		"upgrade your plan",
		"billing",
	} {
		if strings.Contains(lower, marker) {
			return fmt.Errorf("the API gateway answered instead of the model: %s", truncate(trimmed, 200))
		}
	}
	return nil
}

// truncate bounds an error body so a runaway HTML error page does not become
// the whole report.
func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
