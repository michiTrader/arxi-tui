package driver

import (
	"context"
	"strings"
	"testing"
)

// TestProviderAddReturnsTheRegisteredProvider pins the happy path: a
// provider.add answered ok with a model.Provider snapshot must surface every
// field the user needs to confirm the registration -- the resolved protocol and
// base_url (which the core may have filled from its known table), the variable
// name the key is read from, and the models the provider offers with their
// enabled state.
func TestProviderAddReturnsTheRegisteredProvider(t *testing.T) {
	resp := `{"id":"provider-add","ok":true,"result":{"name":"anthropic","protocol":"anthropic-messages/v1","base_url":"https://api.anthropic.com/v1","api_key_env":"ANTHROPIC_API_KEY","models":[{"id":"claude-sonnet-4-6","enabled":true},{"id":"claude-opus-4-1","enabled":false}]}}`
	d := session(t, resp)

	res, err := d.SubmitProviderAdd(context.Background(), ProviderAddParams{Name: "anthropic"})
	if err != nil {
		t.Fatalf("provider.add answered ok and SubmitProviderAdd still reported an "+
			"error: %v", err)
	}
	if res.Name != "anthropic" || res.Protocol != "anthropic-messages/v1" {
		t.Fatalf("got name=%q protocol=%q, want anthropic and anthropic-messages/v1; "+
			"the resolved protocol is a field the user cannot see any other way",
			res.Name, res.Protocol)
	}
	if res.BaseURL != "https://api.anthropic.com/v1" || res.APIKeyEnv != "ANTHROPIC_API_KEY" {
		t.Fatalf("got base_url=%q api_key_env=%q; the endpoint and the variable name "+
			"are what the user registered the provider to confirm", res.BaseURL, res.APIKeyEnv)
	}
	if len(res.Models) != 2 || res.Models[0].ID != "claude-sonnet-4-6" || !res.Models[0].Enabled || res.Models[1].Enabled {
		t.Fatalf("models projection wrong: %+v; the first model is enabled on "+
			"registration and the rest disabled, a spend decision the user must see", res.Models)
	}
}

// TestProviderAddRefusalIsReturnedAsAnError holds provider.add to the contract
// SubmitRunStart established: ok:false is an answered refusal, not a transport
// success. The refusal used is the one validateKeyEnv raises when a key is
// passed where a variable name belongs -- the most important refusal the
// command has, and the exact case the api_key_env invariant exists to catch.
func TestProviderAddRefusalIsReturnedAsAnError(t *testing.T) {
	refusal := `{"id":"provider-add","ok":false,"error":{"code":"bad_params","message":"--api-key-env takes a variable NAME, not a key","fix":["arxi provider add openai --api-key-env OPENAI_API_KEY"]}}`
	d := session(t, refusal)

	res, err := d.SubmitProviderAdd(context.Background(), ProviderAddParams{
		Name: "openai", APIKeyEnv: "sk-secret-looking-value",
	})
	if err == nil {
		t.Fatal("the core refused provider.add and SubmitProviderAdd reported success; " +
			"a key-shaped api_key_env is the refusal this invariant exists to catch")
	}
	if res != nil {
		t.Fatalf("SubmitProviderAdd returned a non-nil result alongside a refusal: %+v", res)
	}
	ref, ok := err.(*Refusal)
	if !ok {
		t.Fatalf("SubmitProviderAdd returned a %T, want *Refusal", err)
	}
	if ref.Code != "bad_params" || ref.Type != "provider.add" {
		t.Fatalf("refusal carried code=%q type=%q, want bad_params and provider.add",
			ref.Code, ref.Type)
	}
}

// TestProviderAddEmptyNameIsRefusedLocally pins the local guard: an unnamed
// provider addresses no record, so SubmitProviderAdd refuses it before a send
// rather than borrowing the core's validateName round-trip -- the rule
// SubmitRunCancel follows for an empty run id.
func TestProviderAddEmptyNameIsRefusedLocally(t *testing.T) {
	d := session(t) // no response needed: nothing should be sent
	res, err := d.SubmitProviderAdd(context.Background(), ProviderAddParams{Name: ""})
	if err == nil {
		t.Fatal("SubmitProviderAdd sent an unnamed provider.add; an empty name " +
			"addresses no record to write and must be refused locally")
	}
	if res != nil {
		t.Fatalf("SubmitProviderAdd returned a result for an unnamed provider: %+v", res)
	}
}

