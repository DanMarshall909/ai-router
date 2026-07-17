## MODIFIED Requirements

### Requirement: On-demand local model startup
A local model manager SHALL start the configured `llama-server` executable when a local-eligible request arrives and the model is stopped. If the request should not block, the manager SHALL start the model in the background and route the request to the cloud provider. If the request should wait, the manager SHALL wait for the llama.cpp HTTP health endpoint to report readiness and only then forward the request. It SHALL wait no longer than the configured startup timeout. Arguments SHALL be passed as individual argument-list entries, never by building a command string.

#### Scenario: Starts once and forwards locally
- **WHEN** the local model is stopped and a local-eligible chat request arrives and the request waits for readiness
- **THEN** exactly one process is started, readiness is awaited, and the request is forwarded to the local server

#### Scenario: Cold start routes to cloud
- **WHEN** the local model is stopped and a local-eligible chat request arrives and the request does not wait for readiness
- **THEN** exactly one process is started in the background and the request is forwarded to the cloud provider

#### Scenario: Startup timeout is bounded
- **WHEN** the local server never reports readiness
- **THEN** startup is abandoned once the configured startup timeout elapses and the request follows the configured fallback path

#### Scenario: Arguments are not shell-concatenated
- **WHEN** the model path or additional arguments contain spaces or quotes
- **THEN** each argument is passed as a distinct argument-list entry and reaches the process intact
