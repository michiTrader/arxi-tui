//go:build !windows

package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/michiTrader/arxi_tui/internal/defaultscene"
	"github.com/michiTrader/arxi_tui/internal/driver"
	"github.com/michiTrader/arxi_tui/internal/fold"
	"github.com/michiTrader/arxi_tui/internal/scene"
	"github.com/michiTrader/arxi_tui/internal/theme"
)

func TestAgentPackageListIsBoundedAndEachIDAppearsOnce(t *testing.T) {
	var many []string
	for i := 0; i < maxPackageOps+1; i++ {
		many = append(many, `{"remove":"p`+string(rune('a'+i))+`"}`)
	}
	cases := map[string]struct{ args, want string }{
		"too many entries": {`{"packages":[` + strings.Join(many, ",") + `]}`, "the most in one change"},
		"same id twice":    {`{"packages":[{"disable":"one"},{"remove":"one"}]}`, "appears twice"},
	}
	for name, c := range cases {
		r := newPkgRig(t, "full access")
		if res := r.edit(t, putArgs("one", "one", ""), true); !res.OK {
			t.Fatal(res)
		}
		res := r.edit(t, c.args, true)
		if res.OK || !strings.Contains(res.Text, c.want) {
			t.Errorf("%s: want a refusal containing %q, got %+v", name, c.want, res)
		}
		if pkgFile(t, "one") == "" {
			t.Errorf("%s: the package was removed by a refused change", name)
		}
	}
}

// A folder in the packages folder that is not a loaded package (no package.json yet, or
// one that does not parse) still owns its id: a document with the same id would sit
// beside it and one of the two would shadow the other on the next load.
func TestAgentCannotPutADocumentBesideAFolderThatIsNotLoaded(t *testing.T) {
	r := newPkgRig(t, "full access")
	if err := os.MkdirAll(filepath.Join(userPackagesDir(), "clash"), 0o700); err != nil {
		t.Fatal(err)
	}
	if _, ok := loadAllPackages("", behaviour{}).find("clash"); ok {
		t.Fatal("the empty folder is loaded as a package, so this test would prove nothing")
	}
	res := r.edit(t, putArgs("clash", "clash", ""), true)
	if res.OK || !strings.Contains(res.Text, "already a folder") {
		t.Fatalf("a document was put beside a folder with its id: %+v", res)
	}
	if pkgFile(t, "clash") != "" {
		t.Error("the document was written")
	}
}

// loopWithBridge runs the real loop on doc with a bridge, calls ui_edit with args from
// outside the loop (as the chat turn does) and returns the result and what was drawn.
// during runs inside the question the user is asked, to play a change made meanwhile.
func loopWithBridge(t *testing.T, doc *scene.Document, args string, during func()) (driver.ClientToolResult, string) {
	t.Helper()
	cfg := t.TempDir()
	t.Setenv(configDirEnv, cfg)
	t.Setenv(historyEnv, t.TempDir())
	t.Setenv(sessionsEnv, "")
	br := newUIBridge()
	br.setMode("full access")
	drv := &bridgeDriver{testDriver: testDriver{evCh: make(chan fold.Event, 64), seq: 1000}, br: br}
	result := make(chan driver.ClientToolResult, 1)
	go func() {
		time.Sleep(400 * time.Millisecond)
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		result <- br.call(ctx, driver.ClientToolCall{Name: uiToolEdit, Arguments: json.RawMessage(args)},
			func(driver.Approval) bool {
				if during != nil {
					during()
				}
				return true
			})
	}()
	script := []scheduledEvent{{1500 * time.Millisecond, ctrlCharEvent('c')}}
	tty := newFakeTTY(100, 30, script)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := loop(ctx, tty, doc, theme.SOBRIA(), drv.evCh, drv, ""); err != nil {
		t.Fatalf("loop: %v", err)
	}
	return <-result, tty.output()
}

// A scene started with -scene is the user's test file, not their everyday interface:
// packages live with the everyday one, so the loop refuses and writes nothing.
func TestLoopRefusesAPackageChangeOnASceneStartedWithDashScene(t *testing.T) {
	doc, err := scene.ParseNamed("custom-scene.json", defaultscene.JSON)
	if err != nil {
		t.Fatal(err)
	}
	res, _ := loopWithBridge(t, doc, putArgs("greet", "greet", ""), nil)
	if res.OK || !strings.Contains(res.Text, "started with -scene") {
		t.Fatalf("a package was written for a scene that does not persist: %+v", res)
	}
	if pkgFile(t, "greet") != "" {
		t.Error("the document was written")
	}
}

// The folder moves on while the question is open: the loop, not only the bridge, holds
// the change back, so the user's hand edit is not overwritten.
func TestLoopRefusesAPackageChangeWhenTheFolderMovedWhileAsking(t *testing.T) {
	handEdit := ""
	res, _ := loopWithBridge(t, builtinDoc(t), putArgs("greet", "greet", ""), func() {
		handEdit = pkgDoc("greet", echoPkg)
		write(t, filepath.Join(userPackagesDir(), "greet.json"), handEdit, 0o644)
	})
	if res.OK || !strings.Contains(res.Text, "changed while the question was open") {
		t.Fatalf("a stale package change went through the loop: %+v", res)
	}
	if got := pkgFile(t, "greet"); got != handEdit {
		t.Errorf("the user's hand edit was overwritten:\n%s", got)
	}
}
