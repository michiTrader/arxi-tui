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
	"github.com/michiTrader/arxi_tui/internal/term"
	"github.com/michiTrader/arxi_tui/internal/theme"
)

func TestScrollKeyPagesAndJumpsToTheLatestLine(t *testing.T) {
	pgup := term.Key{Type: term.KeyPgUp}
	pgdn := term.Key{Type: term.KeyPgDn}
	end := term.Key{Type: term.KeyEnd}
	ctrlEnd := term.Key{Type: term.KeyEnd, Mod: term.ModCtrl}

	if got, ok := scrollKey(pgup, "", 0, 5, 20); !ok || got != 25 {
		t.Errorf("PgUp from 5 by a 20-line page = %d,%v want 25,true", got, ok)
	}
	if got, ok := scrollKey(pgdn, "", 0, 5, 20); !ok || got != 0 {
		t.Errorf("PgDn past the tail = %d,%v want 0,true (never below the latest line)", got, ok)
	}
	if got, ok := scrollKey(pgdn, "", 0, 50, 20); !ok || got != 30 {
		t.Errorf("PgDn from 50 = %d,%v want 30,true", got, ok)
	}
	if got, ok := scrollKey(pgdn, "", 0, 2, 3); !ok || got != 0 {
		t.Errorf("PgDn by one line short of the tail = %d,%v want 0,true", got, ok)
	}
	// End is the caret's while the caret has somewhere to go.
	if got, ok := scrollKey(end, "hello", 2, 40, 20); ok || got != 40 {
		t.Errorf("End mid-line = %d,%v want the key left to the editor", got, ok)
	}
	// ... and the jump when it has not, or when ctrl is held.
	if got, ok := scrollKey(end, "hello", 5, 40, 20); !ok || got != 0 {
		t.Errorf("End at the end of the line = %d,%v want 0,true", got, ok)
	}
	if got, ok := scrollKey(end, "", 0, 40, 20); !ok || got != 0 {
		t.Errorf("End on an empty line = %d,%v want 0,true", got, ok)
	}
	if got, ok := scrollKey(ctrlEnd, "hello", 1, 40, 20); !ok || got != 0 {
		t.Errorf("ctrl+End mid-line = %d,%v want 0,true", got, ok)
	}
	if _, ok := scrollKey(term.Key{Type: term.KeyUp}, "", 0, 5, 20); ok {
		t.Error("an arrow is not a scroll key")
	}
	if scrollPage(24) != 20 || scrollPage(3) != 1 {
		t.Errorf("page sizes: 24 rows -> %d, 3 rows -> %d", scrollPage(24), scrollPage(3))
	}
}

func TestScrollLabelOnlyWhenScrolledUp(t *testing.T) {
	if scrollLabel(0) != "" {
		t.Error("at the tail the bar must say nothing")
	}
	if scrollLabel(1) != scrollHint {
		t.Error("scrolled up the bar must name the key back")
	}
}

