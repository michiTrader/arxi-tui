package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// This file is /folder: the folders beside the project that the model may use.
//
// The file tools stay inside the project on purpose (what they read goes to a remote
// provider). Work often reaches past it, though: a script here imports a library kept
// next to it. Instead of a setting to find, the model asks. The first time it names a
// path outside, the user is shown the folder (the nearest project above that path) and
// agrees or not, once for the conversation; in `full access` it is opened without
// asking. /folder is the same thing done by hand, ahead of time, and the way to see or
// withdraw what was opened:
//
//	/folder                  list the folders open
//	/folder add <path>       open one now (relative paths start at the project)
//	/folder remove <path>    close one;  /folder remove all  closes every one
//
// What was opened belongs to the conversation: /clear closes it. The core keeps
// nothing between turns, so the list rides with each question (chat.go).

// folderLine reports whether Enter on this line is `/folder`, with what follows it.
func folderLine(input string, sel int, cat string) (args string, ok bool) {
	if menuCommand("folder", input, sel, cat) {
		return "", true
	}
	if rest, matched := stripLeadingVerb(strings.TrimSpace(input), "folder"); matched && strings.HasPrefix(strings.TrimSpace(input), "/") {
		return rest, true
	}
	return "", false
}

// folderManager is the optional capability a Driver has when it owns a conversation
// whose model may be given folders (serveDriver). The mock has no tools.
type folderManager interface {
	OpenFolder(dir string) (opened string, err error)
	CloseFolder(dir string) int
	Folders() []string
}

// samePath compares two folder paths the way the machine does: Windows ignores case.
func samePath(a, b string) bool {
	a, b = filepath.Clean(a), filepath.Clean(b)
	if runtime.GOOS == "windows" {
		return strings.EqualFold(a, b)
	}
	return a == b
}

// onVolume completes a Windows path that starts at the root of a drive but names none
// (`\` or `\Users\x`): filepath.IsAbs says no to it, so it would be joined under the
// base folder and `/folder add \` would open the base instead of being refused as the
// whole drive. It means the drive the base is on. Elsewhere, and for every other path,
// dir is returned as it came.
func onVolume(goos, base, dir string) string {
	if goos != "windows" || dir == "" || (dir[0] != '\\' && dir[0] != '/') {
		return dir
	}
	if len(dir) > 1 && (dir[1] == '\\' || dir[1] == '/') {
		return dir // \\server\share: already complete
	}
	if len(base) >= 2 && base[1] == ':' {
		return base[:2] + dir
	}
	return dir
}

// checkFolder resolves dir against base and says whether it may be opened: it has to be
// a folder that exists, and not one so wide that opening it says nothing about the work
// (a drive or filesystem root, the home folder). The core applies the same rule again.
func checkFolder(base, dir string) (string, error) {
	dir = strings.TrimSpace(strings.Trim(strings.TrimSpace(dir), `"'`))
	if dir == "" {
		return "", errors.New("name the folder: /folder add <path>")
	}
	if dir == "~" || strings.HasPrefix(dir, "~/") || strings.HasPrefix(dir, `~\`) {
		if home, err := os.UserHomeDir(); err == nil {
			dir = filepath.Join(home, dir[1:])
		}
	}
	dir = onVolume(runtime.GOOS, base, dir)
	if !filepath.IsAbs(dir) {
		dir = filepath.Join(base, dir)
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return "", err
	}
	real, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return "", fmt.Errorf("%s does not exist", abs)
	}
	st, err := os.Stat(real)
	if err != nil || !st.IsDir() {
		return "", fmt.Errorf("%s is not a folder", real)
	}
	if filepath.Dir(real) == real {
		return "", fmt.Errorf("%s is too wide to open as a whole; name the folder the work is in", real)
	}
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		if h, err := filepath.EvalSymlinks(home); err == nil {
			home = h
		}
		if samePath(home, real) {
			return "", fmt.Errorf("%s is too wide to open as a whole; name the folder the work is in", real)
		}
	}
	return real, nil
}

// runFolderCommand carries out `/folder ...` and returns the line to show.
func runFolderCommand(args string, fm folderManager, base string) string {
	verb, rest := args, ""
	if i := strings.IndexAny(args, " \t"); i >= 0 {
		verb, rest = args[:i], strings.TrimSpace(args[i+1:])
	}
	switch strings.ToLower(verb) {
	case "":
		return folderList(fm.Folders())
	case "add", "open":
		real, err := checkFolder(base, rest)
		if err != nil {
			return "/folder: " + err.Error()
		}
		opened, err := fm.OpenFolder(real)
		if err != nil {
			return "/folder: " + err.Error()
		}
		return "/folder: the model may now use " + opened + " (/clear closes it)"
	case "remove", "close", "rm":
		if strings.EqualFold(rest, "all") || rest == "" {
			if n := fm.CloseFolder(""); n > 0 {
				return fmt.Sprintf("/folder: closed %d folder(s)", n)
			}
			return "/folder: no folder was open"
		}
		real, err := checkFolder(base, rest)
		if err != nil {
			// A folder that is gone can still be withdrawn by the words it was given.
			real = rest
		}
		if fm.CloseFolder(real) == 0 {
			return "/folder: " + rest + " was not open"
		}
		return "/folder: closed " + real
	}
	return "/folder: unknown \"" + verb + "\"; use /folder, /folder add <path> or /folder remove <path|all>"
}

func folderList(dirs []string) string {
	if len(dirs) == 0 {
		return "/folder: only the project is open; the model asks when it needs another, or /folder add <path>"
	}
	return "/folder: open besides the project: " + strings.Join(dirs, ", ")
}

// OpenFolder, CloseFolder and Folders make serveDriver a folderManager.
func (d *serveDriver) OpenFolder(dir string) (string, error) {
	if d.chat == nil {
		return "", errors.New("there is no conversation to open it in")
	}
	d.chat.openFolder(dir)
	return dir, nil
}

func (d *serveDriver) CloseFolder(dir string) int {
	if d.chat == nil {
		return 0
	}
	return d.chat.closeFolder(dir)
}

func (d *serveDriver) Folders() []string {
	if d.chat == nil {
		return nil
	}
	return d.chat.openFolders()
}
