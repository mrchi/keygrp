package completion

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
)

// ShellScript returns the completion script for the given shell
// (fish, zsh, or bash). The scripts are thin executors of the __complete
// protocol: front-part candidates come from keygrp, while command-name
// completion and target-argument delegation are shell-native.
func ShellScript(shell string) (string, error) {
	switch shell {
	case "fish":
		return fishScript, nil
	case "zsh":
		return zshScript, nil
	case "bash":
		return bashScript, nil
	}
	return "", unknownShellError(shell)
}

// unknownShellError reports an unsupported shell argument.
func unknownShellError(shell string) error {
	return fmt.Errorf("unknown shell %q (use fish, zsh, or bash)", shell)
}

// InstallPath returns where `kg init` writes the completion script for the
// given shell. zsh requires an fpath + compinit step afterwards (see ADR-0002).
func InstallPath(shell string) (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	switch shell {
	case "fish":
		base := os.Getenv("XDG_CONFIG_HOME")
		if base == "" {
			base = filepath.Join(home, ".config")
		}
		return filepath.Join(base, "fish", "completions", "kg.fish"), nil
	case "zsh":
		return filepath.Join(home, ".zfunc", "_kg"), nil
	case "bash":
		return filepath.Join(home, ".local", "share", "bash-completion", "completions", "kg"), nil
	}
	return "", unknownShellError(shell)
}

// InstallFile is one file `kg init` writes for a shell's completion.
type InstallFile struct {
	Path    string // absolute install path
	Content string // file contents
}

// InstallFiles returns every file `kg init` must write for the given shell.
// The primary script registers completion for both kg and kgx, but shell
// autoloaders load completion files by command name, so companion autoload
// files (kgx.fish for fish, kgx for bash) that source the primary are
// required for kgx completion to load (ADR-0002). zsh needs no companion: its
// #compdef line registers both commands in compinit's index.
func InstallFiles(shell string) ([]InstallFile, error) {
	path, err := InstallPath(shell)
	if err != nil {
		return nil, err
	}
	script, err := ShellScript(shell)
	if err != nil {
		return nil, err
	}
	files := []InstallFile{{Path: path, Content: script}}
	switch shell {
	case "fish":
		files = append(files, InstallFile{
			Path:    filepath.Join(filepath.Dir(path), "kgx.fish"),
			Content: fishCompanion,
		})
	case "bash":
		files = append(files, InstallFile{
			Path:    filepath.Join(filepath.Dir(path), "kgx"),
			Content: bashCompanion,
		})
	}
	return files, nil
}

// ValidShell reports whether shell is a supported completion target.
func ValidShell(shell string) bool {
	return slices.Contains(validShells(), shell)
}

const fishScript = `# kg completion for fish
# commandline -opc excludes a non-empty current token (a partially typed word),
# so commandline -ct supplies it as the protocol's last element; when the
# current token is empty (cursor after a trailing space) -opc already carries
# the trailing "" and -ct's empty output is harmlessly dropped. Do not append
# an explicit "" to -ct: that would double the empty token and misroute the
# completion.
function __kg_proto -d 'kg completion protocol for $argv[1]'
    set -l cmd $argv[1]
    set -l full (commandline -opc) (commandline -ct)
    set -l out ($cmd __complete -- $full)
    if test -z "$out"
        return
    end
    switch $out[1]
        case '__directive:command'
            __fish_complete_command
        case '__directive:delegate'
            # drop the command name, the run verb (kg only), an optional
            # --verbose, and the combination, then complete as the injected
            # program
            set -l drop 2
            if test (count $full) -ge 2; and test $full[2] = run
                set drop 3
            end
            if test (count $full) -ge $drop; and test $full[$drop] = --verbose
                set drop (math $drop + 1)
            end
            if test (count $full) -ge (math $drop + 1)
                set -e full[1..$drop]
                complete -C (string join ' ' (string escape -- $full))
            end
        case '__directive:file'
            # complete the current token as a file path; extra candidates after
            # the directive (e.g. import's --skip-existing) come first
            if test (count $out) -ge 2
                printf '%s\n' $out[2..]
            end
            __fish_complete_path (commandline -ct)
        case '*'
            printf '%s\n' $out
    end
end

function __kg_complete -d 'kg tab completion'
    __kg_proto kg
end

function __kgx_complete -d 'kgx tab completion'
    __kg_proto kgx
end

complete -c kg -f -a '(__kg_complete)'
complete -c kgx -f -a '(__kgx_complete)'
`

