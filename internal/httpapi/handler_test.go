package httpapi_test

import (
	"context"
	"encoding/json"
	"io"
	"iter"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DanMarshall909/ai-router/internal/httpapi"
	"github.com/DanMarshall909/ai-router/internal/routing"
	"github.com/stretchr/testify/require"
)

type fakeProvider struct {
	chunks []routing.Chunk
	err    error
}

func (f *fakeProvider) Stream(ctx context.Context, req routing.ChatRequest) (iter.Seq2[routing.Chunk, error], error) {
	if f.err != nil {
		return nil, f.err
	}
	return func(yield func(routing.Chunk, error) bool) {
		for _, c := range f.chunks {
			if !yield(c, nil) {
				return
			}
		}
	}, nil
}

func setupHandler(t *testing.T, localProvider, cloudProvider routing.ChatProvider) *httptest.Server {
	t.Helper()
	d := httpapi.NewDispatcher(localProvider, cloudProvider, nil)
	h := httpapi.NewHandler(d)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)
	return httptest.NewServer(mux)
}

func TestChatCompletionsNonStreaming(t *testing.T) {
	local := &fakeProvider{chunks: []routing.Chunk{
		{Content: "Hello"}, {Content: " "}, {Content: "world"},
	}}
	srv := setupHandler(t, local, nil)
	defer srv.Close()

	body := `{"model":"auto","messages":[{"role":"user","content":"hi"}]}`
	resp, err := http.Post(srv.URL+"/v1/chat/completions", "application/json", strings.NewReader(body))
	require.NoError(t, err)
	defer resp.Body.Close()

	require.Equal(t, http.StatusOK, resp.StatusCode)

	var result map[string]any
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&result))
	require.Equal(t, "chat.completion", result["object"])

	choices := result["choices"].([]any)
	choice := choices[0].(map[string]any)
	msg := choice["message"].(map[string]any)
	require.Equal(t, "Hello world", msg["content"])
	require.Equal(t, "assistant", msg["role"])
}

func TestChatCompletionsStreaming(t *testing.T) {
	local := &fakeProvider{chunks: []routing.Chunk{
		{Content: "Hi"}, {Content: " there"},
	}}
	srv := setupHandler(t, local, nil)
	defer srv.Close()

	body := `{"model":"auto","messages":[{"role":"user","content":"hi"}],"stream":true}`
	resp, err := http.Post(srv.URL+"/v1/chat/completions", "application/json", strings.NewReader(body))
	require.NoError(t, err)
	defer resp.Body.Close()

	require.Equal(t, http.StatusOK, resp.StatusCode)
	require.Equal(t, "text/event-stream", resp.Header.Get("Content-Type"))

	data, _ := io.ReadAll(resp.Body)
	lines := strings.Split(string(data), "\n")

	var content strings.Builder
	for _, line := range lines {
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		payload := strings.TrimPrefix(line, "data: ")
		if payload == "[DONE]" {
			break
		}
		var chunk map[string]any
		require.NoError(t, json.Unmarshal([]byte(payload), &chunk))
		choices := chunk["choices"].([]any)
		delta := choices[0].(map[string]any)["delta"].(map[string]any)
		if c, ok := delta["content"].(string); ok {
			content.WriteString(c)
		}
	}

	require.Equal(t, "Hi there", content.String())
}

func TestChatCompletionsRejectsEmptyMessages(t *testing.T) {
	srv := setupHandler(t, &fakeProvider{}, nil)
	defer srv.Close()

	body := `{"model":"auto","messages":[]}`
	resp, err := http.Post(srv.URL+"/v1/chat/completions", "application/json", strings.NewReader(body))
	require.NoError(t, err)
	defer resp.Body.Close()

	require.Equal(t, http.StatusBadRequest, resp.StatusCode)
}

func TestChatCompletionsRejectsUnknownRole(t *testing.T) {
	srv := setupHandler(t, &fakeProvider{}, nil)
	defer srv.Close()

	body := `{"model":"auto","messages":[{"role":"tool","content":"hi"}]}`
	resp, err := http.Post(srv.URL+"/v1/chat/completions", "application/json", strings.NewReader(body))
	require.NoError(t, err)
	defer resp.Body.Close()

	require.Equal(t, http.StatusBadRequest, resp.StatusCode)
}

func TestChatCompletionsRejectsInvalidJSON(t *testing.T) {
	srv := setupHandler(t, &fakeProvider{}, nil)
	defer srv.Close()

	resp, err := http.Post(srv.URL+"/v1/chat/completions", "application/json", strings.NewReader("{bad"))
	require.NoError(t, err)
	defer resp.Body.Close()

	require.Equal(t, http.StatusBadRequest, resp.StatusCode)
}

func TestCloudModelRoutesToCloudProvider(t *testing.T) {
	cloud := &fakeProvider{chunks: []routing.Chunk{{Content: "cloud response"}}}
	srv := setupHandler(t, nil, cloud)
	defer srv.Close()

	body := `{"model":"anthropic/claude-3.5-sonnet","messages":[{"role":"user","content":"hi"}]}`
	resp, err := http.Post(srv.URL+"/v1/chat/completions", "application/json", strings.NewReader(body))
	require.NoError(t, err)
	defer resp.Body.Close()

	require.Equal(t, http.StatusOK, resp.StatusCode)

	var result map[string]any
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&result))
	choices := result["choices"].([]any)
	choice := choices[0].(map[string]any)
	msg := choice["message"].(map[string]any)
	require.Equal(t, "cloud response", msg["content"])
}
