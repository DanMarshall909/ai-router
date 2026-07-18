# Local Model Lifecycle

## Purpose
Define on-demand local model process management and lifecycle safety.

## Requirements

### Requirement: On-demand local model startup
A local model manager SHALL start the configured `llama-server` executable when a local-eligible request arrives and the model is stopped, wait for the llama.cpp HTTP health endpoint to report readiness, and only then forward the request. It SHALL wait no longer than the configured startup timeout. Arguments SHALL be passed as individual argument-list entries, never by building a command string.

#### Scenario: Starts once and forwards locally
- **WHEN** the local model is stopped and a local-eligible chat request arrives
- **THEN** exactly one process is started, readiness is awaited, and the request is forwarded to the local server

#### Scenario: Startup timeout is bounded
- **WHEN** the local server never reports readiness
- **THEN** startup is abandoned once the configured startup timeout elapses and the request follows the configured fallback path

#### Scenario: Arguments are not shell-concatenated
- **WHEN** the model path or additional arguments contain spaces or quotes
- **THEN** each argument is passed as a distinct argument-list entry and reaches the process intact

### Requirement: Single-flight startup
Concurrent local-eligible requests arriving while the model is stopped or starting SHALL share one startup operation. Duplicate concurrent startup attempts SHALL be rejected or coalesced. The process SHALL NOT be restarted per request.

#### Scenario: Five concurrent requests start one process
- **WHEN** five local-eligible requests arrive concurrently with the model stopped
- **THEN** only one llama.cpp process is started and all five requests share the single startup result

#### Scenario: Ready model is reused
- **WHEN** the local model is already ready and another local request arrives
- **THEN** the existing process is reused and no new process is started

### Requirement: Lifecycle state is tracked and exposed
The manager SHALL track and expose local model state as one of `Stopped`, `Starting`, `Ready`, `Stopping`, `Faulted`, together with the process ID while running. State transitions SHALL be safe under concurrency, and locks SHALL NOT be held while awaiting network or process operations.

Busyness SHALL NOT be a lifecycle state. `llama-server` serves concurrent requests, and the active-request count is already tracked for the idle guard, so busyness SHALL be exposed as a derived property (`IsBusy` ⇔ active request count > 0) over that single source of truth.

#### Scenario: Busyness is derived, not stored
- **WHEN** a local request is in flight
- **THEN** the exposed state remains `Ready` and the derived busy property reports true, because the active-request count is the single source of truth

#### Scenario: Concurrent requests do not change lifecycle state
- **WHEN** several local requests are in flight simultaneously
- **THEN** the lifecycle state remains `Ready` throughout and the active-request count reflects the number in flight

#### Scenario: State follows a successful start
- **WHEN** a start begins and completes successfully
- **THEN** the exposed state transitions from `Stopped` through `Starting` to `Ready` and the process ID becomes available

#### Scenario: Failed start faults the manager
- **WHEN** the executable cannot be launched or never becomes ready
- **THEN** the exposed state becomes `Faulted` and the failure reason is logged

### Requirement: Graceful shutdown with forced termination fallback
The manager SHALL terminate the process gracefully, force-kill the process tree once the configured shutdown timeout elapses, and release all process-related resources.

#### Scenario: Graceful stop
- **WHEN** a stop is requested and the process exits within the shutdown timeout
- **THEN** the state becomes `Stopped` and no force-kill is issued

#### Scenario: Unresponsive process is force-killed
- **WHEN** the process does not exit within the configured shutdown timeout
- **THEN** the process tree is force-killed and the state becomes `Stopped`

### Requirement: Unexpected exit detection
The manager SHALL capture standard output and standard error, detect unexpected process exit, and reflect it in lifecycle state rather than continuing to route requests to a dead server.

#### Scenario: Crash observed
- **WHEN** the local process exits without a stop having been requested
- **THEN** the state becomes `Faulted`, the exit is logged, and subsequent local requests do not target the dead process

### Requirement: Idle unloading releases GPU VRAM
A hosted background service SHALL stop the local model when it is running, no local requests are active, the configured idle timeout has elapsed since last local activity, and no lifecycle transition is already in progress. The default proof-of-concept idle timeout SHALL be 5 minutes and SHALL be configurable. Last-activity SHALL be updated when a local request begins, when it completes, and when each generated chunk is received.

#### Scenario: Idle model is unloaded
- **WHEN** the model is ready, no request is active, and the idle timeout elapses
- **THEN** the process is stopped and the state becomes `Stopped`

#### Scenario: Active request protects the model
- **WHEN** a local request is running and the idle timeout elapses
- **THEN** the process is not stopped until the active request completes

#### Scenario: Activity resets the timer
- **WHEN** a local request begins before the idle timeout elapses
- **THEN** the unload is deferred and the timeout is measured from the new last-activity timestamp

#### Scenario: No unload during a transition
- **WHEN** the idle timeout elapses while the model is `Starting` or `Stopping`
- **THEN** the idle service takes no action, because a lifecycle transition is already in progress

### Requirement: Local model client error classification
The local OpenAI-compatible client SHALL send chat-completion requests to `llama-server`, apply configured timeouts, preserve cancellation, and distinguish startup failure, connection failure, timeout, invalid response, and model-generation failure as separate outcomes. It SHALL NOT log full prompts by default. Failures SHALL be distinguishable as occurring before or after the first generated chunk, so the dispatcher can decide whether fallback remains available.

#### Scenario: Timeout distinguished from connection failure
- **WHEN** the local server accepts the connection but does not respond within the request timeout
- **THEN** the client reports a timeout outcome distinct from a connection failure

#### Scenario: Invalid response distinguished from generation failure
- **WHEN** the local server returns a body that is not a valid chat-completion response
- **THEN** the client reports an invalid-response outcome and the raw prompt is not logged

#### Scenario: Failure position is reported
- **WHEN** the local server fails after emitting some chunks
- **THEN** the reported outcome records that generation had already begun, distinguishing it from a failure before the first chunk
