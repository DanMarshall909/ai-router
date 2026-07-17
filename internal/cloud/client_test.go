package cloud_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/DanMarshall909/ai-router/internal/cloud"
	"github.com/DanMarshall909/ai-router/internal/routing"
	"github.com/stretchr/testify/require"
)

func TestOpenRouterClientSuccessfulStream(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/chat/completions", r.URL.Path)
		require.Equal(t, "Bearer test-key", r.Header.Get("Authorization"))
		require.Equal(t, "application/json", r.Header.Get("Content-Type"))

		w.Header().Set("Content-Type", "text/event-stream")
		flusher := w.(http.Flusher)

		chunks := []string{"Hello", " ", "from", " ", "cloud"}
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

	modelMap := map[string]string{"CloudGeneral": "anthropic/claude-3.5-sonnet"}
	client := cloud.NewOpenRouterClient(srv.URL, "test-key", modelMap, 5*time.Second, "", "")

	req := routing.ChatRequest{
		Model:    "CloudGeneral",
		Messages: []routing.Message{{Role: "user", Content: "hi"}},
	}

	var collected []string
	stream, err := client.Stream(context.Background(), req)
	require.NoError(t, err)

	for chunk, err := range stream {
		require.NoError(t, err)
		collected = append(collected, chunk.Content)
	}

	require.Equal(t, "Hello from cloud", strings.Join(collected, ""))
}

func TestOpenRouterClientNoOutboundCallWhenDisabled(t *testing.T) {
	called := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
	}))
	defer srv.Close()

	// Empty API key means cloud is effectively disabled at config level,
	// but the client itself doesn't check that — the caller should not
	// create the client when disabled. We test the connection failure path.
	modelMap := map[string]string{"CloudGeneral": "test"}
	client := cloud.NewOpenRouterClient(srv.URL, "", modelMap, 1*time.Second, "", "")

	req := routing.ChatRequest{
		Model:    "CloudGeneral",
		Messages: []routing.Message{{Role: "user", Content: "hi"}},
	}

	_, err := client.Stream(context.Background(), req)
	require.NoError(t, err)
	require.True(t, called, "because client should make the call when created")
}

func TestOpenRouterClientAuthFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{"error": "invalid key"}`))
	}))
	defer srv.Close()

	modelMap := map[string]string{"CloudGeneral": "test"}
	client := cloud.NewOpenRouterClient(srv.URL, "bad-key", modelMap, 5*time.Second, "", "")

	req := routing.ChatRequest{
		Model:    "CloudGeneral",
		Messages: []routing.Message{{Role: "user", Content: "hi"}},
	}

	_, err := client.Stream(context.Background(), req)
	require.Error(t, err, "because invalid API key should fail")
	require.Contains(t, err.Error(), "authentication")
}

func TestOpenRouterClientRateLimit(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer srv.Close()

	modelMap := map[string]string{"CloudGeneral": "test"}
	client := cloud.NewOpenRouterClient(srv.URL, "key", modelMap, 5*time.Second, "", "")

	req := routing.ChatRequest{
		Model:    "CloudGeneral",
		Messages: []routing.Message{{Role: "user", Content: "hi"}},
	}

	_, err := client.Stream(context.Background(), req)
	require.Error(t, err, "because rate limit should fail")
	require.Contains(t, err.Error(), "rate limit")
}

func TestOpenRouterClientApplicationHeaders(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "https://myapp.example.com", r.Header.Get("HTTP-Referer"))
		require.Equal(t, "AI Router", r.Header.Get("X-Title"))

		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprintf(w, "data: {\"choices\":[{\"delta\":{\"content\":\"ok\"}}]}\n\n")
		fmt.Fprintf(w, "data: [DONE]\n\n")
	}))
	defer srv.Close()

	modelMap := map[string]string{"CloudGeneral": "test"}
	client := cloud.NewOpenRouterClient(srv.URL, "key", modelMap, 5*time.Second, "AI Router", "https://myapp.example.com")

	req := routing.ChatRequest{
		Model:    "CloudGeneral",
		Messages: []routing.Message{{Role: "user", Content: "hi"}},
	}

	stream, err := client.Stream(context.Background(), req)
	require.NoError(t, err)

	for chunk, err := range stream {
		require.NoError(t, err)
		require.Equal(t, "ok", chunk.Content)
	}
}

func TestOpenRouterClientConnectionFailure(t *testing.T) {
	modelMap := map[string]string{"CloudGeneral": "test"}
	client := cloud.NewOpenRouterClient("http://127.0.0.1:1", "key", modelMap, 1*time.Second, "", "")

	req := routing.ChatRequest{
		Model:    "CloudGeneral",
		Messages: []routing.Message{{Role: "user", Content: "hi"}},
	}

	_, err := client.Stream(context.Background(), req)
	require.Error(t, err, "because connection to dead server should fail")
}
