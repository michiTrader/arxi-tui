package driver

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// LogFollow's doc comment claimed this:
//
//	"It reads confirmed lines only: a batch that has not been committed by the
//	 logstore is held until it appears in a subsequent read, so a torn write
//	 never produces a half-event."
//
// The first half is false and the second half is true, which is the worst way
// to be wrong: the sentence describes the guarantee the core actually offers,
// and the code implements only the easy part of it.
//
// What LogFollow does is hold back a trailing line with no newline. That does
// stop a TORN line. It is not what "confirmed" means to the logstore.
//
// arxi's commit protocol (internal/logstore/store.go:219) is:
//
//  1. write pending.commit containing the log's current committed size, fsync;
//  2. append the whole batch to events.ndjson, fsync;
//  3. remove pending.commit -- "this is the commit point".
//
// Between 2 and 3 the log contains a batch of COMPLETE, newline-terminated
// records that are not committed. If the writer dies there, Open calls
// rollbackPending(), which truncates the log back to the marker's
// PreAppendSize -- those complete lines are deleted.
//
// So a follower that trusts newline-termination reads events that the core
// will revoke. The host cannot un-fold them: the fold is append-only and the
// frame has already been drawn. That is a wrong frame from correctly-read
// bytes, which this repo holds to be worse than an error.
//
// ReadConfirmed exists precisely to answer this, and the core is explicit
// that the boundary is NOT monotonic (logstore/pending_race_test.go): "a
// marker naming a rollback point behind what a caller already consumed pulls
// the reported boundary back ... a caller cannot treat NextOffset as a
// high-water mark."

