// Package chattools is the toolbox a chat turn may hand to the model so it can work in
// the directory the user is working in: look at files (list, read, grep), change them
// (edit, write) and run commands (run).
//
// It is deliberately separate from internal/toolrun. That package runs the tools of a
// governed run (writes, shell, frozen sessions) and its confinement leans on
// handle-relative opens that only exist on Linux; a chat on the user's own machine has
// to work on Windows and macOS too. So this one is small and portable, and it starts
// read-only: the caller turns on changes (WithEdits) and commands (WithRuns) separately,
// after settling with the user, and a model cannot be talked into more than it was given.
//
// The model is careless rather than hostile, so the guard is the same as toolrun's:
// every path is resolved (symlinks included) and must land inside the root, the
// repository's control directory is never read, and files that look like they hold
// credentials are never shown, because what the model reads goes to a remote provider.
package chattools

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// Limits. Tool output goes back to the model and into its context window, so each
// tool has a ceiling and says plainly when it stopped.
const (
	defaultReadLines = 2000
	maxLineBytes     = 2000
	maxReadBytes     = 256 << 10
	maxListEntries   = 300
	maxMatches       = 100
	maxGrepFiles     = 5000
	maxGrepFileBytes = 1 << 20
)

// Names of the tools, as the model calls them.
const (
	ToolList = "list"
	ToolRead = "read"
	ToolGrep = "grep"
)

// skipDirs are never searched: they hold other people's code or the repository's own
// bookkeeping, and a hit inside them answers a question nobody asked.
var skipDirs = map[string]bool{".git": true, "node_modules": true, "vendor": true, ".hg": true, ".svn": true}

// sensitive reports whether a file name looks like it holds a secret. The model reads
// with the user's permission to look at the project, not to carry its credentials to a
// provider, so these names are refused by every tool. A template such as .env.example
// is meant to be read and is not refused.
func sensitive(name string) bool {
	n := strings.ToLower(name)
	switch {
	case n == ".env" || (strings.HasPrefix(n, ".env.") && !strings.HasSuffix(n, ".example") &&
		!strings.HasSuffix(n, ".sample") && !strings.HasSuffix(n, ".template")):
		return true
	case n == ".npmrc" || n == ".netrc" || n == ".pypirc" || n == "credentials" || n == ".git-credentials":
		return true
	case strings.HasPrefix(n, "id_rsa") || strings.HasPrefix(n, "id_ed25519") || strings.HasPrefix(n, "id_ecdsa") || strings.HasPrefix(n, "id_dsa"):
		return !strings.HasSuffix(n, ".pub")
	case strings.HasSuffix(n, ".pem") || strings.HasSuffix(n, ".key") || strings.HasSuffix(n, ".p12") || strings.HasSuffix(n, ".pfx"):
		return true
	}
	return false
}

// Result is what one tool call produced.
type Result struct {
	// Text is what the model reads back.
	Text string
	// Summary is the one line the user sees under the call, e.g. "Read 120 lines".
	Summary string
	// Arg is the call as the user sees it inside the parentheses, e.g. "main.go".
	Arg string
	// Diff is, for a tool that changes a file, the change as lines to show (see
	// lineDiff). It is empty for the read tools.
	Diff string
	// Failed is set when the tool ran but did not succeed (a command that exited with
	// an error): Text still carries what the model needs to read.
	Failed bool
}

// Toolbox runs the tools against one directory.
type Toolbox struct {
	root string
	// edits is whether write and edit may run. A toolbox starts read-only; the
	// caller that has settled with the user that files may change says so.
	edits bool
	// runs is whether the run tool may start a command. Off until the caller has
	// settled with the user that commands may run.
	runs bool
}

// New opens a toolbox on dir. The directory must exist.
func New(dir string) (*Toolbox, error) {
	if strings.TrimSpace(dir) == "" {
		return nil, errors.New("chattools: no working directory given")
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return nil, fmt.Errorf("chattools: absolute path of %q: %w", dir, err)
	}
	real, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return nil, fmt.Errorf("chattools: resolve %s: %w", abs, err)
	}
	st, err := os.Stat(real)
	if err != nil {
		return nil, fmt.Errorf("chattools: %w", err)
	}
	if !st.IsDir() {
		return nil, fmt.Errorf("chattools: %s is not a directory", real)
	}
	return &Toolbox{root: real}, nil
}

