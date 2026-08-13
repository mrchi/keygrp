package exportimport

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/mrchi/keygrp/internal/archive"
	"github.com/mrchi/keygrp/internal/keychain"
	"github.com/mrchi/keygrp/internal/registry"
	"github.com/mrchi/keygrp/internal/testutil"
)

// storeWithErr delegates to a testutil.MemStore but returns a wrapped
// ErrBackend for a chosen ref on Get and/or Set, mimicking a non-ErrNotFound
// keychain backend failure at the operation under test.
type storeWithErr struct {
	store     testutil.MemStore
	errRef    string // Get fails for this ref
	setErrRef string // Set fails for this ref
}

func (s storeWithErr) Get(ref string) (string, error) {
	if ref == s.errRef {
		return "", fmt.Errorf("keychain get %q: %w: simulated failure", ref, keychain.ErrBackend)
	}
	return s.store.Get(ref)
}
func (s storeWithErr) Set(ref, value string) error {
	if ref == s.setErrRef {
		return fmt.Errorf("keychain set %q: %w: simulated failure", ref, keychain.ErrBackend)
	}
	return s.store.Set(ref, value)
}
func (s storeWithErr) Delete(ref string) error { return s.store.Delete(ref) }

func pw(v string) PasswordFunc { return func() (string, error) { return v, nil } }

func newRegistry(t *testing.T, refs ...string) *registry.Registry {
	t.Helper()
	r := registry.New(filepath.Join(t.TempDir(), "refs"))
	for _, ref := range refs {
		if err := r.Add(ref); err != nil {
			t.Fatalf("registry.Add(%q) error = %v", ref, err)
		}
	}
	return r
}

func TestExportRoundTrip(t *testing.T) {
	store := testutil.MemStore{"a": "va", "b": "vb"}
	reg := newRegistry(t, "a", "b")

	data, out, err := Export(store, reg, pw("hunter2"), "dev")
	if err != nil {
		t.Fatalf("Export() error = %v", err)
	}
	if out.Exported != 2 {
		t.Errorf("out.Exported = %d, want 2", out.Exported)
	}
	if len(out.Missing) != 0 {
		t.Errorf("out.Missing = %v, want none", out.Missing)
	}
	got, _, err := archive.Decrypt(data, "hunter2")
	if err != nil {
		t.Fatalf("Decrypt(exported archive) error = %v", err)
	}
	want := map[string]string{"a": "va", "b": "vb"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Decrypt() = %#v, want %#v", got, want)
	}
}

func TestExportMissingRefIsPartial(t *testing.T) {
	store := testutil.MemStore{"a": "va"} // "missing" is in the registry but not the keychain
	reg := newRegistry(t, "a", "missing")

	data, out, err := Export(store, reg, pw("pw"), "dev")
	if err != nil {
		t.Fatalf("Export() error = %v", err)
	}
	if out.Exported != 1 {
		t.Errorf("out.Exported = %d, want 1", out.Exported)
	}
	if want := []string{"missing"}; !reflect.DeepEqual(out.Missing, want) {
		t.Errorf("out.Missing = %v, want %v", out.Missing, want)
	}
	// The archive is still produced and decryptable, with the missing ref absent.
	got, _, err := archive.Decrypt(data, "pw")
	if err != nil {
		t.Fatalf("Decrypt(partial archive) error = %v", err)
	}
	if _, ok := got["missing"]; ok {
		t.Errorf("Decrypt() contains skipped ref %q, want absent", "missing")
	}
	if !reflect.DeepEqual(got, map[string]string{"a": "va"}) {
		t.Errorf("Decrypt() = %#v, want %#v", got, map[string]string{"a": "va"})
	}
}

func TestExportBackendErrorAborts(t *testing.T) {
	store := storeWithErr{store: testutil.MemStore{"a": "va"}, errRef: "boom"}
	reg := newRegistry(t, "a", "boom")

	data, _, err := Export(store, reg, pw("pw"), "dev")
	if err == nil {
		t.Fatal("Export() = nil error, want backend error")
	}
	if !errors.Is(err, keychain.ErrBackend) {
		t.Errorf("Export() error = %v, want wrapped ErrBackend", err)
	}
	// The abort contract: no archive bytes escape, whatever was read before
	// the failure (Exported may already count the refs read in order).
	if data != nil {
		t.Errorf("Export() returned data (%d bytes) on backend error, want nil", len(data))
	}
}

