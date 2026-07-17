package main

import (
	"flag"
	"fmt"
	"log"
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
	configPath := flag.String("config", "config.json", "path to config.json")
	flag.Parse()

	cfg, err := config.Load(*configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "configuration error: %v\n", err)
		os.Exit(1)
	}

	// Create process manager
	procFac := local.NewExecFactory()
	mgr := local.NewManager(cfg.LocalModel, procFac)

	// Create local client
	localBaseURL := fmt.Sprintf("http://%s:%d", cfg.LocalModel.Host, cfg.LocalModel.Port)
	localClient := local.NewLocalClient(localBaseURL, cfg.LocalModel.StartupTimeout)

	// Create cloud client (if enabled)
	var cloudClient routing.ChatProvider
	if cfg.OpenRouter.Enabled {
		cloudClient = cloud.NewOpenRouterClient(
			cfg.OpenRouter.BaseURL,
			cfg.OpenRouter.APIKey,
			cloudModelMap(cfg),
			time.Duration(cfg.OpenRouter.Timeout),
			cfg.OpenRouter.ApplicationTitle,
			cfg.OpenRouter.Referer,
		)
	}

	// Wire dispatcher and handler
	d := httpapi.NewDispatcher(localClient, cloudClient, mgr)
	h := httpapi.NewHandler(d)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	addr := fmt.Sprintf("%s:%d", cfg.LocalModel.Host, cfg.LocalModel.Port+1)
	log.Printf("ai-router starting on %s", addr)
	log.Printf("local model: %s", cfg.LocalModel.ExecutablePath)
	log.Printf("cloud enabled: %v", cfg.OpenRouter.Enabled)

	if err := http.ListenAndServe(addr, mux); err != nil {
		log.Fatalf("server error: %v", err)
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
