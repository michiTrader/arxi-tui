package driver

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
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
	// hello is what the core announced, kept because `implemented` is the
	// only place a client can learn that a declared verb has no executor.
	hello *Hello
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

// protoError carries a machine code and a human message, plus the remedy the
// core offers. `fix` was missing from this struct and is the same shape of
// remedy `run why` prints; dropping it threw away the one part of the refusal
// that says what to do next -- the same defect this repo already refuses to
// ship in scene diagnostics.
type protoError struct {
	Code    string   `json:"code"`
	Message string   `json:"message"`
	Fix     []string `json:"fix,omitempty"`
	// Operation names the host operation that failed, when the refusal came
	// from the core's host layer rather than the protocol layer. Measured:
	// run.attach against a missing job answers code "not_found" with
	// operation "event.subscribe".
	Operation string `json:"operation,omitempty"`
}

// Refusal is an answered request the core declined: ok:false with a code.
//
// It is an error type rather than a return value because the alternative is
// what shipped -- SubmitPrompt returned (response, nil) for a refusal, and the
// host's call site wrote `_ = drv.SubmitPrompt(...)`, so a prompt the core
// rejected was indistinguishable from one it accepted. serve.go made `ok` an
// explicit boolean to keep exactly this from being ambiguous: the two
// encodings "disagree the first time a server omits an empty error object or a
// client checks the wrong one, and the disagreement reads as success."
//
// The code is preserved as a field, not folded into the message, because the
// codes are a closed set whose whole purpose is the branch: "you asked
// wrongly" (retrying will not help) versus "this build cannot do that yet"
// (retrying after an upgrade will).
type Refusal struct {
	// Code is the core's machine code: one of malformed, unknown_type,
	// bad_params, not_implemented, failed, line_too_long -- plus codes the
	// host layer raises, measured live: internal, not_found,
	// invalid_argument.
	Code string
	// Message is the core's own sentence, carried verbatim. It is what a
	// person reads in the frame, so it is not paraphrased.
	Message string
	// Fix is the remedy the core suggests, e.g. ["arxi schema"].
	Fix []string
	// Operation is the host operation that failed, when the core named one.
	Operation string
	// Type is the request type that was refused, added by this client: the
	// core's refusal does not echo it, and a message with no verb in it is
	// not addressable.
	Type string
}

func (r *Refusal) Error() string {
	var b strings.Builder
	b.WriteString("arxi refused ")
	b.WriteString(r.Type)
	b.WriteString(" [")
	b.WriteString(r.Code)
	b.WriteString("]: ")
	b.WriteString(r.Message)
	if r.Operation != "" {
		b.WriteString(" (operation: ")
		b.WriteString(r.Operation)
		b.WriteString(")")
	}
	for _, f := range r.Fix {
		b.WriteString("\n  try: ")
		b.WriteString(f)
	}
	return b.String()
}

// Permanent reports whether retrying this request against this binary could
// ever succeed.
//
// This is the branch the closed code set exists for. A host that cannot ask
// the question either retries forever or gives up permanently, "and both are
// wrong half the time." For a TUI the consequence is concrete: a prompt
// answered not_implemented must be shown as a capability this build does not
// have, not as a send that failed.
func (r *Refusal) Permanent() bool {
	switch r.Code {
	case "not_implemented", "unknown_type", "bad_params", "malformed":
		return true
	default:
		// `failed`, `internal` and the host-layer codes describe this
		// attempt, not this build: a later identical request may succeed.
		return false
	}
}

// AsRefusal recovers a *Refusal from an error chain, so the host can branch on
// the code instead of matching English.
func AsRefusal(err error, target **Refusal) bool {
	var r *Refusal
	if errors.As(err, &r) {
		*target = r
		return true
	}
	return false
}