func TestExportEmptyRegistry(t *testing.T) {
	store := testutil.MemStore{}
	reg := newRegistry(t) // no refs

	data, out, err := Export(store, reg, pw("pw"), "dev")
	if err != nil {
		t.Fatalf("Export() error = %v", err)
	}
	if out.Exported != 0 || len(out.Missing) != 0 {
		t.Errorf("out = %+v, want zero outcome", out)
	}
	got, _, err := archive.Decrypt(data, "pw")
	if err != nil {
		t.Fatalf("Decrypt(empty archive) error = %v", err)
	}
	if len(got) != 0 {
		t.Errorf("Decrypt() = %#v, want empty map", got)
	}
}

func TestExportRegistryReadErrorIsTyped(t *testing.T) {
	store := testutil.MemStore{}
	// A directory where the refs file should be: Refs() fails, and Export must
	// surface it as ErrRegistry so main can map it to exit 1 (file I/O), not a
	// keychain condition (exit 2).
	reg := registry.New(t.TempDir())

	_, _, err := Export(store, reg, pw("pw"), "dev")
	if !errors.Is(err, ErrRegistry) {
		t.Errorf("Export() error = %v, want wrapped ErrRegistry", err)
	}
}

func TestExportPasswordErrorPropagates(t *testing.T) {
	store := testutil.MemStore{"a": "va"}
	reg := newRegistry(t, "a")
	pwErr := errors.New("passwords do not match; aborting")

	data, _, err := Export(store, reg, func() (string, error) { return "", pwErr }, "dev")
	if !errors.Is(err, pwErr) {
		t.Errorf("Export() error = %v, want the password error", err)
	}
	if data != nil {
		t.Errorf("Export() returned data on password error, want nil")
	}
}

func TestWriteArchive(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "backup.kgx")

	if err := WriteArchive(path, []byte("payload")); err != nil {
		t.Fatalf("WriteArchive() error = %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "payload" {
		t.Errorf("ReadFile() = %q, want %q", data, "payload")
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("archive perm = %v, want 0600", perm)
	}

	// A subsequent export overwrites the default file.
	if err := WriteArchive(path, []byte("payload2")); err != nil {
		t.Fatalf("WriteArchive(overwrite) error = %v", err)
	}
	data, err = os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "payload2" {
		t.Errorf("ReadFile() after overwrite = %q, want %q", data, "payload2")
	}

	// The temp file from the atomic write is cleaned up.
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != "backup.kgx" {
		t.Errorf("dir entries = %v, want only backup.kgx", entries)
	}
}

// mustEncrypt seals refs with a fixed password for import tests.
func mustEncrypt(t *testing.T, refs map[string]string, password string) []byte {
	t.Helper()
	data, err := archive.Encrypt(refs, password, "dev")
	if err != nil {
		t.Fatalf("archive.Encrypt() error = %v", err)
	}
	return data
}

func TestImportOverwritesByDefault(t *testing.T) {
	store := testutil.MemStore{"a": "old"}
	reg := newRegistry(t) // empty registry: import must add the refs
	data := mustEncrypt(t, map[string]string{"a": "new", "b": "vb"}, "pw")

	out, err := Import(store, reg, pw("pw"), data, false)
	if err != nil {
		t.Fatalf("Import() error = %v", err)
	}
	if out.Imported != 2 || out.Skipped != 0 {
		t.Errorf("out = %+v, want Imported=2 Skipped=0", out)
	}
	// The archive is authoritative: an existing keychain item is overwritten.
	if store["a"] != "new" {
		t.Errorf("store[a] = %q, want %q (overwritten)", store["a"], "new")
	}
	if store["b"] != "vb" {
		t.Errorf("store[b] = %q, want %q", store["b"], "vb")
	}
	// Written refs reach the registry so secret list shows them.
	refs, err := reg.Refs()
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"a", "b"}; !reflect.DeepEqual(refs, want) {
		t.Errorf("registry refs = %v, want %v", refs, want)
	}
}

