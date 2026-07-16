## ADDED Requirements

### Requirement: Child liveness probing detects a hung model
The router SHALL continuously probe the local server's health endpoint while the model is `Ready`, not only during startup. A process that is alive but fails a configurable number of consecutive liveness probes SHALL be treated as faulted, killed, and made eligible for restart. Process exit is not the only failure mode; a hung server produces no exit signal.

#### Scenario: Hung but alive model is recycled
- **WHEN** the local process remains alive but fails the configured number of consecutive liveness probes
- **THEN** the router kills the process, sets state to `Faulted`, and logs the recycle reason as a failed liveness probe

#### Scenario: Liveness probe does not disturb a healthy model
- **WHEN** the model is `Ready` and probes succeed
- **THEN** no recycle occurs and probe activity does not count as request activity for idle-unload purposes

#### Scenario: Probing stops when the model is not running
- **WHEN** the model is `Stopped` or `Stopping`
- **THEN** no liveness probes are issued

#### Scenario: A recycled model does not interrupt an active request
- **WHEN** a liveness probe fails while a local request is still streaming successfully
- **THEN** the probe failure is recorded but the kill is deferred until the active request completes or fails

### Requirement: Orphaned local servers are reclaimed at startup
On startup the router SHALL probe the configured local host and port before attempting to spawn a process. If a healthy `llama-server` is already listening, the router SHALL adopt it rather than starting a second one. If the port is occupied by something unhealthy or unrecognised, the router SHALL NOT silently spawn a competing process.

#### Scenario: Healthy orphan is adopted
- **WHEN** the router starts and a healthy local server is already listening on the configured port
- **THEN** the router adopts it, reports state `Ready` without a cold start, and does not spawn a second process

#### Scenario: Adopted server is still managed
- **WHEN** an adopted server has been idle past the idle timeout
- **THEN** the router stops it exactly as it would a server it spawned itself

#### Scenario: Unhealthy occupant is not raced
- **WHEN** the configured port is occupied but does not answer the health endpoint
- **THEN** the router does not spawn a competing process and surfaces the conflict as a faulted state with an explanatory reason

#### Scenario: Free port starts normally
- **WHEN** the router starts and nothing is listening on the configured port
- **THEN** startup proceeds normally with no adoption

### Requirement: Circuit breaker recovers through a half-open trial
After the cooldown elapses, the breaker SHALL enter a half-open state permitting exactly one trial request. Success SHALL close the breaker and reset the failure count. Failure SHALL re-open it with an exponentially increasing cooldown, bounded by a configured maximum. The breaker SHALL NOT release all waiting traffic at a possibly-still-broken model.

#### Scenario: One trial request probes recovery
- **WHEN** the cooldown elapses and several local-eligible requests arrive
- **THEN** exactly one is admitted as a trial and the remainder take the permitted fallback or error, because a half-open breaker admits a single probe

#### Scenario: Successful trial closes the breaker
- **WHEN** the half-open trial request succeeds
- **THEN** the breaker closes, the consecutive-failure count resets, and the cooldown returns to its configured base

#### Scenario: Failed trial re-opens with a longer cooldown
- **WHEN** the half-open trial request fails
- **THEN** the breaker re-opens and the next cooldown is longer than the previous one, up to the configured maximum

#### Scenario: Backoff is bounded
- **WHEN** repeated trials fail many times in succession
- **THEN** the cooldown stops growing at the configured maximum rather than increasing without limit

### Requirement: Background loops cannot die silently
Every long-running background loop, including idle unloading and liveness probing, SHALL recover from panics, log the failure, and continue running. A background loop that terminates silently SHALL be treated as a defect, because a dead idle-unloader means GPU VRAM is never released and nothing reports it.

#### Scenario: Panic in the idle loop does not kill the loop
- **WHEN** the idle-unload loop encounters a panic during one iteration
- **THEN** the panic is recovered and logged, and the loop performs its next scheduled iteration

#### Scenario: Loop health is observable
- **WHEN** a background loop has recovered from a panic
- **THEN** the recovery is counted in metrics so a repeatedly failing loop is visible rather than silent

### Requirement: Process supervision is external and documented
The router SHALL NOT attempt to supervise or restart itself. Documentation SHALL specify running it as a Windows Service (or via NSSM) with restart-on-failure, and as a `systemd` unit with `Restart=always` on Linux. Startup SHALL be idempotent so that an external supervisor restarting the router is always safe.

#### Scenario: Restart after an unclean exit is safe
- **WHEN** an external supervisor restarts the router after an unclean exit that left a local server running
- **THEN** startup adopts or reclaims the orphan and converges to a correct state without manual intervention

#### Scenario: Documentation covers both platforms
- **WHEN** the README is inspected
- **THEN** it documents supervisor configuration for both Windows and Linux and explains why self-supervision is not attempted

### Requirement: Self-healing actions are observable
Every automatic recovery action SHALL be logged and counted: model recycles due to failed liveness, orphan adoptions, breaker state transitions including half-open trials, and background-loop panic recoveries. Silent self-healing SHALL be treated as a defect, because a router that quietly recovers from a recurring fault hides the fault.

#### Scenario: Recycles are counted
- **WHEN** the router recycles a hung model
- **THEN** a recycle counter increments and a structured log entry records the reason

#### Scenario: Status surfaces recovery state
- **WHEN** an authorised caller requests `/api/router/status` after automatic recovery has occurred
- **THEN** the response reports the current breaker state including half-open, and the count of automatic recoveries
