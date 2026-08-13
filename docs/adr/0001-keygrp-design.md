# ADR-0001: keygrp — On-demand secret injection via process exec

- Status: accepted
- Date: 2026-08-12
- Related: `.scratch/keygrp/spec.md`

## Context

keygrp is a CLI that injects groups of environment variables into a target CLI (e.g. `claude`, `codex`, `terraform`), resolving the secret-bearing variables from the operating system keychain at run time.

Env groups are defined in a TOML file as `[profiles.<name>]` tables. Secret values are referenced as `keychain://<ref>` URIs rather than stored inline; non-secret values may be stored as plaintext.

The design is governed by five principles: least privilege, on-demand injection, compatibility (no existing CLI is modified), transparency (the target program is unaware of keygrp), and secure by default (secrets are never loaded during shell init).

## Decision

### 1. Execution model: exec replacement

`keygrp <profile> <program> [args...]` resolves the profile, then hands control to `<program>` with `syscall.Exec` (POSIX process-image replacement). No spawn-and-wait parent, no post-run hooks.

Rationale: the target's PID, TTY, signal handlers, and exit code pass through naturally; secret strings in keygrp's heap are recycled the instant the image is replaced; the secret's lifetime equals the target process lifetime.

### 2. CLI contract

- `<profile>` must match a `[profiles.<profile>]` table.
- `<program>` is resolved on `PATH`; remaining arguments are passed through unchanged.
- No single-argument shorthand (`keygrp <profile>` runs nothing).
- Documented limitation: `<program>` is an executable on `PATH`; shell aliases and functions are bypassed.

### 3. Configuration file

- Single file: `~/.config/keygrp/config.toml`.
- Override: `$KEYGRP_CONFIG`.
- No multi-file merging.
- On `keygrp init`, a comment-only starter config is written (0600, parent
  dirs 0700) when no file exists; an existing file is never modified
  (ADR-0002).
- Written 0600; keygrp warns on over-permissive permissions.
- Co-located refs registry: `<config dir>/refs` records every ref keygrp writes. Because go-keyring cannot enumerate keychain items (it has no `List` API), `secret list` reads this registry instead of the keychain. The registry can drift if a user deletes a keychain item by hand; `secret list` flags refs that are missing from the keychain.

### 4. Value semantics

- Allowed value forms: `keychain://<ref>` (resolved from keychain at run time) and plaintext non-secret strings.
- Explicitly absent: environment passthrough syntax (`$VAR`) — inherited environment already reaches the target, so an explicit passthrough feature would add ambiguity for no benefit.
- Profile values unconditionally override inherited environment. Determinism over politeness.
- A missing keychain item is a fail-fast error; the target is not started.
- `--verbose` prints injected variable names, never values.

### 5. Keychain

- Backend: `zalando/go-keyring` (macOS Keychain / Linux Secret Service).
- Item identity: service `keygrp`, account = the `keychain://` ref string.
- The ref namespace is global across profiles, so multiple profiles share one secret (e.g. `aws` and `terraform` both reference `keychain://aws-access-key-id`).
- The macOS first-run authorization dialog is documented user-facing behavior, not a bug.

### 6. Command surface

- `keygrp secret set <ref>` — hidden input, verify-by-retype, optional `--stdin`.
- `keygrp secret get <ref>` — reports existence by default; `--reveal` opts into printing the value.
- `keygrp secret delete <ref>` — with confirmation.
- `keygrp secret list` — lists stored refs from the refs registry (see §3), never values; refs missing from the keychain are flagged.
- `keygrp check [--profile <name>]` — validates TOML and keychain resolvability; runs nothing.
- `--value` inline secrets are rejected (shell history / process-list leak).
- keygrp never writes TOML; the file is hand-edited and git-trackable. The one
  exception is `keygrp init`, which writes a comment-only starter when no config
  exists and never modifies an existing file (ADR-0002).
- Exit codes: `1` configuration error, `2` keychain error, before handoff; after handoff the target's exit code is returned verbatim (a consequence of exec).

### 7. Environment policy

- Full inheritance of the parent environment plus profile additions/overrides; nothing is ever deleted.
- No `--clean`/surgical mode in v1.
- The security guarantee is scoped honestly: keygrp guarantees its keychain-managed secrets are injected on demand only; it does not scrub secrets the user has already exported in the shell.

### 8. Engineering

- Module: `github.com/mrchi/keygrp`. Layout: `main.go`; `internal/config`; `internal/keychain`; `internal/runner`.
- Dependencies: `zalando/go-keyring`, `pelletier/go-toml/v2`.
- Tests: table-driven config parsing; keychain behind an interface with a fake; env resolution separable from the (untestable) exec handoff.
- Verification gate: `go vet ./...` and `go test ./...` green.

### 9. Scope and strictness

- Platforms: macOS and Linux. Windows is out of scope for v1 (no build tags).
- TOML strictness: non-string values error; unknown keys within a profile are accepted; unknown top-level tables are ignored (forward compatibility).

## Consequences

### Positive

- Secrets exist in memory only for the target's lifetime.
- Injection is deterministic regardless of shell state.
- The target program is unmodified and unaware; TUI, signals, and exit codes behave as if it were run directly.

### Negative / accepted

- No post-run cleanup or logging hooks.
- No Windows support.
- Shell aliases/functions are not honored.
- The security guarantee excludes pre-existing shell pollution.
