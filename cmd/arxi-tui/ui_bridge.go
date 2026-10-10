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

// uiSystemHint tells the model, on a turn where the user asked for it (uiTrigger), that the
// app it is talking through is arxi-tui and that "the tui", "the interface", "the
// colours" mean that app, not the user's project. The guide itself follows it in the
// same turn. Without the trigger none of this is sent, and "the TUI" means the user's
// own project.
const uiSystemHint = "You are running inside arxi-tui, the app the user talks to you through. \"The TUI\", the interface, its colours mean this app, not their project: never search their files or read scene.json. " +
	"The guide is already below: call ui_guide at most once. Change it only with ui_edit; say done only if it returned \"applied and saved\"."

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
	// below is the theme under the user's colours (factory and plugins); user is
	// the user's own colour layer, which a colour change edits. What is drawn is
	// the one laid over the other, and keeping them apart is what lets a colour be
	// taken off: it falls back to below, which the composed theme no longer knows.
	below *theme.Theme
	user  userTokens
	// texts is the user's own wording layer (ui_texts.go), kept apart from the factory
	// sentences for the same reason user is kept apart from below.
	texts userTexts
	// beh is the user's behaviour layer (behaviour.go).
	beh behaviour
	// policy is what the agent mode says about changing the interface: deny (plan),
	// ask, or allow (full access only).
	policy string
	// lastRefusal and refusals count identical refusals in a row (noteRefusal).
	lastRefusal string
	refusals    int
	// apply carries an approved change to the loop; the loop answers on done.
	apply chan uiApply
}

// uiApply is one approved change on its way to the loop. base is the document the
// change was drafted from: if the screen moved on meanwhile (the user typed a /ui
// command while the question was open), the loop refuses it rather than silently
// reverting what the user did.
type uiApply struct {
	base, doc *scene.Document
	// colors is the user's colour layer after the change, nil when the change
	// touches no colour; baseColors is the layer it was drafted from.
	colors, baseColors userTokens
	// texts is the user's wording after the change, nil when the change touches no
	// text; baseTexts is the layer it was drafted from.
	texts, baseTexts userTexts
	// beh is the behaviour after the change; behChanged says it was touched, since the
	// zero behaviour is also the answer to "remove everything". baseBeh is what it was
	// drafted from.
	beh, baseBeh behaviour
	behChanged   bool
	done         chan error
}

func newUIBridge() *uiBridge {
	return &uiBridge{policy: policyAsk, apply: make(chan uiApply)}
}

// publish records the document and theme on screen. The loop calls it on every
// repaint, so a tool call always drafts against what the user is looking at.
func (b *uiBridge) publish(doc *scene.Document, below *theme.Theme, user userTokens, texts userTexts, beh behaviour) {
	b.mu.Lock()
	b.doc, b.below, b.user, b.texts, b.beh = doc, below, user, texts, beh
	b.mu.Unlock()
}

// snapshot returns the document, the theme under the user's colours, the user's
// colours, the theme on screen (the two laid together) and the user's wording.
func (b *uiBridge) snapshot() (*scene.Document, *theme.Theme, userTokens, *theme.Theme, userTexts) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.doc, b.below, b.user, withBehaviour(withUserTokens(b.below, b.user), b.beh), b.texts
}

// behaviourNow is the behaviour layer on screen.
func (b *uiBridge) behaviourNow() behaviour {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.beh
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
			Description: "Read how YOUR OWN interface is built: the terminal app you are running in and the user is looking at (banner, chat, input bar, status bar, and the colours of everything you write). It is NOT in the project folder; never search files for it. Call this first whenever the user asks to change anything about how you or this screen look: layout, spacing, colours, styles.",
			Schema:      json.RawMessage(`{"type":"object","properties":{}}`)},
		{Name: uiToolEdit,
			Description: "Change your own interface (call ui_guide first). The user sees the diff and approves it; it is kept across sessions. colors recolours; texts rewords menus and screens; commands (/ui lines) or scene change the layout; behaviour changes what it DOES: animated colours, menu keys (horizontal menus), shortcuts, own / commands, reactions to settings. A refusal says what to fix.",
			Schema: json.RawMessage(`{"type":"object","properties":{` +
				`"colors":{"type":"object","additionalProperties":{"type":"string"},"description":"token -> style, e.g. {\"markdown.code\":\"fg=magenta\"}"},` +
				`"texts":{"type":"object","additionalProperties":{"type":"string"},"description":"key -> sentence, e.g. {\"team.title\":\"Crews\"}; empty restores the factory one"},` +
				`"commands":{"type":"array","items":{"type":"string"},"description":"/ui add|move|set|style lines"},` +
				`"scene":{"type":"string","description":"the whole new scene, as JSON"},` +
				`"behaviour":{"type":"object","description":"animations, menu_keys, keys, commands, hooks; format in ui_guide"},` +
				`"summary":{"type":"string","description":"one sentence saying what changes, shown to the user"}}}`)},
	}
}

