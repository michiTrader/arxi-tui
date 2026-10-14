package main

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/michiTrader/arxi_tui/internal/driver"
	"github.com/michiTrader/arxi_tui/internal/fold"
)

// longHistory is n question/answer pairs, each message about `size` bytes.
func longHistory(n, size int) []driver.ChatTurn {
	var h []driver.ChatTurn
	for i := 0; i < n; i++ {
		h = append(h,
			driver.ChatTurn{Role: "user", Text: fmt.Sprintf("question %d %s", i, strings.Repeat("q", size))},
			driver.ChatTurn{Role: "assistant", Text: fmt.Sprintf("answer %d %s", i, strings.Repeat("a", size))})
	}
	return h
}

func TestSplitForCompactKeepsTheRecentTurnsAndStartsOnAQuestion(t *testing.T) {
	h := longHistory(6, 1000)
	old, keep, ok := splitForCompact(h)
	if !ok {
		t.Fatal("a long history must be worth compacting")
	}
	if len(keep) != compactKeepTurns || keep[0].Role != "user" {
		t.Fatalf("kept %d messages starting on %q; want %d starting on a question", len(keep), keep[0].Role, compactKeepTurns)
	}
	if len(old)+len(keep) != len(h) || old[len(old)-1].Role != "assistant" {
		t.Fatalf("old+keep must be the whole history, cut between a pair: old=%d keep=%d", len(old), len(keep))
	}
	// An odd tail still starts the kept part on a question.
	odd := append(longHistory(5, 1000), driver.ChatTurn{Role: "user", Text: "unanswered"})
	_, keep, _ = splitForCompact(odd)
	if keep[0].Role != "user" {
		t.Fatalf("kept part starts on %q", keep[0].Role)
	}
}

func TestSplitForCompactRefusesWhatIsNotWorthIt(t *testing.T) {
	if _, keep, ok := splitForCompact(longHistory(2, 100)); ok || len(keep) != 4 {
		t.Fatal("a short history is not compacted")
	}
	// Many messages but tiny: the summary would be as long as the text.
	if _, _, ok := splitForCompact(longHistory(10, 5)); ok {
		t.Fatal("a history with little text is not compacted")
	}
	if _, _, ok := splitForCompact(nil); ok {
		t.Fatal("nothing is not compacted")
	}
}

func TestCompactTranscriptClipsLongMessagesAndSaysWhatItDropped(t *testing.T) {
	huge := driver.ChatTurn{Role: "assistant", Text: strings.Repeat("x", 50_000)}
	tr := compactTranscript([]driver.ChatTurn{{Role: "user", Text: "hi"}, huge})
	if len(tr) > compactTurnBytes+500 {
		t.Fatalf("one message was not clipped: %d bytes", len(tr))
	}
	if !strings.Contains(tr, "bytes left out") {
		t.Fatal("a clipped message must say so")
	}
	// Too many messages: the OLDEST go, and the transcript says how many.
	h := longHistory(60, 4000)
	tr = compactTranscript(h)
	if len(tr) > compactSendBytes+200 {
		t.Fatalf("transcript is %d bytes, over the cap", len(tr))
	}
	if !strings.Contains(tr, "oldest messages are not shown") || !strings.Contains(tr, "answer 59") || strings.Contains(tr, "question 0 ") {
		t.Fatal("it must drop the oldest, keep the newest and say so")
	}
}

func TestCompactedHistoryStartsOnAUserTurnAndAlternates(t *testing.T) {
	h := compactedHistory("the gist", longHistory(2, 10))
	if h[0].Role != "user" || !strings.Contains(h[0].Text, "the gist") || !strings.HasPrefix(h[0].Text, summaryHeader) {
		t.Fatalf("first turn = %+v", h[0])
	}
	for i, t2 := range h {
		if want := []string{"user", "assistant"}[i%2]; t2.Role != want {
			t.Fatalf("turn %d is %s, want %s: the history must alternate", i, t2.Role, want)
		}
	}
	big := compactedHistory(strings.Repeat("s", 50_000), nil)
	if len(big[0].Text) > compactSummaryBytes+len(summaryHeader)+10 {
		t.Fatalf("a runaway summary was kept whole: %d bytes", len(big[0].Text))
	}
}

