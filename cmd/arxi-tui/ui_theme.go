package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/michiTrader/arxi_tui/internal/defaultscene"
	"github.com/michiTrader/arxi_tui/internal/scene"
	"github.com/michiTrader/arxi_tui/internal/theme"
	"github.com/michiTrader/arxi_tui/internal/ui"
)

// This file is the colour half of the user's interface. The scene document says what
// is drawn and where; the theme says what each style token looks like. Colours live in
// the theme, not in the document, so an interface that could only be changed through
// the document could never be recoloured, which is what a user asking to "change the
// blue words to purple" ran into: the model was handed a document that held no colour.
//
// The user's colours are a token layer over everything else, the `user > plugin >
// factory` precedence docs/TOKENS.md signs, kept as theme.json in the settings folder
// in the same {"token": {"fg","bg","attrs"}} shape any theme file uses.

// userTokens is the user's own token layer: token name -> style. Empty means the
// factory look.
type userTokens map[string]ui.Style

func (u userTokens) clone() userTokens {
	out := make(userTokens, len(u))
	for k, v := range u {
		out[k] = v
	}
	return out
}

// withUserTokens lays the user's colours over a composed theme. Recomposing from the
// layers each time (rather than patching the active theme) is what makes a reset
// exact: a token the user stops overriding falls back to whatever is below it.
func withUserTokens(base *theme.Theme, u userTokens) *theme.Theme {
	if len(u) == 0 {
		return base
	}
	return theme.Merge(base, theme.FromMap(u))
}

// userThemePath is where the user's colours are kept, "" when there is nowhere.
func userThemePath() string {
	d := configDir()
	if d == "" {
		return ""
	}
	return filepath.Join(d, "theme.json")
}

// loadUserTokens reads the user's colours. A missing file is the factory look; a
// damaged one is reported and ignored, so one bad edit cannot cost the interface.
func loadUserTokens() (userTokens, error) {
	path := userThemePath()
	if path == "" {
		return nil, nil
	}
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return parseUserTokens(path, data)
}

func parseUserTokens(name string, data []byte) (userTokens, error) {
	if len(strings.TrimSpace(string(data))) == 0 {
		return nil, nil
	}
	t, err := theme.LoadBytes(name, data)
	if err != nil {
		return nil, err
	}
	out := userTokens{}
	for _, tok := range t.Tokens() {
		out[tok] = t.Resolve(tok)
	}
	return out, nil
}

// encodeUserTokens writes the layer in the theme file shape, sorted so the file a
// person opens reads the same every time.
func encodeUserTokens(u userTokens) []byte {
	names := make([]string, 0, len(u))
	for k := range u {
		names = append(names, k)
	}
	sort.Strings(names)
	var b strings.Builder
	b.WriteString("{\n")
	for i, k := range names {
		def := map[string]any{}
		s := u[k]
		if s.FG.Kind != ui.ColorNone {
			def["fg"] = s.FG.String()
		}
		if s.BG.Kind != ui.ColorNone {
			def["bg"] = s.BG.String()
		}
		var attrs []string
		for _, f := range strings.Fields(s.String()) {
			if !strings.Contains(f, "=") {
				attrs = append(attrs, f)
			}
		}
		if len(attrs) > 0 {
			def["attrs"] = attrs
		}
		kb, _ := json.Marshal(k)
		vb, _ := json.Marshal(def)
		fmt.Fprintf(&b, "  %s: %s", kb, vb)
		if i < len(names)-1 {
			b.WriteString(",")
		}
		b.WriteString("\n")
	}
	b.WriteString("}\n")
	return []byte(b.String())
}

