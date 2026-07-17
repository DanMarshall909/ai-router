package routing

import (
	"context"
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
	Model    string
	Messages []Message
	Stream   bool
}

// Message represents a single chat message.
type Message struct {
	Role    string
	Content string
}

// Chunk is a single streaming response chunk.
type Chunk struct {
	Content string
}
