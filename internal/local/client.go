package local

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
	Kind    FailureKind
	Err     error
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

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, FailureOutcome{Kind: FailureBeforeFirstChunk, Err: fmt.Errorf("connection failure: %w", err)}
	}

	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, FailureOutcome{Kind: FailureBeforeFirstChunk, Err: fmt.Errorf("invalid response: status %d", resp.StatusCode)}
	}

	return c.readStream(ctx, resp), nil
}

func (c *LocalClient) buildRequestBody(req routing.ChatRequest) (io.Reader, error) {
	type message struct {
		Role    string `json:"role"`
		Content string `json:"content"`
	}
	type request struct {
		Model    string    `json:"model"`
		Messages []message `json:"messages"`
		Stream   bool      `json:"stream"`
	}

	msgs := make([]message, len(req.Messages))
	for i, m := range req.Messages {
		msgs[i] = message{Role: m.Role, Content: m.Content}
	}

	r := request{
		Model:    req.Model,
		Messages: msgs,
		Stream:   true,
	}

	data, err := json.Marshal(r)
	if err != nil {
		return nil, err
	}
	return strings.NewReader(string(data)), nil
}

func (c *LocalClient) readStream(ctx context.Context, resp *http.Response) iter.Seq2[routing.Chunk, error] {
	return func(yield func(routing.Chunk, error) bool) {
		defer resp.Body.Close()

		scanner := bufio.NewScanner(resp.Body)
		firstChunk := true

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

			content := chunk.Choices[0].Delta.Content
			if content == "" {
				continue
			}

			firstChunk = false
			if !yield(routing.Chunk{Content: content}, nil) {
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
