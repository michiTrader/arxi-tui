package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"sync"

	hostv1 "github.com/michiTrader/arxi/host/v1"
)

// Protocol event streaming (ADR-0016). The connection carries two message
// kinds: responses answer requests and keep their strict order; notifications
// are server-initiated, carry `type` and no `id` — the shape hello already
// established — and interleave with responses. Everything in this file exists
// to emit those notifications without weakening the response contract.

// eventNotification is one confirmed batch for one subscription, plus the
// terminal marker when the run has ended and the batch delivered everything
// through the sequence at which it ended.
type eventNotification struct {
	Type         string         `json:"type"`
	Subscription string         `json:"subscription"`
	AfterSeq     int64          `json:"after_seq"`
	Events       []hostv1.Event `json:"events"`
	Terminal     bool           `json:"terminal,omitempty"`
	Job          *hostv1.Job    `json:"job,omitempty"`
	Error        *protoError    `json:"error,omitempty"`
}

// attachAck is the response to run.attach. TerminalMarkers says whether
// terminal notifications will be emitted: they require the inspect capability
// on the same job, and a stream that ends silently when the client expected a
// marker would be a client blocked forever on a promise the wire never made.
type attachAck struct {
	Subscription    string `json:"subscription"`
	AfterSeq        int64  `json:"after_seq"`
	TerminalMarkers bool   `json:"terminal_markers"`
}

// connWriter serializes every write on the connection. The loop writes
// responses and the pumps write notifications, all under one mutex; there is
// no other arbitration because there is no other ordering requirement —
// responses are ordered by the loop, a subscription's notifications by its
// single pump, and any interleaving between the two is valid.
type connWriter struct {
	mu  sync.Mutex
	enc *json.Encoder
}

func (w *connWriter) write(v any) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.enc.Encode(v)
}

// subscriptionPump delivers one subscription's batches as notifications.
type subscriptionPump struct {
	id        string // the attach request's id, doubling as the subscription identity
	jobID     hostv1.JobID
	after     int64
	sub       hostv1.Subscription
	host      lifecycleHost
	principal hostv1.Principal
	markers   bool
	// release is closed by the connection loop once the ack response is on
	// the wire. Waiting on it is what guarantees the ack precedes this
	// subscription's first event: the pump exists before the response is
	// written, and without the latch it could win the race.
	release chan struct{}
}

// run delivers batches until the stream ends: terminal marker, host error,
// connection cancellation or Close. It exits silently on cancellation and
// Close — the connection is ending, and a notification nobody can read is
// noise — and emits an error notification for any other failure, because a
// stream that stops delivering without saying why looks identical to a
// quiescent run.
func (p *subscriptionPump) run(ctx context.Context, w *connWriter) {
	defer p.sub.Close()
	<-p.release
	for {
		batch, err := p.sub.Next(ctx)
		if err != nil {
			if ctx.Err() == nil {
				_ = w.write(eventNotification{Type: "events", Subscription: p.id,
					AfterSeq: p.after, Error: &protoError{Code: errFailed, Message: err.Error()}})
			}
			return
		}
		p.after = batch.AfterSeq
		notification := eventNotification{Type: "events", Subscription: p.id,
			AfterSeq: batch.AfterSeq, Events: batch.Events}
		if p.markers {
			// The projection is the same fold the CLI and Wait use. The
			// sequence check prevents announcing terminal before the batch
			// that reached it has been delivered.
			if job, ierr := p.host.Inspect(ctx, hostv1.InspectRequest{Principal: p.principal, JobID: p.jobID}); ierr == nil &&
				job.Terminal && batch.AfterSeq >= job.Sequence {
				notification.Terminal = true
				notification.Job = &job
				_ = w.write(notification)
				return
			}
		}
		if err := w.write(notification); err != nil {
			return
		}
	}
}

// connStreams owns the subscriptions opened on one connection: their release
// latches and their teardown. It hangs off the session by pointer so the
// value copies the loop makes all register into the same lifetime.
type connStreams struct {
	ctx context.Context
	w   *connWriter
	wg  sync.WaitGroup
	// src is the connection's reader. It is here, and not only in the loop, so a
	// request that has to wait for the user's decision can read it (see ask).
	src *lineSource

	mu      sync.Mutex
	pending []*subscriptionPump // registered, waiting for their ack to be written
	live    []*subscriptionPump // released, delivering
}

func newConnStreams(ctx context.Context, w *connWriter) *connStreams {
	return &connStreams{ctx: ctx, w: w}
}

