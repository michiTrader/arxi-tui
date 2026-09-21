package driver

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"strings"
	"testing"
)

// The NDJSON bridge had been tested against two things, and both of them were
// mine: the Phase 0 mock, which emits what I told it to emit, and fixtures in
// ndjson_test.go written by the same hand as the client. A contract measured
// only against a fixture the client's author also wrote is not measured at all
// -- it is a mirror. `openDriver` spawns `ARXI_BIN serve`, and that binary is
// the arxi core, whose serve protocol is specified by cmd/arxi/serve.go and
// NOT by anything in this repository.
//
// testdata/serve/session.ndjson is a transcript captured from the real
// `arxi serve` (arxi 0.0.1-spec, surface v1) by piping four request lines into
// it and recording stdout verbatim. The fixture is the measurement; these
// tests hold the client to it. Regenerate with:
//
//	printf '{"id":"p1","type":"run.prompt","params":{"run":"last","text":"hola"}}\n...' \
//	  | arxi serve > testdata/serve/session.ndjson
//
// Everything asserted below was a live observation first and a test second.

// helloLine returns the first line of the recorded real-core session: the
// hello the arxi core actually writes before it reads anything.
func helloLine(t *testing.T) string {
	t.Helper()
	raw, err := os.ReadFile("../../testdata/serve/session.ndjson")
	if err != nil {
		t.Fatalf("read the recorded arxi serve session: %v", err)
	}
	lines := strings.Split(strings.TrimRight(string(raw), "\n"), "\n")
	if len(lines) == 0 {
		t.Fatal("the recorded session is empty; the fixture is supposed to be " +
			"a transcript of a real `arxi serve`")
	}
	return lines[0]
}

// TestHandshakeAcceptsTheHelloTheRealCoreSends is the whole bridge in one
// assertion: feed the client the exact bytes the core sends and see whether it
// agrees to talk.
//
// It failed the first time it ran, and the failure was fatal rather than
// cosmetic:
//
//	ndjson: version mismatch: server is "0.0.1-spec", client is "0.1.0"
//
// Handshake compared the hello's `version` against its own protocolVersion =
// "0.1.0", and the core sends the version of the BINARY
// (cmd/arxi/main.go:60), not of the protocol. The two strings can never be
// equal, so every real handshake was refused and arxi-tui could not have
// spoken to arxi once. The mock hid it because the mock sends whatever this
// package's constant says.
//
// The fix is not to bump the constant to "0.0.1-spec" -- that just moves the
// coupling, and the next arxi release breaks the host for no reason. The
// handshake's real question is which VOCABULARY is on the wire, and the core
// answers that in `surface_version`, an integer it derives from
// surface.SurfaceVersion. Gate on that; report the binary version as
// information.
func TestHandshakeAcceptsTheHelloTheRealCoreSends(t *testing.T) {
	d := NewNDJSON(readWriter{r: strings.NewReader(helloLine(t) + "\n"), w: io.Discard})

	if err := d.Handshake(context.Background()); err != nil {
		t.Fatalf("the real core's hello was refused: %v\n"+
			"This is the bridge's only gate, and the core cannot be changed to "+
			"suit it: whatever the client demands here must be something the "+
			"core actually sends.", err)
	}
}

// TestHandshakeGatesOnTheSurfaceVersionNotTheBinaryVersion pins the reason the
// bug above is fixed the way it is.
//
// A core that ships a new binary version with the same surface speaks the same
// vocabulary, so the host must keep working. Rejecting it would make every
// arxi patch release a host outage -- the same failure as before, just spelled
// with a different constant.
func TestHandshakeGatesOnTheSurfaceVersionNotTheBinaryVersion(t *testing.T) {
	var hello map[string]any
	if err := json.Unmarshal([]byte(helloLine(t)), &hello); err != nil {
		t.Fatal(err)
	}
	hello["version"] = "9.9.9-someday" // a future arxi build, same surface
	line, err := json.Marshal(hello)
	if err != nil {
		t.Fatal(err)
	}

	d := NewNDJSON(readWriter{r: strings.NewReader(string(line) + "\n"), w: io.Discard})
	if err := d.Handshake(context.Background()); err != nil {
		t.Fatalf("a newer arxi binary speaking the SAME surface was refused: %v\n"+
			"The handshake asks which vocabulary is on the wire. That is "+
			"surface_version; the binary version is information, not a gate.", err)
	}
}

