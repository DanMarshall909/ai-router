package httpapi

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/DanMarshall909/ai-router/internal/routing"
)

const debugTraceFileExtension = ".json"

// TraceLogger persists request and response content for troubleshooting.
type TraceLogger struct {
	directory string
	enabled   bool
}

// NewTraceLogger creates an optional on-disk request trace logger.
func NewTraceLogger(directory string, enabled bool) *TraceLogger {
	return &TraceLogger{directory: directory, enabled: enabled}
}

type completionTrace struct {
	Timestamp time.Time               `json:"timestamp"`
	Request   ChatCompletionRequest   `json:"request"`
	Decision  routing.RoutingDecision `json:"decision"`
	Response  string                  `json:"response,omitempty"`
	Error     string                  `json:"error,omitempty"`
	Duration  time.Duration           `json:"duration"`
}

func (l *TraceLogger) Write(req ChatCompletionRequest, decision routing.RoutingDecision, response string, err error, started time.Time) error {
	if !l.enabled {
		return nil
	}
	if err := os.MkdirAll(l.directory, os.ModePerm); err != nil {
		return fmt.Errorf("creating debug trace directory: %w", err)
	}

	trace := completionTrace{
		Timestamp: started,
		Request:   req,
		Decision:  decision,
		Response:  response,
		Duration:  time.Since(started),
	}
	if err != nil {
		trace.Error = err.Error()
	}
	data, err := json.MarshalIndent(trace, "", "  ")
	if err != nil {
		return fmt.Errorf("encoding debug trace: %w", err)
	}

	name := started.UTC().Format("20060102T150405.000000000Z") + debugTraceFileExtension
	path := filepath.Join(l.directory, name)
	if err := os.WriteFile(path, data, os.FileMode(0600)); err != nil {
		return fmt.Errorf("writing debug trace: %w", err)
	}
	return nil
}
