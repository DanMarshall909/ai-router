package config

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/DanMarshall909/ai-router/internal/routing"
)

// Key paths for localModel configuration.
const (
	KeyLocalModelExecutablePath    = "localModel.executablePath"
	KeyLocalModelModelPath         = "localModel.modelPath"
	KeyLocalModelHost              = "localModel.host"
	KeyLocalModelPort              = "localModel.port"
	KeyLocalModelStartupTimeout    = "localModel.startupTimeoutSeconds"
	KeyLocalModelShutdownTimeout   = "localModel.shutdownTimeoutSeconds"
	KeyLocalModelIdleTimeout       = "localModel.idleTimeoutMinutes"
	KeyLocalModelAdditionalArgs    = "localModel.additionalArgs"
)

// Defaults for localModel.
const (
	DefaultLocalModelHost              = "127.0.0.1"
	DefaultLocalModelPort              = 8080
	DefaultLocalModelStartupTimeout    = 60 * time.Second
	DefaultLocalModelShutdownTimeout   = 10 * time.Second
	DefaultLocalModelIdleTimeout       = 5 * time.Minute
)

// Key paths for openRouter configuration.
const (
	KeyOpenRouterEnabled            = "openRouter.enabled"
	KeyOpenRouterAPIKey             = "openRouter.apiKey"
	KeyOpenRouterBaseURL            = "openRouter.baseUrl"
	KeyOpenRouterTimeout            = "openRouter.timeoutSeconds"
	KeyOpenRouterApplicationTitle   = "openRouter.applicationTitle"
	KeyOpenRouterReferer            = "openRouter.referer"
)

// Defaults for openRouter.
const (
	DefaultOpenRouterEnabled  = false
	DefaultOpenRouterBaseURL  = "https://openrouter.ai/api/v1"
	DefaultOpenRouterTimeout  = 30 * time.Second
)

// Key paths for routing configuration.
const (
	KeyRoutingMaxLocalInputTokens         = "routing.maxLocalInputTokens"
	KeyRoutingComplexityThreshold         = "routing.complexityThreshold"
	KeyRoutingEnableLocalSelfAssessment   = "routing.enableLocalSelfAssessment"
	KeyRoutingAllowedOverrides            = "routing.allowedOverrides"
)

// Defaults for routing.
const (
	DefaultRoutingMaxLocalInputTokens       = 4096
	DefaultRoutingComplexityThreshold       = 0.7
	DefaultRoutingEnableLocalSelfAssessment = false
)

// Key paths for shellTool configuration.
const (
	KeyShellToolEnabled                   = "shellTool.enabled"
	KeyShellToolAllowedWorkingDirectories = "shellTool.allowedWorkingDirectories"
	KeyShellToolAllowedCommands           = "shellTool.allowedCommands"
	KeyShellToolTimeout                   = "shellTool.timeoutSeconds"
	KeyShellToolMaximumOutputCharacters   = "shellTool.maximumOutputCharacters"
)

// Defaults for shellTool.
const (
	DefaultShellToolEnabled                 = false
	DefaultShellToolTimeout                 = 30 * time.Second
	DefaultShellToolMaximumOutputCharacters = 10000
)

// Key paths for modelRouting configuration.
const (
	KeyModelRoutingStrategies = "modelRouting.strategies"
)

// LocalModelConfig holds configuration for the local llama-server process.
type LocalModelConfig struct {
	ExecutablePath    string        `json:"executablePath"`
	ModelPath         string        `json:"modelPath"`
	Host              string        `json:"host"`
	Port              int           `json:"port"`
	StartupTimeout    time.Duration `json:"startupTimeoutSeconds"`
	ShutdownTimeout   time.Duration `json:"shutdownTimeoutSeconds"`
	IdleTimeout       time.Duration `json:"idleTimeoutMinutes"`
	AdditionalArgs    []string      `json:"additionalArgs"`
}

// OpenRouterConfig holds configuration for the OpenRouter cloud provider.
type OpenRouterConfig struct {
	Enabled          bool          `json:"enabled"`
	APIKey           string        `json:"apiKey"`
	BaseURL          string        `json:"baseUrl"`
	Timeout          time.Duration `json:"timeoutSeconds"`
	ApplicationTitle string        `json:"applicationTitle"`
	Referer          string        `json:"referer"`
	Models           map[string]string `json:"models"`
}

// RoutingConfig holds configuration for the routing policy.
type RoutingConfig struct {
	MaxLocalInputTokens         int      `json:"maxLocalInputTokens"`
	ComplexityThreshold         float64  `json:"complexityThreshold"`
	EnableLocalSelfAssessment   bool     `json:"enableLocalSelfAssessment"`
	AllowedOverrides            []string `json:"allowedOverrides"`
}

