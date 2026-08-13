# keygrp

`kg` injects a group of environment variables into a target CLI, resolving
the secret-bearing ones from the OS keychain at run time.

The target program is unmodified and unaware. `kg run <profile> <program>`
hands off via `exec(2)`, replacing its own process image with the target — so
the target's PID, TTY, signals, and exit code behave exactly as if it were run
directly, and secrets exist in memory only for the target's lifetime.

```console
$ kg run claude claude
```

`kgx` is the run shorthand — `kgx claude claude` means `kg run claude claude`,
like `uvx` means `uv tool run`. There are two binaries: `kg` carries the whole
surface (management verbs plus `run`), `kgx` is run-only.

## Why

Environment variables are the standard way to configure CLI tools, but putting
secrets in them has two problems: they sit in your shell (and any process you
spawn) whether you need them or not, and they are unversioned by nature.

keygrp flips the default:

- Secrets live in the **OS keychain**, referenced — never stored — in a TOML
  config.
- **On-demand injection**: a secret only reaches a process when a profile that
  references it is run.
- **No shell-init loading**: profiles are never sourced into your shell.

See [`docs/adr/0001-keygrp-design.md`](docs/adr/0001-keygrp-design.md) for the
full decision record and [`.scratch/keygrp/spec.md`](.scratch/keygrp/spec.md)
for the spec. The `kg`/`kgx` CLI split is recorded in
[`docs/adr/0007-kg-cli-contract.md`](docs/adr/0007-kg-cli-contract.md).

## Install

Requires Go 1.26+. From a checkout:

```console
$ go install ./cmd/kg ./cmd/kgx
```

Upgrading from the pre-split `keygrp` binary? Uninstall it after switching:

```console
$ rm "$(command -v keygrp)"
```

## Quick start

1. Create `~/.config/keygrp/config.toml` (or run `kg init` first to write
   a commented starter you fill in):

   ```toml
   [profiles.claude]
   ANTHROPIC_API_KEY = "keychain://anthropic-api-key"

   [profiles.aws]
   AWS_ACCESS_KEY_ID = "keychain://aws-access-key-id"
   AWS_SECRET_ACCESS_KEY = "keychain://aws-secret-access-key"
   AWS_REGION = "ap-southeast-1"
   ```

2. Store the first secret (the first access shows a macOS keychain
   authorization dialog — expected):

   ```console
   $ kg secret set anthropic-api-key
   Enter value for "anthropic-api-key": ********
   Confirm value for "anthropic-api-key": ********
   stored "anthropic-api-key"
   ```

3. Validate everything resolves without running anything:

   ```console
   $ kg check
   all refs resolve
   ```

4. Run a target with the profile's environment:

   ```console
   $ kg run claude claude
   # or, the shorthand:
   $ kgx claude claude
   ```

## Demo

A terminal session, top to bottom:

```console
$ kg init
created config at /Users/chi/.config/keygrp/config.toml
installed fish completion at /Users/chi/.config/fish/completions/kg.fish
keychain: probe ok (nothing stored)

$ kg secret set anthropic-api-key
Enter value for "anthropic-api-key": ****************
Confirm value for "anthropic-api-key": ****************
stored "anthropic-api-key"

$ kg check
all refs resolve

$ kg run claude claude
… claude runs, its TTY and exit code passed through untouched …
```

## Usage

```
kg run [--verbose] <combination> <program> [args...]
    run <program> with <combination>'s env (profiles comma-separated, e.g. aws,gcp);
    --verbose prints injected var names with their origin profile

kg secret set [--stdin] <ref>       store a secret in the keychain
kg secret get [--reveal] <ref>      show whether a secret exists
kg secret delete <ref>              remove a secret
kg secret list                      list stored secret refs
kg secret export [<file>]           export secrets to a password-encrypted archive
kg secret import [--skip-existing] [<file>]  restore secrets from an archive
kg check [--profile <combination>]  validate config and keychain refs
kg init [--shell fish|zsh|bash]     install completion, create config & authorize keychain
kg completion fish|zsh|bash         print a completion script
kg --help                           show this help

kgx [--verbose] <combination> <program> [args...]
    the run shorthand for 'kg run'
```

Exit codes: `0` ok · `1` configuration error · `2` usage or keychain error.
After handoff the target's exit code is returned verbatim.

## Shell completion

Run `kg init` in the shell you use (or `kg init --shell <name>` to target
another). It detects the current shell, writes a comment-only starter config at
the config path if none exists, installs the completion script — which registers
**both** `kg` and `kgx` — and runs a read-only keychain probe so the macOS
authorization dialog appears now rather than at first `secret set`:

```
$ kg init
created config at /Users/chi/.config/keygrp/config.toml
installed fish completion at /Users/chi/.config/fish/completions/kg.fish
keychain: probe ok (nothing stored)
```

zsh needs one manual step kg cannot do for you — the script is written to
`~/.zfunc/_kg`, but zsh only loads completion dirs listed in `fpath`:

```console
$ printf 'fpath+=(~/.zfunc)\nautoload -Uz compinit\ncompinit\n' >> ~/.zshrc
```

What completion covers:

- after `kg` — the verbs (`run`, `secret`, `check`, `init`, `completion`) and
  `--help`;
- after `kg run` / after `kgx` — profile names (a partial combination such as
  `aws,<TAB>` completes the remaining profiles, excluding already-selected ones);
- after `kg secret` — the operation and its refs/flags (`secret export`
  and `secret import` complete the `<file>` position as a file path);
- after `kg run <profile>` / after `kgx <profile>` — command names;
- after `kg run <profile> <command>` — **the command's own completion**,
  delegated to it (the target must have its completion installed).

Completion is generated from the config and refs registry at every `<TAB>`, so
config and secret changes never require regenerating. Regenerate only when
kg's own command structure changes: re-run `kg init` (or `kg
completion <shell> > file` to place it manually).

## Configuration

Path: `~/.config/keygrp/config.toml`, overridden by `$KEYGRP_CONFIG`. A single
file; there is no multi-file merging. `kg init` writes a comment-only
starter here when none exists, and never edits an existing config.

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

Secrets are written to the keychain under service `keygrp`, account = the ref.
kg tracks written refs in a registry file (`<config dir>/refs`) so
`kg secret list` can enumerate them (the keychain backend cannot list items).

### Combining profiles at run time

Name several profiles at once to merge their environments for a single
invocation — no config edit needed:

```console
$ kg run aws,gcp terraform
# or the shorthand:
$ kgx aws,gcp terraform
```

Two selected profiles that declare the same variable name is a configuration
error; a variable both inherit from a shared base counts once. Order does not
matter. Profile names cannot contain commas. `kg check --profile aws,gcp`
validates a combination without running anything.

## Security model

- Secrets are never written to the config, to stdout (unless `kg secret get
  --reveal`), or to shell history — inline `--value` is rejected.
- Injection happens only at `exec`; a secret's memory lifetime equals the
  target process.
- The guarantee is scoped to keygrp-managed secrets: pre-existing secrets you
  have already `export`ed in your shell are inherited unchanged — kg adds
  no new leak surface, but does not scrub the one you already have.
- Profiles are never loaded at shell init.

## Platform support

macOS and Linux. The `exec(2)` handoff is POSIX; secrets use the macOS
Keychain / Linux Secret Service via [go-keyring](https://github.com/zalando/go-keyring).
Windows is out of scope.

## Limitations

- `<program>` is an executable on `PATH`; shell aliases and functions are
  bypassed.
- No post-run hooks or logging (a deliberate consequence of `exec` handoff).
