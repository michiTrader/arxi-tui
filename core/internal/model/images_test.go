package model

import (
	"encoding/json"
	"strings"
	"testing"
)

// A model does not take images until somebody says so: an image sent to a model that cannot
// read it is billed and answered about nothing.
func TestAModelTakesNoImagesUntilSomebodySaysSo(t *testing.T) {
	p := openAIGatewayOffering(t, "gw", Model{ID: "text-only", Enabled: true})
	got, err := Resolve([]Provider{p}, "text-only")
	if err != nil {
		t.Fatal(err)
	}
	if got.Images {
		t.Error("a model nobody said takes images was resolved as taking them")
	}
}

func TestSettingImagesReachesTheResolutionAndCanBeTurnedOff(t *testing.T) {
	p := openAIGatewayOffering(t, "gw", Model{ID: "a", Enabled: true}, Model{ID: "b", Enabled: true})
	if !p.SetImages("a", true) {
		t.Fatal("SetImages said the model is not there")
	}
	ps := []Provider{p}
	if r, _ := Resolve(ps, "a"); !r.Images {
		t.Error("a model marked as taking images resolves without them")
	}
	if r, _ := Resolve(ps, "b"); r.Images {
		t.Error("setting one model's images changed another's")
	}
	p.SetImages("a", false)
	if r, _ := Resolve([]Provider{p}, "a"); r.Images {
		t.Error("the operator turned images off again and the model still takes them")
	}
	if p.SetImages("missing", true) {
		t.Error("SetImages reported a model that does not exist")
	}
}

// The flag is stored in the provider's file; a record written before it existed reads as no.
func TestImagesAreStoredAndAnOldRecordMeansNo(t *testing.T) {
	p := openAIGatewayOffering(t, "gw", Model{ID: "a", Enabled: true})
	p.SetImages("a", true)
	raw, err := json.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"images":true`) {
		t.Errorf("the stored form does not carry the flag: %s", raw)
	}
	var back Provider
	if err := json.Unmarshal(raw, &back); err != nil || !back.Models[0].Images {
		t.Fatalf("the flag did not survive a round trip: %v %+v", err, back.Models)
	}
	var old Provider
	if err := json.Unmarshal([]byte(`{"name":"gw","base_url":"https://x/v1","models":[{"id":"a","enabled":true}]}`), &old); err != nil {
		t.Fatal(err)
	}
	if old.Models[0].Images {
		t.Error("a record written before the flag existed reads as taking images")
	}
	// off is not written at all, so existing files are not touched
	p.SetImages("a", false)
	raw, _ = json.Marshal(p)
	if strings.Contains(string(raw), "images") {
		t.Errorf("an unset flag is written to the file: %s", raw)
	}
}
