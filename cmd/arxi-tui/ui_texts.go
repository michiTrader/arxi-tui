package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"unicode"
	"unicode/utf8"

	"github.com/michiTrader/arxi_tui/internal/fold"
)

// This file is the words half of the user's interface. The scene document says what
// is drawn and where, the theme says what each token looks like, and the texts say
// what the host writes in its own menus and screens: the description of every command
// in the / menu, the hint under /effort, /mode and /style, the whole of /team's copy,
// the key legends.
//
// # Why this layer exists
//
// Those words were string constants in Go. The interface the user can change was a
// scene document plus colours, so a user who asked to see /team in Spanish was told it
// could not be done: the sentence was not in anything the agent could reach. That is
// the product contradicting itself. The thesis (docs/PLAN.md) is that the factory
// interface is written with the same format the user has; a sentence the factory
// writes in Go and the user cannot touch is a hole in that equality.
//
// # Why a registry and not free-form overrides
//
// Every editable sentence has a key and a default, declared once below. A key that
// is not declared is refused with its name, exactly as an unknown colour token is: a
// typo would otherwise change nothing and report success. The registry is also what
// ui_guide lists, so the model reads the live keys and current values instead of
// guessing them, and a test holds every call site to a declared key.
//
// # Why the values are checked
//
// A text reaches the terminal verbatim. A value carrying an escape sequence could
// move the cursor, retitle the window or hide what the next line says, and the value
// comes from a model. So a control character is refused, newline excepted and only
// for the keys that are paragraphs: a newline inside a one-line title or hint would
// break the layout it sits in.

// textDef is one editable sentence.
type textDef struct {
	key string
	// def is the factory sentence. It is what an interface with no override shows, so
	// every golden and every test that reads the defaults is unchanged by this layer.
	def string
	// role says where it appears, for the guide; empty falls back to the key.
	role string
	// paragraph keys may hold newlines; the rest are one line.
	paragraph bool
}

// maxTextLen bounds one override. The longest factory paragraph is about 300 bytes; a
// value many times that is a mistake (a pasted document) and would swamp the screen.
const maxTextLen = 2000

var (
	textRegistry []textDef
	textIndex    map[string]textDef
)

func init() {
	add := func(key, def, role string, paragraph bool) {
		textRegistry = append(textRegistry, textDef{key: key, def: def, role: role, paragraph: paragraph})
	}
	add("slash.hint", slashMenuHint, "key legend on the bottom row while the / menu is open", false)
	for _, c := range fold.Commands {
		add("command."+c.Name, c.Description, "description of /"+c.Name+" in the / menu", false)
	}
	for _, level := range effortLevelOrder {
		add("effort."+level, effortHints[level], "meaning of the \""+level+"\" level in the /effort menu", false)
	}
	for _, m := range agentModes {
		add("mode."+m.name, m.hint, "meaning of the \""+m.name+"\" mode in the /mode menu", false)
	}
	for _, o := range promptStyles {
		add("style."+o.name, o.hint, "meaning of \""+o.name+"\" in the /style menu", false)
	}
	add("team.title", "Agents & teams", "title of the /team screen", false)
	add("team.count", "{count} in "+teamDir+"/", "what follows the /team title once agents exist; {count} is the number of files", false)
	add("team.reading", "Reading "+teamDir+"/ …", "/team while it reads the agents folder", false)
	add("team.new_agent.label", "＋ New agent…", "the /team row that creates an agent", false)
	add("team.new_agent.status", "one worker", "the note beside that row", false)
	add("team.new_agent.detail", newAgentDetail, "what /team says when that row is highlighted", true)
	add("team.new_team.label", "＋ New team…", "the /team row that creates a team", false)
	add("team.new_team.status", "agents working together", "the note beside that row", false)
	add("team.new_team.detail", newTeamDetail, "what /team says when that row is highlighted", true)
	add("team.no_agents", noAgentsYet, "/team, on the team row, when there is no agent to build one from", true)
	add("team.needs_core", "needs a newer core", "the note on a /team row the connected core cannot do", false)
	add("team.empty", emptyTeams, "/team when the agents folder is empty", true)
	add("team.item.run", "Press enter to run it.", "/team, on an agent or team", false)
	add("team.item.stages", " Press e to change how its stages finish or how long they may take.", "/team, on a team with stages", false)
	add("team.item.members", " Press m to change a member's model, role or tools, and w to change what wakes a member.", "/team, on an agent or team with members", false)
	add("team.hint", teamHint, "key legend of the /team screen", false)
	add("team.form_hint", teamFormHint, "key legend while a /team form is open", false)
	add("flow.hint", flowHint, "key legend of the /flow screen", false)
	add("flow.ask_hint", flowAskHint, "key legend of /flow while an approval waits", false)
	add("flow.reason_hint", flowReasonHint, "key legend of /flow while a rejection reason is typed", false)
	add("auto.hint", autoHint, "key legend of the /auto screen", false)
	add("auto.form_hint", autoFormHint, "key legend while an /auto form is open", false)

	textIndex = make(map[string]textDef, len(textRegistry))
	for _, d := range textRegistry {
		if _, dup := textIndex[d.key]; dup {
			// A duplicate would make the later definition silently win; it is a
			// programming error, caught the first time anything starts.
			panic("ui_texts: duplicate text key " + d.key)
		}
		textIndex[d.key] = d
	}
}

