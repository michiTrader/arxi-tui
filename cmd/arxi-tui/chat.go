package main

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/michiTrader/arxi_tui/internal/driver"
	"github.com/michiTrader/arxi_tui/internal/fold"
)

// chatMaxHistory caps how many earlier messages ride along with a prompt, so a long
// session cannot grow a request without bound.
const chatMaxHistory = 40

// chatMaxHistoryBytes caps the text carried along as history. Forty long answers
// (pasted files, big code blocks) can run to hundreds of thousands of tokens, which a
// message count alone does not prevent. About four bytes make a token, so this is
// roughly 25k tokens: well inside the window of any current model, with room for the
// prompt and the reply.
const chatMaxHistoryBytes = 100_000

// chatSystemPrompt is the standing instruction for a plain chat turn.
//
// It is sent with every message and the provider counts it, so it is kept to one short
// sentence: a bare "hola" should not cost dozens of tokens before the model reads it.
const chatSystemPrompt = "You are a concise assistant in a terminal."

// chatNow is the clock behind the date line; tests replace it.
var chatNow = time.Now

// chatSystem is the standing instruction plus today's date. A model has no clock of its
// own, so without the date "the latest news" or "this week" is answered from whenever its
// training stopped, and a page dated today looks like a mistake to it.
func chatSystem() string {
	return chatSystemPrompt + " Today is " + chatNow().Format("2006-01-02") + "."
}

// errChatBusy is returned when a second line is sent while an answer is pending and the
// line cannot wait for it either (the queue is full).
var errChatBusy = errors.New("still waiting for the previous answer and the queue of lines waiting for it is full; press Esc to cancel the turn, or wait for it to finish")

// chatMaxQueue bounds the lines that may wait for the answer in flight. Each one is a
// whole message the user wrote; past a handful they are no longer "the next thing" and
// are better held back than piled up.
const chatMaxQueue = 8

// What Enter does when a line is sent while an answer is pending (behaviour.json,
// enter_while_busy). Queue is the factory behaviour: the line waits and goes when the
// answer ends. Steer interrupts the turn and sends the line at once.
const (
	busyQueue = "queue"
	busySteer = "steer"
)

// chatDialer opens a connection of its own for one chat turn and returns it with the
// function that closes it. Cancelling a turn is closing that connection: the core's
// protocol has no cancel verb and answers strictly in order, so the only way to stop a
// request that is in flight, without leaving its late answer to be read by the next
// request, is to give the turn a connection it alone owns and drop it.
type chatDialer func(ctx context.Context) (chatSender, func(), error)

// chatSender is the part of the core a chat turn needs.
type chatSender interface {
	Hello() *driver.Hello
	SubmitChatSend(ctx context.Context, p driver.ChatSendParams) (*driver.ChatSendResult, error)
}