func TestFallbackSummaryListsTheRequestsAndNeedsNoModel(t *testing.T) {
	s := fallbackSummary(longHistory(3, 300), "read(a.go)")
	for _, w := range []string{"1. question 0", "2. question 1", "3. question 2", "read(a.go)", "could not be written"} {
		if !strings.Contains(s, w) {
			t.Errorf("outline lacks %q:\n%s", w, s)
		}
	}
	if strings.Contains(s, "answer 0") {
		t.Error("the outline lists what the user asked, not the answers")
	}
}

func TestShouldAutoCompactNeedsTheSettingTheWindowThePercentageAndAWorthyHistory(t *testing.T) {
	h := longHistory(6, 1000)
	on := autoCompactSetting{On: true, Percent: 80}
	cases := []struct {
		name string
		s    autoCompactSetting
		ctx  uint64
		win  int
		h    []driver.ChatTurn
		want bool
	}{
		{"at the threshold", on, 800_000, 1_000_000, h, true},
		{"over it", on, 950_000, 1_000_000, h, true},
		{"just under", on, 799_999, 1_000_000, h, false},
		{"switched off", autoCompactSetting{Percent: 80}, 950_000, 1_000_000, h, false},
		{"window unknown", on, 950_000, 0, h, false},
		{"nothing held", on, 0, 1_000_000, h, false},
		{"history too short to help", on, 950_000, 1_000_000, longHistory(1, 100), false},
		{"percent below range is clamped up", autoCompactSetting{On: true, Percent: 1}, 99_999, 1_000_000, h, false},
		{"percent above range is clamped down", autoCompactSetting{On: true, Percent: 100}, 950_000, 1_000_000, h, true},
	}
	for _, c := range cases {
		if got := shouldAutoCompact(c.s, c.ctx, c.win, c.h); got != c.want {
			t.Errorf("%s: got %v, want %v", c.name, got, c.want)
		}
	}
}

func TestContextPercent(t *testing.T) {
	for _, c := range []struct {
		ctx  uint64
		win  int
		want int
	}{{0, 1_000_000, 0}, {5, 0, 0}, {37_000, 1_000_000, 3}, {1, 1_000_000, 1}, {500_000, 1_000_000, 50}, {1_200_000, 1_000_000, 120}} {
		if got := contextPercent(c.ctx, c.win); got != c.want {
			t.Errorf("contextPercent(%d,%d) = %d, want %d", c.ctx, c.win, got, c.want)
		}
	}
}

func TestCompactLineTakesFreeTextAfterTheVerbOnly(t *testing.T) {
	if f, ok := compactLine("/compact", 0, ""); !ok || f != "" {
		t.Fatalf("bare: %q %v", f, ok)
	}
	if f, ok := compactLine("/compact keep the db schema", 0, ""); !ok || f != "keep the db schema" {
		t.Fatalf("with focus: %q %v", f, ok)
	}
	for _, no := range []string{"compact the file please", "/compaction", "hello /compact", "/clear"} {
		if _, ok := compactLine(no, 0, ""); ok {
			t.Errorf("%q must not be /compact", no)
		}
	}
}

// summariser answers a summary request, or fails it; it also records what it was asked.
type summariser struct {
	seenChat
	reply string
	err   error
	block chan struct{}
}