// WithEdits returns a toolbox on the same folder whose write and edit tools run.
// Without it they refuse, whatever the model asks, so a caller that only meant to
// let the model look cannot be talked into more.
func (t *Toolbox) WithEdits() *Toolbox {
	c := *t
	c.edits = true
	return &c
}

// Root is the resolved directory the tools are confined to.
func (t *Toolbox) Root() string { return t.root }

// Definition is one tool as the model is told about it.
type Definition struct {
	Name        string
	Description string
	// Schema is the JSON schema of the arguments.
	Schema json.RawMessage
}

// Definitions lists the tools in the order they are offered.
func Definitions() []Definition {
	return []Definition{
		{ToolList, "List the files and folders in a directory of the project. Folders end with /.",
			json.RawMessage(`{"type":"object","properties":{"path":{"type":"string","description":"Directory, relative to the project root. Omit for the root."}}}`)},
		{ToolRead, "Read a text file of the project. Lines come back numbered. Long files are read in parts with offset and limit.",
			json.RawMessage(`{"type":"object","properties":{"path":{"type":"string","description":"File, relative to the project root."},"offset":{"type":"integer","description":"First line to read, starting at 1."},"limit":{"type":"integer","description":"How many lines to read."}},"required":["path"]}`)},
		{ToolGrep, "Search the project's text files for a regular expression. Returns path:line:text.",
			json.RawMessage(`{"type":"object","properties":{"pattern":{"type":"string","description":"Regular expression (RE2)."},"path":{"type":"string","description":"Directory or file to search. Omit for the whole project."}},"required":["pattern"]}`)},
	}
}

// Run performs one call. An error is the tool's own refusal or failure and is meant to
// be read by the model (and the user) as the result of the call; it never panics the
// turn.
func (t *Toolbox) Run(name string, rawArgs json.RawMessage) (Result, error) {
	if Mutating(name) {
		if !t.edits {
			return Result{Arg: display(str(mustArgs(rawArgs), "path"))}, fmt.Errorf("%s is not available: this session may only look at files, not change them", name)
		}
		return t.runEdit(name, rawArgs)
	}
	if Runs(name) {
		if !t.runs {
			return Result{}, fmt.Errorf("%s is not available: this session may not run commands", name)
		}
		return t.runCommand(rawArgs)
	}
	args, err := decodeArgs(rawArgs)
	if err != nil {
		return Result{}, err
	}
	switch name {
	case ToolList:
		return t.list(str(args, "path"))
	case ToolRead:
		return t.read(str(args, "path"), num(args, "offset"), num(args, "limit"))
	case ToolGrep:
		return t.grep(str(args, "pattern"), str(args, "path"))
	}
	return Result{}, fmt.Errorf("there is no tool called %q; the tools are list, read, grep, edit, write and run", name)
}

// decodeArgs reads a call's arguments, which must be a JSON object (or nothing).
func decodeArgs(raw []byte) (map[string]any, error) {
	args := map[string]any{}
	if len(strings.TrimSpace(string(raw))) > 0 {
		if err := json.Unmarshal(raw, &args); err != nil {
			return nil, fmt.Errorf("the arguments are not a JSON object: %v", err)
		}
	}
	return args, nil
}

// mustArgs is decodeArgs for a caller that only wants a hint to show: bad
// arguments read as none.
func mustArgs(raw []byte) map[string]any {
	args, err := decodeArgs(raw)
	if err != nil {
		return map[string]any{}
	}
	return args
}

func str(m map[string]any, k string) string {
	s, _ := m[k].(string)
	return s
}

func num(m map[string]any, k string) int {
	switch v := m[k].(type) {
	case float64:
		return int(v)
	case string:
		var n int
		fmt.Sscanf(v, "%d", &n)
		return n
	}
	return 0
}

func outside(t *Toolbox, full string) bool {
	r, err := filepath.Rel(t.root, full)
	return err != nil || r == ".." || strings.HasPrefix(r, ".."+string(filepath.Separator))
}

