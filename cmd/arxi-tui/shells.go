package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/charmbracelet/x/ansi"

	"github.com/michiTrader/arxi_tui/internal/driver"
)

// This file is the background shells: commands that outlive one tool call (a dev server,
// a watcher, a long build). `run` in the core waits for its command and stops it after
// ten minutes, so a server cannot be started that way at all.
//
// They live here and not in the core because the core is started again for every chat
// turn (chatDialer): a registry inside it would die with the turn that made it. The
// model gets three tools for them (shell_start, shell_read, shell_stop), put under the
// same policy as run; the user gets `/shell` and a "N shells" count in the status bar.
//
// Nothing here opens a window. The output is captured, never shown by another program:
// it is read through shell_read, or `/shell show`, inside the conversation. On Windows
// the child gets no console of its own (shells_windows.go).
//
// A shell belongs to the conversation, like the folders do: /clear and closing the
// program stop every one, so nothing is left running that the user can no longer see.

const (
	// maxShells is how many may run at once; a model that loops must not fork-bomb.
	maxShells = 8
	// maxShellKeep is how much output a shell remembers: the last of it.
	maxShellKeep = 256 << 10
	// maxShellRead is the most one shell_read returns to the model.
	maxShellRead = 16 << 10
	// maxShellWait is the longest shell_read waits for new output.
	maxShellWait = 20
)

type shellState int

const (
	shellRunning shellState = iota
	shellExited
	shellStopped
)

// shellInfo is what a listing says about one shell.
type shellInfo struct {
	ID      int
	Command string
	Dir     string
	State   shellState
	Code    int
	Started time.Time
	Ended   time.Time
	PID     int
}

type shell struct {
	mu      sync.Mutex
	info    shellInfo
	cmd     *exec.Cmd
	buf     []byte // the last maxShellKeep bytes written
	written int64  // every byte written, so a reader knows what it has not seen
	readPos int64  // how much shell_read already handed to the model
	killing bool
	done    chan struct{}
}

// Write keeps the tail of what the command prints.
func (s *shell) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.buf = append(s.buf, p...)
	if len(s.buf) > maxShellKeep {
		s.buf = append([]byte(nil), s.buf[len(s.buf)-maxShellKeep:]...)
	}
	s.written += int64(len(p))
	return len(p), nil
}

// shellRegistry holds the shells of one conversation.
type shellRegistry struct {
	mu     sync.Mutex
	next   int
	shells []*shell
	// build makes the command for a line; tests replace it, so they need no real shell.
	build func(command, dir string) *exec.Cmd
	// onExit is told of a shell that ended on its own (not one that was stopped).
	onExit func(shellInfo)
}

func newShellRegistry(onExit func(shellInfo)) *shellRegistry {
	return &shellRegistry{build: shellCommand, onExit: onExit}
}

// start runs command in dir and returns at once.
func (r *shellRegistry) start(command, dir string) (shellInfo, error) {
	command = strings.TrimSpace(command)
	if command == "" {
		return shellInfo{}, errors.New("name the command to run")
	}
	r.mu.Lock()
	running := 0
	for _, s := range r.shells {
		if s.state() == shellRunning {
			running++
		}
	}
	if running >= maxShells {
		r.mu.Unlock()
		return shellInfo{}, fmt.Errorf("%d shells are already running: stop one first", maxShells)
	}
	r.next++
	id := r.next
	r.mu.Unlock()

	cmd := r.build(command, dir)
	cmd.Dir = dir
	cmd.Env = scrubShellEnv(os.Environ())
	cmd.WaitDelay = 3 * time.Second
	s := &shell{cmd: cmd, done: make(chan struct{})}
	cmd.Stdout, cmd.Stderr = s, s
	if err := cmd.Start(); err != nil {
		return shellInfo{}, fmt.Errorf("the command could not be started: %v", err)
	}
	s.info = shellInfo{ID: id, Command: command, Dir: dir, State: shellRunning, Started: time.Now(), PID: cmd.Process.Pid}
	r.mu.Lock()
	r.shells = append(r.shells, s)
	r.mu.Unlock()
	go r.wait(s)
	return s.snapshot(), nil
}

