package provider

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
)

type thinkingKey struct{}

// WithThinking returns a context that asks the provider layer to stream a chat
// turn and hand every fragment of the model's reasoning to fn as it arrives.
// Runs never set it, so they keep the whole-reply path and its budget rules.
func WithThinking(ctx context.Context, fn func(fragment string)) context.Context {
	return context.WithValue(ctx, thinkingKey{}, fn)
}

func thinkingFrom(ctx context.Context) func(string) {
	fn, _ := ctx.Value(thinkingKey{}).(func(string))
	return fn
}

// CompleteStream performs one chat completion over server-sent events.
//
// It returns exactly what Complete would have: the whole reply, assembled, with
// the usage block, and the same error semantics (*APIError for a refusal,
// anything else for transport), so retries and refusal text work unchanged. The
// only addition is onThinking, called with each reasoning fragment as it
// arrives. A server that ignores `stream` and answers with a plain JSON body is
// handled as a plain completion.
func (c *Client) CompleteStream(ctx context.Context, req chatRequest, onThinking func(string)) (*chatResponse, error) {
	if req.Model == "" {
		return nil, errors.New("a completion needs a model; the caller resolved nothing")
	}
	key, err := c.credential()
	if err != nil {
		return nil, err
	}
	req.Stream = true
	req.StreamOptions = &streamOptions{IncludeUsage: true}
	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("encode the request for %s: %w", req.Model, err)
	}
	url := strings.TrimSuffix(c.BaseURL, "/") + "/chat/completions"
	hreq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("build the request for %s: %w", req.Model, err)
	}
	hreq.Header.Set("Content-Type", "application/json")
	hreq.Header.Set("Accept", "text/event-stream")
	if key != "" {
		hreq.Header.Set("Authorization", "Bearer "+key)
	}
	client := c.HTTP
	if client == nil {
		client = DefaultHTTP()
	}
	resp, err := client.Do(hreq)
	if err != nil {
		return nil, fmt.Errorf("call %s for model %s: %w", url, req.Model, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode > 299 ||
		!strings.Contains(resp.Header.Get("Content-Type"), "text/event-stream") {
		raw, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
		if err != nil {
			return nil, fmt.Errorf("read the reply from %s for model %s: %w", url, req.Model, err)
		}
		return judgeWhole(resp.StatusCode, raw, req.Model)
	}

	var (
		out      = &chatResponse{}
		text     strings.Builder
		sawText  bool
		finish   string
		sawEvent bool
	)
	sc := bufio.NewScanner(io.LimitReader(resp.Body, maxResponseBytes))
	sc.Buffer(make([]byte, 64<<10), 1<<20)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		data, ok := strings.CutPrefix(line, "data:")
		if !ok {
			continue
		}
		data = strings.TrimSpace(data)
		if data == "" {
			continue
		}
		if data == "[DONE]" {
			break
		}
		var ch streamChunk
		if err := json.Unmarshal([]byte(data), &ch); err != nil {
			return nil, fmt.Errorf("the provider's stream is not JSON (%w): %s", err, truncate(data, 256))
		}
		sawEvent = true
		if ch.Error != nil {
			return out, &APIError{Status: resp.StatusCode, Message: ch.Error.String(), Model: req.Model}
		}
		if ch.ID != "" {
			out.ID = ch.ID
		}
		if ch.Model != "" {
			out.Model = ch.Model
		}
		if ch.Usage != nil {
			out.Usage = *ch.Usage
		}
		for _, choice := range ch.Choices {
			if choice.Delta.Content != nil {
				text.WriteString(*choice.Delta.Content)
				sawText = true
			}
			frag := choice.Delta.ReasoningContent
			if frag == "" {
				frag = choice.Delta.Reasoning
			}
			if frag != "" && onThinking != nil {
				onThinking(frag)
			}
			if choice.FinishReason != "" {
				finish = choice.FinishReason
			}
		}
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("read the stream from %s for model %s: %w", url, req.Model, err)
	}
	if !sawEvent {
		return nil, fmt.Errorf("the provider sent an empty stream for model %s", req.Model)
	}
	msg := chatResponseMessage{Role: "assistant"}
	if sawText {
		s := text.String()
		msg.Content = &s
	}
	out.Choices = []chatChoice{{Message: msg, FinishReason: finish}}
	return out, nil
}

// judgeWhole judges a plain (non-stream) body exactly as Complete does.
func judgeWhole(status int, raw []byte, model string) (*chatResponse, error) {
	parsed, decErr := decodeResponse(raw)
	if status < 200 || status > 299 {
		msg := strings.TrimSpace(truncate(string(raw), 512))
		if decErr == nil && parsed.Error != nil {
			msg = parsed.Error.String()
		}
		apiErr := &APIError{Status: status, Message: msg, Model: model}
		if decErr == nil {
			return parsed, apiErr
		}
		return nil, apiErr
	}
	if decErr != nil {
		return nil, decErr
	}
	if parsed.Error != nil {
		return parsed, &APIError{Status: status, Message: parsed.Error.String(), Model: model}
	}
	return parsed, nil
}
