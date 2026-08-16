package config

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/pelletier/go-toml/v2"
)

// KeychainRef reports whether v is a keychain://<ref> reference and, if so,
// returns the ref. A ref must be non-empty; an empty or differently-schemed
// value is not a keychain reference.
func KeychainRef(v string) (string, bool) {
	const prefix = "keychain://"
	if len(v) > len(prefix) && strings.HasPrefix(v, prefix) {
		return v[len(prefix):], true
	}
	return "", false
}

// Config is the parsed keygrp configuration file.
type Config struct {
	// Profiles maps a profile name to its environment variable group.
	Profiles map[string]Profile
}

// Profile is one environment variable group. Extends names the base profiles
// whose variables are merged into this profile's effective variable set
// (ADR-0003); Vars maps an environment variable name to its value, which is
// either a keychain://<ref> reference or a plaintext non-secret value. Origins
// maps each effective variable to the profile that declared it; it is computed
// at resolution time, never parsed from config, and used for --verbose
// provenance (ADR-0004).
type Profile struct {
	Extends []string
	Vars    map[string]string
	Origins map[string]string
}

// DefaultFile is the starter configuration written by `kg init` when no
// config file exists yet. It is comments only: a fresh install must parse
// cleanly yet define nothing, so `kg check` reports success immediately.
const DefaultFile = `# keygrp configuration - created by kg init.
#
# Each [profiles.<name>] table is a group of environment variables injected
# into a target program with:
#
#     kg run <name> <program> [args...]
#
# A value is either a keychain reference or plaintext:
#
#     KEY = "keychain://<ref>"   resolved from the OS keychain at run time;
#                                store the secret first with
#                                'kg secret set <ref>'
#     KEY = "value"              plaintext, injected directly
#
# A profile may inherit another profile's variables:
#
#     [profiles.<name>]
#     extends = "<base-profile>"   merge the base profile's variables in
#
# Several profiles may be combined for a single run, no config change needed:
#
#     kg run <name-a>,<name-b> <program>   merge both profiles' variables
#     (or: kgx <name-a>,<name-b> <program>)
#
# This file holds references, never secret values. kg wrote this starter
# because no config existed and never edits it again; keep it hand-edited and
# git-trackable.

# Example profile - uncomment and edit to match your setup:
# [profiles.claude]
# ANTHROPIC_API_KEY = "keychain://anthropic-api-key"
`

// EnsureFile writes DefaultFile to path when no file exists there yet, creating
// parent directories as needed. It returns whether it created the file. An
// existing file is never touched: keygrp never edits user config (ADR-0001). A
// path that is an existing directory is an error, so init fails loudly instead
// of reporting success over an unusable config.
func EnsureFile(path string) (bool, error) {
	if info, err := os.Stat(path); err == nil {
		if info.IsDir() {
			return false, fmt.Errorf("config path %s is a directory", path)
		}
		return false, nil
	} else if !errors.Is(err, fs.ErrNotExist) {
		return false, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return false, err
	}
	// O_EXCL folds the existence check into the create, so a file appearing
	// between the Stat above and here is never clobbered.
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		if errors.Is(err, fs.ErrExist) {
			return false, nil // created concurrently; leave it alone
		}
		return false, err
	}
	if _, err := f.Write([]byte(DefaultFile)); err != nil {
		_ = f.Close()
		return false, err
	}
	if err := f.Close(); err != nil {
		return false, err
	}
	return true, nil
}

// extendsKey is the reserved key declaring a profile's base profiles
// (ADR-0003). It is consumed at parse time and never injected as a variable.
const extendsKey = "extends"

// Parse decodes keygrp configuration from TOML. Unknown top-level tables are
// ignored; variables must be strings; the reserved `extends` key accepts a
// profile name or an array of names.
func Parse(data []byte) (*Config, error) {
	var doc struct {
		Profiles map[string]map[string]any `toml:"profiles"`
	}
	if err := toml.Unmarshal(data, &doc); err != nil {
		return nil, err
	}

	cfg := &Config{Profiles: map[string]Profile{}}
	for name, raw := range doc.Profiles {
		if strings.Contains(name, ",") {
			return nil, fmt.Errorf("profile %q: name contains a comma; commas are reserved as the combination separator (ADR-0004)", name)
		}
		p := Profile{Vars: map[string]string{}}
		for key, val := range raw {
			if key == extendsKey {
				ext, err := parseExtends(val)
				if err != nil {
					return nil, fmt.Errorf("profile %s: %w", name, err)
				}
				p.Extends = ext
				continue
			}
			s, ok := val.(string)
			if !ok {
				return nil, fmt.Errorf("profile %s: variable %q is %T, want string", name, key, val)
			}
			p.Vars[key] = s
		}
		cfg.Profiles[name] = p
	}
	return cfg, nil
}

