package httpapi

import (
	"context"
	"fmt"
	"iter"
	"log/slog"

	"github.com/DanMarshall909/ai-router/internal/local"
	"github.com/DanMarshall909/ai-router/internal/routing"
)

// Dispatcher routes requests to providers with single controlled fallback.
type Dispatcher struct {
	local  routing.ChatProvider
	cloud  routing.ChatProvider
	manager *local.Manager
}

// NewDispatcher creates a dispatcher with local and cloud providers.
func NewDispatcher(localProvider, cloudProvider routing.ChatProvider, mgr *local.Manager) *Dispatcher {
	return &Dispatcher{
		local:   localProvider,
		cloud:   cloudProvider,
		manager: mgr,
	}
}

// Dispatch routes a request. It tries local first (for auto/local strategies),
// falling back to cloud exactly once on pre-first-chunk local failure.
func (d *Dispatcher) Dispatch(ctx context.Context, req routing.ChatRequest, decision routing.RoutingDecision) (iter.Seq2[routing.Chunk, error], error) {
	isLocal := decision.Strategy == routing.QuickLocal || decision.Strategy == routing.DeepLocal

	slog.Debug("dispatching request",
		"model", req.Model,
		"strategy", decision.Strategy,
		"provider", decision.Provider,
		"stream", req.Stream,
		"messages", len(req.Messages),
	)

	if isLocal && d.local != nil {
		// Ensure the local model process is running
		if d.manager != nil {
			if err := d.manager.Start(ctx); err != nil {
				slog.Warn("failed to start local model, trying cloud", "err", err)
				if d.cloud != nil {
					return d.cloud.Stream(ctx, req)
				}
				return nil, fmt.Errorf("local model failed to start and no cloud provider: %w", err)
			}
			d.manager.RequestBegin()
			defer d.manager.RequestEnd()
		}

		stream, err := d.local.Stream(ctx, req)
		if err != nil {
			slog.Warn("local provider failed", "err", err)
			if d.cloud != nil && d.isPreFirstChunkError(err) {
				slog.Info("falling back to cloud", "reason", "pre-first-chunk local failure")
				decision.IsFallback = true
				return d.cloud.Stream(ctx, req)
			}
			return nil, err
		}
		return d.wrapLocalWithFallback(ctx, req, stream, decision), nil
	}

	if d.cloud != nil {
		slog.Debug("routing to cloud provider")
		return d.cloud.Stream(ctx, req)
	}

	return nil, fmt.Errorf("no provider available for strategy %s", decision.Strategy)
}

// wrapLocalWithFallback wraps a local stream, falling back to cloud
// if the first error occurs before any chunk is yielded.
func (d *Dispatcher) wrapLocalWithFallback(
	ctx context.Context,
	req routing.ChatRequest,
	localStream iter.Seq2[routing.Chunk, error],
	decision routing.RoutingDecision,
) iter.Seq2[routing.Chunk, error] {
	return func(yield func(routing.Chunk, error) bool) {
		firstChunk := true
		for chunk, err := range localStream {
			if err != nil {
				if firstChunk && d.cloud != nil && d.isPreFirstChunkError(err) {
					slog.Warn("local failed before first chunk, falling back to cloud",
						"err", err,
						"model", req.Model,
					)
					decision.IsFallback = true
					cloudStream, cloudErr := d.cloud.Stream(ctx, req)
					if cloudErr != nil {
						slog.Error("cloud fallback also failed", "err", cloudErr)
						yield(routing.Chunk{}, cloudErr)
						return
					}
					for cloudChunk, cloudErr := range cloudStream {
						if !yield(cloudChunk, cloudErr) {
							return
						}
					}
					return
				}
				yield(routing.Chunk{}, err)
				return
			}
			firstChunk = false
			if !yield(chunk, nil) {
				return
			}
		}
	}
}

func (d *Dispatcher) isPreFirstChunkError(err error) bool {
	var outcome local.FailureOutcome
	if ok := errorAs(err, &outcome); ok {
		return outcome.Kind == local.FailureBeforeFirstChunk
	}
	return true
}

func errorAs(err error, target any) bool {
	if outcome, ok := err.(local.FailureOutcome); ok {
		if t, ok := target.(*local.FailureOutcome); ok {
			*t = outcome
			return true
		}
	}
	return false
}
