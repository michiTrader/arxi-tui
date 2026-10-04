package main

import (
	"fmt"
	"sort"
	"strings"

	"github.com/michiTrader/arxi_tui/internal/driver"
	"github.com/michiTrader/arxi_tui/internal/fold"
)

// This file is the state machine of the provider hub: the one screen behind
// /provider and /models. /provider manages everything (add a provider, edit its URL
// and key, add or fetch its models, make one the default, remove); /models is the same
// screen opened straight on the model picker.
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
	lvPick                         // /models: choose the default model
)

const hubPageSize = 8

// hubData is what the core says right now: the providers with their credential
// state, every model, and the default model ("provider/id" or "").
type hubData struct {
	providers []driver.ProviderRow
	models    []driver.ModelRow
	def       string
}

func (d hubData) provider(name string) (driver.ProviderRow, bool) {
	for _, p := range d.providers {
		if p.Name == name {
			return p, true
		}
	}
	return driver.ProviderRow{}, false
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
	hubOpenModels
)

// newHub builds the hub for an entry point. /models with nothing to pick lands on the
// provider list instead, with the reason, rather than on an empty picker.
func newHub(data hubData, open hubOpen) (*providerHub, string) {
	h := &providerHub{data: data}
	if open == hubOpenModels {
		if len(data.enabledModels()) == 0 {
			return h, "no models yet: add a provider here and its models are fetched for you"
		}
		h.setLevel(lvPick)
		h.selectDefault()
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

// selectDefault puts the highlight on the default model of the picker.
func (h *providerHub) selectDefault() {
	for i, it := range h.items() {
		if it.id == h.data.def {
			h.sel = i
			return
		}
	}
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
		if q := strings.TrimSpace(h.filter); q != "" && !h.anyCatalogMatch(q) {
			label = fmt.Sprintf("Other: %q…", sanitizeProviderName(q))
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
			hubItem{id: "remove", label: "Remove this model"},
		)

	case lvConfirm:
		out = append(out,
			hubItem{id: "yes", label: "Yes, remove " + h.prov},
			hubItem{id: "no", label: "No, keep it"},
		)

	case lvPick:
		for _, m := range d.enabledModels() {
			label := modelName(m)
			if modelRef(m) == d.def {
				label = "✓ " + label + " · default"
			}
			out = append(out, hubItem{id: modelRef(m), label: label})
		}
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
	case lvPick:
		return "Choose the model to chat with (type to filter):"
	}
	return ""
}

func (h *providerHub) hint() string {
	switch h.level {
	case lvForm:
		return "tab next field · enter next/save · esc back · the key is never shown"
	case lvConfirm:
		return "↑↓ move · enter choose · esc back"
	}
	return "type to filter · ↑↓ move · enter select · esc back"
}
