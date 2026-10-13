package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// otherProject is a second project the first one's model has no business in, unless the
// user opens it.
func otherProject(t *testing.T) (dir, file string) {
	t.Helper()
	d := t.TempDir()
	if err := os.MkdirAll(filepath.Join(d, "src", "pkg"), 0o755); err != nil {
		t.Fatal(err)
	}
	for name, body := range map[string]string{"pyproject.toml": "x\n", "src/pkg/_tools.py": "def _create_glyph(): pass\n"} {
		if err := os.WriteFile(filepath.Join(d, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	real, _ := filepath.EvalSymlinks(d)
	return real, filepath.Join(real, "src", "pkg", "_tools.py")
}

type folderRig struct {
	f     *fakeLLM
	ctx   context.Context
	got   *[]chatToolNotification
	asked []chatApprovalNotification
}

func newFolderRig(t *testing.T, outside, folders string, answer bool, script ...scripted) *folderRig {
	t.Helper()
	r := &folderRig{f: setUpFake(t)}
	r.f.script = script
	var got []chatToolNotification
	r.got = &got
	ctx, err := withTools(context.Background(), projectDir(t), "", func(n chatApprovalNotification) (bool, error) {
		r.asked = append(r.asked, n)
		return answer, nil
	}, func(n chatToolNotification) { got = append(got, n) })
	if err != nil {
		t.Fatal(err)
	}
	if r.ctx, err = withFolders(ctx, folders, outside); err != nil {
		t.Fatal(err)
	}
	return r
}

func (r *folderRig) send(t *testing.T) chatResult {
	t.Helper()
	res, err := chatSendEffort(r.ctx, "look there", "", "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	return res
}

func readCall(path string) scripted {
	b, _ := json.Marshal(map[string]string{"path": path})
	return scripted{name: "read", args: string(b)}
}

// The reported refusal: "X is outside the project folder, so it is not available",
// with nothing the user could do. Without a policy it stays refused, as it always was.
func TestAPathOutsideStaysRefusedWithoutAPolicy(t *testing.T) {
	_, file := otherProject(t)
	r := newFolderRig(t, "", "", true, readCall(file))
	r.send(t)
	if len(*r.got) != 1 || (*r.got)[0].OK || !strings.Contains((*r.got)[0].Summary, "outside the project") {
		t.Fatalf("calls = %+v", *r.got)
	}
	if len(r.asked) != 0 {
		t.Errorf("the user was asked without a policy: %+v", r.asked)
	}
}

func TestAskPutsTheFolderToTheUserOnceAndThenTheModelReadsIt(t *testing.T) {
	dir, file := otherProject(t)
	r := newFolderRig(t, editsAsk, "", true, readCall(file), readCall(file))
	r.send(t)
	if len(r.asked) != 1 {
		t.Fatalf("the user was asked %d times, want once: %+v", len(r.asked), r.asked)
	}
	a := r.asked[0]
	if a.Name != "folder" || a.Arg != dir {
		t.Errorf("approval = %+v; want the project folder %s, not just the file", a, dir)
	}
	if len(*r.got) != 2 || !(*r.got)[0].OK || !(*r.got)[1].OK || !strings.Contains((*r.got)[0].Output, "_create_glyph") {
		t.Fatalf("the folder was not readable after the user agreed: %+v", *r.got)
	}
}

func TestDecliningTheFolderRefusesTheCallAndTellsTheModel(t *testing.T) {
	dir, file := otherProject(t)
	r := newFolderRig(t, editsAsk, "", false, readCall(file))
	r.send(t)
	if len(*r.got) != 1 || (*r.got)[0].OK {
		t.Fatalf("calls = %+v", *r.got)
	}
	if s := (*r.got)[0].Summary; !strings.Contains(s, "did not open") || !strings.Contains(s, dir) {
		t.Errorf("the model is not told what was declined: %q", s)
	}
}

func TestAllowOpensTheFolderWithoutAsking(t *testing.T) {
	_, file := otherProject(t)
	r := newFolderRig(t, editsAllow, "", false, readCall(file))
	r.send(t)
	if len(r.asked) != 0 || len(*r.got) != 1 || !(*r.got)[0].OK {
		t.Fatalf("asked %v, calls %+v", r.asked, *r.got)
	}
}

// The core keeps nothing between turns, so what the user opened comes back as folders.
func TestAFolderTheClientHoldsIsOpenOnTheNextTurnWithoutAsking(t *testing.T) {
	dir, file := otherProject(t)
	folders, _ := json.Marshal([]string{dir})
	r := newFolderRig(t, editsAsk, string(folders), false, readCall(file))
	r.send(t)
	if len(r.asked) != 0 || len(*r.got) != 1 || !(*r.got)[0].OK {
		t.Fatalf("asked %v, calls %+v", r.asked, *r.got)
	}
}

func TestAFolderThatNoLongerExistsIsDroppedNotFatal(t *testing.T) {
	folders, _ := json.Marshal([]string{filepath.Join(t.TempDir(), "gone")})
	r := newFolderRig(t, editsAsk, string(folders), true, readCall("main.go"))
	r.send(t)
	if len(*r.got) != 1 || !(*r.got)[0].OK {
		t.Fatalf("calls = %+v", *r.got)
	}
}

func TestOnlyFileToolsOpenFolders(t *testing.T) {
	// A command is not a path the confinement judges: run keeps its own approval.
	r := newFolderRig(t, editsAsk, "", true, scripted{name: "run", args: `{"command":"echo hi"}`})
	var err error
	if r.ctx, err = withRuns(r.ctx, editsAsk); err != nil {
		t.Fatal(err)
	}
	r.send(t)
	for _, a := range r.asked {
		if a.Name == "folder" {
			t.Errorf("a command opened a folder: %+v", a)
		}
	}
}

func TestTheModelIsToldWhichFoldersAreOpenAndHowToAskForOthers(t *testing.T) {
	dir, _ := otherProject(t)
	folders, _ := json.Marshal([]string{dir})
	r := newFolderRig(t, editsAsk, string(folders), true)
	r.send(t)
	last := r.f.messages[len(r.f.messages)-1]
	sys, _ := last[0]["content"].(string)
	if !strings.Contains(sys, "Also open, by full path: "+dir) || !strings.Contains(sys, "give the full path") {
		t.Errorf("system prompt: %q", sys)
	}
	// Without a policy nothing is said about folders.
	r2 := newFolderRig(t, "", "", true)
	r2.send(t)
	l2 := r2.f.messages[len(r2.f.messages)-1]
	if s, _ := l2[0]["content"].(string); strings.Contains(s, "give the full path") || strings.Contains(s, "Also open") {
		t.Errorf("folders mentioned with no policy: %q", s)
	}
}

func TestFoldersWithoutAWorkdirAreRefused(t *testing.T) {
	if _, err := withFolders(context.Background(), `["/x"]`, ""); err == nil {
		t.Error("folders without a workdir were accepted")
	}
	if _, err := withFolders(context.Background(), "", "ask"); err == nil {
		t.Error("outside without a workdir was accepted")
	}
	ctx, _ := withTools(context.Background(), projectDir(t), "", nil, nil)
	if _, err := withFolders(ctx, "", "sometimes"); err == nil {
		t.Error("an unknown policy was accepted")
	}
	if _, err := withFolders(ctx, "not json", ""); err == nil {
		t.Error("a bad folders list was accepted")
	}
}
