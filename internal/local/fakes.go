package local

import (
	"context"
	"fmt"
	"io"
	"strings"
	"sync"
	"sync/atomic"
)

// FakeProcessFactory creates testProcess instances for testing the manager.
type FakeProcessFactory struct {
	mu         sync.Mutex
	startCount int
	procs      []*testProcess
	healthURL  string
	hangOnWait bool
	failStart  bool
	failOnce   bool
}

// NewFakeProcessFactory returns a factory that creates immediately-ready processes.
func NewFakeProcessFactory() *FakeProcessFactory {
	return &FakeProcessFactory{}
}

// NewAlwaysFailFactory returns a factory where Start always returns an error.
func NewAlwaysFailFactory() *FakeProcessFactory {
	return &FakeProcessFactory{failStart: true}
}

// NewFailThenSucceedFactory returns a factory that fails on the first Start,
// then succeeds on subsequent calls.
func NewFailThenSucceedFactory() *FakeProcessFactory {
	return &FakeProcessFactory{failOnce: true}
}

// NewFailThenSucceedFactoryWithHealth returns a factory that fails on the first Start,
// then succeeds on subsequent calls, with health URL for readiness checking.
func NewFailThenSucceedFactoryWithHealth(healthURL string) *FakeProcessFactory {
	return &FakeProcessFactory{failOnce: true, healthURL: healthURL}
}

func (f *FakeProcessFactory) NewProcess(name string, args ...string) Process {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.startCount++

	fp := &testProcess{
		pid:        int32(12345 + f.startCount),
		hangOnWait: f.hangOnWait,
	}

	if f.failStart {
		fp.failOnStart = true
	} else if f.failOnce && f.startCount == 1 {
		fp.failOnStart = true
	}

	f.procs = append(f.procs, fp)
	return fp
}

func (f *FakeProcessFactory) NewProcessWithEnv(name string, env []string, args ...string) Process {
	return f.NewProcess(name, args...)
}

func (f *FakeProcessFactory) StartCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.startCount
}

func (f *FakeProcessFactory) SetHangOnWait(hang bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.hangOnWait = hang
	for _, p := range f.procs {
		p.hangOnWait = hang
	}
}

func (f *FakeProcessFactory) SimulateCrash() {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, p := range f.procs {
		p.simulateCrash()
	}
}

// testProcess is a fake Process used by the manager tests.
type testProcess struct {
	pid         int32
	started     bool
	waitCh      chan ExitStatus
	hangOnWait  bool
	failOnStart bool
	crashed     bool
	killed      bool
	mu          sync.Mutex
	done        chan struct{}
}

func (p *testProcess) Start(ctx context.Context) error {
	p.mu.Lock()
	if p.failOnStart {
		p.mu.Unlock()
		return fmt.Errorf("process failed to start")
	}
	p.started = true
	p.waitCh = make(chan ExitStatus, 1)
	p.done = make(chan struct{})
	p.mu.Unlock()
	return nil
}

func (p *testProcess) Wait() <-chan ExitStatus {
	p.mu.Lock()
	ch := p.waitCh
	hang := p.hangOnWait
	done := p.done
	p.mu.Unlock()

	if hang {
		// Return a channel that closes when the process is force-killed.
		out := make(chan ExitStatus, 1)
		go func() {
			select {
			case s := <-ch:
				out <- s
			case <-done:
				out <- ExitStatus{Code: -1, Err: fmt.Errorf("force-killed")}
			}
			close(out)
		}()
		return out
	}
	return ch
}

func (p *testProcess) Kill() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.hangOnWait {
		// Simulate force-kill: close done to unblock Wait.
		if p.done != nil {
			close(p.done)
		}
		return nil
	}
	if p.waitCh != nil {
		select {
		case p.waitCh <- ExitStatus{Code: -1, Err: fmt.Errorf("killed")}:
		default:
		}
	}
	return nil
}

func (p *testProcess) Pid() int {
	return int(atomic.LoadInt32(&p.pid))
}

func (p *testProcess) Stdout() io.ReadCloser {
	return io.NopCloser(strings.NewReader(""))
}

func (p *testProcess) Stderr() io.ReadCloser {
	return io.NopCloser(strings.NewReader(""))
}

func (p *testProcess) simulateCrash() {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.waitCh != nil && !p.crashed {
		p.crashed = true
		select {
		case p.waitCh <- ExitStatus{Code: 1, Err: fmt.Errorf("process crashed")}:
		default:
		}
	}
}
