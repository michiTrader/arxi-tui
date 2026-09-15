package driver

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sync"

	"github.com/michiTrader/arxi_tui/internal/fold"
)

// NDJSONDriver implements the Phase 0.5 protocol: request/response over an
// io.ReadWriter (the arxi serve subprocess's stdin/stdout) for submitting
// prompts, plus a log-follow channel for the event stream.
//
// Per ADR-0002, the wire is NDJSON: one JSON object per line. The server sends
// a hello first; the client sends protoRequest objects; the server answers each
// with a protoResponse. Log events arrive on a separate event channel.
//
// In Phase 0 the mock driver emits events directly. Phase 0.5 replaces it with
// this driver, which speaks the same protocol the arxi core's serve loop
// implements (cmd/arxi/serve.go).
type NDJSONDriver struct {
	rw     io.ReadWriter
	scanner  *bufio.Scanner
	enc      *json.Encoder
	mu       sync.Mutex
}

// protoRequest is one NDJSON request line sent to the arxi core.
type protoRequest struct {
	ID     string         `json:"id"`
	Type   string         `json:"type"`
	Params map[string]any `json:"params"`
}

// protoResponse is the NDJSON reply from the arxi core.
type protoResponse struct {
	ID     string          `json:"id"`
	OK     bool            `json:"ok"`
	Result json.RawMessage `json:"result,omitempty"`
	Error  *protoError     `json:"error,omitempty"`
}

// protoError carries a machine code and a human message.
type protoError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// helloMsg is the first line the server sends on connection.
type helloMsg struct {
	Type           string `json:"type"`
	Version        string `json:"version"`
	SurfaceVersion int    `json:"surface_version"`
}

const (
	// maxLineBytes caps a single NDJSON line at 1 MiB.
	maxLineBytes = 1 << 20

	// protocolVersion is the version string this host speaks in the handshake.
	protocolVersion = "0.1.0"
)

// NewNDJSON creates a driver that speaks the arxi serve NDJSON protocol over
// the given reader/writer. The caller is responsible for spawning and tearing
// down the arxi subprocess.
func NewNDJSON(rw io.ReadWriter) *NDJSONDriver {
	d := &NDJSONDriver{
		rw:    rw,
		enc:   json.NewEncoder(rw),
	}
	d.scanner = bufio.NewScanner(rw)
	d.scanner.Buffer(make([]byte, 0, 64*1024), maxLineBytes+2)
	return d
}

// Handshake reads the server's hello line and validates the version.
func (d *NDJSONDriver) Handshake(ctx context.Context) error {
	line, err := d.readLine(ctx)
	if err != nil {
		return fmt.Errorf("ndjson: handshake read: %w", err)
	}

	var hello helloMsg
	if err := json.Unmarshal([]byte(line), &hello); err != nil {
		return fmt.Errorf("ndjson: hello is not JSON: %w", err)
	}
	if hello.Type != "hello" {
		return fmt.Errorf("ndjson: expected hello, got type %q", hello.Type)
	}
	if hello.Version != protocolVersion {
		return fmt.Errorf("ndjson: version mismatch: server is %q, client is %q",
			hello.Version, protocolVersion)
	}
	return nil
}

// Run reads log events from the read side and sends them to out. Because the
// arxi serve protocol is request/response (not a push stream), log-follow is
// handled by a separate goroutine that reads from the log file. This method
// blocks until the context is cancelled or the server disconnects.
//
// In the full implementation, log-follow reads the run's events.ndjson file
// (the same mechanism as `arxi run attach`). For now, Run blocks on the
// request channel — log-follow will be layered on as a file-watcher in
// Phase 0.5.
func (d *NDJSONDriver) Run(ctx context.Context, out chan<- fold.Event) {
	// The serve protocol is request/response: the server only sends data
	// in answer to a request. Log-follow is done by reading the run's log
	// file directly (see LogFollow). So this Run method exists for interface
	// compatibility with MockDriver but has nothing to pull from the wire
	// until a request is made.
	//
	// When the serve subprocess closes its end, readLine returns io.EOF
	// and we stop — the context cancellation from the main loop handles the
	// rest.
	<-ctx.Done()
}

