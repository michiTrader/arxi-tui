package main

import (
	"strings"

	"github.com/michiTrader/arxi_tui/internal/term"
)

// hubKeyResult is what one keypress asks the loop to do.
type hubKeyResult struct {
	close  bool     // leave the hub
	notice string   // a message for the banner ("" = leave it)
	clear  bool     // clear the banner
	work   *hubWork // run on the worker
}

// routeHubKey applies one key to the hub. Every key, printable or not, ends here and
// never reaches the chat input: a key typed into the API-key field must not become a
// prompt.
func routeHubKey(h *providerHub, k term.Key) hubKeyResult {
	switch k.Type {
	case term.KeyEscape:
		return h.back()
	case term.KeyUp:
		h.move(-1)
	case term.KeyDown:
		h.move(1)
	case term.KeyPgUp:
		h.move(-hubPageSize)
	case term.KeyPgDn:
		h.move(hubPageSize)
	case term.KeyTab:
		if h.level == lvForm {
			if k.Mod&term.ModShift != 0 {
				h.move(-1)
			} else {
				h.move(1)
			}
		} else {
			h.move(1)
		}
	case term.KeyEnter:
		return h.enter()
	case term.KeyBackspace:
		h.edit(dropLastRune)
	case term.KeyRunes:
		if k.Mod&term.ModCtrl != 0 {
			if len(k.Runes) == 1 && k.Runes[0] == 'u' {
				h.edit(func(string) string { return "" })
			}
			return hubKeyResult{}
		}
		if k.Mod&term.ModAlt != 0 {
			return hubKeyResult{}
		}
		text := string(k.Runes)
		h.edit(func(s string) string { return s + h.clean(text) })
	}
	return hubKeyResult{}
}

// clean strips what the current input cannot hold: a form field takes no whitespace,
// the filter line takes spaces.
func (h *providerHub) clean(s string) string {
	if h.level == lvForm {
		return cleanFieldText(s)
	}
	return cleanFilterText(s)
}

// edit changes the text being typed: the focused field on a form, the filter elsewhere.
func (h *providerHub) edit(fn func(string) string) {
	if h.offline != "" {
		return
	}
	if h.level == lvForm {
		f := &h.form.fields[h.form.focus]
		f.value = fn(f.value)
		return
	}
	before := h.filter
	h.filter = fn(h.filter)
	if h.filter != before {
		h.sel = 0
		h.landOnBestMatch()
	}
}

// landOnBestMatch puts the highlight where the user most likely wants it after the
// filter changed: the first real match, or the way forward when nothing matches or the
// user typed the word "other".
func (h *providerHub) landOnBestMatch() {
	items := h.items()
	if h.level == lvCatalog && isOtherWord(h.filter) {
		for i, it := range items {
			if it.id == "other" {
				h.sel = i
			}
		}
		return
	}
	for i, it := range items {
		if !it.sticky {
			h.sel = i
			return
		}
	}
	if n := len(items); n > 0 {
		h.sel = n - 1
	}
}

// paste inserts clipboard text into the focused field or the filter. It never reaches
// the chat input: the hub owns the keyboard while it is open.
func (h *providerHub) paste(text string) {
	clean := h.clean(text)
	h.edit(func(s string) string { return s + clean })
}

// back is Esc: first the filter, then one level up, then out.
func (h *providerHub) back() hubKeyResult {
	if h.level != lvForm && h.filter != "" {
		h.filter, h.sel = "", 0
		return hubKeyResult{clear: true}
	}
	switch h.level {
	case lvForm:
		h.wipe()
		h.setLevel(h.formFrom)
	case lvCatalog, lvActions:
		h.setLevel(lvProviders)
	case lvModels, lvConfirm:
		h.setLevel(lvActions)
	case lvModelActions:
		h.setLevel(lvModels)
	default:
		return hubKeyResult{close: true, clear: true}
	}
	return hubKeyResult{clear: true}
}

