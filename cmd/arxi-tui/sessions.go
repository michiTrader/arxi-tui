package main

import (
	"bufio"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/michiTrader/arxi_tui/internal/driver"
	"github.com/michiTrader/arxi_tui/internal/fold"
)

// This file is the saved conversations behind /resume.
//
// Each conversation is one file of JSON lines in <settings>/sessions: a header line, then
// the events that make the conversation (what was asked, what was answered, what the model
// looked at). It only ever grows by appending a line, so a crash or a power cut costs the
// last line at worst and never the file, and reading it back is the same fold the screen
// already uses: the events are replayed, so the transcript comes back exactly as it was.
//
// What is NOT saved: the model's streamed thinking (large and of no use afterwards), an
// approval that was still waiting for an answer, and settings (model, effort, mode), which
// belong to the user and not to a conversation.
//
// A conversation can contain anything the user typed, so the files are private to the
// user (0600 in a 0700 folder), and ARXI_SESSIONS=off turns saving off altogether.

const (
	sessionsEnv   = "ARXI_SESSIONS"
	sessionsKeep  = 100             // conversations kept; the oldest are deleted beyond this
	sessionFieldM = 16 * 1024       // a stored text field is cut to this many bytes
	sessionFileM  = 8 * 1024 * 1024 // a conversation stops growing here
	sessionLoadM  = 4000            // events read back from one file, at most
	sessionTitleM = 48              // runes of the first question shown in the list
	sessionMagic  = "arxi_session"  // the key of a file's header line
)

var sessionIDRe = regexp.MustCompile(`^[0-9]{8}-[0-9]{6}-[0-9a-f]{4}$`)

// sessionKinds are the events a saved conversation holds.
var sessionKinds = map[string]bool{
	"run.prompt": true, "llm.response": true, "chat.tool": true,
	"chat.error": true, "chat.warn": true, "chat.cancelled": true,
}

// sessionsDir is where conversations are kept, "" when saving is off or impossible.
func sessionsDir() string {
	if strings.EqualFold(strings.TrimSpace(os.Getenv(sessionsEnv)), "off") {
		return ""
	}
	d := configDir()
	if d == "" {
		return ""
	}
	return filepath.Join(d, "sessions")
}

// sessionLog writes the conversation on screen to disk as it happens.
type sessionLog struct {
	dir    string
	id     string // "" until the first question of a conversation
	size   int64
	failed bool // a write failed; the user was told once
	now    func() time.Time
}

func newSessionLog(dir string) *sessionLog { return &sessionLog{dir: dir, now: time.Now} }

// End closes the conversation: the next question starts a new file. /clear calls it.
func (l *sessionLog) End() { l.id, l.size = "", 0 }

// Continue makes later events append to an existing conversation (after /resume).
func (l *sessionLog) Continue(id string) {
	l.id, l.size = id, 0
	if st, err := os.Stat(filepath.Join(l.dir, id+".jsonl")); err == nil {
		l.size = st.Size()
	}
}

// Record saves one event if it belongs in a conversation. The first return is whether it
// was the first failure to write, which is the only one worth telling the user about.
func (l *sessionLog) Record(e fold.Event) (firstFailure error) {
	if l == nil || l.dir == "" || !sessionKinds[e.Type] {
		return nil
	}
	if l.id == "" {
		if e.Type != "run.prompt" {
			return nil // nothing was asked yet: a stray notice is not a conversation
		}
		id, err := l.begin()
		if err != nil {
			return l.fail(err)
		}
		l.id = id
	}
	line, err := json.Marshal(shrinkEvent(e))
	if err != nil {
		return l.fail(err)
	}
	line = append(line, '\n')
	if l.size+int64(len(line)) > sessionFileM {
		return nil // an enormous conversation stops being saved rather than filling the disk
	}
	if err := appendLine(filepath.Join(l.dir, l.id+".jsonl"), line); err != nil {
		return l.fail(err)
	}
	l.size += int64(len(line))
	return nil
}

