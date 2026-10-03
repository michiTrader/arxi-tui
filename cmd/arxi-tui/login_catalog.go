package main

import "strings"

// This file is the list of providers /login offers by name.
//
// # Why a list at all
//
// The core ships three providers it knows end to end (anthropic, openai, local:
// endpoint, protocol and models). Every other OpenAI-compatible service is one
// `--base-url` away, but a person who just wants to paste an OpenRouter key should
// not have to know OpenRouter's endpoint. So the TUI carries a short, honest list
// of services whose chat endpoint is stable and publicly documented, and fills the
// URL in for them. Anything not on the list is the "Other" row.
//
// # What the list is NOT
//
// It is not a statement about which models exist. The core cannot ask an endpoint
// what it serves, so a provider outside its own table registers with no models and
// the form asks for one (and its price). Guessing model ids here would put names in
// the product that go stale without a test noticing, and a stale model id fails at
// the first run, far from the screen that caused it.
//
// BaseURL is the root the core appends "/chat/completions" to (client.go), so each
// entry is the documented OpenAI-compatible base, not the full path. An entry whose
// URL is wrong registers fine and fails on first use, so entries are only added for
// endpoints with a documented OpenAI-compatible base; the rest go through "Other".

// catalogEntry is one named service.
type catalogEntry struct {
	// ID is the provider name written to the core: lower case, the key of the
	// stored credential and of the provider record.
	ID string
	// Display is what the list shows.
	Display string
	// BaseURL is the OpenAI-compatible base. Empty for a provider the core already
	// knows: sending an empty base_url makes the core use its own table, so the
	// endpoint (and, for anthropic, the native protocol) has exactly one source.
	BaseURL string
	// CoreKnown marks the providers in the core's table, which arrive with models.
	CoreKnown bool
	// NoKey marks a service that needs no credential (a local server), so the form
	// does not insist on one.
	NoKey bool
}

var loginCatalog = []catalogEntry{
	{ID: "anthropic", Display: "Anthropic", CoreKnown: true},
	{ID: "openai", Display: "OpenAI", CoreKnown: true},
	{ID: "local", Display: "Local server (Ollama)", CoreKnown: true, NoKey: true},
	{ID: "openrouter", Display: "OpenRouter", BaseURL: "https://openrouter.ai/api/v1"},
	{ID: "groq", Display: "Groq", BaseURL: "https://api.groq.com/openai/v1"},
	{ID: "deepseek", Display: "DeepSeek", BaseURL: "https://api.deepseek.com/v1"},
	{ID: "together", Display: "Together AI", BaseURL: "https://api.together.xyz/v1"},
	{ID: "mistral", Display: "Mistral", BaseURL: "https://api.mistral.ai/v1"},
	{ID: "fireworks", Display: "Fireworks AI", BaseURL: "https://api.fireworks.ai/inference/v1"},
	{ID: "cerebras", Display: "Cerebras", BaseURL: "https://api.cerebras.ai/v1"},
	{ID: "xai", Display: "xAI", BaseURL: "https://api.x.ai/v1"},
	{ID: "moonshot", Display: "Moonshot AI", BaseURL: "https://api.moonshot.ai/v1"},
	{ID: "gemini", Display: "Google Gemini", BaseURL: "https://generativelanguage.googleapis.com/v1beta/openai"},
	{ID: "perplexity", Display: "Perplexity", BaseURL: "https://api.perplexity.ai"},
	{ID: "nvidia", Display: "NVIDIA NIM", BaseURL: "https://integrate.api.nvidia.com/v1"},
	{ID: "huggingface", Display: "Hugging Face", BaseURL: "https://router.huggingface.co/v1"},
	{ID: "baseten", Display: "Baseten", BaseURL: "https://inference.baseten.co/v1"},
}

// catalogByID finds an entry by provider name, case-insensitively.
func catalogByID(id string) (catalogEntry, bool) {
	id = strings.ToLower(strings.TrimSpace(id))
	for _, e := range loginCatalog {
		if e.ID == id {
			return e, true
		}
	}
	return catalogEntry{}, false
}
