package httpapi

import (
	"context"
	"fmt"
	"iter"
	"log/slog"
	"strings"
	"time"

	"github.com/DanMarshall909/ai-router/internal/local"
	"github.com/DanMarshall909/ai-router/internal/routing"
)

// Dispatcher routes requests to providers with single controlled fallback.
type Dispatcher struct {
	local   routing.ChatProvider
	cloud   routing.ChatProvider
	manager *local.Manager
}

const (
	localAssessmentInstruction    = "Answer simple requests. For complex coding reply exactly <CLOUD_CODING>. For complex reasoning reply exactly <CLOUD_REASONING>."
	localAssessmentCodingLabel    = "<CLOUD_CODING>"
	localAssessmentReasoningLabel = "<CLOUD_REASONING>"
	localModelName                = "bonsai"
)

// NewDispatcher creates a dispatcher with local and cloud providers.
func NewDispatcher(localProvider, cloudProvider routing.ChatProvider, mgr *local.Manager) *Dispatcher {
	return &Dispatcher{
		local:   localProvider,
		cloud:   cloudProvider,
		manager: mgr,
	}
}

// Assess uses the ready local model to choose a strategy for an automatic request.
// For LOCAL, it returns the completed local response to avoid a second inference.
func (d *Dispatcher) Assess(ctx context.Context, req routing.ChatRequest) (routing.InferenceStrategy, iter.Seq2[routing.Chunk, error], bool) {
	if d.local == nil {
		slog.Info("local self-assessment skipped", "reason", "local provider unavailable")
		return routing.QuickLocal, nil, false
	}
	if d.manager == nil {
		slog.Info("local self-assessment skipped", "reason", "local manager unavailable")
		return routing.QuickLocal, nil, false
	}
	state := d.manager.State()
	if state != routing.StateReady {
		slog.Info("local self-assessment skipped", "reason", "local model not ready", "state", state)
		return routing.QuickLocal, nil, false
	}

	started := time.Now()
	slog.Info("local self-assessment started", "messages", len(req.Messages))
	assessment := routing.ChatRequest{
		Model:    routing.AutoModelName,
		Messages: append([]routing.Message{{Role: "system", Content: localAssessmentInstruction}}, req.Messages...),
	}
	d.manager.RequestBegin()
	defer d.manager.RequestEnd()
	stream, err := d.local.Stream(ctx, assessment)
	if err != nil {
		slog.Warn("local self-assessment failed", "err", err, "duration", time.Since(started))
		return routing.CloudReasoning, nil, true
	}

	var result strings.Builder
	for chunk, err := range stream {
		if err != nil {
			slog.Warn("local self-assessment failed", "err", err, "duration", time.Since(started))
			return routing.CloudReasoning, nil, true
		}
		result.WriteString(chunk.Content)
	}

	response := strings.TrimSpace(result.String())
	label := strings.ToUpper(response)
	switch {
	case label == localAssessmentCodingLabel:
		slog.Info("local self-assessment complete", "strategy", routing.CloudCoding, "duration", time.Since(started), "answer_reused", false)
		return routing.CloudCoding, nil, true
	case label == localAssessmentReasoningLabel:
		slog.Info("local self-assessment complete", "strategy", routing.CloudReasoning, "duration", time.Since(started), "answer_reused", false)
		return routing.CloudReasoning, nil, true
	case response != "":
		slog.Info("local self-assessment complete", "strategy", routing.QuickLocal, "duration", time.Since(started), "answer_reused", true)
		return routing.QuickLocal, singleChunk(routing.Chunk{Content: response, Provider: routing.ProviderLocal, Model: localModelName}), true
	default:
		slog.Warn("local self-assessment returned an empty response", "duration", time.Since(started))
		return routing.CloudReasoning, nil, true
	}
}

func singleChunk(chunk routing.Chunk) iter.Seq2[routing.Chunk, error] {
	return func(yield func(routing.Chunk, error) bool) {
		yield(chunk, nil)
	}
}

// Dispatch routes a request. It tries local first (for auto/local strategies),
// falling back to cloud exactly once on pre-first-chunk local failure.
func (d *Dispatcher) Dispatch(ctx context.Context, req routing.ChatRequest, decision *routing.RoutingDecision) (iter.Seq2[routing.Chunk, error], error) {
	isLocal := decision.Strategy == routing.QuickLocal || decision.Strategy == routing.DeepLocal

	slog.Debug("dispatching request",
		"model", req.Model,
		"strategy", decision.Strategy,
		"provider", decision.Provider,
		"stream", req.Stream,
		"messages", len(req.Messages),
	)

	if isLocal && d.local != nil {
		// Check model state for instant cloud fallback
		if d.manager != nil {
			state := d.manager.State()
			if state == routing.StateStopped {
				// Model not running - start in background, route to cloud immediately
				slog.Info("model cold, routing to cloud and starting model in background")
				slog.Info("sending request to cloud", "reason", "model cold")
				d.manager.StartInBackground(ctx)
				if d.cloud != nil {
					decision.IsFallback = true
					decision.ActualProvider = routing.ProviderCloud
					return d.cloud.Stream(ctx, req)
				}
				return nil, fmt.Errorf("model is stopped and no cloud provider available")
			}
			if state == routing.StateStarting {
				// Model is starting - route to cloud to avoid waiting
				slog.Info("model starting, routing to cloud")
				if d.cloud != nil {
					slog.Info("sending request to cloud", "reason", "model starting")
					decision.IsFallback = true
					decision.ActualProvider = routing.ProviderCloud
					return d.cloud.Stream(ctx, req)
				}
				// No cloud available - wait for model
				if err := d.manager.Start(ctx); err != nil {
					return nil, fmt.Errorf("model starting and no cloud provider: %w", err)
				}
			}
			if state == routing.StateReady {
				d.manager.RequestBegin()
				defer d.manager.RequestEnd()
			}
		}

		decision.ActualProvider = routing.ProviderLocal
		stream, err := d.local.Stream(ctx, req)
		if err != nil {
			slog.Warn("local provider failed", "err", err)
			if d.cloud != nil && d.isPreFirstChunkError(err) {
				slog.Info("falling back to cloud", "reason", "pre-first-chunk local failure")
				slog.Info("sending request to cloud", "reason", "pre-first-chunk local failure")
				decision.IsFallback = true
				decision.ActualProvider = routing.ProviderCloud
				return d.cloud.Stream(ctx, req)
			}
			return nil, err
		}
		return d.wrapLocalWithFallback(ctx, req, stream, decision), nil
	}

	if d.cloud != nil {
		slog.Debug("routing to cloud provider")
		slog.Info("sending request to cloud", "reason", "cloud strategy")
		decision.ActualProvider = routing.ProviderCloud
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
	decision *routing.RoutingDecision,
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
					decision.ActualProvider = routing.ProviderCloud
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
