## Why

Clients that want AI assistance today must each decide which model to call, hold their own API keys, and pay cloud costs for work a local model could handle. Nothing currently manages a local `llama.cpp` model's lifecycle, so it either hogs GPU VRAM permanently or is not running when needed.

`LocalFirst.AiRouter` solves this by presenting a single OpenAI-compatible endpoint that prefers a locally hosted model (Bonsai), starts and unloads it on demand, and falls back to OpenRouter only when local inference is unavailable or the task genuinely needs a more capable cloud model. Callers stay ignorant of model selection entirely, which is what makes the same API reusable by future Android, voice, and Home Assistant clients.

The router is a long-lived process babysitting a child process that can crash, hang, or outlive it. It therefore has to heal itself: detect a wedged model, reclaim a server orphaned by its own crash, and probe recovery without stampeding a still-broken model.

## What Changes

- Create a new Go module for `LocalFirst.AiRouter` — a single static binary (`cmd/airouter`) over `internal/` packages, with routing kept free of I/O — plus PowerShell setup/launch scripts for Windows.
- Expose an OpenAI-compatible `POST /v1/chat/completions` endpoint supporting both `stream: false` and `stream: true` (Server-Sent Events) from day one, so off-the-shelf OpenAI clients work unmodified.
- Expose health endpoints (`/health`, `/health/live`, `/health/ready`), a status endpoint (`GET /api/router/status`), and administrative local-model start/stop endpoints separated from chat traffic by authorization policy.
- Introduce a deterministic routing policy returning an `InferenceStrategy` (`QuickLocal`, `DeepLocal`, `CloudGeneral`, `CloudReasoning`, `CloudCoding`), mapped to concrete providers/models purely through configuration. No cloud model identifier is hard-coded.
- Manage the `llama-server` process: single-flight startup, readiness polling, graceful shutdown with force-kill fallback, unexpected-exit detection, and idle unload after a configurable timeout (default 5 minutes) that never interrupts an active request.
- Add an OpenRouter provider client with configurable base URL, model mappings, timeouts, and an API key sourced from the environment, never source control.
- Add explicit resilience: bounded timeouts, exactly one controlled local→cloud fallback, and a local circuit breaker that prevents restart storms from a crashing process.
- Add self-healing: continuous liveness probing that recycles a hung-but-alive model, adoption of an orphaned `llama-server` at startup, half-open circuit-breaker recovery with bounded exponential backoff, panic-proof background loops, and documented external process supervision.
- Add an `execute_shell_command` tool (PowerShell on Windows, Bash on Linux), **disabled by default**, gated by allow-listed working directories and commands, with a `confirmation_required` pending-action response for non-allow-listed commands.
- Add structured logging (`log/slog`) and in-memory metrics counters for routing decisions, lifecycle events, fallbacks, recoveries, and latency — with API keys, authorization headers, and full prompts excluded by default.
- Bind to loopback by default and provide optional API-key authentication using constant-time comparison for administrative and tool endpoints.

Not breaking: this is a greenfield repository with no existing consumers.

## Capabilities

### New Capabilities
- `openai-compatible-api`: The public `POST /v1/chat/completions` contract, request validation, SSE streaming and non-streaming response shapes, health endpoints, and the administrative start/stop and status endpoints.
- `inference-routing`: The deterministic routing policy, `InferenceStrategy` domain types, routing signals, strategy→model configuration mapping, explicit client overrides, cloud-prohibition policy, and the optional non-authoritative local self-assessment classifier.
- `local-model-lifecycle`: The local model manager — starting `llama-server` with argument-list safety, single-flight startup, readiness detection, state tracking, graceful and forced shutdown, unexpected-exit detection, and the idle-unload loop.
- `cloud-fallback`: The OpenRouter provider client, its configuration and secret handling, the single controlled local→cloud fallback path, the local circuit breaker, timeouts, and cancellation propagation.
- `router-self-healing`: Liveness probing and recycling of a hung model, startup adoption of orphaned local servers, half-open breaker recovery with bounded backoff, panic-proof background loops, external supervision requirements, and observability of every automatic recovery.
- `shell-command-tool`: The `execute_shell_command` tool contract, disabled-by-default posture, directory and command allow lists, path-traversal rejection, destructive-pattern blocking, output caps, and the bound, expiring, non-durable `confirmation_required` pending-action state.
- `router-observability`: Structured logging fields, redaction rules, metrics counters, GPU diagnostics, and the content of `GET /api/router/status`.
- `router-security`: Loopback binding default, API-key authentication with constant-time comparison, the authorization split between chat and administrative endpoints, startup configuration validation, and placeholder cloud model identifiers that fail loudly.

### Modified Capabilities

None — `openspec/specs/` is currently empty.

## Impact

- **New code**: the entire module under `cmd/` and `internal/`, plus `scripts/`, `.gitignore`, `README.md` (with Mermaid architecture diagram, threat model, design decisions, supervisor setup, known limitations, roadmap), and `config.example.json`.
- **Runtime dependencies**: a Go toolchain to build; a local `llama-server` executable and a Bonsai GGUF model file (both configured by path, not vendored); optional OpenRouter account and API key. The build output is one static binary with no runtime installed at all.
- **Library dependencies**: `golang.org/x/sync/singleflight` for single-flight startup and `github.com/stretchr/testify/require` for test assertions. Everything else is the standard library — `net/http`, `log/slog`, `encoding/json`, `os/exec`, `crypto/subtle`, `context`.
- **Test dependencies**: `testing` plus `testify/require` for assertions only. No mocking framework — `net/http/httptest`, hand-written fakes behind interfaces, temporary scripts, and injected clocks instead. `testify/mock` is excluded deliberately.
- **Deliberately excluded**: Temporal, and any large agent/orchestration framework. Also out of scope for this proof of concept: Android app, wake word, STT/TTS, camera, image understanding, Gmail/LinkedIn/Facebook/Home Assistant integrations, persistent conversational memory, autonomous agents, remote shell access, a full confirmation UI, and model-quality learning.
- **Security surface**: the shell tool and administrative endpoints are the primary risk; both are off or authenticated by default, and the README must state that pattern blocking is not a complete security boundary.