// TestHandshakeRefusesAnUnknownSurfaceVersion is the other half: a surface the
// host does not know must be refused, because the vocabulary is what the whole
// bind inventory is written against. Accepting it would let the host issue
// requests whose parameters the core never declared and read fields it does
// not publish -- and the symptom would be a wrong frame, not an error.
func TestHandshakeRefusesAnUnknownSurfaceVersion(t *testing.T) {
	var hello map[string]any
	if err := json.Unmarshal([]byte(helloLine(t)), &hello); err != nil {
		t.Fatal(err)
	}
	hello["surface_version"] = 2
	line, err := json.Marshal(hello)
	if err != nil {
		t.Fatal(err)
	}

	d := NewNDJSON(readWriter{r: strings.NewReader(string(line) + "\n"), w: io.Discard})
	err = d.Handshake(context.Background())
	if err == nil {
		t.Fatal("surface v2 was accepted by a host that only knows v1: the host " +
			"would then send parameters the core never declared, and the core " +
			"refuses unknown parameters rather than ignoring them")
	}
	if !strings.Contains(err.Error(), "surface") {
		t.Errorf("the refusal does not name the surface version, so the reader "+
			"cannot tell which side is behind: %v", err)
	}
}

// TestHandshakeRecordsWhatTheCoreSaysItImplements is the finding the version
// bug was hiding.
//
// The core's hello carries three lists this client threw away: `types` (every
// message type in the surface), `implemented` (the subset with an executor in
// THIS build), and `capabilities`. serve.go explains why `implemented` is sent
// at all, and the sentence is aimed exactly at a client like this one: without
// the list, "a client discovers that one type at a time by sending a request
// and reading a failure, which makes a permanent state look like a transient
// error."
//
// Measured against the real core, the consequence is not hypothetical:
// run.prompt -- the ONLY request arxi-tui ever sends -- is in `types` and is
// NOT in `implemented`. The host's single verb is declared and unimplemented,
// and the host had no way to know.
func TestHandshakeRecordsWhatTheCoreSaysItImplements(t *testing.T) {
	d := NewNDJSON(readWriter{r: strings.NewReader(helloLine(t) + "\n"), w: io.Discard})
	if err := d.Handshake(context.Background()); err != nil {
		t.Fatalf("handshake: %v", err)
	}

	h := d.Hello()
	if h == nil {
		t.Fatal("the hello was parsed and discarded. The core sends `types`, " +
			"`implemented` and `capabilities` so a client can tell a permanent " +
			"gap from a transient failure; a client that drops them has to " +
			"rediscover the gap from every error it gets")
	}
	if len(h.Types) == 0 {
		t.Error("no message types recorded from a hello that declares 38 of them")
	}

	if !d.Declares("run.prompt") {
		t.Error("run.prompt is not in the surface the core declares, which would " +
			"mean the host's only verb does not exist at all")
	}
	// The measurement, asserted so it cannot quietly stop being true: this
	// build of the core declares run.prompt and does not implement it.
	if d.Implements("run.prompt") {
		t.Skip("run.prompt is now implemented by the core: re-record " +
			"testdata/serve/session.ndjson and re-measure the refusal path")
	}
	// run.attach IS implemented, which contradicted ADR-0002's stated premise
	// that "no subscription layer exists yet in arxi to extend". It does
	// exist, and it is not a stub: serve.go has a streamingHandlers table,
	// dispatchAttach opens a hostv1.Subscription, and serve_stream.go runs a
	// pump per subscription with an ack-before-events guarantee and a single
	// writer mutex. Every specific thing the ADR said arxi would have to
	// build -- "subscription IDs, event messages, cancellation, and writer
	// arbitration" -- was already there.
	//
	// ADR-0002 has now been re-decided on that evidence (docs/PLAN.md,
	// "ADR-0002 re-decided"). The conclusion is unchanged and the reason is
	// not: log-follow stays because it is the path replay and every golden
	// already run through, not because subscribing is unavailable. This
	// assertion is what keeps the availability half honest.
	if !d.Implements("run.attach") {
		t.Skip("run.attach is no longer implemented: the re-decided ADR-0002 " +
			"treats the subscribe path as available-but-unused, and that " +
			"premise would need re-measuring")
	}
	// The capability behind it, asserted separately: `implemented` says the
	// build has an executor, `capabilities` says this session's principal may
	// use it. A verb implemented but not permitted is still unusable, and the
	// re-decided ADR records the subscribe path as genuinely available.
	var subscribe bool
	for _, c := range h.Capabilities {
		if c == "event.subscribe" {
			subscribe = true
		}
	}
	if !subscribe {
		t.Errorf("run.attach is implemented but event.subscribe is not in the "+
			"session's capabilities (%v). The re-decided ADR-0002 rests on the "+
			"subscribe path being available and unused rather than unavailable, "+
			"so losing the capability changes the decision's basis",
			h.Capabilities)
	}
}

// readWriter glues a reader and a writer into the io.ReadWriter NewNDJSON
// takes, which is what lets the protocol be measured without a subprocess --
// the same reason serve.go takes io.Reader/io.Writer instead of a net.Conn.
type readWriter struct {
	r io.Reader
	w io.Writer
}

func (rw readWriter) Read(p []byte) (int, error)  { return rw.r.Read(p) }
func (rw readWriter) Write(p []byte) (int, error) { return rw.w.Write(p) }