// Hello is the first line the server sends on connection, as the arxi core
// actually sends it (cmd/arxi/serve.go:helloMsg).
//
// The three list fields were originally omitted from this struct, which made
// the handshake drop them: json.Unmarshal into a struct without them discards
// the keys silently. serve.go states why the core sends `implemented` at all,
// and the sentence describes exactly the client that ignores it -- without the
// list "a client discovers that one type at a time by sending a request and
// reading a failure, which makes a permanent state look like a transient
// error." Measured against arxi 0.0.1-spec, `run.prompt` (the host's only
// verb) is in Types and absent from Implemented, so the distinction is not
// theoretical: the host's one request is declared and has no executor.
type Hello struct {
	Type    string `json:"type"`
	Version string `json:"version"`
	// SurfaceVersion is the vocabulary number, derived by the core from
	// surface.SurfaceVersion. This -- not Version -- is what the handshake
	// gates on. See hostSurfaceVersion.
	SurfaceVersion int `json:"surface_version"`
	// Types is every message type the surface declares; Implemented is the
	// subset this build has an executor for. A type in Types but not in
	// Implemented is answered `not_implemented`: a permanent gap, not a
	// transient failure.
	Types       []string `json:"types"`
	Implemented []string `json:"implemented"`
	// Capabilities is the effective capability set for this session's
	// principal, resolved by the core's host at connection time.
	Capabilities []string `json:"capabilities"`
}

const (
	// maxLineBytes caps a single NDJSON line at 1 MiB.
	maxLineBytes = 1 << 20

	// hostSurfaceVersion is the arxi surface vocabulary this host is written
	// against.
	//
	// The handshake gates on THIS and not on the core's `version` string. The
	// first implementation compared `version` against a "0.1.0" invented here,
	// and the core sends the version of its BINARY ("0.0.1-spec",
	// cmd/arxi/main.go:60), so the two could never be equal and every real
	// handshake was refused -- the bridge could not have opened once. Pinning
	// the binary version instead would only move the coupling: an arxi patch
	// release would then break the host for no reason, because a new binary
	// over the same surface speaks the same vocabulary.
	//
	// The surface version is the right gate because it is what the bind
	// inventory, the request parameters and the event field names are all
	// written against, and the core refuses unknown parameters rather than
	// ignoring them -- so talking v1 to a v2 core must fail loudly here, not
	// as a wrong frame later.
	hostSurfaceVersion = 1

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

	var hello Hello
	if err := json.Unmarshal([]byte(line), &hello); err != nil {
		return fmt.Errorf("ndjson: hello is not JSON: %w", err)
	}
	if hello.Type != "hello" {
		return fmt.Errorf("ndjson: expected hello, got type %q", hello.Type)
	}
	if hello.SurfaceVersion != hostSurfaceVersion {
		return fmt.Errorf("ndjson: surface version mismatch: the core serves "+
			"surface v%d and this host speaks surface v%d (core binary %q). "+
			"The surface is the request vocabulary and the event field names; "+
			"the core refuses parameters it does not declare, so continuing "+
			"would produce wrong frames rather than errors",
			hello.SurfaceVersion, hostSurfaceVersion, hello.Version)
	}

	d.mu.Lock()
	d.hello = &hello
	d.mu.Unlock()
	return nil
}

// Hello returns the greeting the core sent, or nil before a successful
// handshake. The caller needs it to tell a declared-but-unimplemented verb
// from one the surface does not have at all.
func (d *NDJSONDriver) Hello() *Hello {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.hello
}

// Declares reports whether the core's surface contains this message type.
// A type that is not declared is answered `unknown_type`: the client is wrong.
func (d *NDJSONDriver) Declares(msgType string) bool {
	h := d.Hello()
	if h == nil {
		return false
	}
	for _, t := range h.Types {
		if t == msgType {
			return true
		}
	}
	return false
}

// Implements reports whether THIS build of the core has an executor for the
// type. A declared type that is not implemented is answered
// `not_implemented`, which is permanent for this binary: retrying will not
// help, and the host should say so rather than present it as a failed send.
func (d *NDJSONDriver) Implements(msgType string) bool {
	h := d.Hello()
	if h == nil {
		return false
	}
	for _, t := range h.Implemented {
		if t == msgType {
			return true
		}
	}
	return false
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

	resp, err := d.readResponse(ctx)
	if err != nil {
		return nil, err
	}

	// ok:false is an answer, not a transport success. Returning it with a nil
	// error is the disagreement serve.go warns about, and it is what shipped:
	// the refusal "read as success" all the way up to a call site that
	// discarded the value. The response is still returned alongside the error
	// so a caller that wants the raw line has it.
	if !resp.OK {
		return resp, resp.refusal("run.prompt")
	}
	return resp, nil
}

