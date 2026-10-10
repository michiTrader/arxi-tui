package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/michiTrader/arxi_tui/internal/theme"
)

// This file is the per-project layer of the settings: <project>/.arxi/.
//
// # What a project may carry
//
// Three of the user's files, with the same names and the same formats as in ~/.arxi:
// theme.json (colours), texts.json (wording) and behaviour.json (animations, menu keys,
// shortcuts, commands, hooks). They are laid over the user's own, so a repository can
// ship the look and the shortcuts its contributors share.
//
// # What a project may NOT carry
//
// API keys, providers and the search key stay in ~/.arxi: a key must never sit in a
// folder that a `git add .` reaches, and the core refuses a key in a provider record for
// the same reason. Nothing else is read from .arxi/, so a file dropped there is inert.
//
// # Consent
//
// behaviour.json can bind a key or a hook to a command, and a folder is somebody else's
// text the moment it is cloned. So the layer is NOT loaded until the user says
// `/project trust`. The consent is stored in ~/.arxi/trusted-projects.json as folder ->
// a hash of the three files, so a file that changes afterwards asks again. `/project
// forget` withdraws it. -raw and -scene never load the layer, like the user's own.
//
// The layer is never written back: saveInterface keeps the user's layer only, so a
// project colour cannot leak into ~/.arxi by being saved.

const (
	projectDirName = ".arxi"
	trustFileName  = "trusted-projects.json"
)

// projectFiles are the only files read from <project>/.arxi.
var projectFiles = []string{"theme.json", "texts.json", "behaviour.json"}

// projectLayer is what a trusted folder contributes.
type projectLayer struct {
	dir   string
	cols  userTokens
	words userTexts
	beh   behaviour
}

func (p projectLayer) empty() bool {
	return len(p.cols) == 0 && len(p.words) == 0 && p.beh.empty()
}

// projectSettingsDir is <cwd>/.arxi when it exists and is not the user's own folder (a
// shell started in the home directory would otherwise "trust" ~/.arxi as a project).
func projectSettingsDir(cwd string) string {
	if cwd == "" {
		return ""
	}
	dir := filepath.Join(cwd, projectDirName)
	if u := configDir(); u != "" && sameFolder(dir, u) {
		return ""
	}
	if h := homeConfigDir(); h != "" && sameFolder(dir, h) {
		return ""
	}
	if info, err := os.Stat(dir); err != nil || !info.IsDir() {
		return ""
	}
	return dir
}

func sameFolder(a, b string) bool {
	if ra, err := filepath.EvalSymlinks(a); err == nil {
		a = ra
	}
	if rb, err := filepath.EvalSymlinks(b); err == nil {
		b = rb
	}
	return filepath.Clean(a) == filepath.Clean(b)
}

// projectHash is the fingerprint of what the folder would load: the name and the bytes
// of each of the three files that exist. Empty when none does.
func projectHash(dir string) string {
	h := sha256.New()
	any := false
	for _, name := range projectFiles {
		b, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			continue
		}
		any = true
		fmt.Fprintf(h, "%s\x00%d\x00", name, len(b))
		h.Write(b)
	}
	if !any {
		return ""
	}
	return hex.EncodeToString(h.Sum(nil))
}

func trustFilePath() string {
	d := configDir()
	if d == "" {
		return ""
	}
	return filepath.Join(d, trustFileName)
}

// trustKey is how a folder is named in the consent file.
func trustKey(cwd string) string {
	abs, err := filepath.Abs(cwd)
	if err != nil {
		abs = cwd
	}
	if r, err := filepath.EvalSymlinks(abs); err == nil {
		abs = r
	}
	return abs
}

func readTrust() map[string]string {
	out := map[string]string{}
	p := trustFilePath()
	if p == "" {
		return out
	}
	if b, err := os.ReadFile(p); err == nil {
		_ = json.Unmarshal(b, &out)
	}
	return out
}

