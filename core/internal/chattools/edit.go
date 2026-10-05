package chattools

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// This file is the half of the toolbox that changes files: write (create or replace a
// whole file) and edit (replace a piece of text in one). They live apart from the
// read tools because a caller decides separately whether to offer them, and whether
// to ask the user first. For that second decision every change can be previewed:
// Preview works out exactly what would happen, and a diff to show for it, without
// touching the disk. Run then does it.
//
// The guard is the read tools' guard, and then some: a path must land inside the
// project (a file that does not exist yet is judged by the folder it would go in),
// the repository's control directory is never written, and neither is a file that
// looks like it holds credentials. A file is replaced by writing a sibling and
// renaming it over the old one, so a failure half way leaves the old file whole.

// Names of the tools that change files.
const (
	ToolWrite = "write"
	ToolEdit  = "edit"
)

// Limits for what the model may hand over.
const (
	maxEditFileBytes = 1 << 20 // the largest file that may be edited or replaced
	maxDiffLines     = 200     // lines of diff shown before it says how many it left out
	diffContext      = 3       // unchanged lines kept around each change
	diffMaxCells     = 2_500_000
)

// Mutating reports whether a tool changes files.
func Mutating(name string) bool { return name == ToolWrite || name == ToolEdit }

// EditDefinitions lists the tools that change files, in the order they are offered.
func EditDefinitions() []Definition {
	return []Definition{
		{ToolEdit, "Replace text in a file of the project. old_string must appear exactly once in the file " +
			"(include enough surrounding lines to make it unique) unless replace_all is true. Read the file first.",
			[]byte(`{"type":"object","properties":{"path":{"type":"string","description":"File, relative to the project root."},"old_string":{"type":"string","description":"The exact text to replace."},"new_string":{"type":"string","description":"The text to put in its place."},"replace_all":{"type":"boolean","description":"Replace every occurrence instead of requiring one."}},"required":["path","old_string","new_string"]}`)},
		{ToolWrite, "Create a file, or replace a file completely, with the given content. To change part of an existing file use edit instead.",
			[]byte(`{"type":"object","properties":{"path":{"type":"string","description":"File, relative to the project root."},"content":{"type":"string","description":"The whole new content of the file."}},"required":["path","content"]}`)},
	}
}

// change is one planned modification.
type change struct {
	full string // where it will be written (symlinks resolved)
	data []byte
	mode fs.FileMode
	res  Result
}

// Preview works out what a write or edit would do, with the diff to show for it,
// without changing anything. The error is the same one Run would give.
func (t *Toolbox) Preview(name string, rawArgs []byte) (Result, error) {
	c, res, err := t.plan(name, rawArgs)
	if err != nil {
		return res, err
	}
	return c.res, nil
}

// apply performs a planned change.
func (c *change) apply() error {
	dir := filepath.Dir(c.full)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("%s cannot be created: %v", c.res.Arg, err)
	}
	tmp, err := os.CreateTemp(dir, ".arxi-write-*")
	if err != nil {
		return fmt.Errorf("%s cannot be written: %v", c.res.Arg, err)
	}
	name := tmp.Name()
	fail := func(err error) error {
		tmp.Close()
		os.Remove(name)
		return fmt.Errorf("%s cannot be written: %v", c.res.Arg, err)
	}
	if _, err := tmp.Write(c.data); err != nil {
		return fail(err)
	}
	if err := tmp.Chmod(c.mode); err != nil && !errors.Is(err, fs.ErrPermission) {
		// Windows has no mode bits to set; anywhere else a failure here is real.
		if filepath.Separator == '/' {
			return fail(err)
		}
	}
	if err := tmp.Close(); err != nil {
		return fail(err)
	}
	if err := os.Rename(name, c.full); err != nil {
		os.Remove(name)
		return fmt.Errorf("%s cannot be written: %v", c.res.Arg, err)
	}
	return nil
}

