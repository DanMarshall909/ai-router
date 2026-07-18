## 1. Module and package structure

- [x] 1.1 Delete the abandoned .NET tree (`src/`, `tests/`, `LocalFirst.AiRouter.sln`, `Directory.Build.props`, `appsettings.example.json`) — none of it is committed, and it implements groups 1–5 of the superseded .NET plan
- [ ] 1.2 Initialise the Go module with `cmd/airouter` and `internal/{config,routing,local,cloud,shell,httpapi,observability}`, with `internal/routing` importing nothing that performs I/O
- [ ] 1.3 Add `golang.org/x/sync` and `github.com/stretchr/testify` as the only direct dependencies, and record in the README that `testify/mock` is deliberately excluded
- [x] 1.4 Add `.gitignore` covering build output and local configuration files that may carry keys
- [ ] 1.5 Verify `go build ./...` and `go test ./...` succeed on the empty module, and `go vet ./...` is clean

## 2. Configuration and validation

- [ ] 2.1 Add typed config structs for `localModel`, `openRouter`, `routing`, `shellTool`, and `modelRouting.strategies`, each with named exported constants for its key path and defaults
- [ ] 2.2 Load `config.json` via `encoding/json`, apply environment-variable overrides, and validate at startup with errors naming the offending section and setting
- [x] 2.3 Write failing tests then implement: invalid configuration fails fast; cloud settings are optional when cloud is disabled; enabled cloud without an API key fails with a message explaining the environment variable
- [x] 2.4 Write failing test then implement: a strategy with no `modelRouting` mapping fails startup validation naming the strategy
- [ ] 2.5 Write failing test then implement: the unmodified `<configure-me: ...>` placeholder fails startup validation naming the strategy and pointing at the OpenRouter model catalogue
- [ ] 2.6 Add `config.example.json` with all sections documented, an empty API key, and placeholder cloud model identifiers

## 3. Domain types and provider abstractions

- [x] 3.1 Add `InferenceStrategy`, `LocalModelState` (`Stopped|Starting|Ready|Stopping|Faulted`, no `Busy`), and `RoutingDecision` to `internal/routing` ✓ implemented
- [x] 3.2 Add explicit `RequestContext` and `RoutingSignals` structs (no loose maps), including the flag marking a request ineligible for self-assessment ✓ implemented
- [x] 3.3 Define the `ChatProvider` interface returning `iter.Seq2[Chunk, error]`, the local-manager interface, and the `Clock` interface, each declared in its consuming package
- [x] 3.4 Add a deterministic test clock and fake `ChatProvider` implementations (hand-written; no mocking framework)

## 4. Deterministic routing policy

- [ ] 4.1 Write failing tests then implement signal extraction: approximate input token count, coding-shaped, reasoning-shaped, tools requested, request length
- [ ] 4.2 Write failing test then implement: `model: auto` on an ordinary request selects `QuickLocal`
- [ ] 4.3 Write failing test then implement: a request over the complexity threshold selects the configured cloud reasoning or cloud coding strategy
- [ ] 4.4 Write failing test then implement: input exceeding `routing.maxLocalInputTokens` never selects a local strategy
- [ ] 4.5 Write failing test then implement: identical request and system state yield an identical `RoutingDecision`
- [ ] 4.6 Write failing tests then implement explicit override: a permitted override is honoured and recorded in the reason; a policy-violating override is rejected with an explanatory response
- [ ] 4.7 Write failing test then implement: cloud prohibition returns a clear service-unavailable response and transmits nothing externally
- [ ] 4.8 Write failing test then implement strategy→provider/model resolution from configuration

## 5. Optional local self-assessment

