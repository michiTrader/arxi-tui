package main

import "testing"

func TestParseWireWords(t *testing.T) {
	for in, want := range map[string]string{
		"": "", "auto": "", " Responses ": "responses", "openai-responses/v1": "responses",
		"chat": "openai", "anthropic": "anthropic", "messages": "anthropic",
	} {
		if got, ok := parseWire(in); !ok || got != want {
			t.Errorf("parseWire(%q) = %q,%v want %q", in, got, ok, want)
		}
	}
	if _, ok := parseWire("grpc"); ok {
		t.Error("an unknown format must be refused")
	}
}

// Consequence if this fails: a service that only serves /responses (River) cannot be added
// with the right wire and every turn answers empty. Remedy: the add form must forward it.
func TestTheOtherServiceFormSendsTheAPIFormat(t *testing.T) {
	f := newOtherForm("river")
	set := func(label, v string) {
		for i := range f.fields {
			if f.fields[i].label == label {
				f.fields[i].value = v
			}
		}
	}
	set("Base URL", "https://api.river.ai/v1")
	set("API key", "k")
	set(wireFieldLabel, "responses")
	w, msg := f.submit()
	if msg != "" || w.Protocol != "responses" {
		t.Fatalf("work=%+v msg=%q", w, msg)
	}
	set(wireFieldLabel, "grpc")
	if _, msg := f.submit(); msg == "" {
		t.Error("a wrong format must name the field")
	}
}

func TestEditingOnlyTheFormatSendsOnlyTheFormat(t *testing.T) {
	f := newEditForm("river", "https://api.river.ai/v1", "RIVER_KEY", "openai")
	if _, msg := f.submit(); msg != "nothing changed" {
		t.Fatalf("untouched form: %q", msg)
	}
	for i := range f.fields {
		if f.fields[i].label == wireFieldLabel {
			f.fields[i].value = "responses"
		}
	}
	w, msg := f.submit()
	if msg != "" || w.Protocol != "responses" || w.BaseURL != "" || w.EnvName != "" || w.Key != "" {
		t.Fatalf("work=%+v msg=%q", w, msg)
	}
	for i := range f.fields {
		if f.fields[i].label == wireFieldLabel {
			f.fields[i].value = ""
		}
	}
	if w, _ = f.submit(); w.Protocol != "auto" {
		t.Errorf("an emptied format must ask the core to detect again, got %q", w.Protocol)
	}
}

func TestWireWordRoundTrip(t *testing.T) {
	for p, want := range map[string]string{
		"openai-chat-completions/v1": "openai", "openai-responses/v1": "responses",
		"anthropic-messages/v1": "anthropic", "": "",
	} {
		if got := wireWord(p); got != want {
			t.Errorf("wireWord(%q) = %q want %q", p, got, want)
		}
	}
}
