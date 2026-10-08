package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/michiTrader/arxi_tui/internal/defaultscene"
	"github.com/michiTrader/arxi_tui/internal/driver"
	"github.com/michiTrader/arxi_tui/internal/patch"
	"github.com/michiTrader/arxi_tui/internal/scene"
	"github.com/michiTrader/arxi_tui/internal/theme"
)

// This file is the interface bridge: how the chat model reads and changes arxi-tui's
// own interface when the user asks it to ("add a blank line between the input and the
// status bar").
//
// # Why the model gets tools, not files
//
// The interface is a scene document that lives in this process: compiled into the
// binary, or kept in the settings folder once the user has changed it. It is not in
// the project folder, and a model pointed at the project went looking there, found
// nothing, and gave up (the defect that motivated this file). Handing it a path would
// be wrong twice: a release has no source tree, and the model would write bytes the
// host never validates. So the host lends the model two tools over the core's
// client_tools channel, and keeps the authority where the document is.
//
// # Why the knowledge is a tool and not a standing prompt
//
// What the model needs to edit the interface (the live document, the node types,
// keys, binds and style tokens, the /ui grammar) is about 1.5k tokens. Sent with every
// question it would be paid on every "hola", for a request made once in a long while.
// As the result of ui_guide it is paid only on the turn that needs it, and it is
// always the live document rather than a description that can drift from it. What
// rides along with every request is the two tool definitions, about a hundred tokens.
//
// # Why every change is a proposal
//
// ui_edit never changes the screen by itself. It builds the new document from the one
// on screen, holds it to the same validator a scene loaded from disk passes (plus the
// theme's tokens, plus a rule that the input bar must survive), shows the user the diff
// and asks; only an allowed change is handed to the loop, which swaps it in and saves
// it. A refusal goes back to the model with its file:line, which is the repair loop
// the eval corpus measured. Plugins propose, they never write; the agent is held to
// the same rule.

// Names of the two tools, as the model calls them.
const (
	uiToolGuide = "ui_guide"
	uiToolEdit  = "ui_edit"
)

// uiBridge is the host side of the two tools. The chat turn calls it from its own
// goroutine; the loop owns the document, so the bridge reads a snapshot the loop
// publishes and hands an approved change back to the loop to apply.
type uiBridge struct {
	mu  sync.Mutex
	doc *scene.Document
	thm *theme.Theme
	// policy is what the agent mode says about changing the interface: deny (plan),
	// ask, or allow (full access only).
	policy string
	// apply carries an approved change to the loop; the loop answers on done.
	apply chan uiApply
}

// uiApply is one approved change on its way to the loop. base is the document the
// change was drafted from: if the screen moved on meanwhile (the user typed a /ui
// command while the question was open), the loop refuses it rather than silently
// reverting what the user did.
type uiApply struct {
	base, doc *scene.Document
	done      chan error
}

func newUIBridge() *uiBridge {
	return &uiBridge{policy: policyAsk, apply: make(chan uiApply)}
}

// publish records the document and theme on screen. The loop calls it on every
// repaint, so a tool call always drafts against what the user is looking at.
func (b *uiBridge) publish(doc *scene.Document, thm *theme.Theme) {
	b.mu.Lock()
	b.doc, b.thm = doc, thm
	b.mu.Unlock()
}

func (b *uiBridge) snapshot() (*scene.Document, *theme.Theme) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.doc, b.thm
}

// setMode follows the agent mode. Changing the interface is asked about in every mode
// but the one that asks about nothing, including auto: the user is changing the tool
// they are using, and a layout they did not see coming is a worse surprise than a file
// edit they can read in the diff of their own repository.
func (b *uiBridge) setMode(name string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	switch name {
	case "plan":
		b.policy = policyDeny
	case "full access":
		b.policy = policyAllow
	default:
		b.policy = policyAsk
	}
}

