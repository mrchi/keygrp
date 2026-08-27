# ADR-0012: kg profile subcommand — CRUD over a single TOML file

- Status: accepted
- Date: 2026-08-27
- Related: `docs/adr/0001-keygrp-design.md`, `docs/adr/0003-profile-extends.md`,
  `docs/adr/0004-profile-combination.md`, `docs/adr/0007-kg-cli-contract.md`,
  `docs/adr/0013-llm-callability-standard.md`, `CONTEXT.md`

## Context

Profiles live only in a hand-edited TOML file; adding, changing, or deleting a
profile means editing `~/.config/keygrp/config.toml` by hand. ADR-0001
deliberately declared "keygrp never writes TOML; the file is hand-edited and
git-trackable". Hand-editing is tedious, and with LLM-driven CLI use it is a
surface an agent cannot drive. This ADR adds a `kg profile` verb for full CRUD
and reverses the never-write principle for the `[profiles.*]` section.

## Decision

New verb `kg profile {list|show|add|set|unset|delete|rename}` (noun + verb,
mirroring `kg secret {set|get|delete|list|export|import}`):

- `list` — print profile names; `--json` for structured output.
- `show <name>` — print the effective variable set with per-variable origin
  profile; `--raw` for the raw declaration; `--json` for structured output.
- `add <name> [KEY=value...] [-e]` — create a profile; `-e` opens `$EDITOR` on
  the new profile's raw TOML block.
- `set <name> KEY=value...` — set or update variables; `extends=<name>` manages
  base profiles.
- `unset <name> KEY...` — remove variables.
- `delete <name>` — remove a profile after a y/N confirmation;
  `--force` skips the prompt (for agents).
- `rename <old> <new>` — rename and rewrite every `extends` reference to
  `<old>` in the same file, reporting how many references were updated.

**Write-back model** — reverses ADR-0001's never-write-TOML stance for the
profiles section:

- The file is edited surgically: only the affected `[profiles.<name>]` block is
  located in the raw text and replaced with a deterministic rendering; every
  other byte (comments, other profiles) is preserved.
- Writes go through a temp file + atomic rename. The git-trackable,
  hand-edited character of everything outside the edited block is retained.
- Comments inside the edited block are not preserved (the block is
  regenerated) — the accepted cost of tool-assisted editing.

**Validation:**

- Every mutation validates against the existing rules before writing (fail
  fast): no-shadowing / conflict across the reachable set, `extends` cycles,
  unknown bases. A violation rejects the write with exit 1.
- Out-of-bounds inputs error deterministically: `set` on a missing profile,
  `add` on an existing name, `unset`/`delete` of a missing key/profile,
  `rename` to an existing name — all exit 1. `set` never auto-creates.
- Secrets: `keychain://<ref>` values are written and removed as plain strings;
  kg does not resolve or verify refs (that is `kg secret`'s job). `kg secret
  delete` keeps its interactive y/N confirmation.
**Naming contract** — also enforced at parse time (a breaking change):

- Profile names and ref names are both restricted to `[A-Za-z0-9_-]`, enforced
  in `config.Parse` (profiles) and for `keychain://` refs, and in `kg profile
  add`/`rename` and `kg secret set`. A leading `-` is additionally rejected at
  the argv layer (`add`/`rename`/`set`), since it parses as a flag.
- Existing configs with names or refs outside the charset (dots, spaces,
  nested paths) become invalid at parse and must be renamed.

## Considered options

- **Whole-file rewrite via go-toml/v2 Marshal** — rejected: destroys every
  comment and reformats the file, hostile to the hand-edited, git-trackable
  contract.
- **Separate managed file (`profiles.toml`)** — rejected: splits the data path
  and requires migrating existing hand-written configs; surgical edit achieves
  preservation without a migration.
- **Interactive editor as the only surface** — rejected: not driveable by an
  LLM; kept only as the optional human convenience (`-e`), never required.
- **`set` auto-creates profiles** — rejected: implicit creation hides typos;
  explicit `add` keeps behavior predictable.
- **Loose naming (anything but commas)** — rejected for profile names: comma is
  reserved (ADR-0004), and shell/argv quoting of spaces and leading dashes
  makes names unpredictable for both humans and agents; a strict charset at
  creation is a small tax for a stable contract. Applied to refs too for
  symmetry, at the cost of invalidating `nested/path`-style refs.

## Consequences

- kg now writes TOML; ADR-0001's blanket "never writes TOML" is superseded for
  the `[profiles.*]` section (other data paths, e.g. the refs registry, are
  unchanged).
- Breaking change: configs with out-of-charset names or refs no longer parse.
- `kg run`'s conflict path becomes defense-in-depth; the authoring surface
  rejects bad profiles first.
- Completion scripts and the `parseKG` switch must learn the new verb. The
  first token stays a verb (ADR-0007), so a profile named `profile` remains
  reachable via `kg run profile ...`.