// chatSession turns each typed line into one chat.send round-trip and feeds the
// answer (or a clear error) back to the loop. Failures are never swallowed and never
// pushed into a banner: they become a chat.error event, which the fold turns into an
// ordinary line of the conversation.
type chatSession struct {
	core chatSender
	out  chan<- fold.Event

	mu      sync.Mutex
	history []driver.ChatTurn
	busy    bool
	seq     int64
	// gen counts sessions. A turn remembers the generation it started in and
	// drops everything it would report once /clear has bumped it, so an answer
	// that was in flight cannot land in the fresh conversation.
	gen int64
	// effort is the thinking level sent with each turn ("" = the model decides).
	// It is a setting, not conversation: reset() leaves it alone.
	effort string
	// workdir is the folder the model may look into with read-only tools ("" =
	// none). Like effort it is a setting: reset() leaves it alone.
	workdir string
	// edits is what the model may do to files in workdir: "deny" (look only),
	// "ask" (each change waits for the user) or "allow". It follows the agent mode.
	edits string
	// runs is the same for shell commands: "deny" (not offered), "ask" (each command
	// waits for the user) or "allow". It follows the agent mode too.
	runs string
	// web is the same for reading web pages: "deny" (not offered), "ask" (each
	// address waits for the user) or "allow". It follows the agent mode too.
	web string
	// rulesNotice is the last "using AGENTS.md" line shown, so it is not repeated.
	rulesNotice string
	// approval is the change the core is holding for the user, nil when none.
	approval *pendingApproval

	// ui lends the model the interface tools (ui_bridge.go); nil lends none. Like
	// workdir it is a setting, and it travels with the tools, so it needs a workdir.
	ui *uiBridge

	// dial gives each turn its own connection (nil = every turn shares core, which
	// is what the tests and the mock use; cancelling then only abandons the answer).
	dial chatDialer
	// turn numbers the turns; cancel is the live turn's way to stop (nil when idle).
	turn   int64
	cancel context.CancelFunc

	// The turn in flight, as far as the history is concerned: the line that started it
	// and the record of what the model did with tools so far. They are what a turn that
	// does not finish leaves behind (see chat_trace.go).
	curText  string
	curTrace []string

	// notes are what happened to the conversation outside any turn (the chat model was
	// switched) and the model has not been told yet. They ride in the history from the
	// next turn on (see noteModelChange).
	notes []string

	// queue holds the lines sent while a turn was in flight, oldest first; they go one
	// at a time as each turn ends. parent is the context the program runs under, which
	// a queued line's turn needs because the context of the turn that ended is spent.
	queue  []string
	parent context.Context

	// whileBusy says what Enter does with a line sent during a turn (busyQueue or
	// busySteer); the behaviour layer's setting unless a test replaces it.
	whileBusy func() string
}

func newChatSession(core chatSender, out chan<- fold.Event) *chatSession {
	return &chatSession{core: core, out: out, whileBusy: func() string { return currentBehaviour().EnterWhileBusy }}
}

// busyMode is what a line sent during a turn does by default.
func (c *chatSession) busyMode() string {
	if c.whileBusy != nil && c.whileBusy() == busySteer {
		return busySteer
	}
	return busyQueue
}

// chatRequires says why this core cannot chat, or nil when it can.
func chatRequires(h *driver.Hello) error {
	if h == nil {
		return errors.New("the core has not said hello yet, so chat is not available")
	}
	for _, v := range h.Implemented {
		if v == "chat.send" {
			return nil
		}
	}
	return fmt.Errorf("this arxi core is too old to chat (it lacks chat.send); %s", rebuildRemedy)
}

// setEffort chooses the thinking level for the turns that start from now on; "" means
// "send nothing". A turn already in flight keeps the level it began with.
func (c *chatSession) setEffort(level string) {
	c.mu.Lock()
	c.effort = level
	c.mu.Unlock()
}

// setWorkdir chooses the folder the model may look into; "" gives it no tools.
func (c *chatSession) setWorkdir(dir string) {
	c.mu.Lock()
	c.workdir = dir
	c.mu.Unlock()
}

// rulesSeen reports whether this notice is news — it has not been shown in this session
// since the rules last changed — and remembers it.
func (c *chatSession) rulesSeen(notice string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.rulesNotice == notice {
		return false
	}
	c.rulesNotice = notice
	return true
}

// setEdits chooses what the model may do to files from the next turn on.
func (c *chatSession) setEdits(policy string) {
	c.mu.Lock()
	c.edits = policy
	c.mu.Unlock()
}

// setRuns chooses what the model may do about running commands from the next turn on.
func (c *chatSession) setRuns(policy string) {
	c.mu.Lock()
	c.runs = policy
	c.mu.Unlock()
}

// setWeb chooses what the model may do about reading web pages from the next turn on.
func (c *chatSession) setWeb(policy string) {
	c.mu.Lock()
	c.web = policy
	c.mu.Unlock()
}

// pendingApproval is a change on its way to the user: the turn blocks on reply.
type pendingApproval struct{ reply chan bool }