// guideText is the whole guide, "" while no interface is on screen.
func (b *uiBridge) guideText() string {
	doc, _, _, thm, _ := b.snapshot()
	if doc == nil {
		return ""
	}
	return uiGuide(doc, thm) + "\n" + behaviourGuide(b.behaviourNow())
}

// call runs one tool call. ask puts a change to the user and blocks until they decide.
func (b *uiBridge) call(ctx context.Context, c driver.ClientToolCall, ask func(driver.Approval) bool) driver.ClientToolResult {
	switch c.Name {
	case uiToolGuide:
		text := b.guideText()
		if text == "" {
			return driver.ClientToolResult{Text: "the interface is not on screen yet; try again", Summary: "The interface is not ready"}
		}
		return driver.ClientToolResult{OK: true, Text: text, Arg: "interface", Summary: "Read how the interface is built"}
	case uiToolEdit:
		return b.edit(ctx, c.Arguments, ask)
	}
	return driver.ClientToolResult{Text: c.Name + " is not a tool of this client"}
}

func (b *uiBridge) edit(ctx context.Context, raw json.RawMessage, ask func(driver.Approval) bool) driver.ClientToolResult {
	refuse := func(summary, text string) driver.ClientToolResult {
		// The chat shows the Summary and only the model sees the Text, so a bare
		// "The change was refused" told the user nothing while the model looped on a
		// reason nobody could read. The first line of the reason goes on the screen.
		if reason := firstLine(text, 200); reason != "" && summary == "The change was refused" {
			summary += ": " + reason
		}
		// The same refusal again and again is the model guessing; after the third the
		// tool says so, and tells it to report to the user instead of trying a fourth.
		if n := b.noteRefusal(text); n >= 3 {
			text += fmt.Sprintf("\nThis exact refusal has now happened %d times in a row. Stop calling ui_edit with the same input. Tell the user, in plain words, what you tried and this reason, and ask what they want instead. Never say the change was made.", n)
		}
		return driver.ClientToolResult{Arg: "interface", Summary: summary, Text: text}
	}
	b.mu.Lock()
	policy := b.policy
	b.mu.Unlock()
	if policy == policyDeny {
		return refuse("Not available in plan mode", "the interface may not be changed in plan mode; say what you would change instead")
	}
	var args struct {
		Colors    map[string]string `json:"colors"`
		Texts     map[string]string `json:"texts"`
		Commands  []string          `json:"commands"`
		Scene     string            `json:"scene"`
		Behaviour json.RawMessage   `json:"behaviour"`
		Summary   string            `json:"summary"`
	}
	if err := json.Unmarshal(raw, &args); err != nil {
		return refuse("The change was refused", "ui_edit takes {colors: {...}}, {texts: {...}}, {commands: [...]} or {scene: \"...\"}: "+err.Error())
	}
	base, below, user, thm, userWords := b.snapshot()
	if base == nil {
		return refuse("The interface is not ready", "the interface is not on screen yet; try again")
	}
	userBeh := b.behaviourNow()
	nextBeh := userBeh
	behChanged := false
	var behPatch behaviourPatch
	if len(args.Behaviour) > 0 && string(args.Behaviour) != "null" {
		var err error
		if behPatch, err = parseBehaviourPatch(args.Behaviour); err != nil {
			return refuse("The change was refused", err.Error()+"\nNothing was changed. Fix it and call ui_edit again.")
		}
		if nextBeh, err = applyBehaviourPatch(userBeh, behPatch); err != nil {
			return refuse("The change was refused", err.Error()+"\nNothing was changed. Fix it and call ui_edit again.")
		}
		behChanged = !sameBehaviour(userBeh, nextBeh)
	}
	var colors userTokens
	var words userTexts
	var diff string
	nextThm := thm
	if len(args.Colors) > 0 {
		var err error
		if colors, err = applyColors(user, thm, args.Colors); err != nil {
			return refuse("The change was refused", err.Error()+"\nNothing was changed. Fix it and call ui_edit again.")
		}
		nextThm = withBehaviour(withUserTokens(below, colors), userBeh)
		changed := make([]string, 0, len(args.Colors))
		for k := range args.Colors {
			changed = append(changed, k)
		}
		sort.Strings(changed)
		diff = colorDiff(thm, nextThm, changed)
	}
	if len(args.Texts) > 0 {
		var err error
		if words, err = applyTexts(userWords, args.Texts); err != nil {
			return refuse("The change was refused", err.Error()+"\nNothing was changed. Fix it and call ui_edit again.")
		}
		changed := make([]string, 0, len(args.Texts))
		for k := range args.Texts {
			changed = append(changed, k)
		}
		sort.Strings(changed)
		diff += textDiff(userWords, words, changed)
	}
	if behChanged {
		// Animations the same call adds are tokens the scene commands may use at once.
		nextThm = withBehaviour(nextThm, nextBeh)
		bd, err := behaviourDiff(userBeh, nextBeh)
		if err != nil {
			return refuse("The change was refused", err.Error())
		}
		diff += "      behaviour.json:\n" + bd
	}
	next := base
	if len(args.Commands) > 0 || strings.TrimSpace(args.Scene) != "" {
		var err error
		if next, err = draftScene(base, nextThm, args.Commands, args.Scene); err != nil {
			return refuse("The change was refused", err.Error()+"\nNothing was changed. Fix what the message names and call ui_edit again.")
		}
		d, err := uiDiffText(base.Source(), next.Source())
		if err != nil {
			return refuse("The change was refused", err.Error())
		}
		diff += d
	} else if len(args.Colors) == 0 && len(args.Texts) == 0 && len(args.Behaviour) == 0 {
		return refuse("The change was refused", "ui_edit needs colors, texts, behaviour, commands or scene")
	}
	if diff == "" {
		return driver.ClientToolResult{OK: true, Arg: "interface", Summary: "Nothing to change", Text: "the new document is the same as the one on screen; nothing was changed"}
	}
	summary := strings.TrimSpace(args.Summary)
	if summary == "" {
		summary = "Change the interface"
	}
	// What runs on a keypress or a setting change is asked about even in full access.
	if (policy != policyAllow || (behChanged && behPatch.touchesWhatRuns())) && !ask(driver.Approval{Name: uiToolEdit, Arg: "interface", Summary: summary, Diff: diff}) {
		return refuse("You did not allow this change", "the user did not allow this change to the interface, so it was not made; do not retry it, ask what they want instead")
	}
	done := make(chan error, 1)
	var err error
	select {
	case b.apply <- uiApply{base: base, doc: next, colors: colors, baseColors: user, texts: words, baseTexts: userWords, beh: nextBeh, baseBeh: userBeh, behChanged: behChanged, done: done}:
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
	b.resetRefusals()
	text := "applied and saved: the interface on screen now shows the change. /ui undo takes it back."
	for _, n := range unpaintedFrames(next, nextThm) {
		text += fmt.Sprintf("\nBUT: the frame of %q is NOT animated. Its \"style\" paints what is inside the box; the frame is painted by the border's own style. Do not tell the user the frame moves. To make it so: /ui set %s border {\"shape\":%q,\"style\":%q}", n.id, n.id, n.shape, n.token)
	}
	return driver.ClientToolResult{OK: true, Arg: "interface", Summary: summary, Diff: diff, Text: text}
}

// unpaintedFrame is a bordered node whose animated style token reaches its contents
// but not its frame. Found with a model that wrote "border":"round" plus
// "style":{"style":"rainbow"} and told the user the border was a rainbow: both were
// valid, neither coloured the frame, and the tool said "applied".
type unpaintedFrame struct{ id, shape, token string }

func unpaintedFrames(doc *scene.Document, thm *theme.Theme) []unpaintedFrame {
	var out []unpaintedFrame
	var walk func(n *scene.Node)
	walk = func(n *scene.Node) {
		if n == nil {
			return
		}
		if n.BorderShape() != "" && n.BorderStyleName() == "" && n.ID != "" {
			for _, key := range scene.StyleTokenKeys() {
				if tok := n.Style[key]; tok != "" {
					if _, animated := thm.Cycle(tok); animated {
						out = append(out, unpaintedFrame{n.ID, n.BorderShape(), tok})
					}
					break
				}
			}
		}
		walk(n.PrefixNode())
		walk(n.Suffix)
		for _, c := range n.Children {
			walk(c)
		}
		walk(n.RowTemplate)
	}
	if doc != nil {
		walk(doc.Root)
	}
	return out
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
		return nil, fmt.Errorf("%v (the defined tokens are listed in ui_guide; to change what a token looks like use colors)", errs[0])
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
// document, the live theme and the live vocabularies so it cannot fall behind any.
func uiGuide(doc *scene.Document, thm *theme.Theme) string {
	var b strings.Builder
	b.WriteString(`This is your own interface: what the user sees while talking to you. Change it with ui_edit; the user approves every change, and it is kept across sessions (/ui undo takes it back).

COLOURS. Every coloured thing is a style token. Your replies are Markdown, so the coloured words in them are markdown.* tokens (inline code and links are cyan by default). To recolour, pass colors: {token: style}. A style is "fg=<colour>" with optional "bg=<colour>" and attributes (bold dim italic underline reverse strike). A colour is a name (red green yellow blue magenta cyan white black, or bright-<name>), 0-255, or #rrggbb. Purple: magenta, bright-magenta or #a855f7. An empty style puts the default back. Example, purple inline code and links:
  colors: {"markdown.code": "fg=magenta", "markdown.link": "fg=magenta underline"}

WORDS. Every sentence the app writes in its own menus and screens (what each / command is described as, the hint under /effort, /mode and /style, the key legends, the whole of /team and its buttons) is a text key. To reword or translate them pass texts: {key: sentence}; an empty sentence puts the factory one back, a few spaces hides it. Keys are listed below with what they say now. Keep a {placeholder} where the factory sentence has one. Example, reword /team:
  texts: {"team.title": "Crews", "team.new_agent.label": "+ Add an agent..."}
Words that are not keys (the labels inside a form, error messages, the model's own replies) are not editable this way; say so rather than promising it.

LAYOUT. The interface is a JSON scene document drawn top to bottom: {"root": node}; a node is {"type", "id", ...}; stack lays children out vertically, row horizontally; text draws "text" or the value of "bind"; an empty line is {"type":"text","text":""}; "when": <bind> shows a node only while the bind is truthy; "style": {"style": <token>} picks which token paints a node. Give every node you add a new unique id. Change the layout with commands (preferred), applied in order:
  /ui add node <where> <json-node>
  /ui move <id> <where>
  /ui set <id> <key> <value>
  /ui style <id> <token>
  where = above <id> | below <id> | into <id> [top] | above_input | below_input
Example, one more blank line under the input bar:
  /ui add node below_input {"id":"input_gap_extra","type":"text","text":""}
FRAMES. Only a box (or an overlay) draws a border; a border on any other node is refused. "border" is one of single, double, round (rounded corners), heavy, ascii, or {"shape":"round","style":"<token>"} to colour it. To put a rounded frame around the input bar: /ui add node above prompt {"id":"input_frame","type":"box","border":"round"} then /ui move prompt into input_frame. To make that frame move in colour, define an animation and name it as the frame's style (two ui_edit parts in one call): behaviour: {"animations": {"rainbow": {"colors": ["#ff3b30","#ffcc00","#34c759","#00c7be","#0a84ff","#bf5af2"], "fps": 12, "direction": "diagonal"}}} and commands: ["/ui add node above prompt {\"id\":\"input_frame\",\"type\":\"box\",\"border\":{\"shape\":\"round\",\"style\":\"rainbow\"}}", "/ui move prompt into input_frame"]. If the frame already exists, only /ui set input_frame border {"shape":"round","style":"rainbow"} is needed. A colour that moves is always an animation token like this one, never a colors entry. "direction":"diagonal" (or "radial", "horizontal", "vertical", "antidiagonal") makes one smooth gradient run across the whole frame, so the top and bottom lines differ; without it every line repeats the same colours. Animate the frame ONLY through the border's style: never put the animation on the input's or the box's own "style", or the typed letters change colour too, which is not what a frame asks for. Do not invent other words ("rounded", "{type:round}"): they are refused.
Or pass scene: the complete new document, changing as little as the order requires. Every bind and when must come from the bind list; keep the node bound to user.input.

`)
	b.WriteString("Style tokens (token = current style  # what it paints):\n")
	b.WriteString(colorTable(thm))
	b.WriteString("\nText keys (key = what it says now  # where it appears):\n")
	b.WriteString(textsGuide())
	fmt.Fprintf(&b, "\nNode types: %s\n", strings.Join(scene.SignedNodeTypes(), ", "))
	fmt.Fprintf(&b, "Node keys: %s\n", strings.Join(scene.Vocabulary(), ", "))
	fmt.Fprintf(&b, "Binds: %s\n\n", strings.Join(scene.SignedBinds(), ", "))
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

// interfaceBridger is the optional capability a Driver has when its chat can change the
// interface (serveDriver).
type interfaceBridger interface {
	InterfaceBridge() *uiBridge
}

// noteRefusal counts how many times in a row ui_edit has been refused for the same
// reason, and returns the count. A success resets it (see resetRefusals).
func (b *uiBridge) noteRefusal(text string) int {
	b.mu.Lock()
	defer b.mu.Unlock()
	key := firstLine(text, 300)
	if key == b.lastRefusal {
		b.refusals++
	} else {
		b.lastRefusal, b.refusals = key, 1
	}
	return b.refusals
}

func (b *uiBridge) resetRefusals() {
	b.mu.Lock()
	b.lastRefusal, b.refusals = "", 0
	b.mu.Unlock()
}

// firstLine is the first non-empty line of s, cut to max runes.
func firstLine(s string, max int) string {
	for _, l := range strings.Split(s, "\n") {
		if l = strings.TrimSpace(l); l != "" {
			if r := []rune(l); len(r) > max {
				return string(r[:max]) + "..."
			}
			return l
		}
	}
	return ""
}

// errUIPromptEmpty is the answer to a bare "@ui": there is nothing to do yet.
var errUIPromptEmpty = errors.New("/ui needs a request, for example: /ui give the input bar a rainbow border")

// uiTrigger reads the switch that puts the model in charge of arxi-tui's own interface:
// a line starting with "/ui " followed by a request (not one of the typed /ui commands,
// which the host answers itself), or "@ui" anywhere in the line. It returns the line
// without the switch and whether it was there. It is mandatory on purpose: the same
// words ("the TUI", "the colours") are about the user's project when they build one.
func uiTrigger(text string) (string, bool) {
	t := strings.TrimSpace(text)
	if rest, ok := uiPromptAfterSlash(t); ok {
		return rest, true
	}
	if !strings.Contains(strings.ToLower(t), "@ui") {
		return t, false
	}
	stripped := removeToken(t, "@ui")
	if stripped == t {
		return t, false // "@uiux" or "me@ui.com" is not the switch
	}
	return strings.TrimSpace(stripped), true
}

// removeToken deletes whole-word occurrences of tok, leaving the rest as typed.
func removeToken(s, tok string) string {
	var out strings.Builder
	for i := 0; i < len(s); {
		if strings.EqualFold(s[i:min(len(s), i+len(tok))], tok) &&
			(i == 0 || s[i-1] == ' ' || s[i-1] == '\n' || s[i-1] == '\t') &&
			(i+len(tok) == len(s) || s[i+len(tok)] == ' ' || s[i+len(tok)] == '\n' || s[i+len(tok)] == '\t') {
			i += len(tok)
			if i < len(s) && s[i] == ' ' && (out.Len() == 0 || strings.HasSuffix(out.String(), " ")) {
				i++ // no double space where the switch was
			}
			continue
		}
		out.WriteByte(s[i])
		i++
	}
	return out.String()
}

// uiTypedLine reports whether a /ui line is one of the commands the host answers itself.
// A line starting with a patch verb counts only when the patch parser accepts it, so
// "/ui add a blank line under the input" is a request and "/ui add node below_input {...}"
// is a command.
func uiTypedLine(line string) bool {
	f := strings.Fields(line)
	if len(f) < 2 {
		return false
	}
	switch f[1] {
	case "undo", "reset":
		return len(f) == 2
	case "color":
		_, _, ok := uiColorCommand(line)
		return ok
	case "text", "animate", "key", "menukeys":
		return len(f) >= 3
	}
	for _, v := range patch.Verbs() {
		if v == f[1] {
			_, err := patch.Parse(line)
			return err == nil
		}
	}
	return false
}

// uiPromptAfterSlash reports a "/ui <request>" line: more than a bare "/ui", and whose
// first word is not a typed command. A request that starts with a command word
// ("/ui add ...") is read as that command; "@ui" says it unambiguously.
func uiPromptAfterSlash(t string) (string, bool) {
	rest, ok := strings.CutPrefix(t, "/ui")
	if !ok || rest == "" || (rest[0] != ' ' && rest[0] != '\n') {
		return "", false
	}
	rest = strings.TrimSpace(rest)
	if rest == "" {
		return "", false
	}
	if uiTypedLine(t) {
		return "", false
	}
	return rest, true
}
