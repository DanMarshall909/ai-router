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
)

func main() {
	configPath := flag.String("config", defaultConfigPath(), "path to config.json")
	flag.Parse()

	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
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
	h := httpapi.NewHandler(d)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	addr := fmt.Sprintf("%s:%d", cfg.LocalModel.Host, cfg.LocalModel.Port+1)
	slog.Info("ai-router starting", "addr", addr)

	if err := http.ListenAndServe(addr, mux); err != nil {
		slog.Error("server error", "err", err)
		os.Exit(1)
	}
}

func cloudModelMap(cfg config.Config) map[string]string {
	m := make(map[string]string)
	for strategy, mapping := range cfg.ModelRouting.Strategies {
		if mapping.Provider == "openrouter" {
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
