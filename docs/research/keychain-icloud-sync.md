# Does keygrp store macOS Keychain secrets in a way that syncs via iCloud Keychain?

- **Status:** Research note (not an ADR). Question: are keygrp secrets created via `go-keyring v0.2.8` on macOS synced through iCloud Keychain by default, and can they be made to sync?
- **Verdict:** **No, not by default.** keygrp's macOS backend never sets `kSecAttrSynchronizable` (it does not even call the Security framework `SecItem*` API — it shells out to `/usr/bin/security`), so every stored item is a plain, non-synchronizable generic password in the default **login** keychain. Apple documents that an absent `kSecAttrSynchronizable` means the item is **not** added to the iCloud-synced set. Making them sync would require a different keychain backend (cgo/`SecItemAdd`) that sets `kSecAttrSynchronizable = true`, plus careful handling of queries, access groups, and the practical caveats below. keygrp exposes **no** configuration surface for any of this today.

---

## 1. What go-keyring v0.2.8 actually does on macOS

### 1.1 The macOS provider is a `security` CLI wrapper, not SecItem

`go-keyring` dispatches through a per-OS `provider` set in `init()`:

- `keyring.go:32-35` — `func Set(...)` delegates to `provider.Set(service, user, password)`.
- `keyring_darwin.go:138-140` — `init() { provider = macOSXKeychain{} }`.

The macOS implementation is a single file, `keyring_darwin.go`, and it never calls `SecItemAdd`/`SecItemCopyMatching`/`SecItemUpdate`. Instead it execs `/usr/bin/security` (`execPathKeychain`, line 28-29):

- **Set** (`keyring_darwin.go:70-101`): runs `security -i` (interactive mode) and pipes the command
  `add-generic-password -U -s <service> -a <username> -w <password>` (line 86). `-U` = update-if-exists, `-s` service, `-a` account, `-w` password. Note the value is base64-wrapped first (line 74).
- **Get** (`keyring_darwin.go:43-67`): runs `security find-generic-password -s <service> -wa <username>` (line 44-48) — `-w` = print only the password, `-a` = account.
- **Delete** (`keyring_darwin.go:104-114`): runs `security delete-generic-password -s <service> -a <username>`.

Quoting is handled by `internal/shellescape/shellescape.go:29-39` (`Quote`), which only affects how the arguments reach the shell — it does not add any keychain attribute.

### 1.2 No Security framework attributes are ever set

Because the backend is the `security` CLI, go-keyring passes **no** attribute dictionaries. There is no `kSecAttrSynchronizable`, no `kSecAttrUseDataProtectionKeychain`, no `kSecAttrAccessGroup`, no `kSecMatchLimit`, no `kSecUseOperationPrompt` — none of these constants appear anywhere in `keyring_darwin.go` (whole file read: lines 1-140). The library is not capable of expressing the synchronizable flag on this backend.

### 1.3 What the `security` command does with that

The macOS `security` man page (Apple-shipped, `/usr/share/man/man1/security.1`) documents `add-generic-password`:

- Option list: `-a, -c, -C, -D, -G, -j, -l, -s, -p, -w, -A, -T, -U, -X` — **there is no option for iCloud synchronization** (`man 1 security`, add-generic-password section, `/usr/share/man/man1/security.1:424-474`).
- "If no keychain is specified, the password is added to the default keychain." (`/usr/share/man/man1/security.1:473`).

TN3137, Apple's technical note on Mac keychains, says the default keychain in a user context is the **login** keychain and that the CLI targets the file-based keychain:

