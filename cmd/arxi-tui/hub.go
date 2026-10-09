package main

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/michiTrader/arxi_tui/internal/driver"
	"github.com/michiTrader/arxi_tui/internal/fold"
)

// This file is the state machine of the provider hub: the one screen behind
// /provider. /provider manages everything (add a provider, edit its URL
// and key, add or fetch its models, make one the default, remove). Choosing the chat
// model is `/model `, which has its own menu (model_menu.go).
//
// While open it owns the keyboard (after the Ctrl-C branch, invariant 6): Up/Down move
// the highlight instead of walking the chat input, and Enter selects instead of
// sending the text to the model. A key typed into the API-key field must never become
// a prompt. The input line is the FILTER of the current list ("other", "gem"), or the
// focused field on a form.

type hubLevel int

const (
	lvProviders    hubLevel = iota // registered providers + "Add a provider…"
	lvCatalog                      // services to add, filterable, with Other…
	lvActions                      // what to do with one provider
	lvModels                       // one provider's models
	lvModelActions                 // what to do with one model
	lvConfirm                      // are you sure (remove a provider)
	lvForm                         // typed fields
	lvSearch                       // where the model searches the web (/search)
)

const hubPageSize = 8

// hubData is what the core says right now: the providers with their credential
// state, every model, and the default model ("provider/id" or "").
type hubData struct {
	providers []driver.ProviderRow
	models    []driver.ModelRow
	def       string
	// canEditModels is whether the core implements model.update. An older core still
	// gets the rest of the hub; it only lacks the row that edits a model.
	canEditModels bool
}

func (d hubData) provider(name string) (driver.ProviderRow, bool) {
	for _, p := range d.providers {
		if p.Name == name {
			return p, true
		}
	}
	return driver.ProviderRow{}, false
}

// modelRow is the row of a model ref ("provider/id").
func (d hubData) modelRow(ref string) (driver.ModelRow, bool) {
	for _, m := range d.models {
		if modelRef(m) == ref {
			return m, true
		}
	}
	return driver.ModelRow{}, false
}

