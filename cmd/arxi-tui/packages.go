package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	extv2 "github.com/michiTrader/arxi_tui/internal/ext/v2"
)

// This file is the host's store of ext/v2 packages (docs/DESIGN-EXTENSIBILITY.md): where
// they live, how they are read, and how the user's consent to them is remembered.
//
// # Where they live
//
//	<config>/packages/<id>.json          a package that is one document
//	<config>/packages/<id>/package.json  a package with files of its own (scripts)
//	<project>/.arxi/packages/...         the same, shipped by a repository
//
// The file or folder is named after the package id, so the id a person reads in a
// directory listing is the one the host uses.
//
// # Why consent is kept apart from /project trust
//
// /project trust is about colours, wording and key bindings. A package can reach the
// network and run programs, which is a different question, so each package is its own
// grant and a cloned folder cannot ride on the answer the user gave for its theme.
//
// # What a grant is bound to
//
// The package's digest (what it says) plus, for a folder, the bytes of every file in it
// (what it would run): a script edited after the user agreed is a different program.
// The digest leaves out the "disabled" flags, because switching a package off and on
// again adds no power and must not ask the same question twice.
//
// A package that is not granted is listed and inert. It is never run "just this once".

const (
	packagesDirName   = "packages"
	packageFileInDir  = "package.json"
	packageGrantsFile = "packages-consent.json"

	// Bounds on what a folder may hold, so reading it to hash it cannot be made to
	// take long or to fill memory.
	maxPackages          = 64
	maxPackageFiles      = 200
	maxPackageFolderSize = 4 << 20
)

// Package scopes.
const (
	scopeUser    = "user"
	scopeProject = "project"
)

// installedPackage is one package as found on disk.
type installedPackage struct {
	*extv2.Package
	Scope string
	// Path is the file that holds the document; Dir is the folder of a package that has
	// files of its own, "" for a package that is one document.
	Path string
	Dir  string
	// Hash is what a grant is bound to; Granted says the user's grant matches it.
	Hash    string
	Granted bool
}

// Runnable reports whether the package may run: it validated, the user granted exactly
// this version of it, and it is not switched off.
func (p installedPackage) runnable() bool {
	return p.Package != nil && p.Granted && !p.Disabled
}

// packageSet is everything found, usable or not.
type packageSet struct {
	pkgs []installedPackage
	// problems are the reasons something on disk was not loaded, one line each, each
	// naming the file.
	problems []string
}

// packageNamespace says which names a package command may not take.
type packageNamespace struct {
	// taken reports a name the host or the user's own behaviour already uses.
	taken func(name string) bool
	// builtins reports a builtin the host registered.
	builtins func(name string) bool
}

func (ns packageNamespace) options() extv2.Options {
	return extv2.Options{ReservedCommands: ns.taken, Builtins: ns.builtins}
}

func userPackagesDir() string {
	d := configDir()
	if d == "" {
		return ""
	}
	return filepath.Join(d, packagesDirName)
}

func projectPackagesDir(cwd string) string {
	d := projectSettingsDir(cwd)
	if d == "" {
		return ""
	}
	return filepath.Join(d, packagesDirName)
}

// grantKey names a package in the grants file. A project package is keyed by the folder
// too, so the same id in two repositories is two questions.
func grantKey(scope, cwd, id string) string {
	if scope == scopeProject {
		return scopeProject + ":" + trustKey(cwd) + ":" + id
	}
	return scopeUser + ":" + id
}

func grantsPath() string {
	d := configDir()
	if d == "" {
		return ""
	}
	return filepath.Join(d, packageGrantsFile)
}

func readGrants() map[string]string {
	out := map[string]string{}
	p := grantsPath()
	if p == "" {
		return out
	}
	if b, err := os.ReadFile(p); err == nil {
		_ = json.Unmarshal(b, &out)
	}
	return out
}

func writeGrants(m map[string]string) error {
	p := grantsPath()
	if p == "" {
		return errNoConfigDir
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		return err
	}
	b, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	return writeAtomic(p, append(b, '\n'))
}

