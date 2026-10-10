package main

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// This file is where the host's settings live: ~/.arxi.
//
// Before, they were in <user config dir>/arxi (~/.config/arxi on Linux, %AppData%\arxi
// on Windows, ~/Library/Application Support/arxi on macOS). One dot-folder in the home
// is the same on every system, is easy to find and to back up, and sits beside the
// keys and providers the core keeps (~/.arxi/secrets, ~/.arxi/providers).
//
// # Moving without losing anything
//
// The first time the new folder is used, what the old one holds is COPIED in. The old
// folder is never deleted or modified: a build that still reads it keeps working, and
// removing a person's settings is not this program's call. A marker file records that
// the copy happened, so a setting the user later deletes does not come back from the old
// folder. An environment override (ARXI_CONFIG_DIR) means the user chose the place, so
// nothing is imported then.

// legacyMarker is the file in the new folder that says the old one was already read.
const legacyMarker = ".legacy-imported"

// homeConfigDir is ~/.arxi, "" when the system has no home directory.
func homeConfigDir() string {
	home, err := os.UserHomeDir()
	if err != nil || strings.TrimSpace(home) == "" {
		return ""
	}
	return filepath.Join(home, ".arxi")
}

// legacyConfigDir is where the settings lived before ~/.arxi.
func legacyConfigDir() string {
	base, err := os.UserConfigDir()
	if err != nil || strings.TrimSpace(base) == "" {
		return ""
	}
	return filepath.Join(base, "arxi")
}

var (
	importOnce sync.Map // new folder -> *sync.Once
)

// importLegacyConfig copies the old folder's settings into dir, once per process and
// once per folder in total (the marker). Anything already in dir wins. Errors are
// swallowed on purpose: a failed import leaves the user with factory settings and the
// old folder intact, and a convenience must never stop the program from starting.
func importLegacyConfig(dir, old string) {
	if dir == "" || old == "" || filepath.Clean(dir) == filepath.Clean(old) {
		return
	}
	once, _ := importOnce.LoadOrStore(dir+"\x00"+old, new(sync.Once))
	once.(*sync.Once).Do(func() {
		if _, err := os.Stat(filepath.Join(dir, legacyMarker)); err == nil {
			return
		}
		info, err := os.Stat(old)
		if err != nil || !info.IsDir() {
			return
		}
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return
		}
		copyTree(old, dir)
		_ = os.WriteFile(filepath.Join(dir, legacyMarker), []byte("old folder: "+old+"\n"), 0o600)
	})
}

// copyTree copies the regular files under from into to without overwriting, in private
// files and folders. Symbolic links are skipped, so a link in the old folder cannot make
// the copy read something elsewhere.
func copyTree(from, to string) {
	entries, err := os.ReadDir(from)
	if err != nil {
		return
	}
	for _, e := range entries {
		src, dst := filepath.Join(from, e.Name()), filepath.Join(to, e.Name())
		switch {
		case e.Type()&os.ModeSymlink != 0:
			continue
		case e.IsDir():
			if err := os.MkdirAll(dst, 0o700); err != nil {
				continue
			}
			copyTree(src, dst)
		case e.Type().IsRegular():
			if _, err := os.Stat(dst); err == nil {
				continue
			}
			if b, err := os.ReadFile(src); err == nil {
				_ = os.WriteFile(dst, b, 0o600)
			}
		}
	}
}
