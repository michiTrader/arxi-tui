package extv2

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// Limits on what a running service may put in front of the user. A program that prints
// without end must not be able to fill the screen or the memory, so the lines are cut,
// counted and rationed, and what is dropped is reported instead of vanishing.
const (
	MaxServiceLineBytes = 4096
	MaxServiceLineRunes = 400
	// ServiceBurst and ServicePerSecond ration the lines that reach the screen.
	ServiceBurst     = 10
	ServicePerSecond = 5
)

// ServiceHandle is a service that has started. Stop ends it and everything it started;
// Done closes when the program has ended for any reason.
type ServiceHandle struct {
	cancel  context.CancelFunc
	done    chan struct{}
	err     error
	dropped atomic.Int64
	stopped atomic.Bool
}

// Done is closed once the program has ended and its output has been delivered.
func (h *ServiceHandle) Done() <-chan struct{} { return h.done }

// Err says why it ended: nil when the user stopped it or it ended by itself cleanly.
func (h *ServiceHandle) Err() error { <-h.done; return h.err }

// Dropped counts the lines the rationing refused.
func (h *ServiceHandle) Dropped() int64 { return h.dropped.Load() }

// Stop ends the program and its helpers. It is safe to call more than once.
func (h *ServiceHandle) Stop() { h.stopped.Store(true); h.cancel() }

// StartService starts the program of a service. Each line it prints is cleaned and handed
// to onLine, which runs on one goroutine at a time and must not block for long. The
// program's environment is scrubbed exactly like any other exec operation, its program
// must be one the package declared, and it has no standard input.
func (r *Runner) StartService(ctx context.Context, p *Package, sv Service, onLine func(string)) (*ServiceHandle, error) {
	o := sv.Op
	if o.Kind != KindExec || len(o.Argv) == 0 {
		return nil, fmt.Errorf("service %s is not an exec operation with a program, so nothing could start", sv.Name)
	}
	path, dir, err := r.resolveProgram(p, o.Argv[0])
	if err != nil {
		return nil, err
	}
	argv := make([]string, 0, len(o.Argv)-1)
	for i, a := range o.Argv[1:] {
		parts, err := parseTemplate(a)
		if err != nil {
			return nil, err
		}
		if hasPlaceholder(parts) {
			return nil, fmt.Errorf("argument %d of service %s uses a placeholder, and a service has no typed text to fill it", i+1, sv.Name)
		}
		argv = append(argv, expand(parts, Input{}, nil))
	}
	cctx, cancel := context.WithCancel(ctx)
	cmd := exec.CommandContext(cctx, path, argv...)
	cmd.Env = r.exec.childEnv(dir)
	if dir != "" {
		cmd.Dir = dir
	}
	cmd.Stdin = nil
	errOut := &cappedBuffer{max: 4096}
	cmd.Stderr = errOut
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		return nil, fmt.Errorf("service %s could not be set up (%s)", sv.Name, firstLine(err.Error()))
	}
	prepareChild(cmd)
	cmd.Cancel = func() error { return killChild(cmd) }
	cmd.WaitDelay = 500 * time.Millisecond
	if err := cmd.Start(); err != nil {
		cancel()
		return nil, fmt.Errorf("%s could not be started (%s); check that it is installed and may be run", filepath.Base(path), firstLine(err.Error()))
	}
	h := &ServiceHandle{cancel: cancel, done: make(chan struct{})}
	go func() {
		defer close(h.done)
		defer cancel()
		readServiceLines(stdout, h, onLine)
		werr := cmd.Wait()
		switch {
		case h.stopped.Load():
		case errors.Is(cctx.Err(), context.Canceled):
		case werr != nil:
			eb, _ := errOut.bytes()
			why := firstLine(strings.TrimSpace(CleanText(string(eb), 200, 3)))
			if why == "" {
				why = firstLine(werr.Error())
			}
			h.err = fmt.Errorf("%s stopped on its own: %s", filepath.Base(path), why)
		}
	}()
	return h, nil
}

// readServiceLines delivers the program's lines, bounded and rationed. A line longer than
// the limit is cut and the rest of it discarded; a line beyond the rate is dropped and
// counted.
func readServiceLines(rd io.Reader, h *ServiceHandle, onLine func(string)) {
	br := bufio.NewReaderSize(rd, MaxServiceLineBytes)
	bucket := newBucket(ServiceBurst, ServicePerSecond, time.Now)
	for {
		line, err := br.ReadSlice('\n')
		if errors.Is(err, bufio.ErrBufferFull) {
			// Too long: keep what fits and throw the rest of this line away.
			for {
				_, e2 := br.ReadSlice('\n')
				if !errors.Is(e2, bufio.ErrBufferFull) {
					break
				}
			}
			err = nil
		}
		if len(line) > 0 {
			text := CleanText(strings.TrimRight(string(line), "\r\n"), MaxServiceLineRunes, 1)
			if strings.TrimSpace(text) != "" {
				if bucket.take() {
					onLine(text)
				} else {
					h.dropped.Add(1)
				}
			}
		}
		if err != nil {
			return
		}
	}
}

// bucket is a token bucket on a clock a test can move.
type bucket struct {
	mu     sync.Mutex
	tokens float64
	max    float64
	rate   float64
	last   time.Time
	now    func() time.Time
}

func newBucket(burst int, perSecond float64, now func() time.Time) *bucket {
	return &bucket{tokens: float64(burst), max: float64(burst), rate: perSecond, last: now(), now: now}
}

func (b *bucket) take() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	t := b.now()
	b.tokens += t.Sub(b.last).Seconds() * b.rate
	if b.tokens > b.max {
		b.tokens = b.max
	}
	b.last = t
	if b.tokens < 1 {
		return false
	}
	b.tokens--
	return true
}
