## ADDED Requirements

### Requirement: Instant cloud fallback on cold start
When a local-eligible request arrives and the local model is in `Stopped` state, the dispatcher SHALL route the request to the cloud provider immediately without waiting for the local model to start. The local model SHALL be started in the background concurrently.

#### Scenario: Cold start routes to cloud immediately
- **WHEN** a local-eligible request arrives with the model in `Stopped` state
- **THEN** the request is routed to the cloud provider and the local model starts in the background

#### Scenario: Subsequent request uses local model
- **WHEN** the local model has finished starting and a new local-eligible request arrives
- **THEN** the request is routed to the local model

#### Scenario: Multiple cold-start requests coalesce
- **WHEN** multiple local-eligible requests arrive concurrently while the model is `Stopped`
- **THEN** only one background start is triggered and all requests are routed to cloud

### Requirement: Non-blocking model startup
The manager SHALL provide a method to start the local model without blocking the caller. The method SHALL return immediately while startup proceeds in a goroutine.

#### Scenario: StartInBackground returns immediately
- **WHEN** `StartInBackground` is called while the model is `Stopped`
- **THEN** the method returns within 10ms and the model starts asynchronously

#### Scenario: StartInBackground is idempotent
- **WHEN** `StartInBackground` is called while the model is already `Starting`
- **THEN** no additional process is started and the method returns immediately
