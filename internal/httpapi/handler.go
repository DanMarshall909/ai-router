package httpapi

import (
	"encoding/json"
	"fmt"
	"io"
	"iter"
	"net/http"
	"strings"

	"github.com/DanMarshall909/ai-router/internal/routing"
)

// Handler exposes the OpenAI-compatible HTTP API.
type Handler struct {
	dispatcher *Dispatcher
}

// NewHandler creates an API handler.
func NewHandler(d *Dispatcher) *Handler {
	return &Handler{dispatcher: d}
}

// RegisterRoutes wires the endpoints to the mux.
func (h *Handler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /v1/chat/completions", h.handleChatCompletions)
}

// ChatCompletionRequest is the OpenAI-compatible request shape.
type ChatCompletionRequest struct {
	Model    string    `json:"model"`
	Messages []Message `json:"messages"`
	Stream   *bool     `json:"stream,omitempty"`
}

// Message is a single chat message.
type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

func (h *Handler) handleChatCompletions(w http.ResponseWriter, r *http.Request) {
	var req ChatCompletionRequest
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, `{"error":"failed to read body"}`, http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	if err := json.Unmarshal(body, &req); err != nil {
		http.Error(w, `{"error":"invalid JSON"}`, http.StatusBadRequest)
		return
	}

	if len(req.Messages) == 0 {
		http.Error(w, `{"error":"messages array must not be empty"}`, http.StatusBadRequest)
		return
	}

	for _, m := range req.Messages {
		if m.Role != "system" && m.Role != "user" && m.Role != "assistant" {
			http.Error(w, fmt.Sprintf(`{"error":"unknown role %q"}`, m.Role), http.StatusBadRequest)
			return
		}
	}

	stream := req.Stream != nil && *req.Stream

	// Build routing request
	routingReq := routing.ChatRequest{
		Model: req.Model,
		Messages: make([]routing.Message, len(req.Messages)),
		Stream:   stream,
	}
	for i, m := range req.Messages {
		routingReq.Messages[i] = routing.Message{Role: m.Role, Content: m.Content}
	}

	// For the POC, use a simple decision based on model
	decision := routing.RoutingDecision{
		Strategy: routing.QuickLocal,
		Provider: "local",
		Model:    "bonsai",
		Reason:   "default routing",
	}
	if req.Model != "auto" && req.Model != "" {
		decision.Strategy = routing.CloudGeneral
		decision.Provider = "openrouter"
		decision.Model = req.Model
	}

	ctx := r.Context()
	streamIter, err := h.dispatcher.Dispatch(ctx, routingReq, decision)
	if err != nil {
		http.Error(w, fmt.Sprintf(`{"error":"%s"}`, escapeJSON(err.Error())), http.StatusServiceUnavailable)
		return
	}

	if stream {
		h.writeStreamingResponse(w, streamIter)
	} else {
		h.writeNonStreamingResponse(w, streamIter)
	}
}

func (h *Handler) writeStreamingResponse(w http.ResponseWriter, stream iter.Seq2[routing.Chunk, error]) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}

	id := "chatcmpl-poc"
	for chunk, err := range stream {
		if err != nil {
			errJSON, _ := json.Marshal(map[string]string{"error": err.Error()})
			fmt.Fprintf(w, "data: %s\n\n", errJSON)
			flusher.Flush()
			return
		}

		resp := map[string]any{
			"id":    id,
			"object": "chat.completion.chunk",
			"choices": []map[string]any{
				{
					"delta": map[string]any{"content": chunk.Content},
				},
			},
		}
		data, _ := json.Marshal(resp)
		fmt.Fprintf(w, "data: %s\n\n", data)
		flusher.Flush()
	}

	fmt.Fprintf(w, "data: [DONE]\n\n")
	flusher.Flush()
}

func (h *Handler) writeNonStreamingResponse(w http.ResponseWriter, stream iter.Seq2[routing.Chunk, error]) {
	var content strings.Builder
	for chunk, err := range stream {
		if err != nil {
			http.Error(w, fmt.Sprintf(`{"error":"%s"}`, escapeJSON(err.Error())), http.StatusInternalServerError)
			return
		}
		content.WriteString(chunk.Content)
	}

	resp := map[string]any{
		"id":    "chatcmpl-poc",
		"object": "chat.completion",
		"choices": []map[string]any{
			{
				"message": map[string]string{
					"role":    "assistant",
					"content": content.String(),
				},
				"finish_reason": "stop",
			},
		},
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

func escapeJSON(s string) string {
	b, _ := json.Marshal(s)
	return string(b[1 : len(b)-1])
}
