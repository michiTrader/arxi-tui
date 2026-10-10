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

// errChatBusy is returned when a second line is sent while an answer is pending.
var errChatBusy = errors.New("still waiting for the previous answer; press Esc to cancel it, or wait for it to finish")

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
	// modelNote is what the model is told, once, on its next turn: the user switched
	// the model or provider, so earlier replies in the history were not its own.
	modelNote string
	// lastModel is the model the previous switch ended on (or the first one seen).
	lastModel string
	// rulesNotice is the last "using AGENTS.md" line shown, so it is not repeated.
	rulesNotice string
	// approval is the change the core is holding for the user, nil when none.
	approval *pendingApproval

	// uiMode is the interface conversation: once the user has said /ui, the guide and the
	// tools ride with every message after it, so they can go on talking about the
	// interface without repeating the switch. It belongs to the conversation, so
	// reset() ends it; /ui off ends it too.
	uiMode bool

	// ui lends the model the interface tools (ui_bridge.go); nil lends none. Like
	// workdir it is a setting, and it travels with the tools, so it needs a workdir.
	ui *uiBridge

	// dial gives each turn its own connection (nil = every turn shares core, which
	// is what the tests and the mock use; cancelling then only abandons the answer).
	dial chatDialer
	// turn numbers the turns; cancel is the live turn's way to stop (nil when idle).
	turn   int64
	cancel context.CancelFunc
	// retry is the wait the core announced before asking the provider again, so the
	// Thinking line can count it down. Zero value = not waiting.
	retry retryWait
}

// retryWait is one announced wait: until when, and the words that explain it.
type retryWait struct {
	until time.Time
	why   string
	n, of int
}

func newChatSession(core chatSender, out chan<- fold.Event) *chatSession {
	return &chatSession{core: core, out: out}
}

// noteRetry records that the core is waiting to ask the provider again, puts a line in
// the conversation (so the wait is on the record, not only on a spinner), and starts
// the countdown the Thinking line shows.
func (c *chatSession) noteRetry(ctx context.Context, gen int64, r driver.Retry) {
	c.mu.Lock()
	if c.gen != gen {
		c.mu.Unlock()
		return
	}
	c.retry = retryWait{until: chatNow().Add(r.Wait), why: retryWhy(r), n: r.Attempt, of: r.Of}
	c.mu.Unlock()
	c.post(ctx, gen, "chat.warn", map[string]any{"text": retryLine(r)})
}

// RetryLabel is the Thinking line's lead while the core waits to ask again, or "" when
// it is not waiting: "• Retrying in 27s · rate limited (2/6) ".
func (c *chatSession) RetryLabel(now time.Time) string {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.busy || c.retry.until.IsZero() {
		return ""
	}
	left := c.retry.until.Sub(now)
	if left <= 0 {
		return ""
	}
	return fmt.Sprintf("• Retrying in %s · %s (%d/%d) ", waitText(left), c.retry.why, c.retry.n, c.retry.of)
}

// waitText rounds a wait up to whole seconds ("27s", "1m 5s"), so the count never reads
// "0s" while there is still time left.
func waitText(d time.Duration) string {
	secs := int((d + time.Second - 1) / time.Second)
	if secs < 60 {
		return fmt.Sprintf("%ds", secs)
	}
	return fmt.Sprintf("%dm %ds", secs/60, secs%60)
}

// retryWhy names the cause in a few words for the countdown.
func retryWhy(r driver.Retry) string {
	switch r.Status {
	case "429":
		return "rate limited"
	case "408", "502", "503", "504", "529":
		return "provider busy (" + r.Status + ")"
	}
	return "provider said " + r.Status
}