const zshScript = `#compdef kg kgx

# __kg_compadd offers candidates, splitting each "value<TAB>description"
# line (ADR-0006) so the description displays beside the value; a bare "value"
# line simply has none.
__kg_compadd() {
    local -a vals descs
    local line
    for line in "$@"; do
        if [[ $line == *$'\t'* ]]; then
            vals+=("${line%%$'\t'*}")
            descs+=("${line#*$'\t'}")
        else
            vals+=("$line")
            descs+=("")
        fi
    done
    compadd -a vals -d descs
}

_kg() {
    local -a out
    # "${words[@]}" preserves a trailing empty current token (cursor after a
    # space); unquoted $words would drop it, misrouting empty-token positions
    # like "kg secret export <TAB>".
    out=("${(@f)$($service __complete -- "${words[@]}")}")
    if (( ${#out} )); then
        case "$out[1]" in
            '__directive:command')
                _command_names
                ;;
            '__directive:delegate')
                # "$service" is the command being completed (kg or kgx). Drop
                # the command name, the run verb (kg only), an optional
                # --verbose, and the combination, then complete as the injected
                # program — the same shift-and-_normal idiom zsh's own _sudo
                # uses for "sudo user cmd".
                local -i drop=2
                [[ "$service" != "kgx" ]] && [[ "${words[2]}" == "run" ]] && drop=3
                [[ "${words[$drop]}" == "--verbose" ]] && (( drop++ ))
                shift words $drop
                (( CURRENT -= drop ))
                _normal
                ;;
            '__directive:file')
                # complete the current word as a file path; extra candidates
                # after the directive (e.g. import's --skip-existing) are
                # offered alongside, filtered by the current prefix by compadd.
                _files
                if (( ${#out} > 1 )); then
                    __kg_compadd "${out[2,-1]}"
                fi
                ;;
            *)
                __kg_compadd "${out[@]}"
                ;;
        esac
    fi
}

_kg "$@"
`

const bashScript = `# kg completion for bash
_kg() {
    local cur line out=() cmd
    cur="${COMP_WORDS[COMP_CWORD]}"
    # COMP_WORDS[0] is the command being completed; it selects the binary (kg
    # or kgx) that answers the __complete protocol.
    cmd="${COMP_WORDS[0]}"
    # Read candidates one line at a time: no word-splitting or glob expansion.
    while IFS= read -r line; do
        out+=("$line")
    done < <($cmd __complete -- "${COMP_WORDS[@]}")
    if [[ ${#out[@]} -eq 0 ]]; then
        COMPREPLY=()
        return
    fi
    case "${out[0]}" in
        __directive:command)
            COMPREPLY=($(compgen -c -- "$cur"))
            ;;
        __directive:delegate)
            if declare -F _command_offset >/dev/null 2>&1; then
                # offset = command name + (kg: run verb) + optional --verbose +
                # the combination; COMP_WORDS[1] is the run verb for kg.
                local idx=1
                if [[ "$cmd" != "kgx" ]] && [[ "${COMP_WORDS[1]}" == "run" ]]; then
                    idx=2
                fi
                if [[ "${COMP_WORDS[$idx]}" == "--verbose" ]]; then
                    (( idx++ ))
                fi
                _command_offset $(( idx + 1 ))
            else
                COMPREPLY=()
            fi
            ;;
        __directive:file)
            # Read compgen output line-by-line like the main loop, so a
            # filename containing spaces stays one COMPREPLY element.
            COMPREPLY=()
            local p
            while IFS= read -r p; do
                COMPREPLY+=("$p")
            done < <(compgen -f -- "$cur")
            # Extra candidates may carry value<TAB>description lines (ADR-0006);
            # match the current prefix against the value part only, and keep the
            # full line in COMPREPLY so readline shows the description.
            local c val
            for c in "${out[@]:1}"; do
                val="${c%%$'\t'*}"
                if [[ $val == "$cur"* ]]; then
                    COMPREPLY+=("$c")
                fi
            done
            ;;
        *)
            local c val
            for c in "${out[@]}"; do
                val="${c%%$'\t'*}"
                if [[ $val == "$cur"* ]]; then
                    COMPREPLY+=("$c")
                fi
            done
            ;;
    esac
}

complete -o bashdefault -o default -F _kg kg
complete -o bashdefault -o default -F _kg kgx
`

// fishCompanion is the kgx autoload file for fish. fish loads completion
// files by command name, so this companion sources kg.fish, which registers
// completion for both kg and kgx.
const fishCompanion = `# kgx completion for fish
# fish loads completion files by command name, so this companion file makes
# completing kgx load the shared script at kg.fish, which registers
# completion for both kg and kgx.
source (dirname (status filename))/kg.fish
`

// bashCompanion is the kgx autoload file for bash. bash-completion loads
// files by command name, so this companion sources kg, which registers
// completion for both kg and kgx.
const bashCompanion = `# kgx completion for bash
# bash-completion loads files by command name, so this companion file makes
# completing kgx load the shared script at kg, which registers completion
# for both kg and kgx.
source "${BASH_SOURCE[0]%/*}/kg"
`