// definitions are the tools as the model is told about them. They are sent with every
// request, so they say only when to use each and what to pass; ui_guide carries the rest.
func (b *uiBridge) definitions() []driver.ClientToolDef {
	return []driver.ClientToolDef{
		{Name: uiToolGuide,
			Description: "Read how arxi-tui's OWN interface is built (this terminal app you are running in: banner, chat pane, input bar, status bar), with its live document. It is not in the project folder. Call it before ui_edit whenever the user asks to change how this interface looks or is laid out.",
			Schema:      json.RawMessage(`{"type":"object","properties":{}}`)},
		{Name: uiToolEdit,
			Description: "Propose a change to arxi-tui's own interface. The user sees the diff and approves it; it is saved and kept across sessions. Pass either commands (/ui lines, applied in order) or scene (the complete new document). A refusal names file:line: fix that and retry.",
			Schema: json.RawMessage(`{"type":"object","properties":{` +
				`"commands":{"type":"array","items":{"type":"string"},"description":"/ui add|move|set|style lines, as ui_guide explains"},` +
				`"scene":{"type":"string","description":"the complete new scene document, as JSON"},` +
				`"summary":{"type":"string","description":"one sentence saying what changes, shown to the user"}}}`)},
	}
}

// call runs one tool call. ask puts a change to the user and blocks until they decide.
func (b *uiBridge) call(ctx context.Context, c driver.ClientToolCall, ask func(driver.Approval) bool) driver.ClientToolResult {
	switch c.Name {
	case uiToolGuide:
		doc, thm := b.snapshot()
		if doc == nil {
			return driver.ClientToolResult{Text: "the interface is not on screen yet; try again", Summary: "The interface is not ready"}
		}
		return driver.ClientToolResult{OK: true, Text: uiGuide(doc, thm), Arg: "interface", Summary: "Read how the interface is built"}
	case uiToolEdit:
		return b.edit(ctx, c.Arguments, ask)
	}
	return driver.ClientToolResult{Text: c.Name + " is not a tool of this client"}
}

func (b *uiBridge) edit(ctx context.Context, raw json.RawMessage, ask func(driver.Approval) bool) driver.ClientToolResult {
	refuse := func(summary, text string) driver.ClientToolResult {
		return driver.ClientToolResult{Arg: "interface", Summary: summary, Text: text}
	}
	b.mu.Lock()
	policy := b.policy
	b.mu.Unlock()
	if policy == policyDeny {
		return refuse("Not available in plan mode", "the interface may not be changed in plan mode; say what you would change instead")
	}
	var args struct {
		Commands []string `json:"commands"`
		Scene    string   `json:"scene"`
		Summary  string   `json:"summary"`
	}
	if err := json.Unmarshal(raw, &args); err != nil {
		return refuse("The change was refused", "ui_edit takes {commands: [...]} or {scene: \"...\"}: "+err.Error())
	}
	base, thm := b.snapshot()
	if base == nil {
		return refuse("The interface is not ready", "the interface is not on screen yet; try again")
	}
	next, err := draftScene(base, thm, args.Commands, args.Scene)
	if err != nil {
		return refuse("The change was refused", err.Error()+"\nNothing was changed. Fix what the message names and call ui_edit again.")
	}
	diff, err := uiDiffText(base.Source(), next.Source())
	if err != nil {
		return refuse("The change was refused", err.Error())
	}
	if diff == "" {
		return driver.ClientToolResult{OK: true, Arg: "interface", Summary: "Nothing to change", Text: "the new document is the same as the one on screen; nothing was changed"}
	}
	summary := strings.TrimSpace(args.Summary)
	if summary == "" {
		summary = "Change the interface"
	}
	if policy != policyAllow && !ask(driver.Approval{Name: uiToolEdit, Arg: "interface", Summary: summary, Diff: diff}) {
		return refuse("You did not allow this change", "the user did not allow this change to the interface, so it was not made; do not retry it, ask what they want instead")
	}
	done := make(chan error, 1)
	select {
	case b.apply <- uiApply{base: base, doc: next, done: done}:
	case <-ctx.Done():
		return refuse("Cancelled", "the turn ended before the change was applied")
	}
	select {
	case err = <-done:
	case <-ctx.Done():
		return refuse("Cancelled", "the turn ended before the change was applied")
	}
	if err != nil {
		return refuse("The change was not applied", err.Error())
	}
	return driver.ClientToolResult{OK: true, Arg: "interface", Summary: summary, Diff: diff,
		Text: "applied and saved: the interface on screen now shows the change. /ui undo takes it back."}
}