// TestProviderAddOkWithNoNameFailsLoud pins the one result shape that must not
// be accepted: ok:true with an empty name. The name is how model.list and
// model.enable reach the provider, so a nameless success is a silent dead end.
func TestProviderAddOkWithNoNameFailsLoud(t *testing.T) {
	resp := `{"id":"provider-add","ok":true,"result":{"name":"","base_url":"https://x/v1"}}`
	d := session(t, resp)
	res, err := d.SubmitProviderAdd(context.Background(), ProviderAddParams{Name: "x"})
	if err == nil {
		t.Fatal("provider.add answered ok with no name and SubmitProviderAdd " +
			"accepted it; a provider the host cannot name is unreachable")
	}
	if res != nil {
		t.Fatalf("SubmitProviderAdd returned an unusable result: %+v", res)
	}
}

// TestProviderAddOmitsUnsetOptionalsAndSendsSetOnes pins the wire discipline
// SubmitRunStart established: an optional the caller did not set is absent from
// the request (not sent as ""), and one the caller set reaches the wire. The
// counterfactual of each other: an always-send would put base_url:"" on the
// wire, which the core reads as an explicit empty endpoint rather than an
// omission it fills from its table.
func TestProviderAddOmitsUnsetOptionalsAndSendsSetOnes(t *testing.T) {
	// Omission: only a name set.
	var w1 recordingWriter
	resp := `{"id":"provider-add","ok":true,"result":{"name":"local","base_url":"http://127.0.0.1:11434/v1"}}`
	d1 := sessionWithWriter(t, &w1, resp)
	if _, err := d1.SubmitProviderAdd(context.Background(), ProviderAddParams{Name: "local"}); err != nil {
		t.Fatalf("provider.add with only a name: %v", err)
	}
	if strings.Contains(w1.String(), "base_url") || strings.Contains(w1.String(), "api_key_env") {
		t.Fatalf("an unset optional reached the wire: %s; the core fills base_url "+
			"from its table on omission, and an empty one is a different request", w1.String())
	}

	// Presence: base_url and api_key_env both set.
	var w2 recordingWriter
	d2 := sessionWithWriter(t, &w2, resp)
	_, err := d2.SubmitProviderAdd(context.Background(), ProviderAddParams{
		Name: "together", BaseURL: "https://api.together.xyz/v1", APIKeyEnv: "TOGETHER_API_KEY",
	})
	if err != nil {
		t.Fatalf("provider.add with base_url and api_key_env: %v", err)
	}
	if !strings.Contains(w2.String(), `"base_url":"https://api.together.xyz/v1"`) ||
		!strings.Contains(w2.String(), `"api_key_env":"TOGETHER_API_KEY"`) {
		t.Fatalf("a set optional did not reach the wire: %s", w2.String())
	}
}

// TestModelListReturnsRows pins model.list: the rows carry the provider per
// model, the K2 choice that makes model.list the single read for both
// "what providers" and "what models".
func TestModelListReturnsRows(t *testing.T) {
	resp := `{"id":"model-list","ok":true,"result":{"models":[{"provider":"anthropic","id":"claude-sonnet-4-6","enabled":true},{"provider":"openai","id":"gpt-5.1","enabled":false}]}}`
	d := session(t, resp)
	res, err := d.SubmitModelList(context.Background())
	if err != nil {
		t.Fatalf("model.list answered ok and SubmitModelList reported an error: %v", err)
	}
	if len(res.Models) != 2 || res.Models[0].Provider != "anthropic" || res.Models[1].Provider != "openai" {
		t.Fatalf("rows wrong: %+v; the provider per row is what groups the list", res.Models)
	}
}

// TestModelListEmptyIsNotAFailure pins that an empty list is a valid answer --
// no provider registered yet -- so there is no fail-loud guard: nothing is
// named because there is honestly nothing to name.
func TestModelListEmptyIsNotAFailure(t *testing.T) {
	resp := `{"id":"model-list","ok":true,"result":{"models":[]}}`
	d := session(t, resp)
	res, err := d.SubmitModelList(context.Background())
	if err != nil {
		t.Fatalf("an empty model.list is a valid answer and must not error: %v", err)
	}
	if len(res.Models) != 0 {
		t.Fatalf("expected no rows, got %+v", res.Models)
	}
}

