package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/michiTrader/arxi_tui/internal/fold"
	"github.com/michiTrader/arxi_tui/internal/scene"
	"github.com/michiTrader/arxi_tui/internal/theme"
)

func writeScreen(t *testing.T, name, body string) {
	t.Helper()
	dir := screensDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, name+".json"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

const replacementScreen = `{"root":{"type":"text","text":"PROFILE HUB"}}`

// With no file the built-in screen is returned and nothing is said.
// Consequence: a user with no profile would see a notice or no screen.
// Remedy: a missing file is the normal case, silent.
func TestLoadScreenWithNoFileIsTheBuiltInOne(t *testing.T) {
	t.Setenv(configDirEnv, t.TempDir())
	doc, notice, err := loadScreen("hub", loadHubScene, true)
	if err != nil || doc == nil || notice != "" {
		t.Fatalf("loadScreen = %v, %q, %v. Consequence: the built-in screen is lost or a notice nags. Remedy: a missing file is silent.", doc, notice, err)
	}
	built, _ := loadHubScene()
	if doc.Name() != built.Name() {
		t.Errorf("the document is not the built-in one")
	}
}

// A valid replacement is the screen shown.
// Consequence: /provider cannot be redesigned without a Go change.
// Remedy: read <settings>/screens/<name>.json before the embedded document.
func TestLoadScreenUsesTheProfilesReplacement(t *testing.T) {
	t.Setenv(configDirEnv, t.TempDir())
	writeScreen(t, "hub", replacementScreen)
	doc, notice, err := loadScreen("hub", loadHubScene, true)
	if err != nil || notice != "" {
		t.Fatalf("loadScreen = %q, %v", notice, err)
	}
	if got := doc.Root.Text; got != "PROFILE HUB" {
		t.Errorf("the screen is %q, want the profile's. Consequence: a profile's screen is ignored. Remedy: prefer a valid file.", got)
	}
}

// A broken replacement never takes the screen away: the built-in one is shown with the reason.
// Consequence: a typo in a profile makes /provider unreachable, and credentials cannot be set.
// Remedy: validate, and fall back to the built-in document with a notice that names the file.
func TestLoadScreenFallsBackWhenTheReplacementIsBroken(t *testing.T) {
	for name, body := range map[string]string{
		"not json":       `{"root":`,
		"unknown bind":   `{"root":{"type":"text","bind":"no.such.value"}}`,
		"empty document": ``,
	} {
		t.Run(name, func(t *testing.T) {
			t.Setenv(configDirEnv, t.TempDir())
			writeScreen(t, "hub", body)
			doc, notice, err := loadScreen("hub", loadHubScene, true)
			if err != nil || doc == nil {
				t.Fatalf("loadScreen = %v, %v. Consequence: a broken profile removes the screen. Remedy: fall back to the built-in one.", doc, err)
			}
			if !strings.Contains(notice, "hub.json") {
				t.Errorf("notice = %q. Consequence: the user is not told why their screen is not shown. Remedy: name the file.", notice)
			}
			built, _ := loadHubScene()
			if doc.Name() != built.Name() {
				t.Errorf("a broken replacement was used")
			}
		})
	}
}

// -raw and -scene never read the folder.
// Consequence: the start-time escape hatch can be painted unusable by a saved screen.
// Remedy: callers pass allow=false for them.
func TestLoadScreenIgnoresTheFolderWhenNotAllowed(t *testing.T) {
	t.Setenv(configDirEnv, t.TempDir())
	writeScreen(t, "hub", replacementScreen)
	doc, notice, err := loadScreen("hub", loadHubScene, false)
	if err != nil || notice != "" {
		t.Fatalf("loadScreen = %q, %v", notice, err)
	}
	if doc.Root.Text == "PROFILE HUB" {
		t.Errorf("the replacement was read though the caller did not allow it")
	}
}

// screenLoop runs the real loop on doc against a hub core, types line, waits for want to be
// drawn (or 30 s), and returns what was drawn.
func screenLoop(t *testing.T, doc *scene.Document, line, want string) string {
	t.Helper()
	core := &modelCore{def: "openai/gpt-4o", models: sampleModels().models}
	drv := &hubTestDriver{testDriver: testDriver{evCh: make(chan fold.Event, 64), seq: 1000}, hub: core}
	return screenLoopWith(t, doc, drv, drv.evCh, line, want)
}

// screenLoopWith is screenLoop for any driver: it types line, waits for want to be
// drawn and returns the screen text.
func screenLoopWith(t *testing.T, doc *scene.Document, drv Driver, evCh <-chan fold.Event, line, want string) string {
	t.Helper()
	return screenLoopEdit(t, doc, drv, evCh, line, want, nil)
}

// screenLoopEdit is screenLoopWith with an edit made while the loop is already running:
// edit runs after the first frame is drawn and before line is typed, which is how a user
// changes a screen's file without restarting.
func screenLoopEdit(t *testing.T, doc *scene.Document, drv Driver, evCh <-chan fold.Event, line, want string, edit func()) string {
	t.Helper()
	t.Setenv(historyEnv, t.TempDir())
	tty := newOpenFakeTTY(100, 24, nil)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- loop(ctx, tty, doc, theme.SOBRIA(), evCh, drv, "") }()
	if edit != nil {
		deadline := time.Now().Add(30 * time.Second)
		for tty.output() == "" && time.Now().Before(deadline) {
			time.Sleep(5 * time.Millisecond)
		}
		edit()
	}
	for _, ev := range append(typed(line), at(20, enterEvent())) {
		time.Sleep(ev.delay)
		tty.events <- ev.event
	}
	deadline := time.Now().Add(30 * time.Second)
	for !strings.Contains(stripANSI(tty.output()), want) && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	time.Sleep(200 * time.Millisecond)
	out := tty.output()
	for _, ev := range exitKeys() {
		time.Sleep(ev.delay)
		tty.events <- ev.event
	}
	select {
	case <-done:
	case <-time.After(30 * time.Second):
		t.Fatal("the loop did not quit")
	}
	return stripANSI(out)
}