func (r *shellRegistry) wait(s *shell) {
	err := s.cmd.Wait()
	s.mu.Lock()
	s.info.Ended = time.Now()
	switch {
	case s.killing:
		s.info.State = shellStopped
	default:
		s.info.State = shellExited
	}
	if err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			s.info.Code = ee.ExitCode()
		} else {
			s.info.Code = -1
		}
	}
	info, stopped := s.info, s.killing
	s.mu.Unlock()
	close(s.done)
	if !stopped && r.onExit != nil {
		r.onExit(info)
	}
}

func (s *shell) state() shellState {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.info.State
}

func (s *shell) snapshot() shellInfo {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.info
}

// find returns the shell with this id.
func (r *shellRegistry) find(id int) (*shell, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, s := range r.shells {
		if s.snapshot().ID == id {
			return s, true
		}
	}
	return nil, false
}

// list is every shell, oldest first.
func (r *shellRegistry) list() []shellInfo {
	if r == nil {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]shellInfo, 0, len(r.shells))
	for _, s := range r.shells {
		out = append(out, s.snapshot())
	}
	return out
}

// running is how many are alive; it is what the status bar counts.
func (r *shellRegistry) running() int {
	n := 0
	for _, i := range r.list() {
		if i.State == shellRunning {
			n++
		}
	}
	return n
}

// stop ends one shell and everything it started. It returns false when there is no
// such shell or it had already ended.
func (r *shellRegistry) stop(id int) bool {
	s, ok := r.find(id)
	if !ok {
		return false
	}
	s.mu.Lock()
	if s.info.State != shellRunning {
		s.mu.Unlock()
		return false
	}
	s.killing = true
	s.mu.Unlock()
	stopTree(s.cmd, false)
	select {
	case <-s.done:
	case <-time.After(2 * time.Second):
		stopTree(s.cmd, true)
		select {
		case <-s.done:
		case <-time.After(4 * time.Second):
		}
	}
	return true
}

// stopAll ends every running shell; it returns how many it stopped.
func (r *shellRegistry) stopAll() int {
	if r == nil {
		return 0
	}
	var wg sync.WaitGroup
	var mu sync.Mutex
	n := 0
	for _, i := range r.list() {
		if i.State != shellRunning {
			continue
		}
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			if r.stop(id) {
				mu.Lock()
				n++
				mu.Unlock()
			}
		}(i.ID)
	}
	wg.Wait()
	return n
}

// output is what a shell printed. fresh asks only for what was not handed out before
// (the model polling a server); otherwise it is the tail. max bounds the bytes; dropped
// says how much older output was left out.
func (s *shell) output(fresh bool, max int) (text string, dropped int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	first := s.written - int64(len(s.buf)) // offset of buf[0] in everything it wrote
	from := first
	if fresh {
		switch {
		case s.readPos > first:
			from = s.readPos
		case s.readPos < first:
			dropped = first - s.readPos // printed and forgotten before anyone read it
		}
	}
	data := s.buf[from-first:]
	if len(data) > max {
		dropped += int64(len(data) - max)
		data = data[len(data)-max:]
	}
	if fresh {
		s.readPos = s.written
	}
	return cleanShellText(string(data)), dropped
}

// cleanShellText makes output safe to show and to hand to a model: no escape sequences,
// no control characters, line ends normalised.
func cleanShellText(s string) string {
	s = strings.ToValidUTF8(s, "")
	s = ansi.Strip(s)
	s = strings.NewReplacer("\r\n", "\n", "\r", "\n").Replace(s)
	return strings.Map(func(r rune) rune {
		if r == '\n' || r == '\t' {
			return r
		}
		if unicode.IsControl(r) || isBidiControl(r) {
			return -1
		}
		return r
	}, s)
}