// TestModelEnableReportsChanged pins the enable path: the result carries which
// model was toggled, its resulting state, and whether anything changed -- the
// bool arxi returns so the command can say "already enabled" instead of a
// success that did nothing.
func TestModelEnableReportsChanged(t *testing.T) {
	resp := `{"id":"model-enable","ok":true,"result":{"provider":"openai","model":"gpt-5.1","enabled":true,"changed":true}}`
	d := session(t, resp)
	res, err := d.SubmitModelEnable(context.Background(), "openai/gpt-5.1", true)
	if err != nil {
		t.Fatalf("model.enable answered ok and SubmitModelEnable reported an error: %v", err)
	}
	if res.Model != "gpt-5.1" || !res.Enabled || !res.Changed {
		t.Fatalf("result wrong: %+v; the changed flag distinguishes a real toggle "+
			"from an already-enabled no-op", res)
	}
}

// TestModelDisableUsesTheDisableVerb pins that on=false sends model.disable, not
// model.enable: the two verbs are one toggle in the method but a distinct act on
// the wire, so a refusal is addressable to the one the caller requested.
func TestModelDisableUsesTheDisableVerb(t *testing.T) {
	var w recordingWriter
	resp := `{"id":"model-enable","ok":true,"result":{"provider":"openai","model":"gpt-5.1","enabled":false,"changed":true}}`
	d := sessionWithWriter(t, &w, resp)
	if _, err := d.SubmitModelEnable(context.Background(), "openai/gpt-5.1", false); err != nil {
		t.Fatalf("model.disable: %v", err)
	}
	if !strings.Contains(w.String(), `"type":"model.disable"`) {
		t.Fatalf("on=false did not send model.disable: %s", w.String())
	}
}

// TestModelEnableEmptyRefRefusedLocally pins the local guard: an empty ref
// addresses no model, refused before a send.
func TestModelEnableEmptyRefRefusedLocally(t *testing.T) {
	d := session(t)
	res, err := d.SubmitModelEnable(context.Background(), "", true)
	if err == nil {
		t.Fatal("SubmitModelEnable sent a toggle with no model ref; an empty ref " +
			"addresses no model and must be refused locally")
	}
	if res != nil {
		t.Fatalf("returned a result for an empty ref: %+v", res)
	}
}

// TestModelEnableOkWithNoModelFailsLoud pins that an ok result naming no model
// is unverifiable -- the caller cannot confirm what it toggled, so the changed
// answer is meaningless.
func TestModelEnableOkWithNoModelFailsLoud(t *testing.T) {
	resp := `{"id":"model-enable","ok":true,"result":{"provider":"openai","model":"","changed":true}}`
	d := session(t, resp)
	res, err := d.SubmitModelEnable(context.Background(), "openai/gpt-5.1", true)
	if err == nil {
		t.Fatal("model.enable answered ok with no model named and SubmitModelEnable " +
			"accepted it; a toggle the host cannot attribute is unverifiable")
	}
	if res != nil {
		t.Fatalf("returned an unusable result: %+v", res)
	}
}

// TestModelListCarriesTheDeclaredPriceOnlyWhenThereIsOne pins that a row with no
// declared price decodes to a nil Price, so the hub shows "no declared price"
// instead of a misleading 0/0.
func TestModelListCarriesTheDeclaredPriceOnlyWhenThereIsOne(t *testing.T) {
	resp := `{"id":"model-list","ok":true,"result":{"models":[{"provider":"a","id":"priced","enabled":true,"price":{"in_usd_per_mtok":1.5,"out_usd_per_mtok":7}},{"provider":"a","id":"bare","enabled":true}]}}`
	d := session(t, resp)
	res, err := d.SubmitModelList(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if p := res.Models[0].Price; p == nil || p.In != 1.5 || p.Out != 7 {
		t.Errorf("declared price lost: %+v", p)
	}
	if res.Models[1].Price != nil {
		t.Errorf("an unpriced model decoded with a price: %+v", res.Models[1].Price)
	}
}