// draftScene builds the proposed document from base and validates it as strictly as a
// scene loaded at boot, and then some: the theme must define every style token (an
// unknown token draws unstyled with no complaint), no new warning may appear (an
// unknown key is a property the model invented and the engine ignores), and the input
// bar must survive (an interface the user cannot type into cannot be fixed from inside).
func draftScene(base *scene.Document, thm *theme.Theme, commands []string, whole string) (*scene.Document, error) {
	name := base.Name()
	var next *scene.Document
	switch {
	case len(commands) > 0 && strings.TrimSpace(whole) != "":
		return nil, errors.New("pass commands or scene, not both")
	case strings.TrimSpace(whole) != "":
		doc, err := scene.ParseNamed(name, []byte(stripFence(whole)))
		if err != nil {
			return nil, err
		}
		if err := doc.RefuseEmpty(); err != nil {
			return nil, err
		}
		if err := doc.Validate(); err != nil {
			return nil, err
		}
		next = doc
	case len(commands) > 0:
		src := base.Source()
		for i, line := range commands {
			cmd, err := patch.Parse(line)
			if err != nil {
				return nil, fmt.Errorf("command %d (%s): %w", i+1, line, err)
			}
			if !documentVerb(cmd.Verb) {
				return nil, fmt.Errorf("command %d (%s): ui_edit takes add, move, set and style; %s is for the user to type", i+1, line, cmd.Verb)
			}
			res, err := patch.Apply(name, src, line)
			if err != nil {
				return nil, fmt.Errorf("command %d (%s): %w", i+1, line, err)
			}
			src, next = res.Source, res.Doc
		}
	default:
		return nil, errors.New("ui_edit needs commands or scene")
	}
	if errs := scene.ValidateTokens(next, thm); len(errs) > 0 {
		return nil, fmt.Errorf("%v (the defined tokens are listed in ui_guide)", errs[0])
	}
	known := map[string]bool{}
	for _, w := range base.Warnings() {
		known[w.Msg] = true
	}
	for _, w := range next.Warnings() {
		if !known[w.Msg] {
			return nil, fmt.Errorf("%s (the engine would ignore it)", w.String())
		}
	}
	if findUserInput(next.Root) == nil {
		return nil, errors.New("the new document has no input bound to user.input; the user could not type any more, so it is refused")
	}
	return next, nil
}

// documentVerb reports whether a /ui verb changes the document itself, the only kind
// the model may propose and the only kind that is saved. hide/show change view state
// for this session; plugin reaches the network and installs code, which is the user's
// decision to type, not the model's to propose.
func documentVerb(verb string) bool {
	switch verb {
	case "add", "move", "set", "style":
		return true
	}
	return false
}

// stripFence drops a markdown fence around a document, the one formatting habit every
// chat model has (internal/eval.StripFence argues why tolerating it is not cheating).
func stripFence(s string) string {
	s = strings.TrimSpace(s)
	if !strings.HasPrefix(s, "```") {
		return s
	}
	if nl := strings.IndexByte(s, '\n'); nl >= 0 {
		s = s[nl+1:]
	}
	if end := strings.LastIndex(s, "```"); end >= 0 {
		s = s[:end]
	}
	return strings.TrimSpace(s)
}

// uiDiffContext is how many unchanged lines are kept around each change.
const uiDiffContext = 2

// uiMaxDiffLines caps the diff the user is shown, so a rewrite does not bury the
// question under the whole document.
const uiMaxDiffLines = 120