- [x] 5.1 Write failing test then implement: self-assessment is disabled by default and issues no classification prompt
- [ ] 5.2 Implement the classifier requesting strict JSON naming one of the five strategies
- [ ] 5.3 Write failing test then implement: a classifier recommendation cannot bypass privacy, cost, or security policy
- [ ] 5.4 Write failing test then implement: classification requests are structurally ineligible for further classification
- [ ] 5.5 Write failing test then implement: malformed classifier output falls back to the deterministic decision and is logged without prompt content

## 6. llama.cpp process manager

- [x] 6.1 Add the process abstraction over `os/exec` (start, wait/exit notification, kill, stdout/stderr readers) plus a fake simulating slow start, crash, hang, and unresponsive shutdown
- [ ] 6.2 Write failing test then implement start with arguments as separate `exec.Command` entries, covering paths containing spaces and quotes
- [ ] 6.3 Write failing test then implement readiness by polling the llama.cpp health endpoint against an `httptest` server, bounded by the configured startup timeout
- [x] 6.4 Write failing test then implement single-flight startup via `singleflight.Group` so five concurrent requests start exactly one process and share the result
- [x] 6.5 Write failing test then implement: a start that fails is retried on the next request rather than cached, because `singleflight` must not behave like `sync.Once`
- [x] 6.6 Write failing test then implement reuse of an already-ready process
- [ ] 6.7 Write failing test then implement state tracking through `Stopped` → `Starting` → `Ready`, exposing the process ID, holding the mutex only across state reads and writes
- [x] 6.8 Write failing test then implement `IsBusy` derived from the active-request count, and verify concurrent requests do not change lifecycle state
- [x] 6.9 Write failing test then implement `Faulted` on launch failure or readiness timeout
- [ ] 6.10 Write failing test then implement graceful stop, and force-kill of the process group/job object after the shutdown timeout
- [ ] 6.11 Write failing test then implement unexpected-exit detection setting `Faulted` and preventing routing to the dead process
- [ ] 6.12 Verify all process resources and goroutines are released on stop and on shutdown, with `go test -race` clean

## 7. Local model client

- [x] 7.1 Implement the OpenAI-compatible `llama-server` client over `http.Client` with configured timeouts and `ctx` cancellation preserved, yielding `iter.Seq2[Chunk, error]`
- [ ] 7.2 Write failing tests then implement distinct outcomes for startup failure, connection failure, timeout, invalid response, and generation failure
- [x] 7.3 Write failing test then implement before-first-chunk versus after-first-chunk failure reporting, so the dispatcher can tell whether fallback remains available
- [ ] 7.4 Write failing test then verify full prompts are not logged by default

## 8. Idle unload loop

- [ ] 8.1 Implement active-request counting incremented at request start and decremented in a `defer`, updating last activity at begin and on each generated chunk
- [ ] 8.2 Write failing test then implement the idle goroutine unloading when running, idle, past the timeout, and not transitioning, using the injected clock
- [ ] 8.3 Write failing test then implement the active-request guard so an in-flight request defers the unload until it completes
- [ ] 8.4 Write failing test then implement: activity before the timeout defers the unload and restarts the measurement
- [ ] 8.5 Write failing test then implement: no unload action while `Starting` or `Stopping`
- [ ] 8.6 Write failing test then implement: the loop exits only on `ctx.Done()`
- [ ] 8.7 Confirm the default idle timeout is 5 minutes and is configurable

## 9. OpenRouter client

- [ ] 9.1 Implement the client with configurable base URL, model mappings, and timeout, reading the API key from configuration or the environment
- [ ] 9.2 Write failing test then implement optional application title and referer headers driven by configuration
- [ ] 9.3 Write failing test then implement: no outbound call is made when cloud use is disabled
- [ ] 9.4 Write failing test then implement error translation for authentication and rate-limit failures with keys and authorization headers absent from logs

## 10. Resilience and fallback

