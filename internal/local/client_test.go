package local_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/DanMarshall909/ai-router/internal/local"
	"github.com/DanMarshall909/ai-router/internal/routing"
	"github.com/stretchr/testify/require"
)

func TestLocalClientSuccessfulStream(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/v1/chat/completions", r.URL.Path, "because request should go to the chat completions endpoint")
		require.Equal(t, http.MethodPost, r.Method)

		w.Header().Set("Content-Type", "text/event-stream")
		flusher, ok := w.(http.Flusher)
		require.True(t, ok, "because response writer must support flushing")

		chunks := []string{"Hello", " ", "world"}
		for _, c := range chunks {
			resp := map[string]any{
				"choices": []map[string]any{
					{"delta": map[string]any{"content": c}},
				},
			}
			data, _ := json.Marshal(resp)
			fmt.Fprintf(w, "data: %s\n\n", data)
			flusher.Flush()
		}
		fmt.Fprintf(w, "data: [DONE]\n\n")
		flusher.Flush()
	}))
	defer srv.Close()

	client := local.NewLocalClient(srv.URL, 5*time.Second)
	req := routing.ChatRequest{
		Model: "test",
		Messages: []routing.Message{
			{Role: "user", Content: "hi"},
		},
	}

	var collected []string
	stream, err := client.Stream(context.Background(), req)
	require.NoError(t, err)

	for chunk, err := range stream {
		require.NoError(t, err, "because streaming should succeed")
		collected = append(collected, chunk.Content)
	}

	require.Equal(t, "Hello world", strings.Join(collected, ""), "because all chunks should be collected")
}

func TestLocalClientConnectionFailure(t *testing.T) {
	client := local.NewLocalClient("http://127.0.0.1:1", 1*time.Second)
	req := routing.ChatRequest{
		Model:    "test",
		Messages: []routing.Message{{Role: "user", Content: "hi"}},
	}

	_, err := client.Stream(context.Background(), req)
	require.Error(t, err, "because connecting to a dead server should fail")

	var outcome local.FailureOutcome
	require.ErrorAs(t, err, &outcome, "because error should be a FailureOutcome")
	require.Equal(t, local.FailureBeforeFirstChunk, outcome.Kind, "because connection failure is before first chunk")
}

func TestLocalClientTimeout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(2 * time.Second)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	client := local.NewLocalClient(srv.URL, 100*time.Millisecond)
	req := routing.ChatRequest{
		Model:    "test",
		Messages: []routing.Message{{Role: "user", Content: "hi"}},
	}

	_, err := client.Stream(context.Background(), req)
	require.Error(t, err, "because slow server should cause timeout")
}

func TestLocalClientInvalidResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("internal error"))
	}))
	defer srv.Close()

	client := local.NewLocalClient(srv.URL, 5*time.Second)
	req := routing.ChatRequest{
		Model:    "test",
		Messages: []routing.Message{{Role: "user", Content: "hi"}},
	}

	_, err := client.Stream(context.Background(), req)
	require.Error(t, err, "because non-200 status should fail")

	var outcome local.FailureOutcome
	require.ErrorAs(t, err, &outcome, "because error should be a FailureOutcome")
	require.Equal(t, local.FailureBeforeFirstChunk, outcome.Kind)
}

func TestLocalClientMalformedSSE(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprintf(w, "data: {not json\n\n")
	}))
	defer srv.Close()

	client := local.NewLocalClient(srv.URL, 5*time.Second)
	req := routing.ChatRequest{
		Model:    "test",
		Messages: []routing.Message{{Role: "user", Content: "hi"}},
	}

	stream, err := client.Stream(context.Background(), req)
	require.NoError(t, err, "because stream should open successfully")

	for _, err := range stream {
		require.Error(t, err, "because malformed SSE should produce an error")
		return
	}
}

func TestLocalClientFailurePosition(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher := w.(http.Flusher)

		// Send one chunk then break the connection
		resp := map[string]any{
			"choices": []map[string]any{
				{"delta": map[string]any{"content": "hello"}},
			},
		}
		data, _ := json.Marshal(resp)
		fmt.Fprintf(w, "data: %s\n\n", data)
		flusher.Flush()

		hj, ok := w.(http.Hijacker)
		if ok {
			conn, _, _ := hj.Hijack()
			conn.Close()
		}
	}))
	defer srv.Close()

	client := local.NewLocalClient(srv.URL, 5*time.Second)
	req := routing.ChatRequest{
		Model:    "test",
		Messages: []routing.Message{{Role: "user", Content: "hi"}},
	}

	stream, err := client.Stream(context.Background(), req)
	require.NoError(t, err)

	gotFirst := false
	for chunk, err := range stream {
		if err != nil {
			var outcome local.FailureOutcome
			require.ErrorAs(t, err, &outcome)
			if gotFirst {
				require.Equal(t, local.FailureAfterFirstChunk, outcome.Kind, "because failure after first chunk should be classified correctly")
			}
			return
		}
		if chunk.Content != "" {
			gotFirst = true
		}
	}
}

func TestLocalClientCancellationPropagates(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher := w.(http.Flusher)

		for i := 0; i < 100; i++ {
			resp := map[string]any{
				"choices": []map[string]any{
					{"delta": map[string]any{"content": fmt.Sprintf("chunk-%d", i)}},
				},
			}
			data, _ := json.Marshal(resp)
			fmt.Fprintf(w, "data: %s\n\n", data)
			flusher.Flush()
			time.Sleep(50 * time.Millisecond)
		}
	}))
	defer srv.Close()

	client := local.NewLocalClient(srv.URL, 5*time.Second)
	req := routing.ChatRequest{
		Model:    "test",
		Messages: []routing.Message{{Role: "user", Content: "hi"}},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()

	stream, err := client.Stream(ctx, req)
	require.NoError(t, err)

	count := 0
	for _, err := range stream {
		if err != nil {
			require.ErrorIs(t, err, context.DeadlineExceeded, "because cancellation should propagate through the error chain")
			return
		}
		count++
	}
}