// askUser puts one change to the user and blocks the turn until they decide, or the
// turn ends (cancelled, or /clear), which counts as a no.
func (c *chatSession) askUser(ctx context.Context, gen int64, a driver.Approval) bool {
	p := &pendingApproval{reply: make(chan bool, 1)}
	c.mu.Lock()
	c.approval = p
	c.mu.Unlock()
	defer func() {
		c.mu.Lock()
		if c.approval == p {
			c.approval = nil
		}
		c.mu.Unlock()
	}()
	c.post(ctx, gen, "chat.approval", map[string]any{
		"name": a.Name, "arg": a.Arg, "summary": a.Summary, "diff": a.Diff,
	})
	select {
	case allow := <-p.reply:
		return allow
	case <-ctx.Done():
		return false
	}
}

// pendingApproval reports whether a change is waiting for the user's answer.
func (c *chatSession) pendingNow() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.approval != nil
}

// decide answers the change that is waiting. It reports whether there was one.
func (c *chatSession) decide(allow bool) bool {
	c.mu.Lock()
	p, gen := c.approval, c.gen
	c.approval = nil
	c.mu.Unlock()
	if p == nil {
		return false
	}
	p.reply <- allow
	c.post(context.Background(), gen, "chat.decided", map[string]any{"allow": allow})
	return true
}

// send starts one turn, or, when one is already in flight, does with the line what the
// user's setting says (queue it or steer with it). It returns an error immediately for
// anything that can be decided up front (old core, a full queue); everything that
// happens on the network is reported in the chat as a chat.error event.
func (c *chatSession) send(ctx context.Context, text string) error {
	return c.submit(ctx, text, "")
}

// sendOtherWay is send with the opposite of the user's setting for a line sent during a
// turn (Alt+Enter): queue where Enter would steer, steer where Enter would queue. With
// nothing in flight it is a plain send.
func (c *chatSession) sendOtherWay(ctx context.Context, text string) error {
	how := busySteer
	if c.busyMode() == busySteer {
		how = busyQueue
	}
	return c.submit(ctx, text, how)
}

// turnStart is everything one turn needs, read under the lock when the turn begins.
type turnStart struct {
	ctx              context.Context
	text             string
	hist             []driver.ChatTurn
	gen, turn        int64
	effort, workdir  string
	edits, runs, web string
}

// beginLocked makes text the turn in flight. The caller holds c.mu and, once it has
// released it, calls launch.
func (c *chatSession) beginLocked(ctx context.Context, text string) turnStart {
	c.busy = true
	st := turnStart{text: text, gen: c.gen, effort: c.effort, workdir: c.workdir, edits: c.edits, runs: c.runs, web: c.web}
	if st.runs == policyDeny {
		// Denied is what a core that knows nothing about commands already does, and
		// naming it would make an older core refuse the whole request.
		st.runs = ""
	}
	if st.web == policyDeny {
		st.web = ""
	}
	c.flushNotesLocked()
	st.hist = trimHistory(c.history, chatMaxHistory, chatMaxHistoryBytes)
	c.turn++
	st.turn = c.turn
	c.parent = ctx
	turnCtx, cancel := context.WithCancel(ctx)
	c.cancel = cancel
	st.ctx = turnCtx
	c.curText, c.curTrace = text, nil
	return st
}

func (c *chatSession) launch(st turnStart) {
	go c.run(st.ctx, st.text, st.hist, st.gen, st.effort, st.workdir, st.edits, st.runs, st.web, st.turn)
}