// register records a pump before its ack is written. The pump does not start
// here: releasePending starts it, and only the connection loop calls that,
// immediately after writing the response — which is the whole ordering
// guarantee.
func (c *connStreams) register(p *subscriptionPump) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.pending = append(c.pending, p)
}

// releasePending starts every pump registered by the response just written.
func (c *connStreams) releasePending() {
	c.mu.Lock()
	pending := c.pending
	c.pending = nil
	c.live = append(c.live, pending...)
	c.mu.Unlock()
	for _, p := range pending {
		// Closing the latch is what releases the pump: it has been waiting
		// since registration, and only the loop reaches this point, after the
		// ack is written. Without the close the pump would block forever.
		close(p.release)
		c.wg.Add(1)
		go func(p *subscriptionPump) {
			defer c.wg.Done()
			p.run(c.ctx, c.w)
		}(p)
	}
}

// closeAll ends every subscription on connection teardown. Close is the pull
// stream's own cancellation contract: it makes a blocked Next return, so the
// pumps exit and Wait returns without deadlock. A pump mid-write finishes its
// write first — the mutex serializes it — and a notification written to a
// closing socket is harmless.
func (c *connStreams) closeAll() {
	c.mu.Lock()
	pumps := append(append([]*subscriptionPump{}, c.pending...), c.live...)
	c.pending, c.live = nil, nil
	c.mu.Unlock()
	for _, p := range pumps {
		_ = p.sub.Close()
	}
	c.wg.Wait()
}

// lineSource is the connection's reader: the loop takes its requests from it, and a
// turn waiting for the user's decision takes the decision from it. Both run on the
// loop's own goroutine, one at a time, so it needs no lock.
type lineSource struct {
	sc       *bufio.Scanner
	deferred []sourceLine // requests that arrived while a decision was awaited
	cur      sourceLine
}

// sourceLine is one line read, and how long the reader saw it to be (the loop
// refuses a line over maxLineBytes).
type sourceLine struct {
	text string
	size int
}

func newLineSource(sc *bufio.Scanner) *lineSource { return &lineSource{sc: sc} }

// scan advances to the next request: first any that arrived while a decision was
// awaited, in the order they came, then whatever the connection sends.
func (s *lineSource) scan() bool {
	if len(s.deferred) > 0 {
		s.cur, s.deferred = s.deferred[0], s.deferred[1:]
		return true
	}
	if !s.sc.Scan() {
		return false
	}
	s.cur = sourceLine{text: s.sc.Text(), size: len(s.sc.Bytes())}
	return true
}

// chatApprovalNotification asks the client whether the model may make a change. It
// has a type and no id, like every notification; the client answers with a line
// {"type":"chat.decision","call_id":...,"allow":true|false}.
type chatApprovalNotification struct {
	Type    string `json:"type"`
	CallID  string `json:"call_id"`
	Name    string `json:"name"`
	Arg     string `json:"arg"`
	Summary string `json:"summary"`
	Diff    string `json:"diff,omitempty"`
}

// ask puts a question to the user through the client and waits for the answer.
//
// The loop answers requests strictly in order and is busy with the one that is
// asking, so nothing else would read the connection meanwhile: this reads it. A line
// that is the decision for this call ends the wait. Any other line is a request that
// came early; it is kept and handled, in order, once the current one is answered. A
// decision for some other call is stale and dropped, and one that does not say
// allow or deny counts as a refusal: when in doubt a change is not made.
//
// If the connection ends first, nothing was allowed and the error says so. A client
// that cancels a turn closes its connection, which is what unblocks this; there is
// no context to pass because a read on the connection cannot be interrupted any
// other way.
func (c *connStreams) ask(n chatApprovalNotification) (bool, error) {
	if c == nil || c.src == nil {
		return false, fmt.Errorf("this connection cannot ask the user")
	}
	n.Type = "chat.approval"
	if err := c.w.write(n); err != nil {
		return false, fmt.Errorf("ask the user: %w", err)
	}
	src := c.src
	for src.sc.Scan() {
		line := sourceLine{text: src.sc.Text(), size: len(src.sc.Bytes())}
		var d struct {
			Type   string `json:"type"`
			CallID string `json:"call_id"`
			Allow  *bool  `json:"allow"`
		}
		if json.Unmarshal([]byte(line.text), &d) == nil && d.Type == "chat.decision" {
			if d.CallID != n.CallID {
				continue
			}
			return d.Allow != nil && *d.Allow, nil
		}
		if line.text != "" {
			src.deferred = append(src.deferred, line)
		}
	}
	err := src.sc.Err()
	if err == nil {
		err = io.EOF
	}
	return false, fmt.Errorf("the connection ended before the user decided (nothing was changed): %w", err)
}