// ShellToolConfig holds configuration for the shell-command tool.
type ShellToolConfig struct {
	Enabled                 bool          `json:"enabled"`
	AllowedWorkingDirectories []string    `json:"allowedWorkingDirectories"`
	AllowedCommands         []string      `json:"allowedCommands"`
	Timeout                 time.Duration `json:"timeoutSeconds"`
	MaximumOutputCharacters int           `json:"maximumOutputCharacters"`
}

// StrategyMapping maps an inference strategy to a provider and model.
type StrategyMapping struct {
	Provider string `json:"provider"`
	Model    string `json:"model"`
}

// ModelRoutingConfig holds the strategy-to-provider/model mapping.
type ModelRoutingConfig struct {
	Strategies map[string]StrategyMapping `json:"strategies"`
}

// Config is the top-level configuration for the router.
type Config struct {
	LocalModel   LocalModelConfig   `json:"localModel"`
	OpenRouter   OpenRouterConfig   `json:"openRouter"`
	Routing      RoutingConfig      `json:"routing"`
	ShellTool    ShellToolConfig    `json:"shellTool"`
	ModelRouting ModelRoutingConfig `json:"modelRouting"`
}

// PlaceholderPrefix is the prefix used in config values that must be
// replaced with real identifiers before use.
const PlaceholderPrefix = "<configure-me:"

// Placeholder models are values that must be replaced before use.
// Task 2.5 requires startup to reject them.
var PlaceholderModels = map[string]string{
	string(routing.CloudGeneral):  "<configure-me: see https://openrouter.ai/models>",
	string(routing.CloudReasoning): "<configure-me: see https://openrouter.ai/models>",
	string(routing.CloudCoding):   "<configure-me: see https://openrouter.ai/models>",
}

// Load reads a config.json file, applies environment variable overrides,
// and returns a validated Config.
func Load(path string) (Config, error) {
	cfg := DefaultConfig()

	data, err := os.ReadFile(path)
	if err != nil {
		return cfg, fmt.Errorf("reading config file %s: %w", path, err)
	}

	if err := json.Unmarshal(data, &cfg); err != nil {
		return cfg, fmt.Errorf("parsing config file %s: %w", path, err)
	}

	applyEnvOverrides(&cfg)

	if err := Validate(cfg); err != nil {
		return cfg, err
	}

	return cfg, nil
}

// applyEnvOverrides sets config values from environment variables.
// Environment variables use the key path with dots replaced by underscores
// and converted to upper case, e.g. LOCAL_MODEL_EXECUTABLE_PATH.
func applyEnvOverrides(cfg *Config) {
	if v := os.Getenv("LOCAL_MODEL_EXECUTABLE_PATH"); v != "" {
		cfg.LocalModel.ExecutablePath = v
	}
	if v := os.Getenv("LOCAL_MODEL_MODEL_PATH"); v != "" {
		cfg.LocalModel.ModelPath = v
	}
	if v := os.Getenv("LOCAL_MODEL_HOST"); v != "" {
		cfg.LocalModel.Host = v
	}
	if v := os.Getenv("LOCAL_MODEL_PORT"); v != "" {
		if port, err := strconv.Atoi(v); err == nil {
			cfg.LocalModel.Port = port
		}
	}
	if v := os.Getenv("LOCAL_MODEL_STARTUP_TIMEOUT_SECONDS"); v != "" {
		if d, err := time.ParseDuration(v + "s"); err == nil {
			cfg.LocalModel.StartupTimeout = d
		}
	}
	if v := os.Getenv("LOCAL_MODEL_SHUTDOWN_TIMEOUT_SECONDS"); v != "" {
		if d, err := time.ParseDuration(v + "s"); err == nil {
			cfg.LocalModel.ShutdownTimeout = d
		}
	}
	if v := os.Getenv("LOCAL_MODEL_IDLE_TIMEOUT_MINUTES"); v != "" {
		if d, err := time.ParseDuration(v + "m"); err == nil {
			cfg.LocalModel.IdleTimeout = d
		}
	}
	if v := os.Getenv("OPENROUTER_ENABLED"); v != "" {
		cfg.OpenRouter.Enabled = strings.ToLower(v) == "true"
	}
	if v := os.Getenv("OPENROUTER_API_KEY"); v != "" {
		cfg.OpenRouter.APIKey = v
	}
	if v := os.Getenv("OPENROUTER_BASE_URL"); v != "" {
		cfg.OpenRouter.BaseURL = v
	}
	if v := os.Getenv("OPENROUTER_TIMEOUT_SECONDS"); v != "" {
		if d, err := time.ParseDuration(v + "s"); err == nil {
			cfg.OpenRouter.Timeout = d
		}
	}
	if v := os.Getenv("ROUTING_MAX_LOCAL_INPUT_TOKENS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			cfg.Routing.MaxLocalInputTokens = n
		}
	}
	if v := os.Getenv("ROUTING_COMPLEXITY_THRESHOLD"); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			cfg.Routing.ComplexityThreshold = f
		}
	}
	if v := os.Getenv("SHELL_TOOL_ENABLED"); v != "" {
		cfg.ShellTool.Enabled = strings.ToLower(v) == "true"
	}
}

