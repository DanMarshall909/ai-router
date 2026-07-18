package httpapi

import (
	"encoding/json"
	"fmt"
	"io"
	"iter"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/DanMarshall909/ai-router/internal/local"
	"github.com/DanMarshall909/ai-router/internal/routing"
)

// Handler exposes the OpenAI-compatible HTTP API.
type Handler struct {
	dispatcher          *Dispatcher
	manager             *local.Manager
	localSelfAssessment bool
	complexityThreshold float64
	traceLogger         *TraceLogger
}

// NewHandler creates an API handler.
func NewHandler(d *Dispatcher, mgr *local.Manager, localSelfAssessment bool, complexityThreshold float64, traceLogger *TraceLogger) *Handler {
	return &Handler{
		dispatcher:          d,
		manager:             mgr,
		localSelfAssessment: localSelfAssessment,
		complexityThreshold: complexityThreshold,
		traceLogger:         traceLogger,
	}
}

// RegisterRoutes wires the endpoints to the mux.
func (h *Handler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /v1/models", h.handleModels)
	mux.HandleFunc("POST /v1/chat/completions", h.handleChatCompletions)
	mux.HandleFunc("POST /api/router/local-model/stop", h.handleStopModel)
	mux.HandleFunc("POST /api/router/local-model/start", h.handleStartModel)
}

func (h *Handler) handleModels(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"object": "list",
		"data": []map[string]any{
			{
				"id":       routing.AutoModelName,
				"object":   "model",
				"created":  0,
				"owned_by": "ai-router",
			},
		},
	})
}

// ChatCompletionRequest is the OpenAI-compatible request shape.
type ChatCompletionRequest struct {
	Model             string          `json:"model"`
	Messages          []Message       `json:"messages"`
	Tools             json.RawMessage `json:"tools,omitempty"`
	ToolChoice        json.RawMessage `json:"tool_choice,omitempty"`
	ParallelToolCalls *bool           `json:"parallel_tool_calls,omitempty"`
	Stream            *bool           `json:"stream,omitempty"`
	Complexity        *float64        `json:"complexity,omitempty"`
}

// Message is a single chat message.
type Message struct {
	Role       string          `json:"role"`
	Content    string          `json:"content"`
	ToolCallID string          `json:"tool_call_id,omitempty"`
	ToolCalls  json.RawMessage `json:"tool_calls,omitempty"`
}

