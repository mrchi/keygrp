# ADR-0009: Release — manual tag-push with cross-compiled binaries

- Status: accepted
- Date: 2026-08-16
- Related: `docs/adr/0010-platform-scope.md`, `CONTEXT.md`

## Context

keygrp is public and MIT-licensed, and its README invites contributions, so the
audience extends beyond its author. The only install path today is
`go install github.com/mrchi/keygrp/cmd/...@latest`, which requires the user to
have Go. As a secrets-management tool, distribution also has a supply-chain
dimension: prebuilt binaries shipped with checksums give a stronger story than
the `@latest` wildcard. The question was whether to produce release binaries at
all, and how.

One fact shaped the whole answer: go-keyring v0.2.8's darwin backend is **pure
Go** — it shells out to `/usr/bin/security` via `os/exec`, it does not use cgo.
All three platform backends (darwin, linux secret-service, windows wincred) are
cgo-free, so every release target can be cross-compiled from a single Linux
runner with `CGO_ENABLED=0`. No macOS runner or osxcross toolchain is needed.

## Decision

- **Manual release**: the release workflow triggers on `push` of a `v*` tag
  (`on: push: tags: ['v*']`). "Manual" means the author decides the moment by
  `git tag vX.Y.Z && git push origin vX.Y.Z` — the idiomatic Go ecosystem
  gesture, with no workflow_dispatch tag-input plumbing.
- **Targets**: `darwin/amd64`, `darwin/arm64`, `linux/amd64`. The author's daily
  driver is macOS (both architectures) and Linux covers server use. Windows is
  deliberately excluded — see `docs/adr/0010-platform-scope.md`.
- **Artifacts**: one tarball per target, `keygrp-<os>-<arch>.tar.gz`, containing
  both `kg` and `kgx`, plus a `sha256sums.txt`. Built in a single ubuntu job
  with `CGO_ENABLED=0 go build ./cmd/...`.
- **Gate**: the workflow runs `go test ./...` before building, so a release can
  never be cut from a red tree (lint is not re-run; a `v*` tag is only pushed
  onto main, which CI has already verified).
- **Attachment**: assets are attached to the GitHub Release for the triggering
  tag via `softprops/action-gh-release` with `GITHUB_TOKEN`
  (`permissions: contents: write`).
- **Future Homebrew tap, deferred**: the artifacts are deliberately shaped to be
  consumed by a binary-style tap formula (per-arch `url` + `sha256` from
  `sha256sums.txt`). The tap repo itself is out of scope for now.

## Considered options

- **go install only (no binaries).** Rejected: shuts out non-Go users and keeps
  the `@latest` wildcard as the only install path for a security tool.
- **goreleaser.** Rejected: heavy configuration and a learning curve, out of
  proportion to a manual, low-frequency personal release flow.
- **`workflow_dispatch` button with a tag input.** Rejected in favor of
  tag-push: the Actions UI button needs the workflow to create and push the tag
  itself, adding plumbing for no benefit over a tag push.
- **Full platform matrix including Windows.** Rejected: see
  `docs/adr/0010-platform-scope.md`.

## Consequences

### Positive

- Prebuilt, checksummed binaries for users without Go, with a pinned semver
  upgrade path instead of `@latest`.
- One cheap Linux job produces every target — no macOS/Windows runners.
- Artifacts are already in the shape a future binary Homebrew tap needs.

### Negative / accepted

- A release requires the author to create and push a tag; fixes must re-tag and
  re-push.
- Asset naming and the checksum convention must be kept consistent across
  releases (the tarball layout is a contract for the future tap formula).