// resolve turns a model-supplied path into an absolute one inside the root.
func (t *Toolbox) resolve(p string) (string, error) {
	p = strings.TrimSpace(p)
	full := t.root
	if p != "" && p != "." {
		if filepath.IsAbs(p) {
			full = filepath.Clean(p)
		} else {
			full = filepath.Join(t.root, p)
		}
	}
	// A path that leaves the folder is refused before anyone asks whether it exists:
	// "does not exist" for a place outside the project would map the machine.
	if outside(t, full) {
		return "", fmt.Errorf("%s is outside the project folder, so it is not available", display(p))
	}
	real, err := filepath.EvalSymlinks(full)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return "", fmt.Errorf("%s does not exist", display(p))
		}
		return "", err
	}
	if outside(t, real) {
		return "", fmt.Errorf("%s is outside the project folder, so it is not available", display(p))
	}
	rel, _ := filepath.Rel(t.root, real)
	if rel != "." {
		for _, part := range strings.Split(rel, string(filepath.Separator)) {
			if strings.EqualFold(part, ".git") {
				return "", fmt.Errorf("%s is repository bookkeeping and is not available", display(p))
			}
		}
		if sensitive(filepath.Base(real)) {
			return "", fmt.Errorf("%s looks like it holds secrets (keys or passwords), so it is not shown to the model", display(p))
		}
	}
	return real, nil
}

func display(p string) string {
	if strings.TrimSpace(p) == "" {
		return "."
	}
	return p
}

// rel is a path as the user would type it: relative to the root, forward slashes.
func (t *Toolbox) rel(full string) string {
	r, err := filepath.Rel(t.root, full)
	if err != nil {
		return full
	}
	return filepath.ToSlash(r)
}

func (t *Toolbox) list(p string) (Result, error) {
	res := Result{Arg: display(p)}
	full, err := t.resolve(p)
	if err != nil {
		return res, err
	}
	entries, err := os.ReadDir(full)
	if err != nil {
		return res, fmt.Errorf("%s cannot be listed: %v", res.Arg, err)
	}
	var names []string
	for _, e := range entries {
		if e.Name() == ".git" || (!e.IsDir() && sensitive(e.Name())) {
			continue
		}
		n := e.Name()
		if e.IsDir() {
			n += "/"
		}
		names = append(names, n)
	}
	sort.Strings(names)
	shown := names
	more := 0
	if len(shown) > maxListEntries {
		more = len(shown) - maxListEntries
		shown = shown[:maxListEntries]
	}
	var b strings.Builder
	for _, n := range shown {
		b.WriteString(n + "\n")
	}
	if more > 0 {
		fmt.Fprintf(&b, "... and %d more entries\n", more)
	}
	if len(names) == 0 {
		b.WriteString("(empty)\n")
	}
	res.Text = b.String()
	res.Summary = "Listed " + plural(len(names), "entry", "entries")
	return res, nil
}

func (t *Toolbox) read(p string, offset, limit int) (Result, error) {
	res := Result{Arg: display(p)}
	if strings.TrimSpace(p) == "" {
		return res, errors.New("read needs a path")
	}
	full, err := t.resolve(p)
	if err != nil {
		return res, err
	}
	st, err := os.Stat(full)
	if err != nil {
		return res, err
	}
	if st.IsDir() {
		return res, fmt.Errorf("%s is a folder: use list to see what is inside", res.Arg)
	}
	f, err := os.Open(full)
	if err != nil {
		return res, fmt.Errorf("%s cannot be opened: %v", res.Arg, err)
	}
	defer f.Close()
	if offset < 1 {
		offset = 1
	}
	if limit < 1 || limit > defaultReadLines {
		limit = defaultReadLines
	}

	br := bufio.NewReaderSize(f, 64<<10)
	var b strings.Builder
	line, shown, total := 0, 0, 0
	cut := false
	for {
		text, err := br.ReadString('\n')
		if text == "" && err != nil {
			if err != io.EOF {
				return res, fmt.Errorf("%s cannot be read: %v", res.Arg, err)
			}
			break
		}
		line++
		total = line
		if line == 1 && strings.ContainsRune(text, 0) {
			return res, fmt.Errorf("%s looks like a binary file, so it is not shown", res.Arg)
		}
		if line >= offset && shown < limit && !cut {
			text = strings.TrimRight(text, "\r\n")
			if len(text) > maxLineBytes {
				text = text[:maxLineBytes] + " ...[line cut]"
			}
			if b.Len()+len(text) > maxReadBytes {
				cut = true
			} else {
				fmt.Fprintf(&b, "%6d\t%s\n", line, text)
				shown++
			}
		}
		if err != nil {
			break
		}
	}
	if total == 0 {
		res.Text = "(empty file)\n"
		res.Summary = "Read 0 lines"
		return res, nil
	}
	if shown == 0 && offset > total {
		return res, fmt.Errorf("%s has only %s; offset %d is past the end", res.Arg, plural(total, "line", "lines"), offset)
	}
	if end := offset + shown - 1; end < total {
		fmt.Fprintf(&b, "[showing lines %d-%d of %d; read again with offset %d for the rest]\n", offset, end, total, end+1)
	}
	res.Text = b.String()
	res.Summary = "Read " + plural(shown, "line", "lines")
	return res, nil
}

