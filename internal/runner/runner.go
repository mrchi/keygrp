package runner

import (
	"fmt"
	"maps"
	"os"
	"os/exec"
	"sort"
	"strings"
	"syscall"

	"github.com/mrchi/keygrp/internal/config"
	"github.com/mrchi/keygrp/internal/keychain"
)

// Resolve builds the final environment for a profile: the inherited
// environment plus each variable from the profile. Keychain references are
// resolved via store; a missing item is a fatal error. Profile values
// override inherited values unconditionally (ADR-0001 §4).
func Resolve(p config.Profile, inherit map[string]string, store keychain.Store) (map[string]string, error) {
	env := make(map[string]string, len(inherit)+len(p.Vars))
	maps.Copy(env, inherit)
	for name, raw := range p.Vars {
		if ref, ok := config.KeychainRef(raw); ok {
			value, err := store.Get(ref)
			if err != nil {
				return nil, fmt.Errorf("resolve var %s: %w", name, err)
			}
			env[name] = value
		} else {
			env[name] = raw
		}
	}
	return env, nil
}

// Run replaces the current process image with program (ADR-0001 §1), so the
// target inherits our PID, TTY, signals, and exit code. It never returns on
// success; it returns an error only when lookup or resolution fails first.
// The program is resolved before any keychain access, so a missing program
// never triggers a keychain prompt (spec: Run ordering). If verbose is true,
// injected variable names are printed to stderr before handoff; values never
// are. This is the deliberately untested boundary: exec replaces the process.
func Run(p config.Profile, program string, args []string, store keychain.Store, verbose bool) error {
	path, err := exec.LookPath(program)
	if err != nil {
		return fmt.Errorf("find program %q: %w", program, err)
	}
	env, err := Resolve(p, environMap(), store)
	if err != nil {
		return err
	}
	if verbose {
		for _, line := range verboseLines(p.Vars, p.Origins) {
			fmt.Fprintln(os.Stderr, line)
		}
	}
	return syscall.Exec(path, append([]string{program}, args...), envSlice(env))
}

// verboseLines renders the injected variable names for verbose mode, sorted,
// each annotated with the profile that declared it when known. Values are never
// printed (ADR-0001 §4). The origin annotation makes a combination's
// provenance visible (ADR-0004).
func verboseLines(vars, origins map[string]string) []string {
	names := make([]string, 0, len(vars))
	for name := range vars {
		names = append(names, name)
	}
	sort.Strings(names)
	out := make([]string, 0, len(names))
	for _, name := range names {
		if origin, ok := origins[name]; ok {
			out = append(out, fmt.Sprintf("keygrp: injecting %s (from %s)", name, origin))
		} else {
			out = append(out, fmt.Sprintf("keygrp: injecting %s", name))
		}
	}
	return out
}

// environMap converts the inherited environment to a map.
func environMap() map[string]string {
	env := make(map[string]string)
	for _, kv := range os.Environ() {
		if k, v, ok := strings.Cut(kv, "="); ok {
			env[k] = v
		}
	}
	return env
}

// envSlice converts a resolved environment map back to the KEY=VALUE slice
// form expected by exec.
func envSlice(env map[string]string) []string {
	out := make([]string, 0, len(env))
	for k, v := range env {
		out = append(out, k+"="+v)
	}
	return out
}
