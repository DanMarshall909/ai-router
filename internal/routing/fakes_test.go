package routing_test

import (
	"context"
	"fmt"
	"iter"
	"time"

	"github.com/DanMarshall909/ai-router/internal/routing"
)

// TestClock is a deterministic clock for testing.
type TestClock struct {
	NowFn     func() time.Time
	AfterFn   func(d time.Duration) <-chan time.Time
	TickFn    func(d time.Duration) *time.Ticker
}

func (c *TestClock) Now() time.Time {
	if c.NowFn != nil {
		return c.NowFn()
	}
	return time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
}

func (c *TestClock) After(d time.Duration) <-chan time.Time {
	if c.AfterFn != nil {
		return c.AfterFn(d)
	}
	ch := make(chan time.Time, 1)
	ch <- c.Now().Add(d)
	return ch
}

func (c *TestClock) NewTicker(d time.Duration) *time.Ticker {
	if c.TickFn != nil {
		return c.TickFn(d)
	}
	return time.NewTicker(d)
}

// FakeChatProvider is a hand-written fake for testing.
type FakeChatProvider struct {
	// Chunks is the sequence of chunks to return.
	Chunks []routing.Chunk

	// StreamErr is returned by Stream if non-nil.
	StreamErr error

	// CallCount tracks how many times Stream was called.
	CallCount int
}

func (f *FakeChatProvider) Stream(ctx context.Context, req routing.ChatRequest) (iter.Seq2[routing.Chunk, error], error) {
	f.CallCount++
	if f.StreamErr != nil {
		return nil, f.StreamErr
	}
	return func(yield func(routing.Chunk, error) bool) {
		for _, chunk := range f.Chunks {
			if !yield(chunk, nil) {
				return
			}
		}
	}, nil
}

// FailFirstStream is a fake that fails on the first call, then succeeds.
type FailFirstStream struct {
	Chunks    []routing.Chunk
	callCount int
}

func (f *FailFirstStream) Stream(ctx context.Context, req routing.ChatRequest) (iter.Seq2[routing.Chunk, error], error) {
	f.callCount++
	if f.callCount == 1 {
		return nil, fmt.Errorf("simulated first-call failure")
	}
	return func(yield func(routing.Chunk, error) bool) {
		for _, chunk := range f.Chunks {
			if !yield(chunk, nil) {
				return
			}
		}
	}, nil
}
