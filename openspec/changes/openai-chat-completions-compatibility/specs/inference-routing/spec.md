## MODIFIED Requirements

### Requirement: Deterministic routing policy
Routing SHALL be deterministic for a given request and system state. The policy SHALL consider explicit client override, the request's required Chat Completions capabilities, local model availability and startup state, request length, approximate input token count, whether tools are requested, whether the request appears to involve coding, whether it requests extensive reasoning/research/comparison, local circuit-breaker state, whether OpenRouter is configured, and configured cost and privacy preferences. Signals SHALL be carried in explicit request-context and routing-signal types rather than loose dictionaries. The policy SHALL NOT select a provider that cannot satisfy all required request capabilities.

#### Scenario: Same inputs give the same decision
- **WHEN** the identical request is routed twice against identical system state
- **THEN** the policy returns the identical `RoutingDecision`, because the policy is deterministic

#### Scenario: Auto prefers local
- **WHEN** `model` is `auto`, the request is ordinary conversation, local inference is permitted, and the local provider satisfies the request capabilities
- **THEN** the policy selects `QuickLocal`

#### Scenario: Heavy task escalates to cloud
- **WHEN** a request exceeds the configured complexity threshold or clearly requests extensive reasoning
- **THEN** the policy selects the configured cloud reasoning or cloud coding strategy, subject to privacy policy

#### Scenario: Oversized input escalates to cloud
- **WHEN** the approximate input token count exceeds `routing.maxLocalInputTokens`
- **THEN** the policy does not select a local strategy

#### Scenario: Local capability is insufficient
- **WHEN** an automatic request requires a supported capability absent from the local provider and a compatible cloud strategy is permitted
- **THEN** the policy selects that cloud strategy before local inference starts
