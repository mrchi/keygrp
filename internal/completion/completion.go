// Package completion implements keygrp's shell tab completion: the
// __complete protocol brain and the per-shell script generator. See
// docs/adr/0002-shell-completion-and-init.md.
package completion

import (
	"slices"
	"sort"
	"strings"

	"github.com/mrchi/keygrp/internal/config"
)

// Directives told to the shell scripts. The scripts are thin executors:
// command-name completion, target-argument delegation, and file-path
// completion are shell-native. Values beginning with "__directive:" are
// reserved and never emitted as candidates.
const (
	DirectiveCommand  = "__directive:command"
	DirectiveDelegate = "__directive:delegate"
	DirectiveFile     = "__directive:file"
)

// Result is the outcome of completing a command line: a delegation directive
// (shell-native completion) and/or candidate values (keygrp-owned). A
// directive may carry extra Candidates for the shell to offer alongside the
// shell-native completion (e.g. import's --skip-existing next to a file
// position).
type Result struct {
	Directive  string // DirectiveCommand | DirectiveDelegate | DirectiveFile when non-empty
	Candidates []string
}

// Complete resolves completion candidates (or a delegation directive) for the
// current command line. words includes the partial token being completed as
// its last element. A leading command name (as shells pass in) selects the
// grammar: "kgx" is the run shorthand, "kg" (or the legacy "keygrp") the full
// surface; direct callers may omit the command name and are treated as kg. cfg
// and refs may be nil (missing config / registry); completion degrades
// gracefully to static candidates.
func Complete(words []string, cfg *config.Config, refs []string) Result {
	if len(words) > 0 {
		switch words[0] {
		case "kgx":
			return completeKGX(words[1:], cfg)
		case "kg":
			return completeKG(words[1:], cfg, refs)
		}
	}
	return completeKG(words, cfg, refs)
}

// completeKG completes a `kg` command line: a leading verb, then verb-specific
// arguments. The run verb takes the combination + program; the management
// verbs take their own shapes. An unknown first token completes nothing.
func completeKG(words []string, cfg *config.Config, refs []string) Result {
	position := len(words) - 1
	if position < 0 {
		return Result{Candidates: candidates(frontCandidatesKG(), "", "")}
	}
	prefix := words[position]
	toks := words[:position]

	if len(toks) == 0 {
		return Result{Candidates: candidates(frontCandidatesKG(), "", prefix)}
	}
	switch toks[0] {
	case "run":
		return runResult(toks[1:], cfg, prefix)
	case "secret":
		if len(toks) == 1 {
			return Result{Candidates: candidates([]string{"set", "get", "delete", "list", "export", "import"}, "", prefix)}
		}
		return secretResult(toks, prefix, refs)
	case "check":
		if len(toks) == 1 {
			return Result{Candidates: candidates([]string{"--profile"}, "", prefix)}
		}
		if len(toks) == 2 && toks[1] == "--profile" {
			return Result{Candidates: candidates(profileNamesWithCombo(cfg, prefix), profileLabel, prefix)}
		}
		return Result{}
	case "init":
		if len(toks) == 1 {
			return Result{Candidates: candidates([]string{"--shell"}, "", prefix)}
		}
		if len(toks) == 2 && toks[1] == "--shell" {
			return Result{Candidates: candidates(validShells(), "", prefix)}
		}
		return Result{}
	case "completion":
		return Result{Candidates: candidates(validShells(), "", prefix)}
	}
	return Result{} // unknown verb, or a filled position
}

// completeKGX completes a `kgx` command line: the combination is the whole
// first slot, so the line is exactly what `kg run` sees after its verb —
// [--verbose], the combination, command-name completion, then delegation to
// the injected program. Leading --verbose tokens are consumed by runResult.
func completeKGX(words []string, cfg *config.Config) Result {
	if len(words) == 0 {
		return Result{Candidates: candidates(runPositionCandidates(cfg, "", true), profileLabel, "")}
	}
	position := len(words) - 1
	prefix := words[position]
	return runResult(words[:position], cfg, prefix)
}

