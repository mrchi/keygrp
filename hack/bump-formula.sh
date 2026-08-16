#!/usr/bin/env bash
set -euo pipefail

# Bump the Homebrew formula in place from a release's checksums file.
#
# Usage: hack/bump-formula.sh <version> [sha256sums-file] [formula-file]
#   version          the release version without a leading "v" (e.g. 0.2.0)
#   sha256sums-file  default: dist/sha256sums.txt
#   formula-file     default: Formula/keygrp.rb

version="${1:?usage: hack/bump-formula.sh <version> [sha256sums-file] [formula-file]}"
sums_file="${2:-dist/sha256sums.txt}"
formula_file="${3:-Formula/keygrp.rb}"

if [[ ! -f "$sums_file" ]]; then
  echo "error: checksums file not found: $sums_file" >&2
  exit 1
fi
if [[ ! -f "$formula_file" ]]; then
  echo "error: formula file not found: $formula_file" >&2
  exit 1
fi

# Look up the sha256 for a given archive filename in the checksums file.
# The checksums file is `sha256sum` output: "<sha256>  <filename>".
lookup() {
  local archive="$1"
  awk -v archive="$archive" '$2 == archive { print $1 }' "$sums_file"
}

arm64_hash="$(lookup keygrp-darwin-arm64.tar.gz)"
amd64_hash="$(lookup keygrp-darwin-amd64.tar.gz)"
linux_hash="$(lookup keygrp-linux-amd64.tar.gz)"

if [[ -z "$arm64_hash" ]]; then
  echo "error: keygrp-darwin-arm64.tar.gz not found in $sums_file" >&2
  exit 1
fi
if [[ -z "$amd64_hash" ]]; then
  echo "error: keygrp-darwin-amd64.tar.gz not found in $sums_file" >&2
  exit 1
fi
if [[ -z "$linux_hash" ]]; then
  echo "error: keygrp-linux-amd64.tar.gz not found in $sums_file" >&2
  exit 1
fi

# Remember the old version string so we can assert it is gone afterwards.
old_version="$(sed -n 's/^[[:space:]]*version "\([^"]*\)".*/\1/p' "$formula_file" | head -n 1)"
if [[ -z "$old_version" ]]; then
  echo "error: no version line found in $formula_file" >&2
  exit 1
fi

# Rewrite the formula. Each sha256 is anchored by the url line immediately
# above it, so each arch receives its own hash and the urls are untouched.
tmp_file="${formula_file}.tmp.$$"
trap 'rm -f "$tmp_file"' EXIT
awk \
  -v version="$version" \
  -v arm64_hash="$arm64_hash" \
  -v amd64_hash="$amd64_hash" \
  -v linux_hash="$linux_hash" '
{
  line = $0
  if (line ~ /^[[:space:]]*version "/) {
    sub(/version "[^"]*"/, "version \"" version "\"", line)
  }
  if (line ~ /^[[:space:]]*sha256 "/) {
    if (pending == "darwin-arm64") sub(/sha256 "[^"]*"/, "sha256 \"" arm64_hash "\"", line)
    else if (pending == "darwin-amd64") sub(/sha256 "[^"]*"/, "sha256 \"" amd64_hash "\"", line)
    else if (pending == "linux-amd64") sub(/sha256 "[^"]*"/, "sha256 \"" linux_hash "\"", line)
    pending = ""
  } else {
    pending = ""
    if (line ~ /keygrp-darwin-arm64\.tar\.gz/) pending = "darwin-arm64"
    else if (line ~ /keygrp-darwin-amd64\.tar\.gz/) pending = "darwin-amd64"
    else if (line ~ /keygrp-linux-amd64\.tar\.gz/) pending = "linux-amd64"
  }
  print line
}
' "$formula_file" > "$tmp_file"
mv "$tmp_file" "$formula_file"

# Fail if any of the four replacements did not actually land.
if ! grep -F -q "version \"$version\"" "$formula_file"; then
  echo "error: version was not updated to $version in $formula_file" >&2
  exit 1
fi
if ! grep -F -q "$arm64_hash" "$formula_file"; then
  echo "error: darwin-arm64 sha256 was not updated in $formula_file" >&2
  exit 1
fi
if ! grep -F -q "$amd64_hash" "$formula_file"; then
  echo "error: darwin-amd64 sha256 was not updated in $formula_file" >&2
  exit 1
fi
if ! grep -F -q "$linux_hash" "$formula_file"; then
  echo "error: linux-amd64 sha256 was not updated in $formula_file" >&2
  exit 1
fi
if [[ "$old_version" != "$version" ]] && grep -F -q "version \"$old_version\"" "$formula_file"; then
  echo "error: old version \"$old_version\" still present in $formula_file" >&2
  exit 1
fi

echo "bumped $formula_file to version $version"
