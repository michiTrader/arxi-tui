package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"

	"github.com/michiTrader/arxi/internal/model"
)

// maxDiscovered bounds how many model ids one listing may yield. Some gateways
// publish hundreds; more than this is noise, not a catalogue.
const maxDiscovered = 2000

// ListModels asks the endpoint which models it serves.
//
// Every OpenAI-compatible server, Ollama, Gemini's compatibility layer and
// Anthropic itself answer GET {base}/models with {"data":[{"id":...}]}, which
// is what lets a user register a provider and see its models without typing a
// single id. The credential is found exactly as a chat call finds it, so a key
// that works for chat works here and a missing key fails with the same message.
//
// The ids come back sorted, de-duplicated and without ids that could not be
// used as a model reference (whitespace). Gemini prefixes its ids with
// "models/"; that prefix is not part of the id the chat endpoint accepts.
func (c *Client) ListModels(ctx context.Context, protocol string) ([]string, error) {
	key, err := c.credential()
	if err != nil {
		return nil, err
	}
	url := strings.TrimSuffix(c.BaseURL, "/") + "/models"
	hreq, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("build the model listing request: %w", err)
	}
	hreq.Header.Set("Accept", "application/json")
	if protocol == model.ProtocolAnthropicMessages {
		hreq.Header.Set("anthropic-version", anthropicVersion)
		if key != "" {
			hreq.Header.Set("x-api-key", key)
		}
		hreq.URL.RawQuery = "limit=1000"
	} else if key != "" {
		hreq.Header.Set("Authorization", "Bearer "+key)
	}
	client := c.HTTP
	if client == nil {
		client = DefaultHTTP()
	}
	resp, err := client.Do(hreq)
	if err != nil {
		return nil, fmt.Errorf("call %s: %w", url, err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if err != nil {
		return nil, fmt.Errorf("read the reply from %s: %w", url, err)
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return nil, fmt.Errorf("%s answered HTTP %d: %s", url, resp.StatusCode,
			strings.TrimSpace(truncate(string(raw), 300)))
	}
	var doc struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
		Models []struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"models"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil, fmt.Errorf("%s did not answer with a model list: %w", url, err)
	}
	seen := map[string]bool{}
	var ids []string
	add := func(id string) {
		id = strings.TrimPrefix(strings.TrimSpace(id), "models/")
		if id == "" || strings.ContainsAny(id, " \t\r\n") || seen[id] {
			return
		}
		seen[id] = true
		ids = append(ids, id)
	}
	for _, d := range doc.Data {
		add(d.ID)
	}
	for _, m := range doc.Models {
		if m.ID != "" {
			add(m.ID)
		} else {
			add(m.Name)
		}
	}
	if len(ids) == 0 {
		return nil, fmt.Errorf("%s answered, but it lists no models", url)
	}
	sort.Strings(ids)
	if len(ids) > maxDiscovered {
		ids = ids[:maxDiscovered]
	}
	return ids, nil
}
