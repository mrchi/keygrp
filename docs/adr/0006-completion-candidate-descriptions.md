# ADR-0006: Completion candidate descriptions

- Status: accepted
- Date: 2026-08-13
- Related: `docs/adr/0002-shell-completion-and-init.md`,
  `docs/research/fish-completions-research.md`

## Context

`__complete` (ADR-0002) returns candidate lines as bare values. The shell
displays them without context, so `keygrp <TAB>` offers `secret`, `check`,
`init`, `completion` with no hint of what each does, and `keygrp secret <TAB>`
offers `set`/`get`/`delete`/… with none either.

fish's own completion experience shows a one-line description beside each
candidate. Research (`docs/research/fish-completions-research.md`) established
that this does **not** come from parsing `--help` — fish never does that. The
descriptions come from hand-written `complete -c cmd -l opt -d 'desc'` scripts
(fish ships ~1000 of them) or from man-page generation; the display convention
is `candidate<TAB>description`. This ADR records making keygrp's candidates
carry the same.

## Decision

### Protocol

- A candidate line may now be `value<TAB>description`. A candidate without a
  description is emitted as a bare `value` (no tab). The tab is an **optional
  delimiter** — a parser that splits on the first tab reads both old (bare) and
  new (`value<TAB>description`) output.
- This is the format fish's `complete -a` natively treats as
  candidate-description pairs, and the same delimiter zsh `_describe` and
  bash/readline expect, so one encoding serves all three shells.
- Descriptions are authored once, in the static candidate table in
  `internal/completion/completion.go` (the single source of truth).

### Scope

- All three shells (fish/zsh/bash) gain descriptions in one change. The fish
  script passes candidates through unchanged (tab reaches `complete -a`); the
  zsh and bash scripts split the value from its description on the first tab
  (zsh `_describe`/`compadd -d`; bash `value<TAB>description` entries in
  `COMPREPLY`).

### Description content

- **Static candidates** (subcommands, flags, secret operations) reuse the
  wording already present in `main.go`'s `usageText`, so help and completion
  never drift apart. `secret` has no one-line summary in usage, so its
  description is authored once: "manage secrets in the OS keychain".
- **Dynamic values** get generic labels — profile names → `profile`, secret
  refs → `secret ref` — so they read as a distinct class from subcommands in
  the shared candidate list (ADR-0002 reserves subcommand words against
  profiles; the label surfaces the distinction). Shell names
  (`fish`/`zsh`/`bash`) are self-evident and stay bare.

The full description table:

| Candidate | Description | Position |
|---|---|---|
| `secret` | manage secrets in the OS keychain | front |
| `check` | validate config and keychain refs | front |
| `init` | install completion, create config & authorize keychain | front |
| `completion` | print a completion script | front |
| `--help` | show this help | front |
| `--verbose` | prints injected var names with their origin profile | front |
| `<profile>` | `profile` (label) | front / `check --profile` / combination |
| `set` | store a secret in the keychain | `secret` |
| `get` | show whether a secret exists | `secret` |
| `delete` | remove a secret | `secret` |
| `list` | list stored secret refs | `secret` |
| `export` | export secrets to a password-encrypted archive | `secret` |
| `import` | restore secrets from an archive | `secret` |
| `--reveal` | print the secret value | `secret get` first |
| `--stdin` | read the secret from stdin | `secret set` first |
| `--skip-existing` | skip refs that already exist | `secret import` first |
| `--profile` | combination to validate | `check` first |
| `--shell` | shell to install | `init` first |
| `<ref>` | `secret ref` (label) | `secret get/delete` etc. |
| `fish`/`zsh`/`bash` | (bare) | `completion` / `init --shell` |

### Regeneration

- This is a **protocol change**: installed completion scripts must be
  regenerated after upgrading (`keygrp init`, or `keygrp completion <shell>`),
  exactly as adding `__directive:file` required in ADR-0002.
- **No version marker.** While keygrp has no external users, the migration path
  is simply re-running `keygrp init`. A version directive in the protocol would
  not help the only dangerous mismatch (old script + new binary — the old
  script does not parse any marker), and its own vocabulary change would itself
  require regeneration. New scripts must still parse bare-value lines (the
  optional-tab rule above), so the downgrade direction is safe.

## Consequences

### Positive

- `keygrp <TAB>` and `keygrp secret <TAB>` show what each candidate does, at
  parity with fish's own completion experience.
- Descriptions live in one Go table, reusing `usageText` wording — no second
  doc surface to keep in sync.
- New scripts parse both bare values and `value<TAB>description`, so a new
  script against an old binary keeps working.

### Negative / accepted

- Old zsh/bash scripts cannot parse `value<TAB>description` (zsh would insert
  the tab literally; bash depends on readline ≥ 5.0). Scripts must be
  regenerated after upgrade. Accepted while keygrp has no external users.
- zsh and bash scripts gain a tab-split parsing step; the fish script is
  unchanged.