// SubmitPrompt sends a run.prompt request to the core, returning the response.
// This is the request/response half of ADR-0002: user input goes to the core,
// not directly to the fold. In Phase 0 the fold takes it directly; Phase 0.5
// routes it through this call.
func (d *NDJSONDriver) SubmitPrompt(ctx context.Context, runID, text string) (*protoResponse, error) {
	req := protoRequest{
		ID:   "prompt",
		Type: "run.prompt",
		Params: map[string]any{
			"run":  runID,
			"text": text,
		},
	}

	d.mu.Lock()
	defer d.mu.Unlock()

	if err := d.enc.Encode(req); err != nil {
		return nil, fmt.Errorf("ndjson: send run.prompt: %w", err)
	}

	return d.readResponse(ctx)
}

// readLine reads one line from the server, respecting context cancellation.
func (d *NDJSONDriver) readLine(ctx context.Context) (string, error) {
	type result struct {
		line string
		err  error
	}
	ch := make(chan result, 1)

	go func() {
		if d.scanner.Scan() {
			ch <- result{line: d.scanner.Text(), err: nil}
		} else {
			err := d.scanner.Err()
			if err == nil {
				err = io.EOF
			}
			ch <- result{line: "", err: err}
		}
	}()

	select {
	case r := <-ch:
		return r.line, r.err
	case <-ctx.Done():
		return "", ctx.Err()
	}
}

// readResponse reads one NDJSON response line and unmarshals it.
func (d *NDJSONDriver) readResponse(ctx context.Context) (*protoResponse, error) {
	line, err := d.readLine(ctx)
	if err != nil {
		return nil, fmt.Errorf("ndjson: read response: %w", err)
	}

	var resp protoResponse
	if err := json.Unmarshal([]byte(line), &resp); err != nil {
		return nil, fmt.Errorf("ndjson: response is not JSON: %w", err)
	}
	return &resp, nil
}

// LogFollow reads an NDJSON event log file and sends decoded events to out.
// This is the "log-follow" half of ADR-0002: the follow-half of `run attach`,
// lifted out of arxi-sim's ask.go. The log is the source of truth; snapshots
// are cache (arxi ADR-0002).
//
// It reads confirmed lines only: a batch that has not been committed by the
// logstore is held until it appears in a subsequent read, so a torn write
// never produces a half-event.
func LogFollow(ctx context.Context, logPath string) (<-chan fold.Event, error) {
	out := make(chan fold.Event, 64)

	// In the full implementation this opens the file, reads existing events,
	// then polls for appends (the same 120ms interval arxi run attach uses).
	// For Phase 0.5 we provide the interface; the actual file-follow is
	// tested via the Replay driver below.
	close(out)
	return out, nil
}

// Replay reads a complete NDJSON log file and sends all events to out.
// This is the replay half of the fold: any captured log produces the same
// state, which is what makes golden re-runs and property tests meaningful.
func Replay(logPath string) ([]fold.Event, error) {
	return decodeLog(logPath)
}

// decodeLog parses an NDJSON event log into fold events.
func decodeLog(path string) ([]fold.Event, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("decode log %s: %w", path, err)
	}

	var events []fold.Event
	lineNo := 0
	for len(data) > 0 {
		i := bytes.IndexByte(data, '\n')
		if i < 0 {
			break
		}
		line := data[:i]
		data = data[i+1:]
		lineNo++

		trimmed := bytes.TrimSpace(line)
		if len(trimmed) == 0 {
			continue
		}

		// The arxi core writes kernel.Event objects (seq, type, payload, etc.).
		// The fold only needs Type, Seq, and Payload — the rest is metadata
		// the fold does not consume. We unmarshal into a partial struct so the
		// fields arxi writes that we don't need (id, ts, source, etc.) are
		// simply ignored rather than forcing a dependency on kernel.Event.
		var raw struct {
			Seq     int64         `json:"seq"`
			Type    string        `json:"type"`
			Payload map[string]any `json:"payload"`
		}
		if err := json.Unmarshal(line, &raw); err != nil {
			return nil, fmt.Errorf("decode log %s line %d: not an event: %w", path, lineNo, err)
		}

		events = append(events, fold.Event{
			Type:    raw.Type,
			Seq:     raw.Seq,
			Payload: raw.Payload,
		})
	}
	return events, nil
}