// Validate checks a Config for required values and logical consistency.
// It returns an error naming the offending section and setting.
func Validate(cfg Config) error {
	if cfg.LocalModel.ExecutablePath == "" {
		return fmt.Errorf("validation failed: %s is required", KeyLocalModelExecutablePath)
	}
	if cfg.LocalModel.ModelPath == "" {
		return fmt.Errorf("validation failed: %s is required", KeyLocalModelModelPath)
	}
	if cfg.LocalModel.Port <= 0 || cfg.LocalModel.Port > 65535 {
		return fmt.Errorf("validation failed: %s must be between 1 and 65535, got %d", KeyLocalModelPort, cfg.LocalModel.Port)
	}
	if cfg.LocalModel.StartupTimeout <= 0 {
		return fmt.Errorf("validation failed: %s must be positive", KeyLocalModelStartupTimeout)
	}
	if cfg.LocalModel.ShutdownTimeout <= 0 {
		return fmt.Errorf("validation failed: %s must be positive", KeyLocalModelShutdownTimeout)
	}
	if cfg.LocalModel.IdleTimeout <= 0 {
		return fmt.Errorf("validation failed: %s must be positive", KeyLocalModelIdleTimeout)
	}

	if cfg.OpenRouter.Enabled && cfg.OpenRouter.APIKey == "" {
		return fmt.Errorf("validation failed: %s is required when openRouter is enabled; set it via config or OPENROUTER_API_KEY environment variable", KeyOpenRouterAPIKey)
	}

	if cfg.Routing.MaxLocalInputTokens <= 0 {
		return fmt.Errorf("validation failed: %s must be positive", KeyRoutingMaxLocalInputTokens)
	}
	if cfg.Routing.ComplexityThreshold < 0 || cfg.Routing.ComplexityThreshold > 1 {
		return fmt.Errorf("validation failed: %s must be between 0 and 1", KeyRoutingComplexityThreshold)
	}

	if cfg.ShellTool.MaximumOutputCharacters <= 0 {
		return fmt.Errorf("validation failed: %s must be positive", KeyShellToolMaximumOutputCharacters)
	}

	for _, strategy := range routing.AllStrategies() {
		mapping, ok := cfg.ModelRouting.Strategies[string(strategy)]
		if !ok {
			return fmt.Errorf("validation failed: %s has no mapping for strategy %q", KeyModelRoutingStrategies, strategy)
		}
		if isPlaceholder(mapping.Model) {
			return fmt.Errorf("validation failed: %s maps strategy %q to placeholder model %q; replace it with a real OpenRouter model identifier (see https://openrouter.ai/models)", KeyModelRoutingStrategies, strategy, mapping.Model)
		}
	}

	return nil
}

func isPlaceholder(s string) bool {
	return strings.HasPrefix(s, PlaceholderPrefix)
}

// DefaultConfig returns a Config with all default values.
func DefaultConfig() Config {
	return Config{
		LocalModel: LocalModelConfig{
			Host:           DefaultLocalModelHost,
			Port:           DefaultLocalModelPort,
			StartupTimeout: DefaultLocalModelStartupTimeout,
			ShutdownTimeout: DefaultLocalModelShutdownTimeout,
			IdleTimeout:    DefaultLocalModelIdleTimeout,
			AdditionalArgs: []string{},
		},
		OpenRouter: OpenRouterConfig{
			Enabled: DefaultOpenRouterEnabled,
			BaseURL: DefaultOpenRouterBaseURL,
			Timeout: DefaultOpenRouterTimeout,
			Models:  map[string]string{},
		},
		Routing: RoutingConfig{
			MaxLocalInputTokens:       DefaultRoutingMaxLocalInputTokens,
			ComplexityThreshold:       DefaultRoutingComplexityThreshold,
			EnableLocalSelfAssessment: DefaultRoutingEnableLocalSelfAssessment,
			AllowedOverrides:          []string{},
		},
		ShellTool: ShellToolConfig{
			Enabled:                 DefaultShellToolEnabled,
			AllowedWorkingDirectories: []string{},
			AllowedCommands:         []string{},
			Timeout:                 DefaultShellToolTimeout,
			MaximumOutputCharacters: DefaultShellToolMaximumOutputCharacters,
		},
		ModelRouting: ModelRoutingConfig{
			Strategies: map[string]StrategyMapping{},
		},
	}
}
