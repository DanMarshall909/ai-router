package config_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/DanMarshall909/ai-router/internal/config"
	"github.com/stretchr/testify/require"
)

func validStrategyMap() map[string]config.StrategyMapping {
	return map[string]config.StrategyMapping{
		"QuickLocal":    {Provider: "local", Model: "bonsai"},
		"DeepLocal":     {Provider: "local", Model: "bonsai"},
		"CloudGeneral":  {Provider: "openrouter", Model: "anthropic/claude-3.5-sonnet"},
		"CloudReasoning": {Provider: "openrouter", Model: "anthropic/claude-3.5-sonnet"},
		"CloudCoding":   {Provider: "openrouter", Model: "anthropic/claude-3.5-sonnet"},
	}
}

func validConfig() config.Config {
	cfg := config.DefaultConfig()
	cfg.LocalModel.ExecutablePath = "/usr/bin/llama-server"
	cfg.LocalModel.ModelPath = "/models/bonsai.gguf"
	cfg.ModelRouting.Strategies = validStrategyMap()
	return cfg
}

func TestValidateSucceedsWithValidConfig(t *testing.T) {
	err := config.Validate(validConfig())
	require.NoError(t, err, "because a fully populated valid config should pass validation")
}

func TestValidateFailsOnEmptyExecutablePath(t *testing.T) {
	cfg := validConfig()
	cfg.LocalModel.ExecutablePath = ""
	err := config.Validate(cfg)
	require.ErrorContains(t, err, "executablePath", "because missing executable path should be named in the error")
}

func TestValidateFailsOnEmptyModelPath(t *testing.T) {
	cfg := validConfig()
	cfg.LocalModel.ModelPath = ""
	err := config.Validate(cfg)
	require.ErrorContains(t, err, "modelPath", "because missing model path should be named in the error")
}

func TestValidateFailsOnInvalidPort(t *testing.T) {
	cfg := validConfig()
	cfg.LocalModel.Port = 0
	err := config.Validate(cfg)
	require.ErrorContains(t, err, "port", "because port 0 should fail validation")
}

func TestValidateFailsOnCloudEnabledWithoutAPIKey(t *testing.T) {
	cfg := validConfig()
	cfg.OpenRouter.Enabled = true
	cfg.OpenRouter.APIKey = ""
	err := config.Validate(cfg)
	require.ErrorContains(t, err, "apiKey", "because enabled cloud without API key should fail")
	require.ErrorContains(t, err, "OPENROUTER_API_KEY", "because error should mention the env var")
}

func TestValidateSucceedsWithCloudDisabledAndNoAPIKey(t *testing.T) {
	cfg := validConfig()
	cfg.OpenRouter.Enabled = false
	cfg.OpenRouter.APIKey = ""
	err := config.Validate(cfg)
	require.NoError(t, err, "because cloud disabled means no API key is needed")
}

func TestValidateFailsOnUnmappedStrategy(t *testing.T) {
	cfg := validConfig()
	delete(cfg.ModelRouting.Strategies, "CloudCoding")
	err := config.Validate(cfg)
	require.ErrorContains(t, err, "CloudCoding", "because unmapped strategy should be named")
}

func TestValidateFailsOnPlaceholderModel(t *testing.T) {
	cfg := validConfig()
	cfg.ModelRouting.Strategies["CloudGeneral"] = config.StrategyMapping{
		Provider: "openrouter",
		Model:    "<configure-me: see https://openrouter.ai/models>",
	}
	err := config.Validate(cfg)
	require.ErrorContains(t, err, "CloudGeneral", "because placeholder model should be named")
	require.ErrorContains(t, err, "configure-me", "because error should mention the placeholder")
}

func TestLoadReadsConfigFile(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.json")
	cfg := validConfig()
	data, err := json.Marshal(cfg)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(cfgPath, data, 0644))

	loaded, err := config.Load(cfgPath)
	require.NoError(t, err, "because a valid config file should load successfully")
	require.Equal(t, "/usr/bin/llama-server", loaded.LocalModel.ExecutablePath)
}

func TestLoadFailsOnMissingFile(t *testing.T) {
	_, err := config.Load("/nonexistent/config.json")
	require.Error(t, err, "because loading a nonexistent file should fail")
}

func TestLoadFailsOnInvalidJSON(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.json")
	require.NoError(t, os.WriteFile(cfgPath, []byte("{invalid"), 0644))

	_, err := config.Load(cfgPath)
	require.Error(t, err, "because invalid JSON should fail to load")
}

func TestEnvOverridesAreApplied(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.json")
	cfg := validConfig()
	data, err := json.Marshal(cfg)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(cfgPath, data, 0644))

	t.Setenv("LOCAL_MODEL_PORT", "9999")
	t.Setenv("OPENROUTER_API_KEY", "test-key-123")

	loaded, err := config.Load(cfgPath)
	require.NoError(t, err)
	require.Equal(t, 9999, loaded.LocalModel.Port, "because env override should set port")
	require.Equal(t, "test-key-123", loaded.OpenRouter.APIKey, "because env override should set API key")
}

func TestPlaceholderConstantHasCorrectPrefix(t *testing.T) {
	require.Contains(t, config.PlaceholderPrefix, "configure-me", "because placeholder prefix should contain configure-me")
}