// refusal converts an ok:false response into a *Refusal, preserving the code,
// the core's sentence and its remedy. A response with ok:false and no error
// object still becomes a refusal: `ok` is the field of record, and inventing a
// success because the error object was omitted is precisely the ambiguity the
// explicit boolean removes.
func (r *protoResponse) refusal(reqType string) *Refusal {
	if r.Error == nil {
		return &Refusal{
			Code: "failed",
			Message: "the core answered ok:false with no error object. `ok` is " +
				"the field of record, so this is a refusal with no stated reason",
			Type: reqType,
		}
	}
	return &Refusal{
		Code:      r.Error.Code,
		Message:   r.Error.Message,
		Fix:       r.Error.Fix,
		Operation: r.Error.Operation,
		Type:      reqType,
	}
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
// It reads CONFIRMED lines only, and that word has a specific meaning here
// which this function used to get wrong.
//
// The old implementation held back a trailing line with no newline and called
// that confirmed. It does stop a torn line, but newline-termination is not
// the commit point. arxi's commit protocol (logstore/store.go:219) is:
//
//  1. write pending.commit holding the log's current committed size, fsync;
//  2. append the whole batch to events.ndjson, fsync;
//  3. remove pending.commit -- "this is the commit point".
//
// Between 2 and 3 the log holds a batch of COMPLETE, newline-terminated
// records that are not committed. If the writer dies there, the core's own
// Open() calls rollbackPending() and TRUNCATES them away. A follower trusting
// newlines therefore delivers events the core then revokes -- and the fold is
// append-only, so the host cannot take them back. That is a wrong frame built
// from correctly-read bytes, which this repo holds to be worse than an error.
//
// So the follower checks pending.commit beside the log and never reads past
// the rollback point it names. Two consequences, both deliberate:
//
//   - The confirmed boundary is NOT monotonic. The core says so directly
//     (logstore/pending_race_test.go): "a marker naming a rollback point
//     behind what a caller already consumed pulls the reported boundary back
//     ... a caller cannot treat NextOffset as a high-water mark." The
//     follower therefore tracks its own delivered offset and never re-emits,
//     because re-emitting a seq the fold already has is a duplicate event,
//     not a correction.
//   - A missing marker means everything complete is confirmed, which is the
//     state every finished run's directory is in.
//
// LogFollow first drains the confirmed prefix, then polls at the same 120ms
// interval arxi run attach uses.
func LogFollow(ctx context.Context, logPath string) (<-chan fold.Event, error) {
	out := make(chan fold.Event, 64)

	f, err := os.Open(logPath)
	if err != nil {
		return nil, fmt.Errorf("ndjson: log-follow open %s: %w", logPath, err)
	}

	go func() {
		defer close(out)
		defer f.Close()

		// delivered is the byte offset through which events have been sent.
		// It is owned here rather than inferred from the file position
		// because the confirmed boundary can move BACKWARDS (a pending
		// marker naming an earlier rollback point), and a follower that
		// re-read from a rewound boundary would re-emit events the fold
		// already holds. A duplicate is not a correction: the fold appends.
		var delivered int64

		// First: drain the confirmed prefix already on disk.
		if err := followOnce(f, logPath, &delivered, out); err != nil {
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
				if err := followOnce(f, logPath, &delivered, out); err != nil {
					return
				}
			}
		}
	}()

	return out, nil
}

