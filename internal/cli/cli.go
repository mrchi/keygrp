// Package cli holds the entire keygrp command-line surface. The `kg` and `kgx`
// binaries are thin mains over KG and KGX (ADR-0007): two entry points that
// share one run path so the grammars cannot diverge.
package cli

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"

	"github.com/mrchi/keygrp/internal/completion"
	"github.com/mrchi/keygrp/internal/config"
	"github.com/mrchi/keygrp/internal/exportimport"
	"github.com/mrchi/keygrp/internal/keychain"
	"github.com/mrchi/keygrp/internal/registry"
	"github.com/mrchi/keygrp/internal/runner"
	"golang.org/x/term"
)

// secretOpsText is the `secret` op list, shared by usageText and
// `kg secret --help` so the op lines are authored once.
const secretOpsText = `  kg secret set [--stdin] <ref>       store a secret in the keychain
  kg secret get [--reveal] <ref>      show whether a secret exists
  kg secret delete <ref>              remove a secret
  kg secret list                      list stored secret refs
  kg secret export [<file>]           export secrets to a password-encrypted archive
  kg secret import [--skip-existing] [<file>]  restore secrets from an archive
`

const usageText = `kg injects a group of environment variables into a target CLI,
resolving secrets from the OS keychain at run time.

usage:
  kg run [--verbose] <combination> <program> [args...]
      run <program> with <combination>'s env (profiles comma-separated, e.g. aws,gcp);
      --verbose prints injected var names with their origin profile
` + secretOpsText + `  kg check [--profile <combination>]  validate config and keychain refs
  kg init [--shell fish|zsh|bash]     install completion, create config & authorize keychain
  kg completion fish|zsh|bash         print a completion script
  kg --help                           show this help; any verb accepts --help

The run shorthand kgx is equivalent to kg run:
  kgx [--verbose] <combination> <program> [args...]

configuration: ~/.config/keygrp/config.toml (override with $KEYGRP_CONFIG)

exit codes: 0 ok, 1 configuration error, 2 usage or keychain error
`

// kgxUsageText is the one-line run usage that `kgx --help` prints (ADR-0007);
// the full surface belongs to `kg --help`.
const kgxUsageText = `usage: kgx [--verbose] <combination> <program> [args...]
`

// Verb-level help texts, printed by `kg <verb> --help`. The wording mirrors
// usageText so the two surfaces never drift apart.
const (
	runHelpText = `usage: kg run [--verbose] <combination> <program> [args...]
    run <program> with <combination>'s env (profiles comma-separated, e.g. aws,gcp);
    --verbose prints injected var names with their origin profile
`

	// secretHelpText reuses secretOpsText (the same op lines as usageText), so
	// the op wording is authored once (ADR-0006).
	secretHelpText = `usage: kg secret {set|get|delete|list|export|import} ...
` + secretOpsText

	checkHelpText = `usage: kg check [--profile <combination>]
    validate config and keychain refs; with --profile, validate a
    comma-separated combination's merged set
`

	// initHelpText mirrors usageText's wording (ADR-0006: help and completion
	// reuse usageText, never a fourth surface).
	initHelpText = `usage: kg init [--shell fish|zsh|bash]
    install completion, create config & authorize keychain
`

	completionHelpText = `usage: kg completion fish|zsh|bash
    print a completion script for the given shell
`

	// secret op help, printed by `kg secret <op> --help`.
	secretSetHelpText = `usage: kg secret set [--stdin] <ref>
    store a secret in the keychain; the value is prompted for and verified by
    re-entry, or read from stdin with --stdin
`

	secretGetHelpText = `usage: kg secret get [--reveal] <ref>
    show whether a secret exists; --reveal prints the value
`

	secretDeleteHelpText = `usage: kg secret delete <ref>
    remove a secret from the keychain (asks for confirmation)
`

	secretListHelpText = `usage: kg secret list
    list stored secret refs, never values; refs missing from the keychain are
    flagged
`

	secretExportHelpText = `usage: kg secret export [<file>]
    export secrets to a password-encrypted archive; the default file is
    keygrp-secrets.kgx, "-" writes to stdout
`

	secretImportHelpText = `usage: kg secret import [--skip-existing] [<file>]
    restore secrets from a password-encrypted archive; the default file is
    keygrp-secrets.kgx, "-" reads from stdin
`
)

// helpFlag reports whether token is a help request. GNU §4.8.2: --help prints
// usage and exits 0, ignoring the rest of the arguments.
func helpFlag(token string) bool {
	return token == "-h" || token == "--help"
}