// packageHash is what a grant is bound to. dir is the package's own folder, "" for a
// single document.
func packageHash(p *extv2.Package, dir string) (string, error) {
	c := *p
	c.Disabled = false
	c.Commands = append([]extv2.Command(nil), p.Commands...)
	for i := range c.Commands {
		c.Commands[i].Disabled = false
	}
	c.Rules = append([]extv2.Rule(nil), p.Rules...)
	for i := range c.Rules {
		c.Rules[i].Disabled = false
	}
	c.Tools = append([]extv2.Tool(nil), p.Tools...)
	for i := range c.Tools {
		c.Tools[i].Disabled = false
	}
	h := sha256.New()
	fmt.Fprintf(h, "digest\x00%s\x00", c.Digest())
	if dir != "" {
		if err := hashFolder(h, dir); err != nil {
			return "", err
		}
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// hashFolder feeds every file under dir into h, in a fixed order, with its relative name
// and length so moving bytes between files changes the hash. A link is hashed as the
// path it holds, never followed: where it points is not what the user agreed to.
func hashFolder(h io.Writer, dir string) error {
	var files []string
	total := int64(0)
	err := filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if path == dir {
			return nil
		}
		if len(files) >= maxPackageFiles {
			return fmt.Errorf("the folder has more than %d files; a package that big is not one to run", maxPackageFiles)
		}
		files = append(files, path)
		if info, ierr := d.Info(); ierr == nil && info.Mode().IsRegular() {
			total += info.Size()
			if total > maxPackageFolderSize {
				return fmt.Errorf("the folder holds more than %d bytes", maxPackageFolderSize)
			}
		}
		return nil
	})
	if err != nil {
		return err
	}
	sort.Strings(files)
	for _, f := range files {
		rel, err := filepath.Rel(dir, f)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		info, err := os.Lstat(f)
		if err != nil {
			return err
		}
		switch {
		case info.Mode()&os.ModeSymlink != 0:
			target, err := os.Readlink(f)
			if err != nil {
				return err
			}
			fmt.Fprintf(h, "link\x00%s\x00%s\x00", rel, filepath.ToSlash(target))
		case info.IsDir():
			fmt.Fprintf(h, "dir\x00%s\x00", rel)
		case info.Mode().IsRegular():
			b, err := os.ReadFile(f)
			if err != nil {
				return err
			}
			// The executable bit is part of what a file is: a script made runnable
			// after the user agreed is a different thing.
			fmt.Fprintf(h, "file\x00%s\x00%d\x00%o\x00", rel, len(b), info.Mode().Perm()&0o111)
			_, _ = h.Write(b)
		default:
			return fmt.Errorf("%s is not a plain file, a folder or a link", rel)
		}
	}
	return nil
}

// loadPackages reads every package of one scope. grants is the user's consent file as
// read; cwd keys a project's grants.
func loadPackages(dir, scope, cwd string, grants map[string]string, ns packageNamespace) packageSet {
	var set packageSet
	if dir == "" {
		return set
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		if !os.IsNotExist(err) {
			set.problems = append(set.problems, fmt.Sprintf("%s: %v", dir, err))
		}
		return set
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
	for _, e := range entries {
		name := e.Name()
		if strings.HasPrefix(name, ".") {
			continue
		}
		if len(set.pkgs) >= maxPackages {
			set.problems = append(set.problems, fmt.Sprintf("%s: more than %d packages; the rest were not read", dir, maxPackages))
			break
		}
		full := filepath.Join(dir, name)
		info, err := os.Lstat(full)
		if err != nil {
			set.problems = append(set.problems, fmt.Sprintf("%s: %v", full, err))
			continue
		}
		var id, docPath, folder string
		switch {
		case info.Mode()&os.ModeSymlink != 0:
			set.problems = append(set.problems, fmt.Sprintf("%s is a link, and packages are read only from where they sit, so it was not loaded", full))
			continue
		case info.IsDir():
			id, docPath, folder = name, filepath.Join(full, packageFileInDir), full
		case info.Mode().IsRegular() && strings.HasSuffix(name, ".json"):
			id, docPath = strings.TrimSuffix(name, ".json"), full
		default:
			continue
		}
		data, err := readBounded(docPath, extv2.MaxDocBytes+1)
		if err != nil {
			set.problems = append(set.problems, fmt.Sprintf("%s: %v", docPath, err))
			continue
		}
		pkg, err := extv2.Parse(data)
		if err == nil {
			err = pkg.Validate(ns.options())
		}
		if err == nil && pkg.ID != id {
			err = fmt.Errorf("the package id is %q but it is saved as %q; name the file or folder after the id", pkg.ID, id)
		}
		if err != nil {
			set.problems = append(set.problems, fmt.Sprintf("%s: %v", docPath, err))
			continue
		}
		hash, err := packageHash(pkg, folder)
		if err != nil {
			set.problems = append(set.problems, fmt.Sprintf("%s: %v", full, err))
			continue
		}
		set.pkgs = append(set.pkgs, installedPackage{
			Package: pkg, Scope: scope, Path: docPath, Dir: folder, Hash: hash,
			Granted: grants[grantKey(scope, cwd, pkg.ID)] == hash,
		})
	}
	return set
}

// readBounded reads a regular file of at most max bytes; one more is read so a file
// over the limit is seen as over it.
func readBounded(path string, max int64) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("not a regular file")
	}
	return io.ReadAll(io.LimitReader(f, max))
}

