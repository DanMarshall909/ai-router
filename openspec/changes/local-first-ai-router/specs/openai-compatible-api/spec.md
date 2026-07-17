## ADDED Requirements

### Requirement: OpenAI-compatible chat completions endpoint
The router SHALL expose `POST /v1/chat/completions` accepting an OpenAI-shaped request body containing `model`, `messages`, and optional `stream`, and SHALL return an OpenAI-shaped chat-completion response. Callers SHALL NOT need to know which provider or model served the request.

#### Scenario: Auto model request returns an OpenAI-shaped response
- **WHEN** a client posts `{"model":"auto","messages":[{"role":"user","content":"Explain this build error."}],"stream":false}`
- **THEN** the router selects a provider internally and returns `200 OK` with a body containing `id`, `object`, `created`, `model`, and a `choices` array whose first element has `message.role` and `message.content`

#### Scenario: Response never leaks provider credentials
- **WHEN** any chat completion succeeds through either the local or cloud provider
- **THEN** the response body contains no API key, authorization header, or upstream provider secret

### Requirement: Server-Sent Events streaming
The router SHALL support `stream: true`, responding with `Content-Type: text/event-stream` and emitting OpenAI-compatible `data: {chunk}` events terminated by `data: [DONE]`. Chunks SHALL be flushed as they arrive from the provider rather than buffered to completion. Streaming SHALL work identically whether the local or cloud provider serves the request.

#### Scenario: Streaming response emits incremental chunks
- **WHEN** a client posts a chat completion with `"stream": true`
- **THEN** the router responds with `text/event-stream` and emits chunks carrying `choices[0].delta.content`, terminated by `data: [DONE]`

#### Scenario: Chunks are flushed, not buffered
- **WHEN** the provider yields the first token well before generation completes
- **THEN** the first chunk reaches the client before generation completes, because chunks are flushed as they arrive

#### Scenario: Provider choice is invisible to a streaming client
- **WHEN** a streaming request is served by the cloud provider rather than the local model
- **THEN** the client receives the same event-stream shape and terminator

#### Scenario: Client disconnect cancels upstream work
- **WHEN** a client disconnects part-way through a streamed response
- **THEN** cancellation propagates to the provider and generation stops

### Requirement: Non-streaming responses aggregate the same provider path
Non-streaming requests SHALL be served by aggregating the provider's streamed chunks into a single response body, so streaming and non-streaming share one provider code path.

#### Scenario: Non-streaming request returns a single aggregated body
- **WHEN** a client posts a chat completion with `"stream": false`
- **THEN** the router returns a single JSON chat-completion response whose content equals the concatenation of the chunks the provider produced

### Requirement: Request validation
The router SHALL validate incoming chat requests and reject malformed ones with `400 Bad Request` and a message identifying the problem, without starting the local model or contacting any provider.

#### Scenario: Missing messages
- **WHEN** a client posts a chat completion with an empty or absent `messages` array
- **THEN** the router returns `400 Bad Request` and no local process is started and no cloud request is sent

#### Scenario: Unknown role
- **WHEN** a message carries a role outside the supported set
- **THEN** the router returns `400 Bad Request` naming the invalid role

### Requirement: Tool-calling passthrough
The router SHALL forward OpenAI-compatible `tools`, `tool_choice`, and `parallel_tool_calls` request fields to the selected provider. It SHALL preserve assistant `tool_calls` and tool-result message metadata when forwarding subsequent requests, and return provider tool calls in both streaming and non-streaming OpenAI-compatible responses. The router SHALL NOT execute client-supplied tools itself.

#### Scenario: Tool definitions reach the selected provider
- **WHEN** a client posts a chat completion request containing a `tools` array and `tool_choice`
- **THEN** the selected provider receives the same tool definitions and tool-choice instruction

#### Scenario: Streaming tool calls reach the client
- **WHEN** a provider emits a streaming delta containing `tool_calls`
- **THEN** the router emits that delta to the client without replacing it with text content

#### Scenario: Tool result is forwarded
- **WHEN** a client sends a `tool` role message with its `tool_call_id`
- **THEN** the selected provider receives the tool result and its matching call identifier

### Requirement: Health endpoints
The router SHALL expose `GET /health`, `GET /health/live`, and `GET /health/ready`. Liveness SHALL reflect only that the process is running. Readiness SHALL reflect that configuration validated and at least one provider path is usable.

#### Scenario: Liveness independent of local model state
- **WHEN** the local model is stopped and `GET /health/live` is requested
- **THEN** the router returns a healthy result, because liveness does not depend on the local model being loaded

#### Scenario: Readiness fails when no provider is usable
- **WHEN** cloud access is disabled and the local model is faulted
- **THEN** `GET /health/ready` reports an unhealthy result

### Requirement: Administrative local-model endpoints
The router SHALL expose `POST /api/router/local-model/start` and `POST /api/router/local-model/stop` as administrative endpoints, separated from chat traffic by authorization policy, allowing an operator to control the local model lifecycle explicitly.

#### Scenario: Explicit start
- **WHEN** an authorised operator posts to `/api/router/local-model/start` while the model is stopped
- **THEN** the router starts the local model and reports the resulting lifecycle state

#### Scenario: Explicit stop
- **WHEN** an authorised operator posts to `/api/router/local-model/stop` while the model is ready and idle
- **THEN** the router stops the process and the reported state becomes `Stopped`

#### Scenario: Administrative endpoints are not reachable under the chat policy
- **WHEN** a caller authorised only for chat traffic posts to `/api/router/local-model/start`
- **THEN** the router rejects the request with an authorization failure and does not start the process
