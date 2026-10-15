package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"
)

// pngB64 is a few real bytes of PNG header, in base64: enough for the wire, which does
// not look inside the picture.
var pngB64 = base64.StdEncoding.EncodeToString([]byte("\x89PNG\r\n\x1a\nxxxx"))

func imagesJSON(n int, mt string) string {
	one := `{"media_type":"` + mt + `","data":"` + pngB64 + `"}`
	parts := make([]string, n)
	for i := range parts {
		parts[i] = one
	}
	return "[" + strings.Join(parts, ",") + "]"
}

// setupImageModel registers a fake provider with one model, optionally set to take images.
func setupImageModel(t *testing.T, takes bool) *fakeLLM {
	t.Helper()
	isolate(t)
	f := newFakeLLM(t, "m1")
	if _, err := registerProvider("fake", f.url(), "", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := discoverModels(context.Background(), "fake"); err != nil {
		t.Fatal(err)
	}
	if takes {
		if _, err := handleModelUpdate(map[string]any{"model": "fake/m1", "images": true}); err != nil {
			t.Fatal(err)
		}
	}
	return f
}

func TestAPictureReachesAModelSetToTakeImages(t *testing.T) {
	f := setupImageModel(t, true)
	_, err := handleChatSend(map[string]any{"prompt": "what is this?", "model": "fake/m1", "images": imagesJSON(2, "image/png")})
	if err != nil {
		t.Fatal(err)
	}
	if f.calls != 1 {
		t.Fatalf("provider asked %d times, want 1", f.calls)
	}
	raw, _ := json.Marshal(f.messages[0][len(f.messages[0])-1])
	got := string(raw)
	if strings.Count(got, `"type":"image_url"`) != 2 || !strings.Contains(got, "data:image/png;base64,"+pngB64) || !strings.Contains(got, "what is this?") {
		t.Fatalf("the provider did not get the text and both pictures: %s", got)
	}
}

func TestAPictureIsRefusedBeforeBillingWhenTheModelIsNotSetToTakeImages(t *testing.T) {
	f := setupImageModel(t, false)
	_, err := handleChatSend(map[string]any{"prompt": "look", "model": "fake/m1", "images": imagesJSON(1, "image/png")})
	if err == nil || !strings.Contains(err.Error(), "is not set to take images") || !strings.Contains(err.Error(), "--images true") {
		t.Fatalf("err = %v, want the refusal that says how to turn it on", err)
	}
	if f.calls != 0 {
		t.Fatalf("the provider was asked %d times; a refused picture must cost nothing", f.calls)
	}
}

func TestTurningImagesOffRefusesAgain(t *testing.T) {
	f := setupImageModel(t, true)
	if _, err := handleModelUpdate(map[string]any{"model": "fake/m1", "images": false}); err != nil {
		t.Fatal(err)
	}
	if _, err := handleChatSend(map[string]any{"prompt": "look", "model": "fake/m1", "images": imagesJSON(1, "image/png")}); err == nil {
		t.Fatal("a model turned off still took a picture")
	}
	if f.calls != 0 {
		t.Fatalf("billed %d times", f.calls)
	}
}

func TestAMessageWithOnlyAPictureIsAMessage(t *testing.T) {
	f := setupImageModel(t, true)
	if _, err := handleChatSend(map[string]any{"prompt": "", "model": "fake/m1", "images": imagesJSON(1, "image/jpeg")}); err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(f.messages[0][len(f.messages[0])-1])
	if strings.Contains(string(raw), `"text":""`) {
		t.Fatalf("an empty text part went to the provider: %s", raw)
	}
	if _, err := handleChatSend(map[string]any{"prompt": "  ", "model": "fake/m1"}); err == nil {
		t.Fatal("an empty message with no picture was accepted")
	}
}

func TestBadPicturesAreRefusedWithNoProviderCall(t *testing.T) {
	f := setupImageModel(t, true)
	big := `[{"media_type":"image/png","data":"` + strings.Repeat("QUJD", maxChatImageBytes/4+1) + `"}]`
	for name, c := range map[string]struct{ raw, want string }{
		"not json":   {`nope`, "JSON list"},
		"svg":        {imagesJSON(1, "image/svg+xml"), "png, jpeg, gif or webp"},
		"bad base64": {`[{"media_type":"image/png","data":"@@@"}]`, "base64"},
		"no data":    {`[{"media_type":"image/png","data":""}]`, "base64"},
		"too many":   {imagesJSON(maxChatImages+1, "image/png"), "at most"},
		"too heavy":  {big, "at most"},
	} {
		_, err := handleChatSend(map[string]any{"prompt": "x", "model": "fake/m1", "images": c.raw})
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: err = %v, want %q", name, err, c.want)
		}
	}
	if f.calls != 0 {
		t.Fatalf("provider asked %d times for refused pictures", f.calls)
	}
}