// parseHelp scans args for a help request; when one is present it returns a
// kindHelp command carrying the given text. Used where every token is
// kg-owned (management verbs); under run the scan is limited to the leading
// position because the tail is the target program's own args.
func parseHelp(cmd command, args []string, help string) (command, bool) {
	if slices.ContainsFunc(args, helpFlag) {
		cmd.kind = kindHelp
		cmd.help = help
		return cmd, true
	}
	return cmd, false
}

const (
	kindRun        = "run"
	kindSecret     = "secret"
	kindCheck      = "check"
	kindInit       = "init"
	kindCompletion = "completion"
	kindComplete   = "__complete"
	kindHelp       = "help"
)

const (
	opSet    = "set"
	opGet    = "get"
	opDelete = "delete"
	opList   = "list"
	opExport = "export"
	opImport = "import"
)

// defaultArchive is the file `secret export` writes and `secret import` reads
// when no positional is given; "-" selects stdout (export) / stdin (import).
const defaultArchive = "keygrp-secrets.kgx"

// Version is stamped into the export archive's plaintext metadata. Override at
// build time: go build -ldflags "-X github.com/mrchi/keygrp/internal/cli.Version=v1.2.3".
var Version = "dev"

// command is the parsed form of the command line.
type command struct {
	kind            string // kindRun | kindSecret | kindCheck | kindInit | kindCompletion | kindComplete | kindHelp
	verbose         bool
	profiles        []string // run: combination (comma-separated profile names)
	program         string   // run
	args            []string // run passthrough
	secretOp        string   // secret: opSet | opGet | opDelete | opList | opExport | opImport
	secretRef       string
	secretFile      string   // secret export/import: path, "-" = stdout (export) / stdin (import)
	skipExisting    bool     // secret import --skip-existing
	reveal          bool     // secret get --reveal
	fromStdin       bool     // secret set --stdin
	checkTargets    []string // check --profile: combination
	initShell       string   // init --shell
	completionShell string   // completion <shell>
	completeWords   []string // __complete -- <words...>
	help            string   // kindHelp: verb-level help text; empty = the entry point's usage
}

// KG is the entry point for the `kg` binary: management verbs plus the `run`
// verb that injects a combination into a target program (ADR-0007).
func KG(args []string) int {
	return runFor(args, parseKG, usageText)
}

// KGX is the entry point for the `kgx` binary, the run shorthand: a pure
// combination + program grammar with no management verbs (ADR-0007).
func KGX(args []string) int {
	return runFor(args, parseKGX, kgxUsageText)
}

// runFor parses args with the grammar of one entry point, prints help when the
// command asks for it, and dispatches on the parsed kind. The help text differs
// per binary: kg prints the full surface, kgx a one-line run usage.
func runFor(args []string, parse func([]string) (command, error), help string) int {
	cmd, err := parse(args)
	if err != nil {
		code := fail(2, "%v", err)
		fmt.Fprint(os.Stderr, help)
		return code
	}
	if cmd.kind == kindHelp {
		if cmd.help != "" {
			fmt.Print(cmd.help)
		} else {
			fmt.Print(help)
		}
		return 0
	}
	return dispatch(cmd)
}

// dispatch runs a parsed command's kind.
func dispatch(cmd command) int {
	switch cmd.kind {
	case kindRun:
		return runProfile(cmd)
	case kindSecret:
		return runSecret(cmd)
	case kindCheck:
		return runCheck(cmd)
	case kindInit:
		return runInit(cmd)
	case kindCompletion:
		return runCompletion(cmd)
	case kindComplete:
		return runComplete(cmd)
	}
	return 2
}