- [ ] 10.1 Write failing test then implement the single controlled local→cloud fallback marking `IsFallback` on the recorded decision
- [ ] 10.2 Write failing test then implement: fallback is taken when local fails before the first chunk of a streaming request, and the client sees a complete stream with no partial local content
- [ ] 10.3 Write failing test then implement: a local failure after the first chunk has been flushed terminates the stream and is not re-dispatched
- [ ] 10.4 Write failing test then implement: a non-streaming request retains the full fallback window at every point before the aggregated body is written
- [ ] 10.5 Write failing test then implement: a failing fallback does not cascade into further retries
- [ ] 10.6 Write failing test then implement: requests including a non-idempotent tool action are not automatically retried
- [ ] 10.7 Write failing tests then verify `ctx` cancellation propagates through local startup, local inference, and cloud inference

## 11. Self-healing

- [ ] 11.1 Write failing test then implement the liveness prober running for the whole time the model is `Ready`, and not at all while `Stopped` or `Stopping`
- [ ] 11.2 Write failing test then implement recycle after the configured number of *consecutive* probe failures: kill, `Faulted`, logged with the recycle reason
- [ ] 11.3 Write failing test then implement: a single transient probe failure does not recycle a working model
- [ ] 11.4 Write failing test then implement: probe activity does not count as request activity for the idle guard, so probing cannot keep the model loaded forever
- [ ] 11.5 Write failing test then implement: a probe failure while a request is still streaming successfully defers the kill until that request completes or fails
- [ ] 11.6 Write failing test then implement startup adoption: a healthy server already on the configured host and port is adopted straight to `Ready` with no cold start and no second process
- [ ] 11.7 Write failing test then implement: an adopted server is idle-unloaded exactly as a spawned one
- [ ] 11.8 Write failing test then implement: an occupied but unhealthy port is not raced — no competing spawn, `Faulted` with an explanatory reason
- [ ] 11.9 Write failing test then implement: a free port starts normally with no adoption
- [ ] 11.10 Write failing test then implement the half-open breaker admitting exactly one trial after the cooldown, with the remainder taking the permitted fallback or error
- [ ] 11.11 Write failing test then implement: a successful trial closes the breaker and resets both the failure count and the cooldown to its base
- [ ] 11.12 Write failing test then implement: a failed trial re-opens the breaker with a longer cooldown, bounded by the configured maximum
- [ ] 11.13 Write failing test then implement panic recovery in the idle and liveness loops: recovered, logged, counted, next iteration still runs
- [ ] 11.14 Write failing test then verify startup is idempotent, so an external supervisor restarting the router after an unclean exit converges without manual intervention

## 12. API endpoints

- [x] 12.1 Implement `POST /v1/chat/completions` returning an OpenAI-shaped response for a non-streaming `auto` request, aggregating the provider iterator
- [ ] 12.2 Write failing test then implement SSE streaming for `stream: true`: incremental `choices[0].delta.content` chunks, `data: [DONE]`, and `http.Flusher.Flush` per chunk rather than buffering
- [ ] 12.3 Write failing test then verify the provider choice is invisible to a streaming client, and that the aggregated non-streaming body equals the concatenated chunks
- [ ] 12.4 Write failing test then implement: client disconnect cancels upstream work via request `ctx`
- [ ] 12.5 Write failing tests then implement request validation rejecting empty `messages` and unknown roles without starting the model or contacting a provider
- [ ] 12.6 Implement `GET /health`, `/health/live`, `/health/ready`, with liveness independent of local model state and readiness unhealthy when no provider is usable
- [ ] 12.7 Implement `POST /api/router/local-model/start` and `/stop` under the administrative authorization policy
- [ ] 12.8 Write failing test then verify a chat-only caller cannot reach administrative endpoints
- [x] 12.9 Write failing tests then implement OpenAI-compatible tool-calling passthrough for tool definitions, tool-call deltas, and tool-result messages; the router must not execute client tools

## 13. Security

