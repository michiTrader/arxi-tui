package main

import (
	"bytes"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// The arxi core lives in this repository (core/), so the TUI must not ask the user
// to find it, build it and point an environment variable at it. coreBinary and
// prepareCore say where it is and keep it current:
//
//  1. ARXI_BIN, when set: an explicit choice is never second-guessed, and
//     checkArxiBin reports a bad one.
//  2. a built core beside this program, in a bin/ or core/ folder next to it, or in
//     the same places one folder above (a checkout built in place), or at
//     the root of the checkout whose core/ source is found.
//  3. when the core's source (core/go.mod of module github.com/michiTrader/arxi) is
//     found the same way and a Go toolchain is on PATH, the core is built (or rebuilt
//     when the source is newer than the binary) at the checkout root. A stale core is
//     the commonest reason the model "cannot change the interface": it was built
//     before that existed, and nothing told the user to rebuild.
//
// An empty result means "no core": the caller falls back to the offline demo.

const coreModule = "module github.com/michiTrader/arxi\n"

func coreFileName() string {
	if runtime.GOOS == "windows" {
		return "arxi.exe"
	}
	return "arxi"
}

// parents lists dir and its parent: a program in bin/ or dist/ of a checkout reaches
// the checkout root, and nothing further, so a stray file in a distant folder is
// never taken for the core.
func parents(dir string) []string {
	var out []string
	for i := 0; i < 2 && dir != "" && dir != "."; i++ {
		out = append(out, dir)
		up := filepath.Dir(dir)
		if up == dir {
			break
		}
		dir = up
	}
	return out
}

// programRoots are the program's own folder and its parent: where a checkout built
// in place keeps arxi-tui, with core/ beside or below it.
func programRoots(executable func() (string, error)) []string {
	if exe, err := executable(); err == nil && exe != "" {
		return parents(filepath.Dir(exe))
	}
	return nil
}

func isFile(p string) bool {
	info, err := os.Stat(p)
	return err == nil && !info.IsDir()
}

// coreBinary returns the built core to launch, or "". Only the program's own
// surroundings and a verified core/ checkout are searched: a file called "arxi" in
// the user's project is not trusted to be the core.
func coreBinary(env string, executable func() (string, error)) string {
	if env != "" {
		return env
	}
	name := coreFileName()
	if exe, err := executable(); err == nil && exe != "" {
		if c := filepath.Join(filepath.Dir(exe), name); isFile(c) {
			return c
		}
	}
	for _, root := range programRoots(executable) {
		for _, c := range []string{
			filepath.Join(root, name),
			filepath.Join(root, "core", name),
			filepath.Join(root, "bin", name),
		} {
			if isFile(c) {
				return c
			}
		}
	}
	if src := coreSource(executable); src != "" {
		if c := filepath.Join(filepath.Dir(src), name); isFile(c) {
			return c
		}
	}
	return ""
}

// coreSource returns the core's source folder (core/ of this repository), or "". Only
// the program's own surroundings are searched, never the working folder: building
// code that happens to sit in the user's project would run it.
func coreSource(executable func() (string, error)) string {
	for _, root := range programRoots(executable) {
		dir := filepath.Join(root, "core")
		mod, err := os.ReadFile(filepath.Join(dir, "go.mod"))
		if err != nil || !strings.HasPrefix(string(mod), coreModule) {
			continue
		}
		if info, err := os.Stat(filepath.Join(dir, "cmd", "arxi")); err == nil && info.IsDir() {
			return dir
		}
	}
	return ""
}

// newestSource is the modification time of the newest .go or go.mod file under dir.
func newestSource(dir string) time.Time {
	var newest time.Time
	_ = filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if n := d.Name(); n == "testdata" || n == ".git" || n == "node_modules" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(p, ".go") && d.Name() != "go.mod" {
			return nil
		}
		if strings.HasSuffix(p, "_test.go") {
			return nil
		}
		if info, err := d.Info(); err == nil && info.ModTime().After(newest) {
			newest = info.ModTime()
		}
		return nil
	})
	return newest
}

// coreBuilder compiles the core source into out. Replaceable in tests.
var coreBuilder = func(srcDir, out string) error {
	goBin, err := exec.LookPath("go")
	if err != nil {
		return fmt.Errorf("no Go toolchain on PATH")
	}
	cmd := exec.Command(goBin, "build", "-o", out, "./cmd/arxi")
	cmd.Dir = srcDir
	var buf bytes.Buffer
	cmd.Stderr, cmd.Stdout = &buf, &buf
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(buf.String())
		if len(msg) > 600 {
			msg = msg[:600] + "..."
		}
		return fmt.Errorf("%v: %s", err, msg)
	}
	return nil
}

// prepareCore is coreBinary plus the build: it finds the core, and when the source is
// at hand and the binary is missing or older than it, builds a fresh one at the checkout
// root. note receives one line for each thing it does or cannot do, so the user
// sees "building the core" instead of a silent pause, and a failed build is a
// sentence with its remedy, never a crash: the old binary (or the offline demo)
// still runs.
func prepareCore(env string, executable func() (string, error), note func(string)) string {
	if env != "" {
		return env
	}
	bin := coreBinary("", executable)
	src := coreSource(executable)
	if src == "" {
		return bin
	}
	if bin != "" {
		if info, err := os.Stat(bin); err == nil && !newestSource(src).After(info.ModTime()) {
			return bin
		}
	}
	out := bin
	if out == "" {
		out = filepath.Join(filepath.Dir(src), coreFileName())
	}
	if bin == "" {
		note("building the arxi core from " + src + " (first run only, a few seconds)...")
	} else {
		note("the arxi core at " + bin + " is older than its source; rebuilding it...")
	}
	tmp := out + ".new"
	if err := coreBuilder(src, tmp); err != nil {
		_ = os.Remove(tmp)
		if bin != "" {
			note("could not rebuild the arxi core (" + err.Error() + "); using the older one, which may lack newer features. " +
				"Install Go, or run: cd core && go build -o ../" + coreFileName() + " ./cmd/arxi")
			return bin
		}
		note("could not build the arxi core (" + err.Error() + "); running without it. " +
			"Install Go from https://go.dev/dl and start again, or download a release that includes the arxi core")
		return ""
	}
	if err := os.Rename(tmp, out); err != nil {
		// A running Windows program cannot be replaced; the new build still works from its temporary name.
		return tmp
	}
	return out
}