func (h *providerHub) openForm(f *hubForm) {
	h.formFrom = h.level
	h.form = f
	h.level, h.filter, h.sel = lvForm, "", 0
}

// enter is Enter: select the highlighted row, or advance/submit the form.
func (h *providerHub) enter() hubKeyResult {
	if h.offline != "" {
		return hubKeyResult{notice: h.offline}
	}
	if h.working != "" {
		return hubKeyResult{notice: "still working: " + h.working}
	}
	if h.level == lvForm {
		if h.form.focus < len(h.form.fields)-1 {
			h.form.focus++
			return hubKeyResult{}
		}
		w, msg := h.form.submit()
		if msg != "" {
			return hubKeyResult{notice: msg}
		}
		return hubKeyResult{work: &w}
	}

	items := h.items()
	if len(items) == 0 {
		if h.level == lvModels {
			return hubKeyResult{notice: "this provider has no models yet: go back and fetch or add some"}
		}
		return hubKeyResult{notice: "nothing matches " + `"` + h.filter + `"`}
	}
	if h.sel >= len(items) {
		h.sel = len(items) - 1
	}
	it := items[h.sel]
	d := h.data

	switch h.level {
	case lvProviders:
		if it.id == "+add" {
			h.setLevel(lvCatalog)
		} else {
			h.prov = it.id
			h.setLevel(lvActions)
		}

	case lvCatalog:
		if it.id == "other" {
			h.openForm(newOtherForm(h.otherName()))
			break
		}
		e, _ := catalogByID(it.id)
		if _, have := d.provider(e.ID); have {
			h.prov = e.ID
			h.setLevel(lvActions)
			return hubKeyResult{notice: e.Display + " is already added; its options are here"}
		}
		if e.NoKey {
			return hubKeyResult{work: &hubWork{Op: opAdd, Name: e.ID, BaseURL: e.BaseURL}}
		}
		h.openForm(newAddForm(e))

	case lvActions:
		switch it.id {
		case "discover":
			return hubKeyResult{work: &hubWork{Op: opDiscover, Name: h.prov}}
		case "addmodels":
			h.openForm(newModelsForm(h.prov))
		case "models":
			h.setLevel(lvModels)
		case "edit":
			r, _ := d.provider(h.prov)
			h.openForm(newEditForm(h.prov, r.BaseURL, r.APIKeyEnv))
		case "remove":
			h.setLevel(lvConfirm)
			h.sel = 1 // the safe answer is under the highlight
		}

	case lvModels:
		h.model = it.id
		h.setLevel(lvModelActions)

	case lvModelActions:
		switch it.id {
		case "default":
			return hubKeyResult{work: &hubWork{Op: opDefault, Ref: h.model}}
		case "toggle":
			return hubKeyResult{work: &hubWork{Op: opToggle, Ref: h.model, On: !h.modelEnabled(h.model)}}
		case "edit":
			m, _ := d.modelRow(h.model)
			h.openForm(newEditModelForm(m))
		case "remove":
			return hubKeyResult{work: &hubWork{Op: opRemoveModel, Ref: h.model}}
		}

	case lvSearch:
		if it.id == "off" {
			return hubKeyResult{work: &hubWork{Op: opSearch, Close: true}}
		}
		h.openForm(newSearchForm(it.id))

	case lvConfirm:
		if it.id == "yes" {
			return hubKeyResult{work: &hubWork{Op: opRemove, Name: h.prov}}
		}
		return h.back()
	}
	return hubKeyResult{clear: true}
}

// sanitizeProviderName turns what the user typed into the filter into a usable
// provider name (lower case, letters, digits, dash, underscore).
func sanitizeProviderName(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(strings.TrimSpace(s)) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '-', r == '_':
			b.WriteRune(r)
		case r == ' ' || r == '.':
			b.WriteRune('-')
		}
	}
	return b.String()
}
