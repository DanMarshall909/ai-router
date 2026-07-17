package httpapi_test

import (
	"context"
	"encoding/json"
	"io"
	"iter"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/DanMarshall909/ai-router/internal/config"
	"github.com/DanMarshall909/ai-router/internal/httpapi"
	"github.com/DanMarshall909/ai-router/internal/local"
	"github.com/DanMarshall909/ai-router/internal/routing"
	"github.com/stretchr/testify/require"
)

type fakeProvider struct {
	chunks []routing.Chunk
	err    error
	mu     sync.Mutex
	calls  int
	req    routing.ChatRequest
}

func (f *fakeProvider) Stream(ctx context.Context, req routing.ChatRequest) (iter.Seq2[routing.Chunk, error], error) {
	f.mu.Lock()
	f.calls++
	f.req = req
	f.mu.Unlock()
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

func (f *fakeProvider) Request() routing.ChatRequest {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.req
}

func (f *fakeProvider) CallCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

func setupHandler(t *testing.T, localProvider, cloudProvider routing.ChatProvider) *httptest.Server {
	t.Helper()
	d := httpapi.NewDispatcher(localProvider, cloudProvider, nil)
	h := httpapi.NewHandler(d, nil, config.DefaultRoutingEnableLocalSelfAssessment, config.DefaultRoutingComplexityThreshold, nil)
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

func TestDisabledLocalSelfAssessmentDoesNotIssueClassificationPrompt(t *testing.T) {
	local := &fakeProvider{chunks: []routing.Chunk{{Content: "local response"}}}
	srv := setupHandler(t, local, nil)
	defer srv.Close()

	body := `{"model":"auto","messages":[{"role":"user","content":"hello"}]}`
	resp, err := http.Post(srv.URL+"/v1/chat/completions", "application/json", strings.NewReader(body))
	require.NoError(t, err, "because the default handler must serve an automatic request")
	defer resp.Body.Close()

	require.Equal(t, http.StatusOK, resp.StatusCode, "because the local provider is available")
	require.Equal(t, 1, local.CallCount(), "because disabled self-assessment must not issue a classification request")
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
	finished := false
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
		choice := choices[0].(map[string]any)
		require.Equal(t, float64(0), choice["index"], "because streamed choices must have an OpenAI-compatible index")
		if choice["finish_reason"] == "stop" {
			finished = true
			continue
		}
		delta := choice["delta"].(map[string]any)
		if c, ok := delta["content"].(string); ok {
			content.WriteString(c)
		}
	}

	require.Equal(t, "Hi there", content.String())
	require.True(t, finished, "because an OpenAI-compatible stream must end with a choice carrying the stop reason")
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

	body := `{"model":"auto","messages":[{"role":"function","content":"hi"}]}`
	resp, err := http.Post(srv.URL+"/v1/chat/completions", "application/json", strings.NewReader(body))
	require.NoError(t, err)
	defer resp.Body.Close()

	require.Equal(t, http.StatusBadRequest, resp.StatusCode)
}

func TestChatCompletionsAcceptsToolRole(t *testing.T) {
	local := &fakeProvider{chunks: []routing.Chunk{{Content: "tool response"}}}
	srv := setupHandler(t, local, nil)
	defer srv.Close()

	body := `{"model":"auto","messages":[{"role":"tool","content":"hi"}]}`
	resp, err := http.Post(srv.URL+"/v1/chat/completions", "application/json", strings.NewReader(body))
	require.NoError(t, err, "because tool messages are supported by the local model")
	defer resp.Body.Close()

	require.Equal(t, http.StatusOK, resp.StatusCode, "because the handler must forward a supported tool message")
	require.Equal(t, 1, local.CallCount(), "because the tool message must reach the selected provider")
}

func TestChatCompletionsForwardsToolDefinitionsAndResults(t *testing.T) {
	local := &fakeProvider{chunks: []routing.Chunk{{Content: "tool response"}}}
	srv := setupHandler(t, local, nil)
	defer srv.Close()

	body := `{"model":"auto","tools":[{"type":"function","function":{"name":"list_files","parameters":{"type":"object"}}}],"tool_choice":"auto","parallel_tool_calls":true,"messages":[{"role":"assistant","content":"","tool_calls":[{"id":"call_1","type":"function","function":{"name":"list_files","arguments":"{}"}}]},{"role":"tool","tool_call_id":"call_1","content":"README.md"}]}`
	resp, err := http.Post(srv.URL+"/v1/chat/completions", "application/json", strings.NewReader(body))
	require.NoError(t, err, "because a tool-calling request is valid")
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode, "because tool metadata is forwarded to the provider")

	req := local.Request()
	require.JSONEq(t, `[{
        "type":"function",
        "function":{"name":"list_files","parameters":{"type":"object"}}
    }]`, string(req.Tools), "because tool definitions must be preserved")
	require.JSONEq(t, `"auto"`, string(req.ToolChoice), "because tool choice must be preserved")
	require.NotNil(t, req.ParallelToolCalls, "because the parallel-tool-calls option must be preserved")
	require.True(t, *req.ParallelToolCalls, "because the requested parallel-tool-calls value must be preserved")
	require.Equal(t, "call_1", req.Messages[1].ToolCallID, "because tool results must retain their call identifier")
	require.JSONEq(t, `[{"id":"call_1","type":"function","function":{"name":"list_files","arguments":"{}"}}]`, string(req.Messages[0].ToolCalls), "because assistant tool calls must be preserved")
}