func writeTrust(m map[string]string) error {
	p := trustFilePath()
	if p == "" {
		return errNoConfigDir
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		return err
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	ordered := make([]string, 0, len(keys))
	for _, k := range keys {
		kb, _ := json.Marshal(k)
		vb, _ := json.Marshal(m[k])
		ordered = append(ordered, "  "+string(kb)+": "+string(vb))
	}
	body := "{\n" + strings.Join(ordered, ",\n") + "\n}\n"
	if len(keys) == 0 {
		body = "{}\n"
	}
	return writeAtomic(p, []byte(body))
}

// projectState says where a folder stands: no settings, settings waiting for consent,
// consent given and still valid, or consent given for a different version of the files.
type projectState int

const (
	projectNone projectState = iota
	projectAsk
	projectTrusted
	projectChanged
)

// projectStatus classifies cwd.
func projectStatus(cwd string) (projectState, string) {
	dir := projectSettingsDir(cwd)
	if dir == "" {
		return projectNone, ""
	}
	h := projectHash(dir)
	if h == "" {
		return projectNone, dir
	}
	have, ok := readTrust()[trustKey(cwd)]
	switch {
	case !ok:
		return projectAsk, dir
	case have == h:
		return projectTrusted, dir
	}
	return projectChanged, dir
}

// loadProjectLayer reads the three files of a folder whose consent is valid. A file that
// is damaged, or a layer that does not validate on top of the user's own, is refused as a
// whole with the reason: half a project look would be worse than none.
func loadProjectLayer(cwd string, user behaviour) (projectLayer, error) {
	st, dir := projectStatus(cwd)
	if st != projectTrusted {
		return projectLayer{}, nil
	}
	p := projectLayer{dir: dir}
	read := func(name string) ([]byte, bool, error) {
		b, err := os.ReadFile(filepath.Join(dir, name))
		if os.IsNotExist(err) {
			return nil, false, nil
		}
		return b, err == nil, err
	}
	if b, ok, err := read("theme.json"); err != nil {
		return projectLayer{}, err
	} else if ok {
		u, err := parseUserTokens(filepath.Join(dir, "theme.json"), b)
		if err != nil {
			return projectLayer{}, err
		}
		p.cols = u
	}
	if b, ok, err := read("texts.json"); err != nil {
		return projectLayer{}, err
	} else if ok {
		tx, err := parseUserTexts(filepath.Join(dir, "texts.json"), b)
		if err != nil {
			return projectLayer{}, err
		}
		p.words = tx
	}
	if b, ok, err := read("behaviour.json"); err != nil {
		return projectLayer{}, err
	} else if ok {
		bh, err := parseUserBehaviour(filepath.Join(dir, "behaviour.json"), b)
		if err != nil {
			return projectLayer{}, err
		}
		p.beh = bh
	}
	if err := mergeBehaviour(user, p.beh).validate(); err != nil {
		return projectLayer{}, fmt.Errorf("%s: together with your own behaviour: %w", dir, err)
	}
	return p, nil
}

// trustProject records consent for the files as they are now.
func trustProject(cwd string) (string, error) {
	st, dir := projectStatus(cwd)
	if st == projectNone {
		return "", fmt.Errorf("/project: there is no %s folder with settings here", projectDirName)
	}
	m := readTrust()
	m[trustKey(cwd)] = projectHash(dir)
	if err := writeTrust(m); err != nil {
		return "", err
	}
	return dir, nil
}

// forgetProject withdraws consent. It reports whether there was any.
func forgetProject(cwd string) (bool, error) {
	m := readTrust()
	k := trustKey(cwd)
	if _, ok := m[k]; !ok {
		return false, nil
	}
	delete(m, k)
	return true, writeTrust(m)
}

// ---- merging --------------------------------------------------------------------

// mergeTokens lays the project's colours over the user's.
func mergeTokens(user, proj userTokens) userTokens {
	if len(proj) == 0 {
		return user
	}
	out := make(userTokens, len(user)+len(proj))
	for k, v := range user {
		out[k] = v
	}
	for k, v := range proj {
		out[k] = v
	}
	return out
}

func mergeTexts(user, proj userTexts) userTexts {
	if len(proj) == 0 {
		return user
	}
	out := user.clone()
	for k, v := range proj {
		out[k] = v
	}
	return out
}

// mergeBehaviour lays the project's behaviour over the user's: the maps entry by entry,
// the project winning; commands by name; hooks appended after the user's own.
func mergeBehaviour(user, proj behaviour) behaviour {
	if proj.empty() {
		return user
	}
	out := user.clone()
	for k, v := range proj.Animations {
		if out.Animations == nil {
			out.Animations = map[string]theme.CycleDef{}
		}
		out.Animations[k] = v
	}
	for k, v := range proj.MenuKeys {
		if out.MenuKeys == nil {
			out.MenuKeys = map[string][]string{}
		}
		out.MenuKeys[k] = v
	}
	for k, v := range proj.Keys {
		if out.Keys == nil {
			out.Keys = map[string]actionList{}
		}
		out.Keys[k] = v
	}
	for _, c := range proj.Commands {
		replaced := false
		for i := range out.Commands {
			if out.Commands[i].Name == c.Name {
				out.Commands[i], replaced = c, true
			}
		}
		if !replaced {
			out.Commands = append(out.Commands, c)
		}
	}
	out.Hooks = append(out.Hooks, proj.Hooks...)
	return out
}

// projectSummary is what /project status says.
func projectSummary(cwd string, loaded projectLayer) string {
	st, dir := projectStatus(cwd)
	switch st {
	case projectNone:
		return "/project: no " + projectDirName + " settings in this folder"
	case projectAsk:
		return "/project: " + dir + " has settings (" + projectParts(dir) + ") that are not loaded; /project trust to load them"
	case projectChanged:
		return "/project: the files in " + dir + " changed since you trusted them, so they are not loaded; /project trust to load the new ones"
	}
	if loaded.dir == "" {
		return "/project: trusted, loads on the next start"
	}
	return "/project: loaded from " + dir + " (" + projectParts(dir) + "); /project forget stops loading it"
}

func projectParts(dir string) string {
	var have []string
	for _, n := range projectFiles {
		if _, err := os.Stat(filepath.Join(dir, n)); err == nil {
			have = append(have, strings.TrimSuffix(n, ".json"))
		}
	}
	return strings.Join(have, ", ")
}

// projectLine reads what Enter means on the input line: a bare `/project` (typed whole
// or picked in the / menu) is the status, and `/project trust|forget|status` are typed.
// Anything else after /project is a usage notice, never a silent no-op.
func projectLine(input string, sel int, cat string) (verb string, ok bool) {
	if menuCommand("project", input, sel, cat) {
		return "status", true
	}
	body := strings.TrimSpace(input)
	if !strings.HasPrefix(body, "/project ") {
		return "", false
	}
	switch rest := strings.TrimSpace(strings.TrimPrefix(body, "/project")); rest {
	case "status", "trust", "forget":
		return rest, true
	}
	return "usage", true
}
