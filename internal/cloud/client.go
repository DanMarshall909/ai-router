package cloud

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"iter"
	"net/http"
	"strings"
	"time"

	"github.com/DanMarshall909/ai-router/internal/routing"
)

// OpenRouterClient talks to the OpenRouter API.
type OpenRouterClient struct {
	baseURL             string
	apiKey              string
	modelMap            map[string]string // strategy -> model identifier
	httpClient          *http.Client
	appTitle            string
	referer             string
	autoModel           string
	fallbacks           []string
	costQualityTradeoff int
	allowedModels       []string
}

// NewOpenRouterClient creates a client from the given parameters.
func NewOpenRouterClient(baseURL, apiKey string, modelMap map[string]string, timeout time.Duration, appTitle, referer string, opts ...OpenRouterOption) *OpenRouterClient {
	c := &OpenRouterClient{
		baseURL:  strings.TrimRight(baseURL, "/"),
		apiKey:   apiKey,
		modelMap: modelMap,
		httpClient: &http.Client{
			Timeout: timeout,
		},
		appTitle: appTitle,
		referer:  referer,
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

// OpenRouterOption configures the OpenRouter client.
type OpenRouterOption func(*OpenRouterClient)

// WithAutoModel sets the auto-routing model (e.g. "openrouter/auto").
func WithAutoModel(model string) OpenRouterOption {
	return func(c *OpenRouterClient) { c.autoModel = model }
}

// WithFallbacks sets the fallback model chain.
func WithFallbacks(fallbacks []string) OpenRouterOption {
	return func(c *OpenRouterClient) { c.fallbacks = fallbacks }
}

// WithCostQualityTradeoff sets the cost/quality tradeoff (0-10).
func WithCostQualityTradeoff(tradeoff int) OpenRouterOption {
	return func(c *OpenRouterClient) { c.costQualityTradeoff = tradeoff }
}

// WithAllowedModels sets the allowed model patterns for auto routing.
func WithAllowedModels(patterns []string) OpenRouterOption {
	return func(c *OpenRouterClient) { c.allowedModels = patterns }
}

// Stream implements routing.ChatProvider.
func (c *OpenRouterClient) Stream(ctx context.Context, req routing.ChatRequest) (iter.Seq2[routing.Chunk, error], error) {
	model := req.Model
	if mapped, ok := c.modelMap[model]; ok {
		model = mapped
	}

	body, err := c.buildRequestBody(req, model)
	if err != nil {
		return nil, err
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/chat/completions", body)
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+c.apiKey)
	if c.appTitle != "" {
		httpReq.Header.Set("HTTP-Referer", c.referer)
		httpReq.Header.Set("X-Title", c.appTitle)
	}

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("connection failure: %w", err)
	}

	if resp.StatusCode == http.StatusUnauthorized {
		resp.Body.Close()
		return nil, fmt.Errorf("authentication failed: invalid API key")
	}
	if resp.StatusCode == http.StatusTooManyRequests {
		resp.Body.Close()
		return nil, fmt.Errorf("rate limited by OpenRouter")
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, fmt.Errorf("unexpected status %d from OpenRouter", resp.StatusCode)
	}

	return c.readStream(ctx, resp), nil
}

func (c *OpenRouterClient) buildRequestBody(req routing.ChatRequest, model string) (io.Reader, error) {
	type message struct {
		Role    string `json:"role"`
		Content string `json:"content"`
	}
	type plugin struct {
		ID                string   `json:"id"`
		AllowedModels     []string `json:"allowed_models,omitempty"`
		CostQualityTradeoff *int   `json:"cost_quality_tradeoff,omitempty"`
	}
	type request struct {
		Model      string    `json:"model"`
		Messages   []message `json:"messages"`
		Stream     bool      `json:"stream"`
		SessionID  string    `json:"session_id,omitempty"`
		Fallbacks  []string  `json:"models,omitempty"`
		Plugins    []plugin  `json:"plugins,omitempty"`
	}

	msgs := make([]message, len(req.Messages))
	for i, m := range req.Messages {
		msgs[i] = message{Role: m.Role, Content: m.Content}
	}

	r := request{
		Model:     model,
		Messages:  msgs,
		Stream:    true,
		SessionID: req.SessionID,
	}

	// Use request-level fallbacks, then client defaults
	fallbacks := req.Fallbacks
	if len(fallbacks) == 0 {
		fallbacks = c.fallbacks
	}
	if len(fallbacks) > 0 {
		r.Fallbacks = fallbacks
	}

	// Build plugins for auto router
	if model == "openrouter/auto" || c.autoModel != "" {
		p := plugin{ID: "auto-router"}
		if len(c.allowedModels) > 0 {
			p.AllowedModels = c.allowedModels
		}
		if c.costQualityTradeoff > 0 {
			p.CostQualityTradeoff = &c.costQualityTradeoff
		}
		r.Plugins = []plugin{p}
	}

	data, err := json.Marshal(r)
	if err != nil {
		return nil, err
	}
	return strings.NewReader(string(data)), nil
}

func (c *OpenRouterClient) readStream(ctx context.Context, resp *http.Response) iter.Seq2[routing.Chunk, error] {
	return func(yield func(routing.Chunk, error) bool) {
		defer resp.Body.Close()

		scanner := bufio.NewScanner(resp.Body)

		for scanner.Scan() {
			select {
			case <-ctx.Done():
				yield(routing.Chunk{}, ctx.Err())
				return
			default:
			}

			line := scanner.Text()
			if !strings.HasPrefix(line, "data: ") {
				continue
			}
			data := strings.TrimPrefix(line, "data: ")
			if data == "[DONE]" {
				return
			}

			var chunk struct {
				Choices []struct {
					Delta struct {
						Content string `json:"content"`
					} `json:"delta"`
				} `json:"choices"`
			}
			if err := json.Unmarshal([]byte(data), &chunk); err != nil {
				yield(routing.Chunk{}, fmt.Errorf("invalid response: %w", err))
				return
			}

			if len(chunk.Choices) == 0 {
				continue
			}

			content := chunk.Choices[0].Delta.Content
			if content == "" {
				continue
			}

			if !yield(routing.Chunk{Content: content}, nil) {
				return
			}
		}

		if err := scanner.Err(); err != nil {
			yield(routing.Chunk{}, fmt.Errorf("generation failure: %w", err))
		}
	}
}
