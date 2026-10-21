# ADR-0055: A steer joins a running chat turn at its next tool round

- Status: accepted
- Affects: `cmd/arxi/steering.go`, `cmd/arxi/serve_stream.go` (`lineSource`), `cmd/arxi/chat_tools.go` (`runToolLoop`), `internal/surface/surface.go` (`chat.send`)
- Depends on: ADR-0005 (qualifies it; does not reverse it)

## Context

A person watching a chat turn work often sees it going the wrong way: it is
about to edit the wrong file, or the request was missing a detail. Until now the
only way to say so was to stop the turn and send a new request. That cancels
the model's current ask, and the new request carries the whole conversation
again, so everything the turn had read and decided is paid for a second time
and the model's partial thought is gone.

ADR-0005 decided the opposite for the *run* reducer: an injection to a busy
member waits for the turn to end, and the alternative "interrupt the running
turn and restart it with the new context" was discarded because it throws away
paid work. That reasoning is correct and this ADR keeps it.

The two are not the same situation. A chat turn is a loop of asks separated by
tool rounds. Between two asks nothing is in flight: the tool results are in, the
next request has not been sent. Putting the user's words there discards nothing.

## Decision

While `chat.send` runs with tools, the client may write a line

    {"type":"chat.steer","text":"..."}

The turn reads it **between a tool round and the next ask**, adds the text to the
conversation as a user message after the tool results, writes
`{"type":"chat.steered","text":"..."}` so the client knows it was taken, and asks
the model again. Several lines that arrived since the last ask are joined, oldest
first, into one message.

- **Nothing is interrupted.** The ask in flight finishes; its tokens are used.
  This is why it is not ADR-0005's discarded alternative.
- **It is not `on_busy: queue`.** A queued injection waits for the turn to end
  and opens another; a steer changes the turn that is already running. ADR-0005's
  `PendingCauses` is untouched, and `run steer --on-busy steer` stays refused.
- **The turn announces it.** A turn that reads steering writes
  `{"type":"chat.steerable"}` first. A client steers only after it arrives,
  because a core without this ADR would answer the line as an unknown request,
  and a client reading mid-turn takes any line with an id for the turn's own
  response.
- **A steer that finds no later round is not taken.** If the turn answers before
  the next boundary, the line is dropped, no `chat.steered` is written, and the
  client sends the text as an ordinary message. The client therefore keeps its
  text until `chat.steered` arrives. Handing a late steer to the next turn would
  answer a question nobody asked that turn.
- **A steer to a connection with no running turn is ignored**, not refused: the
  client wrote it before it could know the turn had ended.
- **It is wire only**, like `chat.decision`: it exists only inside a streaming
  `chat.send`, so it adds no capability to the frozen surface; the `chat.send`
  description says it.

### Why the connection needs a pump

The loop reads the connection one request at a time and is busy running the turn,
so nothing reads it while a model is thinking. Reading it only when a tool waits
for an answer (as `ask` and `callClient` do) would miss a steer sent during an ask
or a long tool. A turn that takes steering therefore starts a goroutine that does
the blocking reads and hands lines on through a bounded channel. It never stops
before the connection ends, since a blocked read cannot be taken back; it only
passes lines on, so requests still reach the loop in the order they were written.

## Discarded alternatives

- **Interrupt the ask and restart.** ADR-0005's reason, unchanged.
- **Make the turn a goroutine that the loop can message.** The connection loop
  answers requests strictly in order, which clients rely on (a response is matched
  to its request by position among those with the same type). A second concurrent
  request would end that guarantee for every other command.
- **Hold the steer for the next turn.** That is `on_busy: queue`, which already
  exists and is slower: the user wanted this turn to change.

## Consequences

- A steer takes effect at the next tool boundary, not instantly. A turn in one
  long ask with no tools has no boundary; its steer is dropped and the client
  falls back to stop-and-resend, which is today's behaviour.
- The model sees the correction after the work it had done, so it can keep what
  is still right and undo what is not.
- Every turn that has a toolbox reads ahead up to `pumpBuffer` lines. Past that
  the client's writes wait in the pipe, as they did before.

## How it is verified

`cmd/arxi/steering_test.go`, over a real connection to a fake model:

- the turn announces that it takes steering before anything can be sent;
- a steer sent while a tool runs reaches the model at its next ask, in the same
  turn (two asks, not three), and is reported as taken;
- a steer after the last round is dropped and is not given to the next turn;
- requests written beside a steer keep their order;
- a steer with nothing to steer, and a blank one, are harmless;
- several steers between two asks join in order as one message.
