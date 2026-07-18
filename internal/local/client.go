package local

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"iter"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/DanMarshall909/ai-router/internal/routing"
)

// FailureKind classifies where in the stream a failure occurred.
type FailureKind int

const (
	// FailureBeforeFirstChunk means no tokens reached the client.
	FailureBeforeFirstChunk FailureKind = iota
	// FailureAfterFirstChunk means some tokens were already delivered.
	FailureAfterFirstChunk
)

// FailureOutcome describes a local inference failure.
type FailureOutcome struct {
	Kind FailureKind
	Err  error
}

func (f FailureOutcome) Error() string {
	return f.Err.Error()
}

func (f FailureOutcome) Unwrap() error {
	return f.Err
}

// LocalClient talks to a llama-server instance.
type LocalClient struct {
	baseURL    string
	httpClient *http.Client
	logPrompts bool
}

// NewLocalClient creates a client for the given base URL.
func NewLocalClient(baseURL string, timeout time.Duration) *LocalClient {
	return &LocalClient{
		baseURL: strings.TrimRight(baseURL, "/"),
		httpClient: &http.Client{
			Timeout: timeout,
		},
	}
}

// Stream implements routing.ChatProvider.
func (c *LocalClient) Stream(ctx context.Context, req routing.ChatRequest) (iter.Seq2[routing.Chunk, error], error) {
	body, err := c.buildRequestBody(req)
	if err != nil {
		return nil, err
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/v1/chat/completions", body)
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")

	started := time.Now()
	inputCharacters := requestCharacters(req.Messages)
	slog.Debug("sending request to local model", "input_characters", inputCharacters)
	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, FailureOutcome{Kind: FailureBeforeFirstChunk, Err: fmt.Errorf("connection failure: %w", err)}
	}

	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, FailureOutcome{Kind: FailureBeforeFirstChunk, Err: fmt.Errorf("invalid response: status %d", resp.StatusCode)}
	}

	return c.readStream(ctx, resp, started, inputCharacters), nil
}

func requestCharacters(messages []routing.Message) int {
	characters := 0
	for _, message := range messages {
		characters += len(message.Content)
	}
	return characters
}

func (c *LocalClient) buildRequestBody(req routing.ChatRequest) (io.Reader, error) {
	type message struct {
		Role       string          `json:"role"`
		Content    string          `json:"content"`
		ToolCallID string          `json:"tool_call_id,omitempty"`
		ToolCalls  json.RawMessage `json:"tool_calls,omitempty"`
	}
	type request struct {
		Model             string          `json:"model"`
		Messages          []message       `json:"messages"`
		Tools             json.RawMessage `json:"tools,omitempty"`
		ToolChoice        json.RawMessage `json:"tool_choice,omitempty"`
		ParallelToolCalls *bool           `json:"parallel_tool_calls,omitempty"`
		Stream            bool            `json:"stream"`
	}

	msgs := make([]message, len(req.Messages))
	for i, m := range req.Messages {
		msgs[i] = message{Role: m.Role, Content: m.Content, ToolCallID: m.ToolCallID, ToolCalls: m.ToolCalls}
	}

	r := request{
		Model:             req.Model,
		Messages:          msgs,
		Tools:             req.Tools,
		ToolChoice:        req.ToolChoice,
		ParallelToolCalls: req.ParallelToolCalls,
		Stream:            true,
	}

	data, err := json.Marshal(r)
	if err != nil {
		return nil, err
	}
	return strings.NewReader(string(data)), nil
}

func (c *LocalClient) readStream(ctx context.Context, resp *http.Response, started time.Time, inputCharacters int) iter.Seq2[routing.Chunk, error] {
	return func(yield func(routing.Chunk, error) bool) {
		defer resp.Body.Close()

		scanner := bufio.NewScanner(resp.Body)
		firstChunk := true
		firstChunkAt := time.Time{}
		outputCharacters := 0

		for scanner.Scan() {
			select {
			case <-ctx.Done():
				kind := FailureAfterFirstChunk
				if firstChunk {
					kind = FailureBeforeFirstChunk
				}
				yield(routing.Chunk{}, FailureOutcome{Kind: kind, Err: ctx.Err()})
				return
			default:
			}

			line := scanner.Text()
			if !strings.HasPrefix(line, "data: ") {
				continue
			}
			data := strings.TrimPrefix(line, "data: ")
			if data == "[DONE]" {
				if firstChunk {
					yield(routing.Chunk{}, FailureOutcome{Kind: FailureBeforeFirstChunk, Err: fmt.Errorf("local model returned an empty response")})
					return
				}
				logLocalResponseComplete(started, firstChunkAt, inputCharacters, outputCharacters)
				return
			}

			var chunk struct {
				Model   string `json:"model"`
				Choices []struct {
					Delta struct {
						Content   string          `json:"content"`
						ToolCalls json.RawMessage `json:"tool_calls"`
					} `json:"delta"`
					FinishReason string `json:"finish_reason"`
				} `json:"choices"`
			}
			if err := json.Unmarshal([]byte(data), &chunk); err != nil {
				kind := FailureAfterFirstChunk
				if firstChunk {
					kind = FailureBeforeFirstChunk
				}
				yield(routing.Chunk{}, FailureOutcome{Kind: kind, Err: fmt.Errorf("invalid response: %w", err)})
				return
			}

			if len(chunk.Choices) == 0 {
				continue
			}

			choice := chunk.Choices[0]
			content := choice.Delta.Content
			if content == "" && len(choice.Delta.ToolCalls) == 0 {
				if choice.FinishReason != "" && firstChunk {
					yield(routing.Chunk{}, FailureOutcome{Kind: FailureBeforeFirstChunk, Err: fmt.Errorf("local model returned an empty response")})
					return
				}
				continue
			}

			if firstChunk {
				firstChunkAt = time.Now()
				slog.Info("local response started", "first_token_duration", firstChunkAt.Sub(started), "input_characters", inputCharacters)
			}
			firstChunk = false
			outputCharacters += len(content)
			if !yield(routing.Chunk{Content: content, ToolCalls: choice.Delta.ToolCalls, FinishReason: choice.FinishReason, Provider: "local", Model: chunk.Model}, nil) {
				return
			}
		}

		if err := scanner.Err(); err != nil {
			kind := FailureAfterFirstChunk
			if firstChunk {
				kind = FailureBeforeFirstChunk
			}
			yield(routing.Chunk{}, FailureOutcome{Kind: kind, Err: fmt.Errorf("generation failure: %w", err)})
		}
	}
}

func logLocalResponseComplete(started, firstChunkAt time.Time, inputCharacters, outputCharacters int) {
	duration := time.Since(started)
	attributes := []any{
		"duration", duration,
		"input_characters", inputCharacters,
		"output_characters", outputCharacters,
	}
	if !firstChunkAt.IsZero() {
		generationDuration := time.Since(firstChunkAt)
		if generationDuration > 0 {
			attributes = append(attributes, "generation_characters_per_second", float64(outputCharacters)/generationDuration.Seconds())
		}
	}
	slog.Info("local response complete", attributes...)
}
