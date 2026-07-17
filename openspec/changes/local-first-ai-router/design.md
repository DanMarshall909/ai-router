## Context

The repository is empty apart from OpenSpec scaffolding. Everything described here is greenfield, so there is no migration burden and no existing consumer to break.

The shape of the problem: a single machine (Windows, with a GPU) hosts a `llama.cpp` model called Bonsai. Clients want AI answers without knowing or caring which model serves them. Local inference is free and private but occupies VRAM and takes tens of seconds to become ready. Cloud inference via OpenRouter is instant and more capable but costs money and leaves the machine. The router mediates between these, and its correctness is mostly about *state* (is the process running? is a request in flight? has it been crashing? is it wedged?) rather than about clever prompting.

Constraints that drive the design: Go, standard library first, no large framework, and routing logic that stays explicit and readable. Tests use `testing` and `testify/require` for assertions only — no mocking framework. PowerShell rather than Bash for the Windows scripts.

### Why Go

The workload is I/O-bound supervision: wait on a child process, poll a health endpoint, proxy an event-stream, run a few timers. The bottleneck is a 30-second model load and roughly 40ms per token — nothing a faster language moves. What the router actually needs is cheap concurrency, a good HTTP stack, `context` cancellation that propagates through both, painless subprocess handling, and a single binary that an OS supervisor can restart. That is Go's centre of mass rather than its periphery.

Rejected: **Rust**, whose zero-cost abstractions buy nothing here while making streaming lifetimes and process supervision materially harder to write. **Temporal**, which was considered and dropped — the unit of work is an HTTP request that lives for seconds and dies with the caller's connection, so there is nothing to resume; the thing being tracked (`llama-server`) is a child of the router, so durable state buys nothing when the tracker dies with the tracked; its one genuine fit (human-in-the-loop confirmation via signals) solves a problem we deliberately chose to have (see *Pending actions*); and it means running a server and a database on the machine whose entire purpose is keeping VRAM free. What Temporal was really being asked for was self-healing, which is specced directly instead.

## Goals / Non-Goals

**Goals:**
- One OpenAI-compatible endpoint that hides model selection and lifecycle from callers, supporting streaming and non-streaming from day one so stock OpenAI clients work unmodified.
- Deterministic, inspectable routing that returns a strategy, with strategy→model mapping living entirely in configuration.
- A local process manager that starts once under concurrency, detects readiness and crashes, and shuts down cleanly.
- Idle unloading that releases VRAM without ever interrupting an active request.
- Exactly one controlled local→cloud fallback, with a circuit breaker preventing restart storms.
- Self-healing: hangs detected, orphans reclaimed, recovery probed rather than stampeded, background loops that cannot die silently.
- A shell tool that is safe by default: off unless enabled, allow-listed, and confirmation-gated.
- Testability without mocking frameworks: injected clocks, injected process abstractions, `httptest` servers.

**Non-Goals:**
- Any client application (Android, voice, camera), persistent memory, autonomous agents, or third-party integrations.
- A full interactive confirmation UI — a `confirmation_required` pending-action object is the contract.
- Multi-machine or multi-GPU scheduling, model quality learning, or dynamic model download.
- Self-supervision. Restarting the router process is the operating system's job.

## Decisions

### Package layout, with routing free of I/O

`cmd/airouter` is the entry point and wiring. `internal/routing` holds domain types and the routing policy and imports nothing that touches the OS or network. `internal/local` (process manager, liveness, idle loop), `internal/cloud` (OpenRouter client), `internal/shell`, `internal/observability`, and `internal/config` hold the I/O. `internal/httpapi` exposes the endpoints.

The point is that the routing policy — the part with the most branches and the highest test value — is a pure function of a request context and a system-state snapshot. Its tests need no process, no socket, and no clock.

Interfaces are declared in the consuming package, not exported alongside the implementation, which is the Go convention and keeps `internal/routing` from depending on `internal/local` to describe what it needs.

*Alternative considered*: one flat package. Rejected — it makes it too easy for routing to reach for `http.Get` or `exec.Command` directly, which is exactly what makes such code untestable.

### Strategy enum + configuration mapping, not model names in code

