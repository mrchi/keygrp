# ADR-0005: Secret export/import — password-encrypted archive

- Status: accepted
- Date: 2026-08-13
- Related: `docs/adr/0001-keygrp-design.md`, `.scratch/keygrp/spec.md`

## Context

The config holds no secrets, so a keygrp setup is portable by copying
`config.toml` via git or scp. The one thing that cannot be copied is the
keychain contents themselves: there is no way to move secrets between machines
or back them up. This ADR records the transfer mechanism.

## Decision

Add two secret subcommands with an identical file contract:

```
keygrp secret export [<file>]     # default keygrp-secrets.kgx; "-" = stdout
keygrp secret import [<file>]     # default keygrp-secrets.kgx; "-" = stdin
```

- **Export** reads every ref in the refs registry, prompts for a password
  (verified by re-entry, mirroring `set`), and writes a single
  password-encrypted archive. A ref missing from the keychain is warned about
  on stderr, skipped, and the run exits 2 (ADR-0001 §6) so a partial archive
  never passes for a complete one; a non-`ErrNotFound` backend error aborts.
- **Import** prompts once for the password, decrypts, and writes each ref into
  the keychain, **overwriting by default** (the archive is authoritative);
  `--skip-existing` leaves existing refs untouched. Written refs are added to
  the refs registry, mirroring `set`. A wrong password or corrupted archive
  fails GCM authentication and exits 2 with a clear message.
- Scope is keychain secrets only; the config is out of scope.

### Archive format

The archive is a versioned binary envelope. Header fields are plaintext so
import can read the version and KDF parameters before decrypting; refs and
values live only inside the ciphertext.

```
magic "KGXP" (4 bytes) | version (1 byte) | KDF id (1 byte)
iterations (4 bytes BE) | salt (16 bytes) | nonce (12 bytes)
metadata length (2 bytes BE) | metadata (JSON: created timestamp, keygrp version)
ciphertext (AES-256-GCM seal of the JSON ref→value map)
```

- **Key derivation**: PBKDF2-HMAC-SHA256 with 600,000 iterations (OWASP
  recommendation; `crypto/pbkdf2` is stdlib since Go 1.24), random 16-byte
  salt. **Cipher**: AES-256-GCM, random 12-byte nonce. Zero new dependencies.
- **Iteration-count cap**: `Decrypt` rejects an in-header iteration count above
  4 × 600,000 before any key derivation, so a hostile or over-cap archive
  cannot force unbounded PBKDF2 work prior to authentication. This is a
  forward-compatibility ceiling: a future release that raises the emitted count
  must keep it below the cap. An over-cap archive is rejected as `ErrAuth`,
  indistinguishable from a wrong password by design — a caller can never
  mistake a failed open for a successful one.
- The header prefix through the metadata is bound as GCM additional
  authenticated data, so tampering with the version or KDF parameters fails
  decryption rather than downgrading.
- The version byte and in-header KDF parameters keep old archives decryptable
  by future keygrp releases.
- The plaintext metadata is deliberately human-inspectable; a leak reveals that
  an archive exists, when it was made, and which keygrp made it — accepted
  trade-off (refs and values stay confidential).

## Consequences

- Secrets move between machines (copy `config.toml` + export/import) and back
  up to an encrypted file, e.g.
  `keygrp secret export - | ssh host keygrp secret import -`.
- The default file `keygrp-secrets.kgx` is overwritten by each export; dated
  archives require an explicit filename.
- Security rests on the password and PBKDF2 parameters; the ref→value map is
  JSON and not compressed.