// submit is send with the way to treat a line sent during a turn chosen by the caller
// (how = "" takes the user's setting).
func (c *chatSession) submit(ctx context.Context, text, how string) error {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil
	}
	if err := chatRequires(c.core.Hello()); err != nil {
		return err
	}
	c.mu.Lock()
	if !c.busy {
		st := c.beginLocked(ctx, text)
		c.mu.Unlock()
		c.launch(st)
		return nil
	}
	if how == "" {
		how = c.busyMode()
	}
	gen := c.gen
	if how == busySteer {
		// Steering: the turn in flight is stopped where it is and the line goes at once.
		// What the model did so far stays in the history, with the note that the turn was
		// cut short, so the new line is read as a correction of that work and not as a
		// message out of nowhere.
		cancel := c.cancel
		c.recordCutShortLocked("")
		c.cancel = nil
		st := c.beginLocked(ctx, text)
		c.mu.Unlock()
		if cancel != nil {
			cancel()
		}
		c.post(context.Background(), gen, "chat.warn", map[string]any{"text": "interrupted the turn in flight to steer it with your new line"})
		c.launch(st)
		return nil
	}
	if len(c.queue) >= chatMaxQueue {
		c.mu.Unlock()
		return errChatBusy
	}
	c.queue = append(c.queue, text)
	c.parent = ctx
	c.mu.Unlock()
	c.post(context.Background(), gen, "chat.queued", map[string]any{"text": text})
	return nil
}

// recordCutShortLocked keeps in the history a turn that will not finish: the line that
// started it, what the model did with tools before it was stopped, and the note saying so.
// failure is the error the user was shown when the turn failed ("" when it was stopped).
// The caller holds c.mu.
func (c *chatSession) recordCutShortLocked(failure string) {
	if c.curText == "" {
		return
	}
	c.history = append(c.history,
		driver.ChatTurn{Role: "user", Text: c.curText},
		driver.ChatTurn{Role: "assistant", Text: withTrace(c.curTrace, "") + endedNote(c.curTrace, failure)})
	c.curText, c.curTrace = "", nil
}

// noteModelChange tells the conversation that the chat model was switched from one model
// ("" when it was not known) to another. The note reaches the model with the next question
// and is saved with the conversation, so a resumed one knows it too.
func (c *chatSession) noteModelChange(from, to string) {
	if to == "" || from == to {
		return
	}
	c.mu.Lock()
	c.notes = append(c.notes, modelChangeNote(from, to))
	gen := c.gen
	c.mu.Unlock()
	c.post(context.Background(), gen, eventModelChanged, map[string]any{"from": from, "to": to})
}

// flushNotesLocked writes the notes that wait into the history, on the last answer: the
// model reads them as something that happened after it. Before any answer there is
// nothing for a note to explain, so it is dropped. The caller holds c.mu.
func (c *chatSession) flushNotesLocked() {
	if len(c.notes) == 0 {
		return
	}
	defer func() { c.notes = nil }()
	if len(c.history) == 0 || c.history[len(c.history)-1].Role != "assistant" {
		return
	}
	last := &c.history[len(c.history)-1]
	last.Text = strings.TrimRight(last.Text, "\n") + "\n\n" + strings.Join(c.notes, "\n")
}

// giveBackLocked forgets the lines still waiting for a turn and returns the event that
// hands them back to the user, nil when none waited. A turn that was stopped or failed
// does not send them on: they were written for a conversation that is no longer where
// they assumed, so the words go back to the input box, to be sent or changed by the
// person who wrote them. The caller holds c.mu and posts the event once it has let go.
func (c *chatSession) giveBackLocked() map[string]any {
	if len(c.queue) == 0 {
		return nil
	}
	lines := make([]any, len(c.queue))
	for i, q := range c.queue {
		lines[i] = q
	}
	c.queue = nil
	return map[string]any{"lines": lines}
}

// queuedNow is how many lines wait for the turn in flight.
func (c *chatSession) queuedNow() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.queue)
}

// cancelTurn stops the turn in flight, if any, and puts a "request failed:
// Cancelled" line in the conversation. It reports whether there was a turn to stop, so a key that means
// "cancel" can fall back to its other meaning when nothing is running. The turn is
// over at once: the next line may be sent without waiting for the abandoned
// goroutine to notice.
func (c *chatSession) cancelTurn() bool {
	c.mu.Lock()
	if !c.busy || c.cancel == nil {
		c.mu.Unlock()
		return false
	}
	cancel, gen := c.cancel, c.gen
	c.cancel = nil
	c.busy = false
	// A stopped turn still happened: the next question is asked with it in the history.
	c.recordCutShortLocked("")
	back := c.giveBackLocked()
	c.mu.Unlock()
	cancel()
	c.post(context.Background(), gen, "chat.cancelled", nil)
	if back != nil {
		c.post(context.Background(), gen, "chat.unqueued", back)
	}
	return true
}