// runResult completes below `kg run`: [--verbose], then the combination
// position, then command-name completion and delegation to the injected
// program. rest are the tokens already typed after `run`, before the partial
// token being completed. Leading --verbose tokens are consumed here (mirroring
// the cli parser's stripVerboseFlag), and once consumed the flag is not
// re-offered at the combination position.
func runResult(rest []string, cfg *config.Config, prefix string) Result {
	verboseGiven := false
	for len(rest) > 0 && rest[0] == "--verbose" {
		verboseGiven = true
		rest = rest[1:]
	}
	switch len(rest) {
	case 0:
		return Result{Candidates: candidates(runPositionCandidates(cfg, prefix, !verboseGiven), profileLabel, prefix)}
	case 1:
		return Result{Directive: DirectiveCommand}
	default:
		return Result{Directive: DirectiveDelegate}
	}
}

// runPositionCandidates renders the candidates for kg run / kgx's combination
// position: an unused --verbose flag first, then profile names — or a partial
// combination's tail. Once --verbose is given it is not re-offered, matching
// how secret import consumes --skip-existing.
func runPositionCandidates(cfg *config.Config, prefix string, verboseAvailable bool) []string {
	if !verboseAvailable {
		return profileNamesWithCombo(cfg, prefix)
	}
	return append([]string{"--verbose"}, profileNamesWithCombo(cfg, prefix)...)
}