// parseKG parses the kg grammar: a leading verb — run, secret, check, init,
// completion, or __complete — with the combination living after `run`, never in
// the first slot (ADR-0007). An unknown first token is a usage error that hints
// both run paths, without loading the config.
func parseKG(args []string) (command, error) {
	var cmd command
	if len(args) == 0 {
		cmd.kind = kindHelp
		return cmd, nil
	}
	switch args[0] {
	case "-h", "--help":
		cmd.kind = kindHelp
		return cmd, nil
	case "run":
		return parseRun(cmd, args[1:])
	case "check":
		if cmd, ok := parseHelp(cmd, args[1:], checkHelpText); ok {
			return cmd, nil
		}
		cmd.kind = kindCheck
		if len(args) == 3 && args[1] == "--profile" {
			names, err := splitCombination(args[2])
			if err != nil {
				return cmd, err
			}
			cmd.checkTargets = names
		} else if len(args) > 1 {
			return cmd, fmt.Errorf("usage: kg check [--profile <combination>]")
		}
		return cmd, nil
	case "init":
		if cmd, ok := parseHelp(cmd, args[1:], initHelpText); ok {
			return cmd, nil
		}
		cmd.kind = kindInit
		if len(args) == 3 && args[1] == "--shell" {
			cmd.initShell = args[2]
		} else if len(args) > 1 {
			return cmd, fmt.Errorf("usage: kg init [--shell fish|zsh|bash]")
		}
		return cmd, nil
	case "completion":
		if cmd, ok := parseHelp(cmd, args[1:], completionHelpText); ok {
			return cmd, nil
		}
		cmd.kind = kindCompletion
		if len(args) == 2 {
			cmd.completionShell = args[1]
		} else {
			return cmd, fmt.Errorf("usage: kg completion fish|zsh|bash")
		}
		return cmd, nil
	case "__complete":
		cmd.kind = kindComplete
		words := args[1:]
		if len(words) > 0 && words[0] == "--" {
			words = words[1:]
		}
		cmd.completeWords = words
		return cmd, nil
	case "secret":
		cmd.kind = kindSecret
		return parseSecret(cmd, args[1:])
	default:
		return cmd, unknownCommandError(args[0])
	}
}

// stripVerboseFlag consumes a leading run of --verbose flags, reporting whether
// any was present. It is the one place the flag's position is defined: directly
// before the run grammar's positionals.
func stripVerboseFlag(args []string) (bool, []string) {
	verbose := false
	for len(args) > 0 && args[0] == "--verbose" {
		verbose = true
		args = args[1:]
	}
	return verbose, args
}

// parseRun parses `kg run [--verbose] <combination> <program> [args...]`.
// --help is a leading flag only: after the combination, every remaining token
// is the target program's own args, so `kg run aws terraform plan --help`
// runs terraform rather than showing kg's help.
func parseRun(cmd command, rest []string) (command, error) {
	cmd.verbose, rest = stripVerboseFlag(rest)
	if len(rest) > 0 && helpFlag(rest[0]) {
		cmd.kind = kindHelp
		cmd.help = runHelpText
		cmd.verbose = false // a help command carries only kind + help
		return cmd, nil
	}
	if len(rest) < 2 {
		return cmd, fmt.Errorf("usage: kg run [--verbose] <combination> <program> [args...]")
	}
	names, err := splitCombination(rest[0])
	if err != nil {
		return cmd, err
	}
	cmd.kind = kindRun
	cmd.profiles = names
	cmd.program = rest[1]
	cmd.args = rest[2:]
	return cmd, nil
}

// parseKGX parses the kgx grammar: the combination is always the first token,
// so every position is run-scoped (ADR-0007). --verbose is a leading flag; a
// bare invocation is a usage error.
func parseKGX(args []string) (command, error) {
	var cmd command
	cmd.verbose, args = stripVerboseFlag(args)
	if len(args) == 0 {
		return cmd, fmt.Errorf("usage: kgx [--verbose] <combination> <program> [args...]")
	}
	switch args[0] {
	case "-h", "--help":
		cmd.kind = kindHelp
		return cmd, nil
	case "__complete":
		cmd.kind = kindComplete
		words := args[1:]
		if len(words) > 0 && words[0] == "--" {
			words = words[1:]
		}
		cmd.completeWords = words
		return cmd, nil
	default:
		if len(args) < 2 {
			return cmd, fmt.Errorf("usage: kgx [--verbose] <combination> <program> [args...]")
		}
		names, err := splitCombination(args[0])
		if err != nil {
			return cmd, err
		}
		cmd.kind = kindRun
		cmd.profiles = names
		cmd.program = args[1]
		cmd.args = args[2:]
		return cmd, nil
	}
}

// unknownCommandError reports a first token that is neither a kg verb nor a
// flag, statically hinting both run paths so the old bare `kg <combination>`
// muscle memory lands somewhere (ADR-0007).
func unknownCommandError(token string) error {
	return fmt.Errorf("unknown command %q; to run a profile, use \"kg run %s <program>\" or \"kgx %s <program>\"", token, token, token)
}