func (s *summariser) SubmitChatSend(ctx context.Context, p driver.ChatSendParams) (*driver.ChatSendResult, error) {
	s.mu.Lock()
	s.got = append(s.got, p)
	s.mu.Unlock()
	if s.block != nil {
		select {
		case <-s.block:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	if s.err != nil {
		return nil, s.err
	}
	return &driver.ChatSendResult{Text: s.reply, Model: "m", Provider: "p"}, nil
}

func chatWith(core chatSender, h []driver.ChatTurn) (*chatSession, chan fold.Event) {
	out := make(chan fold.Event, 64)
	c := newChatSession(core, out)
	c.setHistory(h)
	return c, out
}

func collect(out chan fold.Event, until string) []fold.Event {
	var evs []fold.Event
	deadline := time.After(3 * time.Second)
	for {
		select {
		case e := <-out:
			evs = append(evs, e)
			if e.Type == until {
				return evs
			}
		case <-deadline:
			return evs
		}
	}
}

func TestCompactReplacesTheOldHistoryWithTheModelsSummaryAndAsksWithNothingElse(t *testing.T) {
	core := &summariser{reply: "GOAL: ship v2. FILES: a.go"}
	c, out := chatWith(core, longHistory(6, 1000))
	before, _ := c.historySize()
	c.setWorkdir(t.TempDir())
	c.setEdits("allow")
	c.setEffort("high")
	if err := c.compact(context.Background(), "the schema"); err != nil {
		t.Fatal(err)
	}
	evs := collect(out, "agent.turn_done")
	p := core.last()
	if p.Workdir != "" || p.Edits != "" || p.Effort != "" || len(p.History) != 0 || p.OnClientTool != nil {
		t.Fatalf("the summary request carried baggage: %+v", p)
	}
	if !strings.Contains(p.Prompt, "the schema") || !strings.Contains(p.Prompt, "question 0") || strings.Contains(p.Prompt, "question 5") {
		t.Fatalf("the prompt must hold the OLD turns and the focus, and not the kept ones:\n%.300s", p.Prompt)
	}
	c.mu.Lock()
	h := append([]driver.ChatTurn(nil), c.history...)
	busy := c.busy
	c.mu.Unlock()
	if busy {
		t.Error("the session must be free again")
	}
	if len(h) != 2+compactKeepTurns || !strings.Contains(h[0].Text, "GOAL: ship v2") || !strings.Contains(h[len(h)-1].Text, "answer 5") {
		t.Fatalf("history = %d messages, first %.60q", len(h), h[0].Text)
	}
	if before <= len(h) {
		t.Errorf("the history did not shrink: %d -> %d", before, len(h))
	}
	var compacted, note bool
	for _, e := range evs {
		compacted = compacted || e.Type == "chat.compacted"
		if e.Type == "chat.note" {
			note = strings.Contains(e.Payload["text"].(string), "summarised by the model")
		}
	}
	if !compacted || !note {
		t.Fatalf("events %v: want chat.compacted and a note naming the model", eventTypes(evs))
	}
}

func eventTypes(evs []fold.Event) []string {
	var out []string
	for _, e := range evs {
		out = append(out, e.Type)
	}
	return out
}

func TestCompactFallsBackToTheOutlineWhenTheModelCannotBeAskedAndSaysWhy(t *testing.T) {
	for name, core := range map[string]*summariser{
		"error": {err: errors.New("context deadline exceeded")},
		"empty": {reply: "  "},
	} {
		c, out := chatWith(core, longHistory(6, 1000))
		if err := c.compact(context.Background(), ""); err != nil {
			t.Fatal(err)
		}
		evs := collect(out, "agent.turn_done")
		c.mu.Lock()
		first := c.history[0].Text
		c.mu.Unlock()
		if !strings.Contains(first, "Automatic outline") || !strings.Contains(first, "question 0") {
			t.Errorf("%s: the outline did not replace the old history: %.100q", name, first)
		}
		var note string
		for _, e := range evs {
			if e.Type == "chat.note" {
				note, _ = e.Payload["text"].(string)
			}
		}
		if !strings.Contains(note, "without the model") {
			t.Errorf("%s: the report must say the model was not used: %q", name, note)
		}
		if name == "error" && !strings.Contains(note, "deadline") {
			t.Errorf("the report must give the reason: %q", note)
		}
	}
}

func TestCompactOnAShortHistoryChangesNothingAndSaysSo(t *testing.T) {
	core := &summariser{reply: "x"}
	h := longHistory(2, 50)
	c, out := chatWith(core, h)
	if err := c.compact(context.Background(), ""); err != nil {
		t.Fatal(err)
	}
	evs := collect(out, "chat.note")
	if len(core.got) != 0 {
		t.Fatal("no model call is spent on a history not worth compacting")
	}
	if n, _ := c.historySize(); n != len(h) {
		t.Fatal("the history must be untouched")
	}
	if len(evs) == 0 || !strings.Contains(evs[len(evs)-1].Payload["text"].(string), "nothing to compact") {
		t.Fatalf("events %v", eventTypes(evs))
	}
}

func TestCompactWhileATurnIsPendingIsRefused(t *testing.T) {
	core := &summariser{block: make(chan struct{})}
	c, _ := chatWith(core, longHistory(6, 1000))
	if err := c.compact(context.Background(), ""); err != nil {
		t.Fatal(err)
	}
	if err := c.compact(context.Background(), ""); !errors.Is(err, errChatBusy) {
		t.Fatalf("a second compact while one runs: %v", err)
	}
	if err := c.send(context.Background(), "hello"); !errors.Is(err, errChatBusy) {
		t.Fatalf("a question while compacting: %v", err)
	}
	close(core.block)
}

func TestCancellingACompactLeavesTheHistoryAsItWasWithNoCancelledQuestionInIt(t *testing.T) {
	core := &summariser{block: make(chan struct{})}
	h := longHistory(6, 1000)
	c, _ := chatWith(core, h)
	if err := c.compact(context.Background(), ""); err != nil {
		t.Fatal(err)
	}
	time.Sleep(50 * time.Millisecond)
	if !c.cancelTurn() {
		t.Fatal("the compaction should be cancellable")
	}
	time.Sleep(50 * time.Millisecond)
	c.mu.Lock()
	got := append([]driver.ChatTurn(nil), c.history...)
	busy := c.busy
	c.mu.Unlock()
	if busy || len(got) != len(h) || texts(got) != texts(h) {
		t.Fatalf("cancel changed the history: %d -> %d messages (busy=%v)", len(h), len(got), busy)
	}
}

func TestResumeRebuildsACompactedConversationFromItsEvents(t *testing.T) {
	var evs []fold.Event
	for i := 0; i < 4; i++ {
		evs = append(evs,
			fold.Event{Type: "run.prompt", Payload: map[string]any{"text": fmt.Sprintf("q%d", i)}},
			fold.Event{Type: "llm.response", Payload: map[string]any{"text": fmt.Sprintf("a%d", i)}})
	}
	evs = append(evs, fold.Event{Type: "chat.compacted", Payload: map[string]any{"summary": "SUMMARY", "keep": float64(2)}})
	evs = append(evs,
		fold.Event{Type: "run.prompt", Payload: map[string]any{"text": "after"}},
		fold.Event{Type: "llm.response", Payload: map[string]any{"text": "reply"}})
	h := historyFromEvents(evs)
	want := "SUMMARY"
	if !strings.Contains(h[0].Text, want) || h[len(h)-1].Text != "reply" || h[len(h)-2].Text != "after" {
		t.Fatalf("resumed history: %v", texts(h))
	}
	if strings.Contains(texts(h), "q0") || strings.Contains(texts(h), "a1") {
		t.Fatal("what was summarised must not come back word for word")
	}
	if !strings.Contains(texts(h), "q3") {
		t.Fatal("the kept messages must survive")
	}
}

func TestTheFoldLowersTheContextFigureWhenTheConversationIsCompacted(t *testing.T) {
	s := fold.Fold([]fold.Event{
		{Type: "llm.response", Payload: map[string]any{"text": "x", "tokens_in": float64(900_000), "tokens_out": float64(10), "context_tokens": float64(900_010)}},
		{Type: "chat.compacted", Payload: map[string]any{"summary": "s", "keep": float64(4), "context_tokens": float64(3_000)}},
	})
	if s.LastContext != 3_000 {
		t.Fatalf("LastContext = %d; /compact must show its effect at once", s.LastContext)
	}
	if s.UsageIn != 900_000 {
		t.Fatalf("UsageIn = %d; the running total is what was spent and compacting does not refund it", s.UsageIn)
	}
}

// A turn of tool calls can fill the model's window while the history between questions
// is a few short messages, which is why /compact said "nothing to compact" right after a
// huge turn. The answer now says what the model held and why the history is small.
func TestNothingToCompactSaysWhatTheModelHeldAndWhyTheHistoryIsSmall(t *testing.T) {
	got := nothingToCompact(14, 4800, 148_000)
	for _, want := range []string{"14 messages", "too short to summarise", "148k tokens at its last request", "tool calls and results", "/clear"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in %q", want, got)
		}
	}
	if quiet := nothingToCompact(14, 4800, 0); strings.Contains(quiet, "last request") {
		t.Errorf("with nothing reported there is no figure to compare: %q", quiet)
	}
}

func TestCompactOnAShortHistoryUsesTheContextTheCoreReported(t *testing.T) {
	c, out := chatWith(&summariser{}, longHistory(2, 20))
	c.mu.Lock()
	c.lastAsk = 90_000
	c.mu.Unlock()
	if err := c.compact(context.Background(), ""); err != nil {
		t.Fatal(err)
	}
	evs := collect(out, "chat.note")
	if len(evs) == 0 || !strings.Contains(evs[len(evs)-1].Payload["text"].(string), "90k tokens at its last request") {
		t.Fatalf("events %v", eventTypes(evs))
	}
}

// A deadline is a timeout, not a window overflow, and the user is told which.
func TestADeadlineErrorIsExplainedAsATimeoutNotAFullWindow(t *testing.T) {
	for _, err := range []error{
		errors.New("context deadline exceeded"),
		&driver.Refusal{Message: "apinex2/free/mimo: Post \"https://x\": context deadline exceeded"},
	} {
		got := chatErrorText(err)
		if !strings.Contains(got, "timeout, not a full context window") {
			t.Errorf("%v: %q", err, got)
		}
	}
	if got := chatErrorText(&driver.Refusal{Message: "the provider is down"}); strings.Contains(got, "timeout") {
		t.Errorf("an unrelated failure must not be called a timeout: %q", got)
	}
}

// The size the core reports ask by ask is what /compact compares against, and /clear
// forgets it with the rest of the conversation.
func TestClearForgetsTheReportedContext(t *testing.T) {
	c, _ := chatWith(&summariser{}, longHistory(2, 20))
	c.mu.Lock()
	c.lastAsk = 5000
	c.mu.Unlock()
	c.reset()
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.lastAsk != 0 {
		t.Errorf("lastAsk = %d after /clear", c.lastAsk)
	}
}

// usageThenFail reports two asks and then fails, like a turn that times out on round 3.
type usageThenFail struct{ seenChat }

func (u *usageThenFail) SubmitChatSend(ctx context.Context, p driver.ChatSendParams) (*driver.ChatSendResult, error) {
	p.OnUsage(40_000)
	p.OnUsage(90_000)
	return nil, errors.New("context deadline exceeded")
}

// The whole path: a turn that dies after answering some asks leaves the bar on the last
// real size of the conversation, and /compact knows it.
func TestAFailedTurnLeavesTheLastReportedContextOnTheBarAndForCompact(t *testing.T) {
	c, out := chatWith(&usageThenFail{}, nil)
	if err := c.send(context.Background(), "do a lot"); err != nil {
		t.Fatal(err)
	}
	evs := collect(out, "chat.error")
	var log []fold.Event
	log = append(log, evs...)
	if st := fold.Fold(log); st.LastContext != 90_000 {
		t.Errorf("LastContext after the failed turn = %d, want 90000; events %v", st.LastContext, eventTypes(evs))
	}
	var errText string
	for _, e := range evs {
		if e.Type == "chat.error" {
			errText, _ = e.Payload["text"].(string)
		}
	}
	if !strings.Contains(errText, "timeout, not a full context window") {
		t.Errorf("error text = %q", errText)
	}
	c.mu.Lock()
	held := c.lastAsk
	c.mu.Unlock()
	if held != 90_000 {
		t.Errorf("lastAsk = %d, want 90000", held)
	}
}