- [ ] 13.1 Bind `127.0.0.1` by default and add a test asserting the default binding
- [ ] 13.2 Implement API-key authentication using `crypto/subtle.ConstantTimeCompare` over fixed-length SHA-256 hashes of the presented and configured keys
- [ ] 13.3 Add distinct chat and administrative authorization middleware and wire endpoints to them
- [ ] 13.4 Write failing tests then verify unauthenticated administrative calls are rejected without performing the action, and a valid key is admitted

## 14. Shell-command tool

- [ ] 14.1 Define the `execute_shell_command` contract (`shell`, `command`, `workingDirectory`, `timeoutSeconds`, `dryRun`) and the result carrying stdout, stderr, and exit code separately
- [ ] 14.2 Write failing test then implement rejection under default configuration because shell execution is disabled
- [ ] 14.3 Write failing tests then implement working-directory allow-listing applied to the `filepath.Abs` + `EvalSymlinks` resolved path, compared component-wise, rejecting traversal escapes before any process starts
- [ ] 14.4 Write failing test then implement destructive-pattern blocking (disk formatting, shutdown, credential dumping, recursive root deletion) evaluated before the confirmation path so a destructive command is never offered as confirmable
- [ ] 14.5 Write failing test then implement the `confirmation_required` pending action with generated `actionId` and `summary`, starting no process
- [ ] 14.6 Write failing test then implement pending actions storing the fully-resolved command and working directory server-side, with confirmation carrying only the `actionId`
- [ ] 14.7 Write failing test then implement: a confirmation supplying a different command does not execute it, because the stored command is authoritative
- [ ] 14.8 Write failing tests then implement TTL expiry (via the injected clock), single-use action IDs, and rejection of unknown IDs — each without executing anything
- [ ] 14.9 Write failing test then verify pending actions are in-memory only, so a restart discards them and a pre-restart `actionId` is rejected
- [ ] 14.10 Write failing test then implement allow-listed execution via `pwsh`/`powershell` on Windows and `bash` on Linux, never `cmd.exe /c`, with arguments as separate argument entries
- [ ] 14.11 Write failing tests then implement the execution timeout and output cap at `shellTool.maximumOutputCharacters` recording truncation
- [ ] 14.12 Write failing test then implement `dryRun` starting no process
- [ ] 14.13 Write failing test then verify logs carry command metadata but not captured output

## 15. Observability and status

- [ ] 15.1 Add correlation IDs and `log/slog` structured logging for strategy, provider, reason, startup duration, inference duration, fallback, process exits, breaker changes, idle unloads, and tool outcomes
- [ ] 15.2 Write failing test then verify API keys, authorization headers, and full prompts are absent from logs for a request containing secrets
- [ ] 15.3 Add in-memory counters for local requests, cloud requests, startups, startup failures, fallbacks, unloads, decisions by strategy, recycles, adoptions, breaker transitions, and panic recoveries
- [ ] 15.4 Write failing test then implement latency recorded as both time-to-first-chunk and total duration, with time-to-first-chunk including the cold-start wait
- [ ] 15.5 Write failing tests then verify the fallback and cloud counters both increment on fallback, and decisions are counted with strategy as a dimension
- [ ] 15.6 Implement `GET /api/router/status` reporting state, process ID, profile, local request count, last activity, idle timeout, OpenRouter configured flag, last decision summary, breaker state including half-open, recovery counts, and GPU availability when detectable
- [ ] 15.7 Write failing tests then verify status contains no secrets and degrades to unknown when GPU availability cannot be detected
- [ ] 15.8 Write failing test then implement `nvidia-smi` detection with a fixed argument list behind a swappable interface, cached with a short TTL so repeated status polls spawn at most one process per window
- [ ] 15.9 Write failing test then verify routing decisions are unaffected by GPU availability, because the field is diagnostic only
- [x] 15.10 Embed and log the UTC build timestamp during update-script builds
- [x] 15.11 Add an opt-in `--debug-log` update-script option for request and response tracing

## 16. Integration tests

