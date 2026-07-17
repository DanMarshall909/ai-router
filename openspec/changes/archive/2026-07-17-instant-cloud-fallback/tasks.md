## 1. Manager: Non-blocking startup

- [x] 1.1 Add `StartInBackground(ctx)` method to Manager that launches `doStart()` in a goroutine without waiting
- [x] 1.2 Write test: `StartInBackground` returns within 10ms while model is `Stopped`
- [x] 1.3 Write test: `StartInBackground` is idempotent when model is already `Starting`

## 2. Dispatcher: Cold start routing

- [x] 2.1 Modify `Dispatch()` to check `Manager.State()` before calling `Start()`; if `Stopped`, route to cloud and call `StartInBackground()` concurrently
- [x] 2.2 Write test: request with model `Stopped` routes to cloud immediately
- [x] 2.3 Write test: subsequent request after model becomes `Ready` routes to local
- [x] 2.4 Write test: multiple concurrent cold-start requests coalesce to one background start

## 3. Integration verification

- [x] 3.1 Run full test suite with `-race` and verify all pass
- [x] 3.2 Manual test: start ai-router with model stopped, send request, verify immediate cloud response and background local startup in logs