The policy returns an `InferenceStrategy`. A `modelRouting.strategies` configuration section maps each strategy to a provider, a model identifier, and optional additional parameters. Startup validation fails if a strategy the policy can return has no mapping.

This is what keeps "do not hard-code an assumed current OpenRouter model identifier" enforceable rather than aspirational: model identifiers age out within months, and the routing code should never be the reason for a rebuild.

*Alternative considered*: returning a concrete model string from the policy. Rejected — it couples routing decisions to a vendor catalogue and makes policy tests assert on strings that will churn.

### Deterministic policy first; self-assessment is advisory and disabled by default

Signals (approximate token count, coding-shaped, reasoning-shaped, tools requested, override present) are computed into an explicit `RoutingSignals` struct, and the policy is a readable sequence of rules over that struct. The optional local classifier can only *recommend escalation*; the deterministic layer applies privacy, cost, and security policy afterwards, so the classifier can never talk the router into sending data to the cloud when cloud is prohibited.

Recursion is prevented structurally: a classification request carries a flag in its request context making it ineligible for self-assessment, rather than relying on a call-depth counter.

*Alternative considered*: LLM-first routing. Rejected for a proof of concept — non-deterministic routing is untestable and unauditable, and a classifier hallucinating a strategy is a worse failure than a heuristic being occasionally suboptimal.

### Streaming is the only provider path; non-streaming aggregates it

The provider interface is `Stream(ctx context.Context, req ChatRequest) (iter.Seq2[Chunk, error], error)`. The streaming handler writes each chunk as an SSE event and calls `http.Flusher.Flush`; the non-streaming handler ranges over the same iterator and aggregates it into one body. The outer `error` covers failures to establish the stream at all; the per-item `error` covers failures during it.

One path, not two. The alternative — separate streaming and non-streaming provider methods — doubles the most correctness-critical code in the system (error classification, cancellation, activity tracking, breaker accounting) and guarantees the two drift. The cost is that a non-streaming request also uses SSE upstream, a real but small inefficiency, and both llama.cpp and OpenRouter support it natively.

*Alternative considered*: a channel of chunks. Rejected — a channel needs a side-band error and a guaranteed drain to avoid leaking the producer goroutine, and `range` over a `Seq2` gets both from the language. Cancellation still travels via `ctx`, which the producer selects on.

### Fallback closes once the first byte is on the wire

This is the sharp consequence of streaming, and it puts an exception on the "retry once through cloud fallback" rule.

Once the router flushes chunk one of a `200 OK` event-stream, the status code is committed and partial content has been delivered. Re-dispatching to the cloud at that point would either duplicate content the client already rendered or require a status code we can no longer send. So the rule is: **fallback is available while no bytes have reached the client, and closed thereafter.** A local failure after the first chunk terminates the stream with an error event.

Note this is a statement about *the client's response*, not about the provider. That is what makes non-streaming unaffected: aggregation means nothing is written until generation completes, so a non-streaming request keeps the full fallback window at every point. The narrowing applies only to streamed requests, and only after first token.

Practically, the window covers the failures that actually matter. Process won't start, GGUF path wrong, server dies during load, readiness timeout, connection refused, breaker open — all of these happen before any token exists. The uncovered case is a model that starts generating and then dies mid-sentence, which is rare and, to a streaming client, is visibly a broken stream rather than a silent wrong answer.

*Alternatives considered*: buffering local output until generation completes would preserve full fallback — and destroy the entire latency benefit of streaming, which is the reason for doing it. Restarting the stream from the cloud after a mid-stream failure would render duplicated content. Both are worse than an honest broken stream.

The local client therefore reports failures as before-first-chunk or after-first-chunk, so the dispatcher knows which regime it is in. Mid-stream failures still count toward the circuit breaker — a crashing model is a crashing model regardless of when it dies.

### `LocalModelState` has no `Busy`

`llama-server` serves concurrent requests, and the idle guard already tracks an active-request count. A `Busy` state would be a second representation of `activeRequests > 0`, and two representations of one fact eventually disagree — the classic form being a request that dies without decrementing, leaving the model pinned in `Busy` forever and never unloading.

