## Context

The repository is empty apart from OpenSpec scaffolding. Everything described here is greenfield, so there is no migration burden and no existing consumer to break.

The shape of the problem: a single machine (Windows, with a GPU) hosts a `llama.cpp` model called Bonsai. Clients want AI answers without knowing or caring which model serves them. Local inference is free and private but occupies VRAM and takes tens of seconds to become ready. Cloud inference via OpenRouter is instant and more capable but costs money and leaves the machine. The router mediates between these, and its correctness is mostly about *state* (is the process running? is a request in flight? has it been crashing?) rather than about clever prompting.

Constraints that drive the design: .NET 10, ASP.NET Core minimal APIs, nullable reference types, DI, `HttpClientFactory`, `System.Text.Json`, xUnit + FluentAssertions with no mocking framework, PowerShell scripts rather than Bash for Windows, and no large framework (no Semantic Kernel, LangChain, Orleans, MassTransit, MediatR). The routing logic must stay explicit and readable.

## Goals / Non-Goals

**Goals:**
- One OpenAI-compatible endpoint that hides model selection and lifecycle from callers, supporting streaming and non-streaming from day one so stock OpenAI clients work unmodified.
- Deterministic, inspectable routing that returns a strategy, with strategy→model mapping living entirely in configuration.
- A local process manager that starts once under concurrency, detects readiness and crashes, and shuts down cleanly.
- Idle unloading that releases VRAM without ever interrupting an active request.
- Exactly one controlled local→cloud fallback, with a circuit breaker preventing restart storms.
- A shell tool that is safe by default: off unless enabled, allow-listed, and confirmation-gated.
- Testability without mocking frameworks: injectable clocks, injectable process abstractions, in-process fake HTTP servers.

**Non-Goals:**
- Any client application (Android, voice, camera), persistent memory, autonomous agents, or third-party integrations.
- A full interactive confirmation UI — a `confirmation_required` pending-action object is the contract.
- Multi-machine or multi-GPU scheduling, model quality learning, or dynamic model download.

## Decisions

### Three projects, with `Core` free of I/O

`Core` holds domain types, routing policy, and abstractions (`ILocalModelProcessManager`, `IChatProvider`, `IClock`). `Infrastructure` holds everything that touches the OS or network. `Api` wires DI and exposes endpoints.

The point is that the routing policy — the part with the most branches and the highest test value — becomes a pure function of a request context and a system-state snapshot. `Core.Tests` can then cover every routing rule with no process, no socket, and no clock.

*Alternative considered*: a single project. Rejected because it makes it too easy for routing to reach for `HttpClient` or `Process` directly, which is exactly what makes such code untestable.

### Strategy enum + configuration mapping, not model names in code

The policy returns `InferenceStrategy`. A `ModelRouting:Strategies` configuration section maps each strategy to a provider, a model identifier, and optional additional parameters. Startup validation fails if a strategy the policy can return has no mapping.

This is what keeps the requirement "do not hard-code an assumed current OpenRouter model identifier" enforceable rather than aspirational: model identifiers age out within months, and the routing code should never be the reason for a redeploy.

*Alternative considered*: returning a concrete model string from the policy. Rejected — it couples routing decisions to a vendor catalogue and makes the policy tests assert on strings that will churn.

### Deterministic policy first; self-assessment is advisory and disabled by default

Signals (approximate token count, coding-shaped, reasoning-shaped, tools requested, override present) are computed into an explicit `RoutingSignals` record, and the policy is a readable sequence of rules over that record. The optional local classifier can only *recommend escalation*; the deterministic layer applies privacy, cost, and security policy afterwards, so the classifier can never talk the router into sending data to the cloud when cloud is prohibited.

Recursion is prevented structurally: classification requests carry a flag in their request context that makes them ineligible for self-assessment, rather than relying on a call-depth counter.

*Alternative considered*: LLM-first routing. Rejected for a proof of concept — non-deterministic routing is untestable and unauditable, and the failure mode (classifier hallucinating a strategy) is worse than the failure mode of a simple heuristic being occasionally suboptimal.

### Streaming is the only provider path; non-streaming aggregates it

`IChatProvider` returns `IAsyncEnumerable<ChatCompletionChunk>`. The streaming endpoint flushes chunks to SSE as they arrive; the non-streaming endpoint consumes the same enumerable and aggregates it into one body.

One path, not two. The alternative — separate streaming and non-streaming provider methods — doubles the most correctness-critical code in the system (error classification, cancellation, activity tracking, circuit-breaker accounting) and guarantees the two drift. The cost is that a non-streaming request also uses SSE upstream, which is a real but small inefficiency, and both llama.cpp and OpenRouter support it natively.

### Fallback closes once the first byte is on the wire

This is the sharp consequence of streaming, and it puts an exception on the "retry once through cloud fallback" rule.