// uiDiffText is the change as the conversation draws diffs ("%5d %c %s", "..." between
// distant changes), computed on the canonical form of both documents so reformatting
// is never shown as a change. Empty means nothing changed.
func uiDiffText(oldSrc, newSrc []byte) (string, error) {
	d, err := patch.DiffSource(oldSrc, newSrc)
	if err != nil {
		return "", err
	}
	if !d.Changed() {
		return "", nil
	}
	keep := make([]bool, len(d.Lines))
	for i, l := range d.Lines {
		if l.Op == patch.OpEqual {
			continue
		}
		for k := i - uiDiffContext; k <= i+uiDiffContext; k++ {
			if k >= 0 && k < len(d.Lines) {
				keep[k] = true
			}
		}
	}
	var out []string
	gap := false
	for i, l := range d.Lines {
		if !keep[i] {
			gap = len(out) > 0
			continue
		}
		if gap {
			out = append(out, "...")
			gap = false
		}
		mark, n := byte(' '), l.OldLine
		switch l.Op {
		case patch.OpInsert:
			mark, n = '+', l.NewLine
		case patch.OpDelete:
			mark = '-'
		}
		out = append(out, fmt.Sprintf("%5d %c %s", n, mark, l.Text))
	}
	if len(out) > uiMaxDiffLines {
		more := len(out) - uiMaxDiffLines
		out = append(out[:uiMaxDiffLines], fmt.Sprintf("... %d more lines of diff", more))
	}
	return strings.Join(out, "\n") + "\n", nil
}

// uiGuide is everything the model needs to change the interface, built from the live
// document and the live vocabularies so it cannot fall behind either.
func uiGuide(doc *scene.Document, thm *theme.Theme) string {
	tokens := thm.Tokens()
	sort.Strings(tokens)
	var b strings.Builder
	b.WriteString(`arxi-tui's interface is a JSON scene document, drawn top to bottom. You change it with ui_edit; the user approves every change, which is saved and kept across sessions.

How it is built:
- {"root": node}. A node is {"type": ..., "id": ..., ...}; containers (stack, row, box, overlay) have "children".
- stack lays children out vertically, row horizontally. text draws "text" or the value of "bind". An empty line is {"type":"text","text":""}.
- "when": <bind> shows a node only while that bind is truthy. "style": {"style": <token>} colours it.
- Ids are addresses: give every node you add a new unique id.

Edit with commands (preferred, small and exact), applied in order:
  /ui add node <where> <json-node>
  /ui move <id> <where>
  /ui set <id> <key> <value>
  /ui style <id> <token>
  where = above <id> | below <id> | into <id> [top] | above_input | below_input
Example, one more blank line under the input bar:
  /ui add node below_input {"id":"input_gap_extra","type":"text","text":""}
Or pass scene: the complete new document, changing as little as the order requires.

Rules: every bind and when must come from the bind list; every style token from the token list; keep the node bound to user.input.

`)
	fmt.Fprintf(&b, "Node types: %s\n", strings.Join(scene.SignedNodeTypes(), ", "))
	fmt.Fprintf(&b, "Node keys: %s\n", strings.Join(scene.Vocabulary(), ", "))
	fmt.Fprintf(&b, "Binds: %s\n", strings.Join(scene.SignedBinds(), ", "))
	fmt.Fprintf(&b, "Style tokens: %s\n\n", strings.Join(tokens, ", "))
	fmt.Fprintf(&b, "Current document (%s):\n%s\n", filepath.Base(doc.Name()), strings.TrimSpace(string(doc.Source())))
	return b.String()
}

// ---- keeping the user's interface ----------------------------------------------

// userScenePath is where the user's own interface is kept, "" when there is nowhere.
// It sits beside the other settings rather than in the project, because it is the
// user's interface in every folder they open, not a property of one repository.
func userScenePath() string {
	d := configDir()
	if d == "" {
		return ""
	}
	return filepath.Join(d, "scene.json")
}

// previousScenePath keeps the interface as it was before the last change, for /ui undo.
func previousScenePath() string {
	p := userScenePath()
	if p == "" {
		return ""
	}
	return strings.TrimSuffix(p, ".json") + ".prev.json"
}

// persistsScene reports whether changes to doc are saved: only the default interface
// (the built-in one, or the user's saved one) is. A scene booted with -scene is the
// user's file to edit, and silently writing the settings copy from it would replace
// their everyday interface with a test document.
func persistsScene(doc *scene.Document) bool {
	if doc == nil {
		return false
	}
	return doc.Name() == defaultscene.Name || (userScenePath() != "" && doc.Name() == userScenePath())
}

