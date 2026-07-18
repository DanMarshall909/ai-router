## MODIFIED Requirements

### Requirement: Single controlled local-to-cloud fallback
When local inference fails with a qualifying failure and cloud fallback is both permitted and configured, the router SHALL retry the request exactly once through the mapped cloud strategy only when that strategy satisfies the original request's required Chat Completions capabilities. The response SHALL indicate that fallback occurred through internal metadata or logs. Non-idempotent or unsafe tool actions SHALL NOT be retried automatically.

#### Scenario: Local failure falls back to cloud once
- **WHEN** local inference cannot start or returns a qualifying failure and a capability-compatible cloud fallback is permitted and configured
- **THEN** the request is sent to the mapped OpenRouter strategy, the response is returned to the client, and the recorded routing decision marks `IsFallback` as true

#### Scenario: Incompatible fallback is not attempted
- **WHEN** local inference fails and the configured cloud fallback cannot satisfy the original request's required capabilities
- **THEN** the router returns an OpenAI-shaped provider-unavailable error without sending a semantically degraded cloud request

### Requirement: Fallback is only possible before the first byte reaches the client
Fallback SHALL be available only while no response bytes have been written to the client. Once the router has flushed the first chunk of a streamed response, the status code and partial content are committed and the request SHALL NOT be re-dispatched to another provider. A local failure after that point SHALL terminate the stream rather than fall back.

#### Scenario: Streaming failure before the first chunk falls back normally
- **WHEN** local inference fails during startup or before yielding its first chunk of a streaming request and a capability-compatible cloud fallback is available
- **THEN** the router falls back to the mapped cloud strategy and the client receives a complete event-stream with no partial local content

#### Scenario: Empty local completion falls back normally
- **WHEN** the local provider ends a completion before yielding content, reasoning content, refusal content, or a tool call
- **THEN** the router treats it as a pre-first-chunk failure and falls back to cloud

#### Scenario: Streaming failure after the first chunk terminates the stream
- **WHEN** the local provider fails mid-generation after chunks have already been flushed to the client
- **THEN** the router does not re-dispatch to the cloud, terminates the stream, and records the failure, because the response is already committed

#### Scenario: Non-streaming keeps the full fallback window
- **WHEN** a non-streaming request fails at any point before the aggregated response is written
- **THEN** the full fallback path remains available, because aggregation means no bytes have reached the client

#### Scenario: Mid-stream failure still counts toward the circuit breaker
- **WHEN** the local provider fails mid-generation after chunks have been flushed
- **THEN** the failure increments the consecutive-failure count for the local circuit breaker

#### Scenario: Fallback does not cascade
- **WHEN** the single cloud fallback attempt also fails
- **THEN** the router returns an error and does not attempt further retries, because retry storms are prohibited

#### Scenario: Unsafe tool action is not retried
- **WHEN** a request whose execution included a non-idempotent tool action fails locally
- **THEN** the router does not automatically re-run the request through the cloud