Once the router flushes chunk one of a `200 OK` event-stream, the status code is committed and partial content has been delivered. Re-dispatching to the cloud at that point would either duplicate content the client already rendered or require a status code we can no longer send. So the rule is: **fallback is available while no bytes have reached the client, and closed thereafter.** A local failure after the first chunk terminates the stream with an error event.

Note this is a statement about *the client's response*, not about the provider. That's what makes non-streaming unaffected: aggregation means nothing is written until generation completes, so a non-streaming request keeps the full fallback window at every point. The narrowing applies only to streamed requests, and only after first token.

Practically, the window covers the failures that actually matter. Process won't start, GGUF path wrong, server dies during load, readiness timeout, connection refused, circuit breaker open — all of these happen before any token exists. The uncovered case is a model that starts generating and then dies mid-sentence, which is rare and, for a streaming client, is visibly a broken stream rather than a silent wrong answer.

*Alternatives considered*: buffering local output until generation completes and only then streaming to the client would preserve full fallback — and destroy the entire latency benefit of streaming, which is the reason for doing it. Restarting the stream from the cloud after a mid-stream failure would render duplicated content. Both are worse than an honest broken stream.

The local client therefore reports failures as before-first-chunk or after-first-chunk, so the dispatcher knows which regime it is in. Mid-stream failures still count toward the circuit breaker — a crashing model is a crashing model regardless of when it dies.

### `LocalModelState` has no `Busy`

`llama-server` serves concurrent requests, and the idle guard already tracks an active-request count. A `Busy` state would be a second representation of `activeRequestCount > 0`, and two representations of one fact eventually disagree — the classic form being a request that dies without decrementing, leaving the model pinned in `Busy` forever and never unloading.

So busyness is a derived property over the count, and the count is the single source of truth. Lifecycle state stays `Stopped | Starting | Ready | Stopping | Faulted` and answers only "does a usable process exist".

### Single-flight startup via a cached `Task`, not a lock held across the await

The manager keeps a nullable `Task<StartResult>` guarded by a short-lived lock. A request that finds the model stopped takes the lock, checks again, creates the startup task, and releases the lock *before* awaiting it. Concurrent arrivals take the lock, find the task present, release, and await the same task.

This is the mechanism that satisfies "five concurrent requests start one process" and "do not hold locks while awaiting network or process operations" simultaneously. `SemaphoreSlim` around the whole start would also serialise, but it would make the second-through-fifth caller await a semaphore rather than share the result, and it invites holding the semaphore across the readiness poll.

*Alternative considered*: `Lazy<Task<T>>`. Workable, but it caches failures permanently unless recreated, and we need a failed start to be retryable once the circuit breaker allows it.

### Readiness by polling llama.cpp's health endpoint, not by parsing stdout

Stdout is captured for logging and crash diagnosis, but readiness is determined by polling the server's HTTP health endpoint until it answers or the startup timeout elapses. Log formats change between llama.cpp releases; the health endpoint is the contract.

### Idle unload as a hosted service reading a shared activity/state snapshot

An `IHostedService` wakes on an interval and asks the manager whether it may unload: model running, active request count zero, `now - lastActivity >= idleTimeout`, and no transition in progress. The check and the stop are the manager's responsibility, so the active-request guard and the state machine live in one place rather than being duplicated in the background service.

Active request count is incremented when a local request begins and decremented in a `finally`, so a crashed or cancelled request cannot pin the model in memory forever.

Time comes from an injected `IClock`, which is what makes the idle tests deterministic instead of `Thread.Sleep`-based and flaky.

### Circuit breaker over consecutive failures, in front of the process manager

The breaker sits between the routing dispatch and the local provider. Consecutive local failures increment a counter; the configured threshold opens the breaker; the configured cooldown closes it. While open, no start is attempted at all.

This directly targets the pathological case: a misconfigured GGUF path means every request tries to spawn a process that dies in 200ms. Without the breaker, a busy client turns that into a fork bomb.

### Process abstraction for testability

`Infrastructure` wraps `System.Diagnostics.Process` behind an interface exposing start, exit notification, kill-tree, and stdout/stderr streams. Tests inject a fake that simulates slow starts, crashes, and unresponsive shutdowns — none of which are reproducible with a real process on a CI machine without shipping test binaries.

*Alternative considered*: temporary PowerShell/Bash scripts as fake servers. Used in `IntegrationTests` (where exercising real process launch is the point), but not in `Core.Tests`.

### Shell tool: resolve the path first, check second

The working directory is resolved to a full canonical path *before* comparison against the allow list, so traversal segments cannot slip past a string prefix check. Commands are dispatched through `pwsh`/`powershell` or `bash` with arguments as argument-list entries — never `cmd.exe /c`, and never string concatenation of untrusted values.

