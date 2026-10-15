package provider

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/michiTrader/arxi/internal/turn"
)

func pic(mt, data string) turn.ContentBlock {
	return turn.ContentBlock{Type: turn.BlockImage, Source: &turn.MediaSource{MediaType: mt, Data: data}}
}

func txt(s string) turn.ContentBlock { return turn.ContentBlock{Type: turn.BlockText, Text: s} }

func TestOpenAISendsAPictureAsADataURLNextToTheText(t *testing.T) {
	msgs, err := openAIMessage(turn.Message{Role: turn.RoleUser, Content: []turn.ContentBlock{
		txt("what is this?"), pic("image/png", "QUJD"), txt("be brief")}})
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(msgs)
	got := string(raw)
	want := `[{"role":"user","content":[{"text":"what is this?","type":"text"},` +
		`{"image_url":{"url":"data:image/png;base64,QUJD"},"type":"image_url"},` +
		`{"text":"be brief","type":"text"}]}]`
	if got != want {
		t.Fatalf("wire changed:\n got %s\nwant %s", got, want)
	}
}

func TestOpenAIPlainTextStaysAPlainString(t *testing.T) {
	msgs, err := openAIMessage(turn.Message{Role: turn.RoleUser, Content: []turn.ContentBlock{txt("hi")}})
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(msgs)
	if string(raw) != `[{"role":"user","content":"hi"}]` {
		t.Fatalf("text-only message changed shape: %s", raw)
	}
}

func TestOpenAIRefusesAPictureInASystemOrAssistantMessage(t *testing.T) {
	for _, role := range []turn.Role{turn.RoleSystem, turn.RoleAssistant, turn.RoleTool} {
		if _, err := openAIMessage(turn.Message{Role: role, Content: []turn.ContentBlock{pic("image/png", "QUJD")}}); err == nil {
			t.Errorf("a picture in a %s message was accepted", role)
		}
	}
}

func TestAnthropicSendsAPictureAsABase64Source(t *testing.T) {
	msg, err := anthropicTurnMessage(turn.Message{Role: turn.RoleUser, Content: []turn.ContentBlock{
		pic("image/jpeg", "QUJD"), txt("describe")}})
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(msg)
	want := `{"role":"user","content":[{"type":"image","source":{"data":"QUJD","media_type":"image/jpeg","type":"base64"}},{"type":"text","text":"describe"}]}`
	if string(raw) != want {
		t.Fatalf("wire changed:\n got %s\nwant %s", raw, want)
	}
}

func TestAnthropicRefusesAPictureOutsideAUserMessage(t *testing.T) {
	for _, role := range []turn.Role{turn.RoleAssistant, turn.RoleTool} {
		if _, err := anthropicTurnMessage(turn.Message{Role: role, Content: []turn.ContentBlock{pic("image/png", "QUJD")}}); err == nil {
			t.Errorf("a picture in a %s message was accepted", role)
		}
	}
}

func TestABadPictureIsRefusedBeforeAnyRequestIsBuilt(t *testing.T) {
	bad := map[string]turn.ContentBlock{
		"no source":  {Type: turn.BlockImage},
		"by url":     {Type: turn.BlockImage, Source: &turn.MediaSource{MediaType: "image/png", Data: "QUJD", URL: "http://x/y.png"}},
		"no data":    pic("image/png", ""),
		"svg":        pic("image/svg+xml", "QUJD"),
		"empty type": pic("", "QUJD"),
	}
	for name, block := range bad {
		m := turn.Message{Role: turn.RoleUser, Content: []turn.ContentBlock{block}}
		if _, err := openAIMessage(m); err == nil {
			t.Errorf("openai accepted %s", name)
		}
		if _, err := anthropicTurnMessage(m); err == nil {
			t.Errorf("anthropic accepted %s", name)
		}
	}
	_, err := openAIMessage(turn.Message{Role: turn.RoleUser, Content: []turn.ContentBlock{pic("image/svg+xml", "QUJD")}})
	if err == nil || !strings.Contains(err.Error(), "png, jpeg, gif or webp") {
		t.Fatalf("the error should say which formats work, got %v", err)
	}
}
