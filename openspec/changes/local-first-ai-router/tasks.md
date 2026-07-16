## 1. Solution and project structure

- [ ] 1.1 Create `LocalFirst.AiRouter.sln` with `src/LocalFirst.AiRouter.Api`, `src/LocalFirst.AiRouter.Core`, `src/LocalFirst.AiRouter.Infrastructure` targeting .NET 10 with nullable reference types and warnings-as-errors enabled
- [ ] 1.2 Create `tests/LocalFirst.AiRouter.Core.Tests` and `tests/LocalFirst.AiRouter.IntegrationTests` with xUnit and FluentAssertions, and project references (Api → Core + Infrastructure; Infrastructure → Core; Core → nothing)
- [ ] 1.3 Add `.gitignore` covering build output, user secrets, and local settings files that may carry keys
- [ ] 1.4 Verify `dotnet build` and `dotnet test` succeed on the empty solution

## 2. Configuration and validation

- [ ] 2.1 Add options types for `LocalModel`, `OpenRouter`, `Routing`, `ShellTool`, and `ModelRouting:Strategies`, each with named public constants for its configuration section key
- [ ] 2.2 Bind options via DI with `ValidateOnStart`, producing error messages that name the offending section and setting
- [ ] 2.3 Write failing tests then implement: invalid configuration fails fast; cloud settings are optional when cloud is disabled; enabled cloud without an API key fails with a message explaining environment variables and user secrets
- [ ] 2.4 Write failing test then implement: a strategy with no `ModelRouting` mapping fails startup validation naming the strategy
- [ ] 2.5 Add `appsettings.example.json` with all sections documented and an empty API key

## 3. Domain types and provider abstractions

- [ ] 3.1 Add `InferenceStrategy`, `LocalModelState`, and `RoutingDecision` to `Core` exactly as specified
- [ ] 3.2 Add explicit `RequestContext` and `RoutingSignals` types (no loose dictionaries), including the flag marking a request ineligible for self-assessment
- [ ] 3.3 Add `IChatProvider`, `ILocalModelProcessManager`, and `IClock` abstractions in `Core`, shaped so a streaming variant can be added without changing routing
- [ ] 3.4 Add a deterministic test clock and fake `IChatProvider` implementations in `Core.Tests` (no mocking framework)

## 4. Deterministic routing policy

- [ ] 4.1 Write failing tests then implement signal extraction: approximate input token count, coding-shaped, reasoning-shaped, tools requested, request length
- [ ] 4.2 Write failing test then implement: `model: auto` on an ordinary request selects `QuickLocal`
- [ ] 4.3 Write failing test then implement: a request over the complexity threshold selects the configured cloud reasoning or cloud coding strategy
- [ ] 4.4 Write failing test then implement: input exceeding `MaxLocalInputTokens` never selects a local strategy
- [ ] 4.5 Write failing test then implement: identical request and system state yield an identical `RoutingDecision`
- [ ] 4.6 Write failing tests then implement explicit override: a permitted override is honoured and recorded in the reason; a policy-violating override is rejected with an explanatory response
- [ ] 4.7 Write failing test then implement: cloud prohibition returns a clear service-unavailable response and transmits nothing externally
- [ ] 4.8 Write failing test then implement strategy→provider/model resolution from configuration

## 5. Optional local self-assessment

- [ ] 5.1 Write failing test then implement: self-assessment is disabled by default and issues no classification prompt
- [ ] 5.2 Implement the classifier requesting strict JSON naming one of the five strategies
- [ ] 5.3 Write failing test then implement: a classifier recommendation cannot bypass privacy, cost, or security policy
- [ ] 5.4 Write failing test then implement: classification requests are structurally ineligible for further classification
- [ ] 5.5 Write failing test then implement: malformed classifier output falls back to the deterministic decision and is logged without prompt content

## 6. llama.cpp process manager

