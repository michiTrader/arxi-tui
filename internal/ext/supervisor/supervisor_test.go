package supervisor

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/michiTrader/arxi_tui/internal/ext"
)

// TestHelperProcess is not a test: it is the plugin the supervisor tests spawn.
// The standard Go pattern re-execs this very test binary with a -test.run that
// selects only this function and an ARXI_EXT_HELPER marker in the environment,
// so a real subprocess speaks the ext/v1 wire without shipping a fixture binary.
// Every path either os.Exit()s or blocks until stdin closes, so the testing
// framework never writes its own PASS line to the stdout the host reads as the
// frame stream.
func TestHelperProcess(t *testing.T) {
	if os.Getenv("ARXI_EXT_HELPER") == "" {
		return
	}
	enc := json.NewEncoder(os.Stdout)
	in := newLineReader(os.Stdin)

	switch os.Getenv("ARXI_MODE") {
	case "nohello":
		// Publish before greeting: the host must refuse (a hello is expected
		// first) and treat it as fatal, never forwarding this frame.
		_ = enc.Encode(map[string]any{"type": "bind", "field": "price", "value": "$1"})
		select {}
	case "badproto":
		_ = enc.Encode(map[string]string{"type": "hello", "protocol": "ext/v2"})
		select {}
	case "slow":
		// Greet after the host's handshake deadline, to exercise the timeout.
		time.Sleep(3 * time.Second)
		_ = enc.Encode(map[string]string{"type": "hello", "protocol": "ext/v1"})
		select {}
	}

	// Normal handshake: greet, then read and verify the ack.
	_ = enc.Encode(map[string]string{"type": "hello", "protocol": "ext/v1"})
	line, err := in.read()
	if err != nil {
		os.Exit(10)
	}
	var ack struct {
		Type     string   `json:"type"`
		Protocol string   `json:"protocol"`
		PluginID string   `json:"plugin_id"`
		Granted  []string `json:"granted"`
	}
	if json.Unmarshal([]byte(line), &ack) != nil || ack.Type != "hello" || ack.Protocol != "ext/v1" {
		os.Exit(11)
	}
	if exp := os.Getenv("ARXI_EXPECT_ID"); exp != "" && ack.PluginID != exp {
		os.Exit(12)
	}
	if exp := os.Getenv("ARXI_EXPECT_GRANTED"); exp != "" && strings.Join(ack.Granted, ",") != exp {
		os.Exit(13)
	}

	// Publishing this frame is the observable proof the ack was correct: the
	// host only ever sees it if the handshake completed and the ack passed the
	// checks above. A supervisor that sent a wrong plugin_id or granted set
	// makes this helper exit non-zero, and the host forwards nothing.
	_ = enc.Encode(map[string]any{"type": "bind", "field": "price", "value": "$1.23"})

	if os.Getenv("ARXI_MODE") == "die" {
		// Exit cleanly after one frame so the host sees EOF and restarts. Each
		// launch forwards exactly one frame, so the frame count equals the
		// number of launches — which is how the bounded-restart test counts.
		os.Exit(0)
	}
	// Otherwise stay alive, draining host→plugin frames until the pipe closes
	// (the host killed us), then exit.
	for {
		if _, err := in.read(); err != nil {
			os.Exit(0)
		}
	}
}

// lineReader reads newline-delimited frames with the same 1 MiB cap the wire
// uses, so the helper and the host agree on framing.
type lineReader struct{ sc *bufio.Scanner }

func newLineReader(r *os.File) *lineReader {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), (1<<20)+2)
	return &lineReader{sc: sc}
}

func (l *lineReader) read() (string, error) {
	if l.sc.Scan() {
		return l.sc.Text(), nil
	}
	return "", errClosed
}

var errClosed = errors.New("closed")

// helperConfig builds a Config that re-execs this test binary as the plugin.
func helperConfig(mode, id string, granted []string) Config {
	env := []string{"ARXI_EXT_HELPER=1", "ARXI_MODE=" + mode}
	if id != "" {
		env = append(env, "ARXI_EXPECT_ID="+id)
	}
	if len(granted) > 0 {
		env = append(env, "ARXI_EXPECT_GRANTED="+strings.Join(granted, ","))
	}
	return Config{
		Manifest: ext.Manifest{
			ID:         id,
			Name:       "helper",
			Version:    "1",
			Protocol:   "ext/v1",
			Executable: os.Args[0],
			Args:       []string{"-test.run=^TestHelperProcess$"},
		},
		Granted:          granted,
		Environment:      env,
		HandshakeTimeout: 2 * time.Second,
		ShutdownTimeout:  time.Second,
		InitialBackoff:   time.Millisecond,
		FrameBuffer:      8,
	}
}

