## ADDED Requirements

### Requirement: Routing produces an inference strategy, not a model name
The routing policy SHALL return an `InferenceStrategy` value from the set `QuickLocal`, `DeepLocal`, `CloudGeneral`, `CloudReasoning`, `CloudCoding`. Concrete providers and model identifiers SHALL be resolved from configuration (`ModelRouting:Strategies`), so models can change without modifying routing code. No cloud model identifier SHALL be hard-coded in source.

#### Scenario: Strategy maps to a configured model
- **WHEN** the policy selects `CloudReasoning` and configuration maps that strategy to a provider and model
- **THEN** the request is dispatched to the configured provider and model, and changing the configured model changes the dispatch without a code change

#### Scenario: Unmapped strategy is a configuration failure
- **WHEN** a strategy the policy can select has no configured mapping
- **THEN** startup configuration validation fails with a message naming the unmapped strategy

### Requirement: Deterministic routing policy
Routing SHALL be deterministic for a given request and system state. The policy SHALL consider explicit client override, local model availability and startup state, request length, approximate input token count, whether tools are requested, whether the request appears to involve coding, whether it requests extensive reasoning/research/comparison, local circuit-breaker state, whether OpenRouter is configured, and configured cost and privacy preferences. Signals SHALL be carried in explicit request-context and routing-signal types rather than loose dictionaries.

#### Scenario: Same inputs give the same decision
- **WHEN** the identical request is routed twice against identical system state
- **THEN** the policy returns the identical `RoutingDecision`, because the policy is deterministic

#### Scenario: Auto prefers local
- **WHEN** `model` is `auto`, the request is ordinary conversation, and local inference is permitted
- **THEN** the policy selects `QuickLocal`

#### Scenario: Heavy task escalates to cloud
- **WHEN** a request exceeds the configured complexity threshold or clearly requests extensive reasoning
- **THEN** the policy selects the configured cloud reasoning or cloud coding strategy, subject to privacy policy

#### Scenario: Oversized input escalates to cloud
- **WHEN** the approximate input token count exceeds `Routing:MaxLocalInputTokens`
- **THEN** the policy does not select a local strategy

### Requirement: Every request records a routing decision
The router SHALL produce a `RoutingDecision(Strategy, Provider, Model, Reason, Confidence, IsFallback)` for every chat request, and SHALL record it in structured logs.

#### Scenario: Decision recorded on the normal path
- **WHEN** any chat request completes
- **THEN** a routing decision with a non-empty reason is recorded, and `IsFallback` is true only when the cloud served the request after a local failure

### Requirement: Explicit client override
The router SHALL honour a permitted explicit model or strategy override supplied by the client. An override that violates configured privacy or cost policy SHALL be rejected with an explanatory response rather than silently downgraded.

#### Scenario: Permitted override honoured
- **WHEN** a client requests a specific permitted model instead of `auto`
- **THEN** the router routes to that model and the routing reason records that an override was applied

#### Scenario: Policy-violating override rejected
- **WHEN** a client requests a cloud model while cloud access is disabled by policy
- **THEN** the router rejects the request with an explanatory response and transmits nothing externally

### Requirement: Cloud prohibition is absolute
When cloud access is disabled by configuration, the router SHALL NOT transmit any request content to an external provider under any circumstances, including when local inference is unavailable.

#### Scenario: Local unavailable and cloud disabled
- **WHEN** cloud access is disabled and local inference cannot start
- **THEN** the router returns a clear service-unavailable response and no outbound cloud request is made

### Requirement: Optional local self-assessment is advisory only
The router MAY support a second-stage local self-assessment in which the local model is asked, via a small classification prompt, to return strict JSON naming one of the five strategies. This SHALL be disabled by default (`Routing:EnableLocalSelfAssessment`). The deterministic router SHALL remain authoritative: the classifier MAY recommend escalation but SHALL NOT bypass privacy, cost, or security policy. A classification request SHALL NOT trigger another classification request.

#### Scenario: Disabled by default
- **WHEN** configuration leaves self-assessment disabled
- **THEN** routing decisions are made purely deterministically and no classification prompt is issued

#### Scenario: Classifier cannot override policy
- **WHEN** self-assessment is enabled and the classifier recommends a cloud strategy while cloud access is disabled
- **THEN** the recommendation is discarded and the deterministic decision stands

#### Scenario: No recursive classification
- **WHEN** a classification request is itself dispatched to the local model
- **THEN** it is routed without triggering a further classification, because classification requests are excluded from self-assessment

#### Scenario: Invalid classifier output is ignored
- **WHEN** the classifier returns output that is not strict JSON naming a known strategy
- **THEN** the deterministic decision is used and the malformed response is logged without the prompt content