func TestChatCompletionsReturnsToolCalls(t *testing.T) {
	local := &fakeProvider{chunks: []routing.Chunk{{
		ToolCalls:    json.RawMessage(`[{"index":0,"id":"call_1","type":"function","function":{"name":"list_files","arguments":"{}"}}]`),
		FinishReason: "tool_calls",
	}}}
	srv := setupHandler(t, local, nil)
	defer srv.Close()

	body := `{"model":"auto","messages":[{"role":"user","content":"list files"}]}`
	resp, err := http.Post(srv.URL+"/v1/chat/completions", "application/json", strings.NewReader(body))
	require.NoError(t, err, "because the provider can return a tool call")
	defer resp.Body.Close()

	var result map[string]any
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&result), "because the response must be JSON")
	choice := result["choices"].([]any)[0].(map[string]any)
	message := choice["message"].(map[string]any)
	toolCalls := message["tool_calls"].([]any)
	require.Equal(t, "call_1", toolCalls[0].(map[string]any)["id"], "because the provider tool-call ID must reach the client")
	require.Equal(t, "tool_calls", choice["finish_reason"], "because tool calls must be distinguished from normal completion")
}

func TestChatCompletionsStreamsToolCalls(t *testing.T) {
	local := &fakeProvider{chunks: []routing.Chunk{{
		ToolCalls: json.RawMessage(`[{"index":0,"id":"call_1","type":"function","function":{"name":"list_files","arguments":"{}"}}]`),
	}}}
	srv := setupHandler(t, local, nil)
	defer srv.Close()

	body := `{"model":"auto","stream":true,"messages":[{"role":"user","content":"list files"}]}`
	resp, err := http.Post(srv.URL+"/v1/chat/completions", "application/json", strings.NewReader(body))
	require.NoError(t, err, "because a streaming tool-call request is valid")
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	require.NoError(t, err, "because the streamed response must be readable")
	require.Contains(t, string(data), `"tool_calls":[{"index":0,"id":"call_1"`, "because the tool-call delta must reach the client")
}

