# ADR-0004: Profile combination — run-time merge without shadowing

- Status: accepted
- Date: 2026-08-12
- Related: `docs/adr/0001-keygrp-design.md`, `docs/adr/0003-profile-extends.md`,
  `.scratch/keygrp/spec.md`

## Context

ADR-0003 gives profiles *declared* composition: a profile statically inherits one
or more bases. There is no way to merge the effective variable sets of
independently-chosen profiles at invocation — `keygrp <profile> <program>`
accepts exactly one profile name, so a multi-cloud run (`terraform` with both
`aws` and `gcp` credentials) requires declaring a new profile that `extends` both.
This ADR records the mechanism for *selection-time* composition: ephemeral,
per-invocation, without a config edit.

## Decision

`keygrp` accepts a **combination** as its first argument: a comma-separated list
of profile names.

```
keygrp aws,gcp terraform
```

- A combination's **effective variable set** is the union of each member
  profile's effective set — each member resolved through its own `extends` chain
  first — merged under the **same no-shadowing rule** as ADR-0003. Two
  declarations of the same variable name from *distinct origins* is a
  **conflict** (configuration error, exit 1); the same variable reached from the
  same origin — a shared base, or a profile repeated in the list — collapses to
  one copy. Order is irrelevant: `aws,dev` ≡ `dev,aws`.
- A single profile name is a combination of one, unchanged from ADR-0001 §2.
- **Strict list syntax.** A leading, trailing, or double comma — an empty
  element — is a usage error (exit 2). An unknown member name is a configuration
  error (exit 1), as today.
- **The comma is reserved.** Profile names may not contain commas; `config.Parse`
  rejects them. (`:` and `+` remain legal in names.)
- `extends` values remain profile names, never combinations — declared and
  selection-time composition stay orthogonal; a combination is not a config
  object.
- **`check --profile <combination>`** validates the merged set with the same
  rules. Bare `check` still validates each profile singly: combinations are
  unbounded and are never enumerated.
- **`--verbose` annotates origin**: each injected variable prints as
  `AWS_ACCESS_KEY_ID (from aws)`.
- **Completion.** The `__complete` brain splits the profile token on its last
  comma and offers the not-yet-selected profile names as whole-token candidates
  (`aws,<TAB>` → `aws,dev`, `aws,gcp`, …); already-selected members are excluded.
  Shell scripts are unchanged; `check --profile` reuses the same logic.

## Considered options

- **Repeated `--profile` flag** — rejected: each flag completes one value, so
  multi-profile selection needs one tab per member; the run command gains flag
  parsing and an ambiguous flag-vs-program position; the CLI contract reshapes
  more heavily than a single-token list.
- **`+` separator** — rejected on convention alone: identical completion
  mechanics, a less familiar separator.
- **Last-wins override precedence** — rejected: order-dependent resolution
  against ADR-0003's determinism-over-politeness; override would blur which
  member is the origin of an injected secret. Layering is achieved by declaring
  a new profile via `extends`.
- **Same-value dedupe, different-value error** — rejected for v1: weakens
  "judged by name regardless of value" with a new value-comparison rule, and
  still enables no layering.

## Consequences

### Positive

- Ephemeral composition without config edits; multi-credential invocations
  (`keygrp aws,gcp terraform`) become first-class.
- Zero new conflict semantics: selection-time merge reuses the origin machinery
  of ADR-0003 (`resolved{env, origin}`).
- Deterministic and commutative: no precedence ordering to reason about.

### Negative / accepted

- Profile names may no longer contain commas (previously allowed).
- A combination cannot override a member's variable; the no-shadowing rule is
  identical to `extends`.
- `--verbose` output format changes (origin annotation).
- The CLI contract (ADR-0001 §2) widens: the `<profile>` token is now a
  comma-separated list; the config contract narrows (`Parse` rejects commas in
  profile names).