// userTexts is the user's own word layer: key -> sentence. Empty means the factory words.
type userTexts map[string]string

func (u userTexts) clone() userTexts {
	out := make(userTexts, len(u))
	for k, v := range u {
		out[k] = v
	}
	return out
}

// activeTexts is the layer on screen. The screens that read it (publish methods, menu
// builders) have no access to the loop's variables, and threading a parameter through
// every one of them would put a texts argument in forty signatures that never otherwise
// change. A guarded package value is the honest cost: the loop sets it whenever the
// layer changes, and nothing else writes it.
var (
	activeMu    sync.RWMutex
	activeTexts userTexts
)

func setActiveTexts(u userTexts) {
	activeMu.Lock()
	activeTexts = u.clone()
	activeMu.Unlock()
}

// uiText is the sentence for key: the user's if they changed it, otherwise the factory's.
// A key that was never declared reads as empty rather than panicking, because a missing
// sentence on a screen is a bug a test finds (TestEveryTextKeyUsedByTheHostIsDeclared),
// not a reason to end the session.
func uiText(key string) string {
	activeMu.RLock()
	v, ok := activeTexts[key]
	activeMu.RUnlock()
	if ok {
		return v
	}
	return textIndex[key].def
}

// uiTextWith is uiText with {name} placeholders filled from pairs (name, value, ...).
func uiTextWith(key string, pairs ...string) string {
	s := uiText(key)
	for i := 0; i+1 < len(pairs); i += 2 {
		s = strings.ReplaceAll(s, "{"+pairs[i]+"}", pairs[i+1])
	}
	return s
}

// validText refuses a value that would not draw safely or would break the layout it
// sits in. The message names the key so the repair turn knows what to fix.
func validText(def textDef, val string) error {
	if !utf8.ValidString(val) {
		return fmt.Errorf("text %q is not valid UTF-8", def.key)
	}
	if len(val) > maxTextLen {
		return fmt.Errorf("text %q is %d bytes; the most one text may hold is %d", def.key, len(val), maxTextLen)
	}
	for _, r := range val {
		if r == '\n' {
			if !def.paragraph {
				return fmt.Errorf("text %q is one line (it is a title, a hint or a label); remove the line break", def.key)
			}
			continue
		}
		if r == '\t' {
			continue
		}
		if unicode.IsControl(r) {
			return fmt.Errorf("text %q contains the control character U+%04X; the terminal would act on it instead of showing it", def.key, r)
		}
	}
	return nil
}

// applyTexts returns base with the requested sentences applied. An empty value takes the
// user's sentence off that key, back to the factory one.
func applyTexts(base userTexts, edits map[string]string) (userTexts, error) {
	out := base.clone()
	keys := make([]string, 0, len(edits))
	for k := range edits {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		def, ok := textIndex[k]
		if !ok {
			return nil, fmt.Errorf("text key %q does not exist; the keys and what each one says are listed in ui_guide", k)
		}
		val := edits[k]
		// Only the empty string puts the factory sentence back. A value of spaces is
		// a deliberate blank: it is how a user hides a hint they do not want.
		if val == "" {
			delete(out, k)
			continue
		}
		if err := validText(def, val); err != nil {
			return nil, err
		}
		out[k] = val
	}
	return out, nil
}