func (h *Handler) handleChatCompletions(w http.ResponseWriter, r *http.Request) {
	started := time.Now()
	var req ChatCompletionRequest
	body, err := io.ReadAll(r.Body)
	if err != nil {
		slog.Warn("failed to read request body", "err", err)
		http.Error(w, `{"error":"failed to read body"}`, http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	if err := json.Unmarshal(body, &req); err != nil {
		slog.Warn("invalid JSON", "err", err)
		http.Error(w, `{"error":"invalid JSON"}`, http.StatusBadRequest)
		return
	}

	if len(req.Messages) == 0 {
		slog.Warn("empty messages array")
		http.Error(w, `{"error":"messages array must not be empty"}`, http.StatusBadRequest)
		return
	}
	if req.Complexity != nil && (*req.Complexity < 0 || *req.Complexity > 1) {
		slog.Warn("invalid complexity hint", "complexity", *req.Complexity)
		http.Error(w, `{"error":"complexity must be between 0 and 1"}`, http.StatusBadRequest)
		return
	}

	for _, m := range req.Messages {
		if !routing.IsValidMessageRole(m.Role) {
			slog.Warn("unknown role", "role", m.Role)
			http.Error(w, fmt.Sprintf(`{"error":"unknown role %q"}`, m.Role), http.StatusBadRequest)
			return
		}
	}

	stream := req.Stream != nil && *req.Stream
	ctx := r.Context()

	// Build routing request
	routingReq := routing.ChatRequest{
		Model:             req.Model,
		Messages:          make([]routing.Message, len(req.Messages)),
		Tools:             req.Tools,
		ToolChoice:        req.ToolChoice,
		ParallelToolCalls: req.ParallelToolCalls,
		Stream:            stream,
	}
	for i, m := range req.Messages {
		routingReq.Messages[i] = routing.Message{Role: m.Role, Content: m.Content, ToolCallID: m.ToolCallID, ToolCalls: m.ToolCalls}
	}
	slog.Info("request received",
		"model", req.Model,
		"stream", stream,
		"messages", len(req.Messages),
	)

	// Explicit models always use cloud. Automatic requests can be assessed locally.
	decision := routing.RoutingDecision{
		Strategy: routing.QuickLocal,
		Provider: routing.ProviderLocal,
		Model:    localModelName,
		Reason:   "default routing",
	}
	var assessedResponse iter.Seq2[routing.Chunk, error]
	if req.Model != routing.AutoModelName && req.Model != "" {
		decision.Strategy = routing.CloudGeneral
		decision.Provider = routing.ProviderCloud
		decision.Model = req.Model
	} else if req.Complexity != nil && *req.Complexity >= h.complexityThreshold {
		decision.Strategy = routing.CloudReasoning
		decision.Provider = routing.ProviderCloud
		decision.Model = routing.AutoModelName
		decision.Reason = "complexity hint meets threshold"
	} else if h.localSelfAssessment {
		if strategy, localResponse, assessed := h.dispatcher.Assess(ctx, routingReq); assessed {
			decision.Strategy = strategy
			if strategy != routing.QuickLocal {
				decision.Provider = routing.ProviderCloud
				decision.Model = routing.AutoModelName
			} else if localResponse != nil {
				assessedResponse = localResponse
			}
		}
	}

	slog.Info("routing decision",
		"strategy", decision.Strategy,
		"provider", decision.Provider,
		"model", decision.Model,
		"stream", stream,
	)
	if assessedResponse != nil {
		slog.Info("request served by local self-assessment", "stream", stream)
		var response string
		var responseErr error
		if stream {
			response, responseErr = h.writeStreamingResponse(w, assessedResponse, started)
		} else {
			response, responseErr = h.writeNonStreamingResponse(w, assessedResponse, started)
		}
		h.writeTrace(req, decision, response, responseErr, started)
		return
	}

	streamIter, err := h.dispatcher.Dispatch(ctx, routingReq, &decision)
	if err != nil {
		slog.Error("dispatch failed", "err", err)
		http.Error(w, fmt.Sprintf(`{"error":"%s"}`, escapeJSON(err.Error())), http.StatusServiceUnavailable)
		h.writeTrace(req, decision, "", err, started)
		return
	}

	var response string
	var responseErr error
	if stream {
		response, responseErr = h.writeStreamingResponse(w, streamIter, started)
	} else {
		response, responseErr = h.writeNonStreamingResponse(w, streamIter, started)
	}
	h.writeTrace(req, decision, response, responseErr, started)
}

func (h *Handler) writeStreamingResponse(w http.ResponseWriter, stream iter.Seq2[routing.Chunk, error], started time.Time) (string, error) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	flusher, ok := w.(http.Flusher)
	if !ok {
		err := fmt.Errorf("streaming not supported")
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return "", err
	}

	id := "chatcmpl-poc"
	created := started.Unix()
	var provider, model string
	var content strings.Builder
	firstChunk := true
	finishReason := "stop"
	for chunk, err := range stream {
		if err != nil {
			errJSON, _ := json.Marshal(map[string]string{"error": err.Error()})
			fmt.Fprintf(w, "data: %s\n\n", errJSON)
			flusher.Flush()
			return content.String(), err
		}
		if provider == "" {
			provider = chunk.Provider
		}
		if model == "" {
			model = chunk.Model
		}
		if model == "" {
			model = routing.AutoModelName
		}
		content.WriteString(chunk.Content)
		if chunk.FinishReason != "" {
			finishReason = chunk.FinishReason
		}

		delta := make(map[string]any)
		if chunk.Content != "" {
			delta["content"] = chunk.Content
		}
		if len(chunk.ToolCalls) > 0 {
			delta["tool_calls"] = chunk.ToolCalls
		}
		if firstChunk {
			delta["role"] = "assistant"
			firstChunk = false
		}

		resp := map[string]any{
			"id":      id,
			"object":  "chat.completion.chunk",
			"created": created,
			"model":   model,
			"debug":   map[string]any{"provider": provider, "model": model},
			"choices": []map[string]any{
				{
					"index": 0,
					"delta": delta,
				},
			},
		}
		data, _ := json.Marshal(resp)
		fmt.Fprintf(w, "data: %s\n\n", data)
		flusher.Flush()
	}

	completed := map[string]any{
		"id":      id,
		"object":  "chat.completion.chunk",
		"created": created,
		"model":   model,
		"choices": []map[string]any{
			{
				"index":         0,
				"delta":         map[string]any{},
				"finish_reason": finishReason,
			},
		},
	}
	completedData, _ := json.Marshal(completed)
	fmt.Fprintf(w, "data: %s\n\n", completedData)
	flusher.Flush()
	fmt.Fprintf(w, "data: [DONE]\n\n")
	flusher.Flush()
	slog.Info("response served", "provider", provider, "model", model, "stream", true, "duration", time.Since(started))
	return content.String(), nil
}

func (h *Handler) writeNonStreamingResponse(w http.ResponseWriter, stream iter.Seq2[routing.Chunk, error], started time.Time) (string, error) {
	var content strings.Builder
	var provider, model string
	toolCalls := make(map[int]toolCall)
	finishReason := "stop"
	for chunk, err := range stream {
		if err != nil {
			http.Error(w, fmt.Sprintf(`{"error":"%s"}`, escapeJSON(err.Error())), http.StatusInternalServerError)
			return content.String(), err
		}
		if provider == "" {
			provider = chunk.Provider
		}
		if model == "" {
			model = chunk.Model
		}
		content.WriteString(chunk.Content)
		if chunk.FinishReason != "" {
			finishReason = chunk.FinishReason
		}
		if err := mergeToolCalls(toolCalls, chunk.ToolCalls); err != nil {
			http.Error(w, fmt.Sprintf(`{"error":"%s"}`, escapeJSON(err.Error())), http.StatusInternalServerError)
			return content.String(), err
		}
	}

	message := map[string]any{"role": "assistant", "content": content.String()}
	if len(toolCalls) > 0 {
		message["tool_calls"] = orderedToolCalls(toolCalls)
	}
	if model == "" {
		model = routing.AutoModelName
	}

	resp := map[string]any{
		"id":      "chatcmpl-poc",
		"object":  "chat.completion",
		"created": started.Unix(),
		"debug":   map[string]any{"provider": provider, "model": model},
		"choices": []map[string]any{
			{
				"index":         0,
				"message":       message,
				"finish_reason": finishReason,
			},
		},
		"model": model,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
	slog.Info("response served",
		"provider", provider,
		"model", model,
		"stream", false,
		"duration", time.Since(started),
	)
	return content.String(), nil
}

type toolCall struct {
	Index    int    `json:"-"`
	ID       string `json:"id,omitempty"`
	Type     string `json:"type,omitempty"`
	Function struct {
		Name      string `json:"name,omitempty"`
		Arguments string `json:"arguments,omitempty"`
	} `json:"function"`
}

func mergeToolCalls(toolCalls map[int]toolCall, raw json.RawMessage) error {
	if len(raw) == 0 {
		return nil
	}
	var deltas []toolCall
	if err := json.Unmarshal(raw, &deltas); err != nil {
		return fmt.Errorf("invalid tool call response: %w", err)
	}
	for _, delta := range deltas {
		call := toolCalls[delta.Index]
		call.Index = delta.Index
		if delta.ID != "" {
			call.ID = delta.ID
		}
		if delta.Type != "" {
			call.Type = delta.Type
		}
		if delta.Function.Name != "" {
			call.Function.Name += delta.Function.Name
		}
		call.Function.Arguments += delta.Function.Arguments
		toolCalls[delta.Index] = call
	}
	return nil
}

func orderedToolCalls(toolCalls map[int]toolCall) []toolCall {
	ordered := make([]toolCall, len(toolCalls))
	for index, call := range toolCalls {
		ordered[index] = call
	}
	return ordered
}

func (h *Handler) writeTrace(req ChatCompletionRequest, decision routing.RoutingDecision, response string, responseErr error, started time.Time) {
	if h.traceLogger == nil {
		return
	}
	if err := h.traceLogger.Write(req, decision, response, responseErr, started); err != nil {
		slog.Warn("failed to write debug trace", "err", err)
	}
}

func escapeJSON(s string) string {
	b, _ := json.Marshal(s)
	return string(b[1 : len(b)-1])
}

func (h *Handler) handleStopModel(w http.ResponseWriter, r *http.Request) {
	if err := h.manager.Stop(r.Context()); err != nil {
		http.Error(w, fmt.Sprintf("failed to stop model: %v", err), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "stopped"})
}

func (h *Handler) handleStartModel(w http.ResponseWriter, r *http.Request) {
	h.manager.StartInBackground(r.Context())
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "starting"})
}