func writeHead(t *testing.T, gitDir, head string) {
	t.Helper()
	if err := os.MkdirAll(gitDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(gitDir, "HEAD"), []byte(head), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestGitBranchReadsHeadWithoutRunningGit(t *testing.T) {
	root := t.TempDir()
	writeHead(t, filepath.Join(root, ".git"), "ref: refs/heads/feat/x\n")
	sub := filepath.Join(root, "a", "b")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	if got := gitBranch(sub); got != "feat/x" {
		t.Errorf("branch from a subfolder = %q want feat/x", got)
	}
	writeHead(t, filepath.Join(root, ".git"), "0123456789abcdef0123456789abcdef01234567\n")
	if got := gitBranch(root); got != "0123456" {
		t.Errorf("detached HEAD = %q want the short id", got)
	}
}

func TestGitBranchFollowsAWorktreeFile(t *testing.T) {
	base := t.TempDir()
	real := filepath.Join(base, "main", ".git", "worktrees", "w")
	writeHead(t, real, "ref: refs/heads/wt\n")
	wt := filepath.Join(base, "w")
	if err := os.MkdirAll(wt, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(wt, ".git"), []byte("gitdir: "+real+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := gitBranch(wt); got != "wt" {
		t.Errorf("worktree branch = %q want wt", got)
	}
	// A relative gitdir is relative to the folder that holds the .git file.
	rel := filepath.Join(base, "r")
	if err := os.MkdirAll(rel, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(rel, ".git"), []byte("gitdir: ../main/.git/worktrees/w\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := gitBranch(rel); got != "wt" {
		t.Errorf("relative gitdir = %q want wt", got)
	}
}

func TestGitBranchOutsideARepositoryIsEmpty(t *testing.T) {
	if got := gitBranch(t.TempDir()); got != "" {
		t.Skipf("the temp dir sits inside a repository (%q)", got)
	}
	if got := gitBranch(""); got != "" {
		t.Errorf("empty dir gave %q", got)
	}
}

func TestBranchCacheReusesTheAnswerForTwoSeconds(t *testing.T) {
	root := t.TempDir()
	writeHead(t, filepath.Join(root, ".git"), "ref: refs/heads/one\n")
	var c branchCache
	t0 := time.Now()
	if got := c.get(root, t0); got != "one" {
		t.Fatalf("first read = %q", got)
	}
	writeHead(t, filepath.Join(root, ".git"), "ref: refs/heads/two\n")
	if got := c.get(root, t0.Add(time.Second)); got != "one" {
		t.Errorf("inside the window the cached %q was expected, got %q", "one", got)
	}
	if got := c.get(root, t0.Add(3*time.Second)); got != "two" {
		t.Errorf("after the window the new branch was expected, got %q", got)
	}
	other := t.TempDir()
	writeHead(t, filepath.Join(other, ".git"), "ref: refs/heads/three\n")
	if got := c.get(other, t0.Add(3*time.Second)); got != "three" {
		t.Errorf("a different folder must not reuse the cache, got %q", got)
	}
}

// scrollLoop runs the real loop on a 100x20 terminal with a chat much taller than the pane and
// returns the frames the terminal received.
func scrollLoop(t *testing.T, keys []scheduledEvent) []string {
	t.Helper()
	t.Setenv(configDirEnv, t.TempDir())
	drv := newCompactDriver(1_000_000, 2)
	var long string
	for i := 0; i < 60; i++ {
		long += "line " + string(rune('A'+i%26)) + "\n\n"
	}
	for i, e := range []fold.Event{
		{Type: "run.prompt", Actor: "assistant", Payload: map[string]any{"text": "go"}},
		{Type: "llm.response", Actor: "assistant", Payload: map[string]any{"text": long, "model": "openai/gpt-4o",
			"tokens_in": float64(10), "tokens_out": float64(5), "context_tokens": float64(15)}},
		{Type: "agent.turn_done", Actor: "assistant"},
	} {
		e.Seq = int64(3000 + i)
		drv.evCh <- e
	}
	doc, err := scene.ParseDocument([]byte(factorySobria))
	if err != nil {
		t.Fatal(err)
	}
	tty := newFakeTTY(100, 20, keys)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := loop(ctx, tty, doc, theme.SOBRIA(), drv.evCh, drv, ""); err != nil {
		t.Fatalf("loop: %v", err)
	}
	tty.mu.Lock()
	defer tty.mu.Unlock()
	out := make([]string, len(tty.frames))
	for i, f := range tty.frames {
		out[i] = stripANSI(f)
	}
	return out
}

func scrollQuit() []scheduledEvent {
	return []scheduledEvent{{200 * time.Millisecond, ctrlCharEvent('c')}, {50 * time.Millisecond, ctrlCharEvent('c')}}
}

func TestLoopScrollHintAppearsOnlyWhileScrolledUpAndEndGoesBack(t *testing.T) {
	keys := []scheduledEvent{
		{400 * time.Millisecond, arrowEvent(term.KeyPgUp)},
		{300 * time.Millisecond, arrowEvent(term.KeyEnd)},
	}
	frames := scrollLoop(t, append(keys, scrollQuit()...))
	first, last, hinted := -1, -1, -1
	for i, f := range frames {
		if strings.Contains(f, scrollHint) {
			if first < 0 {
				first = i
			}
			last = i
		}
		if strings.Contains(f, "line") {
			hinted = i
		}
	}
	if first < 0 {
		t.Fatalf("PgUp never showed %q; frames=%d", scrollHint, len(frames))
	}
	if first == 0 {
		t.Error("the hint was there before any scrolling")
	}
	if last == len(frames)-1 || hinted < 0 {
		t.Errorf("End on an empty line did not take the chat back (hint last seen in frame %d of %d)", last, len(frames))
	}
	if !strings.Contains(frames[first], "~") && !strings.Contains(frames[first], "/") {
		t.Errorf("the hint is not on the cwd line:\n%s", frames[first])
	}
}

func TestLoopEndInTheMiddleOfATypedLineStaysTheCaretsAndKeepsTheScroll(t *testing.T) {
	keys := append([]scheduledEvent{{400 * time.Millisecond, arrowEvent(term.KeyPgUp)}}, typed("abc")...)
	keys = append(keys,
		scheduledEvent{20 * time.Millisecond, arrowEvent(term.KeyLeft)},
		scheduledEvent{300 * time.Millisecond, arrowEvent(term.KeyEnd)}) // caret at 2 of 3 -> moves the caret only
	frames := scrollLoop(t, append(keys, scrollQuit()...))
	n := len(frames)
	// Look at the frame before the exit keys repaint: the hint must still be on screen.
	seen := false
	for _, f := range frames[n/2:] {
		if strings.Contains(f, scrollHint) {
			seen = true
		}
	}
	if !seen {
		t.Errorf("End mid-line cleared the scroll hint, but End there belongs to the caret")
	}
}
