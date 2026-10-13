package provider

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strconv"
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
	infos, err := c.ListModelInfo(ctx, protocol)
	if err != nil {
		return nil, err
	}
	ids := make([]string, len(infos))
	for i, m := range infos {
		ids[i] = m.ID
	}
	return ids, nil
}

// ModelInfo is one model a listing named, with the context window the listing
// reported for it (0 when it reported none: many servers do not).
type ModelInfo struct {
	ID            string
	ContextWindow int
}

// Bounds on a reported window. Anything outside is not a context window (a field
// that meant something else under the same name), and storing it would make every
// percentage on screen wrong.
const (
	minContextWindow = 1_000
	maxContextWindow = 100_000_000
)

// windowFields are the names listings use for the context window, in the order
// they are trusted: the input limit first, since that is what a prompt must fit.
var windowFields = []string{"max_input_tokens", "inputTokenLimit", "context_length", "context_window", "max_model_len", "n_ctx"}

// windowOf reads a context window out of one listing entry, 0 when it has none or
// the number is not plausible. JSON numbers arrive as float64; a string of digits
// is accepted too, since some gateways quote it.
func windowOf(entry map[string]any) int {
	for _, f := range windowFields {
		var n float64
		switch v := entry[f].(type) {
		case float64:
			n = v
		case string:
			x, err := strconv.ParseFloat(strings.TrimSpace(v), 64)
			if err != nil {
				continue
			}
			n = x
		default:
			continue
		}
		if n != float64(int64(n)) || n < minContextWindow || n > maxContextWindow {
			continue
		}
		return int(n)
	}
	return 0
}

// ListModelInfo is ListModels with what each entry says about its context window.
func (c *Client) ListModelInfo(ctx context.Context, protocol string) ([]ModelInfo, error) {
	key, err := c.credential()
	if err != nil {
		return nil, err
	}
	url := strings.TrimSuffix(c.BaseURL, "/") + "/models"
	hreq, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("build the model listing request: %w", err)
	}
	hreq.Header.Set("User-Agent", UserAgent)
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
	if msg := describeWebReply(url, resp.StatusCode, raw); msg != "" {
		return nil, errors.New(msg)
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return nil, fmt.Errorf("%s answered HTTP %d: %s", url, resp.StatusCode,
			strings.TrimSpace(truncate(string(raw), 300)))
	}
	var doc struct {
		Data   []map[string]any `json:"data"`
		Models []map[string]any `json:"models"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil, fmt.Errorf("%s did not answer with a model list: %w", url, err)
	}
	seen := map[string]bool{}
	var out []ModelInfo
	add := func(id string, entry map[string]any) {
		id = strings.TrimPrefix(strings.TrimSpace(id), "models/")
		if id == "" || strings.ContainsAny(id, " \t\r\n") || seen[id] {
			return
		}
		seen[id] = true
		out = append(out, ModelInfo{ID: id, ContextWindow: windowOf(entry)})
	}
	str := func(v any) string { s, _ := v.(string); return s }
	for _, d := range doc.Data {
		add(str(d["id"]), d)
	}
	for _, m := range doc.Models {
		if id := str(m["id"]); id != "" {
			add(id, m)
		} else {
			add(str(m["name"]), m)
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("%s answered, but it lists no models", url)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	if len(out) > maxDiscovered {
		out = out[:maxDiscovered]
	}
	return out, nil
}
