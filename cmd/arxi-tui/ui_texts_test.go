package main

import (
	"context"
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/michiTrader/arxi_tui/internal/driver"
	"github.com/michiTrader/arxi_tui/internal/fold"
	"github.com/michiTrader/arxi_tui/internal/theme"
)

// withTexts installs a user wording layer for one test and puts the factory words
// back afterwards: the layer is package state, so a test that leaked it would change
// every golden that runs after it.
func withTexts(t *testing.T, u userTexts) {
	t.Helper()
	setActiveTexts(u)
	t.Cleanup(func() { setActiveTexts(nil) })
}

// The order that motivated this layer: the /team screen in another language, by changing the
// interface rather than by being told to go edit source files.
func TestTeamScreenIsReworded(t *testing.T) {
	withTexts(t, userTexts{
		"team.title":            "Crews and agents",
		"team.new_agent.label":  "+ Add an agent...",
		"team.new_agent.detail": "Add an agent: a name, a model, some tools.",
		"team.empty":            "The agents/ folder is still empty.",
		"team.hint":             "up/down pick · enter open · esc close",
	})
	ts := &teamScreen{canAgent: true, canTeam: true}
	var st fold.State
	ts.publish(&st)
	if st.HubTitle != "Crews and agents" {
		t.Errorf("title = %q; the /team title did not follow the user's wording.\nConsequence: asking the agent to translate /team reports success and changes nothing, the defect that started this.\nRemedy: read every /team sentence through uiText.", st.HubTitle)
	}
	if len(st.HubRows) == 0 || st.HubRows[0].Label != "+ Add an agent..." {
		t.Errorf("rows = %+v; the first row label must be the reworded one", st.HubRows)
	}
	if !strings.Contains(st.HubDetail, "Add an agent") || !strings.Contains(st.HubDetail, "is still empty") {
		t.Errorf("detail = %q; both the row's explanation and the empty-folder paragraph must be the reworded ones", st.HubDetail)
	}
	if st.HubHint != "up/down pick · enter open · esc close" {
		t.Errorf("hint = %q", st.HubHint)
	}
}

// With no override every sentence is byte-for-byte what it was, so no golden moves.
func TestFactoryWordsAreUnchangedWithoutOverrides(t *testing.T) {
	withTexts(t, nil)
	for _, d := range textRegistry {
		if got := uiText(d.key); got != d.def {
			t.Errorf("%s reads %q with no override; want the factory %q.\nConsequence: a default golden moved, which AGENTS.md treats as a review event.\nRemedy: the factory layer must be the declared default.", d.key, got, d.def)
		}
	}
	ts := &teamScreen{canAgent: true, canTeam: true}
	var st fold.State
	ts.publish(&st)
	if st.HubTitle != "Agents & teams" || st.HubHint != teamHint {
		t.Errorf("factory /team = %q / %q", st.HubTitle, st.HubHint)
	}
}

// The slash menu is the first thing the user asked to be able to restyle.
func TestCommandDescriptionsAreReworded(t *testing.T) {
	withTexts(t, userTexts{"command.effort": "How hard the model thinks"})
	got := describeCommands(fold.FilterSlashMatches("effort"))
	if len(got) != 1 || got[0].Description != "How hard the model thinks" {
		t.Fatalf("got %+v; the / menu must show the user's description", got)
	}
	// The unfiltered list is fold.Commands itself (FilterSlashMatches returns it as is
	// when nothing is typed), so it is the only input on which a describer that writes
	// in place corrupts the factory registry. A filtered list is a fresh slice and would
	// let that defect through, as the first version of this test did.
	factory := append([]fold.SlashMatch(nil), fold.Commands...)
	shown := describeCommands(fold.FilterSlashMatches(""))
	reworded := false
	for _, m := range shown {
		if m.Name == "effort" && m.Description == "How hard the model thinks" {
			reworded = true
		}
	}
	if !reworded {
		t.Error("the full / menu did not show the user's description")
	}
	for i, c := range fold.Commands {
		if c != factory[i] {
			t.Fatalf("fold.Commands[%d] (%s) is now %q.\nConsequence: the factory registry, shared by the fold and every test, is changed for the rest of the process, and /ui reset could not bring the words back.\nRemedy: describeCommands must copy.", i, c.Name, c.Description)
		}
	}
}

