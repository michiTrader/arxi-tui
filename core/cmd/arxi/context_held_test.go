package main

import (
	"context"
	"testing"

	"github.com/michiTrader/arxi/internal/model"
	"github.com/michiTrader/arxi/internal/turn"
)

func TestContextHeldCountsAnthropicCacheButNotTwiceOnOpenAI(t *testing.T) {
	u := turn.Usage{InputTokens: 100, OutputTokens: 50, CacheReadTokens: 900_000, CacheWriteTokens: 4_000}
	if got := contextHeld(u, model.ProtocolAnthropicMessages); got != 100+50+900_000+4_000 {
		t.Fatalf("anthropic held = %d; cache tokens sit outside input_tokens and must be added", got)
	}
	// OpenAI reports cached tokens as a detail of prompt_tokens: adding them again
	// would count the same tokens twice.
	if got := contextHeld(u, model.ProtocolOpenAIChatCompletions); got != 150 {
		t.Fatalf("openai held = %d; want input+output only", got)
	}
}

func TestAddModelKeepsCachePricesAndWindowAndRefusesBadOnes(t *testing.T) {
	isolate(t)
	f := newFakeLLM(t, "m")
	if _, err := registerProvider("fake", f.url(), "", ""); err != nil {
		t.Fatal(err)
	}
	in, out, cr, cw := 3.0, 15.0, 0.3, 3.75
	if _, err := addModelWith("fake", "big", &in, &out, modelExtras{CacheRead: &cr, CacheWrite: &cw, ContextWindow: 1_000_000}); err != nil {
		t.Fatal(err)
	}
	store, _ := providerStore()
	p, _ := store.Load("fake")
	var got model.Model
	for _, m := range p.Models {
		if m.ID == "big" {
			got = m
		}
	}
	if got.ContextWindow != 1_000_000 || got.Price == nil || got.Price.CacheReadUSDPerMTok != 0.3 || got.Price.CacheWriteUSDPerMTok != 3.75 {
		t.Fatalf("stored = %+v; lost the window or the cache prices", got)
	}
	if _, err := addModelWith("fake", "c1", nil, nil, modelExtras{CacheRead: &cr}); err == nil {
		t.Fatal("a cache price alone must be refused")
	}
	if _, err := addModelWith("fake", "c2", &in, &out, modelExtras{ContextWindow: -1}); err == nil {
		t.Fatal("a negative window must be refused")
	}
	neg := -1.0
	if _, err := addModelWith("fake", "c3", &in, &out, modelExtras{CacheRead: &neg}); err == nil {
		t.Fatal("a negative cache price must be refused")
	}
}

func TestDiscoverLearnsTheWindowOnlyWhereNoneIsKnown(t *testing.T) {
	isolate(t)
	f := newFakeLLM(t, "a", "b")
	f.windows = map[string]int{"a": 1_000_000, "b": 200_000}
	if _, err := registerProvider("fake", f.url(), "", ""); err != nil {
		t.Fatal(err)
	}
	// "b" exists with a window the operator typed: discovery must not overwrite it.
	in, out := 1.0, 2.0
	if _, err := addModelWith("fake", "b", &in, &out, modelExtras{ContextWindow: 123_456}); err != nil {
		t.Fatal(err)
	}
	if _, err := discoverModels(context.Background(), "fake"); err != nil {
		t.Fatal(err)
	}
	store, _ := providerStore()
	p, _ := store.Load("fake")
	win := map[string]int{}
	for _, m := range p.Models {
		win[m.ID] = m.ContextWindow
	}
	if win["a"] != 1_000_000 {
		t.Fatalf("a = %d; the reported window should be stored for a new model", win["a"])
	}
	if win["b"] != 123_456 {
		t.Fatalf("b = %d; the operator's window must not be overwritten", win["b"])
	}
}
