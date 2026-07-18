## 1. Protocol Models and Validation

- [ ] 1.1 Define typed Chat Completions request, message/content-part, tool-call, response, stream-chunk, usage, and error models in the HTTP API package.
- [ ] 1.2 Define a single supported-field matrix and validate unsupported, malformed, and mutually incompatible request fields as parameter-specific OpenAI errors.
- [ ] 1.3 Normalize supported generation controls, response formats, metadata, and stream options into the routing request without losing supplied values.
- [ ] 1.4 Add unit tests for request decoding, role/content validation, unsupported-field rejection, and OpenAI error envelopes.

## 2. Provider Capability and Adapter Contracts

- [ ] 2.1 Add immutable provider capability declarations and a required-capability profile derived from each normalized request.
- [ ] 2.2 Update the local llama.cpp adapter to serialize every locally supported request field and parse content, refusal, reasoning, tool, finish, log-probability, and usage response fields.
- [ ] 2.3 Update the OpenRouter adapter to serialize every cloud-supported request field and parse the equivalent response and SSE fields.
- [ ] 2.4 Add adapter contract tests proving fields are preserved and unsupported local modalities or structured-output modes are identified before dispatch.

## 3. Capability-Aware Routing and Fallback

- [ ] 3.1 Incorporate required capabilities into deterministic automatic routing and explicit model validation.
- [ ] 3.2 Prevent local-to-cloud fallback when the configured fallback cannot satisfy the normalized request capability profile.
- [ ] 3.3 Add routing and fallback tests for local-compatible requests, cloud escalation, explicit incompatible models, and no-degradation failure behavior.

## 4. Response and Stream Compatibility

- [ ] 4.1 Generate stable per-request `chatcmpl-` IDs and protocol-correct non-streaming response envelopes without router `debug` fields.
- [ ] 4.2 Aggregate content, refusal, reasoning, tool calls, finish reasons, log probabilities, and provider usage for non-streaming responses.
- [ ] 4.3 Emit protocol-correct SSE deltas, final finish chunks, optional stream-usage chunks, `[DONE]`, and committed-stream error events.
- [ ] 4.4 Add handler tests for response envelopes, tool calls, reasoning, usage, stream errors, cancellation, and absence of internal metadata.

## 5. Discovery, Documentation, and Verification

- [ ] 5.1 Return protocol-correct `/v1/models` entries for `auto` and configured selectable models without unsupported capability claims.
- [ ] 5.2 Document the supported Chat Completions field matrix, local limitations, cloud escalation behavior, and removal of public `debug` fields in the README.
- [ ] 5.3 Create reusable end-to-end compatibility fixtures that run against local and cloud adapter fakes for non-streaming and streaming requests.
- [ ] 5.4 Run `go test ./... -race -count=1`, `go vet ./...`, and `openspec validate openai-chat-completions-compatibility --strict`.
