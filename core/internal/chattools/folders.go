package chattools

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Folders other than the project's. The file tools are confined on purpose (what the
// model reads goes to a remote provider), but the work does not always sit in one
// folder: a script here may import a library kept next to it. The user opens another
// folder for the session, and the tools then treat it like the project's own. Every
// other guard (symlinks, .git, files that look like secrets) applies in it unchanged.

// WithDir returns a toolbox that may also use dir. A relative path still starts at the
// root; a path in dir is given in full. It refuses what is too wide to be a decision
// about one piece of work: a drive or filesystem root, and the user's home folder
// itself.
func (t *Toolbox) WithDir(dir string) (*Toolbox, error) {
	real, err := cleanFolder(dir)
	if err != nil {
		return nil, err
	}
	if tooWide(real) {
		return nil, fmt.Errorf("%s is too wide to open as a whole; name the folder the work is in", real)
	}
	for _, r := range t.Roots() {
		if _, ok := relUnder(r, real); ok {
			return t, nil // already reachable
		}
	}
	c := *t
	c.extra = append(append([]string(nil), t.extra...), real)
	return &c, nil
}

// cleanFolder resolves dir to an existing directory, symlinks followed.
func cleanFolder(dir string) (string, error) {
	if strings.TrimSpace(dir) == "" {
		return "", errors.New("no folder given")
	}
	abs, err := filepath.Abs(strings.TrimSpace(dir))
	if err != nil {
		return "", err
	}
	real, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return "", fmt.Errorf("%s cannot be opened: %v", abs, err)
	}
	st, err := os.Stat(real)
	if err != nil {
		return "", err
	}
	if !st.IsDir() {
		return "", fmt.Errorf("%s is not a folder", real)
	}
	return real, nil
}

// tooWide is a folder that says nothing about the work: a root, or home.
func tooWide(dir string) bool {
	dir = filepath.Clean(dir)
	if filepath.Dir(dir) == dir {
		return true
	}
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		if h, err := filepath.EvalSymlinks(home); err == nil {
			home = h
		}
		if strings.EqualFold(filepath.Clean(home), dir) {
			return true
		}
	}
	return false
}

// Outside reports whether the path a call names (as the model wrote it) lies beyond the
// folders this toolbox may use. It looks at the words only; a symlink that leads out is
// still refused later, when the path is resolved.
func (t *Toolbox) Outside(p string) bool {
	p = strings.TrimSpace(p)
	if p == "" || p == "." {
		return false
	}
	full := filepath.Join(t.root, p)
	if filepath.IsAbs(p) {
		full = filepath.Clean(p)
	}
	return outside(t, full)
}

// markers say that a folder is the top of a piece of work.
var markers = []string{".git", "go.mod", "pyproject.toml", "package.json", "Cargo.toml"}

// SuggestFolder is the folder worth opening for a path outside the toolbox: the nearest
// repository or project folder above the path, so one question covers its sibling
// folders; otherwise the folder holding the path. It is not the toolbox's job to decide
// whether it is allowed, only which folder the question is about. An error means no
// reasonable folder exists (the path leads nowhere that exists, or only to a root or
// home).
func (t *Toolbox) SuggestFolder(p string) (string, error) {
	p = strings.TrimSpace(p)
	full := filepath.Join(t.root, p)
	if filepath.IsAbs(p) {
		full = filepath.Clean(p)
	}
	// The nearest folder that exists: the path itself when it is one, else upward.
	dir := full
	for {
		st, err := os.Stat(dir)
		if err == nil && st.IsDir() {
			break
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("%s does not exist", display(p))
		}
		dir = parent
	}
	real, err := cleanFolder(dir)
	if err != nil {
		return "", err
	}
	// Walk up to the nearest folder that marks a project, never into a root or home.
	// The nearest, not the highest: a drive that happens to hold a repository at its
	// top is not what the user meant by "that script".
	top := real
	for cur := real; !tooWide(cur); {
		found := false
		for _, m := range markers {
			if _, err := os.Stat(filepath.Join(cur, m)); err == nil {
				found = true
				break
			}
		}
		if found {
			top = cur
			break
		}
		parent := filepath.Dir(cur)
		if parent == cur {
			break
		}
		cur = parent
	}
	if tooWide(top) {
		return "", fmt.Errorf("%s is too wide to open as a whole; name the folder the work is in", top)
	}
	return top, nil
}

// FileTool reports whether a tool reads or changes files through a path argument, the
// ones the confinement applies to.
func FileTool(name string) bool {
	switch name {
	case ToolList, ToolRead, ToolGrep:
		return true
	}
	return Mutating(name)
}

// PathArg is the path a file tool's call names ("" when it names none).
func PathArg(raw []byte) string { return str(mustArgs(raw), "path") }