So busyness is a derived property over the count, and the count is the single source of truth. Lifecycle state stays `Stopped | Starting | Ready | Stopping | Faulted` and answers only "does a usable process exist".

### Single-flight startup via `singleflight.Group`

`golang.org/x/sync/singleflight` is the mechanism: every caller that finds the model stopped calls `group.Do("start", startFn)`, and the package guarantees exactly one execution with the result shared by all waiters. This satisfies "five concurrent requests start one process" without any lock being held across the readiness poll.

Crucially `singleflight` does not cache: once a start completes, the next `Do` runs again. That is what we want — a failed start must be retryable as soon as the breaker allows, and a stopped-after-idle model must be startable again. (`sync.Once` would be wrong for exactly that reason.)

The mutex around the state machine is held only to read and write state fields, never across a process or network call.

*Alternative considered*: a hand-rolled `chan struct{}` broadcast. Workable, but it is the same algorithm with more places to get the failure path wrong.

### Readiness by polling llama.cpp's health endpoint, not by parsing stdout

Stdout is captured for logging and crash diagnosis, but readiness is determined by polling the server's HTTP health endpoint until it answers or the startup timeout elapses. Log formats change between llama.cpp releases; the health endpoint is the contract.

### Liveness probing continues for the life of the process

Readiness answers "has it started". Liveness answers "is it still working", and the two are not the same question. A wedged `llama-server` — GPU fault, deadlock, VRAM exhaustion — stays alive, keeps its socket open, and never exits, so exit-code watching alone leaves the router happily routing into a black hole.

So the probe loop runs the whole time the model is `Ready`, not just during startup. A configured number of *consecutive* failures (not one) recycles the process: kill, `Faulted`, eligible for restart under the breaker. Consecutive, because a single probe timeout under heavy generation load is normal and must not recycle a working model.

Two couplings matter. Probes must not count as activity for the idle guard, or the model never unloads — the loop that proves it is alive would be the thing keeping it alive. And a kill is deferred while a request is still streaming successfully: a probe failing while tokens flow means the probe is wrong, not the model, and killing mid-stream would break a request that is demonstrably working.

### Orphan adoption at startup

If the router crashes, `llama-server` may survive it — a child process is not guaranteed to die with its parent, and on Windows without a job object it usually does not. Naively spawning on restart then either fails on a bound port or, worse, loads a second copy of the model and doubles VRAM on the machine whose entire purpose is keeping VRAM free.

So startup probes the configured host and port *before* spawning. Healthy server → adopt it: state goes straight to `Ready` with no cold start, and it is managed exactly as a spawned one, idle unload included. Port occupied but unhealthy → do not race it; surface `Faulted` with a reason, because something we do not understand owns that port and spawning a competitor makes an unclear situation worse. Free port → normal start.

Adoption is what makes restart-after-crash converge, which is what makes external supervision sufficient.

### Circuit breaker with a half-open trial and bounded backoff

The breaker sits between routing dispatch and the local provider. Consecutive local failures open it; while open, no start is attempted at all. This targets the pathological case directly: a misconfigured GGUF path means every request spawns a process that dies in 200ms, and without the breaker a busy client turns that into a fork bomb.

Recovery is the part worth being careful about. When the cooldown elapses the breaker goes **half-open** and admits exactly one trial request; everyone else takes the permitted fallback meanwhile. Success closes it and resets both the failure count and the cooldown to its base. Failure re-opens it with a longer cooldown, growing exponentially up to a configured maximum.

The alternative — closing on a timer and releasing everything — means a genuinely broken model gets stampeded on every cooldown expiry, which is the fork bomb again on a slower clock. One trial costs one request and answers the question.

### Background loops recover from panics

The idle unloader and the liveness prober each run as a goroutine whose body is wrapped in `defer recover()`, logs the panic, counts it, and continues to the next iteration.

Go's default is the opposite: an unrecovered panic in any goroutine takes the whole process down. That is usually right, and it is wrong here — but the reason to recover is not that crashing is bad, it is that *dying quietly* is bad. A dead idle-unloader means VRAM is never released and nothing says so; the router keeps serving requests and looks perfectly healthy. Recovering and counting turns an invisible failure into a visible metric. A loop that panics repeatedly is a defect the counter will surface.

