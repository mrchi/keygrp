# ADR-0010: Platform scope — CI on ubuntu only, no Windows support

- Status: accepted
- Date: 2026-08-16
- Related: `docs/adr/0009-release-manual-tag-push-binaries.md`

## Context

go-keyring, the keychain backend, supports darwin, linux, and windows. keygrp's
test suite, however, runs entirely against an in-memory fake keychain
(`internal/testutil.MemStore`), so every line of testable logic is
platform-independent; the platform-specific code is the keychain backend
selection itself, which the tests never reach. The author's daily driver is
macOS; there is no Windows consumer. The question was how much platform
coverage CI should buy.

## Decision

- **CI test and lint run on a single `ubuntu` runner.** No darwin or windows
  runners, no `GOOS=windows` cross-compile check.
- **Release targets** are `darwin/amd64`, `darwin/arm64`, and `linux/amd64`
  only; no Windows binary (`docs/adr/0009`).

The reasoning for the darwin omission is worth recording, because it looks
counterintuitive: darwin is *the* platform that matters to the author, yet it
gets no CI runner. The macOS keychain backend is compiled and exercised daily by
local development on macOS, so a CI macOS runner would only guard against
contributor PRs breaking the darwin build — near-zero value for a project whose
only contributor is the author. Linux is covered by the ubuntu runner; Windows
(wincred) is the one target validated nowhere, accepted because it has no
consumer.

## Considered options

- **Three-OS matrix (ubuntu + macos + windows).** Rejected: because the tests
  are hermetic, the matrix adds only *compile* coverage of platform-tagged
  files — and that coverage is already free on darwin (local development) and
  linux (CI). Three runners of cost for no marginal value.
- **ubuntu + a `GOOS=windows go build ./...` compile check.** Rejected: cheap,
  but Windows is explicitly out of scope; adding the check keeps a target alive
  in the build that no one consumes.

## Consequences

### Positive

- Cheap, fast CI with no Windows maintenance surface.
- A clear, recorded answer to "why no windows" for a future contributor who
  notices go-keyring supports it.

### Negative / accepted

- A contributor PR that breaks the darwin or windows build goes undetected
  until someone builds on those platforms. For a solo-maintained project this
  is acceptable.
- Windows stays unsupported until a consumer appears; adopting it later is a
  small change (add a runner / target), not a re-architecture.