- [ ] 16.1 Add an in-process fake llama.cpp server (health endpoint plus streaming chat completions) and a fake OpenRouter server using `httptest`
- [ ] 16.2 Add integration tests over real HTTP for local startup and forwarding, concurrent startup, and local reuse using temporary executable scripts
- [ ] 16.3 Add integration tests for streaming end to end, including fallback before first chunk and stream termination after first chunk
- [ ] 16.4 Add integration tests for cloud fallback, cloud prohibition, and breaker open/half-open/closed behaviour end to end
- [ ] 16.5 Add integration tests for orphan adoption: start the router against an already-running fake server and assert adoption without a spawn
- [ ] 16.6 Add integration tests for the status endpoint and administrative authorization
- [ ] 16.7 Confirm every test name reads as a plain-English sentence via `t.Run`, and `require` assertions carry a `"because ..."` message wherever the reason is not obvious

## 17. Scripts and documentation

- [ ] 17.1 Add `scripts/Start-Router.ps1` and `scripts/Test-LocalModel.ps1`
- [ ] 17.2 Write `README.md` covering request flow, llama.cpp start/unload, fallback, configuring Bonsai, configuring OpenRouter securely, running tests, and inspecting status
- [ ] 17.3 Add a Mermaid architecture diagram to the README
- [ ] 17.4 Add README sections for threat model, design decisions, known limitations, and roadmap, stating explicitly why shell execution is dangerous and that pattern blocking is not a complete security boundary
- [ ] 17.5 Document running the router under a Windows Service (or NSSM) with restart-on-failure and under `systemd` with `Restart=always`, and explain why the router does not supervise itself
- [ ] 17.6 Document that loopback exposure is the default and LAN exposure requires authentication and TLS via a trusted reverse proxy
- [ ] 17.7 Add a dated "known-good as of" table of suggested OpenRouter model identifiers, explicitly not configuration defaults
- [ ] 17.8 Add example `curl.exe` and `Invoke-RestMethod` requests including a streaming one, and document how future Android, voice, camera, email, and Home Assistant clients use the same API
- [ ] 17.9 Document the manual end-to-end test procedure for dependencies that cannot be exercised automatically

## 18. Final verification

- [ ] 18.1 Run `gofmt -l .` and `go vet ./...` and correct everything reported
- [ ] 18.2 Run `go build ./...` and correct all warnings
- [ ] 18.3 Run `go test -race ./...` and correct all failures
- [ ] 18.4 Confirm no placeholder functions, pseudocode, or `TODO: implement` comments remain
- [ ] 18.5 Report the resulting repository tree and summarise design decisions and remaining limitations

## Audit Reconciliation (2026-07-17)

Checked tasks are reserved for work whose stated behavior is implemented and supported by an appropriate focused test, or for completed repository-structure work that can be directly inspected. Reopened tasks fall into one or more of these categories:

- **Partial**: some code exists, but the required behavior is incomplete.
- **Divergent**: current behavior differs from the task or requirement.
- **Unproven**: code exists, but the required focused test or verification has not been run.

The following groups require reconciliation before they can be marked complete again:

- **1–2**: required package/dependency/config-example details and full environment override coverage.
- **4**: deterministic signal extraction and policy are absent; current `auto` routing is handler-default/self-assessment driven.
- **6–7**: several lifecycle guarantees lack focused tests; process-group termination, faulted routing protection, resource cleanup, and prompt-redaction proof remain incomplete.
- **8**: idle-loop behavior exists but does not meet the injected-clock, streaming activity, and test requirements.
- **9–10**: cloud configuration, header independence, disablement, redaction, and fallback behavior need the specified tests and guards.
- **12**: core endpoint behavior exists, but required OpenAI response fields, streaming verification, provider invisibility, disconnect handling, and no-side-effect validation tests remain incomplete.
- **11 and 13–18**: remain largely or wholly unimplemented as already unchecked.
