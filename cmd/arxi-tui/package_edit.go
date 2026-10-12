package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	extv2 "github.com/michiTrader/arxi_tui/internal/ext/v2"
)

// This file is how the interface agent (ui_edit) creates, changes, switches off and
// removes packages. It reaches only what a person could do by hand with a text editor
// in the user's own packages folder, and nothing more:
//
//   - only the user's packages that are one document each. A package that is a folder
//     has files of its own (scripts) that the agent cannot write, and a project's
//     packages belong to the repository, not to this user's interface.
//   - every document is validated whole, exactly as at load, before anything is asked.
//   - writing a package never approves it. The approval is bound to the package's
//     hash and is given only by the user typing /package approve <id> <code> (the code
//     is shown by the host, not by the agent), so a package the agent wrote does
//     nothing until then, however the agent was permitted to edit the interface.
//   - it can be taken back with /ui undo: what an edit changed is remembered as the
//     content each touched package had before (packageDelta), so an undo restores
//     those packages and never touches one the user wrote since.

// maxPackageOps bounds one ui_edit call.
const maxPackageOps = 8

var packageIDRe = regexp.MustCompile(`^[a-z][a-z0-9-]{0,62}$`)

// packageDelta says what to write: a package id and the document it should have, or nil
// for no document at all.
type packageDelta map[string]*string

// packageEdit is a drafted change to packages on its way from the bridge to the loop.
type packageEdit struct {
	// delta is what to write; base is what each of those packages held when the change
	// was drafted, so the loop can refuse the change if the folder moved on meanwhile.
	delta, base packageDelta
	// removed lists the ids whose approval goes with them.
	removed []string
	// diff is what the user is shown, and mustAsk says it is asked even in full access
	// (anything written or switched on; removing and switching off only narrow).
	diff    string
	mustAsk bool
	// put lists the ids written or switched on, for the message about approving them.
	put []string
}

// fresh reports whether every package the edit touches still holds what it held when
// the edit was drafted.
func (e *packageEdit) fresh() bool {
	for id, want := range e.base {
		got, ok := readPackageDoc(id)
		switch {
		case want == nil && ok:
			return false
		case want != nil && (!ok || got != *want):
			return false
		}
	}
	return true
}

func packagePath(id string) (string, error) {
	d := userPackagesDir()
	if d == "" {
		return "", errNoConfigDir
	}
	if !packageIDRe.MatchString(id) {
		return "", fmt.Errorf("%q is not a package id", id)
	}
	return filepath.Join(d, id+".json"), nil
}

// readPackageDoc returns the single-document user package with that id as it is on disk.
func readPackageDoc(id string) (string, bool) {
	path, err := packagePath(id)
	if err != nil {
		return "", false
	}
	b, err := readBounded(path, extv2.MaxDocBytes+1)
	if err != nil {
		return "", false
	}
	return string(b), true
}

// snapshotPackages reads what each id holds now, nil where there is no document.
func snapshotPackages(ids []string) packageDelta {
	out := packageDelta{}
	for _, id := range ids {
		if doc, ok := readPackageDoc(id); ok {
			d := doc
			out[id] = &d
		} else {
			out[id] = nil
		}
	}
	return out
}