// runEdit plans and applies a write or an edit.
func (t *Toolbox) runEdit(name string, rawArgs []byte) (Result, error) {
	c, res, err := t.plan(name, rawArgs)
	if err != nil {
		return res, err
	}
	if err := c.apply(); err != nil {
		return c.res, err
	}
	return c.res, nil
}

// plan decodes the arguments of a write or an edit and works out the change.
func (t *Toolbox) plan(name string, rawArgs []byte) (*change, Result, error) {
	args, err := decodeArgs(rawArgs)
	if err != nil {
		return nil, Result{}, err
	}
	path := str(args, "path")
	res := Result{Arg: display(path)}
	if strings.TrimSpace(path) == "" {
		return nil, res, fmt.Errorf("%s needs a path", name)
	}
	full, err := t.resolveForWrite(path)
	if err != nil {
		return nil, res, err
	}
	old, mode, exists, err := readForChange(full, res.Arg)
	if err != nil {
		return nil, res, err
	}

	var next string
	switch name {
	case ToolWrite:
		content, ok := args["content"].(string)
		if !ok {
			return nil, res, errors.New("write needs content, the whole new text of the file")
		}
		if len(content) > maxEditFileBytes {
			return nil, res, fmt.Errorf("the content is %d bytes; a file written in one go may not pass %d", len(content), maxEditFileBytes)
		}
		next = content
	case ToolEdit:
		if !exists {
			return nil, res, fmt.Errorf("%s does not exist, so there is nothing to edit; use write to create it", res.Arg)
		}
		next, err = replaceOnce(old, args, res.Arg)
		if err != nil {
			return nil, res, err
		}
	default:
		return nil, res, fmt.Errorf("%q does not change files", name)
	}
	if mode == 0 {
		mode = 0o644
	}

	diff, added, removed := lineDiff(old, next)
	res.Diff = diff
	switch {
	case exists && next == old:
		res.Summary = "No changes"
		res.Text = fmt.Sprintf("%s already has exactly that content; nothing was changed.\n", res.Arg)
	case !exists:
		res.Summary = "Wrote " + plural(countLines(next), "line", "lines")
		res.Text = fmt.Sprintf("Created %s (%s).\n", res.Arg, plural(countLines(next), "line", "lines"))
	case name == ToolWrite:
		res.Summary = "Wrote " + plural(countLines(next), "line", "lines") + " (" + changeCounts(added, removed) + ")"
		res.Text = fmt.Sprintf("Replaced the content of %s (%s).\n", res.Arg, changeCounts(added, removed))
	default:
		res.Summary = changeCounts(added, removed)
		res.Text = fmt.Sprintf("Edited %s (%s).\n", res.Arg, changeCounts(added, removed))
	}
	return &change{full: full, data: []byte(next), mode: mode, res: res}, res, nil
}

// changeCounts reads "Added 2 lines, removed 1 line".
func changeCounts(added, removed int) string {
	switch {
	case added > 0 && removed > 0:
		return "Added " + plural(added, "line", "lines") + ", removed " + plural(removed, "line", "lines")
	case added > 0:
		return "Added " + plural(added, "line", "lines")
	case removed > 0:
		return "Removed " + plural(removed, "line", "lines")
	}
	return "No changes"
}

// resolveForWrite is resolve for a path that may not exist yet. A file that is
// there is judged as it is; one that is not is judged by the nearest folder that
// is, so a symlinked folder cannot carry a new file out of the project.
func (t *Toolbox) resolveForWrite(p string) (string, error) {
	p = strings.TrimSpace(p)
	full := filepath.Join(t.root, p)
	if filepath.IsAbs(p) {
		full = filepath.Clean(p)
	}
	if outside(t, full) {
		return "", fmt.Errorf("%s is outside the project folder, so it is not available", display(p))
	}
	if full == t.root {
		return "", fmt.Errorf("%s is the project folder itself, not a file", display(p))
	}
	real, tail := full, ""
	for {
		r, err := filepath.EvalSymlinks(real)
		if err == nil {
			real = r
			break
		}
		if !errors.Is(err, fs.ErrNotExist) {
			return "", err
		}
		parent := filepath.Dir(real)
		if parent == real {
			return "", fmt.Errorf("%s cannot be placed inside the project folder", display(p))
		}
		tail = filepath.Join(filepath.Base(real), tail)
		real = parent
	}
	if tail != "" {
		real = filepath.Join(real, tail)
	}
	if outside(t, real) {
		return "", fmt.Errorf("%s is outside the project folder, so it is not available", display(p))
	}
	rel, _ := filepath.Rel(t.root, real)
	for _, part := range strings.Split(rel, string(filepath.Separator)) {
		if strings.EqualFold(part, ".git") {
			return "", fmt.Errorf("%s is repository bookkeeping and may not be changed", display(p))
		}
	}
	if sensitive(filepath.Base(real)) {
		return "", fmt.Errorf("%s looks like it holds secrets (keys or passwords), so the model may not change it", display(p))
	}
	return real, nil
}

