package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

// This file is what makes the thinking level stick. It used to live in one variable:
// leave arxi and come back and it was "off" again, and a model added later started with
// nothing, because nothing remembered a choice and nothing knew a sensible one.
//
// Two things now answer "which level does this model start with":
//
//  1. the level the user last chose for THAT model (effort.json in the settings folder,
//     one entry per model, so a fast model and a deep one each keep their own);
//  2. failing that, a default: the text key effort.default ("medium" unless the user
//     changed it) when the model takes it, otherwise the nearest reasonable level
//     (effortFallbacks). "off" is never a default: it is a choice, and a model that
//     thinks is better left thinking until the user says otherwise.
//
// Choosing "none" for a model is remembered too, so a user who wants no thinking is not
// handed the default again on the next start.

// effortNone is what the file holds for a model whose user chose no level.
const effortNone = "none"

// effortFallbacks is the order tried when the preferred default is not one the model takes.
var effortFallbacks = []string{"medium", "high", "low", "on", "minimal"}

// effortPick is what a user chose for each model ("provider/id" -> level or effortNone).
type effortPick map[string]string

// effortStorePath is the file the picks are kept in, "" when there is nowhere to keep it.
func effortStorePath() string {
	d := configDir()
	if d == "" {
		return ""
	}
	return filepath.Join(d, "effort.json")
}

type effortFile struct {
	Models effortPick `json:"models"`
}

// loadEffortPicks reads the picks. A missing or unreadable file is no picks at all: a
// broken settings file must never stop the program from starting.
func loadEffortPicks(path string) effortPick {
	out := effortPick{}
	if path == "" {
		return out
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return out
	}
	var f effortFile
	if json.Unmarshal(b, &f) == nil {
		for k, v := range f.Models {
			if k != "" && v != "" {
				out[k] = v
			}
		}
	}
	return out
}

// save writes the picks, best effort: failing to remember is not worth an error on screen.
func (p effortPick) save(path string) {
	if path == "" {
		return
	}
	b, err := json.MarshalIndent(effortFile{p}, "", "  ")
	if err != nil || os.MkdirAll(filepath.Dir(path), 0o700) != nil {
		return
	}
	_ = os.WriteFile(path, append(b, '\n'), 0o600)
}

// remember records the level now in force for a model; "" is remembered as effortNone.
func (p effortPick) remember(ref, level string) {
	if ref == "" {
		return
	}
	if level == "" {
		level = effortNone
	}
	p[ref] = level
}

// defaultEffort is the level a model with no pick of its own starts with. prefer is the
// user's wording of it (text key effort.default). "" means start with none: the model
// takes no level, or only "off".
func defaultEffort(levels []string, prefer string) string {
	prefer = strings.ToLower(strings.TrimSpace(prefer))
	if prefer == effortNone {
		return ""
	}
	has := func(l string) bool {
		for _, x := range levels {
			if x == l {
				return true
			}
		}
		return false
	}
	if prefer != "" && prefer != "off" && has(prefer) {
		return prefer
	}
	for _, l := range effortFallbacks {
		if has(l) {
			return l
		}
	}
	// A model whose levels are none of the fallbacks (xhigh, max only): its first that
	// is not "off".
	for _, l := range levels {
		if l != "off" {
			return l
		}
	}
	return ""
}

// effortStart is the level to start a model with: the user's pick for it when it still
// takes that level, the default otherwise. applied is false when the core did not say
// which levels the model takes; nothing is changed then, since a level the model does
// not take is a refused request.
func effortStart(p effortPick, ref string, levels []string, known bool, prefer string) (level string, applied bool) {
	if !known {
		return "", false
	}
	if v, ok := p[ref]; ok {
		if v == effortNone {
			return "", true
		}
		for _, l := range levels {
			if l == v {
				return v, true
			}
		}
	}
	return defaultEffort(levels, prefer), true
}