func TestEffortModeAndStyleMenusAreReworded(t *testing.T) {
	withTexts(t, userTexts{"effort.low": "barely", "mode.plan": "reads only", "style.band": "on a block"})
	for _, m := range effortMenuData(nil, false, "") {
		if m.Name == "low" && m.Provider != "barely" {
			t.Errorf("effort low = %q", m.Provider)
		}
	}
	for _, m := range modeMenuData("ask") {
		if m.Name == "plan" && m.Provider != "reads only" {
			t.Errorf("mode plan = %q", m.Provider)
		}
	}
	for _, m := range styleMenuData("bar") {
		if m.Name == "band" && m.Provider != "on a block" {
			t.Errorf("style band = %q", m.Provider)
		}
	}
}

// An unknown key is refused with its name, like an unknown colour token: a typo would
// otherwise change nothing and report success.
func TestARefusedTextNamesWhatToFix(t *testing.T) {
	for _, tc := range []struct {
		name string
		edit map[string]string
		want string
	}{
		{"unknown key", map[string]string{"team.titel": "x"}, "team.titel"},
		{"escape sequence", map[string]string{"team.title": "a\x1b[2Jb"}, "U+001B"},
		{"line break in a title", map[string]string{"team.title": "a\nb"}, "one line"},
		{"too long", map[string]string{"team.new_agent.detail": strings.Repeat("x", maxTextLen+1)}, "most one text"},
		{"bad utf8", map[string]string{"team.title": "a\xffb"}, "UTF-8"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := applyTexts(nil, tc.edit)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err = %v; want it to name %q so the repair turn has something to fix", err, tc.want)
			}
		})
	}
	// A paragraph may hold line breaks; that is what makes it a paragraph.
	if _, err := applyTexts(nil, map[string]string{"team.new_agent.detail": "uno\n\ndos"}); err != nil {
		t.Errorf("a paragraph key refused a line break: %v", err)
	}
}

// Empty restores the factory sentence; blanks hide it. They are different requests.
func TestEmptyRestoresAndSpacesHide(t *testing.T) {
	u, err := applyTexts(nil, map[string]string{"team.hint": "x"})
	if err != nil {
		t.Fatal(err)
	}
	back, err := applyTexts(u, map[string]string{"team.hint": ""})
	if err != nil || len(back) != 0 {
		t.Fatalf("empty gave %v, %v; it must remove the override", back, err)
	}
	hidden, err := applyTexts(nil, map[string]string{"team.hint": " "})
	if err != nil || hidden["team.hint"] != " " {
		t.Fatalf("a blank gave %v, %v; it must be kept as a deliberate blank", hidden, err)
	}
}

func TestPlaceholdersAreFilled(t *testing.T) {
	withTexts(t, userTexts{"team.count": "{count} files"})
	ts := &teamScreen{items: []teamItem{{Name: "a", Info: featureTeamInfo()}, {Name: "b", Info: featureTeamInfo()}}}
	var st fold.State
	ts.publish(&st)
	if !strings.HasSuffix(st.HubTitle, "2 files") {
		t.Errorf("title = %q; {count} must be filled in", st.HubTitle)
	}
}