// retryLine is the line left in the conversation for one retry.
func retryLine(r driver.Retry) string {
	who := r.Model
	if r.Provider != "" {
		who = r.Provider + "/" + r.Model
	}
	line := fmt.Sprintf("%s: %s. Retrying in %s (try %d of %d)", who, retryWhy(r), waitText(r.Wait), r.Attempt, r.Of)
	if r.Reason != "" {
		line += " · " + r.Reason
	}
	return line
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

// send starts one turn. It returns an error immediately for anything that can be
// decided up front (old core, a turn already running); everything that happens on
// the network is reported in the chat as a chat.error event.
func (c *chatSession) send(ctx context.Context, text string) error {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil
	}
	if err := chatRequires(c.core.Hello()); err != nil {
		return err
	}
	c.mu.Lock()
	if c.busy {
		c.mu.Unlock()
		return errChatBusy
	}
	c.busy = true
	gen := c.gen
	effort := c.effort
	workdir := c.workdir
	edits, runs, web := c.edits, c.runs, c.web
	if runs == policyDeny {
		// Denied is what a core that knows nothing about commands already does, and
		// naming it would make an older core refuse the whole request.
		runs = ""
	}
	if web == policyDeny {
		web = ""
	}
	hist := trimHistory(c.history, chatMaxHistory, chatMaxHistoryBytes)
	// The interface tools and their guide ride only with a question that asks for them.
	text, command := uiTriggerWord(text)
	withUI := command != ""
	if withUI {
		c.uiMode = true
	} else if c.uiMode {
		// Already in the interface conversation: no switch needed, nothing to show.
		withUI = true
	}
	if text == "" {
		c.busy = false
		c.mu.Unlock()
		return errUIPromptEmpty
	}
	c.turn++
	turn := c.turn
	turnCtx, cancel := context.WithCancel(ctx)
	c.cancel = cancel
	c.mu.Unlock()
	go c.run(turnCtx, text, command, withUI, hist, gen, effort, workdir, edits, runs, web, turn)
	return nil
}

// noteModelSwitch records that the user changed the chat model: a line in the
// conversation (so the change is on the record, with the model's name), and a sentence
// the next turn's system prompt carries so the model does not take earlier answers
// in the history for its own, nor claim to be the one that gave them. Choosing the
// model already in use records nothing. It returns the line shown, or "".
func (c *chatSession) noteModelSwitch(ctx context.Context, to string) string {
	c.mu.Lock()
	from := c.lastModel
	c.lastModel = to
	if to == "" || from == to {
		c.mu.Unlock()
		return ""
	}
	gen := c.gen
	line := "model changed to " + to
	c.modelNote = "The user switched the model to " + to + "."
	if from != "" {
		line = "model changed from " + from + " to " + to
		c.modelNote = "The user switched the model from " + from + " to " + to + ". Earlier replies in this conversation were written by " + from + ", not by you; do not claim to have done what they say unless the tool results show it."
	}
	c.mu.Unlock()
	c.post(ctx, gen, "chat.warn", map[string]any{"text": line})
	return line
}

// takeModelNote returns the pending model-switch sentence once.
func (c *chatSession) takeModelNote() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	n := c.modelNote
	c.modelNote = ""
	return n
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
	c.mu.Unlock()
	cancel()
	c.post(context.Background(), gen, "chat.cancelled", nil)
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
	c.uiMode = false
	c.busy = false
	cancel := c.cancel
	c.cancel = nil
	c.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}

// setUIMode turns the interface conversation on or off from the next message.
func (c *chatSession) setUIMode(on bool) {
	c.mu.Lock()
	c.uiMode = on
	c.mu.Unlock()
}

// inUIMode reports whether the interface conversation is on.
func (c *chatSession) inUIMode() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.uiMode
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