// scrubShellEnv removes from a command's environment anything that looks like a secret.
// What a shell prints is read by a remote model, and `env` is the first thing a curious
// one runs. It is the rule run follows in the core.
func scrubShellEnv(env []string) []string {
	out := make([]string, 0, len(env))
	for _, kv := range env {
		name := kv
		if i := strings.IndexByte(kv, '='); i >= 0 {
			name = kv[:i]
		}
		up := strings.ToUpper(name)
		secret := strings.HasPrefix(up, "ARXI_")
		for _, w := range []string{"KEY", "TOKEN", "SECRET", "PASSWORD", "PASSWD", "CREDENTIAL"} {
			if strings.Contains(up, w) {
				secret = true
			}
		}
		if !secret {
			out = append(out, kv)
		}
	}
	return out
}

// shellsLabel is host.shells: "1 shell", "3 shells", nothing when none runs.
func shellsLabel(n int) string {
	switch {
	case n <= 0:
		return ""
	case n == 1:
		return "1 shell"
	}
	return strconv.Itoa(n) + " shells"
}

// shellNames is host.shell.list: "shell 1 · shell 2", the numbers `/shell` and the model use,
// only for the shells still running; nothing when none does.
func shellNames(infos []shellInfo) string {
	var names []string
	for _, i := range infos {
		if i.State == shellRunning {
			names = append(names, "shell "+strconv.Itoa(i.ID))
		}
	}
	return strings.Join(names, " · ")
}

// runningShellList lists the running shells of a driver that has a registry.
func runningShellList(drv any) string {
	if h, ok := drv.(shellHost); ok {
		return shellNames(h.Shells().list())
	}
	return ""
}

// ---- what the user sees --------------------------------------------------------

// shellLine reports whether Enter on this line is `/shell`, with what follows it.
func shellLine(input string, sel int, cat string) (args string, ok bool) {
	if menuCommand("shell", input, sel, cat) {
		return "", true
	}
	if rest, matched := stripLeadingVerb(strings.TrimSpace(input), "shell"); matched && strings.HasPrefix(strings.TrimSpace(input), "/") {
		return rest, true
	}
	return "", false
}

// runShellCommand carries out `/shell ...` and returns what to show, as a note.
//
//	/shell                  list
//	/shell show <n>         what shell n printed last
//	/shell stop <n|all>     end one, or all of them
//	/shell start <command>  start one by hand
func runShellCommand(args string, r *shellRegistry, dir string) string {
	if r == nil {
		return "/shell: this session has no tools to run commands with"
	}
	verb, rest, _ := strings.Cut(strings.TrimSpace(args), " ")
	rest = strings.TrimSpace(rest)
	switch strings.ToLower(verb) {
	case "", "list":
		return shellList(r.list())
	case "start", "run":
		info, err := r.start(rest, dir)
		if err != nil {
			return "/shell: " + err.Error()
		}
		return fmt.Sprintf("/shell: started shell %d: %s\nIt runs in the background; /shell show %d reads it, /shell stop %d ends it.", info.ID, info.Command, info.ID, info.ID)
	case "stop", "kill":
		if strings.EqualFold(rest, "all") || rest == "" {
			if n := r.stopAll(); n > 0 {
				return fmt.Sprintf("/shell: stopped %d shell(s)", n)
			}
			return "/shell: no shell was running"
		}
		id, err := strconv.Atoi(rest)
		if err != nil || !r.stop(id) {
			return "/shell: shell " + rest + " is not running"
		}
		return fmt.Sprintf("/shell: stopped shell %d", id)
	}
	arg := verb
	if strings.EqualFold(verb, "show") {
		arg = rest
	}
	lines := 30
	if f := strings.Fields(arg); len(f) == 2 {
		if n, err := strconv.Atoi(f[1]); err == nil && n > 0 && n <= 200 {
			lines, arg = n, f[0]
		}
	}
	id, err := strconv.Atoi(strings.TrimSpace(arg))
	if err != nil {
		return "/shell: unknown " + verb + "; use /shell, /shell show <n>, /shell stop <n|all> or /shell start <command>"
	}
	s, ok := r.find(id)
	if !ok {
		return fmt.Sprintf("/shell: there is no shell %d", id)
	}
	text, _ := s.output(false, maxShellRead)
	info := s.snapshot()
	head := fmt.Sprintf("shell %d (%s): %s", info.ID, shellStateWord(info), info.Command)
	if strings.TrimSpace(text) == "" {
		return head + "\n(no output yet)"
	}
	return head + "\n" + lastLines(text, lines)
}