// applyColors returns base with the requested colours applied, refusing what would
// not draw: a token the theme does not have (a typo would change nothing and report
// success) or a value the style grammar does not read. An empty value takes the
// user's colour off that token, back to the factory one.
func applyColors(base userTokens, active *theme.Theme, colors map[string]string) (userTokens, error) {
	out := base.clone()
	names := make([]string, 0, len(colors))
	for k := range colors {
		names = append(names, k)
	}
	sort.Strings(names)
	for _, tok := range names {
		if !active.Has(tok) {
			return nil, fmt.Errorf("color token %q does not exist; the tokens and what each one paints are listed in ui_guide", tok)
		}
		if _, animated := active.Cycle(tok); animated {
			return nil, fmt.Errorf("color token %q is an animation (behaviour.animations); a plain colour would be hidden by it. Remove the animation first, or change its colours there", tok)
		}
		val := strings.TrimSpace(colors[tok])
		if val == "" {
			delete(out, tok)
			continue
		}
		s, err := ui.ParseStyle(val)
		if err != nil {
			return nil, fmt.Errorf("color %q for %s: %w (write e.g. \"fg=magenta\", \"fg=#a855f7 bold\")", val, tok, err)
		}
		out[tok] = s
	}
	return out, nil
}

// colorDiff is a change of colours as the conversation draws a diff: a removed and an
// added line per token, with no line number (a theme has no lines the user knows).
func colorDiff(before, after *theme.Theme, changed []string) string {
	var b strings.Builder
	for _, tok := range changed {
		was, now := styleWords(before.Resolve(tok)), styleWords(after.Resolve(tok))
		if was == now {
			continue
		}
		fmt.Fprintf(&b, "      - %s: %s\n      + %s: %s\n", tok, was, tok, now)
	}
	return b.String()
}

func styleWords(s ui.Style) string {
	if w := s.String(); w != "" {
		return w
	}
	return "(plain)"
}

// tokenRoles says what each factory token paints, so the model can find "the blue
// words in your answers" without guessing from a name. A token missing here is still
// listed with its value; this is help, not the vocabulary.
var tokenRoles = map[string]string{
	"text": "plain text", "dim": "faded text (banner tail, status bar)", "header": "agent mode in the status bar",
	"input": "the text you type", "banner": "banner text", "chat.user": "your own messages",
	"chat.band": "background behind your messages (/style band)", "chat.error": "error lines",
	"chat.warn": "warnings", "chat.usage": "time/token line under each answer",
	"chat.tool": "tool names in tool lines", "chat.tool.dot": "dot before a tool line waiting for approval",
	"chat.tool.dot.ok": "dot before a finished tool line", "chat.tool.dot.fail": "dot before a failed tool line",
	"chat.tool.result": "result line under a tool call", "chat.tool.fail": "failed tool result",
	"chat.approval": "the approval question", "chat.diff.add": "added diff lines", "chat.diff.del": "removed diff lines",
	"chat.diff.ctx": "diff context lines", "markdown.heading": "headings in answers", "markdown.strong": "bold in answers",
	"markdown.emphasis": "italics in answers", "markdown.code": "inline `code` in answers",
	"markdown.code.keyword": "keywords in code blocks", "markdown.code.type": "types in code blocks",
	"markdown.code.func": "function names in code blocks", "markdown.code.string": "strings in code blocks",
	"markdown.code.number": "numbers in code blocks", "markdown.code.comment": "comments in code blocks",
	"markdown.link": "links in answers", "markdown.link.url": "link addresses", "markdown.bullet": "list bullets",
	"markdown.quote": "quoted text", "markdown.table.header": "table headers", "menu.name": "command names in the / menu and the level, mode or model names in /effort, /mode, /style and /model",
	"menu.name.selected": "highlighted command", "menu.hint": "hint line under the menu",
	"menu.desc":          "descriptions in the / menu and the meaning beside each row of /effort, /mode, /style and /model",
	"menu.desc.selected": "the description of the highlighted row", "menu.tab": "category tabs and the match count of the / menu",
	"menu.tab.selected": "the active category tab", "menu.rule": "the lines that frame the / menu",
	"brand.1": "the Δ of the logo", "brand.2": "the r of the logo", "brand.3": "the × of the logo", "brand.4": "the i of the logo",
}