// reset starts a new conversation: the history is forgotten, a turn still in
// flight is orphaned (its result is discarded), and the next line may be sent
// at once. The core keeps no chat state of its own (chat.send is stateless, the
// history rides along with each prompt), so forgetting it here is the whole job.
func (c *chatSession) reset() {
	c.mu.Lock()
	c.gen++
	c.history = nil
	c.queue = nil
	c.curText, c.curTrace, c.notes = "", nil, nil
	c.busy = false
	cancel := c.cancel
	c.cancel = nil
	c.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}

// setHistory replaces the conversation the next question is asked in.
func (c *chatSession) setHistory(h []driver.ChatTurn) {
	c.mu.Lock()
	c.history = append([]driver.ChatTurn(nil), h...)
	c.mu.Unlock()
}

// busyNow reports whether a turn is in flight.
func (c *chatSession) busyNow() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.busy
}

// current reports whether gen is still the live session.
func (c *chatSession) current(gen int64) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.gen == gen
}

func (c *chatSession) nextSeq() int64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.seq++
	return c.seq
}

func (c *chatSession) emit(ctx context.Context, gen int64, typ string, payload map[string]any) {
	if ctx.Err() != nil || !c.current(gen) {
		return
	}
	ev := fold.Event{Type: typ, Seq: c.nextSeq(), Actor: "assistant", Payload: payload}
	select {
	case c.out <- ev:
	case <-ctx.Done():
	}
}

// fail puts an error in the conversation. It never blocks the caller: the relay
// channel is buffered, and if it is momentarily full the event waits on its own
// goroutine rather than being dropped.
func (c *chatSession) fail(ctx context.Context, gen int64, msg string) {
	c.post(ctx, gen, "chat.error", map[string]any{"text": msg})
}

// failTurn is fail for the error that ended a turn; the mark tells a saved conversation
// that this error belongs to the question before it (see historyFromEvents).
func (c *chatSession) failTurn(ctx context.Context, gen int64, msg string) {
	c.post(ctx, gen, "chat.error", map[string]any{"text": msg, "turn_failed": true})
}

// post queues a conversation event without ever blocking its caller (see fail).
func (c *chatSession) post(ctx context.Context, gen int64, typ string, payload map[string]any) {
	// A turn that was stopped (cancelled, or steered away from) says nothing more: what
	// it would add would land among the lines of the turn that replaced it.
	if !c.current(gen) || ctx.Err() != nil {
		return
	}
	ev := fold.Event{Type: typ, Seq: c.nextSeq(), Actor: "assistant", Payload: payload}
	select {
	case c.out <- ev:
	default:
		go func() {
			select {
			case c.out <- ev:
			case <-ctx.Done():
			}
		}()
	}
}

