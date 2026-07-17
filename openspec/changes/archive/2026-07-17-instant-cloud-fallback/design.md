## Context

Currently, when a request arrives and the local model isn't running, `Dispatcher.Dispatch()` calls `Manager.Start()` synchronously, blocking until the model becomes ready or times out (2 minutes). This is unacceptable UX — users should get immediate responses.

The existing flow:
1. Request → Dispatcher checks strategy
2. If local: calls `Manager.Start()` (blocks up to 2min)
3. Then calls `LocalClient.Stream()`
4. On failure before first chunk: falls back to cloud

## Goals / Non-Goals

**Goals:**
- First request served immediately via cloud when model is cold
- Local model starts in background during first cloud request
- Subsequent requests use local model once ready
- Zero config changes required

**Non-Goals:**
- Prefetching or warm-up on router startup
- Predicting which requests should go to cloud
- Streaming from both providers simultaneously
- Changing the cloud fallback behavior for model failures (only cold start)

## Decisions

### 1. Check model state before blocking on Start

**Decision**: In `Dispatcher.Dispatch()`, check `Manager.State()` before calling `Manager.Start()`. If state is `Stopped`, skip the blocking start and route directly to cloud. Trigger a background `Manager.Start()` concurrently.

**Rationale**: Simple state check avoids blocking. The manager already tracks state (`Stopped`, `Starting`, `Ready`, etc.) so no new machinery needed.

**Alternative considered**: Always try local first with a short timeout (e.g., 500ms). Rejected because any delay is unnecessary when we know the model isn't running.

### 2. Non-blocking background start

**Decision**: Add `Manager.StartInBackground(ctx)` method that launches `doStart()` in a goroutine without waiting for readiness. Returns immediately.

**Rationale**: Keeps `Start()` synchronous for cases where callers need to wait, while giving the dispatcher a non-blocking option.

**Alternative considered**: Use a channel to signal readiness. Rejected as over-engineering — the existing `State()` check is sufficient.

### 3. Request-triggered startup only

**Decision**: Start the model only when a local-eligible request arrives, not on router startup.

**Rationale**: Saves resources when no one is using the router. The first request pays the cloud latency but subsequent requests get local speed.

## Risks / Trade-offs

- **First request latency**: Goes from "timeout after 2min" to "cloud latency (~200ms)". Major improvement.
- **Thundering herd**: Multiple concurrent cold-start requests could all trigger `StartInBackground()`. Mitigated by existing `singleflight.Group` in `Manager.doStart()`.
- **Race between background start and next request**: If a second request arrives while the model is still starting (state=`Starting`), it should also go to cloud. The existing `Manager.Start()` handles this via singleflight — it waits for the in-progress start. We should check state again after `Start()` returns.
- **Model never becomes ready**: Background start could fail. Subsequent requests would keep trying cloud, which is acceptable degradation.
