package driver

import (
	"context"
	"time"

	"github.com/michiTrader/arxi_tui/internal/fold"
)

// MockDriver is a fake core driver for tests and Phase 0 development.
// It emits a fixed sequence of events to the fold channel, simulating what
// the arxi core would emit over NDJSON.
type MockDriver struct {
	events []fold.Event
}

// NewMock creates a driver that will emit the given events one by one,
// pausing between them to simulate streaming.
func NewMock(events []fold.Event) *MockDriver {
	return &MockDriver{events: events}
}

// Run emits events to the channel until drained, then stays idle.
// In Phase 1, this becomes the NDJSON reader from the arxi subprocess.
func (m *MockDriver) Run(ctx context.Context, out chan<- fold.Event) {
	for _, e := range m.events {
		select {
		case <-ctx.Done():
			return
		case out <- e:
		}
		// Simulate streaming latency
		time.Sleep(50 * time.Millisecond)
	}
}
