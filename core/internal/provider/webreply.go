package provider

import (
	"bytes"
	"fmt"
	"net/url"
	"regexp"
	"strings"
)

// A provider endpoint answers JSON. When it answers a web page instead, the raw
// body is useless to the reader: a screenful of <meta> tags that hides the one
// thing they need, which is WHY. There are two causes with two different cures,
// and the page itself tells them apart:
//
//   - a bot-protection page (Cloudflare "Attention Required", "Just a moment"):
//     the host refused this machine's request before the API saw it; nothing is
//     wrong with the configuration.
//   - the provider's own website: the base URL points at the site, not at its
//     API, so every call lands on the single-page app. Usually the base URL
//     lacks its version segment, typically /v1.
//
// describeWebReply names the cause, the consequence and the remedy in one
// sentence, and never repeats the markup.

var titleRE = regexp.MustCompile(`(?is)<title[^>]*>(.*?)</title>`)

// looksLikeWebPage reports whether a body is HTML rather than data.
func looksLikeWebPage(raw []byte) bool {
	head := bytes.ToLower(bytes.TrimSpace(raw))
	if len(head) > 512 {
		head = head[:512]
	}
	return bytes.HasPrefix(head, []byte("<!doctype html")) ||
		bytes.HasPrefix(head, []byte("<html")) ||
		bytes.HasPrefix(head, []byte("<!--")) && bytes.Contains(head, []byte("<html"))
}

// isBotWall reports whether an HTML body is a bot-protection challenge.
func isBotWall(raw []byte) bool {
	low := strings.ToLower(string(raw))
	for _, m := range []string{
		"attention required", "just a moment", "cf-browser-verification",
		"cf-challenge", "challenge-platform", "checking your browser",
		"enable javascript and cookies", "access denied", "ddos protection",
	} {
		if strings.Contains(low, m) {
			return true
		}
	}
	return strings.Contains(low, "cloudflare") && !strings.Contains(low, "<div id=\"root\"")
}

// describeWebReply returns the explanation for an HTML reply, or "" when the
// body is not a web page. endpoint is the URL that was called, status the HTTP
// status it answered.
func describeWebReply(endpoint string, status int, raw []byte) string {
	if !looksLikeWebPage(raw) {
		return ""
	}
	title := ""
	if m := titleRE.FindSubmatch(raw); m != nil {
		title = strings.Join(strings.Fields(string(m[1])), " ")
		if len(title) > 80 {
			title = title[:80]
		}
	}
	named := ""
	if title != "" {
		named = fmt.Sprintf(" (page title %q)", title)
	}
	if isBotWall(raw) {
		return fmt.Sprintf("%s answered HTTP %d with a bot-protection page%s, not with the API: the host's firewall "+
			"(Cloudflare or similar) refused this request before it reached the model, so nothing was sent or billed. "+
			"The configuration is probably fine: retry in a minute, try another network or turn off a VPN, "+
			"or ask the provider to allow API clients from your address", endpoint, status, named)
	}
	return fmt.Sprintf("%s answered HTTP %d with a web page%s, not with the API: the provider's base URL points at "+
		"its website, so no model was called. Point the base URL at the API root, which usually ends in /v1 "+
		"(for example https://host/v1), with: arxi provider update <name> --base-url <url>%s",
		endpoint, status, named, baseURLHint(endpoint))
}

// baseURLHint suggests the corrected base URL when the called endpoint shows
// what was configured. endpoint ends in /chat/completions, /messages or
// /models, so the base is everything before it.
func baseURLHint(endpoint string) string {
	base := endpoint
	for _, tail := range []string{"/chat/completions", "/messages", "/models"} {
		if strings.HasSuffix(base, tail) {
			base = strings.TrimSuffix(base, tail)
			break
		}
	}
	u, err := url.Parse(base)
	if err != nil || u.Host == "" {
		return ""
	}
	if strings.HasSuffix(strings.TrimSuffix(u.Path, "/"), "/v1") {
		return ""
	}
	return fmt.Sprintf("; you configured %s, try %s/v1", base, strings.TrimSuffix(base, "/"))
}

// webReplyError turns an HTML reply into the error the caller returns, or nil
// when the body is not a web page. It is an *APIError, so the status keeps its
// meaning for the retry decision (a 5xx challenge may pass, a 403 will not).
func webReplyError(endpoint string, status int, raw []byte, model string) error {
	msg := describeWebReply(endpoint, status, raw)
	if msg == "" {
		return nil
	}
	return &APIError{Status: status, Message: msg, Model: model, Web: true}
}

// UserAgent names this program on every provider call. Go's default,
// "Go-http-client/1.1", is the first thing bot-protection rules block, and the
// block arrives as a web page instead of an answer. A named client is both
// politer and passes where an anonymous one is refused.
const UserAgent = "arxi/0.1 (+https://github.com/michiTrader/arxi)"
