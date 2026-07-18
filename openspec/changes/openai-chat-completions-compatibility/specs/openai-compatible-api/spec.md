## MODIFIED Requirements

### Requirement: OpenAI-compatible chat completions endpoint
The router SHALL expose `POST /v1/chat/completions` accepting the supported OpenAI Chat Completions request contract and SHALL return a protocol-correct Chat Completions response. Callers SHALL NOT need to know which provider or model served the request, and the public response SHALL NOT contain router-specific fields.

#### Scenario: Auto model request returns a protocol-correct response
- **WHEN** a client posts `{"model":"auto","messages":[{"role":"user","content":"Explain this build error."}],"stream":false}`
- **THEN** the router selects a compatible provider internally and returns `200 OK` with `id`, `object`, `created`, `model`, and a `choices` array whose first element has `message.role`, `message.content`, and `finish_reason`

#### Scenario: Response never leaks provider credentials
- **WHEN** any chat completion succeeds through either the local or cloud provider
- **THEN** the response body contains no API key, authorization header, upstream provider secret, provider name, or router diagnostic metadata

### Requirement: OpenAI-compatible model discovery
The router SHALL expose `GET /v1/models` returning an OpenAI-shaped model list containing the `auto` routing model and configured selectable model identifiers. Model objects SHALL use only documented model-list fields and SHALL NOT claim provider capabilities that the router cannot satisfy.

#### Scenario: Client discovers the automatic routing model
- **WHEN** a client requests `GET /v1/models`
- **THEN** the router returns `200 OK` with an object of `list` and a `data` entry whose ID is `auto`

### Requirement: Server-Sent Events streaming
The router SHALL support `stream: true`, responding with `Content-Type: text/event-stream` and emitting protocol-correct Chat Completions chunk events terminated by `data: [DONE]`. Chunks SHALL be flushed as they arrive from the provider rather than buffered to completion. Streaming SHALL preserve all supported deltas identically whether the local or cloud provider serves the request.

#### Scenario: Streaming response emits incremental chunks
- **WHEN** a client posts a chat completion with `"stream": true`
- **THEN** the router responds with `text/event-stream` and emits chunks carrying supported `choices[0].delta` fields, terminated by `data: [DONE]`

#### Scenario: Streaming reasoning reaches the client
- **WHEN** a local provider emits `reasoning_content` before final content
- **THEN** the router forwards it in `choices[0].delta.reasoning_content` without waiting for final content

#### Scenario: Chunks are flushed, not buffered
- **WHEN** the provider yields the first token well before generation completes
- **THEN** the first chunk reaches the client before generation completes, because chunks are flushed as they arrive

#### Scenario: Provider choice is invisible to a streaming client
- **WHEN** a streaming request is served by the cloud provider rather than the local model
- **THEN** the client receives the same event-stream shape and terminator without provider-specific fields

#### Scenario: Client disconnect cancels upstream work
- **WHEN** a client disconnects part-way through a streamed response
- **THEN** cancellation propagates to the provider and generation stops

### Requirement: Non-streaming responses aggregate the same provider path
Non-streaming requests SHALL be served by aggregating the provider's streamed chunks into one protocol-correct response body, so streaming and non-streaming share one provider code path. The aggregation SHALL preserve supported content, reasoning, refusal, tool-call, finish-reason, log-probability, and usage fields.

#### Scenario: Non-streaming request returns a single aggregated body
- **WHEN** a client posts a chat completion with `"stream": false`
- **THEN** the router returns a single JSON chat-completion response whose content equals the concatenation of the content chunks the provider produced

#### Scenario: Non-streaming local request avoids hidden reasoning
- **WHEN** a local request is non-streaming
- **THEN** the router passes `chat_template_kwargs.enable_thinking: false` to compatible local models so the client receives final content without waiting for hidden reasoning

### Requirement: Tool-calling passthrough
The router SHALL forward OpenAI-compatible `tools`, `tool_choice`, and `parallel_tool_calls` request fields to the selected compatible provider. It SHALL preserve assistant `tool_calls` and tool-result message metadata when forwarding subsequent requests, and return provider tool calls in both streaming and non-streaming OpenAI-compatible responses. The router SHALL NOT execute client-supplied tools itself.

#### Scenario: Tool definitions reach the selected provider
- **WHEN** a client posts a chat completion request containing a `tools` array and `tool_choice`
- **THEN** the selected provider receives the same tool definitions and tool-choice instruction

#### Scenario: Streaming tool calls reach the client
- **WHEN** a provider emits a streaming delta containing `tool_calls`
- **THEN** the router emits that delta to the client without replacing it with text content

#### Scenario: Tool result is forwarded
- **WHEN** a client sends a `tool` role message with its `tool_call_id`
- **THEN** the selected provider receives the tool result and its matching call identifier

### Requirement: Request validation
The router SHALL validate incoming chat requests against its supported Chat Completions contract and reject malformed, unsupported, and incompatible requests using an OpenAI-shaped `400 Bad Request` error that identifies the problem, without starting the local model or contacting any provider.

#### Scenario: Missing messages
- **WHEN** a client posts a chat completion with an empty or absent `messages` array
- **THEN** the router returns `400 Bad Request` and no local process is started and no cloud request is sent

#### Scenario: Unknown role
- **WHEN** a message carries a role outside the supported set
- **THEN** the router returns `400 Bad Request` naming the invalid role