// followOnce delivers every complete record between *delivered and the
// confirmed boundary, then advances *delivered.
//
// It reads by absolute offset (ReadAt) rather than by the file's own cursor.
// The cursor made the previous implementation's correctness depend on
// seek-arithmetic around partial tails, and it cannot express the case this
// function exists for: a confirmed boundary that moved backwards must NOT
// rewind what has already been delivered.
func followOnce(f *os.File, logPath string, delivered *int64, out chan<- fold.Event) error {
	confirmed, err := confirmedEnd(f, logPath)
	if err != nil {
		return err
	}
	if confirmed <= *delivered {
		// Nothing new. This includes the backwards case: the boundary
		// retreated behind what was already sent, and the only safe action is
		// to send nothing, because the alternative is re-emitting events the
		// fold already has.
		return nil
	}

	body := make([]byte, confirmed-*delivered)
	if _, err := f.ReadAt(body, *delivered); err != nil && err != io.EOF {
		return err
	}

	for _, line := range bytes.Split(body, []byte("\n")) {
		trimmed := bytes.TrimSpace(line)
		if len(trimmed) == 0 {
			continue
		}
		ev, err := decodeEvent(trimmed)
		if err != nil {
			return err
		}
		// Passed through whole rather than rebuilt field by field. The
		// rebuild was a third place that had to know the event's shape, and
		// it did not: it copied Type/Seq/Payload and dropped Actor, so
		// decodeEvent could learn a field and the live stream would still not
		// carry it. That is the same "a decoder duplicated will disagree with
		// itself" failure this file consolidated decodeEvent to prevent,
		// reintroduced one struct literal at a time.
		out <- ev
	}
	*delivered = confirmed
	return nil
}

// confirmedEnd is the byte offset through which the log is committed AND
// complete: the smaller of the last newline and the pending marker's rollback
// point.
//
// Both halves are required and they answer different questions. The newline
// bound excludes a TORN record (a write in progress). The marker bound
// excludes a WHOLE batch that is written and not yet committed -- records that
// are individually complete and that the core will delete if the writer dies
// before step 3. Checking only newlines was the defect; checking only the
// marker would deliver half a line.
func confirmedEnd(f *os.File, logPath string) (int64, error) {
	info, err := f.Stat()
	if err != nil {
		return 0, err
	}
	end, err := lastNewlineBefore(f, info.Size())
	if err != nil {
		return 0, err
	}

	rollback, ok, err := pendingRollback(logPath)
	if err != nil {
		return 0, err
	}
	if ok && rollback < end {
		// The marker's rollback point is a byte offset in the committed
		// prefix, so it is already record-aligned; clamping to the previous
		// newline as well costs nothing and protects against a marker written
		// mid-record by a core the host has not measured.
		return lastNewlineBefore(f, rollback)
	}
	return end, nil
}

// lastNewlineBefore returns the offset just past the last '\n' at or before
// limit, i.e. the end of the last complete record.
func lastNewlineBefore(f *os.File, limit int64) (int64, error) {
	if limit <= 0 {
		return 0, nil
	}
	const window = 64 * 1024
	for end := limit; end > 0; {
		start := end - window
		if start < 0 {
			start = 0
		}
		buf := make([]byte, end-start)
		if _, err := f.ReadAt(buf, start); err != nil && err != io.EOF {
			return 0, err
		}
		if i := bytes.LastIndexByte(buf, '\n'); i >= 0 {
			return start + int64(i) + 1, nil
		}
		end = start
	}
	return 0, nil
}

