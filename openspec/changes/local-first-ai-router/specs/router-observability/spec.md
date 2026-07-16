## ADDED Requirements

### Requirement: Structured logging of routing and lifecycle events
The router SHALL emit structured logs carrying request correlation ID, chosen strategy, chosen provider, routing reason, local startup duration, inference duration, fallback occurrence, process exits, circuit-breaker changes, idle unloads, and tool requests and outcomes.

#### Scenario: Correlated routing log
- **WHEN** a chat request is served
- **THEN** a structured log entry records the correlation ID, strategy, provider, reason, and inference duration

#### Scenario: Lifecycle events logged
- **WHEN** the local model starts, exits unexpectedly, or is unloaded for idleness
- **THEN** a structured log entry records the event and its duration or reason

### Requirement: Sensitive data is never logged
Logs SHALL NOT contain API keys, authorization headers, complete email or personal-service content, full prompts by default, or command output that may contain secrets.

#### Scenario: Secrets absent from logs
- **WHEN** requests containing secrets or personal content are processed and logs are inspected
- **THEN** API keys, authorization headers, and full prompt content are absent

### Requirement: Metrics counters
The router SHALL publish `System.Diagnostics.Metrics` counters (or equivalent in-memory metrics) for local requests, cloud requests, local startup count, local startup failures, fallbacks, local model unloads, routing decisions by strategy, and request latency. Latency SHALL be recorded as both time-to-first-chunk and total duration, because the first is what a streaming client perceives.

#### Scenario: Time to first chunk recorded separately
- **WHEN** a streaming request is served after a cold local start
- **THEN** time-to-first-chunk and total duration are both recorded, and the former includes the startup wait

#### Scenario: Fallback increments its counter
- **WHEN** a request falls back from local to cloud
- **THEN** the fallback counter and the cloud request counter both increment

#### Scenario: Decisions counted by strategy
- **WHEN** requests are routed to different strategies
- **THEN** the routing-decision counter is incremented with the strategy as a dimension

### Requirement: Router status endpoint content
`GET /api/router/status` SHALL report local model state, local model process ID when running, selected local model profile, current local request count, timestamp of last local activity, configured idle timeout, whether OpenRouter is configured, last routing decision summary, current circuit-breaker state, and approximate GPU availability when detectable. It SHALL NOT return secrets, API keys, complete prompts, or sensitive message content.

#### Scenario: Status reports lifecycle and routing state
- **WHEN** an authorised caller requests `/api/router/status` while the router is running
- **THEN** the response reports the local model state, idle timeout, circuit-breaker state, and last routing decision summary

#### Scenario: Status reveals configuration presence, not values
- **WHEN** OpenRouter is configured with an API key
- **THEN** the status response indicates only that OpenRouter is configured and contains no key material

#### Scenario: GPU availability is optional
- **WHEN** GPU availability cannot be detected on the host
- **THEN** the status response omits or reports the GPU field as unknown rather than failing

### Requirement: GPU availability is diagnostic and cheaply obtained
GPU availability SHALL be reported for diagnostics only and SHALL NOT feed any routing decision. Detection SHALL be attempted via `nvidia-smi` with a fixed argument list, behind a swappable abstraction, and SHALL be cached with a short TTL so that polling the status endpoint does not spawn a process per call. Non-NVIDIA and undetectable hosts SHALL report unknown.

#### Scenario: Detection result is cached
- **WHEN** the status endpoint is polled repeatedly within the cache TTL
- **THEN** detection runs at most once for the TTL window

#### Scenario: Missing detector degrades quietly
- **WHEN** `nvidia-smi` is absent or exits non-zero
- **THEN** GPU availability reports unknown, the status endpoint still succeeds, and no error is surfaced to the caller

#### Scenario: Routing ignores GPU availability
- **WHEN** GPU availability is unknown or reports no free VRAM
- **THEN** routing decisions are unaffected, because the field is diagnostic only