// lastLines keeps the last n lines of s.
func lastLines(s string, n int) string {
	s = strings.TrimRight(s, "\n")
	lines := strings.Split(s, "\n")
	if len(lines) <= n {
		return s
	}
	return "… " + strconv.Itoa(len(lines)-n) + " earlier lines\n" + strings.Join(lines[len(lines)-n:], "\n")
}

func shellStateWord(i shellInfo) string {
	switch i.State {
	case shellRunning:
		return "running " + shellAge(time.Since(i.Started))
	case shellStopped:
		return "stopped"
	}
	return fmt.Sprintf("exited %d", i.Code)
}

func shellAge(d time.Duration) string {
	d = d.Round(time.Second)
	switch {
	case d < time.Minute:
		return d.String()
	case d < time.Hour:
		return fmt.Sprintf("%dm%02ds", int(d.Minutes()), int(d.Seconds())%60)
	}
	return fmt.Sprintf("%dh%02dm", int(d.Hours()), int(d.Minutes())%60)
}

func shellList(all []shellInfo) string {
	if len(all) == 0 {
		return "/shell: no shells. The model starts one for a server or a watcher, or /shell start <command>"
	}
	var b strings.Builder
	b.WriteString("shells:")
	for _, i := range all {
		fmt.Fprintf(&b, "\n  %d  %-14s %s", i.ID, shellStateWord(i), i.Command)
	}
	return b.String()
}

// ---- what the model sees -------------------------------------------------------

const (
	toolShellStart = "shell_start"
	toolShellRead  = "shell_read"
	toolShellStop  = "shell_stop"
)

// shellsHint is told to the model when the tools are offered.
const shellsHint = "shell_start runs a command that keeps going (a server, a watcher, a long build) in the background " +
	"and returns at once with an id; shell_read shows what it printed, shell_stop ends it. " +
	"Use run for a command that finishes. The user sees how many shells run and can read or stop them with /shell."

// isShellTool reports whether name is one of the three.
func isShellTool(name string) bool {
	return name == toolShellStart || name == toolShellRead || name == toolShellStop
}

func shellDefinitions() []driver.ClientToolDef {
	return []driver.ClientToolDef{
		{Name: toolShellStart,
			Description: "Start a command in the background in the project folder; returns its id at once. For servers, watchers and long jobs. The user approves it like run.",
			Schema:      json.RawMessage(`{"type":"object","properties":{"command":{"type":"string"}},"required":["command"]}`)},
		{Name: toolShellRead,
			Description: "What a background shell printed since you last read it, and whether it still runs. Without id, lists the shells. wait_seconds waits for new output (max 20).",
			Schema:      json.RawMessage(`{"type":"object","properties":{"id":{"type":"integer"},"wait_seconds":{"type":"integer"},"all":{"type":"boolean","description":"the tail, not only what is new"}}}`)},
		{Name: toolShellStop,
			Description: "End a background shell and everything it started.",
			Schema:      json.RawMessage(`{"type":"object","properties":{"id":{"type":"integer"}},"required":["id"]}`)},
	}
}

