# keygrp

[![Go Version](https://img.shields.io/badge/go-1.26-blue)](go.mod)
[![License: MIT](https://img.shields.io/badge/license-MIT-green)](#license)

Run a CLI with a group of environment variables — resolving the secret-bearing
ones from your **OS keychain** at the moment the target starts. The config holds
*references*, never values; nothing is sourced into your shell; and the target
is handed off with `exec(2)`, so its PID, TTY, signals, and exit code behave
exactly as if it were launched directly.

```console
$ kgx deepseek claude
```

`kgx` is how you run things day to day; `kg run deepseek claude` is the long
form of the same command, just as `uv tool run` is the long form of `uvx`. Two
binaries: `kg` carries the whole surface (management verbs plus `run`), `kgx`
is run-only. One invocation can also merge several profiles —
`kgx aws,gcp terraform` pulls in `aws` and `gcp` at once.

## Contents

- [Why keygrp](#why-keygrp)
- [Features](#features)
- [Install](#install)
- [Quick start](#quick-start)
- [Usage](#usage)
- [Configuration](#configuration)
- [Secrets](#secrets)
- [Shell completion](#shell-completion)
- [Security model](#security-model)
- [Platform support](#platform-support)
- [Limitations](#limitations)
- [Documentation](#documentation)
- [Development](#development)
- [License](#license)

## Why keygrp

Environment variables are the standard way to configure CLIs, but putting
secrets in them has two problems: they sit in your shell — and every process you
spawn — whether you need them or not, and they are unversioned by nature.

keygrp flips the default:

- **Secrets live in the OS keychain**, referenced — never stored — in a TOML
  config.
- **On-demand injection**: a secret only reaches a process when a profile that
  references it is run.
- **No shell-init loading**: profiles are never sourced into your shell.

## Features

- **Keychain-backed secrets** — store once with `kg secret set`, reference from
  any number of profiles. Values never touch the config or shell history; the
  only stdout exception is the explicit `kg secret get --reveal` opt-in.
- **Profile composition** — inherit variables across profiles with `extends`,
  and merge several profiles at run time with comma-separated combinations.
- **True `exec` handoff** — the target replaces kg's process image, so there is
  no wrapper process, no PID indirection, and no post-run hooks to get in the
  way.
- **Shell completion** for fish, zsh, and bash — including delegating to the
  *target command's own* completion after the profile position.
- **Encrypted backup & restore** — `kg secret export` / `kg secret import`
  move your secrets between machines in a password-encrypted archive.
- **`kgx` to run, `kg` to manage** — the day-to-day invocation is
  `kgx <profile> <program>`; the `kg run` long form and the management verbs
  (`secret`, `check`, `init`, `completion`) live on `kg`.

## Install

Requires Go 1.26+. Install both binaries straight from GitHub — no checkout
needed; the `cmd/...` pattern covers every command under `cmd/`:

```console
$ go install github.com/mrchi/keygrp/cmd/...@latest
```

Prefer to build from a checkout? `go install ./cmd/...` also works.

Upgrading from the pre-split `keygrp` binary? Uninstall it after switching:

```console
$ rm "$(command -v keygrp)"
```

## Quick start

1. **Initialize** — `kg init` writes a commented starter config at
   `~/.config/keygrp/config.toml` if none exists, installs shell completion for
   both `kg` and `kgx`, and runs a keychain probe so the macOS authorization
   dialog appears now rather than mid-setup:

   ```console
   $ kg init
   ```

2. **Add a profile** — edit `~/.config/keygrp/config.toml` and declare the
   environments you want to run with:

   ```toml
   [profiles.deepseek]
   DEEPSEEK_API_KEY = "keychain://deepseek-api-key"

   [profiles.aws]
   AWS_ACCESS_KEY_ID = "keychain://aws-access-key-id"
   AWS_SECRET_ACCESS_KEY = "keychain://aws-secret-access-key"
   AWS_REGION = "ap-southeast-1"

   [profiles.gcp]
   GOOGLE_APPLICATION_CREDENTIALS = "keychain://gcp-credentials"
   ```

3. **Store the secrets** — one `kg secret set` per keychain ref in the config,
   each with the same prompt-and-confirm flow:

   ```console
   $ kg secret set deepseek-api-key
   Enter value for "deepseek-api-key": ********
   Confirm value for "deepseek-api-key": ********
   stored "deepseek-api-key"
   ```

   Repeat for `aws-access-key-id`, `aws-secret-access-key`, and
   `gcp-credentials`.

4. **Validate** everything resolves without running anything:

   ```console
   $ kg check
   all refs resolve
   ```

5. **Run** a target with the profile's environment:

   ```console
   $ kgx deepseek claude
   ```

6. **Combine profiles** — merge several profiles' env in one invocation, no
   config edit needed:

   ```console
   $ kgx aws,gcp terraform
   ```

   The members merge under the same no-shadowing rule as `extends` — see
   [Combining profiles at run time](#combining-profiles-at-run-time) for the
   rules.

## Usage

| Command | Description |
|---|---|
| `kgx [--verbose] <combination> <program> [args...]` | run `<program>` with `<combination>`'s env — the recommended, day-to-day invocation (profiles comma-separated, e.g. `aws,gcp`); `--verbose` prints each injected variable with its origin profile |
| `kg run [--verbose] <combination> <program> [args...]` | the long form of `kgx` |
| `kg secret set [--stdin] <ref>` | store a secret in the keychain (hidden prompt, verify by re-entry; `--stdin` for piped input) |
| `kg secret get [--reveal] <ref>` | show whether a secret exists; `--reveal` prints its value |
| `kg secret delete <ref>` | remove a secret, with confirmation |
| `kg secret list` | list stored secret refs, flagging any missing from the keychain |
| `kg secret export [<file>]` | write all secrets to a password-encrypted archive (`-` for stdout) |
| `kg secret import [--skip-existing] [<file>]` | restore secrets from an archive (`-` for stdin) |
| `kg check [--profile <combination>]` | validate config and keychain refs without running anything |
| `kg init [--shell fish\|zsh\|bash]` | install completion, create a starter config, authorize the keychain |
| `kg completion fish\|zsh\|bash` | print a completion script |

Exit codes: `0` ok · `1` configuration error · `2` usage or keychain error.
After handoff the target's exit code is returned verbatim. Every verb accepts
`--help`.

## Configuration

Path: `~/.config/keygrp/config.toml`, overridden by `$KEYGRP_CONFIG`. A single
file — there is no multi-file merging. `kg init` writes a comment-only starter
here when none exists, and never edits an existing config.

### Value forms

| Form | Example | Behavior |
|---|---|---|
| keychain ref | `KEY = "keychain://ref"` | resolved from keychain at run time |
| plaintext | `REGION = "ap-southeast-1"` | injected directly (non-secrets) |

- Ref names are **global** across profiles: `[profiles.aws]` and
  `[profiles.terraform]` can both reference `keychain://aws-access-key-id`.
- Profile values **override** any value already in your environment,
  unconditionally.
- A missing keychain item is a **fail-fast** error: the target is not started.
- Non-string values are a parse error; unknown keys within a profile are
  accepted; unknown top-level tables are ignored.

### Profile inheritance (`extends`)

A profile may inherit another profile's variables with the reserved `extends`
key — a profile name, or an array of names:

```toml
[profiles.aws]
AWS_ACCESS_KEY_ID = "keychain://aws-access-key-id"

[profiles.terraform]
extends = "aws"                                # inherit aws's variables
TF_TOKEN = "keychain://terraform-token"
```

A profile's **effective variable set** is the union of its own variables and
every reachable base's — the transitive closure of `extends`, deduplicated by
profile (a diamond collapses to one copy). Rules:

- **No shadowing** — two distinct declarations of the same variable name within
  one profile's reachable set is a configuration error, judged by name
  regardless of value. A conflict invalidates only the profile that reaches it;
  `kg check` reports all conflicts, `kg run` fails fast.
- **All-or-nothing** — a derived profile takes every variable of its bases;
  there is no exclusion syntax.
- **Fail fast at load** — a missing base or an `extends` cycle is a
  configuration error, detected before any program lookup or keychain access,
  so a broken chain never triggers a keychain prompt.
- `extends` is consumed by kg, never injected.

### Combining profiles at run time

Name several profiles at once to merge their environments for a single
invocation — no config edit needed:

```console
$ kgx aws,gcp terraform
# kg run aws,gcp terraform is the long form
```

Each member resolves its own `extends` chain, then the members merge under the
same no-shadowing rule as `extends` — a shared base collapses to one copy; two
declarations from distinct origins is a configuration error. Order does not
matter (`aws,dev` ≡ `dev,aws`); profile names cannot contain commas; a leading,
trailing, or double comma is a usage error. `kg check --profile aws,gcp`
validates a combination without running anything.

## Secrets

Secrets are stored in the OS keychain under service `keygrp`, with the ref as
the account. Because the keychain backend cannot list items, kg tracks written
refs in a registry file (`<config dir>/refs`, written `0600`) that backs
`kg secret list` and the export/import commands.

### Backup & restore

`kg secret export` captures every ref's value into a single
password-encrypted archive (`keygrp-secrets.kgx` by default; `-` writes to
stdout), and `kg secret import` restores it (`-` reads from stdin) — so moving
secrets to a new machine is one ssh pipe away:

```console
$ kg secret export - | ssh host kg secret import -
```

The archive is a versioned envelope: PBKDF2-HMAC-SHA256 key derivation
(600,000 iterations), AES-256-GCM encryption, and the header bound as
authenticated data so a tampered or wrong-password archive fails decryption
rather than silently downgrading. See
[`docs/adr/0005-secret-export-import.md`](docs/adr/0005-secret-export-import.md)
for the full format.

## Shell completion

Run `kg init` in the shell you use (or `kg init --shell <name>` to target
another). It detects the current shell, writes a comment-only starter config at
the config path if none exists, installs the completion script — which registers
**both** `kg` and `kgx` — and runs a read-only keychain probe so the macOS
authorization dialog appears now rather than at first `secret set`.

zsh needs one manual step kg cannot do for you — the script is written to
`~/.zfunc/_kg`, but zsh only loads completion dirs listed in `fpath`:

```console
$ printf 'fpath+=(~/.zfunc)\nautoload -Uz compinit\ncompinit\n' >> ~/.zshrc
```

What completion covers:

- after `kg` — the verbs (`run`, `secret`, `check`, `init`, `completion`) and
  `--help`;
- after `kg run` / after `kgx` — profile names (a partial combination such as
  `aws,<TAB>` completes the remaining profiles, excluding already-selected
  ones);
- after `kg secret` — the operation and its refs/flags (`secret export` and
  `secret import` complete the `<file>` position as a file path);
- after `kg run <profile>` / after `kgx <profile>` — command names;
- after `kg run <profile> <command>` — **the command's own completion**,
  delegated to it (the target must have its completion installed).

Completion is generated from the config and refs registry at every `<TAB>`, so
config and secret changes never require regenerating. Regenerate only when
kg's own command structure changes: re-run `kg init` — it installs completion
for both `kg` and `kgx`, writing a `kgx` companion autoload file alongside the
primary for fish and bash (those shells load completion files by command
name). Placing the script by hand with `kg completion <shell> > file` installs
`kg` only and skips the `kgx` companion.

## Security model

- Secrets are never written to the config, to stdout (unless `kg secret get
  --reveal`), or to shell history — inline `--value` is rejected.
- The refs registry holds only ref names, never values. It can drift if a
  keychain item is deleted by hand; `kg secret list` flags the discrepancy.
- Injection happens only at `exec`; a secret's memory lifetime equals the
  target process.
- The guarantee is scoped to keygrp-managed secrets: pre-existing secrets you
  have already `export`ed in your shell are inherited unchanged — kg adds
  no new leak surface, but does not scrub the one you already have.
- Profiles are never loaded at shell init.

## Platform support

macOS and Linux. The `exec(2)` handoff is POSIX; secrets use the macOS
Keychain / Linux Secret Service via
[go-keyring](https://github.com/zalando/go-keyring). Windows is out of scope.

## Limitations

- `<program>` is an executable on `PATH`; shell aliases and functions are
  bypassed.
- No post-run hooks or logging (a deliberate consequence of `exec` handoff).

## Documentation

The domain model and glossary live in
[`CONTEXT.md`](CONTEXT.md); design decisions are recorded as ADRs in
[`docs/adr/`](docs/adr/):

| ADR | Topic |
|---|---|
| [0001](docs/adr/0001-keygrp-design.md) | overall design & environment override rules |
| [0002](docs/adr/0002-shell-completion-and-init.md) | shell completion & `init` |
| [0003](docs/adr/0003-profile-extends.md) | profile `extends` |
| [0004](docs/adr/0004-profile-combination.md) | run-time profile combination |
| [0005](docs/adr/0005-secret-export-import.md) | encrypted export / import |
| [0006](docs/adr/0006-completion-candidate-descriptions.md) | completion candidate descriptions |
| [0007](docs/adr/0007-kg-cli-contract.md) | the `kg` / `kgx` CLI contract |
| [0008](docs/adr/0008-verb-level-help.md) | per-verb `--help` |

## Development

Requires Go 1.26+. Build and test from a checkout:

```console
$ go build ./cmd/...
$ go test ./...
```

### Releasing

Releases are manual and tag-driven. When main is green, cut one with
`git tag vX.Y.Z && git push origin vX.Y.Z`. The release workflow builds `kg` and
`kgx` for darwin/amd64, darwin/arm64, and linux/amd64, and attaches tarballs
plus `sha256sums.txt` to a GitHub Release for the tag (see `docs/adr/0009`).

The issue tracker and spec live under
[`.scratch/keygrp/`](.scratch/keygrp/). Contributions are welcome — open an
issue or a pull request. Keep changes scoped, update the relevant ADR when
behavior changes, and make sure the test suite passes. Commits follow the
Conventional Commits spec.

## License

MIT © 2026 [mrchi](https://github.com/mrchi). See [LICENSE](LICENSE).