func (c *chatSession) run(ctx context.Context, text string, hist []driver.ChatTurn, gen int64, effort, workdir, edits, runs, web string, turn int64) {
	defer func() {
		c.mu.Lock()
		// Only the turn that is still current frees the session: a cancelled turn
		// winding down late must not mark its successor idle.
		if c.gen == gen && c.turn == turn {
			c.busy = false
			c.cancel = nil
			// A turn that ended on its own hands over to the next line that was
			// waiting for it. One that was stopped does not: the user stopped it.
			if ctx.Err() == nil && len(c.queue) > 0 && c.parent != nil && c.parent.Err() == nil {
				next := c.queue[0]
				c.queue = c.queue[1:]
				st := c.beginLocked(c.parent, next)
				c.mu.Unlock()
				c.launch(st)
				return
			}
		}
		c.mu.Unlock()
	}()
	c.emit(ctx, gen, "run.prompt", map[string]any{"text": text})
	c.emit(ctx, gen, "agent.activated", map[string]any{"agent": "assistant"})
	started := time.Now()
	core := c.core
	if c.dial != nil {
		conn, closeConn, err := c.dial(ctx)
		if err != nil {
			if ctx.Err() == nil {
				msg := chatErrorText(err)
				back := c.failedTurn(gen, turn, msg)
				c.emit(ctx, gen, "agent.failed", map[string]any{"agent": "assistant"})
				c.failTurn(ctx, gen, msg)
				if back != nil {
					c.post(ctx, gen, "chat.unqueued", back)
				}
			}
			return
		}
		defer closeConn()
		core = conn
	}
	system := chatSystem()
	if workdir != "" {
		// Read on every turn, so an edit to the file counts from the next question;
		// the user is told once per version of it, not once per turn.
		rules := loadProjectRules(workdir)
		if p := rules.Prompt(); p != "" {
			system += "\n\n" + p
		}
		if n := rules.Notice(); n != "" && c.rulesSeen(n) {
			c.post(ctx, gen, "chat.warn", map[string]any{"text": n})
		}
	}
	params := driver.ChatSendParams{
		Prompt: text, System: system, History: hist, Effort: effort,
		// The model's thinking is shown live in the Thinking line.
		OnThinking: func(fragment string) {
			c.post(ctx, gen, "chat.thinking", map[string]any{"text": fragment})
		},
		// What it looks at while answering is shown as it happens.
		Workdir: workdir, Edits: edits, Runs: runs, Web: web,
		OnApproval: func(ctx context.Context, a driver.Approval) bool { return c.askUser(ctx, gen, a) },
		OnTool: func(t driver.ToolCall) {
			// What was done is kept for the history, whether or not the turn finishes.
			c.mu.Lock()
			if c.gen == gen && c.turn == turn && ctx.Err() == nil {
				c.curTrace = append(c.curTrace, traceLine(t.Name, t.Arg, t.Summary, t.OK))
			}
			c.mu.Unlock()
			c.post(ctx, gen, "chat.tool", map[string]any{
				"name": t.Name, "arg": t.Arg, "ok": t.OK, "summary": t.Summary, "output": t.Output, "diff": t.Diff,
			})
		},
	}
	if c.ui != nil && workdir != "" {
		ui := c.ui
		// The tool descriptions alone did not do it. Measured on a real model: asked
		// to recolour "the tui", it searched the project eleven times, because the
		// core's standing hint says the tools are for "the user's project" and
		// nothing said the app it runs in is something else. One sentence names it.
		params.System += " " + uiSystemHint
		params.ClientTools = ui.definitions()
		params.OnClientTool = func(ctx context.Context, call driver.ClientToolCall) driver.ClientToolResult {
			return ui.call(ctx, call, func(a driver.Approval) bool { return c.askUser(ctx, gen, a) })
		}
	}
	res, err := core.SubmitChatSend(ctx, params)
	// An older core refuses what it does not know, one parameter at a time. Take away
	// only that, say so, and ask again: the user still gets everything the core can do.
	for i := 0; i < 4 && ctx.Err() == nil; i++ {
		switch {
		case errors.Is(err, driver.ErrClientToolsUnsupported):
			// The interface tools are this client's own; a core that cannot carry
			// them still answers, it just cannot change the interface.
			c.post(ctx, gen, "chat.warn", map[string]any{"text": "this arxi core cannot let the model change the interface, " +
				"so it will not; " + rebuildRemedy})
			params.ClientTools, params.OnClientTool = nil, nil
			// The hint names ui_guide; without the tool it would send the model
			// looking for one it does not have.
			params.System = strings.TrimSuffix(params.System, " "+uiSystemHint)
		case errors.Is(err, driver.ErrEditsUnsupported):
			// A core that can look but not change files: say so, and go on looking.
			c.post(ctx, gen, "chat.warn", map[string]any{"text": "this arxi core cannot let the model change files, " +
				"so it may only look; " + rebuildRemedy})
			params.Edits = ""
		case errors.Is(err, driver.ErrRunsUnsupported):
			c.post(ctx, gen, "chat.warn", map[string]any{"text": "this arxi core cannot let the model run commands, " +
				"so it will not; " + rebuildRemedy})
			params.Runs = ""
		case errors.Is(err, driver.ErrWebUnsupported):
			c.post(ctx, gen, "chat.warn", map[string]any{"text": "this arxi core cannot let the model read web pages, " +
				"so it will not; " + rebuildRemedy})
			params.Web = ""
		default:
			i = 4
			continue
		}
		// ui_edit asks through the same channel, but on the client's side; it needs
		// no approval from the core, so OnApproval stays for it.
		if params.Edits == "" && params.Runs == "" && params.Web == "" && params.OnClientTool == nil {
			params.OnApproval = nil
		}
		res, err = core.SubmitChatSend(ctx, params)
	}
	if errors.Is(err, driver.ErrToolsUnsupported) && ctx.Err() == nil {
		// An older core cannot give the model tools. Say so, and answer the
		// plain way: the user asked a question and still gets an answer.
		c.post(ctx, gen, "chat.warn", map[string]any{"text": "this arxi core cannot give the model access to your files, " +
			"so it answers without looking; " + rebuildRemedy})
		params.Workdir, params.OnTool, params.ClientTools, params.OnClientTool = "", nil, nil, nil
		params.System = strings.TrimSuffix(params.System, " "+uiSystemHint)
		res, err = core.SubmitChatSend(ctx, params)
	}
	if ctx.Err() != nil {
		// Cancelled (cancelTurn already told the conversation) or the program is
		// closing: whatever came back is not wanted.
		return
	}
	if err != nil {
		msg := chatErrorText(err)
		back := c.failedTurn(gen, turn, msg)
		c.emit(ctx, gen, "agent.failed", map[string]any{"agent": "assistant"})
		c.failTurn(ctx, gen, msg)
		if back != nil {
			c.post(ctx, gen, "chat.unqueued", back)
		}
		return
	}
	c.mu.Lock()
	if c.gen != gen || c.turn != turn {
		c.mu.Unlock()
		return
	}
	c.history = append(c.history, driver.ChatTurn{Role: "user", Text: text}, driver.ChatTurn{Role: "assistant", Text: withTrace(c.curTrace, res.Text)})
	c.curText, c.curTrace = "", nil
	c.mu.Unlock()
	model := res.Model
	if res.Provider != "" && !strings.Contains(model, "/") {
		model = res.Provider + "/" + res.Model
	}
	c.emit(ctx, gen, "llm.response", map[string]any{
		"text": res.Text, "model": model, "agent": "assistant",
		"tokens_in": float64(res.InputTokens), "tokens_out": float64(res.OutputTokens),
		"context_tokens": float64(res.ContextTokens),
		"duration_ms":    float64(time.Since(started).Milliseconds()),
	})
	c.emit(ctx, gen, "agent.turn_done", map[string]any{"agent": "assistant"})
}

// failedTurn keeps a turn that failed in the history (the question, what was done for it
// and the error the user saw are not lost to a failure) and returns the event that gives back the lines that were
// waiting for it, nil when none were.
func (c *chatSession) failedTurn(gen, turn int64, msg string) map[string]any {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.gen != gen || c.turn != turn {
		return nil
	}
	c.recordCutShortLocked(msg)
	return c.giveBackLocked()
}

// eventModelChanged is the event that records a switch of the chat model in the
// conversation. The fold ignores it (the user was told when they switched); a saved
// conversation keeps it so a resumed one tells the model.
const eventModelChanged = "chat.model"

// chatErrorText is the sentence the user reads when a turn fails: the core's own
// words when it refused, a plain description otherwise.
func chatErrorText(err error) string {
	var r *driver.Refusal
	if errors.As(err, &r) && r.Message != "" {
		return r.Message
	}
	return "could not get an answer: " + err.Error()
}
