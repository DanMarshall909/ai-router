## Why

When the local model isn't running, the router currently waits for it to start (up to 2 minutes) before timing out and returning an error. Users shouldn't have to wait for cold starts — requests should be served immediately by a cloud provider while the local model boots in the background.

## What Changes

- **Immediate cloud fallback on cold start**: When the local model is not running, route the request to the cloud provider immediately while starting the local model in the background
- **Background model readiness**: The local model starts asynchronously; subsequent requests use it once ready
- **No more startup timeout blocking**: Requests never block waiting for llama-server to become healthy

## Capabilities

### New Capabilities

- `instant-cloud-fallback`: Routes requests to cloud immediately when local model is not ready, starts local model in background for future requests

### Modified Capabilities

- `local-model-lifecycle`: Startup no longer blocks request dispatch; model starts in background while requests flow to cloud

## Impact

- **Dispatcher**: Changes routing logic to check model state and bypass local on cold start
- **Manager**: Adds non-blocking start method for background initialization
- **Handler/Client**: No changes needed — dispatcher handles routing transparently
- **Latency**: First request(s) served immediately via cloud instead of timing out
