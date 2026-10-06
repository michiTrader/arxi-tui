package main

import (
	"net/url"
	"strconv"
	"strings"
	"unicode"
)

// This file is the typed-fields half of the provider hub: the forms for adding a
// provider, editing one and adding models by hand.
//
// The one rule here is the one the old /login wizard held and this keeps: a secret
// lives ONLY in a field's value, is published as bullets, and never appears in an
// error. Validation names the FIELD that is wrong, never its value.

type fieldKind int

const (
	kindText   fieldKind = iota
	kindName             // a provider name
	kindURL              // an http(s) base URL
	kindKey              // an API key (secret)
	kindEnv              // the NAME of an environment variable
	kindModels           // model ids, comma separated
	kindPrice            // a price, USD per million tokens
)

// hubField is one input of a form.
type hubField struct {
	label    string
	value    string
	secret   bool // shown as bullets
	envName  bool // a variable NAME: shown as bullets if it is not shaped like one
	required bool
	kind     fieldKind
	// unchanged is the placeholder of an edit form's field: leaving it empty keeps
	// what the core stores.
	unchanged string
}

type formKind int

const (
	formAdd      formKind = iota // a catalog service: only the key
	formAddOther                 // any OpenAI-compatible service
	formEdit                     // URL / key / key variable of a registered provider
	formModels                   // model ids (and prices) added by hand
	formSearch                   // the key or address of a web search service
)

// hubForm is the open form. It is wiped when it closes: the key must not outlive it.
type hubForm struct {
	kind   formKind
	title  string
	help   string
	target catalogEntry // formAdd: the service being added
	prov   string       // formEdit / formModels: the provider
	fields []hubField
	focus  int

	origURL, origEnv string // formEdit: what the core holds now
	backend          string // formSearch: brave, exa or searxng
}

// isEnvName reports whether s is shaped like an environment variable NAME. A value
// that is not is rendered masked: the likeliest way to put a secret in this field is
// pasting the key into it.
func isEnvName(s string) bool {
	for i, r := range s {
		switch {
		case r == '_', r >= 'A' && r <= 'Z', r >= 'a' && r <= 'z':
		case r >= '0' && r <= '9' && i > 0:
		default:
			return false
		}
	}
	return true
}

// shown is what a field displays. This is the single function between a typed value
// and the screen.
func (f hubField) shown() string {
	if f.value == "" {
		switch {
		case f.unchanged != "":
			return f.unchanged
		case f.required:
			return "(required)"
		}
		return "(optional)"
	}
	if f.secret || (f.envName && !isEnvName(f.value)) {
		return mask(f.value)
	}
	return f.value
}

// masked is the text the input line shows while this field has the focus.
func (f hubField) masked() string {
	if f.secret || (f.envName && !isEnvName(f.value)) {
		return mask(f.value)
	}
	return f.value
}

// mask is a run of bullets, one per rune and capped at 40 so even the length of a
// long secret is bounded.
func mask(s string) string {
	n := len([]rune(s))
	if n > 40 {
		n = 40
	}
	return strings.Repeat("•", n)
}

func dropLastRune(s string) string {
	r := []rune(s)
	if len(r) == 0 {
		return s
	}
	return string(r[:len(r)-1])
}

// cleanFieldText removes everything a single-line field cannot hold: whitespace and
// control characters. A key never contains either, so stripping them is what makes a
// pasted key with a trailing newline work instead of fail.
func cleanFieldText(s string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsSpace(r) || unicode.IsControl(r) {
			return -1
		}
		return r
	}, s)
}