// parseExtends validates an extends value: a single profile name or an array
// of names. Names must be non-empty, duplicates are dropped, and an empty
// array is equivalent to no extends.
func parseExtends(v any) ([]string, error) {
	switch t := v.(type) {
	case string:
		if err := checkExtendsName(t); err != nil {
			return nil, err
		}
		return []string{t}, nil
	case []any:
		seen := map[string]bool{}
		var out []string
		for _, e := range t {
			s, ok := e.(string)
			if !ok {
				return nil, fmt.Errorf("extends name %v is %T, want string", e, e)
			}
			if err := checkExtendsName(s); err != nil {
				return nil, err
			}
			if !seen[s] {
				out = append(out, s)
				seen[s] = true
			}
		}
		if len(out) == 0 {
			return nil, nil
		}
		return out, nil
	default:
		return nil, fmt.Errorf("extends is %T, want a profile name or array of names", v)
	}
}

// checkExtendsName rejects an empty profile name, or one containing a comma,
// in an extends value. The comma is reserved as the combination separator
// (ADR-0004), so a comma in an extends value can never name a real profile.
func checkExtendsName(s string) error {
	if s == "" {
		return fmt.Errorf("extends name is empty")
	}
	if strings.Contains(s, ",") {
		return fmt.Errorf("extends name %q contains a comma; extends takes profile names, never combinations (ADR-0004)", s)
	}
	return nil
}

// Effective returns the effective variable set for profile name: its own
// variables merged with every reachable base's — the transitive closure of
// extends, deduplicated by profile (ADR-0003). It returns an error for an
// unknown profile, a missing base, an extends cycle, or a no-shadowing
// conflict.
func (c *Config) Effective(name string) (map[string]string, error) {
	env, _, err := c.EffectiveSet([]string{name})
	return env, err
}

// EffectiveSet returns the effective variable set for a combination: each
// comma-separated member resolved independently through its own extends chain,
// then merged under the same no-shadowing rule as extends (ADR-0004). The
// second return value maps every effective variable to the profile that
// declared it (a shared base counts once, since it has a single origin). It
// returns an error for an unknown member, a missing base, an extends cycle, or
// a no-shadowing conflict between two distinct origins.
func (c *Config) EffectiveSet(names []string) (map[string]string, map[string]string, error) {
	if len(names) == 1 {
		r, err := c.effective(names[0], map[string]bool{})
		if err != nil {
			return nil, nil, err
		}
		return r.env, r.origin, nil
	}
	r := &resolved{env: map[string]string{}, origin: map[string]string{}}
	for _, name := range names {
		mr, err := c.effective(name, map[string]bool{})
		if err != nil {
			return nil, nil, fmt.Errorf("combination %q: %w", name, err)
		}
		if err := mergeResolved(r, mr, fmt.Sprintf("combination %q", name)); err != nil {
			return nil, nil, err
		}
	}
	return r.env, r.origin, nil
}

// resolved is a profile's effective variable set together with, for each
// variable, the profile that declared it — so a diamond re-reaching the same
// declaration is told apart from a genuine no-shadowing conflict.
type resolved struct {
	env    map[string]string
	origin map[string]string
}

// effective resolves name's effective variable set. chain tracks the profiles
// on the current extends chain so a cycle is detected before recursing
// forever. Merging visits variables in sorted order so a reported conflict is
// deterministic across runs.
func (c *Config) effective(name string, chain map[string]bool) (*resolved, error) {
	p, ok := c.Profiles[name]
	if !ok {
		return nil, fmt.Errorf("unknown profile %q", name)
	}
	if chain[name] {
		return nil, fmt.Errorf("extends cycle at %q", name)
	}
	chain[name] = true
	defer delete(chain, name)

	r := &resolved{env: map[string]string{}, origin: map[string]string{}}
	for varName, raw := range p.Vars {
		r.env[varName] = raw
		r.origin[varName] = name
	}
	for _, base := range p.Extends {
		br, err := c.effective(base, chain)
		if err != nil {
			return nil, fmt.Errorf("profile %q: %w", name, err)
		}
		if err := mergeResolved(r, br, fmt.Sprintf("profile %q", name)); err != nil {
			return nil, err
		}
	}
	return r, nil
}

// mergeResolved merges from into r under the no-shadowing rule: two
// declarations of the same variable name from distinct origins are a conflict.
// ctx prefixes the error message (the profile or combination member being
// merged). Variables are visited in sorted order so a reported conflict is
// deterministic across runs.
func mergeResolved(r, from *resolved, ctx string) error {
	for _, varName := range sortedKeys(from.env) {
		if o, present := r.origin[varName]; present && o != from.origin[varName] {
			return fmt.Errorf("%s: no-shadowing conflict on %q (declared in %q and %q)", ctx, varName, o, from.origin[varName])
		}
		r.env[varName] = from.env[varName]
		r.origin[varName] = from.origin[varName]
	}
	return nil
}

// sortedKeys returns the keys of m in sorted order.
func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	return keys
}