func TestImportSkipExistingLeavesUntouched(t *testing.T) {
	store := testutil.MemStore{"a": "old"}
	reg := newRegistry(t, "a") // "a" is already tracked
	data := mustEncrypt(t, map[string]string{"a": "new", "b": "vb"}, "pw")

	out, err := Import(store, reg, pw("pw"), data, true)
	if err != nil {
		t.Fatalf("Import() error = %v", err)
	}
	if out.Imported != 1 || out.Skipped != 1 {
		t.Errorf("out = %+v, want Imported=1 Skipped=1", out)
	}
	if store["a"] != "old" {
		t.Errorf("store[a] = %q, want %q (untouched)", store["a"], "old")
	}
	if store["b"] != "vb" {
		t.Errorf("store[b] = %q, want %q", store["b"], "vb")
	}
}

func TestImportWrongPasswordFails(t *testing.T) {
	store := testutil.MemStore{}
	reg := newRegistry(t)
	data := mustEncrypt(t, map[string]string{"a": "b"}, "right")

	_, err := Import(store, reg, pw("wrong"), data, false)
	if !errors.Is(err, archive.ErrAuth) {
		t.Errorf("Import() error = %v, want ErrAuth", err)
	}
	if len(store) != 0 {
		t.Errorf("store = %v, want empty (no partial import on bad password)", store)
	}
}

func TestImportEmptyArchive(t *testing.T) {
	store := testutil.MemStore{}
	reg := newRegistry(t)
	data := mustEncrypt(t, map[string]string{}, "pw")

	out, err := Import(store, reg, pw("pw"), data, false)
	if err != nil {
		t.Fatalf("Import() error = %v", err)
	}
	if out.Imported != 0 {
		t.Errorf("out.Imported = %d, want 0", out.Imported)
	}
}

func TestImportEmptyArchiveDoesNotCreateDir(t *testing.T) {
	// An empty archive writes nothing, so on a fresh machine import must not
	// create the config dir (exportimport.go guards EnsureDir on refs).
	store := testutil.MemStore{}
	dir := filepath.Join(t.TempDir(), "config") // does not exist yet
	reg := registry.New(filepath.Join(dir, "refs"))
	data := mustEncrypt(t, map[string]string{}, "pw")

	if _, err := Import(store, reg, pw("pw"), data, false); err != nil {
		t.Fatalf("Import() error = %v", err)
	}
	if _, err := os.Stat(dir); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("config dir %q exists after empty import, want absent", dir)
	}
}

func TestImportSetErrorAborts(t *testing.T) {
	// Set fails for "boom": refs sorted before it may be written, but the
	// import must abort and never report success.
	store := storeWithErr{store: testutil.MemStore{}, setErrRef: "boom"}
	reg := newRegistry(t)
	data := mustEncrypt(t, map[string]string{"a": "va", "boom": "vb"}, "pw")

	_, err := Import(store, reg, pw("pw"), data, false)
	if err == nil || !errors.Is(err, keychain.ErrBackend) {
		t.Fatalf("Import() error = %v, want wrapped ErrBackend", err)
	}
	if _, ok := store.store["boom"]; ok {
		t.Errorf("store contains %q, want aborted before writing it", "boom")
	}
}

func TestImportSkipExistingBackendErrorAborts(t *testing.T) {
	// With --skip-existing, determining existence requires a Get; a backend
	// error there is ambiguous and must abort, not guess.
	store := storeWithErr{store: testutil.MemStore{"a": "old"}, errRef: "a"}
	reg := newRegistry(t)
	data := mustEncrypt(t, map[string]string{"a": "new", "b": "vb"}, "pw")

	_, err := Import(store, reg, pw("pw"), data, true)
	if err == nil || !errors.Is(err, keychain.ErrBackend) {
		t.Errorf("Import() error = %v, want wrapped ErrBackend", err)
	}
	if store.store["a"] != "old" {
		t.Errorf("store[a] = %q, want %q (aborted before overwrite)", store.store["a"], "old")
	}
}

