// Package supervisor runs a behavioral plugin as a subprocess and speaks the
// ext/v1 wire (ADR-0007) to it over the child's stdin/stdout. It is the I2
// lifecycle: spawn the executable, complete the hello/ack handshake, drain the
// child's stdout on a reader goroutine, and — on process death — restart a
// bounded number of times with backoff before freezing. It owns the child's
// whole process group and kills the group on Close, so an unmount leaves no
// orphan (ADR-0001, invariant 6).
//
// The wire framing is the ext/v1 second channel ADR-0007 signs: one JSON object
// per line each direction, distinct from the host↔core channel because a
// plugin's bind values are a separate authority and must never enter the fold.
// This package reuses internal/driver/ndjson.go's conventions by COPYING the
// pattern (the 1 MiB line cap, the context-cancellable line read), not by
// importing it: the two channels share a dialect, not a code path, and the
// supervisor spawns processes so it cannot sit behind the pure-data arch seam
// internal/ext keeps.
//
// I2 establishes the process and forwards every non-handshake frame verbatim on
// Frames(); mapping a `bind` frame into the <plugin-id>.* store is I3, and
// routing an `on_press` action into the child is I4. The supervisor is a copy of
// arxi-sim's procgroup supervisor (ADR-0001), never an import.
package supervisor

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sync"
	"time"

	"github.com/michiTrader/arxi_tui/internal/ext"
)

// maxLineBytes caps a single ext/v1 line at 1 MiB, the same cap the host↔core
// channel uses (ndjson.go maxLineBytes). A line over the cap is a malformed
// frame, not a value the host stretches to hold.
const maxLineBytes = 1 << 20

var (
	// ErrFatalProtocol marks a failure a restart cannot fix — a plugin speaking
	// the wrong protocol on the wire will speak it again. The supervisor stops
	// rather than burning its restart budget on a certainty.
	ErrFatalProtocol = errors.New("fatal plugin protocol error")
	// ErrHandshakeTimeout is a plugin that did not send its hello in time. It is
	// NOT fatal: a plugin slow to start once may start cleanly on a restart, so
	// it consumes the bounded restart budget rather than stopping outright.
	ErrHandshakeTimeout = errors.New("plugin handshake timed out")
	// ErrUnexpectedExit is the stream ending or the process dying while the host
	// still expected it live. It is the restartable death the bounded backoff
	// exists for.
	ErrUnexpectedExit = errors.New("plugin exited unexpectedly")
)

// wireHello is both directions of the ext/v1 handshake line. The plugin sends
// {type,protocol}; the host acks {type,protocol,plugin_id,granted}. plugin_id
// hands the child its namespace prefix so it never hard-codes it (and so it can
// only ever write inside its own namespace), and granted is the consented
// capability subset — the wire face of "power granted at the gate, once."
type wireHello struct {
	Type     string   `json:"type"`
	Protocol string   `json:"protocol"`
	PluginID string   `json:"plugin_id,omitempty"`
	Granted  []string `json:"granted,omitempty"`
}

// Frame is one decoded plugin→host line the reader forwards. The supervisor
// reads only the `type` discriminator and keeps the rest as raw bytes: I3 owns
// the `bind` shape and its shape-vs-kind check, so committing to a struct here
// would put that decision in two places.
type Frame struct {
	Type string
	Raw  json.RawMessage
}

// Config configures one supervised plugin.
type Config struct {
	// Manifest is the loaded, validated behavioral manifest. Its Executable and
	// Args name the process; its Protocol is the wire version the handshake
	// gates on; its ID is the plugin_id the ack hands the child.
	Manifest ext.Manifest
	// Granted is the capability subset the user consented to at the I5 gate,
	// carried to the child in the ack. I2 does not decide it — it carries it —
	// so a plugin acting on ungranted power is impossible: the ack precedes any
	// frame the plugin may act on.
	Granted []string
	// Environment is appended to the host's own environment for the child, not
	// substituted for it: a real plugin needs the base environment (PATH, and
	// SystemRoot on Windows) to run at all, and a test appends only its markers.
	Environment []string

	HandshakeTimeout time.Duration
	ShutdownTimeout  time.Duration
	MaxRestarts      int
	InitialBackoff   time.Duration
	MaxBackoff       time.Duration
	// FrameBuffer sizes the Frames() channel; defaulted, overridable in tests.
	FrameBuffer int
}

