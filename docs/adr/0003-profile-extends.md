# ADR-0003: Profile extends — merge without shadowing

- Status: accepted
- Date: 2026-08-12
- Related: `docs/adr/0001-keygrp-design.md`, `.scratch/keygrp/spec.md`

## Context

Profiles routinely duplicate each other's variable declarations: the spec's own
`aws` and `terraform` examples both declare `AWS_ACCESS_KEY_ID` and
`AWS_SECRET_ACCESS_KEY` pointing at the same refs. The keychain ref namespace is
already global (ADR-0001 §5), so secrets were already shared; what was duplicated
was the variable mapping. This ADR records the mechanism for sharing variable
mappings between profiles.

## Decision

A profile may declare one or more **base profiles** with the reserved key
`extends`:

```toml
[profiles.terraform]
extends = ["aws", "gcp"]
TF_TOKEN = "keychain://terraform-token"
```

- `extends` accepts a single profile name or an array; duplicates are dropped; a
  non-string or empty name is a configuration error, and an empty array is
  equivalent to no extends. It is consumed by keygrp, never injected, so a
  profile that extends cannot inject an environment variable literally named
  `extends` (consistent with the reserved subcommand words of ADR-0002).
- A base profile is an ordinary profile: it may itself extend, be extended, and
  be run. There is no separate "template" kind.
- The **effective variable set** of a profile is the union of variables across
  its **reachable set** — itself plus the transitive closure of `extends`,
  deduplicated by profile (a diamond collapses to one copy).
- **No-shadowing rule.** Two distinct declarations of the same variable name
  within one profile's reachable set is a **conflict** — a configuration error
  (exit 1), judged by name regardless of value. There is no exclusion syntax:
  inheritance is all-or-nothing.
- Conflicts are scoped per profile: only a profile whose own reachable set
  conflicts is invalid; running an unrelated profile still works. `check`
  reports all conflicts; `run` fails fast; `__complete` stays silent (candidates
  are data-driven and never error).
- A missing base or an `extends` cycle is a configuration error, detected at
  load — before program lookup and keychain access, so a broken chain never
  triggers a keychain prompt.
- Profile-to-shell override is unchanged (ADR-0001 §4); the no-shadowing rule
  governs profile-to-profile only.

## Considered options

- **Child-overrides-parent inheritance** — rejected: it forces a precedence (and
  multi-base ordering) into resolution, against ADR-0001's
  determinism-over-politeness. No-shadowing keeps resolution a pure set
  construction with a unique origin per variable.
- **Adjacent-only disjointness** — rejected: it lets a derived profile shadow a
  grandparent's variable, i.e. an override survives inside the chain,
  contradicting the no-override intent.
- **`exclude = [...]` reserved key** — rejected for v1: a second reserved key
  plus interaction between exclusion and the conflict rule; all-or-nothing keeps
  one reserved key and one rule.
- **Separate top-level `[extends]` table** — rejected: separates declaration from
  profile and invites top-level-table name collisions.

## Consequences

### Positive

- DRY variable mappings; the spec's `aws`/`terraform` duplication collapses to a
  one-line `extends`.
- Deterministic resolution by construction: every effective variable has a
  unique origin.
- Multi-base needs no precedence ordering — the union is commutative under
  no-shadowing.
- Per-profile isolation: a broken derived profile never bricks unrelated
  profiles.

### Negative / accepted

- No override and no exclusion: a profile that wants a base minus one variable
  must copy the declaration.
- `extends` is reserved: an extending profile cannot inject an env var named
  `extends`.
- The v1 config contract narrows (ADR-0001 §9): `config.Parse` consumes
  `extends` instead of accepting every key as injectable.
- In `check`, configuration errors (exit 1) take precedence over keychain
  conditions (exit 2), and `check --profile` with an unknown profile is itself
  a configuration error.
