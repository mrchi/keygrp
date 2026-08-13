# ADR-0007: kg CLI — run verb and kgx shorthand

- Status: accepted
- Date: 2026-08-13
- Related: `docs/adr/0001-keygrp-design.md` §2, `docs/adr/0002-shell-completion-and-init.md`,
  `docs/adr/0004-profile-combination.md`, `docs/adr/0008-verb-level-help.md`,
  `CONTEXT.md`

## Context

The first positional token of `keygrp` was overloaded: it was either a management
verb (`secret`, `check`, `init`, `completion`) or a profile combination
(`keygrp aws terraform`). A profile named after a subcommand (e.g. `secret`) was
silently unreachable as a single-token invocation — `config.Parse` accepted the
name but the parser's switch shadowed it — and completion doubled the collision
(duplicate candidates, hijacked dispatch). The rename also buys ergonomics: the
hot path drops from seven characters to three.

## Decision

- The binary `keygrp` is renamed **`kg`**. The module path
  (`github.com/mrchi/keygrp`) and all data paths stay `keygrp`: the keychain
  service name, the config dir (`~/.config/keygrp/`), the refs registry, and the
  default archive name `keygrp-secrets.kgx`.
- `kg` gains a **`run` verb**: `kg run [--verbose] <combination> <program> [args...]`.
  The bare run form is removed; an unknown first token is a usage error that
  statically hints `kg run <token> <program>` / `kgx <token> <program>`, without
  loading the config.
- **`kgx`** is a second entry point, the run shorthand:
  `kgx [--verbose] <combination> <program> [args...]`, semantically identical to
  `kg run`. It has no management subcommands; `kgx` bare is a usage error and
  `kgx --help` prints a one-line run usage.
- `--verbose` becomes run-scoped: `kg run --verbose` / `kgx --verbose`; it is
  rejected under the other verbs.
- Management verbs are unchanged: `secret {set|get|delete|list|export|import}`,
  `check [--profile <combination>]`, `init [--shell <shell>]`,
  `completion <shell>`; `kg` bare prints help.
- Completion: `kg completion <shell>` and `kg init` emit/install one script that
  registers both `kg` and `kgx`; the `__complete` brain dispatches on the leading
  command word.
- Entry points are two packages `cmd/kg` and `cmd/kgx` sharing one `internal/cli`;
  the run path is single-sourced, so `kg run` and `kgx` cannot diverge.
- Exit codes, no-shadowing combination semantics, and the exec model are
  unchanged (ADR-0001 §6, ADR-0004).

## Considered options

- **Reserve the command words in `config.Parse`** (reject profiles named
  `secret`/`check`/…). Rejected: patched the symptom while the overloaded first
  token remained; a profile named `run` or any future verb would still be a
  footgun.
- **Two independent binaries `kg` (runner) + `keygrp` (management)**. Rejected in
  favor of `kg run` under a single `kg`: one install carries the whole surface,
  and `kgx` preserves the short hot path.
- **Keep bare `kg <combination>` for backward compatibility.** Rejected:
  reintroduces the overloaded slot and two run paths.
- **Rename the data layer to `kg`** (config dir, keychain service). Rejected: the
  keychain service is the secrets' addressing namespace — go-keyring has no
  enumeration or rename API, so changing it orphans every stored secret with no
  scriptable migration.

## Consequences

### Positive

- The first token is unambiguous in both commands: in `kg` it is always a verb and
  the combination lives in the slot after `run`; in `kgx` it is always a
  combination. No reserved-name bookkeeping anywhere.
- A profile named after any verb is fully usable (`kg run run foo`,
  `kgx secret foo`).
- The hot path shortens to `kgx aws terraform`.

### Negative / accepted

- Existing `keygrp` invocations and any scripts must migrate to `kg`/`kgx`, and
  the old binary must be uninstalled.
- Two PATH entries to install, complete, help, and version-stamp.
- `kg`'s data paths still read `keygrp` (deliberate cosmetic mismatch, chosen to
  avoid a data migration).