- [ ] 6.1 Add the process abstraction in `Infrastructure` (start, exit notification, kill-tree, stdout/stderr) plus a fake implementation for tests simulating slow start, crash, and unresponsive shutdown
- [ ] 6.2 Write failing test then implement start with arguments passed as individual argument-list entries, covering paths containing spaces and quotes
- [ ] 6.3 Write failing test then implement readiness by polling the llama.cpp health endpoint, bounded by the configured startup timeout
- [ ] 6.4 Write failing test then implement single-flight startup so five concurrent requests start exactly one process and share the result
- [ ] 6.5 Write failing test then implement reuse of an already-ready process
- [ ] 6.6 Write failing test then implement state tracking through `Stopped` → `Starting` → `Ready`, exposing the process ID, without holding locks across awaits
- [ ] 6.7 Write failing test then implement `Faulted` on launch failure or readiness timeout
- [ ] 6.8 Write failing test then implement graceful stop, and force-kill of the process tree after the shutdown timeout
- [ ] 6.9 Write failing test then implement unexpected-exit detection setting `Faulted` and preventing routing to the dead process
- [ ] 6.10 Verify all process-related resources are released on stop and on dispose

## 7. Local model client

- [ ] 7.1 Implement the OpenAI-compatible `llama-server` client using `HttpClientFactory` with configured timeouts and cancellation preserved
- [ ] 7.2 Write failing tests then implement distinct outcomes for startup failure, connection failure, timeout, invalid response, and generation failure
- [ ] 7.3 Write failing test then verify full prompts are not logged by default

## 8. Idle unload service

- [ ] 8.1 Implement active-request counting incremented at request start and decremented in a `finally`, updating last activity at begin and completion
- [ ] 8.2 Write failing test then implement the hosted service unloading when running, idle, past the timeout, and not transitioning, using the injected clock
- [ ] 8.3 Write failing test then implement the active-request guard so an in-flight request defers the unload until it completes
- [ ] 8.4 Write failing test then implement: activity before the timeout defers the unload and restarts the measurement
- [ ] 8.5 Write failing test then implement: no unload action while `Starting` or `Stopping`
- [ ] 8.6 Confirm the default idle timeout is 5 minutes and is configurable

## 9. OpenRouter client

- [ ] 9.1 Implement the client with configurable base URL, model mappings, and timeout, reading the API key from configuration, environment, or user secrets
- [ ] 9.2 Write failing test then implement optional application title and referer headers driven by configuration
- [ ] 9.3 Write failing test then implement: no outbound call is made when cloud use is disabled
- [ ] 9.4 Write failing test then implement error translation for authentication and rate-limit failures with keys and authorization headers absent from logs

## 10. Resilience and circuit breaker

- [ ] 10.1 Write failing test then implement the single controlled local→cloud fallback marking `IsFallback` on the recorded decision
- [ ] 10.2 Write failing test then implement: a failing fallback does not cascade into further retries
- [ ] 10.3 Write failing test then implement: requests including a non-idempotent tool action are not automatically retried
- [ ] 10.4 Write failing test then implement the circuit breaker opening after the configured consecutive failures and logging the change
- [ ] 10.5 Write failing test then implement: an open breaker attempts no local start during cooldown and uses the permitted fallback or errors
- [ ] 10.6 Write failing test then implement breaker closing after the cooldown
- [ ] 10.7 Write failing tests then verify cancellation propagates through local startup, local inference, and cloud inference

## 11. API endpoints

- [ ] 11.1 Implement `POST /v1/chat/completions` returning an OpenAI-shaped response for a non-streaming `auto` request
- [ ] 11.2 Write failing test then implement `400 Bad Request` for `stream: true` with an explanatory message
- [ ] 11.3 Write failing tests then implement request validation rejecting empty `messages` and unknown roles without starting the model or contacting a provider
- [ ] 11.4 Implement `GET /health`, `/health/live`, `/health/ready`, with liveness independent of local model state and readiness unhealthy when no provider is usable
- [ ] 11.5 Implement `POST /api/router/local-model/start` and `/stop` under the administrative authorization policy
- [ ] 11.6 Write failing test then verify a chat-only caller cannot reach administrative endpoints

## 12. Security

- [ ] 12.1 Configure Kestrel to bind loopback by default and add a test asserting the default binding
- [ ] 12.2 Implement API-key authentication using `CryptographicOperations.FixedTimeEquals` over fixed-length hashes of the presented and configured keys
- [ ] 12.3 Add distinct chat and administrative authorization policies and wire endpoints to them
- [ ] 12.4 Write failing tests then verify unauthenticated administrative calls are rejected without performing the action, and a valid key is admitted