func (c *Config) applyDefaults() {
	if c.HandshakeTimeout <= 0 {
		c.HandshakeTimeout = 5 * time.Second
	}
	if c.ShutdownTimeout <= 0 {
		c.ShutdownTimeout = 2 * time.Second
	}
	if c.InitialBackoff <= 0 {
		c.InitialBackoff = 100 * time.Millisecond
	}
	if c.MaxBackoff <= 0 {
		c.MaxBackoff = 5 * time.Second
	}
	if c.MaxRestarts < 0 {
		c.MaxRestarts = 0
	}
	if c.FrameBuffer <= 0 {
		c.FrameBuffer = 64
	}
}

// Supervisor owns one plugin process across its restarts. Its public surface is
// deliberately small: Frames to consume pushes, Done/Err to observe the terminal
// state, and Close to stop and reap. Everything else runs on its own goroutine.
type Supervisor struct {
	cfg    Config
	ctx    context.Context
	cancel context.CancelFunc

	frames chan Frame
	done   chan struct{}

	// send holds the live child's stdin encoder so SendAction can route an
	// `action` frame to it (I4). It carries its own lock, separate from mu below,
	// so a SendAction blocked writing a frame cannot block run()'s reads and
	// writes of err.
	send sender

	mu  sync.Mutex
	err error

	closeOnce sync.Once
}

// Start spawns and supervises the plugin WITHOUT blocking: the handshake, the
// reader and any restart run on the supervisor's own goroutine, so mounting a
// plugin never stalls the UI loop. The first forwarded frame is the observable
// proof the handshake completed.
func Start(ctx context.Context, cfg Config) *Supervisor {
	cfg.applyDefaults()
	child, cancel := context.WithCancel(ctx)
	s := &Supervisor{
		cfg:    cfg,
		ctx:    child,
		cancel: cancel,
		frames: make(chan Frame, cfg.FrameBuffer),
		done:   make(chan struct{}),
	}
	go s.run()
	return s
}

// Frames delivers every non-handshake plugin→host frame. It is closed when the
// supervisor stops (Done fires at the same time), so a consumer ranging over it
// terminates cleanly on Close or on a fatal/exhausted-restart stop.
func (s *Supervisor) Frames() <-chan Frame { return s.frames }

// Done is closed when supervision has ended for any reason: Close, a fatal
// protocol error, or the restart budget exhausted.
func (s *Supervisor) Done() <-chan struct{} { return s.done }

// Err reports why supervision ended, or nil if it ended by Close. It is only
// meaningful after Done fires.
func (s *Supervisor) Err() error { s.mu.Lock(); defer s.mu.Unlock(); return s.err }

// Close cancels supervision and kills the child's whole process group, blocking
// until the supervisor goroutine has finished. It is idempotent. This is the
// runtime companion to /ui plugin remove <id>: after it returns, no plugin
// process and no reader survive.
func (s *Supervisor) Close() error {
	s.closeOnce.Do(func() {
		s.cancel()
		<-s.done
	})
	return nil
}

// run is the restart loop. Each attempt spawns, handshakes and reads until the
// process dies; a restartable death costs one attempt of the bounded budget and
// a doubling backoff, while a fatal protocol error or an exhausted budget stops
// and records the error. A Close (ctx cancelled) is a clean stop, not an error.
func (s *Supervisor) run() {
	defer close(s.done)
	defer close(s.frames)
	backoff := s.cfg.InitialBackoff
	for attempt := 0; ; attempt++ {
		err := s.runOnce()
		if s.ctx.Err() != nil {
			// Close requested: not an error state, just a stop. runOnce has
			// already killed the child on its way out.
			return
		}
		if errors.Is(err, ErrFatalProtocol) || attempt >= s.cfg.MaxRestarts {
			s.mu.Lock()
			s.err = err
			s.mu.Unlock()
			return
		}
		t := time.NewTimer(backoff)
		select {
		case <-t.C:
		case <-s.ctx.Done():
			t.Stop()
			return
		}
		if backoff *= 2; backoff > s.cfg.MaxBackoff {
			backoff = s.cfg.MaxBackoff
		}
	}
}

