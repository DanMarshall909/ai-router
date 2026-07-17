package main

import (
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"time"

	"github.com/DanMarshall909/ai-router/internal/cloud"
	"github.com/DanMarshall909/ai-router/internal/config"
	"github.com/DanMarshall909/ai-router/internal/httpapi"
	"github.com/DanMarshall909/ai-router/internal/local"
	"github.com/DanMarshall909/ai-router/internal/routing"
	"github.com/lmittmann/tint"
)

func main() {
	configPath := flag.String("config", defaultConfigPath(), "path to config.json")
	debugLog := flag.Bool("debug-log", false, "save request and response traces to disk")
	flag.Usage = printUsage
	flag.Parse()

	if flag.NArg() > 0 && flag.Arg(0) == "help" {
		printUsage()
		os.Exit(0)
	}

	slog.SetDefault(slog.New(tint.NewTextHandler(os.Stderr, &tint.Options{
		Level: slog.LevelDebug,
	})))

	cfg, err := config.Load(*configPath)
	if err != nil {
		slog.Error("configuration error", "err", err)
		os.Exit(1)
	}

	slog.Info("config loaded",
		"executable", cfg.LocalModel.ExecutablePath,
		"model", cfg.LocalModel.ModelPath,
		"host", cfg.LocalModel.Host,
		"port", cfg.LocalModel.Port,
		"cloud_enabled", cfg.OpenRouter.Enabled,
	)

	// Create process manager
	procFac := local.NewExecFactory()
	mgr := local.NewManager(cfg.LocalModel, procFac)

	// Create local client
	localBaseURL := fmt.Sprintf("http://%s:%d", cfg.LocalModel.Host, cfg.LocalModel.Port)
	localClient := local.NewLocalClient(localBaseURL, cfg.LocalModel.StartupTimeout)

	// Create cloud client (if enabled)
	var cloudClient routing.ChatProvider
	if cfg.OpenRouter.Enabled {
		var opts []cloud.OpenRouterOption
		if cfg.OpenRouter.AutoModel != "" {
			opts = append(opts, cloud.WithAutoModel(cfg.OpenRouter.AutoModel))
		}
		if len(cfg.OpenRouter.Fallbacks) > 0 {
			opts = append(opts, cloud.WithFallbacks(cfg.OpenRouter.Fallbacks))
		}
		if cfg.OpenRouter.CostQualityTradeoff > 0 {
			opts = append(opts, cloud.WithCostQualityTradeoff(cfg.OpenRouter.CostQualityTradeoff))
		}
		if len(cfg.OpenRouter.AllowedModels) > 0 {
			opts = append(opts, cloud.WithAllowedModels(cfg.OpenRouter.AllowedModels))
		}
		cloudClient = cloud.NewOpenRouterClient(
			cfg.OpenRouter.BaseURL,
			cfg.OpenRouter.APIKey,
			cloudModelMap(cfg),
			time.Duration(cfg.OpenRouter.Timeout),
			cfg.OpenRouter.ApplicationTitle,
			cfg.OpenRouter.Referer,
			opts...,
		)
		slog.Info("cloud provider enabled", "base_url", cfg.OpenRouter.BaseURL)
	}

	// Wire dispatcher and handler
	d := httpapi.NewDispatcher(localClient, cloudClient, mgr)
	h := httpapi.NewHandler(d, mgr, cfg.Routing.EnableLocalSelfAssessment, cfg.Routing.ComplexityThreshold, httpapi.NewTraceLogger(defaultDebugLogDirectory(), *debugLog))
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	// Start idle unload watcher
	mgr.StartIdleWatcher()
	defer mgr.StopIdleWatcher()

	addr := fmt.Sprintf("%s:%d", cfg.LocalModel.Host, cfg.LocalModel.Port+1)
	slog.Info("ai-router starting",
		"addr", addr,
		"idle_timeout", cfg.LocalModel.IdleTimeout.String(),
	)
	fmt.Fprintf(os.Stderr, "ai-router listening on http://%s\n", addr)
	fmt.Fprintf(os.Stderr, "  POST /v1/chat/completions          Chat completions\n")
	fmt.Fprintf(os.Stderr, "  POST /api/router/local-model/stop   Stop local model\n")
	fmt.Fprintf(os.Stderr, "  POST /api/router/local-model/start  Start local model\n")

	if err := http.ListenAndServe(addr, mux); err != nil {
		slog.Error("server error", "err", err)
		os.Exit(1)
	}
}

func cloudModelMap(cfg config.Config) map[string]string {
	m := make(map[string]string)
	for strategy, mapping := range cfg.ModelRouting.Strategies {
		if mapping.Provider == routing.ProviderCloud {
			m[strategy] = mapping.Model
		}
	}
	return m
}

func defaultConfigPath() string {
	if home, err := os.UserHomeDir(); err == nil {
		return home + "/.config/ai-router/config.json"
	}
	return "config.json"
}

func defaultDebugLogDirectory() string {
	if home, err := os.UserHomeDir(); err == nil {
		return home + "/.local/state/ai-router/debug"
	}
	return "debug"
}

func printUsage() {
	fmt.Fprintf(os.Stderr, `ai-router - Local-first AI inference router

USAGE:
  ai-router [flags]
  ai-router help

FLAGS:
`)
	flag.PrintDefaults()
	fmt.Fprintf(os.Stderr, `
HTTP ENDPOINTS:
  POST /v1/chat/completions    OpenAI-compatible chat completions
  POST /api/router/local-model/stop   Stop the local model
  POST /api/router/local-model/start  Start the local model

CONFIGURATION:
  Default config: ~/.config/ai-router/config.json
  Override with:  -config /path/to/config.json

ENVIRONMENT VARIABLES:
  LOCAL_MODEL_EXECUTABLE_PATH      Path to llama-server executable
  LOCAL_MODEL_MODEL_PATH           Path to GGUF model file
  LOCAL_MODEL_HOST                 Local model host (default: 127.0.0.1)
  LOCAL_MODEL_PORT                 Local model port (default: 8080)
  LOCAL_MODEL_STARTUP_TIMEOUT_SECONDS  Startup timeout in seconds
  LOCAL_MODEL_SHUTDOWN_TIMEOUT_SECONDS Shutdown timeout in seconds
  LOCAL_MODEL_IDLE_TIMEOUT_MINUTES     Idle timeout in minutes (0 = disabled)
  OPENROUTER_ENABLED              Enable OpenRouter cloud (true/false)
  OPENROUTER_API_KEY              OpenRouter API key
  OPENROUTER_BASE_URL             OpenRouter base URL
  OPENROUTER_TIMEOUT_SECONDS      OpenRouter request timeout
  ROUTING_MAX_LOCAL_INPUT_TOKENS  Max tokens for local model
  ROUTING_COMPLEXITY_THRESHOLD    Complexity threshold (0-1)
  SHELL_TOOL_ENABLED              Enable shell tool (true/false)

DEBUG LOGGING:
  -debug-log    Save prompt and response content to ~/.local/state/ai-router/debug/
`)
}