## 13. Shell-command tool

- [ ] 13.1 Define the `execute_shell_command` contract (`shell`, `command`, `workingDirectory`, `timeoutSeconds`, `dryRun`) and the result carrying stdout, stderr, and exit code separately
- [ ] 13.2 Write failing test then implement rejection under default configuration because shell execution is disabled
- [ ] 13.3 Write failing tests then implement working-directory allow-listing applied to the resolved canonical path, rejecting traversal escapes before any process starts
- [ ] 13.4 Write failing test then implement destructive-pattern blocking (disk formatting, shutdown, credential dumping, recursive root deletion) evaluated before the confirmation path
- [ ] 13.5 Write failing test then implement the `confirmation_required` pending action with generated `actionId` and `summary`, starting no process
- [ ] 13.6 Write failing test then implement allow-listed execution via `pwsh`/`powershell` on Windows and `bash` on Linux, never `cmd.exe /c`, with arguments as argument-list entries
- [ ] 13.7 Write failing tests then implement the execution timeout and output cap at `MaximumOutputCharacters` recording truncation
- [ ] 13.8 Write failing test then implement `dryRun` starting no process
- [ ] 13.9 Write failing test then verify logs carry command metadata but not captured output

## 14. Observability and status

- [ ] 14.1 Add correlation IDs and structured logging for strategy, provider, reason, startup duration, inference duration, fallback, process exits, breaker changes, idle unloads, and tool outcomes
- [ ] 14.2 Write failing test then verify API keys, authorization headers, and full prompts are absent from logs for a request containing secrets
- [ ] 14.3 Add `System.Diagnostics.Metrics` counters for local requests, cloud requests, startups, startup failures, fallbacks, unloads, decisions by strategy, and latency
- [ ] 14.4 Write failing tests then verify the fallback and cloud counters both increment on fallback, and decisions are counted with strategy as a dimension
- [ ] 14.5 Implement `GET /api/router/status` reporting state, process ID, profile, local request count, last activity, idle timeout, OpenRouter configured flag, last decision summary, breaker state, and GPU availability when detectable
- [ ] 14.6 Write failing tests then verify status contains no secrets and degrades to unknown when GPU availability cannot be detected

## 15. Integration tests

- [ ] 15.1 Add an in-process fake llama.cpp server (health endpoint plus chat completions) and a fake OpenRouter server
- [ ] 15.2 Add integration tests over real HTTP for local startup and forwarding, concurrent startup, and local reuse using temporary executable scripts
- [ ] 15.3 Add integration tests for cloud fallback, cloud prohibition, and circuit-breaker behaviour end to end
- [ ] 15.4 Add integration tests for the status endpoint and administrative authorization
- [ ] 15.5 Confirm every `[Fact]` has a `DisplayName`, class and method names read as snake-case plain-English sentences, and assertions carry `because` explanations where the reason is not obvious

## 16. Scripts and documentation

- [ ] 16.1 Add `scripts/Start-Router.ps1` and `scripts/Test-LocalModel.ps1`
- [ ] 16.2 Write `README.md` covering request flow, llama.cpp start/unload, fallback, configuring Bonsai, configuring OpenRouter securely, running tests, and inspecting status
- [ ] 16.3 Add a Mermaid architecture diagram to the README
- [ ] 16.4 Add README sections for threat model, design decisions, known limitations, and roadmap, stating explicitly why shell execution is dangerous and that pattern blocking is not a complete security boundary
- [ ] 16.5 Document that loopback exposure is the default and LAN exposure requires authentication and TLS via a trusted reverse proxy
- [ ] 16.6 Add example `curl.exe` and `Invoke-RestMethod` requests, and document how future Android, voice, camera, email, and Home Assistant clients use the same API
- [ ] 16.7 Document the manual end-to-end test procedure for dependencies that cannot be exercised automatically

## 17. Final verification

- [ ] 17.1 Run `dotnet format`
- [ ] 17.2 Run `dotnet build` and correct all warnings
- [ ] 17.3 Run `dotnet test` and correct all failures
- [ ] 17.4 Confirm no placeholder methods, pseudocode, or `TODO: implement` comments remain
- [ ] 17.5 Report the resulting repository tree and summarise design decisions and remaining limitations