func (l *sessionLog) fail(err error) error {
	if l.failed {
		return nil
	}
	l.failed = true
	return fmt.Errorf("could not save this conversation (%v); /resume will not find it", err)
}

// begin creates the file of a new conversation and trims the old ones.
func (l *sessionLog) begin() (string, error) {
	if err := os.MkdirAll(l.dir, 0o700); err != nil {
		return "", err
	}
	var rnd [2]byte
	if _, err := rand.Read(rnd[:]); err != nil {
		return "", err
	}
	id := l.now().Format("20060102-150405") + "-" + hex.EncodeToString(rnd[:])
	head, _ := json.Marshal(map[string]any{sessionMagic: 1, "started": l.now().UTC().Format(time.RFC3339)})
	if err := appendLine(filepath.Join(l.dir, id+".jsonl"), append(head, '\n')); err != nil {
		return "", err
	}
	l.size = int64(len(head)) + 1
	pruneSessions(l.dir, sessionsKeep, id)
	return id, nil
}

func appendLine(path string, line []byte) error {
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	_, werr := f.Write(line)
	if cerr := f.Close(); werr == nil {
		werr = cerr
	}
	return werr
}

// shrinkEvent copies an event with every long text cut, so one enormous tool output
// cannot fill a conversation file.
func shrinkEvent(e fold.Event) fold.Event {
	out := e
	out.Payload = make(map[string]any, len(e.Payload))
	for k, v := range e.Payload {
		if s, ok := v.(string); ok && len(s) > sessionFieldM {
			cut := s[:sessionFieldM]
			for len(cut) > 0 && !validTail(cut) {
				cut = cut[:len(cut)-1]
			}
			v = cut + "…"
		}
		out.Payload[k] = v
	}
	return out
}

// validTail reports whether s does not end in the middle of a UTF-8 character.
func validTail(s string) bool {
	for i := len(s) - 1; i >= 0 && i >= len(s)-4; i-- {
		if s[i] < 0x80 {
			return true
		}
		if s[i] >= 0xC0 {
			r := []rune(s[i:])
			return len(r) == 1 && r[0] != '\uFFFD'
		}
	}
	return true
}

// pruneSessions deletes the oldest conversation files beyond keep. The one just created
// is never a candidate.
func pruneSessions(dir string, keep int, except string) {
	list := listSessions(dir)
	if len(list) <= keep {
		return
	}
	for _, s := range list[keep:] { // newest first
		if s.ID != except {
			_ = os.Remove(filepath.Join(dir, s.ID+".jsonl"))
		}
	}
}

// sessionInfo is one saved conversation as the list shows it.
type sessionInfo struct {
	ID      string
	Title   string // the first question
	Updated time.Time
}