// TestSupervisorHandshakesAndForwardsFrame is the happy path AND the ack-content
// check in one: the helper publishes its bind frame only after verifying the
// ack carried the plugin_id ("tick") and the granted set the host was told to
// send, so receiving the frame proves both the handshake completed and the ack
// was correct. Counterfactual: neutering handshake's ack (a wrong plugin_id or
// an empty granted set) makes the helper os.Exit(12|13) and this test times out
// with no frame — run by hand to confirm the ack contents are load-bearing.
func TestSupervisorHandshakesAndForwardsFrame(t *testing.T) {
	granted := []string{"events.subscribe", "events.emit"}
	s := Start(context.Background(), helperConfig("", "tick", granted))
	defer s.Close()

	select {
	case f, ok := <-s.Frames():
		if !ok {
			t.Fatalf("Frames closed before any frame; supervisor err=%v — the "+
				"handshake or ack failed and the helper exited instead of publishing", s.Err())
		}
		if f.Type != "bind" {
			t.Fatalf("forwarded frame type = %q, want \"bind\"; the reader must "+
				"forward the plugin's frame verbatim with its type intact", f.Type)
		}
		if !strings.Contains(string(f.Raw), "\"price\"") {
			t.Fatalf("forwarded frame raw = %s, want it to carry the plugin's "+
				"published field; the reader must keep the bytes for I3 to parse", f.Raw)
		}
	case <-time.After(6 * time.Second):
		t.Fatal("no frame within 6s; the handshake never completed or the ack " +
			"was rejected by the helper (see the ARXI_EXPECT_* checks)")
	}
}

// TestSupervisorBoundedRestarts proves the restart budget is load-bearing in
// both directions. The helper handshakes, publishes one frame, then exits — so
// the number of forwarded frames equals the number of launches. With
// MaxRestarts=0 the process runs once; with MaxRestarts=2 it runs three times.
// Counterfactual: removing the `attempt >= MaxRestarts` guard in run() makes
// this loop forever (the frame count never settles), and dropping the backoff
// makes it a busy respawn — both are what the bound and backoff prevent.
func TestSupervisorBoundedRestarts(t *testing.T) {
	for _, tc := range []struct {
		name        string
		maxRestarts int
		wantLaunch  int
	}{
		{"no restart runs once", 0, 1},
		{"two restarts run three times", 2, 3},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := helperConfig("die", "tick", nil)
			cfg.MaxRestarts = tc.maxRestarts
			s := Start(context.Background(), cfg)
			defer s.Close()

			launches := 0
			deadline := time.After(10 * time.Second)
			for {
				select {
				case _, ok := <-s.Frames():
					if !ok {
						// Frames closes when supervision stops; that is when the
						// launch count is final.
						if launches != tc.wantLaunch {
							t.Fatalf("launches = %d, want %d; each launch publishes "+
								"one frame, so a wrong count means the restart budget "+
								"was not honoured", launches, tc.wantLaunch)
						}
						if !errors.Is(s.Err(), ErrUnexpectedExit) {
							t.Fatalf("terminal err = %v, want ErrUnexpectedExit; a "+
								"clean process exit the host did not request is the "+
								"restartable death", s.Err())
						}
						return
					}
					launches++
				case <-deadline:
					t.Fatalf("did not settle within 10s (saw %d launches); the "+
						"restart bound may not be terminating the loop", launches)
				}
			}
		})
	}
}