// splitCombination splits a combination token on commas, rejecting any empty
// element — a leading, trailing, or double comma — as a usage error (ADR-0004).
func splitCombination(token string) ([]string, error) {
	parts := strings.Split(token, ",")
	if slices.Contains(parts, "") {
		return nil, fmt.Errorf("invalid combination %q: empty profile name (leading, trailing, or double comma)", token)
	}
	return parts, nil
}

// secretOpHelpText returns the focused help text for a secret op, or "" for a
// token that is not an op (so `kg secret bogus --help` stays an unknown-op
// error rather than pretending bogus exists).
func secretOpHelpText(op string) string {
	switch op {
	case opSet:
		return secretSetHelpText
	case opGet:
		return secretGetHelpText
	case opDelete:
		return secretDeleteHelpText
	case opList:
		return secretListHelpText
	case opExport:
		return secretExportHelpText
	case opImport:
		return secretImportHelpText
	}
	return ""
}

func parseSecret(cmd command, rest []string) (command, error) {
	if len(rest) == 0 {
		return cmd, fmt.Errorf("usage: kg secret {set|get|delete|list|export|import} ...")
	}
	// The op position may itself be a help request (`kg secret --help`).
	if helpFlag(rest[0]) {
		cmd.kind = kindHelp
		cmd.help = secretHelpText
		return cmd, nil
	}
	// Op-level help is resolved before any op state is recorded, so a help
	// command carries only kind + help.
	if h := secretOpHelpText(rest[0]); h != "" && slices.ContainsFunc(rest[1:], helpFlag) {
		cmd.kind = kindHelp
		cmd.help = h
		return cmd, nil
	}
	cmd.secretOp = rest[0]
	rest = rest[1:]
	switch cmd.secretOp {
	case opList:
		if len(rest) != 0 {
			return cmd, fmt.Errorf("usage: kg secret list")
		}
	case opExport:
		for _, a := range rest {
			if a != "-" && strings.HasPrefix(a, "-") {
				return cmd, fmt.Errorf("unknown flag %q for secret export", a)
			}
			if cmd.secretFile != "" {
				return cmd, fmt.Errorf("usage: kg secret export [<file>]")
			}
			cmd.secretFile = a
		}
	case opImport:
		for _, a := range rest {
			switch a {
			case "--skip-existing":
				cmd.skipExisting = true
			default:
				if a != "-" && strings.HasPrefix(a, "-") {
					return cmd, fmt.Errorf("unknown flag %q for secret import", a)
				}
				if cmd.secretFile != "" {
					return cmd, fmt.Errorf("usage: kg secret import [--skip-existing] [<file>]")
				}
				cmd.secretFile = a
			}
		}
	case opSet, opGet, opDelete:
		for _, a := range rest {
			switch a {
			case "--stdin":
				if cmd.secretOp != opSet {
					return cmd, fmt.Errorf("--stdin is only valid for secret set")
				}
				cmd.fromStdin = true
			case "--reveal":
				if cmd.secretOp != opGet {
					return cmd, fmt.Errorf("--reveal is only valid for secret get")
				}
				cmd.reveal = true
			default:
				if strings.HasPrefix(a, "-") {
					return cmd, fmt.Errorf("unknown flag %q for secret %s", a, cmd.secretOp)
				}
				cmd.secretRef = a
			}
		}
		if cmd.secretRef == "" {
			return cmd, fmt.Errorf("usage: kg secret %s <ref>", cmd.secretOp)
		}
	default:
		return cmd, fmt.Errorf("unknown secret operation %q", cmd.secretOp)
	}
	return cmd, nil
}

func runProfile(cmd command) int {
	path := configPath()
	cfg, err := loadConfig(path)
	if err != nil {
		return fail(1, "%v", err)
	}
	warnPermissive(path)

	for _, name := range cmd.profiles {
		if _, ok := cfg.Profiles[name]; !ok {
			return fail(1, "profile %q not found in %s", name, path)
		}
	}
	effective, origins, err := cfg.EffectiveSet(cmd.profiles)
	if err != nil {
		return fail(1, "%v", err)
	}
	p := config.Profile{Vars: effective, Origins: origins}
	if err := runner.Run(p, cmd.program, cmd.args, keychain.Keychain{}, cmd.verbose); err != nil {
		code := 1
		if errors.Is(err, keychain.ErrNotFound) || errors.Is(err, keychain.ErrBackend) {
			code = 2
		}
		return fail(code, "%v", err)
	}
	return 0 // unreachable: runner.Run execs or returns an error
}