Three outcomes, not two: allow-listed → run; destructive-pattern match → reject outright (not confirmable); otherwise → `confirmation_required` with an `actionId` and summary, and no process started. Destructive patterns are checked before the confirmation path so a `format` command can never be presented to a user as a confirmable action.

The README must say plainly that pattern blocking is a speed bump, not a boundary. Anyone who can enable the tool and choose a working directory can eventually run arbitrary code; the real boundary is that the tool is off by default and the router is loopback-only.

### Loopback default, API key with constant-time comparison

Kestrel binds loopback. API-key auth uses `CryptographicOperations.FixedTimeEquals` over fixed-length hashes of the presented and configured keys — hashing first avoids leaking key length, which comparing raw bytes would.

Two authorization policies: one for chat, one for administrative/tool endpoints, so an exposed chat client credential cannot stop the model or run shell commands.

## Risks / Trade-offs

- **Non-deterministic startup timing makes lifecycle tests flaky** → All time flows through `IClock` and all process interaction through the process abstraction; no test sleeps on wall-clock time.
- **A crashing local process starves clients** → Circuit breaker plus single controlled fallback; the breaker's open state is exposed on `/api/router/status` so the operator can see why local stopped being tried.
- **Idle unload races an incoming request** → The unload decision and the request-begin both go through the manager's state machine; a request arriving during `Stopping` waits for the stop and starts fresh rather than attaching to a dying process.
- **Pattern blocking creates false confidence in shell safety** → Documented explicitly as not a security boundary; disabled by default; allow-listed directories and commands are the actual controls.
- **Approximate token counting mis-routes borderline requests** → Threshold is configurable, the routing reason is logged for every decision, and mis-routing degrades to "used the wrong provider", not to a correctness or privacy failure. Privacy policy is applied after routing, not by it.
- **Streaming narrows the fallback window** → Fallback covers every failure that occurs before the first token, which is nearly all of them (start failure, bad path, readiness timeout, connection refused, breaker open). A mid-generation crash produces a terminated stream, which is visible to the client rather than silently wrong. Non-streaming is unaffected.
- **A cold local start is very visible to a streaming client** → Time-to-first-chunk is recorded as a distinct metric precisely so this is measurable; if cold-start latency proves unacceptable, the lever is the idle timeout, not the routing policy.
- **`llama-server` CLI flags drift between releases** → Additional arguments are configuration-driven, and readiness is detected via the health endpoint rather than log parsing.
- **Windows-first with portability claimed but not proven** → Shell executor selection and path handling are the only OS-conditional code; both are isolated behind small abstractions so a Linux run is a configuration change, not a port.

## Resolved Questions

### Cloud model identifiers: placeholders that fail loudly

`appsettings.example.json` ships `"<configure-me: see https://openrouter.ai/models>"` for each cloud strategy, and startup validation rejects it with a message naming the strategy. Real identifiers were rejected because they age out within months and a stale-but-plausible default looks authoritative — someone copies the example, it works, and much later it is silently serving from a deprecated model. Empty strings would fail too, but with "value required" rather than "go look at the catalogue".

The README may carry concrete suggestions in an explicitly **dated** "known-good as of" table. A dated table is honestly stale-able in a way a config default is not.

Also rejected: pointing `CloudGeneral` at `openrouter/auto` and delegating model choice upstream. It makes cost unpredictable and hollows out the routing reason we log for every decision — we would record "chose CloudGeneral" without knowing what served it.

### GPU detection: `nvidia-smi`, cached, degrading to unknown

The clarifying fact is that GPU availability is **diagnostic-only** — it appears on the status endpoint and feeds no routing decision — so the cost of imprecision is zero and the cost of building it properly is real. WMI is rejected as Windows-only, which would undercut the portability claim; NVML P/Invoke is rejected as a native dependency for a field nobody makes decisions on.

Detection runs `nvidia-smi` with a fixed argument list behind a swappable interface, cached with a short TTL so status polling does not spawn a process per call. Non-NVIDIA and undetectable hosts report unknown.

There is a mild irony in a router whose security posture is "shell execution is off by default" shelling out to `nvidia-smi`. It is a fixed argument list with no user input anywhere near it, so it is not the same risk, but the call site should say so.

### Pending actions: in-memory is correct, not a compromise

Initially framed as "in-memory is acceptable for a PoC", implying durability was the better version being deferred. That framing was wrong. **Persisting pending actions would be actively worse.**

A pending shell action surviving a restart means a confirmation minted under one configuration could execute under another — after someone edited the allow list, say. Discarding them on restart is fail-closed, which is precisely the behaviour wanted from the most dangerous component in the system. A TTL is needed regardless, so "unknown, expired, or already-consumed action" is a response path we need either way; restart is just one more route to it.

Consequently the pending action stores the fully-resolved command and working directory server-side, confirmation carries only the `actionId`, and IDs are single-use. Otherwise a caller confirms id X while swapping the command and the confirmation gate is decorative.