// runOnce spawns one child, handshakes, and reads until the stream ends or Close
// is requested. It always leaves the child dead and reaped before returning, so
// no orphan survives a restart or a stop.
func (s *Supervisor) runOnce() error {
	c, err := s.spawn()
	if err != nil {
		// A spawn failure (missing executable, pipe error) is restartable but
		// bounded: a transient failure recovers on a later attempt, and a
		// permanent one (no such file) exhausts the budget and stops rather
		// than looping forever.
		return fmt.Errorf("%w: spawn: %v", ErrUnexpectedExit, err)
	}
	if err := s.handshake(c); err != nil {
		c.close(s.cfg.ShutdownTimeout)
		return err
	}
	// The ack has been sent, so the child knows its grants and may now receive an
	// action (I4). Register its encoder for SendAction, and clear it before the
	// child is reaped so a press racing the death sees no live sender and reports
	// ErrPluginNotLive rather than writing to a closing pipe.
	s.send.set(c.enc)
	defer s.send.clear()
	readErr := make(chan error, 1)
	go func() { readErr <- s.readLoop(c) }()
	select {
	case err := <-readErr:
		// The stream ended: the process is dead or dying. Kill the group to be
		// certain nothing survives, then report the (restartable) death.
		c.close(s.cfg.ShutdownTimeout)
		return err
	case <-s.ctx.Done():
		// Close requested. Terminate the group, then JOIN the reader so run()
		// can close Frames() with no send racing it — the reader returns as soon
		// as the killed pipe reaches EOF.
		c.close(s.cfg.ShutdownTimeout)
		<-readErr
		return s.ctx.Err()
	}
}

// spawn builds and starts the child with its process group established, wires
// its stdin/stdout to the ext/v1 encoder/scanner, and reserves stderr for human
// logs (ADR-0007) by pointing it at the host's own stderr — a plugin's stray
// println therefore cannot corrupt the frame stream.
func (s *Supervisor) spawn() (*child, error) {
	m := s.cfg.Manifest
	cmd := exec.Command(m.Executable, m.Args...)
	cmd.Env = append(os.Environ(), s.cfg.Environment...)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	cmd.Stderr = os.Stderr
	if err := prepareProcess(cmd); err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	// The Windows job object can only be created once the pid exists; on unix
	// this is a no-op because Setpgid already ran before Start. A failure here
	// means the child is running but uncontainable, so it is killed at once.
	if err := attachProcess(cmd); err != nil {
		_ = killProcessTree(cmd)
		_ = cmd.Wait()
		return nil, err
	}
	sc := bufio.NewScanner(stdout)
	sc.Buffer(make([]byte, 0, 64*1024), maxLineBytes+2)
	return &child{cmd: cmd, stdin: stdin, enc: json.NewEncoder(stdin), scanner: sc}, nil
}

// handshake reads the plugin's hello (bounded by the handshake timeout, the way
// the core convention has the spawned process greet first), gates on the
// protocol token, and answers with the ack that carries plugin_id and granted.
// The ack precedes any frame the reader forwards, so the plugin knows its powers
// before it can act on them.
func (s *Supervisor) handshake(c *child) error {
	line, err := c.readLine(s.ctx, s.cfg.HandshakeTimeout)
	if err != nil {
		if errors.Is(err, errReadTimeout) {
			return ErrHandshakeTimeout
		}
		return fmt.Errorf("%w: reading plugin hello: %v", ErrFatalProtocol, err)
	}
	var hello wireHello
	if err := json.Unmarshal([]byte(line), &hello); err != nil {
		return fmt.Errorf("%w: plugin hello is not JSON: %v", ErrFatalProtocol, err)
	}
	if hello.Type != "hello" {
		return fmt.Errorf("%w: expected a hello frame first, got type %q", ErrFatalProtocol, hello.Type)
	}
	if hello.Protocol != s.cfg.Manifest.Protocol {
		return fmt.Errorf("%w: plugin speaks protocol %q but its manifest declares %q; the wire version is code on both sides and is not negotiated (ADR-0007)", ErrFatalProtocol, hello.Protocol, s.cfg.Manifest.Protocol)
	}
	// granted is normalised to a non-nil slice so the ack always carries a
	// "granted" array, never null: a plugin reading it must distinguish "no
	// capabilities" from "field missing", and an explicit empty array says the
	// former unambiguously.
	granted := s.cfg.Granted
	if granted == nil {
		granted = []string{}
	}
	ack := wireHello{Type: "hello", Protocol: s.cfg.Manifest.Protocol, PluginID: s.cfg.Manifest.ID, Granted: granted}
	if err := c.enc.Encode(ack); err != nil {
		return fmt.Errorf("%w: sending ack: %v", ErrUnexpectedExit, err)
	}
	return nil
}