// saveUserScene keeps doc as the user's interface, and what was there as the one /ui
// undo goes back to. The write goes through a temporary file and a rename, so a crash
// mid-write leaves the old interface rather than half a document.
func saveUserScene(doc *scene.Document) error {
	path := userScenePath()
	if path == "" {
		return errNoConfigDir
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	prev := previousScenePath()
	if old, err := os.ReadFile(path); err == nil {
		if err := writeAtomic(prev, old); err != nil {
			return err
		}
	} else if os.IsNotExist(err) {
		// The first change: the interface it replaces is the built-in one.
		if err := writeAtomic(prev, defaultscene.JSON); err != nil {
			return err
		}
	}
	return writeAtomic(path, doc.Source())
}

func writeAtomic(path string, data []byte) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".scene-*")
	if err != nil {
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmp.Name())
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmp.Name())
		return err
	}
	if err := os.Chmod(tmp.Name(), 0o600); err != nil {
		os.Remove(tmp.Name())
		return err
	}
	return os.Rename(tmp.Name(), path)
}

// resolveBootScene is resolveStartScene plus the user's saved interface: with no
// -scene, a saved interface boots in place of the built-in one. A saved interface that
// no longer loads (a newer engine refused it, or it was edited by hand) falls back to
// the built-in one, never to raw, and says why and how to clear it. -raw and -scene
// are untouched, so the escape hatch never depends on the settings folder.
func resolveBootScene(path string) (*scene.Document, string, error) {
	user := userScenePath()
	if path != builtinScene || user == "" {
		return resolveStartScene(path)
	}
	if _, err := os.Stat(user); err != nil {
		return resolveStartScene(path)
	}
	doc, notice, err := loadScene(user, factoryRAW)
	if err == nil && doc.Name() == user && findUserInput(doc.Root) != nil {
		return doc, notice, nil
	}
	if err == nil && notice == "" {
		notice = "it has no input bound to user.input"
	}
	builtin, bnotice, berr := resolveStartScene(builtinScene)
	if berr != nil {
		return builtin, bnotice, berr
	}
	return builtin, "your saved interface (" + user + ") could not be used: " + notice + "; showing the built-in one (/ui reset deletes the saved one)", nil
}

// uiHistoryCommand reports whether the line is /ui undo or /ui reset, which the host
// answers itself: they are about the saved interface, not a patch to the one on screen.
func uiHistoryCommand(input string) (string, bool) {
	switch strings.Join(strings.Fields(input), " ") {
	case "/ui undo":
		return "undo", true
	case "/ui reset":
		return "reset", true
	}
	return "", false
}

// undoUserScene brings back the interface as it was before the last change, and keeps
// the current one in its place, so a second undo is a redo.
func undoUserScene() (*scene.Document, error) {
	prev, cur := previousScenePath(), userScenePath()
	if prev == "" {
		return nil, errNoConfigDir
	}
	old, err := os.ReadFile(prev)
	if os.IsNotExist(err) {
		return nil, errors.New("there is no earlier interface to go back to")
	}
	if err != nil {
		return nil, err
	}
	doc, err := scene.ParseNamed(cur, old)
	if err == nil {
		err = doc.Validate()
	}
	if err != nil {
		return nil, fmt.Errorf("the earlier interface no longer loads: %w", err)
	}
	if now, err := os.ReadFile(cur); err == nil {
		if err := writeAtomic(prev, now); err != nil {
			return nil, err
		}
	}
	if err := writeAtomic(cur, old); err != nil {
		return nil, err
	}
	return doc, nil
}

// resetUserScene goes back to the built-in interface. The saved one is kept as the
// one /ui undo returns to, so a reset is never the end of the user's work.
func resetUserScene() (*scene.Document, error) {
	cur := userScenePath()
	if cur == "" {
		return nil, errNoConfigDir
	}
	if now, err := os.ReadFile(cur); err == nil {
		if err := writeAtomic(previousScenePath(), now); err != nil {
			return nil, err
		}
		if err := os.Remove(cur); err != nil {
			return nil, err
		}
	}
	return scene.ParseNamed(defaultscene.Name, defaultscene.JSON)
}

// interfaceBridger is the optional capability a Driver has when its chat can change the
// interface (serveDriver).
type interfaceBridger interface {
	InterfaceBridge() *uiBridge
}
