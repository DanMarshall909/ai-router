# ai-router

Local-first AI inference router with cloud fallback. Routes requests to a local llama.cpp model, falling back to OpenRouter on failure.

## Quick Start

### 1. Build

```bash
export PATH="/home/dan/go/bin:$PATH"
go build -o ai-router ./cmd/airouter/
```

### 2. Configure

Config lives at `~/.config/ai-router/config.json`:

```json
{
  "localModel": {
    "executablePath": "/path/to/llama-server",
    "modelPath": "/path/to/model.gguf",
    "host": "127.0.0.1",
    "port": 18080,
    "ldLibraryPath": "/path/to/cuda/libs",
    "additionalArgs": ["-ngl", "99", "-c", "2048"]
  },
  "openRouter": {
    "enabled": false,
    "apiKey": "",
    "autoModel": "openrouter/auto",
    "fallbacks": ["anthropic/claude-sonnet-4.5"],
    "costQualityTradeoff": 7,
    "allowedModels": ["anthropic/*", "openai/*"]
  },
  "routing": {
    "maxLocalInputTokens": 4096,
    "complexityThreshold": 0.7
  },
  "modelRouting": {
    "strategies": {
      "QuickLocal": { "provider": "local", "model": "bonsai" },
      "CloudGeneral": { "provider": "openrouter", "model": "anthropic/claude-3.5-sonnet" }
    }
  }
}
```

### 3. Run

```bash
./ai-router
```

Starts on `localhost:<port+1>` (default 18081). The local model launches automatically on first request.

### Update the user service

When AI Router runs as the `ai-router` user service, update it with:

```bash
./scripts/Update-Router.sh
```

The script builds a replacement binary from the current checkout, writes or refreshes the user service, enables it, and restarts it. By default it uses `$XDG_CONFIG_HOME/ai-router/config.json` (or `~/.config/ai-router/config.json`); set `AI_ROUTER_CONFIG` to use another configuration path.

To capture request and response traces while diagnosing an issue, use:

```bash
./scripts/Update-Router.sh --debug-log
```

This writes trace files to `~/.local/state/ai-router/debug/`. Disable tracing after diagnosis by running the script without `--debug-log`, because traces can contain prompt and response content.

### Tail user-service logs

Follow AI Router's live service logs with:

```bash
journalctl --user -u ai-router -f
```

### GitKraken AI

Configure GitKraken's **Custom URL** provider with:

- Base URL: `http://127.0.0.1:18081/v1`
- API key: any non-empty placeholder, such as `ai-router`
- Model: `auto`

### 4. Query

```bash
# Non-streaming
curl http://localhost:18081/v1/chat/completions \
  -H "Content-Type: application/json" \
  -d '{"model":"auto","messages":[{"role":"user","content":"hello"}]}'

# Streaming
curl http://localhost:18081/v1/chat/completions \
  -H "Content-Type: application/json" \
  -d '{"model":"auto","messages":[{"role":"user","content":"hello"}],"stream":true}'

Set optional `complexity` from `0` (simple) to `1` (complex). A value at or above
`routing.complexityThreshold` routes an automatic request to cloud reasoning:

```bash
curl http://localhost:18081/v1/chat/completions \
  -H "Content-Type: application/json" \
  -d '{"model":"auto","complexity":0.9,"messages":[{"role":"user","content":"Compare these designs."}]}'
```

## Routing

- `model: "auto"` → local first, cloud fallback on pre-first-chunk failure
- `model: "anthropic/claude-3.5-sonnet"` → direct to OpenRouter

### OpenRouter Features

- `autoModel: "openrouter/auto"` — let OpenRouter pick the best model
- `fallbacks` — automatic failover chain
- `costQualityTradeoff` — 0 (quality) to 10 (cost)
- `allowedModels` — wildcard patterns to restrict auto-routing
- `session_id` in request — pin model/provider for multi-turn conversations

## Architecture

```
Client → ai-router (HTTP) → Dispatcher → LocalProvider (llama-server)
                                       → CloudProvider (OpenRouter)
```

- **Dispatcher**: Routes with single local→cloud fallback, no cascade retries
- **LocalProvider**: Streams from llama-server, reports pre/post-chunk failures
- **CloudProvider**: Streams from OpenRouter with auth/rate-limit error handling
- **ProcessManager**: Starts llama-server on-demand, health polling, graceful stop

## Configuration

Override config with environment variables:

| Variable | Config Key |
|----------|------------|
| `LOCAL_MODEL_EXECUTABLE_PATH` | `localModel.executablePath` |
| `LOCAL_MODEL_MODEL_PATH` | `localModel.modelPath` |
| `LOCAL_MODEL_PORT` | `localModel.port` |
| `OPENROUTER_API_KEY` | `openRouter.apiKey` |
| `OPENROUTER_ENABLED` | `openRouter.enabled` |

## Testing

```bash
go test ./... -race -count=1
```