// readLoop drains the child's stdout, forwarding each well-formed frame. A
// single malformed frame is DROPPED and reading continues (ADR-0007: the
// offending frame is dropped, not the process); the stream ending — EOF or any
// read error — is the restartable death the supervisor exists to handle. A
// Close (ctx cancelled) returns the context error, which run() reads as a clean
// stop.
func (s *Supervisor) readLoop(c *child) error {
	for {
		line, err := c.readLine(s.ctx, 0)
		if err != nil {
			if s.ctx.Err() != nil {
				return s.ctx.Err()
			}
			return fmt.Errorf("%w: %v", ErrUnexpectedExit, err)
		}
		var head struct {
			Type string `json:"type"`
		}
		if err := json.Unmarshal([]byte(line), &head); err != nil {
			// I2 drops a single malformed frame and continues. The
			// repeated-malformed-stream kill decision (ADR-0007) is a later
			// refinement; it is noted here so a bare `continue` is not mistaken
			// for the whole policy.
			continue
		}
		f := Frame{Type: head.Type, Raw: json.RawMessage(append([]byte(nil), line...))}
		select {
		case s.frames <- f:
		case <-s.ctx.Done():
			return s.ctx.Err()
		}
	}
}

// child is one running plugin process and its wire endpoints.
type child struct {
	cmd     *exec.Cmd
	stdin   io.WriteCloser
	enc     *json.Encoder
	scanner *bufio.Scanner

	waitOnce sync.Once
	waitErr  error
}

// errReadTimeout is the sentinel readLine returns when its per-read deadline
// passes, distinct from a context cancel so handshake can map it to
// ErrHandshakeTimeout (restartable) rather than a fatal read error.
var errReadTimeout = errors.New("read timed out")

// readLine reads one line, respecting context cancellation and an optional
// timeout (0 = none). It copies the cancellable-read pattern from ndjson.go
// rather than importing it: a bufio.Scanner read cannot itself be cancelled, so
// the read runs on a goroutine and the caller selects on it, the context, and
// the timer. The pending goroutine is bounded — after a timeout or cancel the
// child is killed and its pipe closes, so the blocked Scan returns and the
// goroutine exits; the child is never read from concurrently because it is
// killed rather than reused.
func (c *child) readLine(ctx context.Context, timeout time.Duration) (string, error) {
	type result struct {
		line string
		err  error
	}
	ch := make(chan result, 1)
	go func() {
		if c.scanner.Scan() {
			ch <- result{line: c.scanner.Text()}
			return
		}
		err := c.scanner.Err()
		if err == nil {
			err = io.EOF
		}
		ch <- result{err: err}
	}()
	var timer <-chan time.Time
	if timeout > 0 {
		t := time.NewTimer(timeout)
		defer t.Stop()
		timer = t.C
	}
	select {
	case r := <-ch:
		return r.line, r.err
	case <-timer:
		return "", errReadTimeout
	case <-ctx.Done():
		return "", ctx.Err()
	}
}

// close shuts the child's process group down gracefully (terminateProcess),
// then kills the whole group if the deadline passes, and reaps it. Safe to call
// once per child; runOnce is the sole caller and calls it exactly once.
func (c *child) close(timeout time.Duration) {
	_ = terminateProcess(c.cmd)
	done := make(chan struct{})
	go func() { c.wait(); close(done) }()
	t := time.NewTimer(timeout)
	defer t.Stop()
	select {
	case <-done:
	case <-t.C:
		_ = killProcessTree(c.cmd)
		<-done
	}
	releaseProcess(c.cmd)
	_ = c.stdin.Close()
}

// wait reaps the process exactly once, memoising the result so close's timeout
// path and its graceful path cannot both call Wait.
func (c *child) wait() error {
	c.waitOnce.Do(func() { c.waitErr = c.cmd.Wait() })
	return c.waitErr
}