// readForChange loads the current content of a file about to change. A file that
// is not there reads as empty with exists false.
func readForChange(full, shown string) (content string, mode fs.FileMode, exists bool, err error) {
	st, err := os.Stat(full)
	if errors.Is(err, fs.ErrNotExist) {
		return "", 0, false, nil
	}
	if err != nil {
		return "", 0, false, fmt.Errorf("%s cannot be read: %v", shown, err)
	}
	if st.IsDir() {
		return "", 0, false, fmt.Errorf("%s is a folder, not a file", shown)
	}
	if !st.Mode().IsRegular() {
		return "", 0, false, fmt.Errorf("%s is not a regular file", shown)
	}
	if st.Size() > maxEditFileBytes {
		return "", 0, false, fmt.Errorf("%s is %d bytes; files over %d are not changed by the model", shown, st.Size(), maxEditFileBytes)
	}
	b, err := os.ReadFile(full)
	if err != nil {
		return "", 0, false, fmt.Errorf("%s cannot be read: %v", shown, err)
	}
	if bytes.IndexByte(b, 0) >= 0 {
		return "", 0, false, fmt.Errorf("%s looks like a binary file, so it is not changed", shown)
	}
	return string(b), st.Mode().Perm(), true, nil
}

// replaceOnce performs an edit's replacement and says clearly why it cannot.
func replaceOnce(old string, args map[string]any, shown string) (string, error) {
	from, _ := args["old_string"].(string)
	to, ok := args["new_string"].(string)
	if from == "" {
		return "", errors.New("edit needs old_string, the text to replace; to create or replace a whole file use write")
	}
	if !ok {
		return "", errors.New("edit needs new_string, the text to put in its place (it may be empty to delete)")
	}
	if from == to {
		return "", errors.New("old_string and new_string are the same, so there is nothing to change")
	}
	all, _ := args["replace_all"].(bool)
	// A file with Windows line endings is edited by a model that writes \n: meet it
	// half way, or no edit would ever match.
	if strings.Contains(old, "\r\n") && !strings.Contains(from, "\r\n") {
		from = strings.ReplaceAll(from, "\n", "\r\n")
		to = strings.ReplaceAll(to, "\n", "\r\n")
	}
	n := strings.Count(old, from)
	switch {
	case n == 0:
		return "", fmt.Errorf("old_string was not found in %s; read the file again and copy the text exactly, including spaces and indentation", shown)
	case n > 1 && !all:
		return "", fmt.Errorf("old_string appears %d times in %s; add surrounding lines to make it unique, or set replace_all to change every one", n, shown)
	}
	if all {
		return strings.ReplaceAll(old, from, to), nil
	}
	return strings.Replace(old, from, to, 1), nil
}

func countLines(s string) int {
	if s == "" {
		return 0
	}
	n := strings.Count(s, "\n")
	if !strings.HasSuffix(s, "\n") {
		n++
	}
	return n
}

func splitLines(s string) []string {
	if s == "" {
		return nil
	}
	return strings.Split(strings.TrimSuffix(s, "\n"), "\n")
}

