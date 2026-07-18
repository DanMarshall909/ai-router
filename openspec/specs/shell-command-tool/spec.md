# Shell Command Tool

## Purpose
Define the constrained proof-of-concept shell execution tool.

## Requirements

### Requirement: Shell tool contract
The router SHALL provide a single proof-of-concept tool named `execute_shell_command` accepting `shell`, `command`, `workingDirectory`, `timeoutSeconds`, and `dryRun`. It SHALL use PowerShell on Windows and Bash on Linux, and SHALL NOT use `cmd.exe /c` as the default Windows executor. Untrusted values SHALL NOT be concatenated into process arguments unnecessarily.

#### Scenario: Dry run starts no process
- **WHEN** the tool is invoked with `dryRun` set to true
- **THEN** no process is started and the response describes what would have run

### Requirement: Shell execution is disabled by default
Shell execution SHALL be disabled unless explicitly enabled in configuration (`shellTool.enabled`).

#### Scenario: Rejected under default configuration
- **WHEN** a shell-command request is made under default configuration
- **THEN** the request is rejected because shell execution is disabled, and no process is started

### Requirement: Allow-listed working directories and path protection
Execution SHALL be confined to configured allow-listed working directories. Attempts to escape an allowed directory, including via path traversal, SHALL be rejected before any process starts.

#### Scenario: Directory outside the allow list
- **WHEN** execution is requested with a working directory outside `shellTool.allowedWorkingDirectories`
- **THEN** execution is rejected and no process is started

#### Scenario: Path traversal rejected
- **WHEN** the requested working directory uses traversal segments to resolve outside an allowed directory
- **THEN** execution is rejected after path resolution, because the check is applied to the resolved path

### Requirement: Unattended command allow list with explicit confirmation state
Commands on the configured unattended allow list SHALL run without confirmation. Commands that are not allow-listed and not explicitly blocked SHALL return a `confirmation_required` pending-action object containing a generated `actionId` and a human-readable `summary`, and SHALL start no process. Confirmation SHALL be represented as an explicit API state and SHALL NOT be simulated.

#### Scenario: Allow-listed command runs
- **WHEN** shell execution is enabled, `go version` is allow-listed, and the tool is invoked inside an allowed directory
- **THEN** the command runs and stdout, stderr, and exit code are returned separately

#### Scenario: Non-allow-listed command requires confirmation
- **WHEN** a command that is neither allow-listed nor blocked is requested
- **THEN** no process starts and a `confirmation_required` response with an `actionId` and `summary` is returned

### Requirement: Pending actions are bound, expiring, and non-durable
A pending action SHALL store the fully-resolved command and working directory server-side. Confirmation SHALL reference only the `actionId`; any command or directory supplied at confirmation time SHALL be ignored or rejected, never executed. Pending actions SHALL expire after a configurable TTL and SHALL be held in memory only, so a router restart discards them. An unknown, expired, or already-consumed `actionId` SHALL be rejected without executing anything.

#### Scenario: Confirmation cannot swap the command
- **WHEN** a caller confirms a valid `actionId` while supplying a different command than the one that produced it
- **THEN** the supplied command is not executed, because the pending action's stored command is authoritative

#### Scenario: Pending action expires
- **WHEN** a pending action is confirmed after its TTL has elapsed
- **THEN** the confirmation is rejected and no process starts

#### Scenario: Restart discards pending actions
- **WHEN** the router restarts and a caller confirms an `actionId` minted before the restart
- **THEN** the confirmation is rejected, because pending actions are deliberately non-durable so a confirmation cannot execute under configuration it was not minted against

#### Scenario: Action IDs are single-use
- **WHEN** a valid `actionId` is confirmed twice
- **THEN** the second confirmation is rejected and the command runs only once

### Requirement: Destructive pattern blocking
The proof of concept SHALL block known destructive patterns, including disk formatting, shutdown, credential dumping, and recursive root deletion. Documentation SHALL state clearly that pattern blocking is not a complete security boundary.

#### Scenario: Destructive command blocked outright
- **WHEN** a command matching a known destructive pattern is requested
- **THEN** it is rejected outright and is not offered as a confirmable pending action

### Requirement: Bounded execution and output capture
Execution SHALL enforce a configurable timeout, cap captured output at `shellTool.maximumOutputCharacters`, and return stdout, stderr, and exit code separately. Command metadata SHALL be logged without leaking credentials or command output that may contain secrets.

#### Scenario: Timeout enforced
- **WHEN** a permitted command exceeds its timeout
- **THEN** the process is terminated and a timeout outcome is returned

#### Scenario: Output capped
- **WHEN** a permitted command emits more output than the configured maximum
- **THEN** captured output is truncated to the configured cap and the result records that truncation occurred

#### Scenario: Logs carry metadata only
- **WHEN** a shell command runs and its output contains a secret value
- **THEN** the logs record command metadata and outcome but not the captured output