// The real loop shows the profile's screen for /provider.
// Consequence: the loader works in a unit test and the loop still draws the embedded screen.
// Remedy: the loop must load its screens through loadScreen.
func TestLoopShowsTheProfilesProviderScreen(t *testing.T) {
	t.Setenv(configDirEnv, t.TempDir())
	writeScreen(t, "hub", replacementScreen)
	out := screenLoop(t, builtinDoc(t), "/provider", "PROFILE HUB")
	if !strings.Contains(out, "PROFILE HUB") {
		t.Errorf("/provider did not show the profile's screen:\n%s", out)
	}
}

// With no file the loop shows the built-in providers screen, not the replacement text.
func TestLoopShowsTheBuiltInProviderScreenWithoutAProfile(t *testing.T) {
	t.Setenv(configDirEnv, t.TempDir())
	out := screenLoop(t, builtinDoc(t), "/provider", "· providers")
	if strings.Contains(out, "PROFILE HUB") {
		t.Errorf("a replacement text appeared with no profile file")
	}
	if !strings.Contains(out, "· providers") {
		t.Errorf("the built-in providers screen was not shown. Consequence: /provider shows nothing without a profile. Remedy: fall back to the embedded document.\n%s", out)
	}
}

// A scene booted with -scene (or -raw) never reads the profile's screens.
// Consequence: the start-time escape hatch can be painted unusable by a saved screen.
// Remedy: only the everyday interface (persistsScene) reads <settings>/screens.
func TestLoopWithAnExplicitSceneIgnoresTheProfilesScreens(t *testing.T) {
	t.Setenv(configDirEnv, t.TempDir())
	writeScreen(t, "hub", replacementScreen)
	explicit, err := scene.ParseDocument([]byte(factorySobria)) // not the default scene by name
	if err != nil {
		t.Fatal(err)
	}
	out := screenLoop(t, explicit, "/provider", "· providers")
	if strings.Contains(out, "PROFILE HUB") || !strings.Contains(out, "· providers") {
		t.Errorf("an explicit scene showed the profile's screen or none:\n%s", out)
	}
}