func (t *Toolbox) grep(pattern, p string) (Result, error) {
	res := Result{Arg: pattern}
	if strings.TrimSpace(pattern) == "" {
		return res, errors.New("grep needs a pattern: an empty one matches every line")
	}
	if strings.TrimSpace(p) != "" {
		res.Arg = pattern + " in " + p
	}
	re, err := regexp.Compile(pattern)
	if err != nil {
		return res, fmt.Errorf("the pattern is not a valid regular expression (RE2, no lookahead): %v; to search for it literally, escape it: %s", err, regexp.QuoteMeta(pattern))
	}
	start, err := t.resolve(p)
	if err != nil {
		return res, err
	}

	var b strings.Builder
	matches, files, scanned := 0, 0, 0
	stopped := false
	visit := func(path string) {
		if stopped {
			return
		}
		scanned++
		f, err := os.Open(path)
		if err != nil {
			return
		}
		defer f.Close()
		head := make([]byte, 8<<10)
		n, _ := f.Read(head)
		if strings.ContainsRune(string(head[:n]), 0) {
			return
		}
		if _, err := f.Seek(0, io.SeekStart); err != nil {
			return
		}
		sc := bufio.NewScanner(io.LimitReader(f, maxGrepFileBytes))
		sc.Buffer(make([]byte, 64<<10), 1<<20)
		hit := false
		for ln := 1; sc.Scan(); ln++ {
			text := sc.Text()
			if !re.MatchString(text) {
				continue
			}
			if !hit {
				hit = true
				files++
			}
			matches++
			if len(text) > 300 {
				text = text[:300] + "..."
			}
			fmt.Fprintf(&b, "%s:%d:%s\n", t.rel(path), ln, strings.TrimRight(text, " \t\r"))
			if matches >= maxMatches {
				stopped = true
				return
			}
		}
	}

	st, err := os.Stat(start)
	if err != nil {
		return res, err
	}
	if !st.IsDir() {
		visit(start)
	} else {
		_ = filepath.WalkDir(start, func(path string, d fs.DirEntry, err error) error {
			if err != nil || stopped {
				return nil
			}
			if d.IsDir() {
				if path != start && skipDirs[d.Name()] {
					return filepath.SkipDir
				}
				return nil
			}
			if !d.Type().IsRegular() || sensitive(d.Name()) { // symlinks, devices and secrets are not searched
				return nil
			}
			if scanned >= maxGrepFiles {
				stopped = true
				return nil
			}
			visit(path)
			return nil
		})
	}

	if matches == 0 {
		res.Text = fmt.Sprintf("no matches for %s\n", pattern)
		res.Summary = "No matches"
		return res, nil
	}
	if stopped {
		b.WriteString("[stopped early: there may be more matches; narrow the pattern or search one folder with path]\n")
	}
	res.Text = b.String()
	res.Summary = fmt.Sprintf("Found %s in %s", plural(matches, "match", "matches"), plural(files, "file", "files"))
	return res, nil
}

func plural(n int, one, many string) string {
	if n == 1 {
		return "1 " + one
	}
	return fmt.Sprintf("%d %s", n, many)
}
