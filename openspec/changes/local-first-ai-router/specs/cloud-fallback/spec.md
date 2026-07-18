## ADDED Requirements

### Requirement: OpenRouter provider client
The router SHALL provide an OpenRouter client whose API key is read from configuration or environment variables and never stored in source control. Base URL, model mappings, and request timeout SHALL be configurable. The client SHALL support cancellation, translate upstream errors into useful outcomes, and emit structured logs with secrets removed. Application title and referer headers SHALL be optional and configuration-driven. The client SHALL make no outbound call when cloud use is disabled.

#### Scenario: Cloud call carries configured headers
- **WHEN** an application title is configured and a cloud request is dispatched
- **THEN** the request carries the configured title header and the configured base URL is used

#### Scenario: Disabled cloud makes no call
- **WHEN** `openRouter.enabled` is false and any code path attempts a cloud dispatch
- **THEN** no outbound HTTP request is made and the caller receives a disabled-provider outcome

#### Scenario: Upstream error translated without leaking the key
- **WHEN** OpenRouter returns an authentication or rate-limit error
- **THEN** the router returns a useful error and the logged detail contains no API key or authorization header

### Requirement: Single controlled local-to-cloud fallback
When local inference fails with a qualifying failure and cloud fallback is both permitted and configured, the router SHALL retry the request exactly once through the mapped cloud strategy. The response SHALL indicate that fallback occurred through internal metadata or logs. Non-idempotent or unsafe tool actions SHALL NOT be retried automatically.

#### Scenario: Local failure falls back to cloud once
- **WHEN** local inference cannot start or returns a qualifying failure and cloud fallback is permitted and configured
- **THEN** the request is sent to the mapped OpenRouter strategy, the response is returned to the client, and the recorded routing decision marks `IsFallback` as true

### Requirement: Fallback is only possible before the first byte reaches the client
Fallback SHALL be available only while no response bytes have been written to the client. Once the router has flushed the first chunk of a streamed response, the status code and partial content are committed and the request SHALL NOT be re-dispatched to another provider. A local failure after that point SHALL terminate the stream rather than fall back.

#### Scenario: Streaming failure before the first chunk falls back normally
- **WHEN** local inference fails during startup or before yielding its first chunk of a streaming request
- **THEN** the router falls back to the mapped cloud strategy and the client receives a complete event-stream with no partial local content

#### Scenario: Empty local completion falls back normally
- **WHEN** the local provider ends a completion before yielding content or a tool call
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

### Requirement: Local circuit breaker
The router SHALL open a local-provider circuit breaker after a configurable number of consecutive local failures. While open, the router SHALL NOT repeatedly start a crashing local process and SHALL instead use the permitted fallback or return an error. Recovery from the open state is specified by the `router-self-healing` capability.

#### Scenario: Breaker opens after consecutive failures
- **WHEN** the configured number of consecutive local failures occurs
- **THEN** the breaker opens and the state change is logged

#### Scenario: Open breaker prevents restart storms
- **WHEN** another local-eligible request arrives during the cooldown
- **THEN** no local process start is attempted and the request uses the permitted fallback or returns an error

### Requirement: Bounded timeouts and cancellation propagation
Every provider call SHALL be bounded by a configured timeout, and client cancellation SHALL propagate into local startup, local inference, and cloud inference so unnecessary work is stopped where supported.

#### Scenario: Cancellation during local startup
- **WHEN** the client cancels while local startup is in progress
- **THEN** the awaiting request is cancelled and the router does not continue producing a response for it

#### Scenario: Cancellation during cloud inference
- **WHEN** the client cancels while a cloud request is in flight
- **THEN** the outbound HTTP request is cancelled
