package main

import (
	"context"
	"strings"

	"github.com/michiTrader/arxi_tui/internal/fold"
	"github.com/michiTrader/arxi_tui/internal/term"
)

// modelPrefix is what the input must start with for the model menu to open: the
// command and one space. `/model` alone is still the command menu's row; the space
// is the user saying "now I am choosing".
const modelPrefix = "/model "

// modelMenuOpen reports whether the buffer opens the model menu, and the text typed
// after the prefix (the filter).
func modelMenuOpen(input string) (filter string, open bool) {
	return menuOpen(modelPrefix, input)
}

// menuOpen reports whether the buffer opens the menu of the command `prefix` (the
// command and one space), and the text typed after it.
func menuOpen(prefix, input string) (filter string, open bool) {
	if !strings.HasPrefix(input, prefix) {
		return "", false
	}
	return strings.TrimLeft(input[len(prefix):], " "), true
}

// modelMenu is the host-owned state of the `/model ` menu between frames: the models
// read from the core, which one is the chat model, and the highlight.
type modelMenu struct {
	models  []fold.ModelMatch // every enabled model, sorted
	loaded  bool              // the core has answered at least once
	loading bool              // a read is in flight
	err     string            // why the last read failed, shown in the menu
	sel     int
}

// filterModels keeps the models whose reference contains every word typed, case
// insensitively, so `deeps flash` finds `deepseek-v4.1-flash`.
func filterModels(all []fold.ModelMatch, filter string) []fold.ModelMatch {
	words := strings.Fields(strings.ToLower(filter))
	if len(words) == 0 {
		return all
	}
	var out []fold.ModelMatch
	for _, m := range all {
		hay := strings.ToLower(m.Ref)
		ok := true
		for _, w := range words {
			if !strings.Contains(hay, w) {
				ok = false
				break
			}
		}
		if ok {
			out = append(out, m)
		}
	}
	return out
}

// modelMatchesFrom turns the hub data into menu rows, marking the chat model.
func modelMatchesFrom(d hubData) []fold.ModelMatch {
	var out []fold.ModelMatch
	for _, m := range d.enabledModels() {
		out = append(out, fold.ModelMatch{
			Ref: modelRef(m), Name: m.ID, Provider: m.Provider, Current: modelRef(m) == d.def,
		})
	}
	return out
}

// setRows replaces the rows with a fixed list and moves the highlight onto the one
// marked in use. The effort menu uses it; it has nothing to read from the core.
func (mm *modelMenu) setRows(rows []fold.ModelMatch) {
	mm.models, mm.loaded, mm.loading, mm.err = rows, true, false, ""
	mm.sel = 0
	for i, m := range rows {
		if m.Current {
			mm.sel = i
			break
		}
	}
}

// setData replaces the model list and moves the highlight onto the model in use.
func (mm *modelMenu) setData(d hubData) {
	mm.models, mm.loaded, mm.loading, mm.err = modelMatchesFrom(d), true, false, ""
	mm.sel = 0
	for i, m := range mm.models {
		if m.Current {
			mm.sel = i
			break
		}
	}
}

// view returns what the renderer needs for this frame: the filtered rows and the
// highlight clamped to them.
func (mm *modelMenu) view(filter string) (rows []fold.ModelMatch, sel int) {
	rows = filterModels(mm.models, filter)
	sel = mm.sel
	if sel >= len(rows) {
		sel = len(rows) - 1
	}
	if sel < 0 {
		sel = 0
	}
	return rows, sel
}

// modelMenuKey applies one key while the menu is open. It returns the new buffer and
// caret, and the ref to switch to when Enter picked a model ("" otherwise). Up and
// down wrap; Esc clears the line; everything else edits the line like the normal
// input, and any change to the filter puts the highlight back on the first row.
func modelMenuKey(mm *modelMenu, input string, caret int, k term.Key) (next string, nextCaret int, pick string) {
	return choiceMenuKey(mm, modelPrefix, input, caret, k)
}

// choiceMenuKey is modelMenuKey for any menu opened by `<command> `: the model menu
// and the effort menu share the keys, the filter and the wrap-around.
func choiceMenuKey(mm *modelMenu, prefix, input string, caret int, k term.Key) (next string, nextCaret int, pick string) {
	filter, _ := menuOpen(prefix, input)
	rows := filterModels(mm.models, filter)
	switch k.Type {
	case term.KeyUp:
		if len(rows) > 0 {
			mm.sel = (mm.sel - 1 + len(rows)) % len(rows)
		}
		return input, caret, ""
	case term.KeyDown:
		if len(rows) > 0 {
			mm.sel = (mm.sel + 1) % len(rows)
		}
		return input, caret, ""
	case term.KeyTab:
		return input, caret, ""
	case term.KeyEscape:
		return "", 0, ""
	case term.KeyEnter:
		if len(rows) == 0 {
			return input, caret, ""
		}
		sel := mm.sel
		if sel >= len(rows) {
			sel = len(rows) - 1
		}
		return "", 0, rows[sel].Ref
	}
	edited, c, ok := applyEdit(input, caret, k)
	if !ok {
		return input, caret, ""
	}
	if edited != input {
		mm.sel = 0
	}
	// Deleting back through the space closes the menu (the buffer no longer starts
	// with the prefix); the caret must not be left past the end.
	return edited, c, ""
}

// startModelRead reads the model list and the default on a worker, so a slow core
// never freezes the loop. The answer comes back on done.
func startModelRead(ctx context.Context, core hubCore, done chan<- modelRead) {
	go func() {
		d, err := readHubData(ctx, core)
		if err != nil {
			done <- modelRead{err: err.Error()}
			return
		}
		done <- modelRead{data: d}
	}()
}

// startModelPick sets the chat model on a worker and reports the refreshed list.
func startModelPick(ctx context.Context, core hubCore, ref string, done chan<- modelRead) {
	go func() {
		out := runHubWork(ctx, core, hubWork{Op: opDefault, Ref: ref})
		if !out.ok {
			done <- modelRead{err: out.notice, picked: ref}
			return
		}
		done <- modelRead{data: out.data, hasData: out.hasData, notice: out.notice, picked: ref}
	}()
}

// modelRead is a worker's answer.
type modelRead struct {
	data    hubData
	hasData bool
	notice  string
	err     string
	picked  string
}