func runSecret(cmd command) int {
	kc := keychain.Keychain{}
	reg := registry.New(registryPath())
	switch cmd.secretOp {
	case opSet:
		value, err := readSecret(cmd)
		if err != nil {
			return fail(2, "%v", err)
		}
		if err := kc.Set(cmd.secretRef, value); err != nil {
			return fail(2, "%v", err)
		}
		if err := ensureDir(); err != nil {
			return fail(1, "%v", err)
		}
		if err := reg.Add(cmd.secretRef); err != nil {
			return fail(1, "%v", err)
		}
		fmt.Printf("stored %q\n", cmd.secretRef)
		return 0
	case opGet:
		value, err := kc.Get(cmd.secretRef)
		if errors.Is(err, keychain.ErrNotFound) {
			return fail(2, "%q not stored", cmd.secretRef)
		}
		if err != nil {
			return fail(2, "%v", err)
		}
		if cmd.reveal {
			fmt.Println(value)
		} else {
			fmt.Printf("%q is stored\n", cmd.secretRef)
		}
		return 0
	case opDelete:
		if !confirm(cmd.secretRef) {
			fmt.Println("aborted")
			return 0
		}
		if err := kc.Delete(cmd.secretRef); err != nil && !errors.Is(err, keychain.ErrNotFound) {
			return fail(2, "%v", err)
		}
		if err := reg.Remove(cmd.secretRef); err != nil {
			return fail(1, "%v", err)
		}
		fmt.Printf("deleted %q\n", cmd.secretRef)
		return 0
	case opExport:
		return runSecretExport(cmd, kc, reg)
	case opImport:
		return runSecretImport(cmd, kc, reg)
	case opList:
		refs, err := reg.Refs()
		if err != nil {
			return fail(1, "%v", err)
		}
		for _, ref := range refs {
			if _, err := kc.Get(ref); errors.Is(err, keychain.ErrNotFound) {
				fmt.Printf("%s (missing from keychain)\n", ref)
			} else if err != nil {
				fmt.Printf("%s (keychain error: %v)\n", ref, err)
			} else {
				fmt.Println(ref)
			}
		}
		return 0
	}
	return 2
}

// archivePath resolves the file argument of secret export/import to a concrete
// path: the explicit file, or the default archive name (issue 01/02).
func archivePath(file string) string {
	if file == "" {
		return defaultArchive
	}
	return file
}

// runSecretExport implements `secret export`. Refs-to-values orchestration and
// the archive encryption live in exportimport behind the injected seam
// (ADR-0005); this function owns the password prompt and the destination.
func runSecretExport(cmd command, kc keychain.Store, reg *registry.Registry) int {
	data, outcome, err := exportimport.Export(kc, reg, readExportPassword, Version)
	if err != nil {
		return fail(secretExitCode(err), "%v", err)
	}
	for _, ref := range outcome.Missing {
		fmt.Fprintf(os.Stderr, "kg: warning: %q missing from keychain; skipped\n", ref)
	}
	if cmd.secretFile == "-" {
		// stdout export: the archive is the only thing on stdout; prompts and
		// warnings stay on stderr (issue 01).
		if _, err := os.Stdout.Write(data); err != nil {
			return fail(1, "write archive to stdout: %v", err)
		}
	} else {
		path := archivePath(cmd.secretFile)
		if err := exportimport.WriteArchive(path, data); err != nil {
			return fail(1, "write archive: %v", err)
		}
		// A partial export (missing refs) reports only the stderr warnings above
		// and exits 2, so it is never announced as a complete archive (ADR-0005).
		if len(outcome.Missing) == 0 {
			fmt.Printf("exported %d secret%s to %s\n", outcome.Exported, plural(outcome.Exported), path)
		}
	}
	if len(outcome.Missing) > 0 {
		return 2 // a partial archive never passes for a complete one (ADR-0005)
	}
	return 0
}

