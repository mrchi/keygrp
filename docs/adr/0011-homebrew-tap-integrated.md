# ADR-0011: Homebrew tap integrated into the keygrp repo

- Status: accepted
- Date: 2026-08-16
- Related: `docs/adr/0009-release-manual-tag-push-binaries.md`, `docs/adr/0010-platform-scope.md`

## Context

The release artifacts from `docs/adr/0009` (per-arch tarballs plus a
`sha256sums.txt`) were deliberately shaped to be consumed by a binary Homebrew
formula, but the tap itself was deferred. The open question was where the tap
lives and how the formula stays in sync with each release.

Two constraints shaped the answer. First, the repo is named `keygrp`, not
`homebrew-keygrp`, so the conventional short tap command
(`brew tap mrchi/keygrp`) will not resolve — Homebrew only auto-prefixes
`homebrew-*` when the tap is given without a full URL. An explicit-URL tap
(`brew tap mrchi/keygrp https://github.com/mrchi/keygrp`) works, but current
Homebrew treats any third-party tap as untrusted and refuses to install from it
until `brew trust mrchi/keygrp` is run. Second, the formula is a supply-chain
surface: it pins the checksums users' installs are verified against, so it
should be statically reviewed in the repo rather than regenerated from whole
cloth on every release.

## Decision

- **Tap is the keygrp repo itself**, via an explicit URL:
  `brew tap mrchi/keygrp https://github.com/mrchi/keygrp`, followed by
  `brew trust mrchi/keygrp`. The tap name (`mrchi/keygrp`) and the repo path
  (`mrchi/keygrp`) coincide, so the formula lives at `Formula/keygrp.rb` at the
  repo root.
- **The formula is committed and statically reviewed.** It is a binary formula
  whose per-arch `url` lines interpolate `v#{version}` and whose `sha256` lines
  are fixed at release time.
- **The release workflow bumps only `version` + the three `sha256` values in
  place**, via `hack/bump-formula.sh`, and commits the result to `main`. The
  urls never change because they interpolate `#{version}`; the diff is exactly
  four lines (version + darwin-arm64, darwin-amd64, linux-amd64 hashes), each
  hash anchored by the url line immediately above it.
- **The bump commits as `github-actions[bot]`** with a
  `chore(brew): bump formula to vX.Y.Z` message, using the job's
  `permissions: contents: write`.

## Considered options

- **A separate `homebrew-keygrp` repo.** Rejected: gives the standard
  `brew tap mrchi/keygrp` UX (no URL, no `brew trust` in older Homebrew), but
  the release workflow would have to push across repositories, which needs a
  PAT or deploy key instead of the default `GITHUB_TOKEN` — extra secret
  management for a solo-maintained project.
- **Full formula regeneration by the workflow.** Rejected: a generated formula
  loses the reviewed-static-file property; the four-line diff is the point,
  and regeneration would rewrite boilerplate every time, burying the checksum
  change.
- **Manual formula editing.** Rejected: reintroduces human transcription of
  hashes from `sha256sums.txt`, the exact class of error the bump script
  removes.

## Consequences

### Positive

- One reviewed, static file encodes the whole install path; a release diff of
  four lines is trivial to eyeball in review.
- No cross-repo automation or extra secrets — the bump pushes to `main` with
  the default `GITHUB_TOKEN`.
- The tap name equals the repo path, so the formula lives at the repo root with
  no naming indirection.

### Negative / accepted

- Tags must be pushed from the `main` tip: the formula bump commit lands on
  `main`, so a tag pushed before that commit would ship a formula pointing at
  the previous version. The release flow is `tag vX.Y.Z && push` from a green
  `main`, which already satisfies this.
- Every release leaves an automatic `chore(brew)` commit on `main`, which
  triggers a harmless CI run (test + lint) for a non-Go change.
- Current Homebrew requires `brew trust mrchi/keygrp` for third-party taps; the
  install docs call this out so users hit it before `brew install`.
