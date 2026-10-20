package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"

	"github.com/charmbracelet/x/ansi"
	"github.com/michiTrader/arxi_tui/internal/fold"
	"github.com/michiTrader/arxi_tui/internal/term"
	"github.com/michiTrader/arxi_tui/internal/theme"
)

// This file is ui_preview with keys: the model pressing keys in the interface it is
// building, and reading the screen that results.
//
// # Why it exists
//
// A picture says how the screen looks, not whether it works. "A button under the input
// that Enter reaches with the down arrow" is a claim about keys, and the only honest check
// is to press them. With keys the model presses down, down, enter and reads what happened:
// the window that opened, the line that went to the model, the address the browser was
// asked to open.
//
// # Why a copy of the program, not the running loop
//
// The loop reads and writes process-wide state: the saved interface and history, the
// words in use, the shells it runs, the browser it opens, the clipboard helper it starts.
// Keys pressed in the live loop would change the user's screen and run their commands.
// So the keys run in a second copy of this same binary (no new dependency, no new size)
// that is given: its own empty folders, a home and a working folder that are temporary,
// no PATH (no clipboard program, no git), a network that refuses, a browser that only
// records the address, and shells that run nothing. The user's interface is handed to it
// as files in those folders, exactly as the real program would find it at start-up.
// What the copy does is therefore what the real loop does with those keys, and nothing it
// does can leave the copy.
//
// What is not carried over, said in the answer: packages (they live in folders the copy
// cannot see) and the project's own settings.

// simulateArg is how the program is asked to be the simulation copy. It reads one request
// on stdin and writes one answer on stdout, and does nothing else.
const simulateArg = "__simulate-ui"

const (
	simMaxSteps   = 40
	simMaxTyped   = 300
	simStepWait   = 2 * time.Second
	simTotalLimit = 20 * time.Second
	simMaxAnswer  = 1 << 20
)

type simRequest struct {
	Width, Height int
	Shells        int
	Working       bool
	Input         string
	Chat          []previewLine
	Keys          []string
	State         interfaceState
	Cwd           string // the base name of the folder the user is in, for the where-row
}

type simResult struct {
	Screen    string
	Submitted []string // what went to the model
	Opened    []string // addresses the browser was asked to open
	Copied    []string // what the clipboard was given
	QuitAt    int      // 1-based key after which the program ended, 0 when it did not
	Notice    string   // why the saved interface was not used, when it was not
	Err       string
}

// simStep is one press of the model's: one key, or a run of typed text, or one paste.
type simStep struct {
	label  string
	events []term.Event
}

// parseSimKeys turns the model's keys into steps. A token is a key name (down, enter,
// ctrl+c, shift+tab, f2...), type:<text> to type text, or paste:<text> to paste it.
func parseSimKeys(keys []string) ([]simStep, error) {
	if len(keys) > simMaxSteps {
		return nil, fmt.Errorf("at most %d keys per call", simMaxSteps)
	}
	var out []simStep
	for _, k := range keys {
		switch {
		case strings.HasPrefix(k, "type:"):
			text := strings.TrimPrefix(k, "type:")
			if len([]rune(text)) > simMaxTyped {
				return nil, fmt.Errorf("type: takes at most %d characters", simMaxTyped)
			}
			st := simStep{label: k}
			for _, r := range text {
				st.events = append(st.events, keyEvent(r))
			}
			out = append(out, st)
		case strings.HasPrefix(k, "paste:"):
			text := strings.TrimPrefix(k, "paste:")
			if len([]rune(text)) > simMaxTyped*10 {
				return nil, fmt.Errorf("paste: takes at most %d characters", simMaxTyped*10)
			}
			out = append(out, simStep{label: "paste", events: []term.Event{pasteEvent(text)}})
		default:
			key, ok := term.ParseKey(strings.ToLower(strings.TrimSpace(k)))
			if !ok {
				return nil, fmt.Errorf("%q is not a key; use names like down, up, enter, tab, shift+tab, esc, ctrl+c, f2, space, or type:<text>, paste:<text>", k)
			}
			out = append(out, simStep{label: key.String(), events: []term.Event{{Kind: term.EventKey, Key: key}}})
		}
	}
	return out, nil
}