func TestTextsAreSavedBootedUndoneAndReset(t *testing.T) {
	t.Setenv(configDirEnv, t.TempDir())
	doc := builtinDoc(t)
	words, err := applyTexts(nil, map[string]string{"team.title": "Crews and agents"})
	if err != nil {
		t.Fatal(err)
	}
	if err := saveInterface(doc, nil, words, behaviour{}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(userScenePath()); !os.IsNotExist(err) {
		t.Error("a words-only change saved a copy of the built-in layout; the user would stop receiving layout fixes")
	}
	got, err := loadUserTexts()
	if err != nil || got["team.title"] != "Crews and agents" {
		t.Fatalf("saved texts read back as %v, %v", got, err)
	}
	_, _, back, _, err := undoInterface()
	if err != nil || len(back) != 0 {
		t.Fatalf("undo gave %v, %v; it must return the factory words", back, err)
	}
	if _, err := os.Stat(userTextsPath()); !os.IsNotExist(err) {
		t.Error("undo left texts.json on disk; the next session would still be translated")
	}
	_, _, again, _, err := undoInterface()
	if err != nil || again["team.title"] != "Crews and agents" {
		t.Fatalf("a second undo gave %v, %v; it must bring the words back", again, err)
	}
	if _, _, w, _, err := resetInterface(); err != nil || len(w) != 0 {
		t.Fatalf("reset gave %v, %v", w, err)
	}
	if _, err := os.Stat(userTextsPath()); !os.IsNotExist(err) {
		t.Error("/ui reset left the saved texts on disk")
	}
	// A reset must also be undoable: it is never the end of the user's work.
	if _, _, w, _, err := undoInterface(); err != nil || w["team.title"] != "Crews and agents" {
		t.Errorf("undo after reset gave %v, %v; the reset must keep what it removed", w, err)
	}
}

// One undo returns the layout, colours and words of the same moment.
func TestOneUndoRestoresLayoutColoursAndWordsTogether(t *testing.T) {
	t.Setenv(configDirEnv, t.TempDir())
	doc := builtinDoc(t)
	cols, _ := applyColors(nil, theme.SOBRIA(), map[string]string{"markdown.code": "fg=magenta"})
	words, _ := applyTexts(nil, map[string]string{"team.title": "Crews"})
	if err := saveInterface(doc, cols, words, behaviour{}); err != nil {
		t.Fatal(err)
	}
	if err := saveInterface(doc, cols, nil, behaviour{}); err != nil { // the words are taken off
		t.Fatal(err)
	}
	_, c, w, _, err := undoInterface()
	if err != nil || c["markdown.code"].String() != "fg=magenta" || w["team.title"] != "Crews" {
		t.Fatalf("undo gave colours %v, words %v, %v; it must bring back both of the earlier state", c, w, err)
	}
}

func TestABrokenTextsFileIsReportedAndKeyAfterRenameIsDropped(t *testing.T) {
	if _, err := parseUserTexts("texts.json", []byte(`{"team.title": `)); err == nil || !strings.Contains(err.Error(), "texts.json") {
		t.Errorf("err = %v; a damaged file must be named", err)
	}
	got, err := parseUserTexts("texts.json", []byte(`{"team.title":"A","gone.key":"B"}`))
	if err != nil || len(got) != 1 || got["team.title"] != "A" {
		t.Errorf("got %v, %v; a key a newer release dropped must not cost the user the others", got, err)
	}
	if _, err := parseUserTexts("texts.json", []byte(`{"team.title":"a\u001b[2J"}`)); err == nil {
		t.Error("a saved file with an escape sequence was accepted; it is read at every boot")
	}
}

func TestTextCommandKeepsTheSpacing(t *testing.T) {
	k, v, ok := uiTextCommand("/ui text slash.hint   up/down pick")
	if !ok || k != "slash.hint" || v != "  up/down pick" {
		t.Errorf("got %q %q %v; a legend that starts with a margin must keep it", k, v, ok)
	}
	// Like /ui color <token> with no style, a key alone takes the user's sentence off:
	// it is the only way to get back to the factory words by hand.
	if k, v, ok := uiTextCommand("/ui text team.hint"); !ok || k != "team.hint" || v != "" {
		t.Errorf("got %q %q %v; a key alone must restore the factory sentence", k, v, ok)
	}
	if _, _, ok := uiTextCommand("/ui color markdown.code fg=red"); ok {
		t.Error("a colour command read as a text command")
	}
}

// The guide is the model's only way to learn the keys. If a key is missing from it, the
// model cannot translate that sentence and says it cannot be done, the original defect.
func TestTheGuideListsEveryTextKeyWithItsCurrentWords(t *testing.T) {
	withTexts(t, userTexts{"team.title": "Crews and agents"})
	r := newBridgeRig(t, "ask")
	res := r.b.call(context.Background(), driver.ClientToolCall{Name: uiToolGuide}, nil)
	for _, d := range textRegistry {
		if !strings.Contains(res.Text, "  "+d.key+" = ") {
			t.Errorf("the guide lacks the key %s.\nConsequence: the model cannot reword it and reports the interface as closed.\nRemedy: textsGuide must list the whole registry.", d.key)
		}
	}
	if !strings.Contains(res.Text, `team.title = "Crews and agents"`) {
		t.Error("the guide shows the factory words for a key the user changed; it must show the live ones")
	}
	defs, _ := json.Marshal(r.b.definitions())
	if len(defs) > 1900 {
		t.Errorf("the tool definitions are %d bytes and ride with every question; keep the detail in ui_guide", len(defs))
	}
}

func TestAgentRewordsTheInterfaceAfterAsking(t *testing.T) {
	t.Setenv(configDirEnv, t.TempDir())
	r := newBridgeRig(t, "ask")
	var applied userTexts
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		select {
		case req := <-r.b.apply:
			applied = req.texts
			req.done <- nil
		case <-ctx.Done():
		}
	}()
	res := r.b.call(ctx, driver.ClientToolCall{Name: uiToolEdit, Arguments: json.RawMessage(
		`{"texts":{"team.title":"Crews and agents"},"summary":"/team reworded"}`)},
		func(a driver.Approval) bool { r.asked = append(r.asked, a); return true })
	if !res.OK || applied["team.title"] != "Crews and agents" {
		t.Fatalf("res=%+v applied=%v", res, applied)
	}
	if len(r.asked) != 1 || !strings.Contains(r.asked[0].Diff, "team.title") || !strings.Contains(r.asked[0].Diff, "Crews and agents") {
		t.Fatalf("asked %+v; the user must see the words that will change before they change", r.asked)
	}
}

