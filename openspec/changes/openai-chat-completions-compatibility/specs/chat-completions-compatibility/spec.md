## ADDED Requirements

### Requirement: Explicit Chat Completions support matrix
The router SHALL support the documented Chat Completions fields represented by its protocol models and SHALL reject every unimplemented, malformed, or provider-incompatible field with an OpenAI-shaped `invalid_request_error` that identifies the parameter. The router SHALL NOT silently discard a supplied request field.

#### Scenario: Unsupported field is rejected
- **WHEN** a client sends a Chat Completions request containing a field outside the router's support matrix
- **THEN** the router returns `400 Bad Request` with an `error` object whose `type` is `invalid_request_error` and whose `param` identifies that field, without dispatching a provider request

### Requirement: Supported request semantics are preserved
The router SHALL normalize and preserve supported Chat Completions request semantics through routing and the selected provider, including message roles and content parts, `temperature`, `top_p`, `max_completion_tokens`, `stop`, `n`, `presence_penalty`, `frequency_penalty`, `logprobs`, `top_logprobs`, `seed`, `response_format`, `tools`, `tool_choice`, `parallel_tool_calls`, `user`, `metadata`, and `stream_options`.

#### Scenario: Generation controls reach a compatible provider
- **WHEN** a request contains supported generation controls and the selected provider supports them
- **THEN** the provider request contains equivalent values and the router does not substitute defaults for supplied values

#### Scenario: Multimodal request requires compatible routing
- **WHEN** a user message contains an image URL or input-audio content part
- **THEN** the router dispatches only to a provider that declares the required modality or returns an OpenAI-shaped compatibility error before inference begins

### Requirement: Provider capability compatibility
The router SHALL derive required capabilities from each normalized request and SHALL dispatch only to a provider that declares support for all required capabilities. An explicit provider/model request that cannot meet the profile SHALL fail without contacting that provider.

#### Scenario: Explicit incompatible model is rejected
- **WHEN** a client explicitly selects a model that does not support requested strict JSON schema output
- **THEN** the router returns an OpenAI-shaped error identifying `response_format` and does not send the request to that model

#### Scenario: Automatic request selects compatible cloud provider
- **WHEN** an automatic request requires tools and the local adapter lacks tool-call support while cloud access is permitted
- **THEN** the router selects a configured cloud strategy that supports tools

### Requirement: Protocol-correct completion responses
The router SHALL return a Chat Completions response containing a unique `chatcmpl-` ID, `object`, request-start `created` timestamp, resolved `model`, `choices`, and provider-reported `usage` when available. It SHALL preserve assistant content, refusal content, tool calls, finish reason, and log probabilities when produced by the provider. Public responses SHALL NOT contain router diagnostic fields.

#### Scenario: Completion has no router diagnostics
- **WHEN** a non-streaming completion succeeds
- **THEN** its response uses the Chat Completions response envelope and contains no `debug`, provider name, or internal routing metadata field

#### Scenario: Tool completion retains finish reason
- **WHEN** a provider finishes with one or more tool calls
- **THEN** the response choice contains those calls and `finish_reason` is `tool_calls`

### Requirement: Protocol-correct streamed responses
The router SHALL emit Chat Completions SSE chunks with a stable completion ID, `chat.completion.chunk` object, request-start timestamp, and resolved model. It SHALL preserve role, content, refusal, reasoning content, tool-call, log-probability, and finish-reason deltas. When `stream_options.include_usage` is true and usage is available, it SHALL emit one final usage chunk before `[DONE]`.

#### Scenario: Streamed usage is emitted before completion marker
- **WHEN** a streaming request sets `stream_options.include_usage` to true and the provider reports usage
- **THEN** the router sends a final chunk with `usage`, an empty `choices` array, and then sends `data: [DONE]`

#### Scenario: Committed stream failure is protocol shaped
- **WHEN** a provider fails after at least one stream chunk has been flushed
- **THEN** the router emits an SSE event containing an OpenAI-shaped `error` object and terminates the stream without issuing a second completion

### Requirement: OpenAI-shaped errors
The router SHALL return errors as `{"error":{"message":...,"type":...,"param":...,"code":...}}`, omitting optional null fields. It SHALL use `400` for malformed or unsupported requests, `401` for authentication failures, `429` for rate limiting, and `5xx` for unavailable or failed compatible providers without leaking credentials or internal provider URLs.

#### Scenario: Invalid message content reports its parameter
- **WHEN** a message content part is structurally invalid
- **THEN** the router returns `400 Bad Request` with an `invalid_request_error` whose `param` identifies the invalid message field
