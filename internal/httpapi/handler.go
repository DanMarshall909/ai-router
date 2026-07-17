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
	traceLogger         *TraceLogger
}

// NewHandler creates an API handler.
func NewHandler(d *Dispatcher, mgr *local.Manager, localSelfAssessment bool, traceLogger *TraceLogger) *Handler {
	return &Handler{
		dispatcher:          d,
		manager:             mgr,
		localSelfAssessment: localSelfAssessment,
		traceLogger:         traceLogger,
	}
}

// RegisterRoutes wires the endpoints to the mux.
func (h *Handler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /v1/chat/completions", h.handleChatCompletions)
	mux.HandleFunc("POST /api/router/local-model/stop", h.handleStopModel)
	mux.HandleFunc("POST /api/router/local-model/start", h.handleStartModel)
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

	for _, m := range req.Messages {
		if m.Role != "system" && m.Role != "user" && m.Role != "assistant" {
			slog.Warn("unknown role", "role", m.Role)
			http.Error(w, fmt.Sprintf(`{"error":"unknown role %q"}`, m.Role), http.StatusBadRequest)
			return
		}
	}

	stream := req.Stream != nil && *req.Stream
	ctx := r.Context()

	// Build routing request
	routingReq := routing.ChatRequest{
		Model:    req.Model,
		Messages: make([]routing.Message, len(req.Messages)),
		Stream:   stream,
	}
	for i, m := range req.Messages {
		routingReq.Messages[i] = routing.Message{Role: m.Role, Content: m.Content}
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
	if req.Model != "auto" && req.Model != "" {
		decision.Strategy = routing.CloudGeneral
		decision.Provider = routing.ProviderCloud
		decision.Model = req.Model
	} else if h.localSelfAssessment {
		if strategy, localResponse, assessed := h.dispatcher.Assess(ctx, routingReq); assessed {
			decision.Strategy = strategy
			if strategy != routing.QuickLocal {
				decision.Provider = routing.ProviderCloud
				decision.Model = "auto"
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
	var provider, model string
	var content strings.Builder
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
		content.WriteString(chunk.Content)

		resp := map[string]any{
			"id":     id,
			"object": "chat.completion.chunk",
			"debug":  map[string]any{"provider": provider, "model": model},
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
	slog.Info("response served", "provider", provider, "model", model, "stream", true, "duration", time.Since(started))
	return content.String(), nil
}

func (h *Handler) writeNonStreamingResponse(w http.ResponseWriter, stream iter.Seq2[routing.Chunk, error], started time.Time) (string, error) {
	var content strings.Builder
	var provider, model string
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
	}

	resp := map[string]any{
		"id":     "chatcmpl-poc",
		"object": "chat.completion",
		"debug":  map[string]any{"provider": provider, "model": model},
		"choices": []map[string]any{
			{
				"message": map[string]string{
					"role":    "assistant",
					"content": content.String(),
				},
				"finish_reason": "stop",
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