// simKeysArg reads the keys argument: a list, or one string of names split on spaces.
func simKeysArg(raw json.RawMessage) ([]string, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return nil, nil
	}
	var list []string
	if err := json.Unmarshal(raw, &list); err == nil {
		return list, nil
	}
	var one string
	if err := json.Unmarshal(raw, &one); err == nil {
		return strings.Fields(one), nil
	}
	return nil, errors.New(`keys is a list like ["down","down","enter"]`)
}

// ---- the parent: runs the copy -------------------------------------------------

// simExecutable is the program the copy is started from; a test could replace it.
var simExecutable = os.Executable

// simulate runs the request in the isolated copy and returns what it answered.
func simulate(ctx context.Context, req simRequest) (simResult, error) {
	exe, err := simExecutable()
	if err != nil {
		return simResult{}, fmt.Errorf("the program cannot find itself to run the keys: %v", err)
	}
	root, err := os.MkdirTemp("", "arxi-sim-")
	if err != nil {
		return simResult{}, err
	}
	defer os.RemoveAll(root)
	base := filepath.Base(req.Cwd)
	if base == "" || base == "." || base == string(filepath.Separator) {
		base = "project"
	}
	work := filepath.Join(root, "home", base)
	cfg := filepath.Join(root, "config")
	for _, d := range []string{work, cfg} {
		if err := os.MkdirAll(d, 0o700); err != nil {
			return simResult{}, err
		}
	}
	body, err := json.Marshal(req)
	if err != nil {
		return simResult{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, simTotalLimit)
	defer cancel()
	cmd := exec.CommandContext(ctx, exe, simulateArg)
	cmd.Dir = work
	cmd.Env = simEnv(root, cfg)
	cmd.Stdin = bytes.NewReader(body)
	var out, errOut bytes.Buffer
	cmd.Stdout = &limitWriter{w: &out, left: simMaxAnswer}
	cmd.Stderr = &limitWriter{w: &errOut, left: 4096}
	cmd.WaitDelay = time.Second
	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return simResult{}, fmt.Errorf("the keys took longer than %s and were stopped", simTotalLimit)
		}
		return simResult{}, fmt.Errorf("the simulation failed: %v %s", err, strings.TrimSpace(errOut.String()))
	}
	var res simResult
	if err := json.Unmarshal(out.Bytes(), &res); err != nil {
		return simResult{}, fmt.Errorf("the simulation answered something unreadable: %v", err)
	}
	return res, nil
}

// simEnv is the whole environment of the copy: nothing of the user's, and nowhere to find
// a program. The copy cannot read a key, a token or a path that is not in this list.
func simEnv(root, cfg string) []string {
	env := []string{
		"ARXI_SIM_CHILD=1",
		"ARXI_CONFIG_DIR=" + cfg,
		"ARXI_HISTORY_DIR=" + cfg,
		"ARXI_SESSIONS=off",
		"ARXI_CONSENT_FILE=" + filepath.Join(cfg, "consent.json"),
		"ARXI_PLUGINS_DIR=" + filepath.Join(cfg, "plugins"),
		"ARXI_PROJECT_RULES=off",
		"HOME=" + filepath.Join(root, "home"),
		"USERPROFILE=" + filepath.Join(root, "home"),
		"TMPDIR=" + root,
		"PATH=" + filepath.Join(root, "nowhere"),
	}
	if runtime.GOOS == "windows" {
		env = append(env, "SYSTEMROOT="+os.Getenv("SYSTEMROOT"), "TEMP="+root, "TMP="+root)
	}
	return env
}