func (d hubData) modelsOf(name string) []driver.ModelRow {
	var out []driver.ModelRow
	for _, m := range d.models {
		if m.Provider == name {
			out = append(out, m)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// effortsOf is the thinking levels of the model a reference names ("provider/id"),
// and whether the core said (false for an unknown model or an older core).
func (d hubData) effortsOf(ref string) (levels []string, known bool) {
	for _, m := range d.models {
		if modelRef(m) == ref {
			return m.Efforts, m.Efforts != nil
		}
	}
	return nil, false
}

func (d hubData) enabledModels() []driver.ModelRow {
	var out []driver.ModelRow
	for _, m := range d.models {
		if m.Enabled {
			out = append(out, m)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Provider != out[j].Provider {
			return out[i].Provider < out[j].Provider
		}
		return out[i].ID < out[j].ID
	})
	return out
}

// modelRef is "provider/id", the form the core's verbs accept.
func modelRef(m driver.ModelRow) string {
	if m.Provider == "" {
		return m.ID
	}
	return m.Provider + "/" + m.ID
}

// modelName is the display form of a model: "gemini-3.1-flash-lite [google]".
func modelName(m driver.ModelRow) string {
	if m.Provider == "" {
		return m.ID
	}
	return m.ID + " [" + m.Provider + "]"
}

// hubItem is one row of the current picker.
type hubItem struct {
	id, label, status string
	// sticky rows survive the filter: the way forward ("Add a provider", "Other…")
	// must never be filtered out of existence.
	sticky bool
}

// providerHub is the loop-visible state of the open screen. nil means closed.
type providerHub struct {
	level  hubLevel
	data   hubData
	filter string
	sel    int
	prov   string // the provider being looked at
	model  string // the model ref being looked at

	form     *hubForm
	formFrom hubLevel

	working string // what is in flight ("" = idle)
	offline string // why there is no core to talk to ("" = live)
}

// hubOpen says which level a freshly opened hub starts on.
type hubOpen int

const (
	hubOpenProviders hubOpen = iota
	hubOpenSearch            // /search: the web search backends
)

// newHub builds the hub for an entry point. Choosing the chat model is not part of the
// hub: `/model ` has its own minimal menu (model_menu.go).
func newHub(data hubData, open hubOpen) (*providerHub, string) {
	h := &providerHub{data: data}
	if open == hubOpenSearch {
		h.level = lvSearch
	}
	return h, ""
}

// newOfflineHub is the hub when there is no core to talk to: it opens, explains why,
// and offers nothing that cannot work.
func newOfflineHub(reason string) *providerHub {
	return &providerHub{offline: reason}
}

func (h *providerHub) setLevel(l hubLevel) {
	h.level, h.filter, h.sel = l, "", 0
}

// setData replaces the core's state and re-clamps the highlight: a list that shrank
// under the highlight would leave it past the end.
func (h *providerHub) setData(d hubData) {
	h.data = d
	if n := len(h.items()); h.sel >= n {
		h.sel = n - 1
	}
	if h.sel < 0 {
		h.sel = 0
	}
}

// keyStatus is the credential column of a provider.
func keyStatus(r driver.ProviderRow) string {
	switch r.Key {
	case "env":
		return "✓ env: " + r.APIKeyEnv
	case "stored":
		return "✓ key stored"
	case "none":
		return "✓ no key needed"
	case "missing":
		if r.APIKeyEnv != "" {
			return "• key missing (env: " + r.APIKeyEnv + ")"
		}
		return "• key missing"
	case "unreadable":
		return "! key folder unreadable"
	}
	return "• configured"
}

func plural(n int, one, many string) string {
	if n == 1 {
		return fmt.Sprintf("1 %s", one)
	}
	return fmt.Sprintf("%d %s", n, many)
}

// allItems is the full (unfiltered) list of the current level.
func (h *providerHub) allItems() []hubItem {
	d := h.data
	var out []hubItem
	switch h.level {
	case lvProviders:
		for _, p := range d.providers {
			st := keyStatus(p)
			if p.Models > 0 {
				st += " · " + plural(p.Models, "model", "models")
			} else {
				st += " · no models yet"
			}
			out = append(out, hubItem{id: p.Name, label: p.Name, status: st})
		}
		out = append(out, hubItem{id: "+add", label: "+ Add a provider…", sticky: true})

	case lvCatalog:
		for _, e := range providerCatalog {
			st := e.BaseURL
			if e.CoreKnown {
				st = "built in"
			}
			if _, have := d.provider(e.ID); have {
				st = "✓ already added"
			}
			out = append(out, hubItem{id: e.ID, label: e.Display, status: st})
		}
		label := "Other…"
		if name := h.otherName(); name != "" {
			label = fmt.Sprintf("Other: %q…", name)
		}
		out = append(out, hubItem{id: "other", label: label, status: "any OpenAI-compatible service", sticky: true})

	case lvActions:
		r, _ := d.provider(h.prov)
		out = append(out,
			hubItem{id: "discover", label: "Fetch models from the service", status: "asks " + r.BaseURL},
			hubItem{id: "addmodels", label: "Add models by hand…"},
			hubItem{id: "models", label: "See its models", status: plural(len(d.modelsOf(h.prov)), "model", "models")},
			hubItem{id: "edit", label: "Edit URL / key…"},
			hubItem{id: "remove", label: "Remove this provider"},
		)

	case lvModels:
		for _, m := range d.modelsOf(h.prov) {
			st := "enabled"
			if !m.Enabled {
				st = "disabled"
			}
			label := modelName(m)
			if modelRef(m) == d.def {
				label = "✓ " + label + " · default"
			}
			out = append(out, hubItem{id: modelRef(m), label: label, status: st})
		}

	case lvModelActions:
		on := h.modelEnabled(h.model)
		toggle := "Disable"
		if !on {
			toggle = "Enable"
		}
		out = append(out,
			hubItem{id: "default", label: "Use for chat (make default)"},
			hubItem{id: "toggle", label: toggle + " this model"},
		)
		if d.canEditModels {
			out = append(out, hubItem{id: "edit", label: "Edit name / price…"})
		}
		out = append(out, hubItem{id: "remove", label: "Remove this model"})

	case lvSearch:
		cur := loadSearchConfig(searchConfigPath()).Backend
		for _, b := range searchBackends {
			st := b.hint
			if b.id == cur {
				st = "✓ in use · " + b.hint
			}
			out = append(out, hubItem{id: b.id, label: b.label, status: st})
		}
		st := "web search is not offered to the model"
		if cur == "" {
			st = "✓ in use · " + st
		}
		out = append(out, hubItem{id: "off", label: "Turn web search off", status: st})

	case lvConfirm:
		out = append(out,
			hubItem{id: "yes", label: "Yes, remove " + h.prov},
			hubItem{id: "no", label: "No, keep it"},
		)
	}
	return out
}

func (h *providerHub) modelEnabled(ref string) bool {
	for _, m := range h.data.models {
		if modelRef(m) == ref {
			return m.Enabled
		}
	}
	return false
}

// isOtherWord reports whether the user typed the word for the Other… row itself
// ("other", "others", "otro", "otros"), which selects it rather than naming a service.
func isOtherWord(q string) bool {
	switch strings.ToLower(strings.TrimSpace(q)) {
	case "other", "others", "otro", "otros", "otra", "otras":
		return true
	}
	return false
}

// otherName is the provider name the Other… row offers to create: what the user
// typed, when it matches no listed service and is not the word "other" itself.
func (h *providerHub) otherName() string {
	q := strings.TrimSpace(h.filter)
	if q == "" || isOtherWord(q) || h.anyCatalogMatch(q) {
		return ""
	}
	return sanitizeProviderName(q)
}

// anyCatalogMatch reports whether the filter text matches a catalog service, so the
// Other… row can offer to create the thing the user typed when nothing else matches.
func (h *providerHub) anyCatalogMatch(q string) bool {
	q = strings.ToLower(q)
	for _, e := range providerCatalog {
		if strings.Contains(strings.ToLower(e.ID), q) || strings.Contains(strings.ToLower(e.Display), q) {
			return true
		}
	}
	return false
}

// items is the filtered list. The filter is a case-insensitive substring over the
// label, the id and the status; sticky rows always stay.
func (h *providerHub) items() []hubItem {
	all := h.allItems()
	q := strings.ToLower(strings.TrimSpace(h.filter))
	if q == "" || h.level == lvForm {
		return all
	}
	var out []hubItem
	for _, it := range all {
		if it.sticky || strings.Contains(strings.ToLower(it.label+" "+it.id+" "+it.status), q) {
			out = append(out, it)
		}
	}
	// "other" is an alias for the Other… row even when its label was rewritten.
	return out
}

func (h *providerHub) count() int {
	if h.level == lvForm {
		return len(h.form.fields)
	}
	return len(h.items())
}

// move steps the highlight (or the focused field), wrapping at both ends.
func (h *providerHub) move(delta int) {
	n := h.count()
	if n == 0 {
		return
	}
	if h.level == lvForm {
		h.form.focus = ((h.form.focus+delta)%n + n) % n
		return
	}
	h.sel = ((h.sel+delta)%n + n) % n
}

func (h *providerHub) title() string {
	if h.offline != "" {
		return "Providers"
	}
	switch h.level {
	case lvProviders:
		return "Providers — choose one to manage, or add a new one:"
	case lvCatalog:
		return "Add a provider — pick a service (type to filter, or choose Other…):"
	case lvActions:
		return h.prov + " — what do you want to do?"
	case lvModels:
		return "Models of " + h.prov + ":"
	case lvModelActions:
		return h.model + ":"
	case lvConfirm:
		return "Remove " + h.prov + "?"
	case lvForm:
		return h.form.title + ":"
	case lvSearch:
		return "Web search — pick where the model searches:"
	}
	return ""
}

func (h *providerHub) hint() string {
	switch h.level {
	case lvForm:
		return "tab next field · enter next/save · esc back · the key is never shown"
	case lvConfirm:
		return "↑↓ move · enter choose · esc back"
	case lvSearch:
		return "↑↓ move · enter select · esc close"
	}
	return "type to filter · ↑↓ move · enter select · esc back"
}

func (h *providerHub) detail() string {
	var b strings.Builder
	if h.working != "" {
		b.WriteString("⏳ " + h.working + "\n\n")
	}
	if h.offline != "" {
		b.WriteString(h.offline + "\n\nPress esc to go back to the chat.")
		return b.String()
	}
	d := h.data
	switch h.level {
	case lvProviders:
		if len(d.providers) == 0 {
			b.WriteString("No providers yet.\n\n" +
				"Choose “+ Add a provider…” to connect OpenAI, Anthropic, OpenRouter, Gemini and more,\n" +
				"or your own service. Paste the key once; the models are fetched for you.\n" +
				"Then pick the model to chat with using /model.")
			break
		}
		fmt.Fprintf(&b, "%s configured.\n", plural(len(d.providers), "provider", "providers"))
		if d.def != "" {
			fmt.Fprintf(&b, "Chat model: %s\n", d.def)
		} else {
			b.WriteString("No chat model chosen yet: use /model to pick one.\n")
		}
		b.WriteString("\nEnter on a provider to fetch or add models, edit its URL and key, or remove it.")
	case lvCatalog:
		b.WriteString("Pick a service, or type to filter the list. “Other…” adds any service\n" +
			"that speaks the OpenAI chat API: you give its name, base URL and key.")
	case lvActions:
		h.writeProviderSummary(&b)
	case lvModels:
		if len(d.modelsOf(h.prov)) == 0 {
			b.WriteString("This provider has no models yet.\nGo back and choose “Fetch models from the service” or “Add models by hand…”.")
		} else {
			b.WriteString("Enter on a model to make it the default, enable or disable it, edit its name or price, or remove it.")
		}
	case lvModelActions:
		fmt.Fprintf(&b, "Model: %s\n", h.model)
		if m, ok := d.modelRow(h.model); ok && m.Price != nil {
			fmt.Fprintf(&b, "Price you declared: %s in, %s out (USD per million tokens)\n", priceText(m.Price.In), priceText(m.Price.Out))
		}
		switch {
		case h.model == d.def:
			b.WriteString("It is the model the chat uses now.")
		case h.modelEnabled(h.model):
			b.WriteString("Enabled.")
		default:
			b.WriteString("Disabled: enable it before using it.")
		}
	case lvConfirm:
		r, _ := d.provider(h.prov)
		fmt.Fprintf(&b, "This deletes %s, its %s and its stored API key.", h.prov, plural(r.Models, "model", "models"))
		if strings.HasPrefix(d.def, h.prov+"/") {
			b.WriteString("\nIt holds the chat model, so you will have to choose another with /model.")
		}
	case lvForm:
		b.WriteString(h.form.help)
	case lvSearch:
		h.writeSearchSummary(&b)
	}
	return b.String()
}

func (h *providerHub) writeProviderSummary(b *strings.Builder) {
	r, ok := h.data.provider(h.prov)
	if !ok {
		fmt.Fprintf(b, "%s is not registered.", h.prov)
		return
	}
	fmt.Fprintf(b, "URL:     %s\n", r.BaseURL)
	fmt.Fprintf(b, "Key:     %s\n", keyStatus(r))
	fmt.Fprintf(b, "Models:  %s", plural(r.Models, "model", "models"))
	if strings.HasPrefix(h.data.def, h.prov+"/") {
		fmt.Fprintf(b, " · chat model: %s", strings.TrimPrefix(h.data.def, h.prov+"/"))
	}
}

// publish writes the hub's view onto the folded state the renderer reads. It is the
// only thing that does, and it writes only masked text.
func (h *providerHub) publish(st *fold.State) {
	var rows []fold.HubRow
	if h.level == lvForm {
		lo, hi := window(h.form.focus, len(h.form.fields), hubPageSize)
		for i := lo; i < hi; i++ {
			f := h.form.fields[i]
			rows = append(rows, fold.HubRow{Label: f.label, Status: f.shown(), Selected: i == h.form.focus})
		}
		st.UserInput = h.form.fields[h.form.focus].masked()
	} else {
		items := h.items()
		lo, hi := window(h.sel, len(items), hubPageSize)
		for i := lo; i < hi; i++ {
			rows = append(rows, fold.HubRow{Label: items[i].label, Status: items[i].status, Selected: i == h.sel})
		}
		st.UserInput = h.filter
	}
	st.UserInputCaret = len([]rune(st.UserInput))
	st.HubTitle, st.HubRows, st.HubHint, st.HubDetail = h.title(), rows, h.hint(), h.detail()
}

// window is the [lo,hi) slice of a list of n rows, size rows tall, that keeps the
// highlight visible and does not move until the highlight reaches an edge.
func window(sel, n, size int) (lo, hi int) {
	if n <= size {
		return 0, n
	}
	lo = sel - size/2
	if lo < 0 {
		lo = 0
	}
	if lo+size > n {
		lo = n - size
	}
	return lo, lo + size
}

// wipe drops any open form and every secret typed into it.
func (h *providerHub) wipe() {
	if h.form != nil {
		h.form.wipe()
		h.form = nil
	}
}

// writeSearchSummary explains the /search screen: what the choice does, what it costs and
// whether something else is already deciding it.
func (h *providerHub) writeSearchSummary(b *strings.Builder) {
	if searchFromShell() {
		b.WriteString("The environment variable " + envSearchBackend + " is set, so it decides the search\n" +
			"and what you choose here is kept but not used until that variable is removed.\n\n")
	}
	b.WriteString("The model can search the web only after you pick a service here. Each search asks\n" +
		"your permission unless the mode is full access. Brave and Exa need an API key from\n" +
		"their site; SearXNG is a search server you run yourself and needs only its address.\n" +
		"The key is stored on this computer, never shown, and applies from your next question.")
}

// priceText writes a price the way a person types it: no trailing zeros.
func priceText(v float64) string { return strconv.FormatFloat(v, 'f', -1, 64) }