func TestImportRegistryDirErrorIsTyped(t *testing.T) {
	store := testutil.MemStore{}
	// A regular file blocks the registry's containing directory, so EnsureDir
	// cannot create it. Import must surface that as ErrRegistry (main maps it to
	// exit 1, a file I/O error, not a keychain condition — ADR-0001 §6) and must
	// abort before any keychain write, so no item is written that the registry
	// then fails to track.
	blocker := filepath.Join(t.TempDir(), "blocker")
	if err := os.WriteFile(blocker, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	reg := registry.New(filepath.Join(blocker, "refs"))
	data := mustEncrypt(t, map[string]string{"a": "va"}, "pw")

	_, err := Import(store, reg, pw("pw"), data, false)
	if !errors.Is(err, ErrRegistry) {
		t.Errorf("Import() error = %v, want wrapped ErrRegistry", err)
	}
	if len(store) != 0 {
		t.Errorf("store = %v, want empty (aborted before any keychain write)", store)
	}
}

func TestImportPasswordErrorPropagates(t *testing.T) {
	store := testutil.MemStore{}
	reg := newRegistry(t)
	data := mustEncrypt(t, map[string]string{"a": "b"}, "pw")
	pwErr := errors.New("cannot open controlling terminal")

	_, err := Import(store, reg, func() (string, error) { return "", pwErr }, data, false)
	if !errors.Is(err, pwErr) {
		t.Errorf("Import() error = %v, want the password error", err)
	}
	if len(store) != 0 {
		t.Errorf("store = %v, want empty on password error", store)
	}
}

func TestExportImportRoundTripViaPipe(t *testing.T) {
	// A stdout/stdin pipe round-trip: Export produces bytes, Import consumes
	// them directly (as if streamed over a pipe).
	src := testutil.MemStore{"a": "va", "b": "vb"}
	srcReg := newRegistry(t, "a", "b")
	data, _, err := Export(src, srcReg, pw("pw"), "dev")
	if err != nil {
		t.Fatalf("Export() error = %v", err)
	}

	dst := testutil.MemStore{}
	dstReg := newRegistry(t)
	out, err := Import(dst, dstReg, pw("pw"), data, false)
	if err != nil {
		t.Fatalf("Import() error = %v", err)
	}
	if out.Imported != 2 {
		t.Errorf("out.Imported = %d, want 2", out.Imported)
	}
	if !reflect.DeepEqual(dst, testutil.MemStore{"a": "va", "b": "vb"}) {
		t.Errorf("dst = %v, want every ref and value restored", dst)
	}
	refs, err := dstReg.Refs()
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"a", "b"}; !reflect.DeepEqual(refs, want) {
		t.Errorf("registry refs = %v, want %v", refs, want)
	}
}

func TestExportImportRoundTripViaFile(t *testing.T) {
	// A file round-trip: the archive is written to disk (as export does), read
	// back (as import does), and restores every ref and value.
	src := testutil.MemStore{"a": "va", "b": "vb"}
	srcReg := newRegistry(t, "a", "b")
	data, _, err := Export(src, srcReg, pw("pw"), "dev")
	if err != nil {
		t.Fatalf("Export() error = %v", err)
	}
	path := filepath.Join(t.TempDir(), "backup.kgx")
	if err := WriteArchive(path, data); err != nil {
		t.Fatalf("WriteArchive() error = %v", err)
	}
	data, err = os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	dst := testutil.MemStore{}
	dstReg := newRegistry(t)
	out, err := Import(dst, dstReg, pw("pw"), data, false)
	if err != nil {
		t.Fatalf("Import() error = %v", err)
	}
	if out.Imported != 2 {
		t.Errorf("out.Imported = %d, want 2", out.Imported)
	}
	if !reflect.DeepEqual(dst, testutil.MemStore{"a": "va", "b": "vb"}) {
		t.Errorf("dst = %v, want every ref and value restored", dst)
	}
}