// lineDiff is the change from a to b as the lines to show, and how many lines it
// adds and removes. Each line reads "%5d %c %s": the line number, a marker (' '
// unchanged, '-' removed, '+' added) and the text. Numbers are those of the old
// file for ' ' and '-' and of the new file for '+'. Distant changes are separated
// by a line holding only "...", and a diff past maxDiffLines is cut and says so.
func lineDiff(a, b string) (string, int, int) {
	x, y := splitLines(strings.ReplaceAll(a, "\r\n", "\n")), splitLines(strings.ReplaceAll(b, "\r\n", "\n"))
	pre := 0
	for pre < len(x) && pre < len(y) && x[pre] == y[pre] {
		pre++
	}
	suf := 0
	for suf < len(x)-pre && suf < len(y)-pre && x[len(x)-1-suf] == y[len(y)-1-suf] {
		suf++
	}
	mx, my := x[pre:len(x)-suf], y[pre:len(y)-suf]

	type op struct {
		kind byte // ' ', '-', '+'
		text string
		x, y int // line numbers, 1-based, in the old and the new file
	}
	var ops []op
	for i := 0; i < pre; i++ {
		ops = append(ops, op{' ', x[i], i + 1, i + 1})
	}
	// The middle is diffed properly (longest common subsequence) unless it is so
	// large that the table would be absurd, when it is shown as replaced outright.
	if len(mx)*len(my) <= diffMaxCells {
		w := len(my) + 1
		tbl := make([]int32, (len(mx)+1)*w)
		for i := len(mx) - 1; i >= 0; i-- {
			for j := len(my) - 1; j >= 0; j-- {
				if mx[i] == my[j] {
					tbl[i*w+j] = tbl[(i+1)*w+j+1] + 1
				} else if tbl[(i+1)*w+j] >= tbl[i*w+j+1] {
					tbl[i*w+j] = tbl[(i+1)*w+j]
				} else {
					tbl[i*w+j] = tbl[i*w+j+1]
				}
			}
		}
		i, j := 0, 0
		for i < len(mx) || j < len(my) {
			switch {
			case i < len(mx) && j < len(my) && mx[i] == my[j]:
				ops = append(ops, op{' ', mx[i], pre + i + 1, pre + j + 1})
				i++
				j++
			case j >= len(my) || (i < len(mx) && tbl[(i+1)*w+j] >= tbl[i*w+j+1]):
				ops = append(ops, op{'-', mx[i], pre + i + 1, 0})
				i++
			default:
				ops = append(ops, op{'+', my[j], 0, pre + j + 1})
				j++
			}
		}
	} else {
		for i, s := range mx {
			ops = append(ops, op{'-', s, pre + i + 1, 0})
		}
		for j, s := range my {
			ops = append(ops, op{'+', s, 0, pre + j + 1})
		}
	}
	for k := 0; k < suf; k++ {
		ops = append(ops, op{' ', x[len(x)-suf+k], len(x) - suf + k + 1, len(y) - suf + k + 1})
	}

	added, removed := 0, 0
	for _, o := range ops {
		switch o.kind {
		case '+':
			added++
		case '-':
			removed++
		}
	}
	if added == 0 && removed == 0 {
		return "", 0, 0
	}

	// Keep each change with diffContext lines around it.
	keep := make([]bool, len(ops))
	for i, o := range ops {
		if o.kind == ' ' {
			continue
		}
		for k := i - diffContext; k <= i+diffContext; k++ {
			if k >= 0 && k < len(ops) {
				keep[k] = true
			}
		}
	}
	var out []string
	gap := false
	for i, o := range ops {
		if !keep[i] {
			gap = len(out) > 0
			continue
		}
		if gap {
			out = append(out, "...")
			gap = false
		}
		n := o.x
		if o.kind == '+' {
			n = o.y
		}
		out = append(out, fmt.Sprintf("%5d %c %s", n, o.kind, o.text))
	}
	if len(out) > maxDiffLines {
		more := len(out) - maxDiffLines
		out = append(out[:maxDiffLines], fmt.Sprintf("... %d more lines of diff", more))
	}
	return strings.Join(out, "\n") + "\n", added, removed
}
