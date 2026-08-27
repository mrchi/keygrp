# ADR-0013: LLM-callability standard for kg subcommands

- Status: accepted
- Date: 2026-08-27
- Related: `docs/adr/0008-verb-level-help.md`,
  `docs/adr/0012-kg-profile-subcommand.md`

## Context

kg is increasingly driven by LLM agents rather than interactive humans.
ADR-0008's terse verb help (a single usage line) is enough for a human who
already knows the tool, but not for an agent that must discover invocation from
`--help`. Interactive prompts (e.g. `kg secret delete`'s y/N confirmation)
cannot be answered by an agent mid-flight. This ADR sets the standard the
`kg profile` verb was designed against, so later verbs follow it.

## Decision

Every kg subcommand is driveable non-interactively and discoverable from its own
help:

- **Complete per-op help**: each operation's `--help` block includes the usage
  line, every flag, positional argument semantics, 2–3 examples, exit codes
  (0 ok / 1 config / 2 usage-or-keychain), and relevant environment variables
  (e.g. `$KEYGRP_CONFIG`).
- **Structured output**: listing/reading operations (`kg profile list`,
  `kg profile show`) accept `--json`; the default output remains
  human-readable text.
- **Non-interactive mutations**: no operation requires a human in the loop.
  Destructive deletes may prompt y/N — `kg secret delete` and `kg profile
  delete` both do — but every prompt is skippable with `--force`, so an agent
  never gets stuck and never loses a delete by accident.
- **Deterministic errors**: out-of-bounds input fails with a stable exit code
  and a `kg:`-prefixed message on stderr; no silent success, no auto-repair.

## Considered options

- **Keep ADR-0008's minimal help** — rejected: insufficient for agent-driven
  discovery; examples and exit codes are the disambiguation an agent relies on.
- **Machine-readable text as the default output** — rejected: humans still use
  the tool; `--json` is opt-in.
- **Interactive confirmation everywhere** — rejected: not answerable by agents.

## Consequences

- Help text grows; the `secret` ops may be brought to the same standard later
  (out of scope here).
- The completion surface and help text must stay in sync (ADR-0006/0008
  single-source rule).
- Any future verb inherits the standard: complete help, `--json` where it lists
  or reads, and no interactive prompts outside secret maintenance.