// The pictures of one message must always leave room in the request line for the text
// and the conversation sent with them, or the largest allowed message would close the
// connection. 300 KB is what is kept free for those.
func TestThePictureCapLeavesRoomInTheRequestLine(t *testing.T) {
	const roomForTheRest = 300 * 1024
	data := strings.Repeat("QUJD", maxChatImageBytes/4)
	img, _ := json.Marshal([]wireImage{{MediaType: "image/png", Data: data}})
	// the list travels as a string inside the line, so its quotes are escaped once more
	line, _ := json.Marshal(map[string]any{"images": string(img)})
	if len(line)+roomForTheRest > maxLineBytes {
		t.Fatalf("a full picture message takes %d bytes of the %d line cap, leaving under %d for the text and history",
			len(line), maxLineBytes, roomForTheRest)
	}
}

func TestModelListSaysWhichModelsTakeImages(t *testing.T) {
	setupImageModel(t, true)
	got, err := handleModelList(nil)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(got)
	if !strings.Contains(string(b), `"images":true`) {
		t.Fatalf("model.list does not carry images: %s", b)
	}
}

func TestModelUpdateImagesKeepsTheWindowAndNeedsSomethingToChange(t *testing.T) {
	setupImageModel(t, false)
	if _, err := handleModelUpdate(map[string]any{"model": "fake/m1", "context_window": float64(200000)}); err != nil {
		t.Fatal(err)
	}
	if _, err := handleModelUpdate(map[string]any{"model": "fake/m1", "images": true}); err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(mustList(t))
	if !strings.Contains(string(b), `"context_window":200000`) || !strings.Contains(string(b), `"images":true`) {
		t.Fatalf("one update erased the other: %s", b)
	}
	if _, err := handleModelUpdate(map[string]any{"model": "fake/m1", "images": "yes"}); err == nil {
		t.Fatal("a non-boolean images was accepted")
	}
	if _, err := handleModelUpdate(map[string]any{"model": "fake/m1"}); err == nil {
		t.Fatal("an update with nothing to change was accepted")
	}
}

func mustList(t *testing.T) any {
	t.Helper()
	got, err := handleModelList(nil)
	if err != nil {
		t.Fatal(err)
	}
	return got
}

func TestModelAddCanDeclareImages(t *testing.T) {
	isolate(t)
	f := newFakeLLM(t)
	if _, err := registerProvider("fake", f.url(), "", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := handleModelAdd(map[string]any{"provider": "fake", "model": "pix", "images": true}); err != nil {
		t.Fatal(err)
	}
	if _, err := handleModelAdd(map[string]any{"provider": "fake", "model": "txt"}); err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(mustList(t))
	var rows struct {
		Models []struct {
			ID     string
			Images bool
		}
	}
	json.Unmarshal(b, &rows)
	for _, r := range rows.Models {
		if (r.ID == "pix") != r.Images {
			t.Errorf("%s images=%v", r.ID, r.Images)
		}
	}
}