// textDiff is a change of texts as the conversation draws a diff: a removed and an added
// line per key, with no line number.
func textDiff(before, after userTexts, changed []string) string {
	var b strings.Builder
	show := func(u userTexts, key string) string {
		if v, ok := u[key]; ok {
			return strconvQuote(v)
		}
		return strconvQuote(textIndex[key].def) + " (factory)"
	}
	for _, k := range changed {
		was, now := show(before, k), show(after, k)
		if was == now {
			continue
		}
		fmt.Fprintf(&b, "      - %s: %s\n      + %s: %s\n", k, was, k, now)
	}
	return b.String()
}

// strconvQuote keeps a paragraph on one diff line: a diff row is one line, and a raw
// newline would be drawn as several unmarked ones.
func strconvQuote(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

func sameTexts(a, b userTexts) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if w, ok := b[k]; !ok || w != v {
			return false
		}
	}
	return true
}

// textsGuide lists every key with its current sentence for ui_guide, so the model reads
// the live words rather than a description of them. Long paragraphs are cut: the model
// needs to know which key says what, and can ask for a rewrite of the whole value.
func textsGuide() string {
	var b strings.Builder
	for _, d := range textRegistry {
		role := d.role
		if role == "" {
			role = d.key
		}
		cur := uiText(d.key)
		line := strings.ReplaceAll(cur, "\n", " / ")
		if r := []rune(line); len(r) > 70 {
			line = string(r[:70]) + "…"
		}
		fmt.Fprintf(&b, "  %s = %s  # %s\n", d.key, strconvQuote(line), role)
	}
	return b.String()
}

// ---- keeping the user's texts --------------------------------------------------

// userTextsPath is where the user's texts are kept, "" when there is nowhere.
func userTextsPath() string {
	d := configDir()
	if d == "" {
		return ""
	}
	return filepath.Join(d, "texts.json")
}

// loadUserTexts reads the user's texts. A missing file is the factory words; a damaged
// one is reported and ignored, so one bad edit cannot cost the interface.
func loadUserTexts() (userTexts, error) {
	path := userTextsPath()
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
	return parseUserTexts(path, data)
}

func parseUserTexts(name string, data []byte) (userTexts, error) {
	if len(strings.TrimSpace(string(data))) == 0 {
		return nil, nil
	}
	var raw map[string]string
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("%s: %w", name, err)
	}
	// A key this version no longer declares is dropped, not refused: a saved file
	// outliving a release that renamed a sentence must not cost the user the others.
	out := userTexts{}
	for k, v := range raw {
		def, ok := textIndex[k]
		if !ok {
			continue
		}
		if err := validText(def, v); err != nil {
			return nil, fmt.Errorf("%s: %w", name, err)
		}
		out[k] = v
	}
	if len(out) == 0 {
		return nil, nil
	}
	return out, nil
}

// encodeUserTexts writes the layer sorted, so the file a person opens reads the same
// every time and a diff of it shows only what they changed.
func encodeUserTexts(u userTexts) []byte {
	keys := make([]string, 0, len(u))
	for k := range u {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	b.WriteString("{\n")
	for i, k := range keys {
		kb, _ := json.Marshal(k)
		vb, _ := json.Marshal(u[k])
		fmt.Fprintf(&b, "  %s: %s", kb, vb)
		if i < len(keys)-1 {
			b.WriteString(",")
		}
		b.WriteString("\n")
	}
	b.WriteString("}\n")
	return []byte(b.String())
}

// uiTextCommand reads `/ui text <key> [sentence…]`: with a sentence it sets the key, with
// none it takes the user's sentence off. The sentence keeps its spacing, which matters
// for a key legend that starts with a margin.
func uiTextCommand(input string) (key, value string, ok bool) {
	rest := strings.TrimLeft(input, " \t")
	f := strings.Fields(rest)
	if len(f) < 3 || f[0] != "/ui" || f[1] != "text" {
		return "", "", false
	}
	key = f[2]
	after := rest[strings.Index(rest, key)+len(key):]
	return key, strings.TrimPrefix(after, " "), true
}

// slashMenuHint is the legend on the bottom row while the / menu is open.
const slashMenuHint = "  ↑↓ navigate · tab category · enter open · esc close"

// describeCommands puts the user's own description on each command of the / menu. It
// copies: fold.Commands is the factory registry the fold and the tests share, and a
// user's sentence must never leak into it.
func describeCommands(in []fold.SlashMatch) []fold.SlashMatch {
	out := make([]fold.SlashMatch, len(in))
	for i, m := range in {
		out[i] = m
		if d := uiText("command." + m.Name); d != "" {
			out[i].Description = d
		}
	}
	return out
}