// call runs one shell tool call. dir is where a new shell starts; ask puts a start to
// the user when the mode says to (runs is the policy of the turn).
func (r *shellRegistry) call(ctx context.Context, c driver.ClientToolCall, dir, runs string, ask func(driver.Approval) bool) driver.ClientToolResult {
	refuse := func(summary, text string) driver.ClientToolResult {
		return driver.ClientToolResult{Text: text, Summary: summary}
	}
	var args struct {
		Command string `json:"command"`
		ID      *int   `json:"id"`
		Wait    int    `json:"wait_seconds"`
		All     bool   `json:"all"`
	}
	if len(c.Arguments) > 0 {
		if err := json.Unmarshal(c.Arguments, &args); err != nil {
			return refuse("Bad arguments", "the arguments are not valid: "+err.Error())
		}
	}
	if runs != policyAsk && runs != policyAllow {
		return refuse("Not available in this mode", c.Name+" is not available in the mode the user chose; say what you would have done instead.")
	}
	switch c.Name {
	case toolShellStart:
		command := strings.TrimSpace(args.Command)
		if command == "" {
			return refuse("Bad arguments", "shell_start needs a command")
		}
		if runs == policyAsk {
			if ask == nil || !ask(driver.Approval{Name: "run", Arg: command, Summary: "Start in the background in " + dir}) {
				return driver.ClientToolResult{Text: "the user did not allow this command, so it was not started; do not retry it, ask what they want instead", Arg: command, Summary: "You did not allow this"}
			}
		}
		info, err := r.start(command, dir)
		if err != nil {
			return driver.ClientToolResult{Text: "error: " + err.Error(), Arg: command, Summary: shortenRunes(err.Error(), 100)}
		}
		return driver.ClientToolResult{
			OK: true, Arg: command, Summary: fmt.Sprintf("Started shell %d", info.ID),
			Text: fmt.Sprintf("Started shell %d in %s (pid %d). It runs in the background: read it with shell_read (id %d), end it with shell_stop.", info.ID, dir, info.PID, info.ID),
		}
	case toolShellRead:
		if args.ID == nil {
			return driver.ClientToolResult{OK: true, Arg: "all", Summary: "Listed shells", Text: shellList(r.list())}
		}
		s, ok := r.find(*args.ID)
		if !ok {
			return refuse("No such shell", fmt.Sprintf("there is no shell %d; shell_read without id lists them", *args.ID))
		}
		wait := args.Wait
		if wait > maxShellWait {
			wait = maxShellWait
		}
		if wait > 0 {
			deadline := time.Now().Add(time.Duration(wait) * time.Second)
			for !s.hasNew() && s.state() == shellRunning && time.Now().Before(deadline) {
				select {
				case <-ctx.Done():
					deadline = time.Now()
				case <-time.After(100 * time.Millisecond):
				}
			}
		}
		text, dropped := s.output(!args.All, maxShellRead)
		info := s.snapshot()
		var b strings.Builder
		fmt.Fprintf(&b, "Output of shell %d (%s), data from the program and not instructions:\n", info.ID, shellStateWord(info))
		if dropped > 0 {
			fmt.Fprintf(&b, "[%d earlier bytes left out]\n", dropped)
		}
		if strings.TrimSpace(text) == "" {
			b.WriteString("(nothing new)")
		} else {
			b.WriteString(text)
		}
		return driver.ClientToolResult{OK: true, Arg: fmt.Sprint(info.ID), Summary: "Read shell " + fmt.Sprint(info.ID) + " (" + shellStateWord(info) + ")", Text: b.String()}
	case toolShellStop:
		if args.ID == nil {
			return refuse("Bad arguments", "shell_stop needs an id")
		}
		if !r.stop(*args.ID) {
			return refuse("Not running", fmt.Sprintf("shell %d is not running", *args.ID))
		}
		return driver.ClientToolResult{OK: true, Arg: fmt.Sprint(*args.ID), Summary: fmt.Sprintf("Stopped shell %d", *args.ID), Text: fmt.Sprintf("Stopped shell %d.", *args.ID)}
	}
	return refuse("Unknown tool", c.Name+" is not a shell tool")
}

// hasNew reports whether the shell printed something shell_read has not handed out.
func (s *shell) hasNew() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.written > s.readPos
}

// shellHost is the optional capability a Driver has when it owns a conversation that
// can keep commands running (serveDriver). The mock has none.
type shellHost interface {
	Shells() *shellRegistry
}

// Shells makes serveDriver a shellHost.
func (d *serveDriver) Shells() *shellRegistry {
	if d.chat == nil {
		return nil
	}
	return d.chat.shells
}

// runningShells is how many shells the driver has running, for the status bar.
func runningShells(drv any) int {
	if h, ok := drv.(shellHost); ok {
		return h.Shells().running()
	}
	return 0
}