// runSecretImport implements `secret import`. The archive is read first (from
// the file or stdin), then exportimport.Import decrypts and writes each ref.
// When the archive arrives on stdin, the password prompt reads from the
// controlling terminal — stdin is exhausted by the archive.
func runSecretImport(cmd command, kc keychain.Store, reg *registry.Registry) int {
	var data []byte
	var err error
	if cmd.secretFile == "-" {
		data, err = io.ReadAll(os.Stdin)
	} else {
		path := archivePath(cmd.secretFile)
		data, err = os.ReadFile(path)
	}
	if err != nil {
		return fail(1, "read archive: %v", err)
	}
	// The registry's config dir is ensured inside Import — after a successful
	// decrypt, before any keychain write — so a failed restore (wrong password)
	// creates no directory and no orphaned keychain item (ADR-0005).
	passwordFn := readImportPassword
	if cmd.secretFile == "-" {
		passwordFn = readImportPasswordTTY
	}
	outcome, err := exportimport.Import(kc, reg, passwordFn, data, cmd.skipExisting)
	if err != nil {
		return fail(secretExitCode(err), "%v", err)
	}
	if outcome.Skipped > 0 {
		fmt.Printf("imported %d secret%s (%d skipped)\n", outcome.Imported, plural(outcome.Imported), outcome.Skipped)
	} else {
		fmt.Printf("imported %d secret%s\n", outcome.Imported, plural(outcome.Imported))
	}
	return 0
}

// secretExitCode maps an export/import error to an exit code: a refs-registry
// failure is a file I/O error (exit 1, ADR-0001 §6); everything else — password,
// keychain backend, authentication — is exit 2.
func secretExitCode(err error) int {
	if errors.Is(err, exportimport.ErrRegistry) {
		return 1
	}
	return 2
}

// plural returns "" for 1 and "s" otherwise, so counts read naturally.
func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}

// readImportPassword prompts once for the import password on the terminal.
// Import prompts once (a wrong password simply fails GCM authentication, so a
// re-entry would only delay that failure).
func readImportPassword() (string, error) {
	if !term.IsTerminal(int(os.Stdin.Fd())) {
		return "", fmt.Errorf("stdin is not a terminal; run interactively to enter the import password")
	}
	return readOnce("Enter import password: ", int(os.Stdin.Fd()))
}

// readImportPasswordTTY prompts once for the import password from the
// controlling terminal, for when the archive itself arrives on stdin: stdin is
// exhausted by the archive bytes, so the prompt must read from /dev/tty.
func readImportPasswordTTY() (string, error) {
	tty, err := os.OpenFile("/dev/tty", os.O_RDWR, 0)
	if err != nil {
		return "", fmt.Errorf("cannot open controlling terminal for password prompt: %v", err)
	}
	defer tty.Close()
	return readOnce("Enter import password: ", int(tty.Fd()))
}

// readOnce reads a single value from the terminal without echo; the prompt goes
// to stderr.
func readOnce(prompt string, fd int) (string, error) {
	fmt.Fprint(os.Stderr, prompt)
	value, err := term.ReadPassword(fd)
	fmt.Fprintln(os.Stderr)
	if err != nil {
		return "", err
	}
	return string(value), nil
}

// readExportPassword prompts for the archive password twice on the terminal,
// verifying by re-entry, and returns it. Prompts go to stderr so a "-" (stdout)
// export never mixes them into the archive stream.
func readExportPassword() (string, error) {
	if !term.IsTerminal(int(os.Stdin.Fd())) {
		return "", fmt.Errorf("stdin is not a terminal; run interactively to enter the export password")
	}
	return readPair(
		"Enter export password: ",
		"Confirm password: ",
		errors.New("passwords do not match; aborting"),
	)
}

// readPair reads a value twice on the terminal, returning it when both entries
// match; mismatchErr is returned when they differ. The prompts go to stderr.
func readPair(prompt1, prompt2 string, mismatchErr error) (string, error) {
	v1, err := readOnce(prompt1, int(os.Stdin.Fd()))
	if err != nil {
		return "", err
	}
	v2, err := readOnce(prompt2, int(os.Stdin.Fd()))
	if err != nil {
		return "", err
	}
	if v1 != v2 {
		return "", mismatchErr
	}
	return v1, nil
}