func deltaIDs(d packageDelta) []string {
	ids := make([]string, 0, len(d))
	for id := range d {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

// applyPackageDelta writes the documents of a delta. Every id is checked before the
// first write, so a delta that names something that is not an id writes nothing.
func applyPackageDelta(d packageDelta) error {
	if len(d) == 0 {
		return nil
	}
	if len(d) > maxPackages {
		return fmt.Errorf("a change to %d packages at once is more than the %d allowed", len(d), maxPackages)
	}
	for _, id := range deltaIDs(d) {
		if _, err := packagePath(id); err != nil {
			return err
		}
		if d[id] != nil && len(*d[id]) > extv2.MaxDocBytes {
			return fmt.Errorf("package %s is larger than the %d bytes a package may be", id, extv2.MaxDocBytes)
		}
	}
	for _, id := range deltaIDs(d) {
		path, _ := packagePath(id)
		if d[id] == nil {
			if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
				return err
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			return err
		}
		if err := writeAtomic(path, []byte(*d[id])); err != nil {
			return err
		}
	}
	return nil
}

// packageOp is one entry of ui_edit's packages list. Exactly one key is set.
type packageOp struct {
	Put     json.RawMessage `json:"put"`
	Remove  string          `json:"remove"`
	Disable string          `json:"disable"`
	Enable  string          `json:"enable"`
}

// draftPackages turns the packages argument of ui_edit into a validated edit. Nothing
// is written: a refusal names the entry and the reason, and changes nothing.
func draftPackages(raw json.RawMessage, existing packageSet, beh behaviour) (*packageEdit, error) {
	var ops []packageOp
	dec := json.NewDecoder(strings.NewReader(string(raw)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&ops); err != nil {
		return nil, fmt.Errorf("packages: %w (it is a list of {\"put\": <package>}, {\"remove\": \"<id>\"}, {\"disable\": \"<id>\"} or {\"enable\": \"<id>\"})", err)
	}
	if len(ops) == 0 {
		return nil, fmt.Errorf("packages: the list is empty; write at least one of put, remove, disable, enable")
	}
	if len(ops) > maxPackageOps {
		return nil, fmt.Errorf("packages: %d entries, the most in one change is %d", len(ops), maxPackageOps)
	}
	ns := packageNames(beh)
	e := &packageEdit{delta: packageDelta{}, base: packageDelta{}}
	var diffs, caps []string
	seen := map[string]bool{}

	// ownDoc finds an existing package this interface may change.
	ownDoc := func(who, id string) (installedPackage, error) {
		p, ok := existing.find(id)
		switch {
		case !ok:
			return p, fmt.Errorf("%s: there is no package %q; /package lists the ones there are", who, id)
		case p.Scope != scopeUser:
			return p, fmt.Errorf("%s: %q belongs to the project folder, not to the user's own packages, so the interface does not change it", who, id)
		case p.Dir != "":
			return p, fmt.Errorf("%s: %q is a folder with files of its own, so the interface does not change it; the user edits it by hand", who, id)
		}
		return p, nil
	}
	record := func(id, newDoc string, remove bool, widens bool, p *extv2.Package) error {
		if seen[id] {
			return fmt.Errorf("packages: %q appears twice in one change; keep one entry", id)
		}
		seen[id] = true
		old, had := readPackageDoc(id)
		base := snapshotPackages([]string{id})
		e.base[id] = base[id]
		oldSrc, newSrc := []byte("{}"), []byte("{}")
		if had {
			oldSrc = []byte(old)
		}
		if remove {
			e.delta[id] = nil
			e.removed = append(e.removed, id)
		} else {
			d := newDoc
			e.delta[id] = &d
			newSrc = []byte(newDoc)
		}
		if remove && !had {
			delete(e.delta, id)
			delete(e.base, id)
			return nil
		}
		if !remove && had && old == newDoc {
			delete(e.delta, id)
			delete(e.base, id)
			return nil
		}
		d, err := uiDiffText(oldSrc, newSrc)
		if err != nil {
			return fmt.Errorf("packages: %q: %w", id, err)
		}
		diffs = append(diffs, "      package "+id+":\n"+d)
		if widens && p != nil {
			e.mustAsk = true
			e.put = append(e.put, id)
			for _, c := range p.Capabilities() {
				caps = append(caps, "      "+id+" can: "+cleanWarnText(c))
			}
			if len(p.Capabilities()) == 0 {
				caps = append(caps, "      "+id+" can only show text and use what the app itself offers")
			}
		}
		return nil
	}

	for i, op := range ops {
		who := fmt.Sprintf("packages[%d]", i)
		set := 0
		for _, present := range []bool{len(op.Put) > 0, op.Remove != "", op.Disable != "", op.Enable != ""} {
			if present {
				set++
			}
		}
		if set != 1 {
			return nil, fmt.Errorf("%s: write exactly one of put, remove, disable, enable", who)
		}
		switch {
		case len(op.Put) > 0:
			pkg, err := extv2.Parse(op.Put)
			if err != nil {
				return nil, fmt.Errorf("%s: %w", who, err)
			}
			other := map[string]bool{}
			for _, q := range existing.pkgs {
				if q.ID == pkg.ID {
					continue
				}
				for _, c := range q.Commands {
					other[c.Name] = true
				}
			}
			opt := ns.options()
			opt.ReservedCommands = func(n string) bool { return ns.taken(n) || other[n] }
			if err := pkg.Validate(opt); err != nil {
				return nil, fmt.Errorf("%s (%s): %w", who, pkg.ID, err)
			}
			if _, ok := existing.find(pkg.ID); ok {
				if _, err := ownDoc(who, pkg.ID); err != nil {
					return nil, err
				}
			} else if len(existing.pkgs) >= maxPackages {
				return nil, fmt.Errorf("%s: there are already %d packages, the most there can be; remove one first", who, len(existing.pkgs))
			}
			if d := userPackagesDir(); d != "" {
				if st, err := os.Lstat(filepath.Join(d, pkg.ID)); err == nil && st.IsDir() {
					return nil, fmt.Errorf("%s: %q is already a folder in the packages folder, so a document with that id would clash with it; choose another id", who, pkg.ID)
				}
			}
			if err := record(pkg.ID, string(pkg.Encode()), false, true, pkg); err != nil {
				return nil, err
			}
		case op.Remove != "":
			p, err := ownDoc(who, op.Remove)
			if err != nil {
				return nil, err
			}
			if err := record(p.ID, "", true, false, nil); err != nil {
				return nil, err
			}
		default:
			id, off := op.Disable, true
			if op.Enable != "" {
				id, off = op.Enable, false
			}
			p, err := ownDoc(who, id)
			if err != nil {
				return nil, err
			}
			c := *p.Package
			c.Disabled = off
			if err := c.Validate(ns.options()); err != nil {
				return nil, fmt.Errorf("%s (%s): %w", who, id, err)
			}
			// Switching off only narrows, so it is not asked about in full access;
			// switching on puts back what the user approved, so it is.
			if err := record(id, string(c.Encode()), false, !off, &c); err != nil {
				return nil, err
			}
		}
	}
	if len(e.delta) == 0 {
		return e, nil
	}
	e.diff = strings.Join(diffs, "") + strings.Join(caps, "\n")
	if len(caps) > 0 {
		e.diff += "\n"
	}
	return e, nil
}

// packagesGuide is the PACKAGES chapter of ui_guide: the format, what can go in it, and
// what is there now.
func packagesGuide(set packageSet) string {
	var b strings.Builder
	b.WriteString(`PACKAGES. A package adds / commands, rules and tools without any program: one JSON document in the user's packages folder. Pass packages: [...] to ui_edit, a list of entries, each exactly one of
  {"put": <package>}      create it, or replace the package with that id
  {"remove": "<id>"}      delete it
  {"disable": "<id>"} / {"enable": "<id>"}   switch it off or on without deleting it
You can only change the user's own packages that are a single document; packages that are folders or belong to the project are theirs to edit.

  {"protocol":"ext/v2", "id":"weather", "name":"Weather", "version":"1",
   "permissions": {"net":["wttr.in"]},
   "commands":[{"name":"weather","description":"Weather of a city","usage":"/weather <city>","min_args":1,
                "op":{"kind":"http","url":"https://wttr.in/${args}?format=3"}}]}

  id          lowercase letters, digits and '-', starting with a letter. The command name is lowercase too and cannot be an existing command.
  permissions everything the package may reach; empty means nothing. net: hosts an http op may call ("api.example.com", "api.example.com:8443", or "local:127.0.0.1:8080" for this machine). exec: bare program names an exec op may start ("git"). open: https hosts the open: action may send the browser to. send: true lets a command's text go to the model. List only what an op uses; the user reads this list before approving.
  commands    [{"name","description","usage","min_args","category","op"}]. The op runs when the user types /name <words>.
  rules       [{"id","stage":"input.pre","when":{"prefix"|"contains"|"matches"},"on_error":"continue"|"block","op"}]. A rule sees every plain-text line (never a / line) before it is sent. It can reword the line (result "transform"), hold it back or answer for it, with {"result":"outcome"} and the op printing {"verdict":"continue|transform|handled|block","text":"...","say":"...","reason":"..."}. It can never send more than the user typed, and a rule that fails lets the line through unless on_error is "block".
  tools       [{"name","description","usage","min_args","op"}]. An operation YOU (the model) may call by yourself as pkg_<id>_<name> with {"args": "<text>"}; the text it returns comes back to you as data. description is required and says when to use it. Only "result":"say" (the default) is allowed. A tool that starts a program asks the user like the run tool does, one that makes a request like the web tools do. The user is told at approval that the package lets the model call its tools. Offer a tool only when the user wants you to be able to do it unasked; at most 8 per package, id and name together at most 59 characters, no underscores in either.
  op          {"kind":"builtin","name":"<host function>"} | {"kind":"http","url","method":"GET|POST","headers","body","body_encoding":"json|form|text","extract":"a.b.0.c"} | {"kind":"exec","argv":["./script.sh","${args}"]}. "result" says what the output is, and is only for http and exec: leave it out of a builtin. "say" (shown to the user, the default for commands), "send" (to the model; needs permissions.send), "transform" (rules), "outcome" (a JSON outcome). extract picks one value out of a JSON answer by a dotted path.
  templates   ${args} (everything typed after the name), ${1}..${9} (its words), ${input} (the line, in rules). A url must start with https://host/ written out; in a JSON body a placeholder sits inside a JSON string. exec is argv only, never a shell; ./name is a file inside the package folder, which you cannot create, so use a program named in permissions.exec.
  builtins    `)
	b.WriteString(strings.Join(hostBuiltinNames(), ", "))
	b.WriteString(` (echo shows the words typed after the command).

A package you write DOES NOTHING until the user approves exactly that version by typing /package approve <id> <code> themselves; the host shows them what it can do and the code. You cannot approve it, and changing it later asks again. After a ui_edit with a package, say it was saved and that they need to approve it by typing exactly /package approve <id>, with nothing after the id: the host then shows them what it can do and the code to type. Never make up or quote a code yourself, and never say it works before. Every change to packages is shown to the user as a diff and asked about, even in full access (switching off and removing excepted), and /ui undo takes the last one back.
`)
	b.WriteString("\nPackages now:")
	if len(set.pkgs) == 0 {
		b.WriteString(" none\n")
		return b.String()
	}
	grants := readGrants()
	cwd, _ := os.Getwd()
	for _, p := range set.pkgs {
		kind := "single document"
		if p.Dir != "" {
			kind = "folder"
		}
		fmt.Fprintf(&b, "\n  %s (%s, %s): %s", cleanWarnText(p.ID), p.Scope, kind, packageState(p, grants, cwd))
		for _, c := range p.Commands {
			b.WriteString(" /" + cleanWarnText(c.Name))
		}
		for _, t := range p.Tools {
			b.WriteString(" tool:" + cleanWarnText(t.Name))
		}
	}
	for _, pr := range set.problems {
		b.WriteString("\n  not loaded: " + cleanWarnText(pr))
	}
	b.WriteString("\n")
	return b.String()
}

func hostBuiltinNames() []string {
	names := make([]string, 0, len(hostBuiltins))
	for n := range hostBuiltins {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

// applyPackageEdit writes a drafted change to the packages and withdraws the approval of
// the ones removed (an id used again later is a new question). A failure puts back what
// the edit had already written, so the folder is never left half changed.
func applyPackageEdit(e *packageEdit, cwd string) error {
	if e == nil {
		return nil
	}
	if err := applyPackageDelta(e.delta); err != nil {
		_ = applyPackageDelta(e.base)
		return fmt.Errorf("the packages could not be written: %w", err)
	}
	for _, id := range e.removed {
		if _, err := revokePackage(scopeUser, cwd, id); err != nil {
			return fmt.Errorf("the approval of %s could not be withdrawn: %w", id, err)
		}
	}
	return nil
}
