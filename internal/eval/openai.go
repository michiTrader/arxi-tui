package eval

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
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
	// MaxAttempts bounds the retry on a *transient* transport failure (a
	// gateway 502/503/504 or a connection error), never on a model answer.
	// Zero means defaultMaxAttempts.
	MaxAttempts int
	// Backoff is the wait before attempt n+1 (1-based). Injectable so a test
	// does not sleep real seconds; nil means defaultBackoff.
	Backoff func(attempt int) time.Duration
}

// defaultMaxAttempts and defaultBackoff tune the retry.
//
// The retry exists because of a measured incident, like gatewayRefusal below:
// a hosted gateway in front of a large model answers the corpus's big
// scene-patch prompts with an intermittent 504 Gateway Timeout — some requests
// complete, most do not — so a single-shot run reports model_error for most
// cases and the operator reads a flaky proxy as a finding about the model. A
// bounded retry on exactly the transient transport codes (502/503/504) and on
// connection errors lets the loop ride out the proxy without touching the
// model_error-vs-score separation: a 4xx, a vendor error envelope, bad JSON or
// a non-gateway 5xx is still returned on the first attempt, because those are
// the model's or the operator's answer and retrying them would only hide a
// real result behind a delay.
const defaultMaxAttempts = 5

func defaultBackoff(attempt int) time.Duration {
	// 0.5s, 1s, 2s, 4s … capped at 8s. Exponential so a briefly overloaded
	// gateway is given room, capped so a long run does not stall on one case.
	d := 500 * time.Millisecond << (attempt - 1)
	if d > 8*time.Second {
		d = 8 * time.Second
	}
	return d
}

// transientError marks a transport failure worth retrying. It is distinct from
// every other error Patch can return precisely so classification is explicit:
// only an error wrapped as transient is retried; everything else surfaces at
// once as the model/operator result it is.
type transientError struct{ err error }

func (e *transientError) Error() string { return e.err.Error() }
func (e *transientError) Unwrap() error { return e.err }

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
		MaxAttempts: maxAttemptsFromEnv(),
	}, nil
}

// maxAttemptsFromEnv lets an operator widen the retry against a gateway worse
// than the default rides out, without a recompile. A missing or unparseable
// value is the default rather than an error: the retry is an operability knob,
// not part of the measurement, so a typo here must not fail a run the way a
// missing key does.
func maxAttemptsFromEnv() int {
	v := os.Getenv("OPENAI_MAX_ATTEMPTS")
	if v == "" {
		return defaultMaxAttempts
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < 1 {
		return defaultMaxAttempts
	}
	return n
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

// Patch implements Model. It retries a transient transport failure (a gateway
// 502/503/504 or a connection error) up to MaxAttempts, with backoff, and
// returns every other error — a 4xx, a vendor refusal, bad JSON — on the first
// attempt, so the retry hardens the harness against a flaky proxy without ever
// retrying the model's own answer.
func (m *OpenAIModel) Patch(ctx context.Context, req PatchRequest) ([]byte, error) {
	attempts := m.MaxAttempts
	if attempts <= 0 {
		attempts = defaultMaxAttempts
	}
	backoff := m.Backoff
	if backoff == nil {
		backoff = defaultBackoff
	}

	var last error
	for attempt := 1; attempt <= attempts; attempt++ {
		body, err := m.patchOnce(ctx, req)
		if err == nil {
			return body, nil
		}
		var tr *transientError
		if !errors.As(err, &tr) {
			// A model/operator result, not transport: surface it now.
			return nil, err
		}
		last = err
		if attempt == attempts {
			break
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(backoff(attempt)):
		}
	}
	return nil, fmt.Errorf("after %d attempts: %w", attempts, last)
}

// patchOnce is one request/response. It wraps a connection error or a gateway
// 502/503/504 as *transientError; every other failure is returned bare.
func (m *OpenAIModel) patchOnce(ctx context.Context, req PatchRequest) ([]byte, error) {
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
		// A cancelled/expired context is the operator stopping the run, not
		// a flaky gateway: do not retry it.
		if ctx.Err() != nil {
			return nil, err
		}
		return nil, &transientError{fmt.Errorf("connection error: %w", err)}
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		if ctx.Err() != nil {
			return nil, err
		}
		return nil, &transientError{fmt.Errorf("reading response: %w", err)}
	}

	if resp.StatusCode != http.StatusOK {
		msg := fmt.Errorf("chat completions: %s: %s", resp.Status, truncate(string(raw), 400))
		if isTransientStatus(resp.StatusCode) {
			// A gateway/proxy transient: the request never reached the
			// model, so retrying it is not retrying an answer. The body is
			// still carried so a run that exhausts its attempts reports what
			// the gateway said.
			return nil, &transientError{msg}
		}
		// A 4xx (bad key, bad model, insufficient quota) or a non-gateway
		// 5xx is the operator's or the service's answer, not transport —
		// returned at once so it is not hidden behind a delay.
		return nil, msg
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

// isTransientStatus names the gateway/proxy codes a retry is for. It is
// deliberately narrow — the three classic reverse-proxy transients — because a
// plain 500 is as likely to be a deterministic server fault that a retry only
// delays, and the measured incident is a 504.
func isTransientStatus(code int) bool {
	switch code {
	case http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout:
		return true
	default:
		return false
	}
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