func runCheck(cmd command) int {
	path := configPath()
	cfg, err := loadConfig(path)
	if err != nil {
		return fail(1, "%v", err)
	}
	warnPermissive(path)

	kc := keychain.Keychain{}
	ok := true
	cfgErr := false
	names := make([]string, 0, len(cfg.Profiles))
	for name := range cfg.Profiles {
		names = append(names, name)
	}
	sort.Strings(names)
	checkNames := names
	if len(cmd.checkTargets) > 0 {
		checkNames = cmd.checkTargets
		for _, name := range checkNames {
			if _, exists := cfg.Profiles[name]; !exists {
				return fail(1, "profile %q not found in %s", name, path)
			}
		}
	}
	if len(cmd.checkTargets) > 1 {
		// A combination validates its merged set once (ADR-0004). A merge
		// conflict is a configuration error (exit 1) that beats any keychain
		// condition (ADR-0001 §6); otherwise every ref of the merged set is
		// resolved, reported keyed by origin so a shared base is checked once.
		// Bare check never enters here: combinations are unbounded and are not
		// enumerated (ADR-0004).
		merged, origins, err := cfg.EffectiveSet(checkNames)
		if err != nil {
			fmt.Fprintf(os.Stderr, "%v\n", err)
			return 1
		}
		for varName, raw := range merged {
			ref, isRef := config.KeychainRef(raw)
			if !isRef {
				continue
			}
			if _, err := kc.Get(ref); errors.Is(err, keychain.ErrNotFound) {
				fmt.Fprintf(os.Stderr, "%s: %s -> %s MISSING\n", origins[varName], varName, raw)
				ok = false
			} else if err != nil {
				fmt.Fprintf(os.Stderr, "%s: %s -> %s error: %v\n", origins[varName], varName, raw, err)
				ok = false
			}
		}
		if ok {
			fmt.Println("all refs resolve")
			return 0
		}
		return 2 // missing keychain refs are a keychain condition (ADR-0001 §6)
	}
	for _, name := range checkNames {
		effective, err := cfg.Effective(name)
		if err != nil {
			// A broken reachable set is a configuration error (ADR-0003); its
			// refs cannot be resolved meaningfully, so skip to the next profile.
			fmt.Fprintf(os.Stderr, "%v\n", err)
			cfgErr = true
			continue
		}
		for varName, raw := range effective {
			ref, isRef := config.KeychainRef(raw)
			if !isRef {
				continue
			}
			if _, err := kc.Get(ref); errors.Is(err, keychain.ErrNotFound) {
				fmt.Fprintf(os.Stderr, "%s: %s -> %s MISSING\n", name, varName, raw)
				ok = false
			} else if err != nil {
				fmt.Fprintf(os.Stderr, "%s: %s -> %s error: %v\n", name, varName, raw, err)
				ok = false
			}
		}
	}
	if cfgErr {
		return 1 // configuration error (ADR-0001 §6) takes precedence over keychain
	}
	if ok {
		fmt.Println("all refs resolve")
		return 0
	}
	return 2 // missing keychain refs are a keychain condition (ADR-0001 §6)
}