Every loop takes `ctx` and exits only on `ctx.Done()`, so shutdown is the one way a loop is allowed to end.

### Supervision is the OS's job

The router does not restart itself; a process cannot reliably resurrect itself, and pretending otherwise produces a watchdog that dies with the thing it watches. Windows gets a Service (or NSSM) with restart-on-failure; Linux gets a `systemd` unit with `Restart=always`. Startup is idempotent — adoption is the mechanism that makes it so — which is what makes an external supervisor's "just start it again" always safe.

### Process abstraction for testability

`internal/local` wraps `os/exec` behind an interface exposing start, wait/exit notification, kill, and stdout/stderr readers. Tests inject a fake that simulates slow starts, crashes, hangs, and unresponsive shutdowns — none of which are reproducible with a real process on a CI machine without shipping test binaries.

`exec.CommandContext` provides kill-on-cancel. On Windows a job object (and on Linux a process group) is used so a killed `llama-server` cannot leave grandchildren behind.

*Alternative considered*: temporary PowerShell/Bash scripts as fake servers. Used in the integration tests, where exercising real process launch is the point, but not in the unit tests.

### Time comes from an injected clock

A `Clock` interface (`Now`, `After`, `NewTicker`) is injected into the idle loop, the liveness prober, the breaker, and the pending-action store. This is what makes those tests deterministic rather than `time.Sleep`-based and flaky, and it is the only reason a test can assert "the cooldown grew" without waiting for it.

### Shell tool: resolve the path first, check second

The working directory is resolved with `filepath.Abs` and `filepath.EvalSymlinks` *before* comparison against the allow list, so traversal segments and symlinks cannot slip past a string prefix check. The comparison is path-component-wise, not `strings.HasPrefix` — `/srv/workdir-evil` must not match an allow list entry of `/srv/workdir`.

Commands are dispatched through `pwsh`/`powershell` or `bash` with arguments as separate `exec.Command` argument entries — never `cmd.exe /c`, and never string concatenation of untrusted values.

Three outcomes, not two: allow-listed → run; destructive-pattern match → reject outright (not confirmable); otherwise → `confirmation_required` with an `actionId` and summary, and no process started. Destructive patterns are checked before the confirmation path so a `format` command can never be presented to a user as a confirmable action.

The README must say plainly that pattern blocking is a speed bump, not a boundary. Anyone who can enable the tool and choose a working directory can eventually run arbitrary code; the real boundary is that the tool is off by default and the router is loopback-only.

### Loopback default, API key with constant-time comparison

The server binds `127.0.0.1` by default. API-key auth uses `crypto/subtle.ConstantTimeCompare` over fixed-length SHA-256 hashes of the presented and configured keys — hashing first avoids leaking key length, which comparing raw bytes would.

Two authorization policies: one for chat, one for administrative/tool endpoints, so an exposed chat client credential cannot stop the model or run shell commands.

### Configuration is `encoding/json` plus environment overrides

A `config.json` unmarshalled into a typed struct, with environment variables overriding nested keys, and validation at startup that names the offending section and setting. Secrets come from the environment only.

*Alternative considered*: Viper. Rejected under the no-large-framework constraint — it brings a large dependency tree to solve a problem that is one `json.Unmarshal` and one env lookup, and its silent key-coercion is the opposite of the fail-loudly posture we want at startup.

## Risks / Trade-offs

