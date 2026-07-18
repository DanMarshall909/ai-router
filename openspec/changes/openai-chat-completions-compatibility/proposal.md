## Why

The router currently accepts only a small subset of the Chat Completions contract and synthesizes responses that omit or alter fields OpenAI-compatible clients rely on. Clients should be able to use the documented Chat Completions API through the router without provider-specific request rewriting or response parsing.

## What Changes

- Expand `POST /v1/chat/completions` request parsing and validation to support the practical OpenAI Chat Completions request surface, including multimodal content parts, generation controls, structured outputs, metadata, and stream options.
- Preserve supported request fields through routing and provider adapters, rejecting unsupported or unsafe fields with OpenAI-shaped errors rather than silently discarding them.
- Return protocol-correct completion and streaming responses, including generated IDs, usage, finish reasons, tool calls, log probabilities where supported, and optional stream usage events.
- Define deterministic compatibility behavior for local llama.cpp and cloud OpenRouter providers, including capability-aware routing and clear limitations.
- **BREAKING** Remove undocumented router-only response fields from the OpenAI-compatible response envelope; route diagnostics remain available only through structured logs and router administrative interfaces.

## Capabilities

### New Capabilities
- `chat-completions-compatibility`: Defines the supported OpenAI Chat Completions request and response contract, validation, error format, and streaming behavior across providers.

### Modified Capabilities
- `openai-compatible-api`: Replace the minimal OpenAI-shaped endpoint requirements with protocol-compatible request, response, stream, and error requirements.
- `inference-routing`: Add provider capability checks so automatic routing and fallback do not select a provider that cannot satisfy requested Chat Completions features.
- `cloud-fallback`: Preserve request semantics and prohibit fallback when a provider transition would change the requested compatibility contract.

## Impact

- Affected code: HTTP request/response types and validation, routing request types and policy, local llama.cpp adapter, OpenRouter adapter, and API integration tests.
- Affected API: `POST /v1/chat/completions` response envelopes and validation behavior; `GET /v1/models` model metadata.
- No new external runtime dependency is required; provider support is discovered from configured adapter capabilities.