// mergePackages lays the project's packages after the user's. A name or id that is
// already taken is refused for the later one, so what the user has wins over what a
// folder ships, and the refusal says so.
func mergePackages(user, proj packageSet) packageSet {
	out := packageSet{problems: append(append([]string(nil), user.problems...), proj.problems...)}
	ids := map[string]string{}
	cmds := map[string]string{}
	add := func(p installedPackage) {
		if other, ok := ids[p.ID]; ok {
			out.problems = append(out.problems, fmt.Sprintf("%s: package id %q is already used by %s, so this one was not loaded; rename its id", p.Path, p.ID, other))
			return
		}
		for _, c := range p.Commands {
			if other, ok := cmds[c.Name]; ok {
				out.problems = append(out.problems, fmt.Sprintf("%s: /%s is already a command of package %q, so this package was not loaded; rename the command", p.Path, c.Name, other))
				return
			}
		}
		ids[p.ID] = p.Path
		for _, c := range p.Commands {
			cmds[c.Name] = p.ID
		}
		out.pkgs = append(out.pkgs, p)
	}
	for _, p := range user.pkgs {
		add(p)
	}
	for _, p := range proj.pkgs {
		add(p)
	}
	return out
}

// runnablePackages lists the packages that may run, as the engine wants them.
func (s packageSet) runnablePackages() []*extv2.Package {
	var out []*extv2.Package
	for _, p := range s.pkgs {
		if p.runnable() {
			out = append(out, p.Package)
		}
	}
	return out
}

// find returns the package with that id.
func (s packageSet) find(id string) (installedPackage, bool) {
	for _, p := range s.pkgs {
		if p.ID == id {
			return p, true
		}
	}
	return installedPackage{}, false
}

// commandOwner finds the package and command that answer to /name, only among the
// packages that may run.
func (s packageSet) commandOwner(name string) (installedPackage, extv2.Command, bool) {
	for _, p := range s.pkgs {
		if !p.runnable() {
			continue
		}
		for _, c := range p.Commands {
			if c.Name == name && !c.Disabled {
				return p, c, true
			}
		}
	}
	return installedPackage{}, extv2.Command{}, false
}

// grantPackage records the user's consent to this exact version of the package.
func grantPackage(p installedPackage, cwd string) error {
	m := readGrants()
	m[grantKey(p.Scope, cwd, p.ID)] = p.Hash
	return writeGrants(m)
}

// revokePackage withdraws consent. It reports whether there was any.
func revokePackage(scope, cwd, id string) (bool, error) {
	m := readGrants()
	k := grantKey(scope, cwd, id)
	if _, ok := m[k]; !ok {
		return false, nil
	}
	delete(m, k)
	return true, writeGrants(m)
}

// savePackage writes the package where it came from, or as a single document in dir when
// it is new. The document is validated by the caller; this only keeps the bytes.
func savePackage(dir string, p *extv2.Package, existing *installedPackage) (string, error) {
	if dir == "" {
		return "", errNoConfigDir
	}
	path := filepath.Join(dir, p.ID+".json")
	if existing != nil {
		path = existing.Path
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return "", err
	}
	return path, writeAtomic(path, p.Encode())
}

// removePackage deletes a package's document, and its folder when it has one. The
// grant goes with it: an id used again later is a new question.
func removePackage(p installedPackage, cwd string) error {
	target := p.Path
	if p.Dir != "" {
		target = p.Dir
	}
	if err := os.RemoveAll(target); err != nil {
		return err
	}
	_, err := revokePackage(p.Scope, cwd, p.ID)
	return err
}