- **Non-deterministic startup timing makes lifecycle tests flaky** → All time flows through the injected clock and all process interaction through the process abstraction; no test sleeps on wall-clock time.
- **A crashing local process starves clients** → Circuit breaker plus single controlled fallback; breaker state, including half-open, is exposed on `/api/router/status` so an operator can see why local stopped being tried.
- **Idle unload races an incoming request** → The unload decision and the request-begin both go through the manager's state machine; a request arriving during `Stopping` waits for the stop and starts fresh rather than attaching to a dying process.
- **Liveness probing recycles a healthy model under load** → Recycle requires consecutive failures, the threshold and timeout are configurable, and a kill is deferred while a request is still streaming successfully.
- **Adoption attaches to a server we did not configure** → Adoption requires a healthy response from llama.cpp's health endpoint on the configured host and port. An unhealthy or unrecognised occupant is surfaced as faulted rather than raced. The residual risk is adopting a correctly-shaped server running a *different* model, which the status endpoint's reported model profile makes visible.
- **Panic recovery hides a real defect** → Recoveries are counted and logged, so a repeatedly panicking loop is louder than a crash-restart loop would be, not quieter.
- **Pattern blocking creates false confidence in shell safety** → Documented explicitly as not a security boundary; disabled by default; allow-listed directories and commands are the actual controls.
- **Approximate token counting mis-routes borderline requests** → Threshold is configurable, the routing reason is logged for every decision, and mis-routing degrades to "used the wrong provider", not to a correctness or privacy failure. Privacy policy is applied after routing, not by it.
- **Streaming narrows the fallback window** → Fallback covers every failure occurring before the first token, which is nearly all of them. A mid-generation crash produces a terminated stream, visible to the client rather than silently wrong. Non-streaming is unaffected.
- **A cold local start is very visible to a streaming client** → Time-to-first-chunk is recorded as a distinct metric precisely so this is measurable; if cold-start latency proves unacceptable, the lever is the idle timeout, not the routing policy.
- **`llama-server` CLI flags drift between releases** → Additional arguments are configuration-driven, and readiness is detected via the health endpoint rather than log parsing.
- **Windows-first with portability claimed but not proven** → Shell executor selection, process-group handling, and path handling are the only OS-conditional code; each is isolated behind a small abstraction (with `_windows.go` / `_unix.go` build-tagged files where needed) so a Linux run is a configuration change, not a port.

## Resolved Questions

### Cloud model identifiers: placeholders that fail loudly

`config.example.json` ships `"<configure-me: see https://openrouter.ai/models>"` for each cloud strategy, and startup validation rejects it with a message naming the strategy. Real identifiers were rejected because they age out within months and a stale-but-plausible default looks authoritative — someone copies the example, it works, and much later it is silently serving from a deprecated model. Empty strings would fail too, but with "value required" rather than "go look at the catalogue".

The README may carry concrete suggestions in an explicitly **dated** "known-good as of" table. A dated table is honestly stale-able in a way a config default is not.

Also rejected: pointing `CloudGeneral` at `openrouter/auto` and delegating model choice upstream. It makes cost unpredictable and hollows out the routing reason we log for every decision — we would record "chose CloudGeneral" without knowing what served it.

### GPU detection: `nvidia-smi`, cached, degrading to unknown

The clarifying fact is that GPU availability is **diagnostic-only** — it appears on the status endpoint and feeds no routing decision — so the cost of imprecision is zero and the cost of building it properly is real. NVML via cgo is rejected as a native dependency for a field nobody makes decisions on, and it would forfeit the pure-Go static binary.

Detection runs `nvidia-smi` with a fixed argument list behind a swappable interface, cached with a short TTL so status polling does not spawn a process per call. Non-NVIDIA and undetectable hosts report unknown.

There is a mild irony in a router whose security posture is "shell execution is off by default" shelling out to `nvidia-smi`. It is a fixed argument list with no user input anywhere near it, so it is not the same risk, but the call site should say so.

### Pending actions: in-memory is correct, not a compromise

Initially framed as "in-memory is acceptable for a PoC", implying durability was the better version being deferred. That framing was wrong. **Persisting pending actions would be actively worse.**

A pending shell action surviving a restart means a confirmation minted under one configuration could execute under another — after someone edited the allow list, say. Discarding them on restart is fail-closed, which is precisely the behaviour wanted from the most dangerous component in the system. A TTL is needed regardless, so "unknown, expired, or already-consumed action" is a response path we need either way; restart is just one more route to it.

Consequently the pending action stores the fully-resolved command and working directory server-side, confirmation carries only the `actionId`, and IDs are single-use. Otherwise a caller confirms id X while swapping the command and the confirmation gate is decorative.
