# ADR-0002: Shell completion and init

- Status: accepted
- Date: 2026-08-12
- Related: `docs/adr/0001-keygrp-design.md`, `.scratch/keygrp/spec.md`

## Context

keygrp's first release had no shell tab completion. This ADR records the
completion architecture and the `init` setup command. A CLI framework (cobra)
was explicitly considered and rejected: its completion covers only its own
command tree, so the hardest part of keygrp completion — delegating to the
injected program's own completion — would still require custom per-shell work,
at the cost of rewriting the reviewed, committed command dispatch.

## Decision

### Completion architecture

- **No framework. DIY.**
- `keygrp completion <shell>` prints a per-shell completion script
  (`fish`/`zsh`/`bash`) to stdout. It is the low-level mechanism; `init` is the
  recommended user path.
- `keygrp __complete -- <words...>` is the hidden protocol brain. It returns
  either candidate lines (keygrp-owned positions) or a directive, possibly with
  extra candidate lines that follow the directive line:
  - candidates: plain values, one per line (values beginning with
    `__directive:` are reserved and never emitted);
  - `__directive:command` — complete a command name (shell-native);
  - `__directive:delegate` — delegate to the injected program's completion;
  - `__directive:file` — complete the current token as a file path
    (shell-native). Extra candidates after the directive line (e.g. import's
    `--skip-existing` at its first, file-or-flag position) are offered
    alongside the path completion.
- Shell scripts are thin executors of the directive. Front-part candidates
  (subcommands, flags, profile names, refs) come from `__complete`; command-name
  completion, target-argument delegation, and file-path completion are
  shell-native (fish `complete -C`, zsh strip-and-`_normal`, bash
  `_command_offset`; fish `__fish_complete_path`, zsh `_files`, bash
  `compgen -f`).
- Protocol rules: always exit 0; stderr silent — a missing config means empty
  candidates, never an error.
- **Regeneration**: required only when the directive vocabulary or shell
  mechanics change. Data changes (config profiles, stored refs) never require
  regeneration because candidates are fetched at completion time.

### `init` command

- `keygrp init [--shell fish|zsh|bash]`:
  1. detects the current shell — version variables (`$ZSH_VERSION`,
     `$BASH_VERSION`; fish exports none), then the parent process name, then
     `$SHELL` — overridable with `--shell`;
  2. ensures a config file exists at the config path (`~/.config/keygrp/
     config.toml` or `$KEYGRP_CONFIG`), writing a comment-only starter (0600,
     parent dirs 0700) when none exists — an existing config is never
     modified, so re-running `init` after the user writes their real config
     is a no-op for the file;
  3. writes the completion script to that shell's standard location
     (fish `~/.config/fish/completions/keygrp.fish`, honoring
     `$XDG_CONFIG_HOME`; zsh `~/.zfunc/_keygrp`;
     bash `~/.local/share/bash-completion/completions/keygrp`);
  4. prints what it did — zsh additionally prints the `fpath` + `compinit`
     step it cannot perform for the user;
  5. runs a **read-only** keychain authorization probe (`Get` on a sentinel
     ref that cannot exist), so the macOS authorization dialog appears during
     setup rather than at first `secret set`. Linux (Secret Service) has no
     such dialog; the probe is a no-op there.
- Only the detected (or `--shell`-given) shell is installed to — never all
  shells by default.

### Help text

- `usage`/`--help` mentions `$KEYGRP_CONFIG` and the new subcommands
  (`init`, `completion`). No `config-path` command.

## Consequences

### Positive

- One-command setup (`keygrp init`) covering completion install, a starter
  config when none exists, and keychain pre-authorization — so `keygrp check`
  succeeds on a fresh install before any editing.
- Completion logic is testable Go (the `__complete` brain); shell scripts are
  thin.
- Data changes never require regenerating completion.
- Keychain authorization is prompted during an explicit setup step.
- Adding the `__directive:file` vocabulary (ADR-0005's `secret export`/`import`
  file position) required regenerating the scripts; the zsh script also now
  passes `"${words[@]}"` quoted so a trailing empty current token (cursor after
  a space) reaches `__complete` — unquoted `$words` drops it, misrouting
  empty-token positions.

### Negative / accepted

- Subcommand words (`secret`, `check`, `init`, `completion`) are reserved in
  the completion dispatch; a profile named after one is unroutable, matching
  the run command's own reserved-word handling.
- The per-shell scripts genuinely differ (delegation mechanics are not
  portable across shells).
- zsh keeps one manual `fpath` step.
- macOS keychain authorization is granted per binary path; reinstalling at a
  different path may require re-running `init`.
- The keychain probe is macOS-only; on Linux it is a no-op.
