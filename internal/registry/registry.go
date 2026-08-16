package registry

import (
	"bufio"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
)

// Registry is a file-backed set of keychain refs, co-located with the config
// file. It exists because the keychain backend cannot enumerate items
// (go-keyring has no List); it records which refs keygrp has written, so
// `secret list` can report them.
type Registry struct {
	path string
}

// New returns a Registry persisted at path.
func New(path string) *Registry {
	return &Registry{path: path}
}

// EnsureDir creates the registry's containing directory as needed. The registry
// co-locates with the config dir, which may not exist yet on a fresh machine
// (secret import's primary case); EnsureDir is called before a registry write
// so a write can never fail for lack of its parent.
func (r *Registry) EnsureDir() error {
	return os.MkdirAll(filepath.Dir(r.path), 0o700)
}

// Refs returns the tracked refs, sorted and deduplicated. A missing file is
// the empty set.
func (r *Registry) Refs() ([]string, error) {
	f, err := os.Open(r.path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()

	seen := map[string]bool{}
	var refs []string
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		ref := strings.TrimSpace(sc.Text())
		if ref != "" && !seen[ref] {
			refs = append(refs, ref)
			seen[ref] = true
		}
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	sort.Strings(refs)
	return refs, nil
}

// Add records ref if not already tracked.
func (r *Registry) Add(ref string) error {
	refs, err := r.Refs()
	if err != nil {
		return fmt.Errorf("registry add %q: %w", ref, err)
	}
	if slices.Contains(refs, ref) {
		return nil
	}
	return r.write(append(refs, ref))
}

// Remove drops ref from the registry. Removing an untracked ref is a no-op.
func (r *Registry) Remove(ref string) error {
	refs, err := r.Refs()
	if err != nil {
		return fmt.Errorf("registry remove %q: %w", ref, err)
	}
	out := refs[:0]
	for _, existing := range refs {
		if existing != ref {
			out = append(out, existing)
		}
	}
	if len(out) == len(refs) {
		return nil
	}
	return r.write(out)
}

func (r *Registry) write(refs []string) error {
	sort.Strings(refs)
	data := strings.Join(refs, "\n")
	if data != "" {
		data += "\n"
	}
	if err := os.WriteFile(r.path, []byte(data), 0o600); err != nil {
		return fmt.Errorf("registry write: %w", err)
	}
	return nil
}
