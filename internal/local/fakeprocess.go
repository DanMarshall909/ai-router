package local

import (
	"context"
	"io"
	"strings"
	"sync"
	"time"
)

// FakeProcess simulates a subprocess for testing.
type FakeProcess struct {
	// StartDelay is how long Start blocks before succeeding.
	// Zero means immediate.
	StartDelay time.Duration

	// StartErr is returned by Start if non-nil.
	StartErr error

	// ExitStatus is sent on the Wait channel after ExitDelay.
	// Default is {Code: 0}.
	ExitStatus ExitStatus

	// ExitDelay is how long after Start before the process "exits".
	// Zero means immediate. Negative means never (hang).
	ExitDelay time.Duration

	// KillErr is returned by Kill. Default nil.
	KillErr error

	// KillBlocks makes Kill block forever (unresponsive shutdown).
	KillBlocks bool

	// StdoutContent is returned by Stdout().
	StdoutContent string

	// StderrContent is returned by Stderr().
	StderrContent string

	mu     sync.Mutex
	pid    int
	started bool
}

// NewFakeProcess creates a FakeProcess with sensible defaults.
func NewFakeProcess() *FakeProcess {
	return &FakeProcess{
		StdoutContent: "",
		StderrContent: "",
	}
}

func (f *FakeProcess) Start(ctx context.Context) error {
	if f.StartErr != nil {
		return f.StartErr
	}

	if f.StartDelay > 0 {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(f.StartDelay):
		}
	}

	f.mu.Lock()
	f.pid = 12345
	f.started = true
	f.mu.Unlock()

	if f.ExitDelay >= 0 {
		go func() {
			if f.ExitDelay > 0 {
				select {
				case <-ctx.Done():
					return
				case <-time.After(f.ExitDelay):
				}
			}
			f.mu.Lock()
			f.mu.Unlock()
		}()
	}

	return nil
}

func (f *FakeProcess) Wait() <-chan ExitStatus {
	ch := make(chan ExitStatus, 1)

	if f.ExitDelay < 0 {
		return ch
	}

	go func() {
		if f.ExitDelay > 0 {
			time.Sleep(f.ExitDelay)
		}
		ch <- f.ExitStatus
		close(ch)
	}()

	return ch
}

func (f *FakeProcess) Kill() error {
	if f.KillBlocks {
		select {}
	}
	return f.KillErr
}

func (f *FakeProcess) Pid() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.pid
}

func (f *FakeProcess) Stdout() io.ReadCloser {
	return io.NopCloser(strings.NewReader(f.StdoutContent))
}

func (f *FakeProcess) Stderr() io.ReadCloser {
	return io.NopCloser(strings.NewReader(f.StderrContent))
}
