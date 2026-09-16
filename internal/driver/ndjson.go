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
	"time"

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
	rw      io.ReadWriter
	scanner *bufio.Scanner
	enc     *json.Encoder
	mu      sync.Mutex
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

	// pollInterval is how often LogFollow re-checks the event log for appends
	// — the same cadence arxi run attach uses.
	pollInterval = 120 * time.Millisecond
)

// NewNDJSON creates a driver that speaks the arxi serve NDJSON protocol over
// the given reader/writer. The caller is responsible for spawning and tearing
// down the arxi subprocess.
func NewNDJSON(rw io.ReadWriter) *NDJSONDriver {
	d := &NDJSONDriver{
		rw:  rw,
		enc: json.NewEncoder(rw),
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

// Run blocks until the context is cancelled. The serve protocol is
// request/response, so there is nothing to pull from the wire here — log-follow
// is handled by LogFollow (file polling).
func (d *NDJSONDriver) Run(ctx context.Context, out chan<- fold.Event) {
	<-ctx.Done()
}

// SubmitPrompt sends a run.prompt request to the core, returning the response.
// This is the request/response half of ADR-0002: user input goes to the core,
// not directly to the fold.
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
//
// LogFollow first drains any existing events from the file, then polls the
// file for appends at the same 120ms interval arxi run attach uses.
func LogFollow(ctx context.Context, logPath string) (<-chan fold.Event, error) {
	out := make(chan fold.Event, 64)

	f, err := os.Open(logPath)
	if err != nil {
		return nil, fmt.Errorf("ndjson: log-follow open %s: %w", logPath, err)
	}

	go func() {
		defer close(out)
		defer f.Close()

		// First: drain any events already written to the log.
		if err := replayFile(f, out); err != nil {
			return
		}

		// Then: poll for appends.
		ticker := time.NewTicker(pollInterval)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if err := replayFile(f, out); err != nil {
					return
				}
			}
		}
	}()

	return out, nil
}

// replayFile reads any fully-formed lines from f (without a trailing partial
// line) and sends them as events. It reads from the current file offset, so
// repeated calls pick up only new appends. A trailing partial line is rewound
// so it is picked up on the next poll.
func replayFile(f *os.File, out chan<- fold.Event) error {
	buf := make([]byte, 64*1024)
	n, err := f.Read(buf)
	if err != nil && err != io.EOF {
		return err
	}
	if n == 0 {
		return nil
	}

	data := buf[:n]
	// Only process complete lines; any trailing partial line is left for the
	// next poll (rewind the file to before those bytes).
	lastNL := bytes.LastIndexByte(data, '\n')
	if lastNL < 0 {
		// No complete line this round — rewind so next poll re-reads.
		_, _ = f.Seek(-int64(n), io.SeekCurrent)
		return nil
	}

	complete := data[:lastNL+1]
	leftover := data[lastNL+1:]

	// Rewind past the partial tail so next read gets it again.
	if len(leftover) > 0 {
		_, _ = f.Seek(-int64(len(leftover)), io.SeekCurrent)
	}

	for _, line := range bytes.Split(complete, []byte("\n")) {
		trimmed := bytes.TrimSpace(line)
		if len(trimmed) == 0 {
			continue
		}
		var raw struct {
			Seq     int64          `json:"seq"`
			Type    string         `json:"type"`
			Payload map[string]any `json:"payload"`
		}
		if err := json.Unmarshal(trimmed, &raw); err != nil {
			return err
		}
		out <- fold.Event{
			Type:    raw.Type,
			Seq:     raw.Seq,
			Payload: raw.Payload,
		}
	}
	return nil
}

// Replay reads a complete NDJSON log file and returns all events.
// This is the replay half of the fold: any captured log produces the same
// stat
// Replay reads a complete NDJSON log file and returns all events.
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
		var line []byte
		if i < 0 {
			// Last line without trailing newline — process it if non-empty.
			line = data
			data = nil
		} else {
			line = data[:i]
			data = data[i+1:]
		}
		lineNo++

		trimmed := bytes.TrimSpace(line)
		if len(trimmed) == 0 {
			continue
		}

		var raw struct {
			Seq     int64          `json:"seq"`
			Type    string         `json:"type"`
			Payload map[string]any `json:"payload"`
		}
		if err := json.Unmarshal(trimmed, &raw); err != nil {
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