// pendingRollback reads the logstore's pending.commit marker, if present.
//
// The marker lives beside events.ndjson in the run directory and holds the
// log's size before the in-flight append. Two spellings are accepted because
// the core accepts both (logstore readPendingMarker): a JSON object with
// pre_append_size, and a bare integer, which it parses as the legacy form.
// Reading only the JSON form would silently treat a legacy marker as absent --
// the same class of failure as reading only `seq` and not `sequence`.
//
// A marker that cannot be parsed is reported as an ERROR rather than treated
// as absent. Absent means "everything is confirmed", which is the most
// permissive possible reading, and inferring it from a marker this host does
// not understand would turn a file it cannot read into permission to deliver
// uncommitted events.
func pendingRollback(logPath string) (int64, bool, error) {
	body, err := os.ReadFile(filepath.Join(filepath.Dir(logPath), "pending.commit"))
	if errors.Is(err, os.ErrNotExist) {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, fmt.Errorf("ndjson: read pending.commit: %w", err)
	}

	text := strings.TrimSpace(string(body))
	if text == "" {
		// An empty marker names no rollback point. The core's own reader
		// treats an unparsable marker as corruption, so this host refuses it
		// rather than guessing which end of the log it meant.
		return 0, false, fmt.Errorf("ndjson: pending.commit is empty, so the "+
			"rollback point it is supposed to name is unknown: %s", logPath)
	}

	// Legacy form first: a bare integer, which is what the core's
	// readPendingMarker parses before trying JSON.
	if n, perr := strconv.ParseInt(text, 10, 64); perr == nil {
		if n < 0 {
			return 0, false, fmt.Errorf("ndjson: pending.commit names a "+
				"negative rollback point %d", n)
		}
		return n, true, nil
	}

	var marker struct {
		PreAppendSize *int64 `json:"pre_append_size"`
	}
	if jerr := json.Unmarshal([]byte(text), &marker); jerr != nil {
		return 0, false, fmt.Errorf("ndjson: pending.commit is neither a bare "+
			"offset nor JSON with pre_append_size: %w", jerr)
	}
	if marker.PreAppendSize == nil {
		return 0, false, fmt.Errorf("ndjson: pending.commit carries no " +
			"pre_append_size, so its rollback point is unknown")
	}
	if *marker.PreAppendSize < 0 {
		return 0, false, fmt.Errorf("ndjson: pending.commit names a negative "+
			"rollback point %d", *marker.PreAppendSize)
	}
	return *marker.PreAppendSize, true, nil
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

// decodeEvent parses one NDJSON event line.
//
// It is one function because it used to be two, copy-pasted between LogFollow
// and decodeLog, and a decoder duplicated is a decoder that will disagree with
// itself: the moment one learns a field the other does not, the live stream
// and the replay produce different states from the same bytes -- and replay
// determinism is the property every golden test in this repo rests on.
//
// The sequence number has two spellings and BOTH are real, which is the defect
// this function exists to fix:
//
//   - `seq`      -- internal/kernel.Event, the on-disk log record the core's
//     log writer appends. This is what `run attach` and every
//     replay fixture read.
//   - `sequence` -- host/v1.Event, the event embedded in a `run.attach`
//     notification on the socket (cmd/arxi/serve_stream.go).
//
// The previous decoder read only `seq`, so it was correct about the file and
// wrong about the wire. Being wrong did not produce an error: json.Unmarshal
// left the field at zero, so every event arriving over a subscription folded
// at Seq 0 and the log appeared to be one unordered batch. Timestamps differ
// the same way (`ts` vs `time`) and are accepted from either spelling for the
// same reason.
//
// A record carrying NEITHER spelling is refused rather than folded at zero.
// Accepting both must not degrade into accepting anything: an event with no
// sequence has an unknown position in the log, and defaulting it to zero
// orders it ahead of every real event. That is a wrong frame, and this repo
// holds a wrong frame to be worse than a refusal.
func decodeEvent(line []byte) (fold.Event, error) {
	// `actor` is read here and not from the payload. Both event shapes spell
	// it at the top level (kernel.Event.Actor, host/v1.Event.Actor), and it is
	// the field arxi's own reducer treats as the member's identity. It was
	// absent from this struct, so every event reached the fold with no actor
	// and the only name available was payload.agent -- which the real provider
	// path does not write on tool events. See fold.Event.Actor.
	var raw struct {
		Seq      *int64         `json:"seq"`
		Sequence *int64         `json:"sequence"`
		Type     string         `json:"type"`
		Ts       string         `json:"ts"`
		Time     string         `json:"time"`
		Actor    string         `json:"actor"`
		Payload  map[string]any `json:"payload"`
	}
	if err := json.Unmarshal(line, &raw); err != nil {
		return fold.Event{}, fmt.Errorf("not an event: %w", err)
	}

	seq := raw.Seq
	if seq == nil {
		seq = raw.Sequence
	}
	if seq == nil {
		return fold.Event{}, fmt.Errorf("event of type %q carries neither `seq` "+
			"(the on-disk kernel.Event spelling) nor `sequence` (the host/v1.Event "+
			"spelling used on the wire), so its position in the log is unknown; "+
			"folding it at zero would order it before every real event",
			raw.Type)
	}

	return fold.Event{
		Type:    raw.Type,
		Seq:     *seq,
		Actor:   raw.Actor,
		Payload: raw.Payload,
	}, nil
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

		ev, err := decodeEvent(trimmed)
		if err != nil {
			return nil, fmt.Errorf("decode log %s line %d: %w", path, lineNo, err)
		}

		// Whole value, not a field-by-field copy -- see replayFile.
		events = append(events, ev)
	}
	return events, nil
}
