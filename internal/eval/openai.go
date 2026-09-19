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

	return StripFence([]byte(parsed.Choices[0].Message.Content)), nil
}

// truncate bounds an error body so a runaway HTML error page does not become
// the whole report.
func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