// TestSupervisorRefusesWrongProtocolWithoutRestart proves a fatal protocol error
// stops immediately and does NOT consume the restart budget. The helper sends a
// hello naming ext/v2; despite MaxRestarts=3 the supervisor stops at once with
// ErrFatalProtocol. Counterfactual: classifying the protocol mismatch as
// ErrUnexpectedExit instead would restart three times (slower, and ending in
// ErrUnexpectedExit) — the assertion on ErrFatalProtocol is what distinguishes
// "will never work" from "died, try again."
func TestSupervisorRefusesWrongProtocolWithoutRestart(t *testing.T) {
	cfg := helperConfig("badproto", "tick", nil)
	cfg.MaxRestarts = 3
	s := Start(context.Background(), cfg)
	defer s.Close()

	select {
	case <-s.Done():
		if !errors.Is(s.Err(), ErrFatalProtocol) {
			t.Fatalf("terminal err = %v, want ErrFatalProtocol; a wrong wire "+
				"version is code on both sides and a restart cannot fix it", s.Err())
		}
	case <-time.After(6 * time.Second):
		t.Fatal("supervisor did not stop within 6s; a fatal protocol error must " +
			"stop at once, not burn the restart budget with backoff")
	}
}

// TestSupervisorRefusesPublishBeforeHello proves the host demands a hello first:
// a plugin that publishes a bind before greeting is refused fatally and its
// frame is never forwarded. Counterfactual: dropping handshake's `hello.Type !=
// "hello"` check would forward that premature bind and accept a plugin that
// never announced its wire version.
func TestSupervisorRefusesPublishBeforeHello(t *testing.T) {
	cfg := helperConfig("nohello", "tick", nil)
	cfg.MaxRestarts = 0
	s := Start(context.Background(), cfg)
	defer s.Close()

	select {
	case f, ok := <-s.Frames():
		if ok {
			t.Fatalf("forwarded a frame (%q) before any hello; the host must "+
				"refuse a publish that precedes the handshake", f.Type)
		}
		// Frames closed with no value: supervision stopped, which is correct.
	case <-time.After(6 * time.Second):
		t.Fatal("supervisor neither forwarded a frame nor stopped within 6s")
	}
	if !errors.Is(s.Err(), ErrFatalProtocol) {
		t.Fatalf("terminal err = %v, want ErrFatalProtocol; a frame before the "+
			"hello is a protocol violation, not a restartable death", s.Err())
	}
}

// TestSupervisorHandshakeTimeoutIsBounded proves a plugin that never greets is
// stopped by the handshake deadline and — because a slow start is not proof of
// a broken plugin — treated as a restartable ErrHandshakeTimeout, not a fatal.
// The tight HandshakeTimeout and MaxRestarts=0 keep the test fast: one attempt,
// one timeout, stop.
func TestSupervisorHandshakeTimeoutIsBounded(t *testing.T) {
	cfg := helperConfig("slow", "tick", nil)
	cfg.HandshakeTimeout = 200 * time.Millisecond
	cfg.MaxRestarts = 0
	s := Start(context.Background(), cfg)
	defer s.Close()

	select {
	case <-s.Done():
		if !errors.Is(s.Err(), ErrHandshakeTimeout) {
			t.Fatalf("terminal err = %v, want ErrHandshakeTimeout; a plugin that "+
				"never greets must time out, and a timeout is restartable not fatal", s.Err())
		}
	case <-time.After(5 * time.Second):
		t.Fatal("handshake timeout did not fire within 5s")
	}
}

// TestSupervisorCloseReapsTheProcess proves Close stops a live, blocking plugin:
// the helper handshakes, publishes, then blocks reading forever, so the only way
// Done can fire is the supervisor killing and reaping it. Done firing within the
// deadline is therefore proof the process was terminated — a Close that merely
// abandoned the child would hang here past the timeout.
//
// The whole-process-group kill (an orphaned grandchild) is guaranteed by the
// ported mechanics rather than asserted here, because spawning a grandchild and
// probing its liveness is not portable across the unix/Windows split this test
// runs on; the reap of the direct child is what this test pins.
func TestSupervisorCloseReapsTheProcess(t *testing.T) {
	s := Start(context.Background(), helperConfig("", "tick", nil))

	select {
	case _, ok := <-s.Frames():
		if !ok {
			t.Fatalf("Frames closed before the process was live; err=%v", s.Err())
		}
	case <-time.After(6 * time.Second):
		t.Fatal("plugin never became live (no first frame)")
	}

	done := make(chan struct{})
	go func() { _ = s.Close(); close(done) }()
	select {
	case <-done:
	case <-time.After(6 * time.Second):
		t.Fatal("Close did not return within 6s; the blocking plugin was not " +
			"killed and reaped")
	}

	select {
	case <-s.Done():
	default:
		t.Fatal("Done not closed after Close returned; supervision must be over")
	}
}
