package main

import (
	"sync"
	"time"

	"github.com/michiTrader/arxi/internal/provider"
)

// paceStatus marks a chat.retry notification that is not a retry at all: the core is
// waiting for the provider's request window to open before it spends a request that
// would only be refused. The client words it differently from a retry.
const paceStatus = "pace"

// emptyStatus marks a chat.retry notification caused by an answer with nothing in it.
const emptyStatus = "empty"

// pacer remembers what the provider last said about its request budget, so the next
// ask of a turn can wait for the window instead of being refused for it. A free tier
// that allows five requests a minute answers every ask with how many are left and when
// the window resets; a tool loop asks again right after each tool, and without pacing
// it spends the sixth ask on a 429 and then waits the same time anyway, with one
// request fewer in its retry budget.
type pacer struct {
	mu    sync.Mutex
	until time.Time
}

// note records an answer's rate-limit headers: a spent budget sets the time the window
// opens, anything else clears it (a window that has requests left needs no wait).
func (p *pacer) note(rl provider.RateLimit) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if rl.Spent() {
		p.until = time.Now().Add(rl.Reset)
		return
	}
	p.until = time.Time{}
}

// due is how long the next ask should wait, zero when it can go now. A second of
// margin is added for the same reason a retry has one.
func (p *pacer) due() time.Duration {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.until.IsZero() {
		return 0
	}
	d := time.Until(p.until)
	if d <= 0 {
		return 0
	}
	return d + chatRetryMargin
}