// colorTable lists every token with what it looks like now and what it paints.
func colorTable(active *theme.Theme) string {
	toks := active.Tokens()
	sort.Strings(toks)
	var b strings.Builder
	for _, tok := range toks {
		fmt.Fprintf(&b, "  %s = %s", tok, styleWords(active.Resolve(tok)))
		if r := tokenRoles[tok]; r != "" {
			b.WriteString("  # " + r)
		}
		b.WriteString("\n")
	}
	return b.String()
}

// uiColorCommand reads `/ui color <token> [style…]`: with a style it sets the
// token's colour, with none it takes the user's colour off.
func uiColorCommand(input string) (tok, style string, ok bool) {
	f := strings.Fields(input)
	if len(f) < 3 || f[0] != "/ui" || f[1] != "color" {
		return "", "", false
	}
	return f[2], strings.Join(f[3:], " "), true
}

// ---- the saved interface: document and colours, kept and undone together -------

// interfaceState is everything the user has made their own. Scene is nil when the
// document is the built-in one, so a user who only changed colours keeps receiving
// layout improvements with each release.
//
// The parts are strings, not raw JSON: a raw part that is absent would be written as
// null and read back as the four bytes "null", a document with no root, which is how
// the first undo after the first change came back as an empty screen.
type interfaceState struct {
	Scene  string `json:"scene,omitempty"`
	Tokens string `json:"tokens,omitempty"`
	// Texts is the user's own wording (ui_texts.go). It travels with the layout and the
	// colours so one /ui undo can never bring back the layout of one change with the
	// words of another.
	Texts string `json:"texts,omitempty"`
	// Behaviour is what the interface does (behaviour.go): animations, menu keys,
	// shortcuts, commands, hooks. It travels with the rest for the same reason.
	Behaviour string `json:"behaviour,omitempty"`
}

func previousInterfacePath() string {
	d := configDir()
	if d == "" {
		return ""
	}
	return filepath.Join(d, "interface.prev.json")
}

// currentInterface reads what is saved now.
func currentInterface() (interfaceState, error) {
	var st interfaceState
	if b, err := os.ReadFile(userScenePath()); err == nil {
		st.Scene = string(b)
	} else if !os.IsNotExist(err) {
		return st, err
	}
	if b, err := os.ReadFile(userThemePath()); err == nil {
		st.Tokens = string(b)
	} else if !os.IsNotExist(err) {
		return st, err
	}
	if b, err := os.ReadFile(userTextsPath()); err == nil {
		st.Texts = string(b)
	} else if !os.IsNotExist(err) {
		return st, err
	}
	if b, err := os.ReadFile(userBehaviourPath()); err == nil {
		st.Behaviour = string(b)
	} else if !os.IsNotExist(err) {
		return st, err
	}
	return st, nil
}

// writeInterface makes st the saved interface: a nil part is removed.
func writeInterface(st interfaceState) error {
	if err := os.MkdirAll(configDir(), 0o700); err != nil {
		return err
	}
	for _, part := range []struct {
		path string
		data []byte
	}{{userScenePath(), []byte(st.Scene)}, {userThemePath(), []byte(st.Tokens)}, {userTextsPath(), []byte(st.Texts)}, {userBehaviourPath(), []byte(st.Behaviour)}} {
		if len(part.data) == 0 {
			if err := os.Remove(part.path); err != nil && !os.IsNotExist(err) {
				return err
			}
			continue
		}
		if err := writeAtomic(part.path, part.data); err != nil {
			return err
		}
	}
	return nil
}

// saveInterface keeps doc and the user's colours, and what was saved before as the
// one /ui undo returns to. The previous state is one file, so an undo can never bring
// back the layout of one change with the colours of another.
func saveInterface(doc *scene.Document, u userTokens, tx userTexts, bh behaviour) error {
	if configDir() == "" {
		return errNoConfigDir
	}
	prev, err := currentInterface()
	if err != nil {
		return err
	}
	pb, _ := json.Marshal(prev)
	if err := os.MkdirAll(configDir(), 0o700); err != nil {
		return err
	}
	if err := writeAtomic(previousInterfacePath(), pb); err != nil {
		return err
	}
	var next interfaceState
	if !sameDocument(doc.Source(), defaultscene.JSON) {
		next.Scene = string(doc.Source())
	}
	if len(u) > 0 {
		next.Tokens = string(encodeUserTokens(u))
	}
	if len(tx) > 0 {
		next.Texts = string(encodeUserTexts(tx))
	}
	if !bh.empty() {
		next.Behaviour = string(encodeBehaviour(bh))
	}
	return writeInterface(next)
}