// profileNames returns sorted profile names from the config, excluding any
// that collide with the reserved "__directive:" protocol namespace.
func profileNames(cfg *config.Config) []string {
	if cfg == nil {
		return nil
	}
	names := make([]string, 0, len(cfg.Profiles))
	for name := range cfg.Profiles {
		if !isReserved(name) {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	return names
}

// frontCandidatesKG are what `kg <TAB>` offers: the verbs and --help. Profile
// names appear only below `run` (and are the whole grammar of kgx's first
// slot), never mixed into the verb position (ADR-0007).
func frontCandidatesKG() []string {
	return []string{"run", "secret", "check", "init", "completion", "--help"}
}

// profileNamesWithCombo returns profile names for a position that may hold a
// combination (check --profile), completing the tail of a partial token
// whole-word when it already contains a comma.
func profileNamesWithCombo(cfg *config.Config, prefix string) []string {
	if i := strings.LastIndex(prefix, ","); i >= 0 {
		return combinationCandidates(cfg, prefix, i)
	}
	return profileNames(cfg)
}

// combinationCandidates completes the tail of a partial combination token: the
// already-chosen prefix (everything before the last comma) is preserved and
// prepended to each remaining profile name, so the shell replaces the whole
// word in one shot. Already-chosen members are excluded (ADR-0004).
func combinationCandidates(cfg *config.Config, prefix string, lastComma int) []string {
	chosen := strings.Split(prefix[:lastComma], ",")
	kept := prefix[:lastComma+1]
	names := profileNames(cfg)
	out := make([]string, 0, len(names))
	for _, n := range names {
		if slices.Contains(chosen, n) {
			continue
		}
		out = append(out, kept+n)
	}
	return out
}

// secretResult completes below the secret subcommand. toks[0] is "secret".
// The export/import file position delegates to shell-native path completion
// (DirectiveFile); import's first position also offers --skip-existing.
func secretResult(toks []string, prefix string, refs []string) Result {
	op := toks[1]
	after := toks[2:] // already-typed tokens after the op
	switch op {
	case "get":
		if len(after) == 0 {
			return Result{Candidates: candidates(append([]string{"--reveal"}, safeRefs(refs)...), secretRefLabel, prefix)}
		}
		if len(after) == 1 && after[0] == "--reveal" {
			return Result{Candidates: candidates(safeRefs(refs), secretRefLabel, prefix)}
		}
	case "set":
		if len(after) == 0 {
			return Result{Candidates: candidates([]string{"--stdin"}, "", prefix)}
		}
	case "delete":
		if len(after) == 0 {
			return Result{Candidates: candidates(safeRefs(refs), secretRefLabel, prefix)}
		}
	case "export":
		if len(after) == 0 {
			return Result{Directive: DirectiveFile}
		}
	case "import":
		switch len(after) {
		case 0:
			// first position: the archive file, or --skip-existing
			return Result{Directive: DirectiveFile, Candidates: candidates([]string{"--skip-existing"}, "", prefix)}
		case 1:
			if after[0] == "--skip-existing" {
				return Result{Directive: DirectiveFile}
			}
		}
	}
	return Result{} // list, or a ref/flag/file position already filled: nothing more to complete
}

// safeRefs drops refs that collide with the reserved "__directive:" protocol
// namespace.
func safeRefs(refs []string) []string {
	out := make([]string, 0, len(refs))
	for _, r := range refs {
		if !isReserved(r) {
			out = append(out, r)
		}
	}
	return out
}

// isReserved reports whether a value collides with the __complete protocol's
// directive namespace and must never be emitted as a candidate.
func isReserved(s string) bool {
	return strings.HasPrefix(s, "__directive:")
}

// candidateDescriptions is the single source of truth for candidate
// descriptions (ADR-0006). The wording mirrors internal/cli's usageText so help and
// completion never drift apart; `secret` and `--reveal` have no one-line
// summary in usage, so their descriptions are authored here.
var candidateDescriptions = map[string]string{
	"run":             "run a program with a combination's env",
	"secret":          "manage secrets in the OS keychain",
	"check":           "validate config and keychain refs",
	"init":            "install completion, create config & authorize keychain",
	"completion":      "print a completion script",
	"--help":          "show this help; any verb accepts --help",
	"--verbose":       "prints injected var names with their origin profile",
	"set":             "store a secret in the keychain",
	"get":             "show whether a secret exists",
	"delete":          "remove a secret",
	"list":            "list stored secret refs",
	"export":          "export secrets to a password-encrypted archive",
	"import":          "restore secrets from an archive",
	"--reveal":        "print the secret value",
	"--stdin":         "read the secret from stdin",
	"--skip-existing": "skip refs that already exist",
	"--profile":       "combination to validate",
	"--shell":         "shell to install",
}

// Dynamic candidate values (profile names, secret refs) carry a generic label
// so they read as a distinct class from the static words around them
// (ADR-0006).
const (
	profileLabel   = "profile"
	secretRefLabel = "secret ref"
)

// candidateLine renders one candidate for the __complete protocol: "value", or
// "value<TAB>description" when there is one. A static word takes its
// description from candidateDescriptions; a dynamic value (profile name, secret
// ref) falls back to label; a value with neither stays bare. The tab is an
// optional delimiter, so a parser that splits on the first tab reads both old
// (bare) and new output (ADR-0006).
func candidateLine(value, label string) string {
	if d, ok := candidateDescriptions[value]; ok {
		return value + "\t" + d
	}
	if label != "" {
		return value + "\t" + label
	}
	return value
}

// candidates renders bare candidate values as protocol lines and keeps only
// those whose value part begins with prefix.
func candidates(values []string, label, prefix string) []string {
	lines := make([]string, 0, len(values))
	for _, v := range values {
		lines = append(lines, candidateLine(v, label))
	}
	return filter(lines, prefix)
}

// filter keeps candidates whose value part begins with prefix. A candidate
// line may be "value" or "value<TAB>description"; the description never
// participates in the match (ADR-0006).
func filter(cands []string, prefix string) []string {
	if len(cands) == 0 {
		return nil
	}
	out := make([]string, 0, len(cands))
	for _, c := range cands {
		if strings.HasPrefix(valuePart(c), prefix) {
			out = append(out, c)
		}
	}
	return out
}

// valuePart returns the value portion of a candidate line — everything before
// the first tab, or the whole line when there is no tab (ADR-0006).
func valuePart(line string) string {
	value, _, _ := strings.Cut(line, "\t")
	return value
}

// validShells are the shells keygrp can generate completion for.
func validShells() []string {
	return []string{"fish", "zsh", "bash"}
}
