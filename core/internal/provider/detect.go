package provider

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/michiTrader/arxi/internal/model"
)

// detectTimeout bounds the whole detection: it runs while the user waits for a
// provider to be added, and a slow host must not hold that up.
const detectTimeout = 8 * time.Second

// DetectProtocol decides which wire a custom endpoint speaks, without spending a
// token: it sends each wire an EMPTY request body, which no model ever sees (a host
// that is up answers it with a JSON complaint about the missing fields).
//
// Why this exists: some providers put their OpenAI-style route behind a firewall rule
// (Cloudflare answers 403 with a web page) while the native Anthropic route of the same
// host is open, which is why other clients work and ours, speaking only the OpenAI wire
// to every custom URL, did not. A page where an API answer should be means "this route
// is not reachable for us"; any other answer means it is.
//
// The OpenAI wire wins when both are reachable (it is what almost every custom
// endpoint speaks). The result is "" when neither route is reachable or the network
// failed: the caller then keeps its default and says nothing it cannot know.
func DetectProtocol(ctx context.Context, baseURL, key string, hc *http.Client) string {
	if hc == nil {
		hc = &http.Client{}
	}
	ctx, cancel := context.WithTimeout(ctx, detectTimeout)
	defer cancel()
	base := strings.TrimRight(baseURL, "/")
	switch probe(ctx, hc, base+"/chat/completions", key, false) {
	case reachable:
		return model.ProtocolOpenAIChatCompletions
	case unreachable:
		if probe(ctx, hc, base+"/messages", key, true) == reachable {
			return model.ProtocolAnthropicMessages
		}
	}
	return ""
}

// DetectEndpoint is DetectProtocol that also finds the API root: a base URL given
// without its version segment (https://host, when the API lives at https://host/v1)
// answers every route with the site's own page, so /v1 is tried too. It returns the
// base URL that answered and the wire; both are "" when nothing answered.
func DetectEndpoint(ctx context.Context, baseURL, key string, hc *http.Client) (string, string) {
	base := strings.TrimRight(strings.TrimSpace(baseURL), "/")
	cands := []string{base}
	if !versioned(base) {
		cands = append(cands, base+"/v1")
	}
	for _, c := range cands {
		if proto := DetectProtocol(ctx, c, key, hc); proto != "" {
			return c, proto
		}
	}
	return "", ""
}

// versioned reports whether the URL already ends in a version segment (/v1, /v2beta).
func versioned(u string) bool {
	seg := u[strings.LastIndex(u, "/")+1:]
	return len(seg) >= 2 && seg[0] == 'v' && seg[1] >= '0' && seg[1] <= '9'
}

type routeState int

const (
	unknown routeState = iota
	reachable
	unreachable
)

// probe posts an empty JSON object to one route and classifies the answer. A web page
// (the firewall's or the site's own) or a missing route is unreachable; a transport
// failure is unknown, so a flaky network never talks the user out of a working default.
func probe(ctx context.Context, hc *http.Client, url, key string, anthropic bool) routeState {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader([]byte("{}")))
	if err != nil {
		return unknown
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", UserAgent)
	if anthropic {
		req.Header.Set("anthropic-version", anthropicVersion)
		if key != "" {
			req.Header.Set("x-api-key", key)
		}
	} else if key != "" {
		req.Header.Set("Authorization", "Bearer "+key)
	}
	resp, err := hc.Do(req)
	if err != nil {
		return unknown
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if looksLikeWebPage(raw) || resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusMethodNotAllowed {
		return unreachable
	}
	return reachable
}
