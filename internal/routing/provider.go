package routing

import (
	"context"
	"encoding/json"
	"iter"
)

// ChatProvider streams chat completion chunks from a model.
type ChatProvider interface {
	// Stream sends a chat request and returns an iterator of chunks.
	// The outer error covers failures to establish the stream;
	// the per-item error covers failures during generation.
	Stream(ctx context.Context, req ChatRequest) (iter.Seq2[Chunk, error], error)
}

// ChatRequest is an OpenAI-compatible chat completion request.
type ChatRequest struct {
	Model             string
	Messages          []Message
	Tools             json.RawMessage
	ToolChoice        json.RawMessage
	ParallelToolCalls *bool
	EnableThinking    *bool
	Stream            bool
	SessionID         string
	Fallbacks         []string
}

// Message represents a single chat message.
type Message struct {
	Role       string
	Content    string
	ToolCallID string
	ToolCalls  json.RawMessage
}

// Chunk is a single streaming response chunk.
type Chunk struct {
	Content      string
	Reasoning    string
	ToolCalls    json.RawMessage
	FinishReason string
	Provider     string
	Model        string
}