// limitWriter keeps the first left bytes and drops the rest, without failing the writer.
type limitWriter struct {
	w    io.Writer
	left int
}

func (l *limitWriter) Write(p []byte) (int, error) {
	n := len(p)
	if len(p) > l.left {
		p = p[:l.left]
	}
	l.left -= len(p)
	_, _ = l.w.Write(p)
	return n, nil
}

// ---- the copy: runs the loop ---------------------------------------------------

// simDriver is the model's side of the simulation: it records what would have been sent
// and echoes the line into the conversation the way a real driver does.
type simDriver struct {
	evCh      chan fold.Event
	seq       int64
	submitted []string
	shells    *shellRegistry
}

func (d *simDriver) SubmitPrompt(ctx context.Context, text string) error {
	d.submitted = append(d.submitted, text)
	d.seq += 1000
	select {
	case <-ctx.Done():
		return ctx.Err()
	case d.evCh <- fold.Event{Type: "run.prompt", Seq: d.seq, Payload: map[string]any{"text": text}}:
		return nil
	}
}

func (d *simDriver) Close() error { return nil }

// Shells makes the simulation a shellHost: the registry holds pretend shells.
func (d *simDriver) Shells() *shellRegistry { return d.shells }

// refuseTransport is the network of the copy: there is none.
type refuseTransport struct{}

func (refuseTransport) RoundTrip(*http.Request) (*http.Response, error) {
	return nil, errors.New("there is no network in a simulation")
}

// simulateMain is the copy's whole life: read a request, run it, write the answer.
func simulateMain(in io.Reader, out io.Writer) int {
	var req simRequest
	if err := json.NewDecoder(io.LimitReader(in, 8<<20)).Decode(&req); err != nil {
		_ = json.NewEncoder(out).Encode(simResult{Err: "unreadable request: " + err.Error()})
		return 0
	}
	_ = json.NewEncoder(out).Encode(runSimulation(req))
	return 0
}

func runSimulation(req simRequest) (res simResult) {
	defer func() {
		if p := recover(); p != nil {
			res.Err = fmt.Sprintf("the interface could not be run: %v", p)
		}
	}()
	// Nothing leaves this process: the browser records, there is no clipboard program, and
	// the network refuses.
	var opened []string
	openBrowser = func(url string) error { opened = append(opened, url); return nil }
	lookPath = func(string) (string, error) { return "", exec.ErrNotFound }
	http.DefaultTransport = refuseTransport{}

	if configDir() == "" {
		res.Err = "the simulation has no folder to keep the interface in"
		return res
	}
	if err := writeInterface(req.State); err != nil {
		res.Err = "the interface could not be handed to the simulation: " + err.Error()
		return res
	}
	steps, err := parseSimKeys(req.Keys)
	if err != nil {
		res.Err = err.Error()
		return res
	}
	doc, notice, err := resolveBootScene(builtinScene)
	if err != nil {
		res.Err = "the interface could not be loaded: " + err.Error()
		return res
	}
	res.Notice = notice

	drv := &simDriver{evCh: make(chan fold.Event, 128), seq: 1000}
	drv.shells = newSimShells(req.Shells, func(a string) { opened = append(opened, a) })
	push := func(typ, text string) {
		drv.seq += 1000
		drv.evCh <- fold.Event{Type: typ, Seq: drv.seq, Payload: map[string]any{"text": text}}
	}
	for _, l := range req.Chat {
		switch strings.ToLower(strings.TrimSpace(l.Role)) {
		case "user":
			push("run.prompt", l.Text)
		case "assistant":
			push("llm.response", l.Text)
		case "note":
			push("chat.note", l.Text)
		case "error":
			push("chat.error", l.Text)
		case "warn":
			push("chat.warn", l.Text)
		}
	}
	if req.Working {
		drv.seq += 1000
		drv.evCh <- fold.Event{Type: "agent.activated", Seq: drv.seq, Payload: map[string]any{"agent": "assistant"}}
	}

	tty := newOpenFakeTTY(req.Width, req.Height, nil)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- loop(ctx, tty, doc, theme.SOBRIA(), drv.evCh, drv, "") }()

	quit := func() bool {
		select {
		case <-done:
			return true
		default:
			return false
		}
	}
	// Wait for the first frame, for the conversation to be read, and for the loop to be idle.
	if !simWait(func() bool { return tty.frameCount() >= 1 && len(drv.evCh) == 0 }, 3*time.Second) {
		res.Err = "the interface did not draw its first screen"
		return res
	}
	time.Sleep(40 * time.Millisecond)
	send := func(evs []term.Event) {
		for _, ev := range evs {
			select {
			case tty.events <- ev:
			case <-done:
				return
			}
		}
	}
	if req.Input != "" {
		before := tty.frameCount()
		var evs []term.Event
		for _, r := range req.Input {
			evs = append(evs, keyEvent(r))
		}
		send(evs)
		simSettle(tty, before)
	}
	for i, st := range steps {
		before := tty.frameCount()
		send(st.events)
		if quit() {
			res.QuitAt = i + 1
			break
		}
		simSettle(tty, before)
		if quit() {
			res.QuitAt = i + 1
			break
		}
	}
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
	}
	res.Screen = screenOfFrames(tty)
	res.Submitted = append(res.Submitted, drv.submitted...)
	res.Opened = opened
	for n := 0; n < tty.frameCount(); n++ {
		res.Copied = append(res.Copied, copiedIn(tty.frameAt(n))...)
	}
	return res
}