func TestADeclinedOrRefusedRewordingChangesNothing(t *testing.T) {
	r := newBridgeRig(t, "ask")
	res := r.b.call(context.Background(), driver.ClientToolCall{Name: uiToolEdit, Arguments: json.RawMessage(
		`{"texts":{"team.title":"x"}}`)}, func(driver.Approval) bool { return false })
	if res.OK || !strings.Contains(res.Text, "did not allow") {
		t.Errorf("a declined rewording gave %+v", res)
	}
	r = newBridgeRig(t, "plan")
	if res := r.b.call(context.Background(), driver.ClientToolCall{Name: uiToolEdit, Arguments: json.RawMessage(
		`{"texts":{"team.title":"x"}}`)}, nil); res.OK || !strings.Contains(res.Text, "plan mode") {
		t.Errorf("plan mode allowed a rewording: %+v", res)
	}
	r = newBridgeRig(t, "full access")
	if res := r.b.call(context.Background(), driver.ClientToolCall{Name: uiToolEdit, Arguments: json.RawMessage(
		`{"texts":{"team.nope":"x"}}`)}, nil); res.OK || !strings.Contains(res.Text, "team.nope") {
		t.Errorf("an unknown key was accepted: %+v", res)
	}
}

// The registry must be the whole of what the host says through uiText, and every
// sentence the factory writes into a menu or screen must have a key. The first half is
// checked by reading the source: a call with a literal key nobody declared reads as
// empty on screen, which no other test would notice.
func TestEveryTextKeyUsedByTheHostIsDeclared(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	fset := token.NewFileSet()
	used := map[string]bool{}
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		af, err := parser.ParseFile(fset, f, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		ast.Inspect(af, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			id, ok := call.Fun.(*ast.Ident)
			if !ok || (id.Name != "uiText" && id.Name != "uiTextWith") || len(call.Args) == 0 {
				return true
			}
			switch a := call.Args[0].(type) {
			case *ast.BasicLit:
				if k, err := strconv.Unquote(a.Value); err == nil {
					used[k] = true
					if _, ok := textIndex[k]; !ok {
						t.Errorf("%s uses text key %q which is not declared.\nConsequence: the sentence draws as empty.\nRemedy: add it to the registry in ui_texts.go.", fset.Position(call.Pos()), k)
					}
				}
			case *ast.BinaryExpr:
				// "prefix." + name: the family is checked by the registry itself.
			}
			return true
		})
	}
	// Every declared key must be read by something, or it is a promise the interface
	// does not keep: the model would change it and nothing on screen would move.
	prefixes := []string{"command.", "effort.", "mode.", "style.", "tool.title."}
	for _, d := range textRegistry {
		if used[d.key] {
			continue
		}
		family := false
		for _, p := range prefixes {
			if strings.HasPrefix(d.key, p) {
				family = true
			}
		}
		if !family {
			t.Errorf("text key %s is declared but nothing reads it.\nConsequence: a user, or the model, rewords it and nothing changes, and the change is reported as made.\nRemedy: read it through uiText where the sentence is drawn, or delete the key.", d.key)
		}
	}
}

// The word layer is not allowed to widen the panic gesture: a sentence is only ever drawn.
func TestEveryDeclaredSentenceIsPlainText(t *testing.T) {
	for _, d := range textRegistry {
		if err := validText(d, d.def); err != nil {
			t.Errorf("the factory sentence for %s fails its own validation: %v.\nConsequence: /ui text <key> with the factory words is refused, so a user cannot even restore it by hand.\nRemedy: fix the sentence or the key's paragraph flag.", d.key, err)
		}
	}
}
