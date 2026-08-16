// Package exportimport orchestrates secret export and import against the
// keychain, the refs registry, and the password source. It is the single test
// seam for the feature (ADR-0005): the cli package wires the production dependencies
// and maps the outcome to an exit code; tests inject a fake store, a fake
// password source, and a temp-file registry.
package exportimport

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/mrchi/keygrp/internal/archive"
	"github.com/mrchi/keygrp/internal/keychain"
)

// PasswordFunc supplies the archive password. main wires it to the terminal
// prompt; an error (e.g. a re-entry mismatch) aborts the operation.
type PasswordFunc func() (string, error)

// ErrRegistry marks a failure reading the refs registry — a file I/O error
// (exit 1, mirroring secret list), not a keychain condition (exit 2, ADR-0001 §6).
var ErrRegistry = errors.New("refs registry error")

// refsRegistry is the part of the registry the seam needs — enumerate the refs
// keygrp has written and record newly written ones. *registry.Registry satisfies it.
type refsRegistry interface {
	Refs() ([]string, error)
	Add(ref string) error
	// EnsureDir creates the registry's containing directory as needed, so a
	// fresh-machine import can write refs (ADR-0005: written refs mirror set).
	EnsureDir() error
}

// Outcome reports what an export did, so main can warn and choose an exit code.
type Outcome struct {
	Exported int      // refs written to the archive
	Missing  []string // registry refs absent from the keychain, skipped
}

// Export reads every ref from the registry, reads each value from the
// keychain, and seals the ref→value map into a password-encrypted archive.
// A ref missing from the keychain is skipped and reported in the outcome — the
// archive is still produced, and the caller decides it is partial; any other
// (non-ErrNotFound) backend error aborts with no archive. The version string is
// recorded in the plaintext metadata.
func Export(store keychain.Store, reg refsRegistry, password PasswordFunc, version string) ([]byte, Outcome, error) {
	var out Outcome
	refs, err := reg.Refs()
	if err != nil {
		return nil, out, fmt.Errorf("read refs registry: %w: %v", ErrRegistry, err)
	}
	pw, err := password()
	if err != nil {
		return nil, out, err
	}
	refsMap := make(map[string]string, len(refs))
	for _, ref := range refs {
		value, ok, err := getValue(store, ref)
		if err != nil {
			return nil, out, err
		}
		if !ok {
			out.Missing = append(out.Missing, ref)
			continue
		}
		refsMap[ref] = value
		out.Exported++
	}
	data, err := archive.Encrypt(refsMap, pw, version)
	if err != nil {
		return nil, out, err
	}
	return data, out, nil
}

// ImportOutcome reports what an import did, so main can print a count.
type ImportOutcome struct {
	Imported int // refs written to the keychain and registry
	Skipped  int // existing refs left untouched (--skip-existing)
}

// Import decrypts data (the archive, read by the caller from a file or stdin)
// and writes every ref into the keychain, overwriting by default — the archive
// is authoritative. With skipExisting, a ref already in the keychain is left
// untouched (its registry entry too). Every written ref is added to the refs
// registry, mirroring secret set. A wrong password or corrupted archive
// surfaces as archive.ErrAuth and nothing is written. A non-ErrNotFound
// backend error aborts.
func Import(store keychain.Store, reg refsRegistry, password PasswordFunc, data []byte, skipExisting bool) (ImportOutcome, error) {
	var out ImportOutcome
	pw, err := password()
	if err != nil {
		return out, err
	}
	refs, _, err := archive.Decrypt(data, pw)
	if err != nil {
		return out, err
	}
	// The registry co-locates with the config dir, which may not exist yet on a
	// fresh machine (import's primary case). Ensure it now — after a successful
	// decrypt, before any keychain write — so a restore cannot write a keychain
	// item the registry then fails to track. A wrong password or corrupted
	// archive returns above, before any directory is created; an empty archive
	// writes nothing, so no directory is needed.
	if len(refs) > 0 {
		if err := reg.EnsureDir(); err != nil {
			return out, fmt.Errorf("registry dir: %w: %v", ErrRegistry, err)
		}
	}
	for _, ref := range sortedKeys(refs) {
		if skipExisting {
			_, ok, err := getValue(store, ref)
			if err != nil {
				return out, err
			}
			if ok {
				out.Skipped++
				continue
			}
		}
		if err := store.Set(ref, refs[ref]); err != nil {
			return out, fmt.Errorf("write %q to keychain: %w", ref, err)
		}
		if err := reg.Add(ref); err != nil {
			return out, fmt.Errorf("%w: %v", ErrRegistry, err)
		}
		out.Imported++
	}
	return out, nil
}

// getValue reads ref from the keychain, distinguishing a missing ref (ok=false,
// no error) from any other backend failure (a wrapped error). Export and Import
// both classify a Get this way, and share the read-error wording.
func getValue(store keychain.Store, ref string) (value string, ok bool, err error) {
	value, err = store.Get(ref)
	if err == nil {
		return value, true, nil
	}
	if errors.Is(err, keychain.ErrNotFound) {
		return "", false, nil
	}
	return "", false, fmt.Errorf("read %q from keychain: %w", ref, err)
}

// sortedKeys returns the map's keys in sorted order, so import writes refs in a
// deterministic sequence.
func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// WriteArchive writes data to path atomically with 0600 permissions: a temp
// file in the same directory is renamed over path, so an interrupted export
// never leaves a truncated archive and a previous archive is never partially
// clobbered. The temp file is removed on any failure.
func WriteArchive(path string, data []byte) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".kgx-*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }() // no-op after a successful rename
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpName, path); err != nil {
		return err
	}
	return nil
}