// simWait polls until ok, or the limit.
func simWait(ok func() bool, limit time.Duration) bool {
	end := time.Now().Add(limit)
	for time.Now().Before(end) {
		if ok() {
			return true
		}
		time.Sleep(2 * time.Millisecond)
	}
	return ok()
}

// simSettle waits until the loop has answered what was sent: a frame after before, and
// then no new frame for a moment (a key can run queued actions that paint again).
func simSettle(tty *fakeTTY, before int) {
	if !simWait(func() bool { return tty.frameCount() > before }, simStepWait) {
		return
	}
	last, quiet := tty.frameCount(), time.Now()
	for time.Since(quiet) < 30*time.Millisecond && time.Since(quiet) < simStepWait {
		time.Sleep(3 * time.Millisecond)
		if n := tty.frameCount(); n != last {
			last, quiet = n, time.Now()
		}
	}
}

// screenOfFrames is the last full frame the loop wrote, as plain text.
func screenOfFrames(tty *fakeTTY) string {
	for n := tty.frameCount() - 1; n >= 0; n-- {
		if f := tty.frameAt(n); strings.Contains(f, frameBegin) {
			return framePlain(f)
		}
	}
	return ""
}

var cupRow = regexp.MustCompile(`\x1b\[\d+;1H\x1b\[K`)

// framePlain reads the rows out of the bytes emitFrame wrote: each row is placed with a
// cursor move and erased before it is painted, so the text between two of those is one row.
func framePlain(frame string) string {
	parts := cupRow.Split(frame, -1)
	if len(parts) < 2 {
		return strings.TrimRight(ansi.Strip(frame), " \n")
	}
	rows := make([]string, 0, len(parts)-1)
	for _, p := range parts[1:] {
		rows = append(rows, strings.TrimRight(ansi.Strip(p), " "))
	}
	return strings.TrimRight(strings.Join(rows, "\n"), "\n ")
}

var osc52Seq = regexp.MustCompile(`\x1b\]52;c;([A-Za-z0-9+/=]*)\a`)

// copiedIn is what a frame's bytes asked the terminal to put on the clipboard.
func copiedIn(frame string) []string {
	var out []string
	for _, m := range osc52Seq.FindAllStringSubmatch(frame, -1) {
		if b, err := base64.StdEncoding.DecodeString(m[1]); err == nil {
			out = append(out, string(b))
		}
	}
	return out
}
