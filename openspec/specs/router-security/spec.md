# Router Security

## Purpose
Define the router's baseline access and secret-handling safeguards.

## Requirements

### Requirement: Loopback binding by default
The router SHALL bind to loopback by default. Documentation SHALL state that exposing it to a LAN requires authentication and TLS through a trusted reverse proxy or an explicit authentication implementation.

#### Scenario: Default binding is loopback
- **WHEN** the router starts under default configuration
- **THEN** it listens on a loopback address only

### Requirement: API-key authentication for administrative and tool endpoints
The router SHALL provide an API-key authentication option. Normal chat endpoints and administrative endpoints SHALL be distinguishable by authorization policy. Configured API keys SHALL be compared using a constant-time comparison where practical.

#### Scenario: Administrative endpoint requires the administrative policy
- **WHEN** an unauthenticated caller requests an administrative or tool endpoint while API-key authentication is enabled
- **THEN** the request is rejected with an authorization failure and the underlying action does not occur

#### Scenario: Valid key admits the caller
- **WHEN** a caller presents a configured API key to an administrative endpoint
- **THEN** the request is authorised and processed

#### Scenario: Key comparison does not short-circuit on first mismatch
- **WHEN** a presented key shares a prefix with a configured key
- **THEN** the comparison is constant-time with respect to the shared prefix length

### Requirement: Startup configuration validation
All configuration SHALL be validated at startup with useful error messages naming the offending section and setting. OpenRouter configuration SHALL NOT be required when cloud fallback is disabled.

#### Scenario: Invalid configuration fails fast
- **WHEN** a required setting such as the local executable path is missing or malformed
- **THEN** startup fails with an error message naming the section and setting

#### Scenario: Cloud settings optional when cloud is disabled
- **WHEN** cloud access is disabled and no OpenRouter API key is configured
- **THEN** startup succeeds, because OpenRouter configuration is not required in that mode

#### Scenario: Enabled cloud requires a key
- **WHEN** `openRouter.enabled` is true and no API key is available from configuration or the environment
- **THEN** startup fails with a message explaining how to supply the key without committing it

### Requirement: Secrets are never committed
The repository SHALL ship `config.example.json` without secrets, and `.gitignore` SHALL exclude local configuration files that may carry keys.

#### Scenario: Example config carries no key
- **WHEN** the example configuration is inspected
- **THEN** the OpenRouter API key field is empty and documentation directs the reader to environment variables

### Requirement: Example model identifiers are unusable placeholders
`config.example.json` SHALL NOT contain real cloud model identifiers. Each cloud strategy mapping SHALL carry an obvious placeholder that fails startup validation with a message directing the reader to the provider's model catalogue. Model identifiers age out, and a stale-but-plausible default would route silently to the wrong model.

#### Scenario: Unmodified example fails to start
- **WHEN** the example configuration is used verbatim with cloud enabled
- **THEN** startup fails with a message naming the placeholder strategy and pointing at the OpenRouter model catalogue

#### Scenario: Documentation dates its suggestions
- **WHEN** the README suggests concrete model identifiers
- **THEN** they appear in an explicitly dated "known-good as of" table rather than as configuration defaults
