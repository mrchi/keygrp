# keygrp

The `kg` CLI injects groups of environment variables into a target CLI,
resolving the secret-bearing ones from the OS keychain at run time. Profiles are
declared in a single TOML file and handed off to the target via `exec(2)`.
Several profiles may be combined for a single invocation — see **Combination**.
A run is `kg run <combination> <program> [args...]`, with
`kgx <combination> <program> [args...]` as its shorthand.

## Language

### Profiles

**Profile**:
An environment-variable group declared as `[profiles.<name>]` and injected into
a target program with `kg run <name> <program> [args...]`
(shorthand `kgx <name> <program>`).
_Avoid_: env group

**Base profile**:
A profile named by another profile's `extends`; its variables are merged into
the extending profile's effective variable set. Any profile can be a base, and
bases themselves are runnable.
_Avoid_: parent profile, template

**extends**:
The reserved key declaring a profile's base profile(s); its value is a profile
name or an array of names. Consumed by kg, never injected.
_Avoid_: inherit, include

**Reachable set**:
A profile together with every profile reachable through its `extends` chain,
deduplicated by profile — a diamond collapses to one copy.

**Effective variable set**:
The union of variables across a profile's reachable set, or across a
combination's member profiles. The no-shadowing rule guarantees every variable
name in it has a unique origin.

**Conflict**:
Two distinct declarations of the same variable name within a profile's reachable
set, or across a combination's members; a configuration error that invalidates
only that profile or combination.
_Avoid_: shadowing, override

**No-shadowing rule**:
A derived profile may not redeclare a variable name present anywhere in its
reachable set; doing so is a conflict.
_Avoid_: no-override

**Combination**:
A comma-separated list of profile names given to
`kg run <combination> <program>` (shorthand `kgx <combination> <program>`);
its effective variable set is the union of each member profile's effective set,
merged under the same no-shadowing rule.
_Avoid_: profile list, layers

### Secrets

**Keychain reference**:
A `keychain://<ref>` value; the only way a secret appears in the config. It is
resolved from the OS keychain at run time; the ref namespace is global across
profiles.
_Avoid_: keychain link, secret value

**Refs registry**:
The file co-located with the config recording every ref kg has written; the
source of truth for `kg secret list`, since the keychain backend cannot enumerate
items.
_Avoid_: store, database

**Secret export**:
The `kg secret export` operation; captures every ref's value from the keychain
into a single password-encrypted export archive. It covers keychain secrets
only — the config, which holds no secrets, is copied separately (git, scp).
Writes `<file>` (default `keygrp-secrets.kgx`; `-` means stdout).
_Avoid_: backup, dump

**Secret import**:
The `kg secret import` operation; decrypts an export archive and writes its
secrets into the keychain, overwriting existing refs by default or skipping
them with `--skip-existing`. Reads `<file>` (default `keygrp-secrets.kgx`;
`-` means stdin).
_Avoid_: restore, load

**Export archive**:
The password-encrypted file `secret export` produces and `secret import`
consumes; its refs and values are never readable without the password.
_Avoid_: backup file, dump

### Run

**Run command**:
The invocation `kg run <combination> <program> [args...]` that injects a
combination's effective variable set into a target program.
`kgx <combination> <program> [args...]` is its shorthand and behaves
identically.
_Avoid_: exec, launch

**Target program**:
The unmodified CLI that `kg run <profile> <program>` replaces its process image
with, carrying the profile's effective variable set.
_Avoid_: command, target