// listSessions reads the saved conversations, newest first. It reads each file only as
// far as the first question, so a long list stays quick.
func listSessions(dir string) []sessionInfo {
	if dir == "" {
		return nil
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var out []sessionInfo
	for _, ent := range entries {
		name := ent.Name()
		id := strings.TrimSuffix(name, ".jsonl")
		if ent.IsDir() || id == name || !sessionIDRe.MatchString(id) {
			continue
		}
		info, err := ent.Info()
		if err != nil {
			continue
		}
		title, ok := firstQuestion(filepath.Join(dir, name))
		if !ok {
			continue // a header with no question is not worth offering
		}
		out = append(out, sessionInfo{ID: id, Title: title, Updated: info.ModTime()})
	}
	sort.Slice(out, func(i, j int) bool {
		if !out[i].Updated.Equal(out[j].Updated) {
			return out[i].Updated.After(out[j].Updated)
		}
		return out[i].ID > out[j].ID
	})
	return out
}

// firstQuestion is the text of the first run.prompt in a file, cut for the list.
func firstQuestion(path string) (string, bool) {
	f, err := os.Open(path)
	if err != nil {
		return "", false
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 2*sessionFieldM+4096)
	for sc.Scan() {
		var e fold.Event
		if json.Unmarshal(sc.Bytes(), &e) != nil || e.Type != "run.prompt" {
			continue
		}
		text, _ := e.Payload["text"].(string)
		text = strings.Join(strings.Fields(text), " ")
		if text == "" {
			continue
		}
		if r := []rune(text); len(r) > sessionTitleM {
			text = string(r[:sessionTitleM-1]) + "…"
		}
		return text, true
	}
	return "", false
}

var errNoSession = errors.New("that conversation is not there any more")

// loadSession reads a conversation back as events, numbered from 1. A damaged line is
// skipped (a crash can cut the last one) and an event of a kind a conversation does not
// hold is ignored, so a file edited by hand cannot inject anything else into the screen.
func loadSession(dir, id string) ([]fold.Event, error) {
	if dir == "" || !sessionIDRe.MatchString(id) {
		return nil, errNoSession
	}
	f, err := os.Open(filepath.Join(dir, id+".jsonl"))
	if err != nil {
		return nil, errNoSession
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 2*sessionFieldM+4096)
	var evs []fold.Event
	for sc.Scan() && len(evs) < sessionLoadM {
		var e fold.Event
		if json.Unmarshal(sc.Bytes(), &e) != nil || !sessionKinds[e.Type] {
			continue
		}
		e.Seq = int64(len(evs) + 1)
		evs = append(evs, e)
	}
	if len(evs) == 0 {
		return nil, errNoSession
	}
	return evs, nil
}

// historyFromEvents rebuilds what the model has to be told about the conversation so far:
// each question that got an answer, with that answer. A question that failed or was
// cancelled never entered the history while it was live, so it does not here either.
func historyFromEvents(evs []fold.Event) []driver.ChatTurn {
	var out []driver.ChatTurn
	pending, have := "", false
	for _, e := range evs {
		text, _ := e.Payload["text"].(string)
		switch e.Type {
		case "run.prompt":
			pending, have = text, true
		case "llm.response":
			switch {
			case have:
				out = append(out, driver.ChatTurn{Role: "user", Text: pending}, driver.ChatTurn{Role: "assistant", Text: text})
				have = false
			case len(out) > 0 && out[len(out)-1].Role == "assistant":
				out[len(out)-1].Text += text // a streamed answer arrives in pieces
			}
		}
	}
	return out
}

// sessionAge words how long ago a conversation was last used.
func sessionAge(now, t time.Time) string {
	d := now.Sub(t)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%d min ago", int(d.Minutes()))
	case d < 48*time.Hour:
		return fmt.Sprintf("%d h ago", int(d.Hours()))
	case d < 30*24*time.Hour:
		return fmt.Sprintf("%d d ago", int(d.Hours()/24))
	}
	return t.Format("2006-01-02")
}

// The /resume menu. It is the model menu's twin: each row is a saved conversation, and the
// reference carries the id first and the title after it, so what the user types filters
// both while the id is still what a pick returns.

const resumePrefix = "/resume "

func resumeMenuOpen(input string) (filter string, open bool) { return menuOpen(resumePrefix, input) }

func resumeCommand(input string, sel int, cat string) bool {
	return menuCommand("resume", input, sel, cat)
}

// resumeMenuData builds the rows, newest first.
func resumeMenuData(dir string, now time.Time) []fold.ModelMatch {
	if dir == "" {
		return []fold.ModelMatch{{Name: "saving is off", Provider: "unset " + sessionsEnv + " to keep conversations"}}
	}
	list := listSessions(dir)
	if len(list) == 0 {
		return []fold.ModelMatch{{Name: "no saved conversations yet"}}
	}
	out := make([]fold.ModelMatch, 0, len(list))
	for _, s := range list {
		out = append(out, fold.ModelMatch{Ref: s.ID + " " + s.Title, Name: s.Title, Provider: sessionAge(now, s.Updated)})
	}
	return out
}

// resumePick is the conversation id inside a row's reference.
func resumePick(ref string) string {
	id, _, _ := strings.Cut(ref, " ")
	return id
}