func TestChatCompletionsHighComplexityHintRoutesToCloud(t *testing.T) {
	local := &fakeProvider{chunks: []routing.Chunk{{Content: "local response"}}}
	cloud := &fakeProvider{chunks: []routing.Chunk{{Content: "cloud response"}}}
	srv := setupHandler(t, local, cloud)
	defer srv.Close()

	body := `{"model":"auto","complexity":0.9,"messages":[{"role":"user","content":"Compare these designs."}]}`
	resp, err := http.Post(srv.URL+"/v1/chat/completions", "application/json", strings.NewReader(body))
	require.NoError(t, err, "because a complexity hint is a valid request field")
	defer resp.Body.Close()

	require.Equal(t, http.StatusOK, resp.StatusCode, "because cloud reasoning is available")
	require.Equal(t, 0, local.CallCount(), "because a high complexity hint must bypass local routing")
	require.Equal(t, 1, cloud.CallCount(), "because the high complexity request must reach cloud reasoning")
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

// --- 2.2: Cold start routes to cloud immediately ---

func testManager(t *testing.T) (*local.Manager, *httptest.Server) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	u, _ := url.Parse(srv.URL)
	host := u.Hostname()
	port, _ := strconv.Atoi(u.Port())

	cfg := config.LocalModelConfig{
		ExecutablePath:  "/usr/bin/llama-server",
		ModelPath:       "/models/test.gguf",
		Host:            host,
		Port:            port,
		StartupTimeout:  5 * time.Second,
		ShutdownTimeout: 1 * time.Second,
		IdleTimeout:     5 * time.Minute,
		AdditionalArgs:  []string{},
	}
	fac := local.NewFakeProcessFactory()
	mgr := local.NewManager(cfg, fac)
	return mgr, srv
}

func TestColdStartRoutesToCloud(t *testing.T) {
	mgr, healthSrv := testManager(t)
	defer healthSrv.Close()

	local := &fakeProvider{chunks: []routing.Chunk{{Content: "local response"}}}
	cloud := &fakeProvider{chunks: []routing.Chunk{{Content: "cloud response"}}}

	d := httpapi.NewDispatcher(local, cloud, mgr)
	h := httpapi.NewHandler(d, mgr, config.DefaultRoutingEnableLocalSelfAssessment, config.DefaultRoutingComplexityThreshold, nil)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)
	srv := httptest.NewServer(mux)
	defer srv.Close()

	body := `{"model":"auto","messages":[{"role":"user","content":"hi"}]}`
	resp, err := http.Post(srv.URL+"/v1/chat/completions", "application/json", strings.NewReader(body))
	require.NoError(t, err)
	defer resp.Body.Close()

	require.Equal(t, http.StatusOK, resp.StatusCode)

	var result map[string]any
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&result))
	choices := result["choices"].([]any)
	choice := choices[0].(map[string]any)
	msg := choice["message"].(map[string]any)
	require.Equal(t, "cloud response", msg["content"], "should route to cloud on cold start")
}

// --- 2.3: Subsequent request uses local after model ready ---

func TestSubsequentRequestUsesLocalAfterReady(t *testing.T) {
	mgr, healthSrv := testManager(t)
	defer healthSrv.Close()

	local := &fakeProvider{chunks: []routing.Chunk{{Content: "local response"}}}
	cloud := &fakeProvider{chunks: []routing.Chunk{{Content: "cloud response"}}}

	d := httpapi.NewDispatcher(local, cloud, mgr)
	h := httpapi.NewHandler(d, mgr, config.DefaultRoutingEnableLocalSelfAssessment, config.DefaultRoutingComplexityThreshold, nil)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)
	srv := httptest.NewServer(mux)
	defer srv.Close()

	// First request: cold start → cloud
	body := `{"model":"auto","messages":[{"role":"user","content":"hi"}]}`
	resp, err := http.Post(srv.URL+"/v1/chat/completions", "application/json", strings.NewReader(body))
	require.NoError(t, err)
	resp.Body.Close()

	// Wait for model to become ready (health check polls every 200ms)
	time.Sleep(250 * time.Millisecond)

	// Second request: should use local now
	resp2, err := http.Post(srv.URL+"/v1/chat/completions", "application/json", strings.NewReader(body))
	require.NoError(t, err)
	defer resp2.Body.Close()

	var result map[string]any
	require.NoError(t, json.NewDecoder(resp2.Body).Decode(&result))
	choices := result["choices"].([]any)
	choice := choices[0].(map[string]any)
	msg := choice["message"].(map[string]any)
	require.Equal(t, "local response", msg["content"], "should route to local after model is ready")
}