// writeLogWithPendingBatch builds a run directory in exactly the state step 2
// of the commit protocol leaves behind: committed records, then an uncommitted
// batch of complete records, plus the pending.commit marker naming the
// rollback point.
func writeLogWithPendingBatch(t *testing.T, committed, pending string) string {
	t.Helper()
	dir := t.TempDir()

	// events.ndjson: the committed prefix followed by the in-flight batch.
	// Every line here is complete and newline-terminated, which is what makes
	// the defect invisible to a newline-only check.
	if err := os.WriteFile(filepath.Join(dir, "events.ndjson"),
		[]byte(committed+pending), 0o644); err != nil {
		t.Fatal(err)
	}

	// pending.commit naming the pre-append size. The legacy spelling (a bare
	// integer) is one the core still accepts: readPendingMarker parses a bare
	// int64 and returns it with legacy:true, so a test using it exercises a
	// real supported form rather than an invented one.
	if err := os.WriteFile(filepath.Join(dir, "pending.commit"),
		[]byte(itoa(len(committed))+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}

// TestLogFollowDoesNotDeliverAnUncommittedBatch is the measurement.
//
// Two committed events, then two uncommitted ones with the marker present.
// The follower must deliver exactly the two committed events. Delivering four
// means it read past the commit point and the host has folded events the core
// is about to truncate away.
func TestLogFollowDoesNotDeliverAnUncommittedBatch(t *testing.T) {
	committed := `{"seq":1,"type":"run.started","payload":{"simulated":true}}` + "\n" +
		`{"seq":2,"type":"agent.activated","actor":"backend","payload":{}}` + "\n"
	// A COMPLETE, newline-terminated batch that is nonetheless uncommitted.
	// This is the shape a newline-only check cannot distinguish from
	// committed data.
	pending := `{"seq":3,"type":"tool.call","actor":"backend","payload":{"tool":"read"}}` + "\n" +
		`{"seq":4,"type":"tool.call_completed","actor":"backend","payload":{"tool":"read"}}` + "\n"

	dir := writeLogWithPendingBatch(t, committed, pending)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	events, err := LogFollow(ctx, filepath.Join(dir, "events.ndjson"))
	if err != nil {
		t.Fatalf("log-follow refused the run directory: %v", err)
	}

	// Collect whatever arrives in a bounded window. The follower polls, so
	// "nothing more arrives" needs a deadline rather than a closed channel.
	var got []int64
	deadline := time.After(700 * time.Millisecond)
collect:
	for {
		select {
		case ev, ok := <-events:
			if !ok {
				break collect
			}
			got = append(got, ev.Seq)
		case <-deadline:
			break collect
		}
	}

	if len(got) != 2 {
		t.Fatalf("log-follow delivered %d events (seqs %v), want 2.\n"+
			"pending.commit is present and names byte %d as the rollback "+
			"point, so seq 3 and 4 are an in-flight batch: complete, "+
			"newline-terminated, and NOT committed. The core's own Open() "+
			"truncates them away (logstore rollbackPending), so a follower "+
			"that delivers them has folded events that never existed. The "+
			"fold is append-only -- the host cannot take them back, and the "+
			"frame is already drawn.",
			len(got), got, len(committed))
	}
	if got[0] != 1 || got[1] != 2 {
		t.Errorf("delivered seqs %v, want [1 2]: the committed prefix", got)
	}
}

// TestLogFollowDeliversTheBatchOnceItCommits is the guard against fixing the
// above by never delivering anything.
//
// Withholding an uncommitted batch is only correct if it is released when the
// marker disappears -- step 3 of the protocol. A follower that refused the
// tail permanently would pass the test above and show a run that stops
// mid-execution, which is the blank screen this whole phase of work exists to
// remove.
func TestLogFollowDeliversTheBatchOnceItCommits(t *testing.T) {
	committed := `{"seq":1,"type":"run.started","payload":{}}` + "\n"
	pending := `{"seq":2,"type":"tool.call","actor":"backend","payload":{"tool":"read"}}` + "\n"

	dir := writeLogWithPendingBatch(t, committed, pending)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	events, err := LogFollow(ctx, filepath.Join(dir, "events.ndjson"))
	if err != nil {
		t.Fatalf("log-follow refused the run directory: %v", err)
	}

	// The committed event arrives.
	select {
	case ev := <-events:
		if ev.Seq != 1 {
			t.Fatalf("first event seq = %d, want 1", ev.Seq)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("the committed event never arrived")
	}

	// Commit point: the marker is removed, exactly as step 3 does.
	if err := os.Remove(filepath.Join(dir, "pending.commit")); err != nil {
		t.Fatal(err)
	}

	// Now the withheld batch must be released.
	select {
	case ev := <-events:
		if ev.Seq != 2 {
			t.Errorf("after the commit point the follower delivered seq %d, "+
				"want 2", ev.Seq)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("the batch was withheld permanently. Holding an uncommitted " +
			"batch is only correct if removing pending.commit releases it -- " +
			"that removal IS the commit point (logstore store.go step 3). A " +
			"follower that never releases it shows a run that stops " +
			"mid-execution, which is the blank screen this work exists to " +
			"remove.")
	}
}

// TestLogFollowStillWorksWithNoMarkerAtAll pins the ordinary case, so the
// pending-marker handling cannot be "fixed" by requiring a marker that a
// finished run does not have.
//
// A completed run's directory has no pending.commit: the last commit removed
// it. Every event in such a log is confirmed.
func TestLogFollowStillWorksWithNoMarkerAtAll(t *testing.T) {
	dir := t.TempDir()
	body := `{"seq":1,"type":"run.started","payload":{}}` + "\n" +
		`{"seq":2,"type":"run.result","payload":{"summary":"done"}}` + "\n"
	if err := os.WriteFile(filepath.Join(dir, "events.ndjson"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	events, err := LogFollow(ctx, filepath.Join(dir, "events.ndjson"))
	if err != nil {
		t.Fatalf("log-follow refused a committed log: %v", err)
	}

	var got []int64
	deadline := time.After(700 * time.Millisecond)
collect:
	for {
		select {
		case ev, ok := <-events:
			if !ok {
				break collect
			}
			got = append(got, ev.Seq)
		case <-deadline:
			break collect
		}
	}

	if len(got) != 2 {
		t.Errorf("delivered %d events (seqs %v), want 2: with no "+
			"pending.commit the whole complete prefix is confirmed, and a "+
			"follower that withholds it has made the marker mandatory when a "+
			"finished run does not have one", len(got), got)
	}
}

// TestATornTailIsStillWithheld keeps the original guarantee, which was real.
//
// The newline check was not wrong, it was insufficient. A half-written final
// line must still be held back, marker or no marker.
func TestATornTailIsStillWithheld(t *testing.T) {
	dir := t.TempDir()
	// A complete record, then a torn one with no newline.
	body := `{"seq":1,"type":"run.started","payload":{}}` + "\n" +
		`{"seq":2,"type":"tool.call","actor":"bac`
	if err := os.WriteFile(filepath.Join(dir, "events.ndjson"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	events, err := LogFollow(ctx, filepath.Join(dir, "events.ndjson"))
	if err != nil {
		t.Fatalf("log-follow refused a log with a torn tail: %v", err)
	}

	var got []int64
	deadline := time.After(700 * time.Millisecond)
collect:
	for {
		select {
		case ev, ok := <-events:
			if !ok {
				break collect
			}
			got = append(got, ev.Seq)
		case <-deadline:
			break collect
		}
	}

	if len(got) != 1 || got[0] != 1 {
		t.Errorf("delivered seqs %v, want [1]: a final line with no newline "+
			"is a partial write and must be held until it completes. This "+
			"guarantee predates the pending-marker fix and must survive it",
			got)
	}

	// The assertion above is NOT sufficient on its own, and the sweep proved
	// it: the weld that drops the newline bound ESCAPED.
	//
	// Without the bound, the torn bytes are handed to decodeEvent, which
	// fails to parse them and returns an error. followOnce propagates it, the
	// goroutine returns, and the channel closes. One event had already been
	// delivered -- so "delivered seqs == [1]" holds either way. The count
	// cannot tell "held back the partial line" from "blew the follower up on
	// it", and the second is strictly worse: the run goes dead silent for the
	// rest of its life.
	//
	// What discriminates is completing the line and requiring it to arrive.
	// A withheld tail is released; a dead follower delivers nothing ever
	// again.
	f, err := os.OpenFile(filepath.Join(dir, "events.ndjson"),
		os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString("kend\",\"payload\":{}}\n"); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}

	select {
	case ev, ok := <-events:
		if !ok {
			t.Fatal("the event channel closed instead of delivering the " +
				"completed record. The follower died on the partial line " +
				"rather than withholding it, which means the stream is over: " +
				"no further event in the run will ever reach the fold. A test " +
				"that only counts what arrived before the tear cannot tell " +
				"these apart.")
		}
		if ev.Seq != 2 {
			t.Errorf("after the partial line was completed the follower "+
				"delivered seq %d, want 2", ev.Seq)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("the completed record never arrived. Withholding a partial " +
			"line is only correct if finishing it releases the record; a " +
			"follower that stops reading has turned a transient torn write " +
			"into a permanently blank screen.")
	}
}

// TestAMarkerInsideARecordDoesNotCutItInHalf is the guard the sweep found
// missing: the weld that applies the rollback point without clamping it to a
// record boundary ESCAPED every test in this file.
//
// It escaped because all the other fixtures place the marker exactly at a
// newline -- which is where a healthy core puts it, since PreAppendSize is the
// log's committed size. With the marker record-aligned, clamping is a no-op
// and its absence is invisible.
//
// The clamp is not defending against arxi. It defends against a marker this
// host has not measured: a different core version, a partially-written marker
// file, or a legacy offset computed before some earlier truncation. The host
// cannot verify the number it is handed, so it treats it as an upper bound and
// still refuses to emit a fragment. Cutting a record in half hands decodeEvent
// invalid JSON, which kills the follower -- a transient oddity in a file the
// host does not own becomes a dead stream.
func TestAMarkerInsideARecordDoesNotCutItInHalf(t *testing.T) {
	dir := t.TempDir()
	first := `{"seq":1,"type":"run.started","payload":{}}` + "\n"
	second := `{"seq":2,"type":"tool.call","actor":"backend","payload":{}}` + "\n"
	if err := os.WriteFile(filepath.Join(dir, "events.ndjson"),
		[]byte(first+second), 0o644); err != nil {
		t.Fatal(err)
	}

	// A rollback point 20 bytes INTO the second record, rather than at the
	// newline that ends the first.
	misaligned := len(first) + 20
	if err := os.WriteFile(filepath.Join(dir, "pending.commit"),
		[]byte(itoa(misaligned)+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	events, err := LogFollow(ctx, filepath.Join(dir, "events.ndjson"))
	if err != nil {
		t.Fatalf("log-follow refused the run directory: %v", err)
	}

	var got []int64
	var closed bool
	deadline := time.After(700 * time.Millisecond)
collect:
	for {
		select {
		case ev, ok := <-events:
			if !ok {
				closed = true
				break collect
			}
			got = append(got, ev.Seq)
		case <-deadline:
			break collect
		}
	}

	if closed {
		t.Fatalf("the follower died on a misaligned marker (delivered %v "+
			"first). The rollback point named byte %d, which is inside record "+
			"2; reading up to it hands decodeEvent a fragment, the parse "+
			"fails, and the stream ends. The host cannot verify a marker it "+
			"did not write, so it must clamp to the last record boundary "+
			"rather than trust the offset exactly", got, misaligned)
	}
	if len(got) != 1 || got[0] != 1 {
		t.Errorf("delivered seqs %v, want [1]: everything at or before the "+
			"rollback point is confirmed, and the only whole record there is "+
			"seq 1. Record 2 is both uncommitted and incomplete at that "+
			"offset", got)
	}
}

// TestARetreatingConfirmedBoundaryDoesNotReEmit is the second guard the sweep
// found missing. The weld that changes `confirmed <= *delivered` to
// `confirmed == *delivered` ESCAPED, because no fixture here ever made the
// boundary move backwards.
//
// The core states plainly that it can (logstore/pending_race_test.go):
//
//	read at offset 0, no marker  -> NextOffset 20
//	marker appears naming 10     -> NextOffset 10   (backwards)
//
//	"That was measured, not predicted ... a caller cannot treat NextOffset as
//	 a high-water mark."
//
// So the sequence below is a real one: a run with no marker is fully
// confirmed and delivered, then a new batch begins and its marker names a
// rollback point behind what the follower already sent.
//
// The only safe response is to send nothing. The fold is append-only: it
// cannot un-receive seq 2, so re-delivering it would duplicate the event
// rather than retract it -- two assistant lines, two tool calls, a cost
// counted twice.
func TestARetreatingConfirmedBoundaryDoesNotReEmit(t *testing.T) {
	dir := t.TempDir()
	committed := `{"seq":1,"type":"run.started","payload":{}}` + "\n" +
		`{"seq":2,"type":"agent.activated","actor":"backend","payload":{}}` + "\n"
	if err := os.WriteFile(filepath.Join(dir, "events.ndjson"),
		[]byte(committed), 0o644); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	events, err := LogFollow(ctx, filepath.Join(dir, "events.ndjson"))
	if err != nil {
		t.Fatalf("log-follow refused the run directory: %v", err)
	}

	// Both records arrive: no marker, so the whole complete prefix is
	// confirmed.
	for want := int64(1); want <= 2; want++ {
		select {
		case ev := <-events:
			if ev.Seq != want {
				t.Fatalf("first pass delivered seq %d, want %d", ev.Seq, want)
			}
		case <-time.After(2 * time.Second):
			t.Fatalf("seq %d never arrived on the first pass", want)
		}
	}

	// Now a batch starts, and its marker names a rollback point BEHIND what
	// was already delivered: back to just after record 1.
	rollback := len(`{"seq":1,"type":"run.started","payload":{}}`) + 1
	if err := os.WriteFile(filepath.Join(dir, "pending.commit"),
		[]byte(itoa(rollback)+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Nothing may be emitted. Not seq 2 again, not anything.
	select {
	case ev, ok := <-events:
		if !ok {
			t.Fatal("the follower closed the channel when the confirmed " +
				"boundary retreated. A retreating boundary is a normal " +
				"consequence of a batch starting, not a fatal condition.")
		}
		t.Errorf("the follower re-emitted seq %d after the confirmed boundary "+
			"moved backwards. The fold is append-only: it cannot un-receive an "+
			"event, so a re-delivery is a DUPLICATE, not a correction -- the "+
			"transcript, the tool list and the cost counters would all double "+
			"count. The boundary is not a high-water mark and the follower "+
			"must keep its own delivered offset", ev.Seq)
	case <-time.After(700 * time.Millisecond):
		// Correct: silence.
	}
}

// TestAnUnparsablePendingMarkerIsRefused is the third guard the sweep found
// missing. The weld that returns "absent" for a marker it cannot parse
// ESCAPED, because every fixture here writes a marker the host understands.
//
// "Absent" is the most permissive possible answer: it means the whole
// complete prefix is confirmed. Inferring it from a file the host failed to
// read turns confusion into permission -- precisely the events this fix
// exists to withhold. The core's own reader treats a bad marker as corruption
// (corruptPending), so refusing is agreeing with it.
//
// Refusing means the follower reports the error rather than delivering. That
// is the correct trade: a host that shows nothing and says why is recoverable,
// and a host that shows events the core is about to delete is not.
func TestAnUnparsablePendingMarkerIsRefused(t *testing.T) {
	for _, tc := range []struct {
		name, marker string
	}{
		{"garbage", "not-a-number-and-not-json\n"},
		{"empty", "\n"},
		{"json without pre_append_size", `{"version":1,"commit_id":"abc"}` + "\n"},
		{"negative offset", "-5\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			body := `{"seq":1,"type":"run.started","payload":{}}` + "\n" +
				`{"seq":2,"type":"tool.call","actor":"backend","payload":{}}` + "\n"
			if err := os.WriteFile(filepath.Join(dir, "events.ndjson"),
				[]byte(body), 0o644); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(dir, "pending.commit"),
				[]byte(tc.marker), 0o644); err != nil {
				t.Fatal(err)
			}

			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()

			events, err := LogFollow(ctx, filepath.Join(dir, "events.ndjson"))
			if err != nil {
				// Refused at open: also correct, and unambiguous.
				return
			}

			// Otherwise the stream must end without delivering, because the
			// host cannot know which part of the log is committed.
			var got []int64
			deadline := time.After(700 * time.Millisecond)
		collect:
			for {
				select {
				case ev, ok := <-events:
					if !ok {
						break collect
					}
					got = append(got, ev.Seq)
				case <-deadline:
					break collect
				}
			}

			if len(got) != 0 {
				t.Errorf("delivered seqs %v from a log whose pending.commit "+
					"is %q. Treating an unparsable marker as absent is the "+
					"most permissive reading available -- it declares the "+
					"whole prefix confirmed -- so it turns a file the host "+
					"could not read into permission to emit events the core "+
					"may truncate. The core calls this corruption "+
					"(corruptPending); the host must not be more trusting "+
					"than the writer", got, tc.marker)
			}
		})
	}
}