func sameDocument(a, b []byte) bool {
	d, err := uiDiffText(a, b)
	return err == nil && d == ""
}

// loadInterface turns a saved state into what the loop draws.
func loadInterface(st interfaceState) (*scene.Document, userTokens, userTexts, behaviour, error) {
	var doc *scene.Document
	var err error
	if len(st.Scene) == 0 {
		doc, err = scene.ParseNamed(defaultscene.Name, defaultscene.JSON)
	} else {
		doc, err = scene.ParseNamed(userScenePath(), []byte(st.Scene))
		if err == nil {
			err = doc.Validate()
		}
	}
	if err != nil {
		return nil, nil, nil, behaviour{}, err
	}
	u, err := parseUserTokens(userThemePath(), []byte(st.Tokens))
	if err != nil {
		return nil, nil, nil, behaviour{}, err
	}
	tx, err := parseUserTexts(userTextsPath(), []byte(st.Texts))
	if err != nil {
		return nil, nil, nil, behaviour{}, err
	}
	bh, err := parseUserBehaviour(userBehaviourPath(), []byte(st.Behaviour))
	if err != nil {
		return nil, nil, nil, behaviour{}, err
	}
	return doc, u, tx, bh, nil
}

// undoInterface brings back the interface as it was before the last change and keeps
// the current one in its place, so a second undo is a redo.
func undoInterface() (*scene.Document, userTokens, userTexts, behaviour, error) {
	if configDir() == "" {
		return nil, nil, nil, behaviour{}, errNoConfigDir
	}
	b, err := os.ReadFile(previousInterfacePath())
	if os.IsNotExist(err) {
		return nil, nil, nil, behaviour{}, errors.New("there is no earlier interface to go back to")
	}
	if err != nil {
		return nil, nil, nil, behaviour{}, err
	}
	var prev interfaceState
	if err := json.Unmarshal(b, &prev); err != nil {
		return nil, nil, nil, behaviour{}, fmt.Errorf("the earlier interface cannot be read: %w", err)
	}
	doc, u, tx, bh, err := loadInterface(prev)
	if err != nil {
		return nil, nil, nil, behaviour{}, fmt.Errorf("the earlier interface no longer loads: %w", err)
	}
	cur, err := currentInterface()
	if err != nil {
		return nil, nil, nil, behaviour{}, err
	}
	cb, _ := json.Marshal(cur)
	if err := writeAtomic(previousInterfacePath(), cb); err != nil {
		return nil, nil, nil, behaviour{}, err
	}
	return doc, u, tx, bh, writeInterface(prev)
}

// resetInterface goes back to the built-in look. What was saved is kept as the one
// /ui undo returns to, so a reset is never the end of the user's work.
func resetInterface() (*scene.Document, userTokens, userTexts, behaviour, error) {
	if configDir() == "" {
		return nil, nil, nil, behaviour{}, errNoConfigDir
	}
	cur, err := currentInterface()
	if err != nil {
		return nil, nil, nil, behaviour{}, err
	}
	if len(cur.Scene) > 0 || len(cur.Tokens) > 0 || len(cur.Texts) > 0 || len(cur.Behaviour) > 0 {
		cb, _ := json.Marshal(cur)
		if err := os.MkdirAll(configDir(), 0o700); err != nil {
			return nil, nil, nil, behaviour{}, err
		}
		if err := writeAtomic(previousInterfacePath(), cb); err != nil {
			return nil, nil, nil, behaviour{}, err
		}
	}
	if err := writeInterface(interfaceState{}); err != nil {
		return nil, nil, nil, behaviour{}, err
	}
	doc, err := scene.ParseNamed(defaultscene.Name, defaultscene.JSON)
	return doc, nil, nil, behaviour{}, err
}