> "In a user context the search list includes a per-user *login* keychain and a single *System* keychain, with the former being the default."
> "The keychain support in the `security` command-line tool is primarily focused on the file-based keychain."
> (Apple TN3137, "On Mac keychain APIs and implementations", https://developer.apple.com/documentation/technotes/tn3137-on-mac-keychains)

So `security add-generic-password` (as invoked by go-keyring) creates a **file-based** generic password in the **login keychain**, with the synchronizable flag unset.

### 1.4 keygrp's call path

`internal/keychain/keychain.go` is a thin wrapper that hardcodes the service name and adds no other attributes:

- `service = "keygrp"` (`keychain.go:20`).
- `Set(ref, value)` → `keyring.Set(service, ref, value)` (`keychain.go:46-51`).
- `Get(ref)` → `keyring.Get(service, ref)` (`keychain.go:35-44`).
- `Delete(ref)` → `keyring.Delete(service, ref)` (`keychain.go:53-62`).

The dependency is pinned: `github.com/zalando/go-keyring v0.2.8` (`go.mod:7`, `go.sum:15-16`).

---

## 2. Verdict on the default: not synced

Two independent Apple first-party sources confirm the default behavior:

1. **`kSecAttrSynchronizable` documentation:**
   > "If the key is not supplied, or has a value of `kCFBooleanFalse`, then no synchronizable items are added or returned."
   > (https://developer.apple.com/documentation/security/ksecattrsynchronizable)

   go-keyring v0.2.8 does not supply the key at all → the item is added as a **non-synchronizable** item.

2. **TN3137** — syncing is a property of the **data protection keychain** (the backing store for iCloud Keychain), and the SecItem API must be told to target it:
   > "To target the data protection keychain, set the `kSecUseDataProtectionKeychain` attribute or the `kSecAttrSynchronizable` attribute to true."
   > "The keychain support in the `security` command-line tool is primarily focused on the file-based keychain."
   > (https://developer.apple.com/documentation/technotes/tn3137-on-mac-keychains)

   go-keyring v0.2.8 sets neither attribute and uses the CLI → the item lives in the file-based login keychain and is outside the iCloud sync set.

**Conclusion:** by default, keygrp-stored secrets are **local to the Mac's login keychain** and are **not** synced through iCloud Keychain. They also will not be included in iCloud *restore* the way synchronized items are; they are plain file-based keychain items.

---

## 3. What it would take to make them sync

The change is entirely in the keychain backend; keygrp itself would not change if the backend supported it, but with go-keyring v0.2.8 it cannot (no attribute is expressible).

1. **Create items through the SecItem API with the attribute set.** A macOS backend must call `SecItemAdd` with a dictionary that includes:
   - `kSecClass = kSecClassGenericPassword`
   - `kSecAttrService = "keygrp"`, `kSecAttrAccount = <ref>`, `kSecValueData = <secret>`
   - **`kSecAttrSynchronizable = kCFBooleanTrue`**
   This routes the item to the **data protection keychain** (iCloud Keychain sync store), per TN3137. The `security` CLI cannot express this attribute, so go-keyring v0.2.8 would need to be replaced (e.g., a cgo backend, or a newer go-keyring revision that supports it).

2. **Queries/updates/deletes must also carry the key.** Apple:
   > "If the key is not supplied, or has a value of `kCFBooleanFalse`, then no synchronizable items are added or returned."
   So `Get`/`Delete`/updates must include `kSecAttrSynchronizable` (or query with `kSecAttrSynchronizableAny` to match both kinds). Additionally, when a query uses the key, "search keys are limited to the item's class and attributes. The only search constant that may be used is `kSecMatchLimit`." (same `kSecAttrSynchronizable` page).

3. **macOS access model changes.** Apple:
   > "A keychain item created in macOS with this attribute behaves like an iOS keychain item. For example, you share the item between apps using Access Groups instead of Access Control Lists."
   > "Items stored or obtained using the `kSecAttrSynchronizable` key cannot specify `SecAccess`-based access control with `kSecAttrAccess`. If a password is intended to be shared between multiple applications, the `kSecAttrAccessGroup` key must be specified ... [with] the `keychain-access-groups` [entitlement]."
   (https://developer.apple.com/documentation/security/ksecattrsynchronizable)

   Sharing among apps/processes therefore requires a common access group; access groups are derived from code-signing entitlements (`com.apple.application-identifier` on macOS; TN3137 and https://developer.apple.com/documentation/security/sharing-access-to-keychain-items-among-a-collection-of-apps). For an unsigned or ad-hoc-signed CLI binary the access-group story is non-trivial and would need to be designed and tested.

4. **OS support.** `kSecAttrSynchronizable` is available from macOS 10.9 onward (https://developer.apple.com/documentation/security/ksecattrsynchronizable).

---

## 4. Practical caveats (if keygrp ever made items synchronizable)

- **iCloud Keychain must be enabled on the device**, the device must be signed into the same Apple Account, two-factor authentication is required, and a new device must be approved by an already-trusted device. If iCloud Keychain is not enabled, the item still lands in the data protection keychain but is shown in Keychain Access as **"Local Items"** and does **not** sync (TN3137: "It displays the latter as either *iCloud Keychain* or *Local Items*, depending on whether the user has enabled iCloud Keychain"; Apple Support, "Set up iCloud Keychain", https://support.apple.com/en-us/109016).
- **Per-item, opt-in.** Only items created with the attribute sync; existing login-keychain items do not. This is a per-item attribute, not a blanket switch.
- **Updates/deletes propagate.** Apple: "Updating or deleting items using the `kSecAttrSynchronizable` key affects all copies of the item, not just the one on your local device." (kSecAttrSynchronizable docs)
- **Class coverage.** "macOS 11 and later synchronize all classes; earlier versions synchronize only the password classes." (TN3137) — generic passwords sync on all supported versions.
- **Security implications for a secrets-injection tool.** A synced item is, by design, replicated to every trusted device on the account, and on those devices any code sharing the item's access group can read it. This materially widens the exposure of keygrp secrets (API tokens etc.) compared to the current login-keychain-only model, and it also moves the item from macOS ACL-based access control to the iOS-style access-group model, where the ACL you may have come to rely on (`-A`/`-T` semantics of the `security` CLI) no longer applies.
- **Runtime context.** The data protection keychain is "only available in a user login context. You can't use it, for example, from a `launchd` daemon." (TN3137) — matters if keygrp is ever used from daemon/service contexts.
- **Backend mismatch.** Because the `security` CLI is focused on the file-based keychain (TN3137), even a backend that *writes* synchronizable items may not *read* them back reliably via `security find-generic-password`; a real implementation would use the SecItem API for both read and write.

---

## 5. Does keygrp expose any way to configure this today?

**No.** A repo-wide search for `synchroniz|ksecattr|icloud|sync` (excluding this file) returns nothing, and the code confirms it:

- `internal/keychain/keychain.go` hardcodes `service = "keygrp"` and passes exactly three strings to go-keyring (`Set`, `Get`, `Delete`) — no attribute, flag, or option.
- The configuration surface (`internal/config/config.go`) only defines `[profiles.<name>]` tables with `extends` and plain variable→value/`keychain://` mappings; there is no keychain-attribute setting.
- There is no CLI flag for it; keygrp's interface is `keygrp <profile> <program>` / `keygrp secret set/get/delete <ref>`.

So a user cannot opt keygrp secrets into iCloud Keychain sync today; it would require a code change to the keychain backend.

---

## 6. Library options & the CLI access-group question

Follow-up research (same question, user wants the sync idea on several macOS devices): which Go libraries can actually write `kSecAttrSynchronizable=true` items, and does the whole approach work for an **unsigned** CLI?

### 6.1 Library survey — can any Go library write synchronizable items on macOS?

**`github.com/keybase/go-keychain` — YES (the only realistic option).** Classic cgo wrapper over the SecItem API.
- `Item` is just an attribute map: `type Item struct { attr map[string]interface{} }` (`keychain.go:270-273`). Nothing is set implicitly; whatever you put in the map is what gets sent.
- Synchronization is first-class: `SynchronizableDefault/Any/Yes/No` enum (`keychain.go:196-205`); `SynchronizableKey = kSecAttrSynchronizable` (`keychain.go:209`) mapped to `kSecAttrSynchronizableAny` / `kCFBooleanTrue` / `kCFBooleanFalse` (`keychain.go:210-214`); applied via `SetSynchronizable` (`keychain.go:363-370`).
- Pass-through to Security framework: `AddItem` → `C.SecItemAdd(cfDict, nil)` (`keychain.go:423-433`); `QueryItemRef` → `C.SecItemCopyMatching` (`keychain.go:476-493`); `UpdateItem`/`DeleteItem` → `SecItemUpdate`/`SecItemDelete` (`keychain.go:436-450`, `609-618`). So `Synchronizable` and `AccessGroup` you set are sent verbatim.
- Access groups: `AccessGroupKey` (`keychain.go:180`), `SetAccessGroup` (`keychain.go:358-361`), wired into `NewGenericPassword` (`keychain.go:418`) and `GetGenericPassword` (`keychain.go:653`).
- It does **not** expose `kSecAttrUseDataProtectionKeychain` (no such constant in the file) — irrelevant for sync, because setting `kSecAttrSynchronizable` alone routes to the data protection keychain (TN3137).
- Maintenance: **active** — last commit 2026-07-27, not archived, 668 stars (github.com/keybase/go-keychain). Requires macOS 10.9+ (README).
- Decisive caveat: the library cannot help an unsigned CLI get past the entitlement/access-group wall in §6.2.

**`github.com/99designs/keyring` — advertises sync in its config, but it is not wired up (does not work).**
- macOS backend (`keychain.go`) builds on the fork `github.com/99designs/go-keychain` (`keychain.go:10`), whose last commit is 2016-01-05.
- Config exposes `KeychainSynchronizable bool` — "whether the item can be synchronized to iCloud" (`config.go:17-18`); `Item` has `KeychainNotSynchronizable bool` (`keyring.go:85`).
- The backend struct has `isSynchronizable bool` (`keychain.go:19`) and `Set` would call `kcItem.SetSynchronizable(gokeychain.SynchronizableYes)` (`keychain.go:170-172`) — but the opener never assigns `isSynchronizable` from `cfg.KeychainSynchronizable` (`keychain.go:24-43`), so it is always `false`. **Items are never actually made synchronizable — dead config field.**
- Also `Get` never includes `kSecAttrSynchronizable` in the query (`keychain.go:45-60`), so even a fixed write path could not read the synchronizable item back (Apple: absent key in a query → synchronizable items not returned).
- Verdict: not usable for sync.

**`github.com/tmc/keyring` — NO.** Its macOS backend is a `/usr/bin/security` CLI wrapper identical in kind to go-keyring v0.2.8 (`Set` at `keyring_darwin.go:83-116`, `Get` 55-81, `Delete` 118-131). No synchronizable / access-group support at all.

**`github.com/zalando/go-keyring` — NO, including the latest code.**
- v0.2.8 is the newest release (releases page; v0.2.8 is only "gh: hardening workflows"). No release after v0.2.8.
- Even current `master` is still a `security` CLI wrapper. Diffing master `keyring_darwin.go` against v0.2.8: the only darwin changes are a `ListUsers` method (parses `security dump-keychain` on the default keychain — still file-based) and a test-only `restoreProvider`. No cgo/SecItem, no synchronizable support, no data-protection keychain.

**Custom ~100-line cgo shim — technically the most surgical way** to control every attribute (`kSecClass`, `kSecAttrService`/`kSecAttrAccount`, `kSecValueData`, `kSecAttrSynchronizable = kCFBooleanTrue`, `kSecAttrSynchronizableAny` on query, optionally `kSecAttrUseDataProtectionKeychain`). Pitfalls: CFType memory management, OSStatus mapping, and — the decisive one — the entitlement/access-group requirement below, which no amount of library code can bypass.

### 6.2 The crux — does iCloud Keychain sync work for an unsigned CLI? (No.)

**Apple's requirement is code-signing entitlements + a provisioning profile, not merely the API call.** TN3137, verbatim:

> "macOS builds the list of data protection keychain access groups available to your program from its code signing entitlements. For the details, see 'Sharing access to keychain items among a collection of apps'. These entitlements must be authorized by a provisioning profile. Your program needs an app-like bundle structure in which to embed that profile. This is standard for app and app extensions but not for command-line tools. For information on how to wrap a command-line tool in a dummy app-like structure, see 'Signing a daemon with a restricted entitlement'."

> "The data protection keychain is only available in a user login context. You can't use it, for example, from a `launchd` daemon."

> "If you're building library code, its data protection keychain access is determined by the entitlements of the host process's main executable."

A synchronizable item lives in the data protection keychain, and a process may only touch that keychain through an access group granted by its own entitlements.

**What access group is assigned when none is specified?** Per "Sharing access to keychain items among a collection of apps", keychain services applies the caller's *default* access group — the first group in the list built from code-signing entitlements, concretely the `application-identifier` entitlement (team ID + bundle ID) that code signing stamps into a signed app (macOS: `com.apple.application-identifier`). An unsigned / ad-hoc-signed CLI has no such entitlement. Apple DTS engineer Quinn ("The Eskimo!") states in "Can CLI apps not use SecItemAdd?" that the data protection keychain is not designed for command-line tools — there is no obvious place to embed a provisioning profile; the realistic options are to stay on the file-based keychain, or to wrap the tool in an app-like bundle signed with a stable (non-ad-hoc) identity (developer.apple.com/forums/thread/824311).

**The OS enforces this with `errSecMissingEntitlement` (-34018):** "Client has neither application-identifier nor keychain-access-groups entitlements" (developer.apple.com/forums/thread/733449).

**Consequences for keygrp:**
- **Writing** a synchronizable item from an unsigned/ad-hoc keygrp binary is expected to fail with -34018 (no access group to place it in).
- **Reading it back on the SAME device from a different process:** per the sharing article, "If you specify a group to which your app doesn't belong, no items match and the query returns the `errSecItemNotFound` status. If you don't specify an access group in the query, the search matches any of your app's groups." An unsigned CLI belongs to no groups → it can match nothing in the data protection keychain.
- **Reading it back on ANOTHER device:** iCloud Keychain replication delivers the item to the other device's sync view, but the item is still tagged with the creator's access group; only a process entitled to that same group can match it. Two ad-hoc/unsigned CLIs cannot share a group (they cannot declare one). Cross-device CLI reads therefore require shipping a signed, entitlement-bearing, app-bundle-wrapped keygrp (same team + bundle ID + `keychain-access-groups`) on every Mac.
- The correct query flags (`kSecAttrSynchronizable` / `kSecAttrSynchronizableAny` + service + account) are necessary but not sufficient — they only select items within the caller's accessible set.

**Plain verdict: native iCloud Keychain sync is not viable for an unsigned CLI.** It requires distributing keygrp as a signed app-like bundle with a provisioning profile on every Mac — a major distribution change, and a fragile one (ad-hoc signing explicitly will not do). The `kSecAttrSynchronizable` docs' "behaves like an iOS keychain item" paragraph reinforces this: such items move to the access-group (iOS-style) sharing model that an unsigned CLI cannot participate in.

### 6.3 Recommendation

- **(a) iCloud Keychain sync via a cgo backend** (e.g., keybase/go-keychain with `SetSynchronizable(SynchronizableYes)`): blocked for keygrp by the entitlement/app-wrapper requirement. High friction; changes distribution and the trust model (secrets replicated to every trusted device, readable by same-access-group code). Not recommended today.
- **(b) Age-encrypted vault file synced via iCloud Drive, master key in the local login keychain — recommended:**
  - Keep the existing go-keyring v0.2.8 / `security` CLI backend for the keychain (works fine, no code-signing needed).
  - Store the actual secrets in one age-encrypted file under iCloud Drive (e.g. `~/Library/Mobile Documents/com~apple~CloudDocs/keygrp/vault.age`), which iCloud Drive syncs to all the user's Macs automatically.
  - Keep the age X25519 identity (or a passphrase-derived key) in the local login keychain — non-synchronized; each Mac does a one-time `keygrp vault key import` or uses the same passphrase. The vault blob is end-to-end encrypted with age, so neither Apple nor iCloud can read the plaintext — a stronger guarantee than iCloud Keychain item encryption.
  - Robustness: works with an unsigned CLI; no entitlements, no code signing, no app bundle. Effort: a new backend + a few CLI commands; `internal/config` resolution already generalizes to non-keychain refs.
  - Trade-off vs (a): no instant push to all devices and no per-item ACL — acceptable for a developer-secrets tool that resolves a secret at run time.

**Recommendation: (b).** It delivers the user's actual goal (secrets available on all their Macs) with far less friction and a better security model for keygrp specifically.

---

## Sources

### Code (local, authoritative)
- `github.com/zalando/go-keyring v0.2.8` — pinned in `/Users/chi/PlayGround/keygrp/go.mod:7`.
- `/Users/chi/Golang/pkg/mod/github.com/zalando/go-keyring@v0.2.8/keyring_darwin.go` — macOS provider; `Set` lines 70-101, `Get` 43-67, `Delete` 104-114, `init` 138-140; `execPathKeychain` 28-29.
- `/Users/chi/Golang/pkg/mod/github.com/zalando/go-keyring@v0.2.8/keyring.go` — `Set`/`Get`/`Delete` delegation, lines 32-50.
- `/Users/chi/Golang/pkg/mod/github.com/zalando/go-keyring@v0.2.8/internal/shellescape/shellescape.go` — arg quoting only, lines 29-39.
- `/Users/chi/PlayGround/keygrp/internal/keychain/keychain.go` — `service` const line 20; `Set` 46-51, `Get` 35-44, `Delete` 53-62.
- `/Users/chi/PlayGround/keygrp/internal/config/config.go` — config surface (no keychain attributes).

### Libraries surveyed (fetched from GitHub; keygrp does NOT depend on them)
- `github.com/keybase/go-keychain` `keychain.go` — https://raw.githubusercontent.com/keybase/go-keychain/master/keychain.go (`Item` struct 270-273; `Synchronizable` enum 196-205; `SynchronizableKey` 209-214; `SetSynchronizable` 363-370; `AccessGroupKey` 180; `SetAccessGroup` 358-361; `AddItem`→`SecItemAdd` 423-433; `QueryItemRef`→`SecItemCopyMatching` 476-493; no `UseDataProtectionKeychain`). Repo: https://github.com/keybase/go-keychain (last commit 2026-07-27, not archived).
- `github.com/99designs/keyring` — `keychain.go` (macOS backend on `99designs/go-keychain`, line 10; `isSynchronizable` 19; opener 24-43 never assigns it; `Set` sync check 170-172; `Get` query 45-60), `config.go` (`KeychainSynchronizable` 17-18), `keyring.go` (`KeychainNotSynchronizable` 85). https://github.com/99designs/keyring/blob/master/keychain.go ; fork https://github.com/99designs/go-keychain (last commit 2016-01-05).
- `github.com/tmc/keyring` `keyring_darwin.go` — `security` CLI wrapper; no sync support. https://github.com/tmc/keyring/blob/master/keyring_darwin.go
- `github.com/zalando/go-keyring` — releases page https://github.com/zalando/go-keyring/releases (v0.2.8 latest); master `keyring_darwin.go` still a `security` CLI wrapper (only `ListUsers` + `restoreProvider` added vs v0.2.8).

### Apple first-party documentation
- `kSecAttrSynchronizable` — https://developer.apple.com/documentation/security/ksecattrsynchronizable
- TN3137 "On Mac keychain APIs and implementations" — https://developer.apple.com/documentation/technotes/tn3137-on-mac-keychains (access groups from code-signing entitlements / provisioning profile / app-like bundle; data protection keychain only in a user login context; `security` CLI is file-based-focused)
- "Keychain services" overview — https://developer.apple.com/documentation/security/keychain-services
- "Sharing access to keychain items among a collection of apps" — https://developer.apple.com/documentation/security/sharing-access-to-keychain-items-among-a-collection-of-apps (default access group = first group from code-signing entitlements / `application-identifier`; add to a group you don't belong to → `errSecMissingEntitlement`; query a group you don't belong to → `errSecItemNotFound`)
- "Signing a daemon with a restricted entitlement" — https://developer.apple.com/documentation/xcode/signing-a-daemon-with-a-restricted-entitlement (cross-referenced by TN3137: how to wrap a command-line tool in a dummy app-like structure to carry restricted entitlements)
- Apple Developer Forums (Apple DTS / Quinn): "Can CLI apps not use SecItemAdd?" — https://developer.apple.com/forums/thread/824311 (data protection keychain not designed for CLIs; no place to embed a provisioning profile; use file-based keychain or an app-wrapper; no ad-hoc signing). "-34018 Client has neither application-identifier nor keychain-access-groups entitlements" — https://developer.apple.com/forums/thread/733449
- `security(1)` man page (Apple-shipped) — `/usr/share/man/man1/security.1`; `add-generic-password` options + "added to the default keychain" at line 473; `find-generic-password` `-w` at line 580.
- Apple Support, "Set up iCloud Keychain" — https://support.apple.com/en-us/109016 (requirements: latest OS, two-factor authentication, same Apple Account, per-device enablement, device approval).