// --- 2.4: Multiple cold-start requests coalesce ---

func TestMultipleColdStartRequestsCoalesce(t *testing.T) {
	mgr, healthSrv := testManager(t)
	defer healthSrv.Close()

	local := &fakeProvider{chunks: []routing.Chunk{{Content: "local"}}}
	cloud := &fakeProvider{chunks: []routing.Chunk{{Content: "cloud"}}}

	d := httpapi.NewDispatcher(local, cloud, mgr)

	// Send multiple concurrent requests
	var results []string
	var mu sync.Mutex
	var wg sync.WaitGroup
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			req := routing.ChatRequest{
				Model:    "auto",
				Messages: []routing.Message{{Role: "user", Content: "hi"}},
			}
			decision := routing.RoutingDecision{
				Strategy: routing.QuickLocal,
				Provider: routing.ProviderLocal,
			}
			stream, err := d.Dispatch(context.Background(), req, &decision)
			if err != nil {
				return
			}
			var content strings.Builder
			for chunk, err := range stream {
				if err != nil {
					return
				}
				content.WriteString(chunk.Content)
			}
			mu.Lock()
			results = append(results, content.String())
			mu.Unlock()
		}()
	}
	wg.Wait()

	// All should get cloud response
	for _, r := range results {
		require.Equal(t, "cloud", r, "all cold-start requests should route to cloud")
	}
}

func TestLocalSelfAssessmentSelectsCloudCoding(t *testing.T) {
	mgr, healthSrv := testManager(t)
	defer healthSrv.Close()
	require.NoError(t, mgr.Start(context.Background()), "because the local assessor requires a ready model")

	local := &fakeProvider{chunks: []routing.Chunk{{Content: "<CLOUD_CODING>"}}}
	d := httpapi.NewDispatcher(local, nil, mgr)
	strategy, response, assessed := d.Assess(context.Background(), routing.ChatRequest{
		Model:    "auto",
		Messages: []routing.Message{{Role: "user", Content: "Implement a scheduler"}},
	})

	require.True(t, assessed, "because the ready local model returned a valid assessment")
	require.Equal(t, routing.CloudCoding, strategy, "because the local model selected CLOUD_CODING")
	require.Nil(t, response, "because cloud routing should not include a local response")
}

func TestLocalSelfAssessmentSkipsWhenModelUnavailable(t *testing.T) {
	d := httpapi.NewDispatcher(&fakeProvider{}, nil, nil)
	strategy, response, assessed := d.Assess(context.Background(), routing.ChatRequest{
		Model:    "auto",
		Messages: []routing.Message{{Role: "user", Content: "Hello"}},
	})

	require.False(t, assessed, "because local assessment needs a ready local model")
	require.Equal(t, routing.QuickLocal, strategy, "because normal cold-start routing should handle unavailable local models")
	require.Nil(t, response, "because no local answer can be returned without a ready model")
}

func TestLocalSelfAssessmentReturnsAnswerWithoutSecondInference(t *testing.T) {
	mgr, healthSrv := testManager(t)
	defer healthSrv.Close()
	require.NoError(t, mgr.Start(context.Background()), "because the local assessor requires a ready model")

	local := &fakeProvider{chunks: []routing.Chunk{{Content: "The answer is 4."}}}
	d := httpapi.NewDispatcher(local, nil, mgr)
	strategy, response, assessed := d.Assess(context.Background(), routing.ChatRequest{
		Model:    "auto",
		Messages: []routing.Message{{Role: "user", Content: "What is 2 + 2?"}},
	})

	require.True(t, assessed, "because the local model returned a local answer")
	require.Equal(t, routing.QuickLocal, strategy, "because LOCAL selects local routing")
	require.NotNil(t, response, "because the completed local answer should be reused")
	for chunk, err := range response {
		require.NoError(t, err, "because the saved local answer should not fail")
		require.Equal(t, "The answer is 4.", chunk.Content, "because the completed local answer should be reused")
	}
}