// cleanFilterText is cleanFieldText for the filter line, where a space is legal
// ("open ai") but control characters are not.
func cleanFilterText(s string) string {
	return strings.Map(func(r rune) rune {
		if r == '\n' || r == '\r' || r == '\t' {
			return ' '
		}
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, s)
}

func priceFields() []hubField {
	return []hubField{
		{label: "Price in (USD / 1M tokens)", kind: kindPrice},
		{label: "Price out (USD / 1M tokens)", kind: kindPrice},
	}
}

func newAddForm(e catalogEntry) *hubForm {
	return &hubForm{
		kind:   formAdd,
		title:  "Add " + e.Display,
		target: e,
		help: "Paste your " + e.Display + " API key. It is stored by the arxi core (never shown, never in a log), " +
			"and the models are fetched for you.",
		fields: []hubField{{label: "API key", secret: true, required: true, kind: kindKey}},
	}
}

func newOtherForm(name string) *hubForm {
	return &hubForm{
		kind:  formAddOther,
		title: "Add another service",
		help: "Any service that speaks the OpenAI chat API. Give its name, its base URL (the part before " +
			"/chat/completions) and a key. If you know the model ids, list them separated by commas; otherwise " +
			"they are fetched from the service.",
		fields: []hubField{
			{label: "Name", value: name, required: true, kind: kindName},
			{label: "Base URL", required: true, kind: kindURL},
			{label: "API key", secret: true, kind: kindKey},
			{label: "Env var name", envName: true, kind: kindEnv},
			{label: "Model ids", kind: kindModels},
		},
	}
}

func newEditForm(name, baseURL, envName string) *hubForm {
	return &hubForm{
		kind:  formEdit,
		title: "Edit " + name,
		prov:  name,
		help: "Change the base URL, store a new key, or point to an environment variable. " +
			"Leave a field as it is to keep it. Clear the variable name to stop using it.",
		fields: []hubField{
			{label: "Base URL", value: baseURL, kind: kindURL},
			{label: "API key", secret: true, kind: kindKey, unchanged: "(unchanged)"},
			{label: "Env var name", value: envName, envName: true, kind: kindEnv},
		},
		origURL: baseURL,
		origEnv: envName,
	}
}

func newModelsForm(prov string) *hubForm {
	return &hubForm{
		kind:  formModels,
		title: "Add models to " + prov,
		prov:  prov,
		help: "Type one or more model ids separated by commas, exactly as the service names them. " +
			"Prices are optional (USD per million tokens) and apply to every id you list; give both or neither.",
		fields: append([]hubField{{label: "Model ids", required: true, kind: kindModels}}, priceFields()...),
	}
}

// parseModelIDs splits "a, b c,a" into ["a","b","c"]: commas and spaces separate,
// duplicates collapse, order is kept.
func parseModelIDs(s string) []string {
	var out []string
	seen := map[string]bool{}
	for _, id := range strings.FieldsFunc(s, func(r rune) bool { return r == ',' || unicode.IsSpace(r) }) {
		if !seen[id] {
			seen[id] = true
			out = append(out, id)
		}
	}
	return out
}

func (f *hubForm) get(label string) string {
	for _, fl := range f.fields {
		if fl.label == label {
			return strings.TrimSpace(fl.value)
		}
	}
	return ""
}

// wipe drops every typed value, secrets included.
func (f *hubForm) wipe() {
	for i := range f.fields {
		f.fields[i].value = ""
	}
	f.focus = 0
}

// submit validates the form and builds the work. The returned message names the FIELD
// that is wrong and never its value: the value may be a secret, and a refusal that
// quotes it would put the secret in the banner.
func (f *hubForm) submit() (hubWork, string) {
	for _, fl := range f.fields {
		v := strings.TrimSpace(fl.value)
		if fl.required && v == "" {
			return hubWork{}, fl.label + " is required"
		}
		switch fl.kind {
		case kindURL:
			if v == "" {
				continue
			}
			u, err := url.Parse(v)
			if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
				return hubWork{}, fl.label + " must start with http:// or https://"
			}
		case kindEnv:
			if !isEnvName(v) {
				return hubWork{}, fl.label + " is the NAME of a variable (like MY_API_KEY), not the key itself; type the key in the API key field"
			}
		case kindName:
			if v != sanitizeProviderName(v) {
				return hubWork{}, fl.label + " may use only lower-case letters, digits, - and _"
			}
		case kindPrice:
			if v != "" {
				if n, err := strconv.ParseFloat(v, 64); err != nil || n < 0 {
					return hubWork{}, fl.label + " must be a number, 0 or more"
				}
			}
		}
	}

	switch f.kind {
	case formSearch:
		return hubWork{Op: opSearch, Name: f.backend, Key: f.get("API key"), BaseURL: f.get("Address"), Close: true}, ""

	case formAdd:
		return hubWork{Op: opAdd, Name: f.target.ID, BaseURL: f.target.BaseURL, Key: f.get("API key")}, ""

	case formAddOther:
		w := hubWork{Op: opAdd, Name: f.get("Name"), BaseURL: f.get("Base URL"),
			Key: f.get("API key"), EnvName: f.get("Env var name"), Models: parseModelIDs(f.get("Model ids"))}
		if w.Key == "" && w.EnvName == "" {
			return hubWork{}, "give an API key, or the name of the environment variable that holds it"
		}
		return w, ""

	case formEdit:
		w := hubWork{Op: opUpdate, Name: f.prov, Key: f.get("API key")}
		if u := f.get("Base URL"); u != f.origURL {
			w.BaseURL = u
		}
		if e := f.get("Env var name"); e != f.origEnv {
			w.EnvName = e
			if e == "" {
				w.EnvName = "none" // the core's spelling of "stop using a variable"
			}
		}
		if w.BaseURL == "" && w.EnvName == "" && w.Key == "" {
			return hubWork{}, "nothing changed"
		}
		return w, ""

	case formModels:
		ids := parseModelIDs(f.get("Model ids"))
		if len(ids) == 0 {
			return hubWork{}, "Model ids is required"
		}
		w := hubWork{Op: opAddModels, Name: f.prov, Models: ids}
		pin, pout := f.get("Price in (USD / 1M tokens)"), f.get("Price out (USD / 1M tokens)")
		if (pin == "") != (pout == "") {
			return hubWork{}, "give both prices or neither; one alone would price the other direction at zero"
		}
		if pin != "" {
			in, _ := strconv.ParseFloat(pin, 64)
			out, _ := strconv.ParseFloat(pout, 64)
			w.In, w.Out = &in, &out
		}
		return w, ""
	}
	return hubWork{}, "unknown form"
}

// searchBackend is one service /search can use.
type searchBackend struct{ id, label, hint string }

var searchBackends = []searchBackend{
	{"brave", "Brave Search", "needs an API key · brave.com/search/api"},
	{"exa", "Exa", "needs an API key · exa.ai"},
	{"searxng", "SearXNG", "your own server · needs its address"},
}

func newSearchForm(id string) *hubForm {
	name := id
	for _, b := range searchBackends {
		if b.id == id {
			name = b.label
		}
	}
	f := &hubForm{kind: formSearch, backend: id, title: "Search with " + name}
	if id == "searxng" {
		f.help = "The address of your SearXNG server, for example http://localhost:8080. It must allow the JSON format."
		f.fields = []hubField{{label: "Address", required: true, kind: kindURL}}
		return f
	}
	f.help = "Paste your " + name + " API key. It is stored on this computer (never shown, never in a log) " +
		"and used from your next question."
	f.fields = []hubField{{label: "API key", secret: true, required: true, kind: kindKey}}
	return f
}