// post queues a conversation event without ever blocking its caller (see fail).
func (c *chatSession) post(ctx context.Context, gen int64, typ string, payload map[string]any) {
	if !c.current(gen) {
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

func (c *chatSession) run(ctx context.Context, text, command string, withUI bool, hist []driver.ChatTurn, gen int64, effort, workdir, edits, runs, web string, turn int64) {
	defer func() {
		c.mu.Lock()
		// Only the turn that is still current frees the session: a cancelled turn
		// winding down late must not mark its successor idle.
		if c.gen == gen && c.turn == turn {
			c.busy = false
			c.cancel = nil
			c.retry = retryWait{}
		}
		c.mu.Unlock()
	}()
	c.emit(ctx, gen, "run.prompt", map[string]any{"text": text, "command": command})
	c.emit(ctx, gen, "agent.activated", map[string]any{"agent": "assistant"})
	started := time.Now()
	core := c.core
	if c.dial != nil {
		conn, closeConn, err := c.dial(ctx)
		if err != nil {
			if ctx.Err() == nil {
				c.emit(ctx, gen, "agent.failed", map[string]any{"agent": "assistant"})
				c.fail(ctx, gen, chatErrorText(err))
			}
			return
		}
		defer closeConn()
		core = conn
	}
	system := chatSystem()
	if n := c.takeModelNote(); n != "" {
		system += " " + n
	}
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
		// A rate limit or an overload is waited out by the core itself; this only shows it.
		OnRetry: func(r driver.Retry) { c.noteRetry(ctx, gen, r) },
		// What it looks at while answering is shown as it happens.
		Workdir: workdir, Edits: edits, Runs: runs, Web: web,
		OnApproval: func(ctx context.Context, a driver.Approval) bool { return c.askUser(ctx, gen, a) },
		OnTool: func(t driver.ToolCall) {
			c.post(ctx, gen, "chat.tool", map[string]any{
				"name": t.Name, "arg": t.Arg, "ok": t.OK, "summary": t.Summary, "output": t.Output, "diff": t.Diff,
			})
		},
	}
	uiSystem := ""
	if withUI && c.ui != nil && workdir != "" {
		ui := c.ui
		// Only on a question that asked for it (/ui <request> or @ui). Always-on, the
		// hint made a user vibe-coding a TUI project unsure whether "the TUI" meant
		// this app or theirs; now the words mean this app only when the user said so.
		// The guide is read here, not left for the model to fetch: it is the whole
		// point of the trigger, and it saves the round trip.
		uiSystem = " " + uiSystemHint + "\n\n" + ui.guideText()
		params.System += uiSystem
		params.ClientTools = ui.definitions()
		params.OnClientTool = func(ctx context.Context, call driver.ClientToolCall) driver.ClientToolResult {
			return ui.call(ctx, call, func(a driver.Approval) bool { return c.askUser(ctx, gen, a) })
		}
	} else if withUI {
		c.post(ctx, gen, "chat.warn", map[string]any{"text": "/ui: the model can change the interface only inside a project folder with a core that supports it"})
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
			params.System = strings.TrimSuffix(params.System, uiSystem)
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
		params.System = strings.TrimSuffix(params.System, uiSystem)
		res, err = core.SubmitChatSend(ctx, params)
	}
	if ctx.Err() != nil {
		// Cancelled (cancelTurn already told the conversation) or the program is
		// closing: whatever came back is not wanted.
		return
	}
	if err != nil {
		c.emit(ctx, gen, "agent.failed", map[string]any{"agent": "assistant"})
		c.fail(ctx, gen, chatErrorText(err))
		return
	}
	c.mu.Lock()
	if c.gen != gen || c.turn != turn {
		c.mu.Unlock()
		return
	}
	c.history = append(c.history, driver.ChatTurn{Role: "user", Text: text}, driver.ChatTurn{Role: "assistant", Text: res.Text})
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

// chatErrorText is the sentence the user reads when a turn fails: the core's own
// words when it refused, a plain description otherwise.
func chatErrorText(err error) string {
	var r *driver.Refusal
	if errors.As(err, &r) && r.Message != "" {
		return r.Message
	}
	return "could not get an answer: " + err.Error()
}