func runInit(cmd command) int {
	shell := cmd.initShell
	if shell == "" {
		shell = detectShell()
	}
	if shell == "" {
		return fail(2, "cannot detect shell; pass --shell fish|zsh|bash")
	}
	// ShellScript validates the shell and reports an unsupported one.
	script, err := completion.ShellScript(shell)
	if err != nil {
		return fail(2, "%v", err)
	}
	// Write a starter config when none exists; an existing file is never
	// touched (config.EnsureFile guarantees this).
	cfgPath := configPath()
	if created, err := config.EnsureFile(cfgPath); err != nil {
		return fail(1, "%v", err)
	} else if created {
		fmt.Printf("created config at %s\n", cfgPath)
	}
	path, err := completion.InstallPath(shell)
	if err != nil {
		return fail(1, "%v", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fail(1, "%v", err)
	}
	if err := os.WriteFile(path, []byte(script), 0o644); err != nil {
		return fail(1, "%v", err)
	}
	fmt.Printf("installed %s completion at %s\n", shell, path)
	if shell == "zsh" {
		fmt.Fprintln(os.Stderr, "note: add ~/.zfunc to fpath and run compinit, e.g.")
		fmt.Fprintln(os.Stderr, `  printf 'fpath+=(~/.zfunc)\nautoload -Uz compinit\ncompinit\n' >> ~/.zshrc`)
	}
	probeKeychain()
	return 0
}

func runCompletion(cmd command) int {
	script, err := completion.ShellScript(cmd.completionShell)
	if err != nil {
		return fail(2, "%v", err)
	}
	fmt.Print(script)
	return 0
}

// runComplete is the __complete protocol: always exit 0, stderr silent,
// candidates or a directive on stdout (ADR-0002).
func runComplete(cmd command) int {
	var cfg *config.Config
	if data, err := os.ReadFile(configPath()); err == nil {
		if c, err := config.Parse(data); err == nil {
			cfg = c
		}
	}
	var refs []string
	if r, err := registry.New(registryPath()).Refs(); err == nil {
		refs = r
	}
	res := completion.Complete(cmd.completeWords, cfg, refs)
	// A directive, when present, is the first line; candidates (which may
	// accompany a directive) follow. Shells read the directive from the first
	// line and offer the remaining lines alongside the shell-native completion.
	if res.Directive != "" {
		fmt.Println(res.Directive)
	}
	for _, c := range res.Candidates {
		fmt.Println(c)
	}
	return 0
}

// detectShell identifies the current shell. Version variables are checked
// first (zsh and bash export them; fish exports none, so fish is detected via
// the parent process), then the parent process name, then $SHELL as a last
// resort — $SHELL alone is unreliable because it records the login shell.
func detectShell() string {
	switch {
	case os.Getenv("ZSH_VERSION") != "":
		return "zsh"
	case os.Getenv("BASH_VERSION") != "":
		return "bash"
	}
	switch name := parentProcessName(); {
	case strings.Contains(name, "fish"):
		return "fish"
	case strings.Contains(name, "zsh"):
		return "zsh"
	case strings.Contains(name, "bash"):
		return "bash"
	}
	if base := filepath.Base(os.Getenv("SHELL")); base != "" {
		return base
	}
	return ""
}

// parentProcessName returns the name of the process that spawned us (normally
// the interactive shell), or "" when it cannot be read.
func parentProcessName() string {
	out, err := exec.Command("ps", "-o", "comm=", "-p", strconv.Itoa(os.Getppid())).Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// probeKeychain performs a read-only keychain access so the macOS
// authorization dialog appears during setup rather than at first secret use.
// It stores nothing: the sentinel ref cannot exist (ADR-0002).
func probeKeychain() {
	kc := keychain.Keychain{}
	_, err := kc.Get("__keygrp_init__")
	switch {
	case errors.Is(err, keychain.ErrNotFound):
		fmt.Println("keychain: probe ok (nothing stored)")
	case err != nil:
		fmt.Fprintf(os.Stderr, "keychain: probe warning: %v\n", err)
	default:
		fmt.Println("keychain: probe ok")
	}
}

func readSecret(cmd command) (string, error) {
	if cmd.fromStdin {
		data, err := io.ReadAll(os.Stdin)
		if err != nil {
			return "", err
		}
		return strings.TrimRight(string(data), "\r\n"), nil
	}
	if !term.IsTerminal(int(os.Stdin.Fd())) {
		return "", fmt.Errorf("stdin is not a terminal; pass --stdin to read the value from stdin")
	}
	return readPair(
		fmt.Sprintf("Enter value for %q: ", cmd.secretRef),
		fmt.Sprintf("Confirm value for %q: ", cmd.secretRef),
		errors.New("values do not match; aborting"),
	)
}

// confirm asks for a y/N confirmation on stdin. EOF or any non-y answer is a no.
func confirm(ref string) bool {
	fmt.Fprintf(os.Stderr, "delete %q from keychain? [y/N] ", ref)
	line, err := bufio.NewReader(os.Stdin).ReadString('\n')
	if err != nil && line == "" {
		return false
	}
	ans := strings.ToLower(strings.TrimSpace(line))
	return ans == "y" || ans == "yes"
}

// fail prints a kg-prefixed message to stderr and returns the exit code.
func fail(code int, format string, args ...any) int {
	fmt.Fprintf(os.Stderr, "kg: "+format+"\n", args...)
	return code
}

func configPath() string {
	if p := os.Getenv("KEYGRP_CONFIG"); p != "" {
		return p
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ".keygrp.toml"
	}
	return filepath.Join(home, ".config", "keygrp", "config.toml")
}

func registryPath() string {
	return filepath.Join(filepath.Dir(configPath()), "refs")
}

// ensureDir creates the config directory (which holds the registry) as needed.
func ensureDir() error {
	return os.MkdirAll(filepath.Dir(registryPath()), 0o700)
}

func loadConfig(path string) (*config.Config, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, fmt.Errorf("config not found at %s (set $KEYGRP_CONFIG to override)", path)
	}
	if err != nil {
		return nil, err
	}
	return config.Parse(data)
}

// warnPermissive warns when the config file is group/world readable. The file
// holds no secrets, so this is hygiene, not a hard error.
func warnPermissive(path string) {
	info, err := os.Stat(path)
	if err != nil {
		return
	}
	if perm := info.Mode().Perm(); perm&0o077 != 0 {
		fmt.Fprintf(os.Stderr, "kg: warning: %s is group/world readable (%v); consider chmod 600\n", path, perm)
	}
}
