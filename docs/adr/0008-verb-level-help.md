# ADR-0008: Verb-level --help

- Status: accepted
- Date: 2026-08-14
- Related: `docs/adr/0007-kg-cli-contract.md`, `docs/research/cli-help-flag-vs-subcommand.md`

## Context

The help surface was flat: `kg --help` and `kgx --help` printed the entry point's
usage, but `kg <verb> --help` was a usage error (exit 2). GNU §4.8.2 makes
`--help` a hard requirement, and the ecosystem (git, docker, kubectl, cargo, go,
…) shows deep help below commands — `cmd sub --help`. Research
(`docs/research/cli-help-flag-vs-subcommand.md`) surveyed standards,
frameworks, and real CLIs: every one supports `--help`; the `help` subcommand
exists only alongside the flag, never instead of it. A `help` subcommand would
also occupy the first token slot, which ADR-0007 deliberately keeps free of
reserved names — a profile named `help` would be silently unreachable.

## Decision

- Every `kg` verb (`run`, `secret`, `check`, `init`, `completion`) accepts
  `-h`/`--help`, printing a focused usage block to stdout and exiting 0.
- Each `secret` op (`set|get|delete|list|export|import`) accepts `-h`/`--help`
  the same way.
- `kg run --help` is a **leading** flag only: after the combination every
  remaining token belongs to the target program, so
  `kg run aws terraform plan --help` runs terraform — it does not show kg's
  help.
- **No `help` subcommand** (ADR-0007 first-slot overloading).
- `kgx` gains nothing: it has no verbs, and `kgx --help` already works.
- A help command carries only `kind` + the help text; a help request never
  exits non-zero and never writes to stderr.
- `kg secret <bogus> --help` stays an unknown-op error: `--help` is honored
  only under a real op. A deliberate departure from the strict GNU §4.8.2
  "ignore the rest" reading, chosen so a typo surfaces as an error, not as
  help for something that does not exist.

## Considered options

- **Add a `help` subcommand** (`kg help`, `kg help secret`). Rejected: it
  reintroduces the reserved-name collision ADR-0007 removed — a profile named
  `help` would be shadowed in the first slot — and no real CLI uses a help
  subcommand without `--help`, so it adds surface without adding capability.
- **Leave verb-level help as a usage error.** Rejected: `cmd sub --help` is the
  muscle memory the ecosystem teaches; erroring on it is hostile.
- **Honor `--help` under an unknown op.** Rejected: it would show help for an op
  that is not there; the unknown-op error is more useful.

## Consequences

- The help surface is single-sourced where practical: the `secret` op lines are
  one constant shared by `usageText` and `kg secret --help`; the verb help texts
  mirror `usageText` wording (ADR-0006), and the completion `--help` description
  stays in sync.
- README's usage block notes that any verb accepts `--help`.
- Completion is unchanged: `--help` remains a front-position candidate only.
  Offering `-h`/`--help` below verbs is a possible follow-up, not required.
