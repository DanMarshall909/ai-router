## Context

The router currently converts a narrow `ChatCompletionRequest` into an internal request, forwards only tools-related fields, and emits synthetic response IDs plus a router-specific `debug` object. The local llama.cpp and OpenRouter adapters accept different subsets of Chat Completions. Silently ignoring a field can produce a successful but semantically different completion.

This change covers the OpenAI Chat Completions endpoint only. The router remains local-first, uses llama.cpp locally and OpenRouter for cloud inference, and must retain its existing privacy and single-fallback guarantees.

## Goals / Non-Goals

**Goals:**
- Accept and validate the practical Chat Completions request contract: messages and content parts, generation controls, tools, structured output, log probabilities, metadata, and stream options.
- Preserve supported request fields unchanged through the routing and provider layers.
- Emit OpenAI-shaped success, stream, and error payloads with no router-only fields in the public envelope.
- Choose and fall back only to providers that can meet the requested feature set.
- Establish contract tests that run the same fixtures against local and cloud adapter fakes.

**Non-Goals:**
- Implement Responses, Assistants, Realtime, Batch, Files, Images, Audio, or fine-tuning APIs.
- Make a local GGUF model support capabilities unavailable from llama.cpp or its chat template.
- Execute tools, validate tool arguments against application schemas, or guarantee model adherence to a JSON schema.
- Proxy arbitrary undocumented OpenAI fields or provider extensions.

## Decisions

### Use typed protocol models with an explicit supported-field matrix

Replace raw JSON request fragments with typed OpenAI protocol models at the HTTP boundary. Message content is a tagged union for text, image URL, input audio, and refusal parts; assistant tool calls and tool-result IDs remain typed. Fields not represented by the supported matrix are rejected with an `invalid_request_error` naming the field rather than dropped.

A typed model makes validation and cross-provider serialization auditable. A generic map would preserve more bytes but cannot distinguish unknown fields from valid features or prove that fallback preserves semantics.

### Carry a compatibility profile in the routing request

Validation derives a `RequiredCapabilities` profile from each request, such as tools, parallel tool calls, JSON object mode, strict JSON schema, vision, input audio, log probabilities, seed, and stream usage. Each adapter publishes immutable capabilities from its configured server version and model/template settings. The router selects only an adapter satisfying the profile; explicit requests that cannot be met fail before provider dispatch.

Routing based only on the requested model was considered, but it permits an automatic request to reach a provider that silently ignores an option. Capability-aware selection preserves request meaning without treating local and cloud providers as interchangeable.

### Preserve the request semantics across fallback

The normalized request is the sole object sent to both initial and fallback providers. Before the first response byte, fallback is allowed only when the fallback adapter satisfies the same compatibility profile. A request whose required capability cannot be preserved returns an OpenAI-shaped error, not a degraded completion.

This deliberately favors correctness over availability for feature-specific requests.

### Produce public protocol envelopes at the HTTP boundary

The handler generates cryptographically random `chatcmpl-` IDs, fixes `created` at request start, includes the resolved model ID, and maps provider chunks into OpenAI completion chunks. Non-streaming aggregation produces `usage` when the adapter supplies it; streaming with `stream_options.include_usage` emits the final usage chunk before `[DONE]`. Router diagnostics remain structured log fields and trace data, never response fields.

Passing upstream response bodies through was rejected because local and cloud envelopes differ and would expose provider routing details.

### Normalize errors once

Introduce a protocol error type with `message`, `type`, `param`, and `code`. Decode, validation, unsupported capability, authentication, timeout, and provider failures map to this type with an appropriate HTTP status. Streaming errors are emitted as an OpenAI-shaped SSE error event only after headers have been committed; pre-first-byte failures use the normal JSON error response or eligible fallback.

## Risks / Trade-offs

- [OpenAI evolves fields faster than the router] → Maintain an explicit support matrix and reject unimplemented fields with a field-specific error rather than silently accepting them.
- [Adapter capability declarations drift from installed llama.cpp behavior] → Derive local capabilities from explicit configuration and add adapter contract tests against supported server versions.
- [Strict schema or multimodal requests reduce local-first availability] → Route compatible automatic requests to cloud when allowed; return a clear compatibility error when privacy policy prohibits cloud.
- [Removing `debug` may break an undocumented client] → Keep diagnostics in existing logs/traces and document the public-envelope change as breaking.
- [Token accounting differs between providers] → Report usage only when supplied by an adapter and omit unavailable detailed fields instead of fabricating counts.

## Migration Plan

1. Add typed request, response, error, and capability models alongside the current types.
2. Update both adapters and routing tests, then switch the public handler to the normalized models.
3. Remove the public `debug` field and publish the supported-field matrix in the README and API specification.
4. Deploy with trace logging enabled for a bounded validation window; roll back by restoring the prior binary if a client depends on undocumented response fields.

## Open Questions

- Which installed llama.cpp version and chat templates will be declared as supporting vision, audio, and strict structured outputs? Until configured and tested, those capabilities remain unavailable locally.
- Whether OpenRouter provider-specific request extensions need a separately namespaced pass-through API; this change intentionally excludes them.