func TestLocalSelfAssessmentEscalatesEmptyResponseToCloud(t *testing.T) {
	mgr, healthSrv := testManager(t)
	defer healthSrv.Close()
	require.NoError(t, mgr.Start(context.Background()), "because the local assessor requires a ready model")

	d := httpapi.NewDispatcher(&fakeProvider{}, nil, mgr)
	strategy, response, assessed := d.Assess(context.Background(), routing.ChatRequest{
		Model:    "auto",
		Messages: []routing.Message{{Role: "user", Content: "Design a scheduler"}},
	})

	require.True(t, assessed, "because an empty local response should select cloud routing")
	require.Equal(t, routing.CloudReasoning, strategy, "because the local model did not produce a usable answer")
	require.Nil(t, response, "because an empty response cannot be reused")
}

func TestEmptyAssessmentRoutesOriginalRequestToCloud(t *testing.T) {
	mgr, healthSrv := testManager(t)
	defer healthSrv.Close()
	require.NoError(t, mgr.Start(context.Background()), "because the local assessor requires a ready model")

	local := &fakeProvider{}
	cloud := &fakeProvider{chunks: []routing.Chunk{{Content: "cloud response"}}}
	d := httpapi.NewDispatcher(local, cloud, mgr)
	h := httpapi.NewHandler(d, mgr, true, config.DefaultRoutingComplexityThreshold, nil)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)
	srv := httptest.NewServer(mux)
	defer srv.Close()

	body := `{"model":"auto","messages":[{"role":"user","content":"Design a scheduler"}]}`
	resp, err := http.Post(srv.URL+"/v1/chat/completions", "application/json", strings.NewReader(body))
	require.NoError(t, err, "because the request should complete through cloud fallback")
	defer resp.Body.Close()

	var result map[string]any
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&result), "because the cloud response should be valid JSON")
	choices := result["choices"].([]any)
	choice := choices[0].(map[string]any)
	message := choice["message"].(map[string]any)
	require.Equal(t, "cloud response", message["content"], "because an empty assessment must route the original request to cloud")
	require.Equal(t, 1, local.CallCount(), "because local must only be called for assessment")
	require.Equal(t, 1, cloud.CallCount(), "because cloud must serve the original request")
}

func TestTraceLoggerWritesRequestResponseAndRoutingMetadata(t *testing.T) {
	directory := t.TempDir()
	logger := httpapi.NewTraceLogger(directory, true)
	request := httpapi.ChatCompletionRequest{
		Model:    "auto",
		Messages: []httpapi.Message{{Role: "user", Content: "hello"}},
	}
	decision := routing.RoutingDecision{
		Strategy:       routing.QuickLocal,
		Provider:       routing.ProviderLocal,
		ActualProvider: routing.ProviderLocal,
		Model:          "bonsai",
	}

	require.NoError(t, logger.Write(request, decision, "Hello!", nil, time.Now()), "because a debug trace should persist")
	entries, err := os.ReadDir(directory)
	require.NoError(t, err, "because the trace directory should be readable")
	require.Len(t, entries, 1, "because one request should produce one trace")
	data, err := os.ReadFile(filepath.Join(directory, entries[0].Name()))
	require.NoError(t, err, "because the trace file should be readable")
	require.Contains(t, string(data), "Hello!", "because the response should be captured")
	require.Contains(t, string(data), "bonsai", "because the routed model should be captured")
}
