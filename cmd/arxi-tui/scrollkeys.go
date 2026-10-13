package main

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/michiTrader/arxi_tui/internal/term"
)

// scrollHint is what the bottom bar says while the chat is scrolled away from its last line. It
// names the key back, and only then: a hint that is always there stops being read.
const scrollHint = "scrolled up · End: latest"

// scrollKey is the keyboard side of scrolling the chat. PgUp and PgDn move a page (the pane's
// height less a couple of lines of overlap, so the eye keeps its place), and End jumps to the
// latest line. End is also the key that moves the caret to the end of what is being typed, so
// it is the caret's while the caret has somewhere to go; ctrl+End always means the latest line.
// It reports the new offset and whether the key was spent here.
func scrollKey(k term.Key, input string, caret, scroll, page int) (int, bool) {
	if page < 1 {
		page = 1
	}
	switch k.Type {
	case term.KeyPgUp:
		return scroll + page, true
	case term.KeyPgDn:
		if scroll -= page; scroll < 0 {
			scroll = 0
		}
		return scroll, true
	case term.KeyEnd:
		if k.Mod&term.ModCtrl != 0 || caret >= len([]rune(input)) {
			return 0, true
		}
	}
	return scroll, false
}

// scrollPage is how many lines a PgUp/PgDn moves for a terminal h rows tall.
func scrollPage(h int) int {
	if p := h - 4; p > 1 {
		return p
	}
	return 1
}

func ttyHeight(t Terminal) int {
	_, h := t.Size()
	return h
}

// scrollLabel is host.scroll: the hint while scrolled up, nothing at the tail.
func scrollLabel(scroll int) string {
	if scroll > 0 {
		return scrollHint
	}
	return ""
}

// gitBranch reads the branch of the repository dir lives in straight from .git/HEAD, so drawing
// the bar never starts a process. A linked worktree has a .git file that points at its own
// directory, and that is followed. Detached HEAD gives the first seven characters of the commit;
// anything that is not a repository gives "".
func gitBranch(dir string) string {
	for d := dir; d != ""; d = filepath.Dir(d) {
		gitPath := filepath.Join(d, ".git")
		fi, err := os.Stat(gitPath)
		if err != nil {
			if filepath.Dir(d) == d {
				return ""
			}
			continue
		}
		gitDir := gitPath
		if !fi.IsDir() {
			b, err := os.ReadFile(gitPath)
			if err != nil {
				return ""
			}
			rest, ok := strings.CutPrefix(strings.TrimSpace(string(b)), "gitdir:")
			if !ok {
				return ""
			}
			gitDir = strings.TrimSpace(rest)
			if !filepath.IsAbs(gitDir) {
				gitDir = filepath.Join(d, gitDir)
			}
		}
		head, err := os.ReadFile(filepath.Join(gitDir, "HEAD"))
		if err != nil {
			return ""
		}
		s := strings.TrimSpace(string(head))
		if ref, ok := strings.CutPrefix(s, "ref:"); ok {
			return strings.TrimPrefix(strings.TrimSpace(ref), "refs/heads/")
		}
		if len(s) > 7 {
			s = s[:7]
		}
		return s
	}
	return ""
}

// branchCache keeps gitBranch from touching the disk on every repaint (the loop paints twice a
// second while a turn runs): the answer is reused for two seconds, which is also how long a
// `git switch` made in another window takes to show.
type branchCache struct {
	mu   sync.Mutex
	dir  string
	at   time.Time
	last string
}

func (c *branchCache) get(dir string, now time.Time) string {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.dir == dir && !c.at.IsZero() && now.Sub(c.at) < 2*time.Second {
		return c.last
	}
	c.dir, c.at, c.last = dir, now, gitBranch(dir)
	return c.last
}
